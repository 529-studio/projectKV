package subscription

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// recordingSender captures confirmation DMs without touching the Bot API.
type recordingSender struct {
	mu       sync.Mutex
	chats    []string
	texts    []string
	failWith error
}

func (s *recordingSender) SendMessage(_ context.Context, chatID, html string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.chats = append(s.chats, chatID)
	s.texts = append(s.texts, html)
	return s.failWith
}

func (s *recordingSender) messages() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.texts...)
}

func newS12Router(t *testing.T, handler *Handler, opts ...RouteOption) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api")
	handler.RegisterRoutes(api, opts...)
	return r
}

func postSubscription(t *testing.T, r *gin.Engine, body SubscribeRequest) *httptest.ResponseRecorder {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/subscriptions", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestGetTaxonomy_PublishesBotInfo(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	handler := NewHandler(NewSQLiteStore(db), nil, WithBotInfo("@WardedAlertBot", true))
	r := newS12Router(t, handler)

	req := httptest.NewRequest(http.MethodGet, "/api/subscriptions/taxonomy", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp TaxonomyResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode taxonomy: %v", err)
	}
	if resp.BotUsername != "WardedAlertBot" {
		t.Errorf("bot_username = %q, want WardedAlertBot", resp.BotUsername)
	}
	if !resp.DeepLinkEnabled {
		t.Error("deep_link_enabled = false, want true when provisioned")
	}

	// Unprovisioned handlers omit the handle and disable the CTA.
	handlerOff := NewHandler(NewSQLiteStore(db), nil)
	off := newS12Router(t, handlerOff)
	wOff := httptest.NewRecorder()
	off.ServeHTTP(wOff, httptest.NewRequest(http.MethodGet, "/api/subscriptions/taxonomy", nil))
	if strings.Contains(wOff.Body.String(), "bot_username") {
		t.Errorf("unprovisioned taxonomy leaked bot_username: %s", wOff.Body.String())
	}
	if strings.Contains(wOff.Body.String(), `"deep_link_enabled":true`) {
		t.Errorf("unprovisioned taxonomy enabled deep links: %s", wOff.Body.String())
	}
}

func TestSubscribe_RejectsNonPrivateChatIDs(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	r := newS12Router(t, NewHandler(NewSQLiteStore(db), nil, WithSubscribeLimiter(nil)))

	for _, chatID := range []string{"abc", "-1001234567890", "1234", strings.Repeat("1", 21)} {
		w := postSubscription(t, r, SubscribeRequest{
			ChannelName: "telegram",
			ChatID:      chatID,
			TargetType:  "all",
		})
		if w.Code != http.StatusBadRequest {
			t.Errorf("chat_id %q: status = %d, want 400", chatID, w.Code)
		}
	}

	w := postSubscription(t, r, SubscribeRequest{
		ChannelName: "telegram",
		ChatID:      "12345",
		TargetType:  "all",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("5-digit chat_id: status = %d, want 201: %s", w.Code, w.Body.String())
	}
}

func TestSubscribe_SendsConfirmationDM(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := NewSQLiteStore(db)
	sender := &recordingSender{}
	r := newS12Router(t, NewHandler(store, nil, WithConfirmationSender(sender)))

	w := postSubscription(t, r, SubscribeRequest{
		ChannelName: "telegram",
		ChatID:      "555000111",
		TargetType:  "all",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", w.Code, w.Body.String())
	}

	msgs := sender.messages()
	if len(msgs) != 1 {
		t.Fatalf("confirmation DMs = %d, want 1", len(msgs))
	}
	for _, want := range []string{"/stop", "Toàn thành phố", "City-wide"} {
		if !strings.Contains(msgs[0], want) {
			t.Errorf("confirmation missing %q:\n%s", want, msgs[0])
		}
	}

	// A failing Bot API must not change the response (best-effort DM).
	senderFail := &recordingSender{failWith: errors.New("telegram unavailable")}
	rFail := newS12Router(t, NewHandler(store, nil, WithConfirmationSender(senderFail)))
	wFail := postSubscription(t, rFail, SubscribeRequest{
		ChannelName: "telegram",
		ChatID:      "555000112",
		TargetType:  "all",
	})
	if wFail.Code != http.StatusCreated {
		t.Fatalf("DM failure changed status to %d, want 201", wFail.Code)
	}
}

func TestSubscribeLimiter_BurstThenRetryAfter(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	now := time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	limiter := newSubscribeLimiterWithClock(subscribeRatePerMinute, subscribeBurst, clock)
	r := newS12Router(t, NewHandler(NewSQLiteStore(db), nil, WithSubscribeLimiter(limiter)))

	request := func() *httptest.ResponseRecorder {
		body, _ := json.Marshal(SubscribeRequest{ChannelName: "telegram", ChatID: "666000111", TargetType: "all"})
		req := httptest.NewRequest(http.MethodPost, "/api/subscriptions", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "203.0.113.7:4242"
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	for i := 0; i < subscribeBurst; i++ {
		if w := request(); w.Code != http.StatusCreated {
			t.Fatalf("request %d: status = %d, want 201: %s", i+1, w.Code, w.Body.String())
		}
	}
	blocked := request()
	if blocked.Code != http.StatusTooManyRequests {
		t.Fatalf("burst+1 status = %d, want 429", blocked.Code)
	}
	if blocked.Header().Get("Retry-After") == "" {
		t.Error("429 must carry a Retry-After header")
	}

	// A minute later the bucket refills and the same IP is served again.
	now = now.Add(time.Minute)
	if w := request(); w.Code != http.StatusCreated {
		t.Fatalf("after refill status = %d, want 201: %s", w.Code, w.Body.String())
	}
}

func TestRegisterRoutes_AuditAuthGuardsOnlyTheListing(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	deny := func(c *gin.Context) { c.AbortWithStatus(http.StatusUnauthorized) }
	r := newS12Router(t, NewHandler(NewSQLiteStore(db), nil), WithAuditAuth(deny))

	req := httptest.NewRequest(http.MethodGet, "/api/subscriptions?channel_name=telegram&chat_id=12345678", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("GET listing status = %d, want 401", w.Code)
	}

	// The citizen-facing taxonomy stays public.
	wTax := httptest.NewRecorder()
	r.ServeHTTP(wTax, httptest.NewRequest(http.MethodGet, "/api/subscriptions/taxonomy", nil))
	if wTax.Code != http.StatusOK {
		t.Fatalf("taxonomy status = %d, want 200", wTax.Code)
	}
}
