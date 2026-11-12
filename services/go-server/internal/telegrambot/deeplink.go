// Package telegrambot implements the inbound Telegram surface for the Warded
// alert bot: the deep-link payload codec, the bilingual command renderers and
// the webhook pipeline (authentication, dedupe, queue, rate limiting).
//
// This is a leaf package under services/go-server/internal/: it MUST NOT import
// any other internal/ package (spec §4.1, docs/GO_BACKEND_ARCHITECTURE.md).
// Persistence, delivery and taxonomy knowledge arrive through the Store,
// Sender and Taxonomy interfaces defined in handler.go (DIP).
package telegrambot

import (
	"encoding/base64"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	// MaxStartPayloadLen is Telegram's hard limit for a `start` deep-link payload.
	MaxStartPayloadLen = 64
	// PayloadPrefix separates the Telegram `start` argument from the target.
	PayloadPrefix = "sub_"

	targetTypeAll        = "all"
	targetTypeZone       = "zone"
	targetTypeWard       = "ward"
	targetTypeChokepoint = "chokepoint"

	zonePrefix = "zone_"
	wardPrefix = "ward_"
	camPrefix  = "cam_"
)

// Sentinel errors for deep-link payload handling.
var (
	// ErrPayloadTooLong reports a payload above MaxStartPayloadLen.
	ErrPayloadTooLong = errors.New("telegram start payload exceeds 64 characters")
	// ErrPayloadMalformed reports a payload that cannot be decoded.
	ErrPayloadMalformed = errors.New("telegram start payload is malformed")
	// ErrPayloadUnknown reports a well-formed payload with an unknown target type.
	ErrPayloadUnknown = errors.New("telegram start payload has an unknown target type")
)

// camIDPattern is the normative camera/chokepoint id grammar (spec §2.1).
var camIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,40}$`)

// Target is the decoded subscription scope carried by a deep link.
type Target struct {
	Type string
	ID   string
}

// SlugEncode encodes an identifier as unpadded base64url over its UTF-8 bytes
// (RFC 4648 §5). Callers must pass valid UTF-8; the taxonomy always does.
func SlugEncode(id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(id))
}

// SlugDecode decodes an unpadded base64url slug back to a UTF-8 identifier.
// Padding, non-alphabet characters, non-zero trailing bits and invalid UTF-8
// are all rejected so that one identifier has exactly one wire form.
func SlugDecode(slug string) (string, error) {
	if slug == "" {
		return "", ErrPayloadMalformed
	}
	if strings.ContainsRune(slug, '=') || strings.ContainsAny(slug, "\r\n") {
		return "", ErrPayloadMalformed
	}
	// base64 length 4k+1 is impossible for unpadded output.
	if len(slug)%4 == 1 {
		return "", ErrPayloadMalformed
	}
	for i := 0; i < len(slug); i++ {
		c := slug[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return "", ErrPayloadMalformed
		}
	}
	// Strict decoding rejects non-zero trailing bits ("trailing-bit variance").
	raw, err := base64.RawURLEncoding.Strict().DecodeString(slug)
	if err != nil {
		return "", ErrPayloadMalformed
	}
	if len(raw) == 0 || !utf8.Valid(raw) {
		return "", ErrPayloadMalformed
	}
	return string(raw), nil
}

// EncodePayload renders the `start` payload for a target, enforcing the 64-char
// Telegram bound (spec §2.1, E1).
func EncodePayload(t Target) (string, error) {
	var payload string
	switch t.Type {
	case targetTypeAll:
		payload = PayloadPrefix + "all"
	case targetTypeZone:
		if strings.TrimSpace(t.ID) == "" {
			return "", ErrPayloadMalformed
		}
		payload = PayloadPrefix + zonePrefix + SlugEncode(t.ID)
	case targetTypeWard:
		if strings.TrimSpace(t.ID) == "" {
			return "", ErrPayloadMalformed
		}
		payload = PayloadPrefix + wardPrefix + SlugEncode(t.ID)
	case targetTypeChokepoint:
		if !camIDPattern.MatchString(t.ID) {
			return "", ErrPayloadMalformed
		}
		payload = PayloadPrefix + camPrefix + t.ID
	default:
		return "", ErrPayloadUnknown
	}
	if len(payload) > MaxStartPayloadLen {
		return "", ErrPayloadTooLong
	}
	return payload, nil
}

// DecodePayload parses a `start` payload into a Target. The "sub_" and target
// type prefixes are matched case-insensitively; the slug itself is strict.
// The returned target is NOT taxonomy-validated: the handler does that against
// the live catalog before touching the database.
func DecodePayload(payload string) (Target, error) {
	p := strings.TrimSpace(payload)
	if p == "" {
		return Target{}, ErrPayloadMalformed
	}
	if len(p) > MaxStartPayloadLen {
		return Target{}, ErrPayloadTooLong
	}
	if !strings.HasPrefix(strings.ToLower(p), PayloadPrefix) {
		return Target{}, ErrPayloadMalformed
	}
	rest := p[len(PayloadPrefix):]
	if rest == "" {
		return Target{}, ErrPayloadMalformed
	}
	lower := strings.ToLower(rest)

	switch {
	case lower == "all":
		return Target{Type: targetTypeAll}, nil
	case strings.HasPrefix(lower, zonePrefix):
		id, err := SlugDecode(rest[len(zonePrefix):])
		if err != nil {
			return Target{}, err
		}
		return Target{Type: targetTypeZone, ID: id}, nil
	case strings.HasPrefix(lower, wardPrefix):
		id, err := SlugDecode(rest[len(wardPrefix):])
		if err != nil {
			return Target{}, err
		}
		return Target{Type: targetTypeWard, ID: id}, nil
	case strings.HasPrefix(lower, camPrefix):
		id := rest[len(camPrefix):]
		if !camIDPattern.MatchString(id) {
			return Target{}, ErrPayloadMalformed
		}
		return Target{Type: targetTypeChokepoint, ID: id}, nil
	default:
		return Target{}, ErrPayloadUnknown
	}
}
