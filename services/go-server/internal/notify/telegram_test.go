package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode"
)

func TestFormatTelegramMessage_ZeroEmojiAndBilingual(t *testing.T) {
	alert := FloodAlert{
		CameraID:       "cam-test-01",
		CameraName:     "Võ Văn Kiệt - Ký Con & Cầu Ông Lãnh",
		District:       "Quận 1",
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
		District:      "Bình Thạnh",
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
