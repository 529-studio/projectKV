package subscription

import (
	"context"
	"fmt"
	"html"
	"log/slog"
	"math"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	// telegramChannel is the only citizen-facing delivery channel today.
	telegramChannel = "telegram"
	// confirmationTimeout bounds the best-effort confirmation DM so a slow Bot
	// API can never hold the POST response (S12-T4 spec §2.5).
	confirmationTimeout = 3 * time.Second
	// subscribeRatePerMinute / subscribeBurst protect the public POST route.
	// Web users are private chats; five subscriptions a minute is far above any
	// legitimate interaction and far below a useful enumeration rate.
	subscribeRatePerMinute = 5
	subscribeBurst         = 3
)

// chatIDPattern is the web direct-path contract: private chat ids only, 5 to 20
// digits. Group ids ("-100…") are rejected (S12-T4 spec §2.5). It lives here
// rather than in IsValid so historical rows stay readable.
var chatIDPattern = regexp.MustCompile(`^[0-9]{5,20}$`)

// MessageSender delivers one HTML message to a chat. notify.TelegramChannel
// satisfies it structurally; the composition root wires the adapter so this
// leaf package never imports notify (DIP, spec §4.3).
type MessageSender interface {
	SendMessage(ctx context.Context, chatID, html string) error
}

// Handler handles HTTP requests for managing flood alert subscriptions.
type Handler struct {
	store    Store
	taxonomy TaxonomyProvider

	botUsername      string
	deepLinkEnabled  bool
	confirmation     func(Subscription) string
	confirmationSend MessageSender
	subscribeLimiter gin.HandlerFunc
	logger           *slog.Logger
}

// Option configures the subscription Handler.
type Option func(*Handler)

// WithBotInfo publishes the bot handle and provisioning state on
// GET /api/subscriptions/taxonomy. The web UI disables its deep-link CTA when
// either is absent (AC4).
func WithBotInfo(username string, enabled bool) Option {
	return func(h *Handler) {
		h.botUsername = strings.TrimPrefix(strings.TrimSpace(username), "@")
		h.deepLinkEnabled = enabled && h.botUsername != ""
	}
}

// WithConfirmationRenderer replaces the bilingual confirmation copy sent after
// a successful direct-path subscription. The signature takes the stored row so
// callers can name the exact scope.
func WithConfirmationRenderer(renderer func(Subscription) string) Option {
	return func(h *Handler) {
		if renderer != nil {
			h.confirmation = renderer
		}
	}
}

// WithConfirmationSender installs the best-effort confirmation DM transport.
// Without it, POST /api/subscriptions behaves exactly as before.
func WithConfirmationSender(sender MessageSender) Option {
	return func(h *Handler) {
		if sender != nil {
			h.confirmationSend = sender
		}
	}
}

// WithSubscribeLimiter replaces the default per-IP limiter on POST. Passing nil
// disables limiting (tests and non-citizen deployments only).
func WithSubscribeLimiter(mw gin.HandlerFunc) Option {
	return func(h *Handler) {
		h.subscribeLimiter = mw
	}
}

// NewHandler creates a subscription Handler. taxonomy may be nil; the default
// confirmation copy and per-IP limiter are always installed unless overridden.
func NewHandler(store Store, taxonomy TaxonomyProvider, opts ...Option) *Handler {
	h := &Handler{
		store:            store,
		taxonomy:         taxonomy,
		confirmation:     DefaultConfirmationMessage,
		subscribeLimiter: newSubscribeLimiterWithClock(subscribeRatePerMinute, subscribeBurst, time.Now),
		logger:           slog.Default(),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(h)
		}
	}
	return h
}

// RouteOption configures route registration.
type RouteOption func(*routeOptions)

type routeOptions struct {
	auditAuth gin.HandlerFunc
}

// WithAuditAuth puts GET /api/subscriptions behind developer authentication.
// The endpoint enumerates chat ids, which must never be publicly readable
// (S12-T4 spec §2.5).
func WithAuditAuth(mw gin.HandlerFunc) RouteOption {
	return func(o *routeOptions) {
		o.auditAuth = mw
	}
}

// RegisterRoutes registers subscription endpoints on the given router group.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup, opts ...RouteOption) {
	if rg == nil {
		return
	}
	options := routeOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(&options)
		}
	}

	subs := rg.Group("/subscriptions")
	{
		postHandlers := make([]gin.HandlerFunc, 0, 2)
		if h.subscribeLimiter != nil {
			postHandlers = append(postHandlers, h.subscribeLimiter)
		}
		postHandlers = append(postHandlers, h.Subscribe)
		subs.POST("", postHandlers...)
		subs.DELETE("", h.Unsubscribe)
		if options.auditAuth != nil {
			subs.GET("", options.auditAuth, h.ListSubscriptions)
		} else {
			subs.GET("", h.ListSubscriptions)
		}
		subs.GET("/taxonomy", h.GetTaxonomy)
	}
}

// SubscribeRequest represents the JSON payload to create a subscription.
type SubscribeRequest struct {
	ChannelName string `json:"channel_name" binding:"required" example:"telegram"`
	ChatID      string `json:"chat_id" binding:"required" example:"123456789"`
	TargetType  string `json:"target_type" binding:"required" example:"zone"`
	TargetID    string `json:"target_id" example:"Quận 7 - Nhà Bè"`
}

// Subscribe creates a new flood alert subscription.
// @Summary Create flood alert subscription
// @Description Subscribes a user chat ID on a channel (e.g. Telegram) to flood alerts (scope: all, chokepoint, zone, or ward).
// @Tags Subscriptions
// @Accept json
// @Produce json
// @Param request body SubscribeRequest true "Subscription details"
// @Success 201 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 429 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /api/subscriptions [post]
func (h *Handler) Subscribe(c *gin.Context) {
	var req SubscribeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "valid_target_types": CanonicalTargetTypes})
		return
	}

	rawTargetType := strings.TrimSpace(req.TargetType)
	sub := Subscription{
		ChannelName: strings.TrimSpace(req.ChannelName),
		ChatID:      strings.TrimSpace(req.ChatID),
		TargetType:  TargetType(rawTargetType),
		TargetID:    strings.TrimSpace(req.TargetID),
	}

	if err := sub.IsValid(); err != nil {
		resp := gin.H{
			"error":              err.Error(),
			"valid_target_types": CanonicalTargetTypes,
		}
		if sub.TargetType == TargetZone {
			resp["valid_zones"] = CanonicalZones
		}
		c.JSON(http.StatusBadRequest, resp)
		return
	}

	// Web users are private chats. Reject group ids and any non-digit value
	// before touching the database so a typo cannot create an unreachable row.
	if strings.EqualFold(sub.ChannelName, telegramChannel) && !chatIDPattern.MatchString(sub.ChatID) {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid chat_id: telegram web subscriptions require 5 to 20 digits",
		})
		return
	}

	// Validate target against live taxonomy catalog if provider is available
	if h.taxonomy != nil {
		switch sub.TargetType {
		case TargetZone:
			if !h.taxonomy.HasZone(sub.TargetID) {
				c.JSON(http.StatusBadRequest, gin.H{
					"error":       "invalid zone target_id",
					"valid_zones": h.taxonomy.Zones(),
				})
				return
			}
		case TargetWard:
			if !h.taxonomy.HasWard(sub.TargetID) {
				c.JSON(http.StatusBadRequest, gin.H{
					"error": "invalid ward target_id",
					"hint":  "consult GET /api/subscriptions/taxonomy or /api/phuong",
				})
				return
			}
		case TargetChokepoint:
			if !h.taxonomy.HasChokepoint(sub.TargetID) {
				c.JSON(http.StatusBadRequest, gin.H{
					"error": "invalid chokepoint target_id",
					"hint":  "consult GET /api/subscriptions/taxonomy or /api/cameras",
				})
				return
			}
		}
	}

	if err := h.store.Subscribe(c.Request.Context(), sub); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save subscription"})
		return
	}

	// Best-effort confirmation DM. A failure never changes the response: the
	// owner may still not own the chat id (accepted risk R1), but the message
	// tells the real owner what was registered and how to revoke it.
	h.sendConfirmation(sub)

	c.JSON(http.StatusCreated, gin.H{
		"status":       "subscribed",
		"subscription": sub,
	})
}

// sendConfirmation delivers the §2.5 confirmation DM with a hard 3 s budget.
func (h *Handler) sendConfirmation(sub Subscription) {
	if h.confirmationSend == nil || h.confirmation == nil {
		return
	}
	if !strings.EqualFold(sub.ChannelName, telegramChannel) {
		return
	}
	text := h.confirmation(sub)
	if strings.TrimSpace(text) == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), confirmationTimeout)
	defer cancel()
	if err := h.confirmationSend.SendMessage(ctx, sub.ChatID, text); err != nil {
		h.logger.Warn("subscription: confirmation DM failed",
			"chat", tailChatID(sub.ChatID), "target_type", string(sub.TargetType), "error", err)
	}
}

// DefaultConfirmationMessage is the bilingual DM sent when a subscription is
// created through the public web form. Zero emoji, HTML parse mode, /stop hint.
func DefaultConfirmationMessage(sub Subscription) string {
	vi, en := confirmationScope(sub)
	return fmt.Sprintf(
		"Bạn đã đăng ký nhận cảnh báo ngập cho phạm vi: <b>%s</b>.\n"+
			"Gửi /status để xem lại, gửi /stop để huỷ bất cứ lúc nào.\n\n"+
			"You subscribed to flood alerts for: <b>%s</b>.\n"+
			"Send /status to review, /stop to unsubscribe at any time.",
		html.EscapeString(vi), html.EscapeString(en),
	)
}

func confirmationScope(sub Subscription) (string, string) {
	target := strings.TrimSpace(sub.TargetID)
	switch sub.TargetType {
	case TargetAll:
		return "Toàn thành phố", "City-wide"
	case TargetZone:
		return "Khu vực: " + target, "Zone: " + target
	case TargetWard:
		return "Phường / Xã: " + target, "Ward / Commune: " + target
	case TargetChokepoint:
		return "Điểm ngập: " + target, "Chokepoint: " + target
	default:
		return target, target
	}
}

// Unsubscribe deletes an existing flood alert subscription.
// @Summary Delete flood alert subscription
// @Description Unsubscribes a user chat ID from a specific flood alert target.
// @Tags Subscriptions
// @Produce json
// @Param channel_name query string true "Channel name (e.g. telegram)"
// @Param chat_id query string true "Chat ID"
// @Param target_type query string true "Target type (all, chokepoint, zone, ward)"
// @Param target_id query string false "Target ID"
// @Success 200 {object} map[string]string
// @Failure 400 {object} map[string]interface{}
// @Failure 500 {object} map[string]string
// @Router /api/subscriptions [delete]
func (h *Handler) Unsubscribe(c *gin.Context) {
	channelName := strings.TrimSpace(c.Query("channel_name"))
	chatID := strings.TrimSpace(c.Query("chat_id"))
	targetType := TargetType(strings.TrimSpace(c.Query("target_type")))
	targetID := strings.TrimSpace(c.Query("target_id"))

	// Fallback to JSON request body if query params are not provided
	if channelName == "" && chatID == "" {
		var req struct {
			ChannelName string `json:"channel_name"`
			ChatID      string `json:"chat_id"`
			TargetType  string `json:"target_type"`
			TargetID    string `json:"target_id"`
		}
		if err := c.ShouldBindJSON(&req); err == nil {
			channelName = strings.TrimSpace(req.ChannelName)
			chatID = strings.TrimSpace(req.ChatID)
			targetType = TargetType(strings.TrimSpace(req.TargetType))
			targetID = strings.TrimSpace(req.TargetID)
		}
	}

	if channelName == "" || chatID == "" || targetType == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":              "channel_name, chat_id, and target_type are required",
			"valid_target_types": CanonicalTargetTypes,
		})
		return
	}

	// Normalize zone targetID if unregistering a zone
	if targetType == TargetZone && targetID != "" {
		if canonical, ok := NormalizeZone(targetID); ok {
			targetID = canonical
		}
	} else if targetType == TargetAll {
		targetID = ""
	}

	if err := h.store.Unsubscribe(c.Request.Context(), channelName, chatID, targetType, targetID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to unsubscribe"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "unsubscribed"})
}

// ListSubscriptions returns all active subscriptions for a given channel and chat ID.
// @Summary List flood alert subscriptions
// @Description Lists all active subscriptions for a given user chat ID on a channel.
// @Tags Subscriptions
// @Produce json
// @Param channel_name query string true "Channel name (e.g. telegram)"
// @Param chat_id query string true "Chat ID"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /api/subscriptions [get]
func (h *Handler) ListSubscriptions(c *gin.Context) {
	channelName := strings.TrimSpace(c.Query("channel_name"))
	chatID := strings.TrimSpace(c.Query("chat_id"))

	if channelName == "" || chatID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "channel_name and chat_id are required"})
		return
	}

	list, err := h.store.ListSubscriptions(c.Request.Context(), channelName, chatID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list subscriptions"})
		return
	}

	if list == nil {
		list = []Subscription{}
	}

	c.JSON(http.StatusOK, gin.H{
		"channel_name":  channelName,
		"chat_id":       chatID,
		"subscriptions": list,
	})
}

// GetTaxonomy returns the canonical subscription taxonomy (target types, zones, wards, chokepoints).
// @Summary Get subscription taxonomy
// @Description Retrieves all canonical targets available for subscription (operational zones, administrative wards, camera chokepoints, and target types) plus the public bot handle for deep links.
// @Tags Subscriptions
// @Produce json
// @Success 200 {object} TaxonomyResponse
// @Router /api/subscriptions/taxonomy [get]
func (h *Handler) GetTaxonomy(c *gin.Context) {
	resp := TaxonomyResponse{
		TargetTypes:     CanonicalTargetTypes,
		Zones:           CanonicalZones,
		Wards:           []WardInfo{},
		Chokepoints:     []ChokepointInfo{},
		BotUsername:     h.botUsername,
		DeepLinkEnabled: h.deepLinkEnabled,
	}

	if h.taxonomy != nil {
		if z := h.taxonomy.Zones(); len(z) > 0 {
			resp.Zones = z
		}
		if w := h.taxonomy.Wards(); len(w) > 0 {
			resp.Wards = w
		}
		if cp := h.taxonomy.Chokepoints(); len(cp) > 0 {
			resp.Chokepoints = cp
		}
	}

	c.JSON(http.StatusOK, resp)
}

// --- Per-IP limiter --------------------------------------------------------

type subscribeBucket struct {
	tokens float64
	last   time.Time
}

type subscribeLimiter struct {
	mu      sync.Mutex
	buckets map[string]*subscribeBucket
	rate    float64 // tokens per second
	burst   float64
	now     func() time.Time
}

// newSubscribeLimiterWithClock builds the dedicated POST /api/subscriptions
// limiter: 5 requests per minute per IP with a burst of 3. The clock is
// injectable so the table test does not sleep.
func newSubscribeLimiterWithClock(perMinute, burst int, now func() time.Time) gin.HandlerFunc {
	if perMinute <= 0 || burst <= 0 {
		return func(c *gin.Context) { c.Next() }
	}
	if now == nil {
		now = time.Now
	}
	l := &subscribeLimiter{
		buckets: make(map[string]*subscribeBucket),
		rate:    float64(perMinute) / 60,
		burst:   float64(burst),
		now:     now,
	}
	retryAfter := fmt.Sprintf("%d", int(math.Ceil(60/float64(perMinute))))

	return func(c *gin.Context) {
		ip := c.ClientIP()
		if ip == "" {
			ip = "unknown"
		}
		if !l.allow(ip) {
			c.Header("Retry-After", retryAfter)
			c.AbortWithStatus(http.StatusTooManyRequests)
			return
		}
		c.Next()
	}
}

func (l *subscribeLimiter) allow(ip string) bool {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()

	// Bound memory: a public endpoint can see many source addresses, so prune
	// idle buckets before the map grows without limit.
	if len(l.buckets) > 4096 {
		for key, bucket := range l.buckets {
			if now.Sub(bucket.last) > 10*time.Minute {
				delete(l.buckets, key)
			}
		}
	}

	bucket, ok := l.buckets[ip]
	if !ok {
		l.buckets[ip] = &subscribeBucket{tokens: l.burst - 1, last: now}
		return true
	}
	elapsed := now.Sub(bucket.last).Seconds()
	if elapsed > 0 {
		bucket.tokens = math.Min(l.burst, bucket.tokens+elapsed*l.rate)
		bucket.last = now
	}
	if bucket.tokens >= 1 {
		bucket.tokens--
		return true
	}
	return false
}

// tailChatID returns the last four characters of a chat id for logs.
func tailChatID(chatID string) string {
	if len(chatID) <= 4 {
		return chatID
	}
	return chatID[len(chatID)-4:]
}
