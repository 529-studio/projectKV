package telegrambot

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	// WebhookPath is the inbound endpoint served by RegisterRoutes.
	WebhookPath = "/api/notify/telegram/webhook"

	// SubscriptionChannel is the channel_name persisted for bot subscriptions.
	SubscriptionChannel = "telegram"

	defaultWorkers          = 4
	defaultQueueSize        = 256
	defaultMaxSubsPerChat   = 20
	defaultPerChatPerMinute = 6

	// dedupeTTL / dedupeCapacity bound the seen-update_id cache (spec §2.3).
	dedupeTTL      = 15 * time.Minute
	dedupeCapacity = 2048

	// maxWebhookBodyBytes caps an update body (spec §2.2).
	maxWebhookBodyBytes = 64 << 10

	// defaultWebURL is the citizen-facing site linked from /help.
	defaultWebURL = "https://warded.529studio.site"

	helpNoticeWindow = time.Minute

	secretHeader = "X-Telegram-Bot-Api-Secret-Token"
)

var (
	// botUsernamePattern validates a public Telegram handle (not a credential).
	botUsernamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{3,30}$`)
	// webhookSecretPattern validates the Telegram secret token alphabet/length.
	webhookSecretPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{32,256}$`)
)

// Store is the persistence port the bot needs. cmd/server adapts the
// subscription SQLite store to it (DIP); telegrambot imports no internal package.
type Store interface {
	Subscribe(ctx context.Context, channelName, chatID string, rec Record) error
	UnsubscribeAll(ctx context.Context, channelName, chatID string) (int64, error)
	List(ctx context.Context, channelName, chatID string) ([]Record, error)
}

// Sender delivers one HTML reply to a chat. The adapter owns token redaction.
type Sender interface {
	SendMessage(ctx context.Context, chatID, html string) error
}

// Taxonomy validates and labels targets against the live catalog.
type Taxonomy interface {
	CanonicalZone(string) (string, bool)
	CanonicalWard(string) (string, bool)
	CanonicalChokepoint(string) (string, bool)
	Label(Target) string
}

// Config controls the bot runtime. Zero numeric fields take the spec defaults.
type Config struct {
	BotUsername   string
	WebhookSecret string
	// WebURL is the public site linked from /help (optional, has a default).
	WebURL string

	Workers          int
	QueueSize        int
	MaxSubsPerChat   int
	PerChatPerMinute int
}

func (c Config) normalized() Config {
	c.BotUsername = strings.TrimPrefix(strings.TrimSpace(c.BotUsername), "@")
	c.WebhookSecret = strings.TrimSpace(c.WebhookSecret)
	c.WebURL = strings.TrimSpace(c.WebURL)
	if c.WebURL == "" {
		c.WebURL = defaultWebURL
	}
	if c.Workers <= 0 {
		c.Workers = defaultWorkers
	}
	if c.QueueSize <= 0 {
		c.QueueSize = defaultQueueSize
	}
	if c.MaxSubsPerChat <= 0 {
		c.MaxSubsPerChat = defaultMaxSubsPerChat
	}
	if c.PerChatPerMinute <= 0 {
		c.PerChatPerMinute = defaultPerChatPerMinute
	}
	return c
}

// Handler owns the webhook pipeline: authenticate, dedupe, enqueue, and reply
// from a small worker pool so Telegram is never kept waiting on the Bot API.
type Handler struct {
	cfg      Config
	store    Store
	taxonomy Taxonomy
	sender   Sender
	logger   *slog.Logger

	jobs     chan job
	quit     chan struct{}
	workers  sync.WaitGroup
	inflight sync.WaitGroup

	dedupe   *updateDedupe
	limiter  *chatLimiter
	helpNote *chatThrottle
	now      func() time.Time

	closeOnce sync.Once
	closed    atomic.Bool
}

type job struct {
	updateID int64
	message  *telegramMessage
}

// NewHandler builds the webhook handler and starts its worker pool.
// Call Shutdown to drain and stop it.
func NewHandler(cfg Config, store Store, taxonomy Taxonomy, sender Sender, logger *slog.Logger) *Handler {
	cfg = cfg.normalized()
	if logger == nil {
		logger = slog.Default()
	}
	now := time.Now
	h := &Handler{
		cfg:      cfg,
		store:    store,
		taxonomy: taxonomy,
		sender:   sender,
		logger:   logger,
		jobs:     make(chan job, cfg.QueueSize),
		quit:     make(chan struct{}),
		now:      now,
		dedupe:   &updateDedupe{ttl: dedupeTTL, capacity: dedupeCapacity, now: now, entries: make(map[int64]time.Time)},
		limiter:  newChatLimiter(cfg.PerChatPerMinute, time.Minute, now),
		helpNote: &chatThrottle{window: helpNoticeWindow, now: now, last: make(map[string]time.Time)},
	}
	for i := 0; i < cfg.Workers; i++ {
		h.workers.Add(1)
		go h.worker()
	}
	return h
}

// ValidBotUsername reports whether raw is a syntactically valid public Telegram
// handle. A public handle is not a credential (spec §2.6); the composition root
// uses this to decide whether the webhook may be mounted.
func ValidBotUsername(raw string) bool {
	return botUsernamePattern.MatchString(strings.TrimPrefix(strings.TrimSpace(raw), "@"))
}

// ValidWebhookSecret reports whether raw matches Telegram's secret-token
// alphabet and length. The value itself is never logged.
func ValidWebhookSecret(raw string) bool {
	return webhookSecretPattern.MatchString(strings.TrimSpace(raw))
}

// Enabled reports whether the webhook may be served. Fail-closed: an invalid or
// missing username or secret leaves the route unregistered (spec §2.2, §2.6).
func (h *Handler) Enabled() bool {
	return ValidBotUsername(h.cfg.BotUsername) && ValidWebhookSecret(h.cfg.WebhookSecret)
}

// RegisterRoutes mounts the webhook, or does nothing when the bot is
// unprovisioned so the endpoint answers 404 instead of accepting traffic.
func (h *Handler) RegisterRoutes(r *gin.Engine) {
	if r == nil || !h.Enabled() {
		return
	}
	r.POST(WebhookPath, h.handleWebhook)
}

// Shutdown stops accepting work, drains the queue and waits for in-flight jobs.
func (h *Handler) Shutdown(ctx context.Context) error {
	h.closeOnce.Do(func() {
		h.closed.Store(true)
		close(h.quit)
	})
	done := make(chan struct{})
	go func() {
		h.workers.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// --- Telegram wire types ---------------------------------------------------

type telegramUser struct {
	ID       int64  `json:"id"`
	IsBot    bool   `json:"is_bot"`
	Username string `json:"username"`
}

type telegramChat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

type telegramMessage struct {
	MessageID int64         `json:"message_id"`
	From      *telegramUser `json:"from"`
	Chat      telegramChat  `json:"chat"`
	Date      int64         `json:"date"`
	Text      string        `json:"text"`
}

type telegramUpdate struct {
	UpdateID      int64            `json:"update_id"`
	Message       *telegramMessage `json:"message"`
	EditedMessage *telegramMessage `json:"edited_message"`
	ChannelPost   *telegramMessage `json:"channel_post"`
}

// --- Webhook ---------------------------------------------------------------

func (h *Handler) handleWebhook(c *gin.Context) {
	if !h.secretMatches(c.GetHeader(secretHeader)) {
		// Never echo the supplied value; the body stays empty (spec E12).
		c.Writer.WriteHeader(http.StatusUnauthorized)
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxWebhookBodyBytes)
	var update telegramUpdate
	if err := json.NewDecoder(c.Request.Body).Decode(&update); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			c.Writer.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"ok": false})
		return
	}

	// A duplicate must not create a second row or a second reply (spec E6).
	if h.dedupe.seen(update.UpdateID) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
		return
	}

	// Refuse new work once Shutdown has closed the queue: no job can be left
	// with an unbalanced in-flight count, so Shutdown cannot hang.
	if h.closed.Load() {
		c.Header("Retry-After", "5")
		c.JSON(http.StatusServiceUnavailable, gin.H{"ok": false})
		return
	}

	h.inflight.Add(1)
	select {
	case h.jobs <- job{updateID: update.UpdateID, message: update.Message}:
		// Remember only after the update is queued: if the queue is full we
		// answer 503 so Telegram retries the same update_id (spec E11).
		h.dedupe.remember(update.UpdateID)
	default:
		h.inflight.Done()
		c.Header("Retry-After", "5")
		c.JSON(http.StatusServiceUnavailable, gin.H{"ok": false})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) secretMatches(supplied string) bool {
	want := h.cfg.WebhookSecret
	if want == "" || len(supplied) != len(want) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(supplied), []byte(want)) == 1
}

// --- Worker pipeline -------------------------------------------------------

func (h *Handler) worker() {
	defer h.workers.Done()
	for {
		select {
		case j := <-h.jobs:
			h.process(j)
		case <-h.quit:
			// Drain what is already queued, then stop.
			for {
				select {
				case j := <-h.jobs:
					h.process(j)
				default:
					return
				}
			}
		}
	}
}

func (h *Handler) process(j job) {
	defer h.inflight.Done()
	started := h.now()

	msg := j.message
	if msg == nil || msg.From == nil || msg.From.IsBot {
		return
	}

	chatID := strconv.FormatInt(msg.Chat.ID, 10)

	// Groups and channels get exactly one guidance reply and nothing persisted.
	if msg.Chat.Type != "private" {
		h.reply(msg.Chat.ID, RenderPrivateChatOnly(h.cfg.BotUsername))
		h.log("non_private", chatID, "", j.updateID, started)
		return
	}

	if allowed, notify := h.limiter.allow(chatID); !allowed {
		if notify {
			h.reply(msg.Chat.ID, RenderSlowDown())
			h.log("rate_limited", chatID, "", j.updateID, started)
		}
		return
	}

	cmd := ParseCommand(msg.Text, h.cfg.BotUsername)
	if cmd.Name == "" {
		h.replyHelp(chatID, msg.Chat.ID)
		h.log("non_command", chatID, "", j.updateID, started)
		return
	}

	switch cmd.Name {
	case "start":
		h.handleStart(chatID, msg.Chat.ID, cmd.Arg)
	case "help":
		h.reply(msg.Chat.ID, RenderHelp(h.cfg.WebURL))
	case "status":
		h.handleStatus(chatID, msg.Chat.ID)
	case "stop", "unsubscribe":
		h.handleStop(chatID, msg.Chat.ID)
	default:
		h.replyHelp(chatID, msg.Chat.ID)
	}

	h.log("handled", chatID, cmd.Name, j.updateID, started)
}

func (h *Handler) handleStart(chatID string, numericChatID int64, arg string) {
	ctx := context.Background()
	arg = strings.TrimSpace(arg)

	if arg == "" {
		subs, err := h.store.List(ctx, SubscriptionChannel, chatID)
		if err != nil {
			h.logger.Error("telegram start: list failed", "chat", tail4(chatID), "error", err)
			h.reply(numericChatID, RenderDeliveryFailed())
			return
		}
		h.reply(numericChatID, RenderWelcome(len(subs), h.cfg.BotUsername))
		return
	}

	decoded, err := DecodePayload(arg)
	if err != nil {
		h.reply(numericChatID, RenderUnknownTarget(arg))
		return
	}

	target, ok := h.resolve(decoded)
	if !ok {
		h.reply(numericChatID, RenderUnknownTarget(arg))
		return
	}

	subs, err := h.store.List(ctx, SubscriptionChannel, chatID)
	if err != nil {
		h.logger.Error("telegram start: list failed", "chat", tail4(chatID), "error", err)
		h.reply(numericChatID, RenderDeliveryFailed())
		return
	}

	label := h.labelFor(target)
	for _, existing := range subs {
		if sameTarget(existing.TargetType, existing.TargetID, target) {
			h.reply(numericChatID, RenderAlreadySubscribed(target, label))
			return
		}
	}
	if len(subs) >= h.cfg.MaxSubsPerChat {
		h.reply(numericChatID, RenderLimitReached(h.cfg.MaxSubsPerChat))
		return
	}

	rec := Record{TargetType: target.Type, TargetID: target.ID, CreatedAt: h.now()}
	if err := h.store.Subscribe(ctx, SubscriptionChannel, chatID, rec); err != nil {
		h.logger.Error("telegram start: subscribe failed", "chat", tail4(chatID), "error", err)
		h.reply(numericChatID, RenderDeliveryFailed())
		return
	}
	h.reply(numericChatID, RenderSubscribed(target, label))
}

func (h *Handler) handleStatus(chatID string, numericChatID int64) {
	subs, err := h.store.List(context.Background(), SubscriptionChannel, chatID)
	if err != nil {
		h.logger.Error("telegram status: list failed", "chat", tail4(chatID), "error", err)
		h.reply(numericChatID, RenderDeliveryFailed())
		return
	}
	h.reply(numericChatID, RenderStatus(subs, h.taxonomy))
}

func (h *Handler) handleStop(chatID string, numericChatID int64) {
	removed, err := h.store.UnsubscribeAll(context.Background(), SubscriptionChannel, chatID)
	if err != nil {
		h.logger.Error("telegram stop: unsubscribe failed", "chat", tail4(chatID), "error", err)
		h.reply(numericChatID, RenderDeliveryFailed())
		return
	}
	h.reply(numericChatID, RenderStopped(int(removed)))
}

// replyHelp sends the /help body at most once per chat per minute (spec §2.4).
func (h *Handler) replyHelp(chatID string, numericChatID int64) {
	if !h.helpNote.allow(chatID) {
		return
	}
	h.reply(numericChatID, RenderHelp(h.cfg.WebURL))
}

// resolve validates a decoded target against the live taxonomy and returns the
// canonical form (legacy zone aliases, prefixed wards, camera ids).
func (h *Handler) resolve(t Target) (Target, bool) {
	if t.Type == targetTypeAll {
		return t, true
	}
	if h.taxonomy == nil {
		return Target{}, false
	}
	switch t.Type {
	case targetTypeZone:
		if canonical, ok := h.taxonomy.CanonicalZone(t.ID); ok {
			return Target{Type: targetTypeZone, ID: canonical}, true
		}
	case targetTypeWard:
		if canonical, ok := h.taxonomy.CanonicalWard(t.ID); ok {
			return Target{Type: targetTypeWard, ID: canonical}, true
		}
	case targetTypeChokepoint:
		if canonical, ok := h.taxonomy.CanonicalChokepoint(t.ID); ok {
			return Target{Type: targetTypeChokepoint, ID: canonical}, true
		}
	}
	return Target{}, false
}

func (h *Handler) labelFor(t Target) string {
	if t.Type == targetTypeAll || h.taxonomy == nil {
		return ""
	}
	return h.taxonomy.Label(t)
}

func (h *Handler) reply(numericChatID int64, html string) {
	chatID := strconv.FormatInt(numericChatID, 10)
	if err := h.sender.SendMessage(context.Background(), chatID, html); err != nil {
		h.logger.Warn("telegram reply failed", "chat", tail4(chatID), "error", err)
	}
}

func (h *Handler) log(outcome, chatID, cmd string, updateID int64, started time.Time) {
	h.logger.Info("telegram update",
		"update", updateID,
		"chat", tail4(chatID),
		"cmd", cmd,
		"outcome", outcome,
		"duration_ms", time.Since(started).Milliseconds(),
	)
}

// sameTarget compares a stored row with a canonical target. Zone and ward names
// are matched case-insensitively; chokepoint ids are exact.
func sameTarget(storedType, storedID string, t Target) bool {
	if storedType != t.Type {
		return false
	}
	if t.Type == targetTypeChokepoint {
		return storedID == t.ID
	}
	return strings.EqualFold(strings.TrimSpace(storedID), strings.TrimSpace(t.ID))
}

// tail4 returns the last four characters of a chat id, the only part allowed in
// logs (spec §2.3: never the full chat id).
func tail4(chatID string) string {
	if len(chatID) <= 4 {
		return chatID
	}
	return chatID[len(chatID)-4:]
}

// --- Dedupe ----------------------------------------------------------------

type updateDedupe struct {
	mu       sync.Mutex
	ttl      time.Duration
	capacity int
	now      func() time.Time
	entries  map[int64]time.Time
}

// seen reports whether updateID was already processed, pruning expired entries.
func (d *updateDedupe) seen(updateID int64) bool {
	now := d.now()
	d.mu.Lock()
	defer d.mu.Unlock()
	for id, at := range d.entries {
		if now.Sub(at) > d.ttl {
			delete(d.entries, id)
		}
	}
	_, ok := d.entries[updateID]
	return ok
}

// remember records updateID, evicting the oldest entry at capacity.
func (d *updateDedupe) remember(updateID int64) {
	now := d.now()
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.entries) >= d.capacity {
		var oldestID int64
		var oldest time.Time
		first := true
		for id, at := range d.entries {
			if first || at.Before(oldest) {
				oldestID, oldest, first = id, at, false
			}
		}
		if !first {
			delete(d.entries, oldestID)
		}
	}
	d.entries[updateID] = now
}

// --- Per-chat limiter ------------------------------------------------------

type chatBucket struct {
	count       int
	windowStart time.Time
	lastNotice  time.Time
}

type chatLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	now     func() time.Time
	buckets map[string]*chatBucket
}

func newChatLimiter(limit int, window time.Duration, now func() time.Time) *chatLimiter {
	if limit <= 0 {
		limit = defaultPerChatPerMinute
	}
	if now == nil {
		now = time.Now
	}
	return &chatLimiter{limit: limit, window: window, now: now, buckets: make(map[string]*chatBucket)}
}

// allow reports whether the command may run and whether a "slow down" notice is
// due. At most one notice per chat per window; the rest stay silent.
func (l *chatLimiter) allow(chatID string) (allowed bool, notify bool) {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()

	bucket, ok := l.buckets[chatID]
	if !ok || now.Sub(bucket.windowStart) >= l.window {
		if len(l.buckets) > 4096 {
			for key, b := range l.buckets {
				if now.Sub(b.windowStart) >= 2*l.window {
					delete(l.buckets, key)
				}
			}
		}
		l.buckets[chatID] = &chatBucket{count: 1, windowStart: now}
		return true, false
	}
	if bucket.count < l.limit {
		bucket.count++
		return true, false
	}
	if bucket.lastNotice.IsZero() || now.Sub(bucket.lastNotice) >= l.window {
		bucket.lastNotice = now
		return false, true
	}
	return false, false
}

// chatThrottle allows one reply per chat per window (plain-text /help guidance).
type chatThrottle struct {
	mu     sync.Mutex
	window time.Duration
	now    func() time.Time
	last   map[string]time.Time
}

func (t *chatThrottle) allow(chatID string) bool {
	now := t.now()
	t.mu.Lock()
	defer t.mu.Unlock()
	if last, ok := t.last[chatID]; ok && now.Sub(last) < t.window {
		return false
	}
	if len(t.last) > 4096 {
		for key, at := range t.last {
			if now.Sub(at) >= 2*t.window {
				delete(t.last, key)
			}
		}
	}
	t.last[chatID] = now
	return true
}
