package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/529-studio/projectKV/services/go-server/internal/notify"
	"github.com/529-studio/projectKV/services/go-server/internal/store"
	"github.com/529-studio/projectKV/services/go-server/internal/subscription"
	"github.com/gin-gonic/gin"
)

func TestNotifyIntegration_EndToEnd(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 1. Setup in-memory SQLite store with migrations
	db, err := store.NewDB(":memory:")
	if err != nil {
		t.Fatalf("failed to initialize in-memory DB: %v", err)
	}
	defer db.Close()

	subStore := subscription.NewSQLiteStore(db)
	subHandler := subscription.NewHandler(subStore)

	// 2. Setup mock Telegram Bot API server
	var mu sync.Mutex
	receivedMessages := make(map[string][]string) // chat_id -> []messages

	mockTelegram := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/bot") || !strings.HasSuffix(r.URL.Path, "/sendMessage") {
			http.NotFound(w, r)
			return
		}

		var payload struct {
			ChatID    string `json:"chat_id"`
			Text      string `json:"text"`
			ParseMode string `json:"parse_mode"`
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Errorf("mock telegram unmarshal error: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		mu.Lock()
		receivedMessages[payload.ChatID] = append(receivedMessages[payload.ChatID], payload.Text)
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok": true, "result": {"message_id": 101}}`))
	}))
	defer mockTelegram.Close()

	// 3. Register subscriptions via HTTP handler
	router := gin.New()
	api := router.Group("/api")
	subHandler.RegisterRoutes(api)

	subs := []struct {
		chatID     string
		targetType string
		targetID   string
	}{
		{"chat-all-1001", "all", ""},
		{"chat-cam-1002", "chokepoint", "cam-hotspot-binh-thanh"},
		{"chat-zone-1003", "zone", "Quận Bình Thạnh"},
		{"chat-ward-1004", "ward", "Phường 25"},
		{"chat-unrelated-1005", "zone", "Quận 1"}, // Should not receive alert
	}

	for _, s := range subs {
		reqBody, _ := json.Marshal(subscription.SubscribeRequest{
			ChannelName: "telegram",
			ChatID:      s.chatID,
			TargetType:  s.targetType,
			TargetID:    s.targetID,
		})
		req := httptest.NewRequest(http.MethodPost, "/api/subscriptions", bytes.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("expected HTTP 201 for chat %s, got %d: %s", s.chatID, w.Code, w.Body.String())
		}
	}

	// 4. Wire notification Manager with Debouncer and TelegramChannel
	debouncer := notify.NewDebouncer(30 * time.Minute)
	manager := notify.NewManager(debouncer)

	subFinder := notify.SubscriberFinderFunc(func(ctx context.Context, channelName string, alert notify.FloodAlert) ([]string, error) {
		return subStore.GetSubscribersForChokepoint(ctx, channelName, alert.CameraID, alert.District, alert.Ward)
	})

	tgChannel := notify.NewTelegramChannel(
		"test-mock-token-xyz",
		subFinder,
		notify.WithBaseURL(mockTelegram.URL),
	)
	manager.Register(tgChannel)

	// 5. Dispatch verified flood alert for cam-hotspot-binh-thanh
	alert := notify.FloodAlert{
		CameraID:       "cam-hotspot-binh-thanh",
		CameraName:     "Nguyễn Xí - Cầu Đỏ",
		District:       "Quận Bình Thạnh",
		Ward:           "Phường 25",
		FloodSeverity:  "FLOODED_DEEP",
		TypicalDepthCm: 45,
		ObservedAt:     time.Now(),
		SnapshotURL:    "https://traffic.hochiminhcity.gov.vn/snapshot/123",
	}

	ctx := context.Background()
	if err := manager.Dispatch(ctx, alert); err != nil {
		t.Fatalf("unexpected dispatch error: %v", err)
	}

	// 6. Verify recipients
	mu.Lock()
	matchedChats := []string{"chat-all-1001", "chat-cam-1002", "chat-zone-1003", "chat-ward-1004"}
	for _, chatID := range matchedChats {
		msgs, ok := receivedMessages[chatID]
		if !ok || len(msgs) == 0 {
			t.Errorf("expected chat %s to receive message, got none", chatID)
			continue
		}
		msg := msgs[0]
		// Verify Rule 1 zero-emoji policy
		bannedEmojis := []string{"⚠️", "🚨", "🌊", "🌧️", "📢", "🛑"}
		for _, emoji := range bannedEmojis {
			if strings.Contains(msg, emoji) {
				t.Errorf("chat %s message violated Rule 1 emoji policy, contains %s", chatID, emoji)
			}
		}
		// Verify bilingual keywords
		if !strings.Contains(msg, "CẢNH BÁO NGẬP NƯỚC") || !strings.Contains(msg, "FLOOD ALERT") {
			t.Errorf("chat %s message missing bilingual header", chatID)
		}
		if !strings.Contains(msg, "Nguyễn Xí - Cầu Đỏ") {
			t.Errorf("chat %s message missing camera name", chatID)
		}
		if !strings.Contains(msg, "45 cm") {
			t.Errorf("chat %s message missing depth info", chatID)
		}
	}

	// Verify unrelated chat got 0 messages
	if msgs, ok := receivedMessages["chat-unrelated-1005"]; ok && len(msgs) > 0 {
		t.Errorf("chat-unrelated-1005 should not receive alert, but received %d messages", len(msgs))
	}
	initialCount := len(receivedMessages)
	mu.Unlock()

	// 7. Verify 30-minute anti-spam debounce: dispatching again immediately MUST be suppressed
	if err := manager.Dispatch(ctx, alert); err != nil {
		t.Fatalf("unexpected error on repeated dispatch: %v", err)
	}

	mu.Lock()
	for _, chatID := range matchedChats {
		if len(receivedMessages[chatID]) > 1 {
			t.Errorf("chat %s received duplicate message; debounce failed", chatID)
		}
	}
	if len(receivedMessages) != initialCount {
		t.Errorf("receivedMessages count changed despite debounce")
	}
	mu.Unlock()

	// 8. Verify non-flood severity is skipped
	dryAlert := alert
	dryAlert.CameraID = "cam-different-hotspot"
	dryAlert.FloodSeverity = "DRY"
	if err := manager.Dispatch(ctx, dryAlert); err != nil {
		t.Fatalf("unexpected error on dry dispatch: %v", err)
	}

	mu.Lock()
	if _, ok := receivedMessages["cam-different-hotspot"]; ok {
		t.Errorf("dry alert should not be dispatched")
	}
	mu.Unlock()
}

// TestNotifyIntegration_DebounceFiveMinuteWindow reproduces the Issue #246 /
// S8-T9 staging acceptance scenario deterministically: a second flood alert for
// the same chokepoint within 5 minutes MUST be suppressed by the 30-minute
// sliding debouncer, while an alert after the window expires dispatches again.
func TestNotifyIntegration_DebounceFiveMinuteWindow(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db, err := store.NewDB(":memory:")
	if err != nil {
		t.Fatalf("failed to initialize in-memory DB: %v", err)
	}
	defer db.Close()

	subStore := subscription.NewSQLiteStore(db)
	subHandler := subscription.NewHandler(subStore)

	var mu sync.Mutex
	receivedCount := 0

	mockTelegram := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		receivedCount++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok": true, "result": {"message_id": 202}}`))
	}))
	defer mockTelegram.Close()

	router := gin.New()
	api := router.Group("/api")
	subHandler.RegisterRoutes(api)

	reqBody, _ := json.Marshal(subscription.SubscribeRequest{
		ChannelName: "telegram",
		ChatID:      "chat-staging-verify",
		TargetType:  "chokepoint",
		TargetID:    "586e2995f9fab7001111b0b5",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/subscriptions", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected HTTP 201 for staging subscription, got %d: %s", w.Code, w.Body.String())
	}

	debouncer := notify.NewDebouncer(30 * time.Minute)
	manager := notify.NewManager(debouncer)

	subFinder := notify.SubscriberFinderFunc(func(ctx context.Context, channelName string, alert notify.FloodAlert) ([]string, error) {
		return subStore.GetSubscribersForChokepoint(ctx, channelName, alert.CameraID, alert.District, alert.Ward)
	})

	manager.Register(notify.NewTelegramChannel(
		"test-mock-token-staging",
		subFinder,
		notify.WithBaseURL(mockTelegram.URL),
	))

	baseAlert := notify.FloodAlert{
		CameraID:       "586e2995f9fab7001111b0b5",
		CameraName:     "Ung Văn Khiêm - Nguyễn Gia Trí (D2)",
		Ward:           "Thạnh Mỹ Tây",
		District:       "Khu vực Nội thành TP.HCM", // production cam.Zone (traffic zone, not administrative district)
		FloodSeverity:  "FLOODED_DEEP",
		TypicalDepthCm: 35,
	}

	ctx := context.Background()
	t0 := time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)

	// Dispatch 1: initial verified flood at T0 — must deliver.
	first := baseAlert
	first.ObservedAt = t0
	if err := manager.Dispatch(ctx, first); err != nil {
		t.Fatalf("unexpected dispatch error at T0: %v", err)
	}
	mu.Lock()
	if receivedCount != 1 {
		t.Fatalf("expected 1 delivered message after first dispatch, got %d", receivedCount)
	}
	mu.Unlock()

	// Dispatch 2: repeat at T0+5min (Issue #246 acceptance step) — must be suppressed.
	second := baseAlert
	second.ObservedAt = t0.Add(5 * time.Minute)
	if err := manager.Dispatch(ctx, second); err != nil {
		t.Fatalf("unexpected dispatch error at T0+5m: %v", err)
	}
	mu.Lock()
	if receivedCount != 1 {
		t.Fatalf("30-minute debouncer failed: expected suppression at T0+5m, got %d delivered messages", receivedCount)
	}
	mu.Unlock()

	if got := debouncer.LastDispatched(baseAlert.CameraID); !got.Equal(t0) {
		t.Errorf("suppressed dispatch must not advance the sliding window anchor: want %v, got %v", t0, got)
	}

	// Dispatch 3: repeat at T0+31min — window expired, must deliver again.
	third := baseAlert
	third.ObservedAt = t0.Add(31 * time.Minute)
	if err := manager.Dispatch(ctx, third); err != nil {
		t.Fatalf("unexpected dispatch error at T0+31m: %v", err)
	}
	mu.Lock()
	if receivedCount != 2 {
		t.Fatalf("expected window expiry to re-deliver at T0+31m, got %d delivered messages", receivedCount)
	}
	mu.Unlock()
}
