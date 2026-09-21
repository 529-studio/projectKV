package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// TelegramChannel implements Channel using the Telegram Bot HTTP API
// directly (net/http, no SDK dependency — keeps with the project's
// zero-bloat/minimal-dependency philosophy). recipientID here is the
// Telegram chat_id obtained when a user sends /start (and /subscribe
// <zone>) to the bot; capturing that mapping is the webhook handler's job,
// not this file's.
type TelegramChannel struct {
	botToken string
	client   *http.Client
}

func NewTelegramChannel(botToken string) *TelegramChannel {
	return &TelegramChannel{
		botToken: botToken,
		client:   &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *TelegramChannel) Name() string { return "telegram" }

type telegramSendMessageRequest struct {
	ChatID    string `json:"chat_id"`
	Text      string `json:"text"`
	ParseMode string `json:"parse_mode,omitempty"`
}

func (c *TelegramChannel) Send(ctx context.Context, recipientID string, alert Alert) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", c.botToken)

	body, err := json.Marshal(telegramSendMessageRequest{
		ChatID: recipientID,
		Text:   formatAlertText(alert),
	})
	if err != nil {
		return fmt.Errorf("marshal telegram payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build telegram request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("telegram request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram API returned status %d", resp.StatusCode)
	}
	return nil
}

// formatAlertText renders a human-readable alert. Replace with a proper
// i18n-aware template once the vi/en message catalog from apps/web is
// shared with the backend (or duplicate the relevant strings here — do not
// let this drift silently from the frontend's wording).
func formatAlertText(alert Alert) string {
	return fmt.Sprintf(
		"⚠️ Cảnh báo khu vực %s\nCamera: %s\nMức độ: %s\n%s",
		alert.Zone, alert.CameraName, alert.Severity, alert.Message,
	)
}

// NOTE on subscription capture (not implemented here — belongs in a
// webhook handler, e.g. internal/notify/telegram_webhook.go):
//
//   POST /telegram/webhook receives Update objects from Telegram.
//   On a "/subscribe <zone>" command, upsert a Subscription{
//       ChannelName: "telegram", RecipientID: update.Message.Chat.ID, Zone: zone,
//   } into the SubscriptionStore (SQLite).
//
// Telegram requires either webhook (public HTTPS endpoint) or long-polling
// via getUpdates; webhook is preferable once the Go server has a public
// domain, long-polling is fine for local development.
