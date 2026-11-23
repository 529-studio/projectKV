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
		client:   &http.Client{Timeout: defaultPushTimeout},
		log:      slog.Default(),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
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

	// 1. Parse audience origin from endpoint
	u, err := url.Parse(endpoint)
	if err != nil {
		return fmt.Errorf("invalid subscription endpoint url: %w", err)
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

	// 5. Construct HTTP request
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
