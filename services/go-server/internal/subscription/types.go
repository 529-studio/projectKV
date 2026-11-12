// Package subscription provides persistence and management of user subscriptions
// for flood alerts across multiple scopes (citywide, chokepoints, zones, wards).
// This is a leaf package under services/go-server/internal/ and MUST NOT import
// any other internal/ package.
package subscription

import (
	"errors"
	"strings"
	"time"
)

// TargetType defines the granularity of a subscription.
type TargetType string

const (
	TargetAll        TargetType = "all"        // City-wide alerts
	TargetChokepoint TargetType = "chokepoint" // Specific camera / chokepoint ID
	TargetZone       TargetType = "zone"       // Operational traffic zone (e.g. "Quận 7 - Nhà Bè")
	TargetWard       TargetType = "ward"       // Administrative ward / Phường
)

// CanonicalTargetTypes defines the complete, fixed set of valid target types.
var CanonicalTargetTypes = []string{
	string(TargetAll),
	string(TargetChokepoint),
	string(TargetZone),
	string(TargetWard),
}

// CanonicalZones contains the authoritative traffic zone classifications.
var CanonicalZones = []string{
	"Bà Rịa - Vũng Tàu",
	"Bình Dương",
	"Cụm Cảng Cát Lái",
	"Quận 7 - Nhà Bè",
	"TP. Thủ Đức",
	"Bình Tân - Bình Chánh",
	"Q.12 - Hóc Môn - Củ Chi",
	"Khu vực Nội thành TP.HCM",
}

// LegacyZoneAliases maps legacy administrative district and alias names to their canonical operational zone.
var LegacyZoneAliases = map[string]string{
	// South Saigon (Quận 7 - Nhà Bè)
	"quận 7":          "Quận 7 - Nhà Bè",
	"q7":              "Quận 7 - Nhà Bè",
	"q.7":             "Quận 7 - Nhà Bè",
	"nhà bè":          "Quận 7 - Nhà Bè",
	"huyện nhà bè":    "Quận 7 - Nhà Bè",
	"quận 7 - nhà bè": "Quận 7 - Nhà Bè",

	// East Saigon / Thu Duc (TP. Thủ Đức)
	"thủ đức":           "TP. Thủ Đức",
	"tp. thủ đức":       "TP. Thủ Đức",
	"tp thủ đức":        "TP. Thủ Đức",
	"thành phố thủ đức": "TP. Thủ Đức",
	"quận 2":            "TP. Thủ Đức",
	"q2":                "TP. Thủ Đức",
	"q.2":               "TP. Thủ Đức",
	"quận 9":            "TP. Thủ Đức",
	"q9":                "TP. Thủ Đức",
	"q.9":               "TP. Thủ Đức",

	// West Saigon (Bình Tân - Bình Chánh)
	"bình tân":              "Bình Tân - Bình Chánh",
	"quận bình tân":         "Bình Tân - Bình Chánh",
	"bình chánh":            "Bình Tân - Bình Chánh",
	"huyện bình chánh":      "Bình Tân - Bình Chánh",
	"bình tân - bình chánh": "Bình Tân - Bình Chánh",

	// Northwest Gateway (Q.12 - Hóc Môn - Củ Chi)
	"quận 12":                    "Q.12 - Hóc Môn - Củ Chi",
	"q12":                        "Q.12 - Hóc Môn - Củ Chi",
	"q.12":                       "Q.12 - Hóc Môn - Củ Chi",
	"hóc môn":                    "Q.12 - Hóc Môn - Củ Chi",
	"huyện hóc môn":              "Q.12 - Hóc Môn - Củ Chi",
	"củ chi":                     "Q.12 - Hóc Môn - Củ Chi",
	"huyện củ chi":               "Q.12 - Hóc Môn - Củ Chi",
	"q.12 - hóc môn - củ chi":    "Q.12 - Hóc Môn - Củ Chi",
	"quận 12 - hóc môn - củ chi": "Q.12 - Hóc Môn - Củ Chi",

	// Cat Lai Port
	"cát lái":          "Cụm Cảng Cát Lái",
	"cảng cát lái":     "Cụm Cảng Cát Lái",
	"cụm cảng cát lái": "Cụm Cảng Cát Lái",

	// Binh Duong
	"bình dương":      "Bình Dương",
	"tỉnh bình dương": "Bình Dương",

	// Ba Ria - Vung Tau
	"bà rịa - vũng tàu":      "Bà Rịa - Vũng Tàu",
	"vũng tàu":               "Bà Rịa - Vũng Tàu",
	"bà rịa":                 "Bà Rịa - Vũng Tàu",
	"tỉnh bà rịa - vũng tàu": "Bà Rịa - Vũng Tàu",

	// Inner City (Khu vực Nội thành TP.HCM)
	"quận 1":                   "Khu vực Nội thành TP.HCM",
	"q1":                       "Khu vực Nội thành TP.HCM",
	"q.1":                      "Khu vực Nội thành TP.HCM",
	"quận 3":                   "Khu vực Nội thành TP.HCM",
	"q3":                       "Khu vực Nội thành TP.HCM",
	"q.3":                      "Khu vực Nội thành TP.HCM",
	"quận 4":                   "Khu vực Nội thành TP.HCM",
	"q4":                       "Khu vực Nội thành TP.HCM",
	"q.4":                      "Khu vực Nội thành TP.HCM",
	"quận 5":                   "Khu vực Nội thành TP.HCM",
	"q5":                       "Khu vực Nội thành TP.HCM",
	"q.5":                      "Khu vực Nội thành TP.HCM",
	"quận 6":                   "Khu vực Nội thành TP.HCM",
	"q6":                       "Khu vực Nội thành TP.HCM",
	"q.6":                      "Khu vực Nội thành TP.HCM",
	"quận 8":                   "Khu vực Nội thành TP.HCM",
	"q8":                       "Khu vực Nội thành TP.HCM",
	"q.8":                      "Khu vực Nội thành TP.HCM",
	"quận 10":                  "Khu vực Nội thành TP.HCM",
	"q10":                      "Khu vực Nội thành TP.HCM",
	"q.10":                     "Khu vực Nội thành TP.HCM",
	"quận 11":                  "Khu vực Nội thành TP.HCM",
	"q11":                      "Khu vực Nội thành TP.HCM",
	"q.11":                     "Khu vực Nội thành TP.HCM",
	"bình thạnh":               "Khu vực Nội thành TP.HCM",
	"quận bình thạnh":          "Khu vực Nội thành TP.HCM",
	"phú nhuận":                "Khu vực Nội thành TP.HCM",
	"quận phú nhuận":           "Khu vực Nội thành TP.HCM",
	"tân bình":                 "Khu vực Nội thành TP.HCM",
	"quận tân bình":            "Khu vực Nội thành TP.HCM",
	"tân phú":                  "Khu vực Nội thành TP.HCM",
	"quận tân phú":             "Khu vực Nội thành TP.HCM",
	"gò vấp":                   "Khu vực Nội thành TP.HCM",
	"quận gò vấp":              "Khu vực Nội thành TP.HCM",
	"nội thành":                "Khu vực Nội thành TP.HCM",
	"khu vực nội thành":        "Khu vực Nội thành TP.HCM",
	"khu vực nội thành tp.hcm": "Khu vực Nội thành TP.HCM",
	"nội thành tp.hcm":         "Khu vực Nội thành TP.HCM",
}

// NormalizeZone maps an input string (canonical zone name or legacy district alias) to its canonical zone.
// Returns the canonical zone and true if matched, or the original string and false if unrecognized.
func NormalizeZone(input string) (string, bool) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return "", false
	}
	// Direct canonical match
	for _, z := range CanonicalZones {
		if strings.EqualFold(trimmed, z) {
			return z, true
		}
	}
	// Alias lookup
	lower := strings.ToLower(trimmed)
	if canonical, ok := LegacyZoneAliases[lower]; ok {
		return canonical, true
	}
	return trimmed, false
}

// Sentinel errors for subscription operations
var (
	ErrInvalidChatID     = errors.New("chat_id is required")
	ErrInvalidChannel    = errors.New("channel_name is required")
	ErrInvalidTargetType = errors.New("invalid target_type: must be all, chokepoint, zone, or ward")
	ErrMissingTargetID   = errors.New("target_id is required for chokepoint, zone, and ward target types")
	ErrInvalidTargetID   = errors.New("invalid target_id: target does not exist in canonical taxonomy")
)

// WardInfo carries structured ward details for taxonomy introspection.
type WardInfo struct {
	Type string `json:"type,omitempty"` // "Phường", "Xã", "Thị trấn"
	Name string `json:"name"`           // e.g. "Bến Nghé", "Thảo Điền"
}

// ChokepointInfo carries camera / flood chokepoint information for taxonomy introspection.
type ChokepointInfo struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// TaxonomyResponse represents the payload returned by GET /api/subscriptions/taxonomy.
type TaxonomyResponse struct {
	TargetTypes []string         `json:"target_types"`
	Zones       []string         `json:"zones"`
	Wards       []WardInfo       `json:"wards"`
	Chokepoints []ChokepointInfo `json:"chokepoints"`
	// BotUsername is the public Telegram handle the web UI turns into a deep
	// link. Empty when the bot is unprovisioned; the UI then disables the CTA
	// and keeps the manual chat-id form usable (S12-T4 spec §4.3, AC4).
	BotUsername string `json:"bot_username,omitempty"`
	// DeepLinkEnabled reports whether the server considers the bot fully
	// provisioned (token, username and webhook secret all present and valid).
	DeepLinkEnabled bool `json:"deep_link_enabled"`
}

// TaxonomyProvider defines the interface for verifying and introspecting canonical taxonomy targets.
// (Dependency Inversion Principle: consumer package defines the interface, composition root adapts the catalog).
type TaxonomyProvider interface {
	Zones() []string
	Wards() []WardInfo
	Chokepoints() []ChokepointInfo
	HasZone(zone string) bool
	HasWard(ward string) bool
	HasChokepoint(id string) bool
}

// Subscription represents a persistent alert registration for a user on a delivery channel.
type Subscription struct {
	ID          int64      `json:"id,omitempty"`
	ChannelName string     `json:"channel_name"` // "telegram"
	ChatID      string     `json:"chat_id"`      // Telegram chat ID or recipient identifier
	TargetType  TargetType `json:"target_type"`  // "all" | "chokepoint" | "zone" | "ward"
	TargetID    string     `json:"target_id"`    // Camera ID, Canonical Zone name, Ward name, or empty for "all"
	CreatedAt   time.Time  `json:"created_at,omitempty"`
}

// IsValid validates subscription fields and normalizes zone targets.
func (s *Subscription) IsValid() error {
	s.ChatID = strings.TrimSpace(s.ChatID)
	if s.ChatID == "" {
		return ErrInvalidChatID
	}
	s.ChannelName = strings.TrimSpace(s.ChannelName)
	if s.ChannelName == "" {
		return ErrInvalidChannel
	}
	s.TargetID = strings.TrimSpace(s.TargetID)

	switch s.TargetType {
	case TargetAll:
		s.TargetID = ""
		return nil
	case TargetChokepoint, TargetWard:
		if s.TargetID == "" {
			return ErrMissingTargetID
		}
		return nil
	case TargetZone:
		if s.TargetID == "" {
			return ErrMissingTargetID
		}
		canonical, ok := NormalizeZone(s.TargetID)
		if !ok {
			return ErrInvalidTargetID
		}
		s.TargetID = canonical
		return nil
	default:
		return ErrInvalidTargetType
	}
}
