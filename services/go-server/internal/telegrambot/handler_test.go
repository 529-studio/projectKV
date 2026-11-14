package telegrambot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

var testWebhookSecret = "unit-test-secret-" + strings.Repeat("a", 32)

// --- Fakes -----------------------------------------------------------------

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

type fakeStore struct {
	mu sync.Mutex

	rows     map[string][]Record
	channels map[string]string

	subscribeCalls   int
	unsubscribeCalls int
	listCalls        int
	subscribeErr     error
	unsubscribeErr   error
	listErr          error
}

func newFakeStore() *fakeStore {
	return &fakeStore{rows: make(map[string][]Record), channels: make(map[string]string)}
}

func (s *fakeStore) Subscribe(_ context.Context, channelName, chatID string, rec Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subscribeCalls++
	if s.subscribeErr != nil {
		return s.subscribeErr
	}
	s.channels[chatID] = channelName
	for _, existing := range s.rows[chatID] {
		if existing.TargetType == rec.TargetType && existing.TargetID == rec.TargetID {
			return nil
		}
	}
	s.rows[chatID] = append(s.rows[chatID], rec)
	return nil
}

func (s *fakeStore) UnsubscribeAll(_ context.Context, channelName, chatID string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.unsubscribeCalls++
	if s.unsubscribeErr != nil {
		return 0, s.unsubscribeErr
	}
	existing := s.rows[chatID]
	delete(s.rows, chatID)
	return int64(len(existing)), nil
}

func (s *fakeStore) List(_ context.Context, channelName, chatID string) ([]Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listCalls++
	if s.listErr != nil {
		return nil, s.listErr
	}
	out := make([]Record, len(s.rows[chatID]))
	copy(out, s.rows[chatID])
	return out, nil
}

func (s *fakeStore) rowsFor(chatID string) []Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Record, len(s.rows[chatID]))
	copy(out, s.rows[chatID])
	return out
}

func (s *fakeStore) channelFor(chatID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.channels[chatID]
}

func (s *fakeStore) counts() (subscribe, unsubscribe, list int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.subscribeCalls, s.unsubscribeCalls, s.listCalls
}

type sentMessage struct {
	chatID string
	html   string
}

type fakeSender struct {
	mu          sync.Mutex
	messages    []sentMessage
	err         error
	gate        <-chan struct{}
	enteredGate chan struct{}
}

func (s *fakeSender) SendMessage(ctx context.Context, chatID, html string) error {
	s.mu.Lock()
	gate := s.gate
	entered := s.enteredGate
	s.mu.Unlock()

	if entered != nil {
		select {
		case entered <- struct{}{}:
		default:
		}
	}

	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	s.messages = append(s.messages, sentMessage{chatID: chatID, html: html})
	return nil
}

func (s *fakeSender) all() []sentMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]sentMessage, len(s.messages))
	copy(out, s.messages)
	return out
}

func (s *fakeSender) texts() []string {
	out := make([]string, 0, len(s.all()))
	for _, m := range s.all() {
		out = append(out, m.html)
	}
	return out
}

type fakeTaxonomy struct {
	zones   map[string]string // canonical name (lower) -> canonical name
	aliases map[string]string // legacy alias (lower) -> canonical
	wards   map[string]string // bare name (lower) -> bare name
	cams    map[string]string // id -> display label
}

func newFakeTaxonomy() *fakeTaxonomy {
	return &fakeTaxonomy{
		zones: map[string]string{
			"quận 7 - nhà bè":          "Quận 7 - Nhà Bè",
			"bình dương":               "Bình Dương",
			"bà rịa - vũng tàu":        "Bà Rịa - Vũng Tàu",
			"khu vực nội thành tp.hcm": "Khu vực Nội thành TP.HCM",
			"cụm cảng cát lái":         "Cụm Cảng Cát Lái",
		},
		aliases: map[string]string{
			"quận 7": "Quận 7 - Nhà Bè",
			"q7":     "Quận 7 - Nhà Bè",
		},
		wards: map[string]string{
			"bến nghé":        "Bến Nghé",
			"đông hưng thuận": "Đông Hưng Thuận",
		},
		cams: map[string]string{
			"591c6d3ba6bfb500119400ac": "Nguyễn Văn Linh - Nguyễn Hữu Thọ",
			"cam-vvk-01":               "Võ Văn Kiệt - Ký Con",
		},
	}
}

func (f *fakeTaxonomy) CanonicalZone(id string) (string, bool) {
	key := strings.ToLower(strings.TrimSpace(id))
	if canonical, ok := f.zones[key]; ok {
		return canonical, true
	}
	if canonical, ok := f.aliases[key]; ok {
		return canonical, true
	}
	return "", false
}

func (f *fakeTaxonomy) CanonicalWard(id string) (string, bool) {
	key := strings.ToLower(strings.TrimSpace(id))
	for _, prefix := range []string{"phường ", "xã ", "thị trấn "} {
		if strings.HasPrefix(key, prefix) {
			key = strings.TrimSpace(key[len(prefix):])
			break
		}
	}
	if canonical, ok := f.wards[key]; ok {
		return canonical, true
	}
	return "", false
}

func (f *fakeTaxonomy) CanonicalChokepoint(id string) (string, bool) {
	if _, ok := f.cams[id]; ok {
		return id, true
	}
	return "", false
}

func (f *fakeTaxonomy) Label(t Target) string {
	if t.Type == targetTypeChokepoint {
		if name, ok := f.cams[t.ID]; ok {
			return name
		}
	}
	return t.ID
}

// --- Fixture ---------------------------------------------------------------

type botFixture struct {
	h      *Handler
	store  *fakeStore
	sender *fakeSender
	tax    *fakeTaxonomy
	router *gin.Engine
	clock  *fakeClock
	logs   *bytes.Buffer
}

func newBotFixture(t *testing.T, mutate func(*Config)) *botFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)

	cfg := Config{
		BotUsername:   testBotUsername,
		WebhookSecret: testWebhookSecret,
		WebURL:        "https://warded.example.test",
		// One worker keeps reply ordering deterministic; the queue, dedupe and
		// limiter paths are unaffected. Concurrency has its own test below.
		Workers:          1,
		QueueSize:        32,
		MaxSubsPerChat:   20,
		PerChatPerMinute: 6,
	}
	if mutate != nil {
		mutate(&cfg)
	}

	store := newFakeStore()
	sender := &fakeSender{}
	tax := newFakeTaxonomy()
	logs := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	h := NewHandler(cfg, store, tax, sender, logger)

	clock := &fakeClock{now: time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)}
	h.now = clock.Now
	h.dedupe.now = clock.Now
	h.limiter.now = clock.Now
	h.helpNote.now = clock.Now

	router := gin.New()
	h.RegisterRoutes(router)

	f := &botFixture{h: h, store: store, sender: sender, tax: tax, router: router, clock: clock, logs: logs}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := h.Shutdown(ctx); err != nil {
			t.Errorf("Shutdown failed: %v", err)
		}
	})
	return f
}

func (f *botFixture) drain() {
	f.h.inflight.Wait()
}

func (f *botFixture) post(t *testing.T, body string, secret string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, WebhookPath, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if secret != "" {
		req.Header.Set(secretHeader, secret)
	}
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	return w
}

func (f *botFixture) postRaw(t *testing.T, body []byte, secret string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, WebhookPath, bytes.NewReader(body))
	if secret != "" {
		req.Header.Set(secretHeader, secret)
	}
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	return w
}

func (f *botFixture) postUpdate(t *testing.T, update map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(update)
	if err != nil {
		t.Fatalf("marshal update: %v", err)
	}
	return f.postRaw(t, body, testWebhookSecret)
}

func privateMessageUpdate(updateID, chatID int64, text string) map[string]any {
	return map[string]any{
		"update_id": updateID,
		"message": map[string]any{
			"message_id": updateID,
			"date":       1763000000,
			"text":       text,
			"from": map[string]any{
				"id":         chatID,
				"is_bot":     false,
				"username":   "citizen",
				"first_name": "Citizen",
			},
			"chat": map[string]any{
				"id":   chatID,
				"type": "private",
			},
		},
	}
}

func chatMessageUpdate(updateID, chatID int64, chatType, text string, isBot bool) map[string]any {
	return map[string]any{
		"update_id": updateID,
		"message": map[string]any{
			"message_id": updateID,
			"date":       1763000000,
			"text":       text,
			"from": map[string]any{
				"id":     chatID,
				"is_bot": isBot,
			},
			"chat": map[string]any{
				"id":   chatID,
				"type": chatType,
			},
		},
	}
}

func mustPayload(t *testing.T, target Target) string {
	t.Helper()
	payload, err := EncodePayload(target)
	if err != nil {
		t.Fatalf("EncodePayload(%+v): %v", target, err)
	}
	return payload
}

// --- Webhook authentication -------------------------------------------------

func TestWebhookSecretAuthentication(t *testing.T) {
	cases := []struct {
		name       string
		secret     string
		statusCode int
	}{
		{"missing header", "", http.StatusUnauthorized},
		{"secret with trailing junk", testWebhookSecret + "x", http.StatusUnauthorized},
		{"wrong secret, same length", strings.Repeat("b", len(testWebhookSecret)), http.StatusUnauthorized},
		{"wrong secret, short", "short", http.StatusUnauthorized},
		{"valid secret", testWebhookSecret, http.StatusOK},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newBotFixture(t, nil)
			w := f.post(t, `{"update_id":1}`, tc.secret)

			if w.Code != tc.statusCode {
				t.Fatalf("status = %d, want %d (body %q)", w.Code, tc.statusCode, w.Body.String())
			}
			if tc.statusCode == http.StatusUnauthorized {
				if w.Body.Len() != 0 {
					t.Fatalf("401 must have an empty body, got %q", w.Body.String())
				}
				if strings.Contains(w.Body.String(), testWebhookSecret) {
					t.Fatal("401 response leaked the configured secret")
				}
				if _, _, list := f.store.counts(); list != 0 {
					t.Fatal("a rejected request must not touch the database")
				}
			}
			if tc.statusCode == http.StatusOK && !strings.Contains(w.Body.String(), `"ok":true`) {
				t.Fatalf("accepted webhook must answer {\"ok\":true}, got %q", w.Body.String())
			}
		})
	}
}

func TestWebhookNotRegisteredWhenDisabled(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Config)
	}{
		{"missing secret", func(c *Config) { c.WebhookSecret = "" }},
		{"short secret", func(c *Config) { c.WebhookSecret = "too-short" }},
		{"invalid secret alphabet", func(c *Config) { c.WebhookSecret = strings.Repeat("!", 40) }},
		{"missing username", func(c *Config) { c.BotUsername = "" }},
		{"username too short", func(c *Config) { c.BotUsername = "ab" }},
		{"username with invalid chars", func(c *Config) { c.BotUsername = "bad-handle" }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newBotFixture(t, tc.mutate)
			if f.h.Enabled() {
				t.Fatal("Enabled() must be false for an unprovisioned bot")
			}
			w := f.post(t, `{"update_id":1}`, testWebhookSecret)
			if w.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404 when the route is not registered", w.Code)
			}
		})
	}

	// A valid configuration registers exactly once and is idempotent to check.
	f := newBotFixture(t, nil)
	if !f.h.Enabled() {
		t.Fatal("Enabled() must be true for a complete configuration")
	}
}

func TestWebhookMalformedJSON(t *testing.T) {
	cases := []struct {
		name string
		body string
		want int
	}{
		{"not json", `not-json`, http.StatusBadRequest},
		{"truncated", `{"update_id": 1, "message":`, http.StatusBadRequest},
		{"wrong type", `{"update_id": "abc"}`, http.StatusBadRequest},
		{"empty body", ``, http.StatusBadRequest},
		{"valid empty object", `{}`, http.StatusOK},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newBotFixture(t, nil)
			w := f.post(t, tc.body, testWebhookSecret)
			if w.Code != tc.want {
				t.Fatalf("status = %d, want %d (body %q)", w.Code, tc.want, w.Body.String())
			}
		})
	}
}

func TestWebhookRejectsOversizeBody(t *testing.T) {
	f := newBotFixture(t, nil)
	body := []byte(`{"update_id":1,"message":{"text":"` + strings.Repeat("x", 70*1024) + `"}}`)
	w := f.postRaw(t, body, testWebhookSecret)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413 for a body above 64 KiB", w.Code)
	}
}

// --- Dedupe ----------------------------------------------------------------

func TestWebhookDeduplicatesUpdateID(t *testing.T) {
	f := newBotFixture(t, nil)
	update := privateMessageUpdate(42, 987654321, "/start sub_all")
	body, err := json.Marshal(update)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	first := f.postRaw(t, body, testWebhookSecret)
	second := f.postRaw(t, body, testWebhookSecret)
	if first.Code != http.StatusOK || second.Code != http.StatusOK {
		t.Fatalf("both deliveries must answer 200, got %d and %d", first.Code, second.Code)
	}
	f.drain()

	if subscribe, _, _ := f.store.counts(); subscribe != 1 {
		t.Fatalf("replayed update_id must persist once, got %d Subscribe calls", subscribe)
	}
	if got := len(f.sender.all()); got != 1 {
		t.Fatalf("replayed update_id must reply once, got %d replies", got)
	}
	if rows := f.store.rowsFor("987654321"); len(rows) != 1 {
		t.Fatalf("expected exactly one row, got %v", rows)
	}

	// A different update_id is processed normally.
	next := privateMessageUpdate(43, 987654321, "/status")
	if w := f.postUpdate(t, next); w.Code != http.StatusOK {
		t.Fatalf("new update_id status = %d, want 200", w.Code)
	}
	f.drain()
	if got := len(f.sender.all()); got != 2 {
		t.Fatalf("a new update must produce a reply, got %d replies", got)
	}
}

func TestDedupeEvictsBeyondCapacity(t *testing.T) {
	d := &updateDedupe{ttl: dedupeTTL, capacity: 2, now: time.Now, entries: make(map[int64]time.Time)}
	d.entries[1] = time.Now().Add(-time.Hour)
	d.entries[2] = time.Now().Add(-2 * time.Hour)
	d.remember(3)
	if len(d.entries) != 2 {
		t.Fatalf("capacity must be enforced, got %d entries", len(d.entries))
	}
	if _, ok := d.entries[2]; ok {
		t.Fatal("the oldest entry must be evicted first")
	}
	// Expired entries are pruned by seen().
	d.now = func() time.Time { return time.Now().Add(dedupeTTL + time.Minute) }
	d.seen(1)
	if len(d.entries) != 0 {
		t.Fatalf("expired entries must be pruned, got %d", len(d.entries))
	}
}

// --- Chat filtering --------------------------------------------------------

func TestNonPrivateChatNeverPersists(t *testing.T) {
	cases := []struct {
		chatType string
		chatID   int64
	}{
		{"group", -1001234567890},
		{"supergroup", -1009876543210},
		{"channel", -1005555555555},
	}

	for _, tc := range cases {
		t.Run(tc.chatType, func(t *testing.T) {
			f := newBotFixture(t, nil)
			w := f.postUpdate(t, chatMessageUpdate(7, tc.chatID, tc.chatType, "/start sub_all", false))
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", w.Code)
			}
			f.drain()

			if subscribe, _, _ := f.store.counts(); subscribe != 0 {
				t.Fatalf("non-private chat must not persist, got %d Subscribe calls", subscribe)
			}
			replies := f.sender.all()
			if len(replies) != 1 {
				t.Fatalf("expected exactly one guidance reply, got %d", len(replies))
			}
			if !strings.Contains(replies[0].html, "cuộc trò chuyện riêng") ||
				!strings.Contains(replies[0].html, "private chats") {
				t.Fatalf("guidance reply must explain private-chat-only usage:\n%s", replies[0].html)
			}
		})
	}
}

func TestBotAndEditedMessagesAreIgnored(t *testing.T) {
	f := newBotFixture(t, nil)

	// from.is_bot = true
	f.postUpdate(t, chatMessageUpdate(1, 111111111, "private", "/start sub_all", true))

	// edited_message only: no `message` field at all.
	edited := privateMessageUpdate(2, 222222222, "/start sub_all")
	payload := edited["message"].(map[string]any)
	f.postUpdate(t, map[string]any{
		"update_id":      2,
		"edited_message": payload,
	})

	// channel_post only.
	f.postUpdate(t, map[string]any{
		"update_id":    3,
		"channel_post": payload,
	})

	// callback_query only.
	f.postUpdate(t, map[string]any{
		"update_id":      4,
		"callback_query": map[string]any{"id": "abc", "data": "sub_all"},
	})

	f.drain()

	if subscribe, _, _ := f.store.counts(); subscribe != 0 {
		t.Fatalf("ignored updates must not persist, got %d Subscribe calls", subscribe)
	}
	if got := len(f.sender.all()); got != 0 {
		t.Fatalf("ignored updates must not reply, got %d replies", got)
	}
}

// --- Rate limiting ---------------------------------------------------------

func TestPerChatRateLimit(t *testing.T) {
	f := newBotFixture(t, nil)
	const chatID = 555666777

	for i := int64(1); i <= 6; i++ {
		f.postUpdate(t, privateMessageUpdate(i, chatID, "/status"))
	}
	f.drain()
	if got := len(f.sender.all()); got != 6 {
		t.Fatalf("first six commands must be answered, got %d", got)
	}

	// The seventh is refused with exactly one "slow down" reply.
	f.postUpdate(t, privateMessageUpdate(7, chatID, "/status"))
	f.drain()
	replies := f.sender.texts()
	if len(replies) != 7 {
		t.Fatalf("seventh command must get one slow-down reply, got %d replies", len(replies))
	}
	if !strings.Contains(replies[6], "quá nhanh") || !strings.Contains(replies[6], "too quickly") {
		t.Fatalf("seventh reply must be the slow-down copy:\n%s", replies[6])
	}

	// The eighth stays silent for the rest of the window.
	f.postUpdate(t, privateMessageUpdate(8, chatID, "/status"))
	f.drain()
	if got := len(f.sender.all()); got != 7 {
		t.Fatalf("later commands in the window must be silent, got %d replies", got)
	}

	// Another chat has its own bucket.
	f.postUpdate(t, privateMessageUpdate(9, 999888777, "/status"))
	f.drain()
	if got := len(f.sender.all()); got != 8 {
		t.Fatalf("a different chat must not be limited, got %d replies", got)
	}

	// After the window the chat is served again.
	f.clock.Advance(61 * time.Second)
	f.postUpdate(t, privateMessageUpdate(10, chatID, "/status"))
	f.drain()
	if got := len(f.sender.all()); got != 9 {
		t.Fatalf("chat must recover after one minute, got %d replies", got)
	}
}

func TestPlainTextGuidanceIsThrottledPerMinute(t *testing.T) {
	f := newBotFixture(t, nil)
	const chatID = 121212121

	f.postUpdate(t, privateMessageUpdate(1, chatID, "hello bot"))
	f.postUpdate(t, privateMessageUpdate(2, chatID, "hello again"))
	f.drain()

	replies := f.sender.texts()
	if len(replies) != 1 {
		t.Fatalf("plain text must produce at most one guidance reply per minute, got %d", len(replies))
	}
	if !strings.Contains(replies[0], "Warded Alert - Trợ giúp") {
		t.Fatalf("plain text must answer with the /help body:\n%s", replies[0])
	}

	f.clock.Advance(61 * time.Second)
	f.postUpdate(t, privateMessageUpdate(3, chatID, "hello a minute later"))
	f.drain()
	if got := len(f.sender.all()); got != 2 {
		t.Fatalf("guidance must resume after one minute, got %d replies", got)
	}
}

// --- Command execution -----------------------------------------------------

func TestStartCommandPersistsCanonicalTarget(t *testing.T) {
	cases := []struct {
		name     string
		target   Target
		wantType string
		wantID   string
	}{
		{"all", Target{Type: targetTypeAll}, targetTypeAll, ""},
		{"zone", Target{Type: targetTypeZone, ID: "Bình Dương"}, targetTypeZone, "Bình Dương"},
		{"ward", Target{Type: targetTypeWard, ID: "Bến Nghé"}, targetTypeWard, "Bến Nghé"},
		{"chokepoint", Target{Type: targetTypeChokepoint, ID: "591c6d3ba6bfb500119400ac"},
			targetTypeChokepoint, "591c6d3ba6bfb500119400ac"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newBotFixture(t, nil)
			const chatID = 303030303
			payload := mustPayload(t, tc.target)

			w := f.postUpdate(t, privateMessageUpdate(1, chatID, "/start "+payload))
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", w.Code)
			}
			f.drain()

			rows := f.store.rowsFor("303030303")
			if len(rows) != 1 {
				t.Fatalf("expected one row, got %v", rows)
			}
			if rows[0].TargetType != tc.wantType || rows[0].TargetID != tc.wantID {
				t.Fatalf("stored %+v, want type=%s id=%q", rows[0], tc.wantType, tc.wantID)
			}
			if channel := f.store.channelFor("303030303"); channel != SubscriptionChannel {
				t.Fatalf("stored on channel %q, want %q", channel, SubscriptionChannel)
			}

			replies := f.sender.texts()
			if len(replies) != 1 {
				t.Fatalf("expected one confirmation reply, got %d", len(replies))
			}
			if !strings.Contains(replies[0], "Đã đăng ký thành công.") ||
				!strings.Contains(replies[0], "Subscription confirmed.") {
				t.Fatalf("unexpected confirmation copy:\n%s", replies[0])
			}
			if tc.wantType == targetTypeAll {
				if !strings.Contains(replies[0], "Toàn thành phố") || !strings.Contains(replies[0], "City-wide") {
					t.Fatalf("all-scope confirmation must name the city-wide scope:\n%s", replies[0])
				}
			}
		})
	}
}

func TestStartCommandNormalizesLegacyZoneAlias(t *testing.T) {
	f := newBotFixture(t, nil)
	const chatID = 404040404

	// Legacy alias inside a valid slug: "q7" -> "Quận 7 - Nhà Bè" (spec E3).
	payload := mustPayload(t, Target{Type: targetTypeZone, ID: "q7"})
	f.postUpdate(t, privateMessageUpdate(1, chatID, "/start "+payload))
	f.drain()

	rows := f.store.rowsFor("404040404")
	if len(rows) != 1 || rows[0].TargetID != "Quận 7 - Nhà Bè" {
		t.Fatalf("alias must be stored canonically, got %v", rows)
	}
	replies := f.sender.texts()
	if len(replies) != 1 || !strings.Contains(replies[0], "Quận 7 - Nhà Bè") {
		t.Fatalf("confirmation must print the canonical zone, got %v", replies)
	}
}

func TestStartCommandWardPrefixToleratedAndStoredBare(t *testing.T) {
	f := newBotFixture(t, nil)
	const chatID = 505050505

	prefixed := mustPayload(t, Target{Type: targetTypeWard, ID: "Phường Bến Nghé"})
	f.postUpdate(t, privateMessageUpdate(1, chatID, "/start "+prefixed))
	f.drain()

	rows := f.store.rowsFor("505050505")
	if len(rows) != 1 || rows[0].TargetID != "Bến Nghé" {
		t.Fatalf("prefixed ward must be stored bare, got %v", rows)
	}

	// The bare form is the same subscription: no new row, "already active".
	bare := mustPayload(t, Target{Type: targetTypeWard, ID: "Bến Nghé"})
	f.postUpdate(t, privateMessageUpdate(2, chatID, "/start "+bare))
	f.drain()

	if rows := f.store.rowsFor("505050505"); len(rows) != 1 {
		t.Fatalf("duplicate target must not create a second row, got %v", rows)
	}
	if subscribe, _, _ := f.store.counts(); subscribe != 1 {
		t.Fatalf("duplicate target must not call Subscribe again, got %d calls", subscribe)
	}
	replies := f.sender.texts()
	if len(replies) != 2 {
		t.Fatalf("expected 2 replies, got %d", len(replies))
	}
	if !strings.Contains(replies[1], "đã được đăng ký") || !strings.Contains(replies[1], "already active") {
		t.Fatalf("second reply must be the already-active copy:\n%s", replies[1])
	}
}

func TestStartCommandUnknownTargetWritesNothing(t *testing.T) {
	cases := []struct {
		name string
		arg  string
	}{
		{"unknown camera (E5)", "sub_cam_does-not-exist"},
		{"unknown zone", "sub_zone_" + SlugEncode("Sao Hỏa")},
		{"unknown ward", "sub_ward_" + SlugEncode("Phường Không Có")},
		{"plain-text target (E2)", "sub_zone_quận 7"},
		{"unknown target type", "sub_district_abc"},
		{"missing payload prefix", "zone_QuG6v24gTmdow6k"},
		{"empty start argument after trimming", "   "},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newBotFixture(t, nil)
			const chatID = 606060606
			f.postUpdate(t, privateMessageUpdate(1, chatID, "/start "+tc.arg))
			f.drain()

			if subscribe, _, _ := f.store.counts(); subscribe != 0 {
				t.Fatalf("unknown target must not persist, got %d Subscribe calls", subscribe)
			}
			replies := f.sender.texts()
			if len(replies) != 1 {
				t.Fatalf("expected one reply, got %d", len(replies))
			}
			if tc.arg == "" || strings.TrimSpace(tc.arg) == "" {
				if !strings.Contains(replies[0], "Chào mừng bạn") {
					t.Fatalf("bare /start must answer with the welcome copy:\n%s", replies[0])
				}
				return
			}
			if !strings.Contains(replies[0], "Không tìm thấy phạm vi") ||
				!strings.Contains(replies[0], "Target not found") {
				t.Fatalf("unknown target must answer with guidance:\n%s", replies[0])
			}
			if !strings.Contains(replies[0], "/help") {
				t.Fatalf("guidance must point at /help:\n%s", replies[0])
			}
		})
	}
}

func TestStartWelcomeReportsActiveCount(t *testing.T) {
	f := newBotFixture(t, nil)
	const chatID = 707070707

	f.postUpdate(t, privateMessageUpdate(1, chatID, "/start "+mustPayload(t, Target{Type: targetTypeAll})))
	f.postUpdate(t, privateMessageUpdate(2, chatID,
		"/start "+mustPayload(t, Target{Type: targetTypeZone, ID: "Bình Dương"})))
	f.postUpdate(t, privateMessageUpdate(3, chatID, "/start"))
	f.drain()

	replies := f.sender.texts()
	last := replies[len(replies)-1]
	if !strings.Contains(last, "Số đăng ký đang hoạt động: 2.") ||
		!strings.Contains(last, "Active subscriptions: 2.") {
		t.Fatalf("welcome must report two active subscriptions:\n%s", last)
	}
}

func TestSubscribeLimitPerChat(t *testing.T) {
	f := newBotFixture(t, func(c *Config) { c.MaxSubsPerChat = 2 })
	const chatID = 808080808

	f.postUpdate(t, privateMessageUpdate(1, chatID, "/start sub_all"))
	f.postUpdate(t, privateMessageUpdate(2, chatID,
		"/start "+mustPayload(t, Target{Type: targetTypeZone, ID: "Bình Dương"})))
	f.postUpdate(t, privateMessageUpdate(3, chatID,
		"/start "+mustPayload(t, Target{Type: targetTypeWard, ID: "Bến Nghé"})))
	f.drain()

	rows := f.store.rowsFor("808080808")
	if len(rows) != 2 {
		t.Fatalf("limit of 2 must be enforced, got %v", rows)
	}
	replies := f.sender.texts()
	last := replies[len(replies)-1]
	if !strings.Contains(last, "giới hạn 2 đăng ký") || !strings.Contains(last, "limit of 2 subscriptions") {
		t.Fatalf("third subscription must be refused with the limit copy:\n%s", last)
	}
	if !strings.Contains(last, "/stop") {
		t.Fatalf("limit copy must point at /stop:\n%s", last)
	}
}

func TestStatusAndStopFlow(t *testing.T) {
	f := newBotFixture(t, nil)
	const chatID = 909090909

	f.postUpdate(t, privateMessageUpdate(1, chatID, "/start "+mustPayload(t, Target{Type: targetTypeZone, ID: "Bình Dương"})))
	f.postUpdate(t, privateMessageUpdate(2, chatID,
		"/start "+mustPayload(t, Target{Type: targetTypeChokepoint, ID: "cam-vvk-01"})))
	f.postUpdate(t, privateMessageUpdate(3, chatID, "/status"))
	f.drain()

	replies := f.sender.texts()
	status := replies[len(replies)-1]
	if !strings.Contains(status, "Đăng ký đang hoạt động (2):") ||
		!strings.Contains(status, "Active subscriptions (2):") {
		t.Fatalf("status must list two subscriptions:\n%s", status)
	}
	if !strings.Contains(status, "Khu vực: Bình Dương") || !strings.Contains(status, "Zone: Bình Dương") {
		t.Fatalf("status must name the zone:\n%s", status)
	}
	if !strings.Contains(status, "Võ Văn Kiệt - Ký Con") {
		t.Fatalf("status must label the chokepoint through the taxonomy:\n%s", status)
	}

	// /stop@BotUsername removes everything and reports the count.
	f.postUpdate(t, privateMessageUpdate(4, chatID, "/stop@"+testBotUsername))
	f.drain()
	stopped := f.sender.texts()
	last := stopped[len(stopped)-1]
	if !strings.Contains(last, "Đã huỷ 2 đăng ký.") || !strings.Contains(last, "Removed 2 subscriptions.") {
		t.Fatalf("/stop must report the removed count:\n%s", last)
	}
	if rows := f.store.rowsFor("909090909"); len(rows) != 0 {
		t.Fatalf("/stop must remove every row, got %v", rows)
	}

	// /stop again: zero is not an error (E7).
	f.postUpdate(t, privateMessageUpdate(5, chatID, "/unsubscribe"))
	f.drain()
	again := f.sender.texts()
	if !strings.Contains(again[len(again)-1], "Đã huỷ 0 đăng ký.") {
		t.Fatalf("/unsubscribe with no rows must report zero:\n%s", again[len(again)-1])
	}

	// /status reports none.
	f.postUpdate(t, privateMessageUpdate(6, chatID, "/status@"+testBotUsername))
	f.drain()
	none := f.sender.texts()
	if !strings.Contains(none[len(none)-1], "Bạn chưa có đăng ký nào.") {
		t.Fatalf("/status must report an empty list:\n%s", none[len(none)-1])
	}
}

func TestHelpCommand(t *testing.T) {
	f := newBotFixture(t, nil)
	f.postUpdate(t, privateMessageUpdate(1, 121314151, "/help"))
	f.drain()

	replies := f.sender.texts()
	if len(replies) != 1 {
		t.Fatalf("expected one reply, got %d", len(replies))
	}
	for _, want := range []string{"/status", "/stop", "/unsubscribe", "https://warded.example.test"} {
		if !strings.Contains(replies[0], want) {
			t.Fatalf("/help missing %q:\n%s", want, replies[0])
		}
	}
}

func TestCommandAddressedToAnotherBotIsIgnored(t *testing.T) {
	f := newBotFixture(t, nil)
	const chatID = 161616161

	f.postUpdate(t, privateMessageUpdate(1, chatID, "/start@SomeOtherBot sub_all"))
	f.drain()

	if subscribe, _, _ := f.store.counts(); subscribe != 0 {
		t.Fatalf("a command for another bot must not persist, got %d Subscribe calls", subscribe)
	}
}

// TestStoreFailuresOnEveryCommand keeps the bot honest when SQLite is unhappy:
// every command answers with the delivery-failure copy, never a panic and never
// a stack trace in the chat.
func TestStoreFailuresOnEveryCommand(t *testing.T) {
	cases := []struct {
		name   string
		text   string
		inject func(*fakeStore)
	}{
		{"welcome list failure", "/start", func(s *fakeStore) { s.listErr = fmt.Errorf("db closed") }},
		{"status list failure", "/status", func(s *fakeStore) { s.listErr = fmt.Errorf("db closed") }},
		{"start target list failure", "/start sub_all", func(s *fakeStore) { s.listErr = fmt.Errorf("db closed") }},
		{"stop failure", "/stop", func(s *fakeStore) { s.unsubscribeErr = fmt.Errorf("disk full") }},
		{"unsubscribe failure", "/unsubscribe", func(s *fakeStore) { s.unsubscribeErr = fmt.Errorf("disk full") }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newBotFixture(t, nil)
			tc.inject(f.store)
			f.postUpdate(t, privateMessageUpdate(1, 131313131, tc.text))
			f.drain()

			replies := f.sender.texts()
			if len(replies) != 1 {
				t.Fatalf("expected one reply, got %d", len(replies))
			}
			if !strings.Contains(replies[0], "Không xử lý được lệnh này.") ||
				!strings.Contains(replies[0], "could not be processed") {
				t.Fatalf("failure reply must be the delivery-failure copy:\n%s", replies[0])
			}
			if strings.Contains(replies[0], "disk full") || strings.Contains(replies[0], "db closed") {
				t.Fatalf("failure reply must not leak internal detail:\n%s", replies[0])
			}
		})
	}
}

// TestSenderFailureIsLoggedNotFatal covers a blocked bot (Telegram 403) or a
// transient Bot API error: the update is still acknowledged and the row is kept
// (spec E10).
func TestSenderFailureIsLoggedNotFatal(t *testing.T) {
	f := newBotFixture(t, nil)
	f.sender.err = fmt.Errorf("telegram api error (status 403): bot was blocked by the user")

	w := f.postUpdate(t, privateMessageUpdate(1, 141414141, "/start sub_all"))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 even when the reply fails", w.Code)
	}
	f.drain()

	if subscribe, _, _ := f.store.counts(); subscribe != 1 {
		t.Fatalf("a failed reply must not roll back the subscription, got %d Subscribe calls", subscribe)
	}
	if rows := f.store.rowsFor("141414141"); len(rows) != 1 {
		t.Fatalf("expected the row to be kept, got %v", rows)
	}
	if !strings.Contains(f.logs.String(), "telegram reply failed") {
		t.Fatalf("a failed reply must be logged:\n%s", f.logs.String())
	}
}

func TestStoreFailureIsReportedWithoutLeaking(t *testing.T) {
	f := newBotFixture(t, nil)
	f.store.subscribeErr = fmt.Errorf("database is locked")
	const chatID = 171717171

	f.postUpdate(t, privateMessageUpdate(1, chatID, "/start sub_all"))
	f.drain()

	replies := f.sender.texts()
	if len(replies) != 1 || !strings.Contains(replies[0], "Vui lòng thử lại sau.") {
		t.Fatalf("a store failure must answer with the delivery-failure copy, got %v", replies)
	}
	if rows := f.store.rowsFor("171717171"); len(rows) != 0 {
		t.Fatalf("failed subscribe must not leave a row, got %v", rows)
	}
}

// --- Queue backpressure ----------------------------------------------------

func TestQueueFullReturns503WithRetryAfter(t *testing.T) {
	gate := make(chan struct{})
	enteredGate := make(chan struct{}, 1)
	released := false
	release := func() {
		if !released {
			released = true
			close(gate)
		}
	}
	defer release()

	f := newBotFixture(t, func(c *Config) {
		c.Workers = 1
		c.QueueSize = 1
	})
	f.sender.mu.Lock()
	f.sender.gate = gate
	f.sender.enteredGate = enteredGate
	f.sender.mu.Unlock()

	// 1. Post first update and wait until the single worker picks it up and blocks on gate.
	if w := f.postUpdate(t, privateMessageUpdate(1, 202020202, "/status")); w.Code != http.StatusOK {
		t.Fatalf("first update must be accepted, got %d", w.Code)
	}
	select {
	case <-enteredGate:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not pick up first job in time")
	}

	// 2. Worker is now busy with job 1. Post second update to fill the queue buffer (QueueSize = 1).
	if w := f.postUpdate(t, privateMessageUpdate(2, 202020202, "/status")); w.Code != http.StatusOK {
		t.Fatalf("second update must be queued, got %d", w.Code)
	}

	// 3. Queue is now completely full (1 worker busy + 1 in queue buffer).
	// Third update must be rejected with 503 Service Unavailable.
	w := f.postUpdate(t, privateMessageUpdate(3, 202020202, "/status"))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 when the queue is full", w.Code)
	}
	if got := w.Header().Get("Retry-After"); got != "5" {
		t.Fatalf("Retry-After = %q, want \"5\"", got)
	}

	release()
	f.drain()

	// The rejected update_id was not marked seen, so a retry is accepted.
	if w := f.postUpdate(t, privateMessageUpdate(3, 202020202, "/status")); w.Code != http.StatusOK {
		t.Fatalf("retry after 503 must be accepted, got %d", w.Code)
	}
	f.drain()
}

// --- Logging hygiene -------------------------------------------------------

func TestLogsNeverContainSecretsChatIDsOrMessageText(t *testing.T) {
	f := newBotFixture(t, nil)
	const (
		chatID     = 1987654321
		secretText = "super-secret-message-body"
	)

	f.postUpdate(t, privateMessageUpdate(1, chatID, "/start sub_all"))
	f.postUpdate(t, privateMessageUpdate(2, chatID, secretText))
	f.drain()

	logged := f.logs.String()
	if logged == "" {
		t.Fatal("expected structured logs, got none")
	}
	for _, forbidden := range []string{testWebhookSecret, secretText, "1987654321"} {
		if strings.Contains(logged, forbidden) {
			t.Fatalf("logs leaked %q:\n%s", forbidden, logged)
		}
	}
	if !strings.Contains(logged, "4321") {
		t.Fatalf("logs should include the last four chat digits:\n%s", logged)
	}
	if strings.Contains(logged, "eyJ") {
		t.Fatal("logs must not contain token-like material")
	}
}

// TestConcurrentWorkersProcessDistinctChats exercises the pool with several
// workers: every chat gets exactly one reply and the store stays consistent.
func TestConcurrentWorkersProcessDistinctChats(t *testing.T) {
	f := newBotFixture(t, func(c *Config) {
		c.Workers = 4
		c.QueueSize = 64
	})

	const chats = 24
	for i := 0; i < chats; i++ {
		chatID := int64(300000000 + i)
		f.postUpdate(t, privateMessageUpdate(int64(i+1), chatID, "/start sub_all"))
	}
	f.drain()

	replies := f.sender.all()
	if len(replies) != chats {
		t.Fatalf("expected %d replies, got %d", chats, len(replies))
	}
	seen := make(map[string]bool, chats)
	for _, r := range replies {
		seen[r.chatID] = true
	}
	if len(seen) != chats {
		t.Fatalf("expected one reply per chat, got %d distinct chats", len(seen))
	}
	if subscribe, _, _ := f.store.counts(); subscribe != chats {
		t.Fatalf("expected %d Subscribe calls, got %d", chats, subscribe)
	}
}

// TestWebhookAfterShutdownReturns503 makes sure a drained handler cannot accept
// work it will never process.
func TestWebhookAfterShutdownReturns503(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewHandler(Config{BotUsername: testBotUsername, WebhookSecret: testWebhookSecret, Workers: 1},
		newFakeStore(), newFakeTaxonomy(), &fakeSender{},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	r := gin.New()
	h.RegisterRoutes(r)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := h.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	body, _ := json.Marshal(privateMessageUpdate(1, 123456789, "/status"))
	req := httptest.NewRequest(http.MethodPost, WebhookPath, bytes.NewReader(body))
	req.Header.Set(secretHeader, testWebhookSecret)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 after shutdown", w.Code)
	}
	if got := w.Header().Get("Retry-After"); got != "5" {
		t.Fatalf("Retry-After = %q, want \"5\"", got)
	}
}

// --- Shutdown --------------------------------------------------------------

func TestShutdownIsIdempotent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewHandler(Config{BotUsername: testBotUsername, WebhookSecret: testWebhookSecret},
		newFakeStore(), newFakeTaxonomy(), &fakeSender{},
		slog.New(slog.NewTextHandler(io.Discard, nil)))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := h.Shutdown(ctx); err != nil {
		t.Fatalf("first Shutdown: %v", err)
	}
	if err := h.Shutdown(ctx); err != nil {
		t.Fatalf("second Shutdown must be a no-op: %v", err)
	}
}

// TestConfigDefaults pins the normative defaults of spec §4.1.
func TestConfigDefaults(t *testing.T) {
	cfg := Config{BotUsername: testBotUsername, WebhookSecret: testWebhookSecret}.normalized()
	if cfg.Workers != 4 || cfg.QueueSize != 256 || cfg.MaxSubsPerChat != 20 || cfg.PerChatPerMinute != 6 {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if cfg.WebURL != defaultWebURL {
		t.Fatalf("WebURL default = %q, want %q", cfg.WebURL, defaultWebURL)
	}
	if got := (Config{BotUsername: "@WardedAlertBot"}).normalized().BotUsername; got != "WardedAlertBot" {
		t.Fatalf("leading @ must be stripped, got %q", got)
	}
}
