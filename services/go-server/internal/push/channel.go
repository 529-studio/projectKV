package push

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/529-studio/projectKV/services/go-server/internal/notify"
)

// WebPushChannel implements notify.Channel and corridor hazard delivery for browser Web Push subscribers.
// Strictly zero emojis per Rule 1.
type WebPushChannel struct {
	client         *Client
	store          Store
	finder         notify.SubscriberFinder
	corridorFinder notify.CorridorSubscriberFinder
	log            *slog.Logger
}

// ChannelOption configures WebPushChannel.
type ChannelOption func(*WebPushChannel)

// WithCorridorSubscriberFinder configures corridor subscriber lookup.
func WithCorridorSubscriberFinder(finder notify.CorridorSubscriberFinder) ChannelOption {
	return func(c *WebPushChannel) {
		if finder != nil {
			c.corridorFinder = finder
		}
	}
}

// WithChannelLogger configures the logger.
func WithChannelLogger(log *slog.Logger) ChannelOption {
	return func(c *WebPushChannel) {
		if log != nil {
			c.log = log
		}
	}
}

// NewWebPushChannel initializes a new WebPushChannel.
func NewWebPushChannel(client *Client, store Store, finder notify.SubscriberFinder, opts ...ChannelOption) *WebPushChannel {
	c := &WebPushChannel{
		client: client,
		store:  store,
		finder: finder,
		log:    slog.Default(),
	}
	if cf, ok := finder.(notify.CorridorSubscriberFinder); ok {
		c.corridorFinder = cf
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Name returns the channel identifier matching database records.
func (c *WebPushChannel) Name() string {
	return "webpush"
}

// Send dispatches a FloodAlert to all browser push subscribers matching the alert scope.
func (c *WebPushChannel) Send(ctx context.Context, alert notify.FloodAlert) error {
	if c.client == nil || c.store == nil || c.finder == nil {
		c.log.Debug("webpush: channel not fully configured, skipping alert delivery")
		return nil
	}

	subIDs, err := c.finder.GetSubscribersForAlert(ctx, c.Name(), alert)
	if err != nil {
		return fmt.Errorf("webpush: lookup subscribers for alert %s: %w", alert.CameraID, err)
	}

	if len(subIDs) == 0 {
		return nil
	}

	subs, err := c.store.GetByIDs(ctx, subIDs)
	if err != nil {
		return fmt.Errorf("webpush: fetch subscriptions by ids: %w", err)
	}

	title := "CẢNH BÁO NGẬP NƯỚC / FLOOD ALERT"
	camName := alert.CameraName
	if camName == "" {
		camName = alert.CameraID
	}
	body := fmt.Sprintf("%s: %s", camName, alert.FloodSeverity)
	if alert.TypicalDepthCm > 0 {
		body += fmt.Sprintf(" (~%d cm)", alert.TypicalDepthCm)
	}

	payload := NotificationPayload{
		Title:    title,
		Body:     body,
		URL:      fmt.Sprintf("/?cam=%s", alert.CameraID),
		Severity: alert.FloodSeverity,
		Tag:      fmt.Sprintf("flood-%s", alert.CameraID),
	}

	return c.dispatchConcurrently(ctx, subs, payload)
}

// SendCorridor dispatches a CorridorAlert to all browser push subscribers registered for the arterial corridor.
func (c *WebPushChannel) SendCorridor(ctx context.Context, alert notify.CorridorAlert) error {
	if c.client == nil || c.store == nil || c.corridorFinder == nil {
		c.log.Debug("webpush: corridor channel not fully configured, skipping delivery")
		return nil
	}

	subIDs, err := c.corridorFinder.GetSubscribersForCorridorAlert(ctx, c.Name(), alert)
	if err != nil {
		return fmt.Errorf("webpush: lookup corridor subscribers for %s: %w", alert.RoadKey, err)
	}

	if len(subIDs) == 0 {
		return nil
	}

	subs, err := c.store.GetByIDs(ctx, subIDs)
	if err != nil {
		return fmt.Errorf("webpush: fetch corridor subscriptions: %w", err)
	}

	roadName := alert.RoadName
	if roadName == "" {
		roadName = alert.RoadKey
	}
	title := fmt.Sprintf("CẢNH BÁO TUYẾN ĐƯỜNG: %s", roadName)
	body := fmt.Sprintf("Trạng thái lưu thông: %s", alert.Passability)
	if alert.MaxDepthCm != nil && *alert.MaxDepthCm > 0 {
		body += fmt.Sprintf(" - Ngập đỉnh: %.0f cm", *alert.MaxDepthCm)
	}
	if alert.EstimatedDelayMin > 0 {
		body += fmt.Sprintf(" (+%d phút)", alert.EstimatedDelayMin)
	}

	payload := NotificationPayload{
		Title:    title,
		Body:     body,
		URL:      fmt.Sprintf("/roads/%s", alert.RoadKey),
		RoadKey:  alert.RoadKey,
		Severity: alert.Passability,
		Tag:      fmt.Sprintf("corridor-%s", alert.RoadKey),
	}

	return c.dispatchConcurrently(ctx, subs, payload)
}

func (c *WebPushChannel) dispatchConcurrently(ctx context.Context, subs []Subscription, payload NotificationPayload) error {
	// Bound concurrency to max 10 workers
	sem := make(chan struct{}, 10)
	var wg sync.WaitGroup
	var errMu sync.Mutex
	var errs []error

	for _, sub := range subs {
		wg.Add(1)
		go func(s Subscription) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			if err := c.client.Send(ctx, s, payload); err != nil {
				c.log.Warn("webpush: delivery to subscription failed", "id", s.ID, "error", err)
				errMu.Lock()
				errs = append(errs, err)
				errMu.Unlock()
			}
		}(sub)
	}

	wg.Wait()
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}
