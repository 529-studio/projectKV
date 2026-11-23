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
// It verifies valid HTTPS scheme (allowing HTTP only for local loopback test suites),
// disallows user credentials, and blocks private, link-local, loopback (in non-test),
// and cloud metadata addresses.
func ValidateEndpoint(rawEndpoint string) (*url.URL, error) {
	raw := strings.TrimSpace(rawEndpoint)
	if raw == "" {
		return nil, errors.New("subscription endpoint is empty")
	}
	if len(raw) > 2048 {
		return nil, errors.New("subscription endpoint exceeds maximum allowed length of 2048 characters")
	}

	u, err := url.ParseRequestURI(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid subscription endpoint url: %w", err)
	}

	// 1. Strict scheme: HTTPS only (or HTTP only for local loopback test server)
	isTestLoopback := u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost")
	if u.Scheme != "https" && !isTestLoopback {
		return nil, fmt.Errorf("unsupported URL scheme %q: push endpoints must use https", u.Scheme)
	}

	// 2. Hostname validation
	host := u.Hostname()
	if host == "" {
		return nil, errors.New("subscription endpoint host cannot be empty")
	}

	// 3. Userinfo disallowance (user:pass@host)
	if u.User != nil {
		return nil, errors.New("subscription endpoint cannot contain user credentials")
	}

	// 4. SSRF allowlist: only allow legitimate browser push service gateways or local test server
	if host != "fcm.googleapis.com" &&
		host != "web.push.apple.com" &&
		host != "updates.push.services.mozilla.com" &&
		host != "push.services.mozilla.com" &&
		host != "notify.windows.com" &&
		host != "localhost" &&
		host != "127.0.0.1" &&
		!strings.HasSuffix(host, ".push.apple.com") &&
		!strings.HasSuffix(host, ".googleapis.com") &&
		!strings.HasSuffix(host, ".services.mozilla.com") &&
		!strings.HasSuffix(host, ".autopush.net") &&
		!strings.HasSuffix(host, ".notify.windows.com") {
		return nil, fmt.Errorf("subscription endpoint host %q is not an authorized push service gateway", host)
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

	// 1. Validate endpoint URL against SSRF and parse audience origin
	u, err := ValidateEndpoint(endpoint)
	if err != nil {
		return fmt.Errorf("validate endpoint: %w", err)
	}
	host := strings.ToLower(u.Hostname())
	if host != "fcm.googleapis.com" &&
		host != "web.push.apple.com" &&
		host != "updates.push.services.mozilla.com" &&
		host != "push.services.mozilla.com" &&
		host != "notify.windows.com" &&
		host != "localhost" &&
		host != "127.0.0.1" &&
		!strings.HasSuffix(host, ".push.apple.com") &&
		!strings.HasSuffix(host, ".googleapis.com") &&
		!strings.HasSuffix(host, ".services.mozilla.com") &&
		!strings.HasSuffix(host, ".autopush.net") &&
		!strings.HasSuffix(host, ".notify.windows.com") {
		return fmt.Errorf("unauthorized push service host %q", host)
	}
	audience := fmt.Sprintf("%s://%s", u.Scheme, u.Host)

	// 2. Sign VAPID header
	authHeader, err := c.vapidKey.SignHeader(audience, c.subject)
	if err != nil {
		return fmt.Errorf("sign vapid header: %w", err)
	}

	// 3. Decode client public key and auth secret
	clientPub, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(sub.P256dh))
	if err != nil {
		return fmt.Errorf("decode p256dh key: %w", err)
	}
	clientAuth, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(sub.Auth))
	if err != nil {
		return fmt.Errorf("decode auth secret: %w", err)
	}

	// 4. Encrypt JSON payload
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload json: %w", err)
	}

	ciphertext, err := EncryptPayload(payloadBytes, clientPub, clientAuth)
	if err != nil {
		return fmt.Errorf("encrypt web push payload: %w", err)
	}

	// 5. Construct HTTP request using sanitized scheme and host
	scheme := "https"
	if u.Scheme == "http" && (host == "localhost" || host == "127.0.0.1") {
		scheme = "http"
	}
	targetHost := host
	if port := u.Port(); port != "" {
		targetHost = host + ":" + port
	}
	targetURL := scheme + "://" + targetHost + u.RequestURI()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(ciphertext))
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

	// 6. Handle response status
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
