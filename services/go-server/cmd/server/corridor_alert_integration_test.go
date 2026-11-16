package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode"

	"github.com/529-studio/projectKV/services/go-server/internal/corridor"
	"github.com/529-studio/projectKV/services/go-server/internal/notify"
	"github.com/529-studio/projectKV/services/go-server/internal/store"
	"github.com/529-studio/projectKV/services/go-server/internal/subscription"
	"github.com/529-studio/projectKV/services/go-server/internal/telegrambot"
	"github.com/gin-gonic/gin"
)

// assertMonochromaticNoEmoji validates that a citizen-facing message adheres strictly
// to AGENTS.md Rule 1: zero emojis and monochromatic styling.
func assertMonochromaticNoEmoji(t *testing.T, label, text string) {
	t.Helper()
	for _, r := range text {
		if (unicode.Is(unicode.So, r) && r > 0x00A9) || (unicode.Is(unicode.Sk, r) && r > 0x00A9) {
			t.Fatalf("Rule 1 violation in %s: found symbol/emoji codepoint U+%04X (%c)", label, r, r)
		}
		switch {
		case r >= 0x1F000 && r <= 0x1FAFF, // emoji & pictographs
			r >= 0x2600 && r <= 0x27BF, // misc symbols & dingbats
			r >= 0x2B00 && r <= 0x2BFF, // star / box icons
			r >= 0x2190 && r <= 0x21FF, // arrows
			r == 0xFE0F,                // variation selector-16
			r == 0x200D:                // zero-width joiner
			t.Fatalf("Rule 1 violation in %s: found emoji codepoint U+%04X (%c)", label, r, r)
		}
	}
	for _, banned := range []string{"⚠️", "🚨", "🌊", "🚗", "📍", "🔔"} {
		if strings.Contains(text, banned) {
			t.Fatalf("Rule 1 violation in %s: found banned alert emoji %q", label, banned)
		}
	}
}

// TestCorridorAlertIntegration_Rule10Scenarios executes all 5 manual testing scenarios
// defined in Issue #478 to provide rigorous, empirical end-to-end verification.
func TestCorridorAlertIntegration_Rule10Scenarios(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 1. Setup in-memory SQLite store
	db, err := store.NewDB(":memory:")
	if err != nil {
		t.Fatalf("failed to initialize in-memory DB: %v", err)
	}
	defer db.Close()

	subStore := subscription.NewSQLiteStore(db)

	// 2. Setup mock Telegram API capture server
	var mu sync.Mutex
	receivedMessages := make(map[string][]string) // chat_id -> list of HTML bodies

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		mu.Lock()
		receivedMessages[payload.ChatID] = append(receivedMessages[payload.ChatID], payload.Text)
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok": true, "result": {"message_id": 1001}}`))
	}))
	defer mockServer.Close()

	// 3. Wire notification manager and Telegram delivery channel
	corridorSubFinder := notify.CorridorSubscriberFinderFunc(func(ctx context.Context, channelName string, alert notify.CorridorAlert) ([]string, error) {
		return subStore.GetSubscribersForCorridor(ctx, channelName, alert.RoadKey)
	})
	telegramChan := notify.NewTelegramChannel(
		"test-bot-token",
		nil,
		notify.WithBaseURL(mockServer.URL),
		notify.WithCorridorSubscriberFinder(corridorSubFinder),
	)
	notifyManager := notify.NewManager(nil) // Uses 15-minute corridor debounce window by default
	notifyManager.Register(telegramChan)

	// 4. Wire inbound Telegram bot handler
	const secret = "secret-token-for-testing-projectkv-webhook-32chars"
	const botName = "WardedAlertBot"

	botTaxonomy := &telegramBotTaxonomyAdapter{
		catalog:         nil, // corridor definitions are checked from corridor.ArterialCorridors
		corridorService: nil,
	}
	botHandler := telegrambot.NewHandler(
		telegrambot.Config{
			BotUsername:      botName,
			WebhookSecret:    secret,
			Workers:          1,
			QueueSize:        32,
			MaxSubsPerChat:   10,
			PerChatPerMinute: 60,
		},
		&telegramBotStoreAdapter{store: subStore},
		botTaxonomy,
		telegramChan,
		slog.Default(),
	)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = botHandler.Shutdown(ctx)
	}()

	botRouter := gin.New()
	botHandler.RegisterRoutes(botRouter)

	postWebhook := func(updateID int64, chatID string, text string) {
		payload := map[string]any{
			"update_id": updateID,
			"message": map[string]any{
				"message_id": updateID,
				"date":       time.Now().Unix(),
				"text":       text,
				"from": map[string]any{
					"id":       10001,
					"is_bot":   false,
					"username": "commuter",
				},
				"chat": map[string]any{
					"id":   10001,
					"type": "private",
				},
			},
		}
		if chatID != "10001" {
			// override chat ID
			var numID int64 = 20002
			if chatID == "chat_user_1" {
				numID = 10001
			} else if chatID == "chat_user_2" {
				numID = 20002
			}
			payload["message"].(map[string]any)["from"].(map[string]any)["id"] = numID
			payload["message"].(map[string]any)["chat"].(map[string]any)["id"] = numID
		}
		bodyBytes, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, telegrambot.WebhookPath, bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Telegram-Bot-Api-Secret-Token", secret)
		w := httptest.NewRecorder()
		botRouter.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("webhook returned %d: %s", w.Code, w.Body.String())
		}
		// Wait for single worker to finish processing
		time.Sleep(50 * time.Millisecond)
	}

	// -------------------------------------------------------------------------
	// Scenario 1: Send command /sub corridor vo_van_kiet to the bot;
	// verify confirmation response and record in SQLite database.
	// -------------------------------------------------------------------------
	postWebhook(1, "chat_user_1", "/sub corridor vo_van_kiet")

	mu.Lock()
	msgsUser1 := receivedMessages["10001"]
	mu.Unlock()

	if len(msgsUser1) != 1 {
		t.Fatalf("Scenario 1 failed: expected 1 confirmation message for chat 10001, got %d", len(msgsUser1))
	}
	assertMonochromaticNoEmoji(t, "Scenario 1 confirmation", msgsUser1[0])
	if !strings.Contains(msgsUser1[0], "Đã đăng ký thành công") || !strings.Contains(msgsUser1[0], "Đại lộ Võ Văn Kiệt") {
		t.Fatalf("Scenario 1 failed: confirmation message missing expected text:\n%s", msgsUser1[0])
	}

	// Verify persistence in SQLite
	subsUser1, err := subStore.ListSubscriptions(context.Background(), "telegram", "10001")
	if err != nil {
		t.Fatalf("Scenario 1 failed: ListSubscriptions error: %v", err)
	}
	if len(subsUser1) != 1 || subsUser1[0].TargetType != subscription.TargetCorridor || subsUser1[0].TargetID != "vo_van_kiet" {
		t.Fatalf("Scenario 1 failed: unexpected DB records: %+v", subsUser1)
	}

	// -------------------------------------------------------------------------
	// Scenario 2: Simulate incoming /start sub_corridor_mai_chi_tho;
	// verify user is subscribed and received initial condition report.
	// -------------------------------------------------------------------------
	postWebhook(2, "chat_user_2", "/start sub_corridor_mai_chi_tho")

	mu.Lock()
	msgsUser2 := receivedMessages["20002"]
	mu.Unlock()

	if len(msgsUser2) != 1 {
		t.Fatalf("Scenario 2 failed: expected 1 response for chat 20002, got %d", len(msgsUser2))
	}
	assertMonochromaticNoEmoji(t, "Scenario 2 start reply", msgsUser2[0])
	if !strings.Contains(msgsUser2[0], "Đường Mai Chí Thọ") || !strings.Contains(msgsUser2[0], "Tình trạng hiện tại") {
		t.Fatalf("Scenario 2 failed: initial condition report missing expected text:\n%s", msgsUser2[0])
	}

	// Verify persistence for user 2
	subsUser2, err := subStore.ListSubscriptions(context.Background(), "telegram", "20002")
	if err != nil {
		t.Fatalf("Scenario 2 failed: ListSubscriptions error: %v", err)
	}
	if len(subsUser2) != 1 || subsUser2[0].TargetType != subscription.TargetCorridor || subsUser2[0].TargetID != "mai_chi_tho" {
		t.Fatalf("Scenario 2 failed: unexpected DB records for user 2: %+v", subsUser2)
	}

	// -------------------------------------------------------------------------
	// Scenario 3: Trigger simulated high flood depth event (>20 cm) on corridor;
	// verify alert message dispatches with correct peak depth (cm) and no emojis.
	// -------------------------------------------------------------------------
	t0 := time.Date(2026, 11, 16, 8, 0, 0, 0, time.UTC)
	peakDepth := 35.0
	hazardAlert1 := notify.BuildCorridorAlert(
		"vo_van_kiet",
		corridor.ArterialCorridors["vo_van_kiet"].DisplayName,
		"IMPASSABLE",
		"FLOODED_DEEP",
		&peakDepth,
		"Cầu Rạch Chiếc",
		25,
		t0,
		"https://warded.529studio.site/roads/vo_van_kiet",
	)

	if err := notifyManager.DispatchCorridor(context.Background(), hazardAlert1); err != nil {
		t.Fatalf("Scenario 3 failed: DispatchCorridor error: %v", err)
	}

	mu.Lock()
	msgsUser1 = receivedMessages["10001"]
	mu.Unlock()

	// msgsUser1 should now have 2 messages: [0] = subscription confirmation, [1] = corridor hazard alert
	if len(msgsUser1) != 2 {
		t.Fatalf("Scenario 3 failed: expected 2 messages for user 1, got %d", len(msgsUser1))
	}
	alertMsg := msgsUser1[1]
	assertMonochromaticNoEmoji(t, "Scenario 3 hazard alert", alertMsg)
	for _, expected := range []string{
		"[CẢNH BÁO TUYẾN ĐƯỜNG / CORRIDOR HAZARD ALERT]",
		"Đại lộ Võ Văn Kiệt",
		"[NGẬP SÂU - ÙN TẮC / IMPASSABLE]",
		"35 cm tại Cầu Rạch Chiếc",
		"+25 phút / +25 mins",
		"https://warded.529studio.site/roads/vo_van_kiet",
	} {
		if !strings.Contains(alertMsg, expected) {
			t.Fatalf("Scenario 3 failed: alert message missing %q:\n%s", expected, alertMsg)
		}
	}

	// User 2 (subscribed to mai_chi_tho) should NOT receive this alert for vo_van_kiet
	mu.Lock()
	msgsUser2 = receivedMessages["20002"]
	mu.Unlock()
	if len(msgsUser2) != 1 {
		t.Fatalf("Scenario 3 failed: user 2 should not receive vo_van_kiet alert, got %d messages", len(msgsUser2))
	}

	// -------------------------------------------------------------------------
	// Scenario 4: Trigger a second event 2 minutes later; verify debounce logic suppresses message.
	// -------------------------------------------------------------------------
	hazardAlert2 := notify.BuildCorridorAlert(
		"vo_van_kiet",
		corridor.ArterialCorridors["vo_van_kiet"].DisplayName,
		"IMPASSABLE",
		"FLOODED_DEEP",
		&peakDepth,
		"Cầu Rạch Chiếc",
		30,
		t0.Add(2*time.Minute),
		"https://warded.529studio.site/roads/vo_van_kiet",
	)

	if err := notifyManager.DispatchCorridor(context.Background(), hazardAlert2); err != nil {
		t.Fatalf("Scenario 4 failed: DispatchCorridor returned error on debounce: %v", err)
	}

	mu.Lock()
	msgsUser1AfterDebounce := receivedMessages["10001"]
	mu.Unlock()

	if len(msgsUser1AfterDebounce) != 2 {
		t.Fatalf("Scenario 4 failed: debounce must suppress message, but message count became %d", len(msgsUser1AfterDebounce))
	}

	// -------------------------------------------------------------------------
	// Scenario 5: Send /unsub corridor vo_van_kiet; verify cancellation confirmed and verified in DB.
	// -------------------------------------------------------------------------
	postWebhook(3, "chat_user_1", "/unsub corridor vo_van_kiet")

	mu.Lock()
	msgsUser1Final := receivedMessages["10001"]
	mu.Unlock()

	// Expect 3 messages: [0] sub confirm, [1] hazard alert, [2] unsub confirm
	if len(msgsUser1Final) != 3 {
		t.Fatalf("Scenario 5 failed: expected 3 messages for user 1, got %d", len(msgsUser1Final))
	}
	unsubMsg := msgsUser1Final[2]
	assertMonochromaticNoEmoji(t, "Scenario 5 unsub reply", unsubMsg)
	if !strings.Contains(unsubMsg, "Đã huỷ đăng ký thành công") || !strings.Contains(unsubMsg, "Đại lộ Võ Văn Kiệt") {
		t.Fatalf("Scenario 5 failed: unsub confirmation missing expected text:\n%s", unsubMsg)
	}

	// Verify cancellation in DB
	subsUser1Final, err := subStore.ListSubscriptions(context.Background(), "telegram", "10001")
	if err != nil {
		t.Fatalf("Scenario 5 failed: ListSubscriptions error: %v", err)
	}
	if len(subsUser1Final) != 0 {
		t.Fatalf("Scenario 5 failed: expected 0 DB subscriptions for user 1, got %d (%+v)", len(subsUser1Final), subsUser1Final)
	}
}
