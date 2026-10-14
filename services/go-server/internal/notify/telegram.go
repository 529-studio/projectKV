package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const (
	defaultTelegramBaseURL = "https://api.telegram.org"
	defaultHTTPTimeout     = 10 * time.Second
)

// Sentinel errors for TelegramChannel
var (
	ErrTelegramTokenMissing = errors.New("telegram bot token is empty")
	ErrTelegramNoRecipient  = errors.New("recipient chat_id is empty")
)

// TelegramChannel implements Channel using the official Telegram Bot HTTP API.
// It formats flood alerts into clean, monochromatic, bilingual HTML messages
// with strictly zero emojis per Rule 1.
type TelegramChannel struct {
	botToken string
	baseURL  string
	client   *http.Client
	finder   SubscriberFinder
	log      *slog.Logger
}

// TelegramOption configures TelegramChannel.
type TelegramOption func(*TelegramChannel)

// WithBaseURL overrides the default Telegram API base URL (useful for test servers).
func WithBaseURL(url string) TelegramOption {
	return func(c *TelegramChannel) {
		if url != "" {
			c.baseURL = strings.TrimRight(url, "/")
		}
	}
}

// WithHTTPClient overrides the HTTP client.
func WithHTTPClient(client *http.Client) TelegramOption {
	return func(c *TelegramChannel) {
		if client != nil {
			c.client = client
		}
	}
}

// WithLogger overrides the logger.
func WithLogger(log *slog.Logger) TelegramOption {
	return func(c *TelegramChannel) {
		if log != nil {
			c.log = log
		}
	}
}

// NewTelegramChannel initializes a new Telegram delivery channel.
func NewTelegramChannel(botToken string, finder SubscriberFinder, opts ...TelegramOption) *TelegramChannel {
	c := &TelegramChannel{
		botToken: strings.TrimSpace(botToken),
		baseURL:  defaultTelegramBaseURL,
		client:   &http.Client{Timeout: defaultHTTPTimeout},
		finder:   finder,
		log:      slog.Default(),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Name returns the identifier for this channel.
func (c *TelegramChannel) Name() string {
	return "telegram"
}

// IsConfigured returns true if botToken is present.
func (c *TelegramChannel) IsConfigured() bool {
	return c.botToken != ""
}

// telegramSendMessagePayload represents the POST /bot<token>/sendMessage request.
type telegramSendMessagePayload struct {
	ChatID                string `json:"chat_id"`
	Text                  string `json:"text"`
	ParseMode             string `json:"parse_mode"`
	DisableWebPagePreview bool   `json:"disable_web_page_preview,omitempty"`
}

// telegramAPIResponse captures error details from Telegram Bot API.
type telegramAPIResponse struct {
	OK          bool   `json:"ok"`
	ErrorCode   int    `json:"error_code,omitempty"`
	Description string `json:"description,omitempty"`
}

// Send dispatches alert to all subscribers registered on Telegram for this alert.
func (c *TelegramChannel) Send(ctx context.Context, alert FloodAlert) error {
	if !c.IsConfigured() {
		c.log.Info("telegram: bot token not configured, skipping alert delivery", "camera_id", alert.CameraID)
		return nil
	}

	if c.finder == nil {
		c.log.Warn("telegram: no SubscriberFinder configured, skipping alert", "camera_id", alert.CameraID)
		return nil
	}

	subscribers, err := c.finder.GetSubscribersForAlert(ctx, c.Name(), alert)
	if err != nil {
		return fmt.Errorf("telegram: lookup subscribers for alert %s: %w", alert.CameraID, err)
	}

	if len(subscribers) == 0 {
		c.log.Debug("telegram: no subscribers matching flood alert", "camera_id", alert.CameraID, "severity", alert.FloodSeverity)
		return nil
	}

	c.log.Info("telegram: dispatching flood alert", "camera_id", alert.CameraID, "severity", alert.FloodSeverity, "recipients_count", len(subscribers))

	var errs []error
	for _, chatID := range subscribers {
		if err := c.SendTo(ctx, chatID, alert); err != nil {
			errs = append(errs, fmt.Errorf("chat %s: %w", chatID, err))
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// SendTo sends a formatted flood alert message directly to a specific Telegram chat ID.
func (c *TelegramChannel) SendTo(ctx context.Context, chatID string, alert FloodAlert) error {
	if !c.IsConfigured() {
		return ErrTelegramTokenMissing
	}
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		return ErrTelegramNoRecipient
	}

	text := FormatTelegramMessage(alert)
	payload := telegramSendMessagePayload{
		ChatID:                chatID,
		Text:                  text,
		ParseMode:             "HTML",
		DisableWebPagePreview: false,
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal telegram payload: %w", err)
	}

	endpoint := fmt.Sprintf("%s/bot%s/sendMessage", c.baseURL, c.botToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("create telegram request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		// Rule 8: Do not leak bot token if request URL is formatted in error
		sanitizedErr := strings.ReplaceAll(err.Error(), c.botToken, "[REDACTED_BOT_TOKEN]")
		return fmt.Errorf("telegram http post: %s", sanitizedErr)
	}
	defer resp.Body.Close()

	respBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		var apiResp telegramAPIResponse
		_ = json.Unmarshal(respBytes, &apiResp)
		desc := apiResp.Description
		if desc == "" {
			desc = strings.TrimSpace(string(respBytes))
		}
		return fmt.Errorf("telegram api error (status %d): %s", resp.StatusCode, desc)
	}

	return nil
}

// FormatTelegramMessage formats a FloodAlert into clean, monochromatic, bilingual HTML.
// Strictly zero emojis per Rule 1.
func FormatTelegramMessage(alert FloodAlert) string {
	var severityVI, severityEN string
	switch alert.FloodSeverity {
	case "FLOODED_DEEP":
		severityVI = "NGẬP SÂU"
		severityEN = "DEEP FLOOD"
	case "FLOODED_SHALLOW":
		severityVI = "NGẬP NÔNG"
		severityEN = "SHALLOW FLOOD"
	case "WET":
		severityVI = "ĐƯỜNG ƯỚT"
		severityEN = "WET PAVEMENT"
	default:
		severityVI = "CẢNH BÁO"
		severityEN = "ALERT"
	}

	obsTime := alert.ObservedAt
	if obsTime.IsZero() {
		obsTime = time.Now()
	}
	// Vietnam ICT is UTC+7
	ict := time.FixedZone("ICT", 7*3600)
	timeFormatted := obsTime.In(ict).Format("15:04 02/01/2006 (ICT)")

	camName := escapeHTML(alert.CameraName)
	if camName == "" {
		camName = escapeHTML(alert.CameraID)
	}

	var locationParts []string
	if alert.Ward != "" {
		locationParts = append(locationParts, escapeHTML(alert.Ward))
	}
	if alert.District != "" {
		locationParts = append(locationParts, escapeHTML(alert.District))
	}
	locationStr := strings.Join(locationParts, ", ")
	if locationStr == "" {
		locationStr = "TP. Hồ Chí Minh"
	}

	var sb strings.Builder
	sb.WriteString("<b>[CẢNH BÁO NGẬP NƯỚC / FLOOD ALERT]</b>\n\n")
	sb.WriteString(fmt.Sprintf("<b>Điểm ngập / Chokepoint:</b> %s\n", camName))
	sb.WriteString(fmt.Sprintf("<b>Khu vực / Area:</b> %s\n", locationStr))
	sb.WriteString(fmt.Sprintf("<b>Mức độ / Severity:</b> [%s / %s]\n", severityVI, severityEN))

	if alert.TypicalDepthCm > 0 {
		sb.WriteString(fmt.Sprintf("<b>Độ sâu ước lượng / Depth:</b> ~%d cm\n", alert.TypicalDepthCm))
	}

	sb.WriteString(fmt.Sprintf("<b>Thời gian / Time:</b> %s\n", timeFormatted))

	liveURL := alert.LiveWebURL
	if liveURL == "" && alert.CameraID != "" {
		liveURL = fmt.Sprintf("https://warded.529studio.site/?cam=%s", alert.CameraID)
	}
	if liveURL != "" {
		sb.WriteString(fmt.Sprintf("<b>Chi tiết / Live View:</b> <a href=\"%s\">%s</a>", escapeHTML(liveURL), escapeHTML(liveURL)))
	}

	return sb.String()
}

func escapeHTML(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		"\"", "&quot;",
	)
	return r.Replace(s)
}
