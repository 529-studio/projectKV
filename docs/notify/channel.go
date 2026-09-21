// Package notify implements the Notification Adapter described in
// ADR-001 §3.4: a pluggable set of delivery channels (Telegram, Zalo OA,
// Web Push, SMS) fanned out by geographic zone subscription. Mirrors the
// same "pluggable adapter" philosophy already used for the CV Engine, so
// adding a channel later never changes the scheduling/state logic.
package notify

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)

// Alert is what gets delivered to a subscriber. Keep this channel-agnostic;
// each Channel implementation decides how to render it (plain text for
// Telegram/SMS, a ZNS template payload for Zalo, a push payload for WebPush).
type Alert struct {
	Zone       string
	CameraName string
	Severity   string // e.g. "FLOODED_DEEP"
	Message    string // human-readable summary, already localized (vi/en)
}

// Channel is the pluggable delivery adapter interface. Implement one per
// platform. None of these should know about zones/subscriptions — that is
// the Manager's job — a Channel only knows how to send one message to one
// recipient identifier on its platform.
type Channel interface {
	// Name identifies the channel for logging/metrics, e.g. "telegram".
	Name() string
	// Send delivers alert to recipientID (platform-specific: Telegram
	// chat_id, Zalo user_id, a Web Push subscription endpoint, a phone
	// number for SMS). Must respect ctx cancellation.
	Send(ctx context.Context, recipientID string, alert Alert) error
}

// Subscription binds a recipient on a given channel to a zone. Persisted in
// the state store (SQLite) — this in-memory type is just the shape used to
// pass data between the store and the Manager.
type Subscription struct {
	ChannelName string
	RecipientID string
	Zone        string
}

// SubscriptionStore is the minimal persistence contract the Manager needs.
// Implement this against SQLite (see ADR-002: modernc.org/sqlite, pure Go,
// no cgo). Kept as an interface here so notify package has zero DB
// dependency and stays trivially testable.
type SubscriptionStore interface {
	SubscribersForZone(ctx context.Context, zone string) ([]Subscription, error)
	// HasRecentAlert / RecordAlert implement the dedupe requirement from
	// ADR-001 §3.4: do not re-alert the same subscriber every cycle while a
	// flood is ongoing. Typical implementation: a (recipient, zone) row
	// with a "cooldown until" timestamp.
	HasRecentAlert(ctx context.Context, sub Subscription, severity string) (bool, error)
	RecordAlert(ctx context.Context, sub Subscription, severity string) error
}

// Manager fans an Alert out to every subscriber of Alert.Zone, across every
// registered Channel, applying dedupe via the store before sending.
type Manager struct {
	channels map[string]Channel
	store    SubscriptionStore
	log      *slog.Logger
}

func NewManager(store SubscriptionStore, log *slog.Logger) *Manager {
	if log == nil {
		log = slog.Default()
	}
	return &Manager{
		channels: make(map[string]Channel),
		store:    store,
		log:      log,
	}
}

// Register plugs in a Channel implementation. Call once per channel at
// startup, e.g.:
//
//	mgr.Register(notify.NewTelegramChannel(botToken))
func (m *Manager) Register(ch Channel) {
	m.channels[ch.Name()] = ch
}

// Dispatch delivers alert to every subscriber of alert.Zone across all
// registered channels, skipping any recipient whose dedupe cooldown for
// this severity has not elapsed. Delivery is best-effort and concurrent per
// subscriber; a failure on one channel/recipient does not block others.
func (m *Manager) Dispatch(ctx context.Context, alert Alert) error {
	subs, err := m.store.SubscribersForZone(ctx, alert.Zone)
	if err != nil {
		return fmt.Errorf("load subscribers for zone %q: %w", alert.Zone, err)
	}

	var wg sync.WaitGroup
	for _, sub := range subs {
		ch, ok := m.channels[sub.ChannelName]
		if !ok {
			m.log.Warn("no channel registered", "channel", sub.ChannelName)
			continue
		}

		skip, err := m.store.HasRecentAlert(ctx, sub, alert.Severity)
		if err != nil {
			m.log.Warn("dedupe check failed", "error", err)
		} else if skip {
			continue
		}

		sub, ch := sub, ch
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := ch.Send(ctx, sub.RecipientID, alert); err != nil {
				m.log.Warn("send failed", "channel", ch.Name(), "recipient", sub.RecipientID, "error", err)
				return
			}
			if err := m.store.RecordAlert(ctx, sub, alert.Severity); err != nil {
				m.log.Warn("record alert failed", "error", err)
			}
		}()
	}
	wg.Wait()
	return nil
}
