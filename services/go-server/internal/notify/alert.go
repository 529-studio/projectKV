// Package notify implements the real-time notification subsystem for ProjectKV.
// This is a leaf package under services/go-server/internal/ and MUST NOT import
// any other internal/ package. It defines the core notification models, channel
// abstractions, 30-minute anti-spam sliding debouncing, and Telegram delivery.
package notify

import (
	"context"
	"time"
)

// FloodAlert represents a verified flood event payload dispatched to delivery channels.
type FloodAlert struct {
	CameraID       string    `json:"camera_id"`
	CameraName     string    `json:"camera_name"`
	District       string    `json:"district"`
	Ward           string    `json:"ward,omitempty"`
	FloodSeverity  string    `json:"flood_severity"` // "FLOODED_SHALLOW" | "FLOODED_DEEP"
	TypicalDepthCm int       `json:"typical_depth_cm,omitempty"`
	ObservedAt     time.Time `json:"observed_at"`
	SnapshotURL    string    `json:"snapshot_url,omitempty"`
	LiveWebURL     string    `json:"live_web_url,omitempty"`
}

// Channel defines the pluggable delivery adapter contract for notification channels.
// Implementations know how to deliver a FloodAlert to all relevant subscribers of that channel.
type Channel interface {
	// Name identifies the channel (e.g. "telegram", "mock").
	Name() string
	// Send dispatches the alert to all subscribers registered on this channel.
	Send(ctx context.Context, alert FloodAlert) error
}

// SubscriberFinder is the dependency inversion interface for looking up recipient
// identifiers (such as Telegram chat IDs) matching a specific flood alert.
type SubscriberFinder interface {
	GetSubscribersForAlert(ctx context.Context, channelName string, alert FloodAlert) ([]string, error)
}

// SubscriberFinderFunc allows plain functions to satisfy the SubscriberFinder interface.
type SubscriberFinderFunc func(ctx context.Context, channelName string, alert FloodAlert) ([]string, error)

// GetSubscribersForAlert calls the underlying function.
func (f SubscriberFinderFunc) GetSubscribersForAlert(ctx context.Context, channelName string, alert FloodAlert) ([]string, error) {
	return f(ctx, channelName, alert)
}
