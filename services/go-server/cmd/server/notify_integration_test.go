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
