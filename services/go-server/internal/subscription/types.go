// Package subscription provides persistence and management of user subscriptions
// for flood alerts across multiple scopes (citywide, chokepoints, zones, wards).
// This is a leaf package under services/go-server/internal/ and MUST NOT import
// any other internal/ package.
package subscription

import (
	"errors"
	"time"
)

// TargetType defines the granularity of a subscription.
type TargetType string

const (
	TargetAll        TargetType = "all"        // City-wide alerts
	TargetChokepoint TargetType = "chokepoint" // Specific camera / chokepoint ID
	TargetZone       TargetType = "zone"       // District / Quận / Huyện
	TargetWard       TargetType = "ward"       // Ward / Phường / Xã
)

// Sentinel errors for subscription operations
var (
	ErrInvalidChatID     = errors.New("chat_id is required")
	ErrInvalidChannel    = errors.New("channel_name is required")
	ErrInvalidTargetType = errors.New("invalid target_type (must be all, chokepoint, zone, or ward)")
)

// Subscription represents a persistent alert registration for a user on a delivery channel.
type Subscription struct {
	ID          int64      `json:"id,omitempty"`
	ChannelName string     `json:"channel_name"` // "telegram"
	ChatID      string     `json:"chat_id"`      // Telegram chat ID or recipient identifier
	TargetType  TargetType `json:"target_type"`  // "all" | "chokepoint" | "zone" | "ward"
	TargetID    string     `json:"target_id"`    // Camera ID, District name, Ward name, or empty for "all"
	CreatedAt   time.Time  `json:"created_at,omitempty"`
}

// IsValid validates subscription fields.
func (s *Subscription) IsValid() error {
	if s.ChatID == "" {
		return ErrInvalidChatID
	}
	if s.ChannelName == "" {
		return ErrInvalidChannel
	}
	switch s.TargetType {
	case TargetAll, TargetChokepoint, TargetZone, TargetWard:
		return nil
	default:
		return ErrInvalidTargetType
	}
}
