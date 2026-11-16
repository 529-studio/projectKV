package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode"
)

func TestFormatTelegramMessage_ZeroEmojiAndBilingual(t *testing.T) {
	alert := FloodAlert{
		CameraID:       "cam-test-01",
		CameraName:     "Võ Văn Kiệt - Ký Con & Cầu Ông Lãnh",
		Zone:           "Quận 1",
		Ward:           "Phường Cầu Ông Lãnh",
		FloodSeverity:  "FLOODED_DEEP",
		TypicalDepthCm: 35,
		ObservedAt:     time.Date(2026, 10, 14, 17, 30, 0, 0, time.UTC),
		LiveWebURL:     "https://warded.529studio.site/?cam=cam-test-01",
	}

	msg := FormatTelegramMessage(alert)

	// 1. Verify Zero-Emoji policy (Rule 1)
	for _, r := range msg {
		if unicode.Is(unicode.So, r) || unicode.Is(unicode.Sk, r) {
			// Symbols / pictographs
			if r > 0x1F000 {
				t.Fatalf("Rule 1 violation: found emoji in message: %c (U+%04X)", r, r)
			}
		}
	}
	if strings.Contains(msg, "⚠️") || strings.Contains(msg, "🚨") || strings.Contains(msg, "🌊") {
		t.Fatalf("Rule 1 violation: found explicit banned alert emoji in formatted message")
	}

	// 2. Verify bilingual format
	expectedSubstrings := []string{
		"[CẢNH BÁO NGẬP NƯỚC / FLOOD ALERT]",
		"Điểm ngập / Chokepoint:",
		"Võ Văn Kiệt - Ký Con &amp; Cầu Ông Lãnh", // properly HTML escaped &
		"Phường Cầu Ông Lãnh, Quận 1",
		"[NGẬP SÂU / DEEP FLOOD]",
		"~35 cm",
		"00:30 15/10/2026 (ICT)", // 17:30 UTC + 7 = 00:30 next day ICT
		"<a href=\"https://warded.529studio.site/?cam=cam-test-01\">",
	}

	for _, sub := range expectedSubstrings {
		if !strings.Contains(msg, sub) {
			t.Errorf("expected message to contain %q, but got:\n%s", sub, msg)
		}
	}
}

func TestTelegramChannel_Send_MockServer(t *testing.T) {
	var receivedRequests []telegramSendMessagePayload

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/botmy-secret-token/sendMessage") {
			t.Errorf("unexpected URL path: %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var p telegramSendMessagePayload
		if err := json.Unmarshal(body, &p); err != nil {
			t.Errorf("unmarshal request payload: %v", err)
		}
		receivedRequests = append(receivedRequests, p)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok": true, "result": {"message_id": 12345}}`))
	}))
	defer server.Close()

	finder := SubscriberFinderFunc(func(ctx context.Context, channelName string, alert FloodAlert) ([]string, error) {
		return []string{"111222", "333444"}, nil
	})

	ch := NewTelegramChannel(
		"my-secret-token",
		finder,
		WithBaseURL(server.URL),
	)

	alert := FloodAlert{
		CameraID:      "cam-01",
		CameraName:    "Ung Văn Khiêm",
		Zone:          "Bình Thạnh",
		FloodSeverity: "FLOODED_SHALLOW",
	}

	err := ch.Send(context.Background(), alert)
	if err != nil {
		t.Fatalf("expected Send to succeed, got %v", err)
	}

	if len(receivedRequests) != 2 {
		t.Fatalf("expected 2 telegram requests, got %d", len(receivedRequests))
	}

	if receivedRequests[0].ChatID != "111222" || receivedRequests[1].ChatID != "333444" {
		t.Errorf("unexpected chat IDs: %v", receivedRequests)
	}
	if receivedRequests[0].ParseMode != "HTML" {
		t.Errorf("expected parse_mode HTML, got %s", receivedRequests[0].ParseMode)
	}
}

func TestFormatTelegramCorridorMessage_ZeroEmojiAndBilingual(t *testing.T) {
	depth := 35.0
	alert := CorridorAlert{
		RoadKey:            "vo_van_kiet",
		RoadName:           "Đại lộ Võ Văn Kiệt & Ký Con",
		Passability:        "IMPASSABLE",
		WorstFloodSeverity: "FLOODED_DEEP",
		MaxDepthCm:         &depth,
		PeakLocation:       "Cầu Rạch Chiếc",
		EstimatedDelayMin:  25,
		ObservedAt:         time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC),
		RouteWebURL:        "https://warded.529studio.site/roads/vo_van_kiet",
	}

	msg := FormatTelegramCorridorMessage(alert)

	// 1. Verify Zero-Emoji policy (Rule 1)
	for _, r := range msg {
		if unicode.Is(unicode.So, r) || unicode.Is(unicode.Sk, r) {
			if r > 0x1F000 {
				t.Fatalf("Rule 1 violation: found emoji in message: %c (U+%04X)", r, r)
			}
		}
	}
	if strings.Contains(msg, "⚠️") || strings.Contains(msg, "🚨") || strings.Contains(msg, "🌊") || strings.Contains(msg, "🚗") {
		t.Fatalf("Rule 1 violation: found explicit banned alert emoji in corridor message")
	}

	// 2. Verify bilingual format and HTML escaping
	expectedSubstrings := []string{
		"[CẢNH BÁO TUYẾN ĐƯỜNG / CORRIDOR HAZARD ALERT]",
		"Tuyến đường / Corridor:",
		"Đại lộ Võ Văn Kiệt &amp; Ký Con", // properly HTML escaped &
		"Trạng thái / Passability:",
		"[NGẬP SÂU - ÙN TẮC / IMPASSABLE]",
		"35 cm tại Cầu Rạch Chiếc",
		"+25 phút / +25 mins",
		"17:00 16/11/2026 (ICT)", // 10:00 UTC + 7 = 17:00 ICT
		"<a href=\"https://warded.529studio.site/roads/vo_van_kiet\">",
	}

	for _, sub := range expectedSubstrings {
		if !strings.Contains(msg, sub) {
			t.Errorf("expected corridor message to contain %q, but got:\n%s", sub, msg)
		}
	}
}

func TestTelegramChannel_SendCorridor_MockServer(t *testing.T) {
	var receivedRequests []telegramSendMessagePayload

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var p telegramSendMessagePayload
		_ = json.Unmarshal(body, &p)
		receivedRequests = append(receivedRequests, p)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok": true, "result": {"message_id": 999}}`))
	}))
	defer server.Close()

	corridorFinder := CorridorSubscriberFinderFunc(func(ctx context.Context, channelName string, alert CorridorAlert) ([]string, error) {
		return []string{"chat-corr-1", "chat-corr-2"}, nil
	})

	ch := NewTelegramChannel(
		"my-secret-token",
		nil,
		WithCorridorSubscriberFinder(corridorFinder),
		WithBaseURL(server.URL),
	)

	depth := 22.0
	alert := CorridorAlert{
		RoadKey:     "mai_chi_tho",
		RoadName:    "Đường Mai Chí Thọ",
		Passability: "CAUTION",
		MaxDepthCm:  &depth,
	}

	err := ch.SendCorridor(context.Background(), alert)
	if err != nil {
		t.Fatalf("expected SendCorridor to succeed, got %v", err)
	}

	if len(receivedRequests) != 2 {
		t.Fatalf("expected 2 telegram requests, got %d", len(receivedRequests))
	}
	if receivedRequests[0].ChatID != "chat-corr-1" || receivedRequests[1].ChatID != "chat-corr-2" {
		t.Errorf("unexpected recipients: %+v", receivedRequests)
	}
	if !strings.Contains(receivedRequests[0].Text, "Đường Mai Chí Thọ") || !strings.Contains(receivedRequests[0].Text, "22 cm") {
		t.Errorf("unexpected message text:\n%s", receivedRequests[0].Text)
	}
}

func TestTelegramChannel_Send_TokenRedactedOnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"ok": false, "error_code": 400, "description": "Bad Request: chat not found"}`))
	}))
	defer server.Close()

	ch := NewTelegramChannel(
		"secret-token-12345",
		nil,
		WithBaseURL(server.URL),
	)

	alert := FloodAlert{CameraID: "cam-01"}
	err := ch.SendTo(context.Background(), "invalid-chat-id", alert)
	if err == nil {
		t.Fatalf("expected error from 400 bad request")
	}

	// Verify token is NOT leaked in error string (Rule 8)
	if strings.Contains(err.Error(), "secret-token-12345") {
		t.Fatalf("Rule 8 violation: secret token leaked in error message: %v", err)
	}
}

func TestTelegramChannel_UnconfiguredTokenNoop(t *testing.T) {
	ch := NewTelegramChannel("", nil)
	alert := FloodAlert{CameraID: "cam-01"}
	// Should gracefully return nil without error when unconfigured
	if err := ch.Send(context.Background(), alert); err != nil {
		t.Fatalf("expected nil when token is unconfigured, got %v", err)
	}
}

func TestTelegramChannel_SendMessage_RateLimitRetriesOnce(t *testing.T) {
	var mu sync.Mutex
	calls := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		attempt := calls
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if attempt == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"ok":false,"error_code":429,"description":"Too Many Requests","parameters":{"retry_after":1}}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	ch := NewTelegramChannel("retry-token", nil, WithBaseURL(server.URL))
	if err := ch.SendMessage(context.Background(), "123456789", "<b>Xin chào</b>"); err != nil {
		t.Fatalf("expected the single 429 retry to succeed, got %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if calls != 2 {
		t.Fatalf("sendMessage calls = %d, want 2 (one retry)", calls)
	}
}

func TestTelegramChannel_SendMessage_BlockedBotReturnsError(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"ok":false,"error_code":403,"description":"Forbidden: bot was blocked by the user"}`))
	}))
	defer server.Close()

	ch := NewTelegramChannel("blocked-token", nil, WithBaseURL(server.URL))
	err := ch.SendMessage(context.Background(), "987654321", "hello")
	if err == nil {
		t.Fatal("expected an error when the bot is blocked")
	}
	if calls != 1 {
		t.Fatalf("a 403 must not be retried: calls = %d, want 1", calls)
	}
	if strings.Contains(err.Error(), "blocked-token") {
		t.Fatalf("Rule 8 violation: token leaked in error: %v", err)
	}
}

// TestFormatTelegramMessage_StagingChokepointFixture pins the exact rendered
// alert for the staging verification camera (Issue #246 / S8-T9). The ops
// pre-flight script scripts/verify_staging_telegram.sh mirrors this output;
// if this test changes, the script message must be updated in lockstep.
func TestFormatTelegramMessage_StagingChokepointFixture(t *testing.T) {
	alert := FloodAlert{
		CameraID:       "586e2995f9fab7001111b0b5",
		CameraName:     "Ung Văn Khiêm - Nguyễn Gia Trí (D2)",
		Ward:           "Thạnh Mỹ Tây",
		Zone:           "Khu vực Nội thành TP.HCM", // production cam.Zone for this camera (default ZoneNoiThanh classification)
		FloodSeverity:  "FLOODED_DEEP",
		TypicalDepthCm: 35,
		ObservedAt:     time.Date(2026, 10, 24, 14, 15, 0, 0, time.UTC),
	}

	msg := FormatTelegramMessage(alert)

	// Rule 1: zero emojis across the full pictograph/symbol spectrum.
	for _, r := range msg {
		if r > 0x1F000 && (unicode.Is(unicode.So, r) || unicode.Is(unicode.Sk, r)) {
			t.Fatalf("Rule 1 violation: found emoji in message: %c (U+%04X)", r, r)
		}
	}

	// Exact canonical rendering: 14:15 UTC + 7 = 21:15 ICT same day.
	want := "<b>[CẢNH BÁO NGẬP NƯỚC / FLOOD ALERT]</b>\n\n" +
		"<b>Điểm ngập / Chokepoint:</b> Ung Văn Khiêm - Nguyễn Gia Trí (D2)\n" +
		"<b>Khu vực / Area:</b> Thạnh Mỹ Tây, Khu vực Nội thành TP.HCM\n" +
		"<b>Mức độ / Severity:</b> [NGẬP SÂU / DEEP FLOOD]\n" +
		"<b>Độ sâu ước lượng / Depth:</b> ~35 cm\n" +
		"<b>Thời gian / Time:</b> 21:15 24/10/2026 (ICT)\n" +
		"<b>Chi tiết / Live View:</b> <a href=\"https://warded.529studio.site/?cam=586e2995f9fab7001111b0b5\">https://warded.529studio.site/?cam=586e2995f9fab7001111b0b5</a>"

	if msg != want {
		t.Fatalf("staging fixture message drifted from canonical format.\nwant:\n%s\n\ngot:\n%s", want, msg)
	}
}
