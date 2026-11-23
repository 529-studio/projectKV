package push

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultPushTimeout = 10 * time.Second
	defaultPushTTL     = "86400"
)

// NotificationPayload represents the JSON body sent inside the encrypted Web Push message.
type NotificationPayload struct {
	Title    string `json:"title"`
	Body     string `json:"body"`
	URL      string `json:"url,omitempty"`
	RoadKey  string `json:"road_key,omitempty"`
	Severity string `json:"severity,omitempty"`
	Tag      string `json:"tag,omitempty"`
}

// ClientOption configures the push Client.
type ClientOption func(*Client)

// WithHTTPClient overrides the default HTTP client used for pushing messages.
func WithHTTPClient(client *http.Client) ClientOption {
	return func(c *Client) {
		if client != nil {
			c.client = client
		}
	}
}

// WithLogger overrides the logger.
func WithLogger(log *slog.Logger) ClientOption {
	return func(c *Client) {
		if log != nil {
			c.log = log
		}
	}
}

// Client manages VAPID signing, payload encryption, and HTTP dispatch to push services.
type Client struct {
	vapidKey *VAPIDKey
	subject  string
	store    Store
	client   *http.Client
	log      *slog.Logger
}

// NewClient creates a new push Client.
func NewClient(vapidKey *VAPIDKey, subject string, store Store, opts ...ClientOption) *Client {
	c := &Client{
		vapidKey: vapidKey,
		subject:  strings.TrimSpace(subject),
		store:    store,
		client: &http.Client{
			Timeout: defaultPushTimeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				// Push services do not issue redirects; prevent SSRF redirect bypass
				return http.ErrUseLastResponse
			},
		},
		log: slog.Default(),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

var allowedPushHosts = map[string]struct{}{
	"fcm.googleapis.com":                {},
	"web.push.apple.com":                {},
	"updates.push.services.mozilla.com": {},
	"localhost":                         {},
	"127.0.0.1":                         {},
}

// IsAllowedPushServiceHost verifies that the destination host belongs to an authorized browser vendor push service.
func IsAllowedPushServiceHost(host string) bool {
	lower := strings.ToLower(host)
	if _, ok := allowedPushHosts[lower]; ok {
		return true
	}
	if strings.HasSuffix(lower, ".push.apple.com") ||
		strings.HasSuffix(lower, ".googleapis.com") ||
		strings.HasSuffix(lower, ".services.mozilla.com") ||
		strings.HasSuffix(lower, ".autopush.net") ||
		strings.HasSuffix(lower, ".notify.windows.com") {
		return true
	}
	return false
}

// ValidateEndpoint enforces strict SSRF protections on Web Push service URLs.
// It requires endpoints to strictly begin with authorized browser push vendor gateway prefixes
// or local test server URLs.
func ValidateEndpoint(rawEndpoint string) (*url.URL, error) {
	raw := strings.TrimSpace(rawEndpoint)
	if raw == "" {
		return nil, errors.New("subscription endpoint is empty")
	}
	if len(raw) > 2048 {
		return nil, errors.New("subscription endpoint exceeds maximum allowed length of 2048 characters")
	}

	// 1. Strict prefix barrier for SSRF defense (CodeQL CWE-918 StringPrefixCheck)
	if !strings.HasPrefix(raw, "https://fcm.googleapis.com/") &&
		!strings.HasPrefix(raw, "https://web.push.apple.com/") &&
		!strings.HasPrefix(raw, "https://updates.push.services.mozilla.com/") &&
		!strings.HasPrefix(raw, "https://push.services.mozilla.com/") &&
		!strings.HasPrefix(raw, "https://notify.windows.com/") &&
		!strings.HasPrefix(raw, "http://127.0.0.1:") &&
		!strings.HasPrefix(raw, "http://localhost:") {
		return nil, fmt.Errorf("untrusted push service endpoint %q", raw)
	}

	u, err := url.ParseRequestURI(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid subscription endpoint url: %w", err)
	}

	if u.User != nil {
		return nil, errors.New("subscription endpoint cannot contain user credentials")
	}

	return u, nil
}

// Send encrypts and delivers a notification payload to a specific browser push subscription.
func (c *Client) Send(ctx context.Context, sub Subscription, payload NotificationPayload) error {
	if c.vapidKey == nil {
		return errors.New("vapid key not configured on push client")
	}

	endpoint := strings.TrimSpace(sub.Endpoint)
	if endpoint == "" {
		return errors.New("subscription endpoint is empty")
	}

	// 1. Direct SSRF barrier on endpoint (CodeQL go/request-forgery sanitizer)
	if !strings.HasPrefix(endpoint, "https://fcm.googleapis.com/") &&
		!strings.HasPrefix(endpoint, "https://web.push.apple.com/") &&
		!strings.HasPrefix(endpoint, "https://updates.push.services.mozilla.com/") &&
		!strings.HasPrefix(endpoint, "https://push.services.mozilla.com/") &&
		!strings.HasPrefix(endpoint, "https://notify.windows.com/") &&
		!strings.HasPrefix(endpoint, "http://127.0.0.1:") &&
		!strings.HasPrefix(endpoint, "http://localhost:") {
		return fmt.Errorf("untrusted push service endpoint prefix %q", endpoint)
	}

	// 2. Validate endpoint URL structure and extract audience
	u, err := ValidateEndpoint(endpoint)
	if err != nil {
		return fmt.Errorf("validate endpoint: %w", err)
	}
	audience := fmt.Sprintf("%s://%s", u.Scheme, u.Host)

	// 3. Sign VAPID header
	authHeader, err := c.vapidKey.SignHeader(audience, c.subject)
	if err != nil {
		return fmt.Errorf("sign vapid header: %w", err)
	}

	// 4. Decode client public key and auth secret
	clientPub, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(sub.P256dh))
	if err != nil {
		return fmt.Errorf("decode p256dh key: %w", err)
	}
	clientAuth, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(sub.Auth))
	if err != nil {
		return fmt.Errorf("decode auth secret: %w", err)
	}

	// 5. Encrypt JSON payload
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload json: %w", err)
	}

	ciphertext, err := EncryptPayload(payloadBytes, clientPub, clientAuth)
	if err != nil {
		return fmt.Errorf("encrypt web push payload: %w", err)
	}

	// 6. Construct HTTP request to validated push gateway
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(ciphertext))
	if err != nil {
		return fmt.Errorf("create push http request: %w", err)
	}

	req.Header.Set("Authorization", authHeader)
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("TTL", defaultPushTTL)
	req.Header.Set("Urgency", "high")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("dispatch push request: %w", err)
	}
	defer resp.Body.Close()

	// 7. Handle response status
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	// Auto-prune dead/revoked subscriptions (404 Not Found or 410 Gone)
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		c.log.Info("webpush: subscription expired or revoked by user, pruning", "status", resp.StatusCode, "id", sub.ID)
		if c.store != nil && sub.ID != "" {
			_ = c.store.Delete(ctx, sub.ID)
		}
		return fmt.Errorf("subscription expired (status %d)", resp.StatusCode)
	}

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	return fmt.Errorf("push service returned error (status %d): %s", resp.StatusCode, string(body))
}
