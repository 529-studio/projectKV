package telegrambot

import (
	"errors"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/529-studio/projectKV/services/go-server/internal/subscription"
)

// Shared Go/TS conformance vectors (spec §2.1). apps/web/src/utils/subscriptionUtils.test.ts
// pins the exact same literals so the two implementations cannot drift.
const (
	slugVectorBenNghe      = "QuG6v24gTmdow6k"
	slugVectorBinhDuong    = "QsOsbmggRMawxqFuZw"
	slugVectorBaRiaVungTau = "QsOgIFLhu4thIC0gVsWpbmcgVMOgdQ"
)

// sampleWards mirrors the live ward taxonomy (catalog.Phuongs) closely enough to
// cover every diacritic class and the longest real names; the taxonomy is read
// from SQLite at runtime, so a fixed sample is the honest unit-test fixture.
var sampleWards = []string{
	"Bến Nghé",
	"Đông Hưng Thuận",
	"Thạnh Mỹ Tây",
	"Thị trấn Cần Thạnh",
	"Thị trấn Củ Chi",
	"Xã Bình Lợi",
	"Tân Nhựt",
	"Phường 25",
	"An Phú Đông",
}

// sampleChokepoints covers the 24-hex Mongo camera ids used by the catalog plus
// the short synthetic ids used by tests and fixtures.
var sampleChokepoints = []string{
	"591c6d3ba6bfb500119400ac",
	"662b86c41afb9c00172dd31c",
	"cam-vvk-01",
	"A_b-9",
}

func TestSlugConformanceVectors(t *testing.T) {
	vectors := []struct {
		name string
		slug string
	}{
		{"Bến Nghé", slugVectorBenNghe},
		{"Bình Dương", slugVectorBinhDuong},
		{"Bà Rịa - Vũng Tàu", slugVectorBaRiaVungTau},
	}

	for _, v := range vectors {
		if got := SlugEncode(v.name); got != v.slug {
			t.Errorf("SlugEncode(%q) = %q, want %q", v.name, got, v.slug)
		}
		if got, err := SlugDecode(v.slug); err != nil || got != v.name {
			t.Errorf("SlugDecode(%q) = (%q, %v), want (%q, nil)", v.slug, got, err, v.name)
		}
	}
}

func TestEncodePayloadConformanceVectors(t *testing.T) {
	vectors := []struct {
		name    string
		target  Target
		payload string
	}{
		{"city-wide", Target{Type: targetTypeAll}, "sub_all"},
		{"ward Bến Nghé", Target{Type: targetTypeWard, ID: "Bến Nghé"}, "sub_ward_" + slugVectorBenNghe},
		{"zone Bình Dương", Target{Type: targetTypeZone, ID: "Bình Dương"}, "sub_zone_" + slugVectorBinhDuong},
		{"zone Bà Rịa - Vũng Tàu", Target{Type: targetTypeZone, ID: "Bà Rịa - Vũng Tàu"}, "sub_zone_" + slugVectorBaRiaVungTau},
		{"camera", Target{Type: targetTypeChokepoint, ID: "591c6d3ba6bfb500119400ac"}, "sub_cam_591c6d3ba6bfb500119400ac"},
	}

	for _, v := range vectors {
		got, err := EncodePayload(v.target)
		if err != nil {
			t.Fatalf("%s: EncodePayload returned error: %v", v.name, err)
		}
		if got != v.payload {
			t.Errorf("%s: EncodePayload = %q, want %q", v.name, got, v.payload)
		}
		if len(got) > MaxStartPayloadLen {
			t.Errorf("%s: payload length %d exceeds %d", v.name, len(got), MaxStartPayloadLen)
		}
	}
}

// TestPayloadLengthBoundOverTaxonomy is the whole-taxonomy 64-char gate of
// spec §2.1: every canonical zone, a representative ward sample and a camera
// sample must encode inside Telegram's limit and decode back unchanged.
func TestPayloadLengthBoundOverTaxonomy(t *testing.T) {
	type sample struct {
		kind  string
		value string
	}
	var samples []sample
	for _, zone := range subscription.CanonicalZones {
		samples = append(samples, sample{targetTypeZone, zone})
	}
	for _, ward := range sampleWards {
		samples = append(samples, sample{targetTypeWard, ward})
	}
	for _, cam := range sampleChokepoints {
		samples = append(samples, sample{targetTypeChokepoint, cam})
	}

	maxLen, maxValue := 0, ""
	for _, s := range samples {
		target := Target{Type: s.kind, ID: s.value}
		payload, err := EncodePayload(target)
		if err != nil {
			t.Fatalf("%s %q: EncodePayload failed: %v", s.kind, s.value, err)
		}
		if len(payload) > MaxStartPayloadLen {
			t.Errorf("%s %q: payload %q is %d chars, exceeds %d", s.kind, s.value, payload, len(payload), MaxStartPayloadLen)
		}
		if len(payload) > maxLen {
			maxLen, maxValue = len(payload), s.value
		}
		decoded, err := DecodePayload(payload)
		if err != nil {
			t.Fatalf("%s %q: DecodePayload(%q) failed: %v", s.kind, s.value, payload, err)
		}
		if decoded.Type != s.kind || decoded.ID != s.value {
			t.Errorf("%s %q: round trip produced %+v", s.kind, s.value, decoded)
		}
	}

	t.Logf("longest payload: %d chars for %q (headroom %d)", maxLen, maxValue, MaxStartPayloadLen-maxLen)
	if maxLen > MaxStartPayloadLen {
		t.Fatalf("longest payload %d exceeds the bound", maxLen)
	}
}

// TestPayloadWorstCaseZone pins the longest real zone payload. Note that the
// spec §2.1 parenthetical claims 44 chars; the measured length is 48
// (9 + 39), which is what this test enforces — still well inside the bound.
func TestPayloadWorstCaseZone(t *testing.T) {
	const want = "sub_zone_S2h1IHbhu7FjIE7hu5lpIHRow6BuaCBUUC5IQ00"
	got, err := EncodePayload(Target{Type: targetTypeZone, ID: "Khu vực Nội thành TP.HCM"})
	if err != nil {
		t.Fatalf("EncodePayload failed: %v", err)
	}
	if got != want {
		t.Fatalf("EncodePayload = %q, want %q", got, want)
	}
	if len(got) != 48 {
		t.Fatalf("worst-case zone payload length = %d, want 48", len(got))
	}
}

// TestPayloadStructuralBound proves the encoder refuses inputs that would cross
// 64 chars instead of silently truncating (spec E1).
func TestPayloadStructuralBound(t *testing.T) {
	// 41 UTF-8 bytes -> 55 base64url chars -> 9 + 55 = 64 exactly.
	atLimit := strings.Repeat("a", 41)
	payload, err := EncodePayload(Target{Type: targetTypeZone, ID: atLimit})
	if err != nil {
		t.Fatalf("41-byte id should fit exactly, got error: %v", err)
	}
	if len(payload) != MaxStartPayloadLen {
		t.Fatalf("41-byte id payload length = %d, want %d", len(payload), MaxStartPayloadLen)
	}

	overLimit := strings.Repeat("a", 42) // 56 chars -> 65 total
	if _, err := EncodePayload(Target{Type: targetTypeZone, ID: overLimit}); !errors.Is(err, ErrPayloadTooLong) {
		t.Fatalf("expected ErrPayloadTooLong for a 65-char payload, got %v", err)
	}
	if _, err := EncodePayload(Target{Type: targetTypeWard, ID: overLimit}); !errors.Is(err, ErrPayloadTooLong) {
		t.Fatalf("expected ErrPayloadTooLong for a 65-char ward payload, got %v", err)
	}
	// A camera id is bounded to 40 chars by its own grammar, so 8 + 40 = 48.
	longCam := strings.Repeat("c", 40)
	if _, err := EncodePayload(Target{Type: targetTypeChokepoint, ID: longCam}); err != nil {
		t.Fatalf("40-char camera id must be valid: %v", err)
	}
	if _, err := EncodePayload(Target{Type: targetTypeChokepoint, ID: longCam + "c"}); !errors.Is(err, ErrPayloadMalformed) {
		t.Fatalf("41-char camera id must be rejected as malformed, got %v", err)
	}
}

func TestDecodePayloadAcceptsPrefixCaseInsensitively(t *testing.T) {
	vectors := []struct {
		payload string
		want    Target
	}{
		{"sub_all", Target{Type: targetTypeAll}},
		{"SUB_ALL", Target{Type: targetTypeAll}},
		{"Sub_All", Target{Type: targetTypeAll}},
		{"SUB_ZONE_" + slugVectorBinhDuong, Target{Type: targetTypeZone, ID: "Bình Dương"}},
		{"sub_WARD_" + slugVectorBenNghe, Target{Type: targetTypeWard, ID: "Bến Nghé"}},
		{"sub_CAM_591c6d3ba6bfb500119400ac", Target{Type: targetTypeChokepoint, ID: "591c6d3ba6bfb500119400ac"}},
		{"  sub_all  ", Target{Type: targetTypeAll}},
	}
	for _, v := range vectors {
		got, err := DecodePayload(v.payload)
		if err != nil {
			t.Fatalf("DecodePayload(%q) failed: %v", v.payload, err)
		}
		if got != v.want {
			t.Errorf("DecodePayload(%q) = %+v, want %+v", v.payload, got, v.want)
		}
	}
}

func TestDecodePayloadRejectsMalformed(t *testing.T) {
	// 65 chars: valid grammar, over the Telegram limit.
	oversize := "sub_zone_" + strings.Repeat("A", 56)

	cases := []struct {
		name    string
		payload string
		want    error
	}{
		{"empty", "", ErrPayloadMalformed},
		{"whitespace only", "   ", ErrPayloadMalformed},
		{"missing sub_ prefix", "zone_QsOsbmggRMawxqFuZw", ErrPayloadMalformed},
		{"descriptive url instead of payload", "start=sub_all", ErrPayloadMalformed},
		{"prefix only", "sub_", ErrPayloadMalformed},
		{"plain-text target (E2)", "sub_zone_quận 7", ErrPayloadMalformed},
		{"missing slug", "sub_zone_", ErrPayloadMalformed},
		{"padded base64", "sub_zone_QsOsbmggRMawxqFuZw=", ErrPayloadMalformed},
		{"impossible base64 length", "sub_zone_QsOsbmggRMawxqFuZ", ErrPayloadMalformed},
		{"trailing-bit variance", "sub_zone_QsOsbmggRMawxqFuZx", ErrPayloadMalformed},
		{"non-alphabet characters", "sub_zone_QsOsbmggRMawxqFuZ!", ErrPayloadMalformed},
		{"standard base64 alphabet", "sub_zone_//8", ErrPayloadMalformed},
		{"invalid utf-8", "sub_zone__w", ErrPayloadMalformed},
		{"newline inside slug", "sub_zone_QsOsbmgg\nRMawxqFuZw", ErrPayloadMalformed},
		{"empty camera id", "sub_cam_", ErrPayloadMalformed},
		{"camera id with space (E5 style)", "sub_cam_bad id", ErrPayloadMalformed},
		{"camera id over 40 chars", "sub_cam_" + strings.Repeat("a", 41), ErrPayloadMalformed},
		{"unknown target type", "sub_unknown_abc", ErrPayloadUnknown},
		{"unknown target type with slug", "sub_district_Z2h1", ErrPayloadUnknown},
		{"oversize payload", oversize, ErrPayloadTooLong},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DecodePayload(tc.payload)
			if err == nil {
				t.Fatalf("DecodePayload(%q) = %+v, want error", tc.payload, got)
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("DecodePayload(%q) error = %v, want %v", tc.payload, err, tc.want)
			}
			if got != (Target{}) {
				t.Errorf("failed decode must return zero Target, got %+v", got)
			}
		})
	}
}

func TestEncodePayloadRejectsInvalidTargets(t *testing.T) {
	cases := []struct {
		name   string
		target Target
		want   error
	}{
		{"unknown type", Target{Type: "district", ID: "1"}, ErrPayloadUnknown},
		{"empty type", Target{}, ErrPayloadUnknown},
		{"zone without id", Target{Type: targetTypeZone}, ErrPayloadMalformed},
		{"ward without id", Target{Type: targetTypeWard, ID: "  "}, ErrPayloadMalformed},
		{"camera without id", Target{Type: targetTypeChokepoint}, ErrPayloadMalformed},
		{"camera id with slash", Target{Type: targetTypeChokepoint, ID: "../../etc"}, ErrPayloadMalformed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := EncodePayload(tc.target); !errors.Is(err, tc.want) {
				t.Fatalf("EncodePayload(%+v) = (%q, %v), want error %v", tc.target, got, err, tc.want)
			}
		})
	}

	// "all" carries no id; an id supplied by mistake is ignored, not encoded.
	payload, err := EncodePayload(Target{Type: targetTypeAll, ID: "ignored"})
	if err != nil || payload != "sub_all" {
		t.Fatalf("EncodePayload(all with id) = (%q, %v), want (\"sub_all\", nil)", payload, err)
	}
}

// TestPackageIsLeaf enforces spec §4.1 / §8.1 at the source level: the
// production files of internal/telegrambot must import no other internal/
// package. go build alone cannot catch this because the test files are exempt.
func TestPackageIsLeaf(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse package dir: %v", err)
	}

	found := 0
	for _, pkg := range pkgs {
		for name, file := range pkg.Files {
			found++
			for _, imp := range file.Imports {
				path := strings.Trim(imp.Path.Value, `"`)
				if strings.Contains(path, "/internal/") && !strings.HasSuffix(path, "/internal/telegrambot") {
					t.Errorf("%s imports internal package %q; telegrambot must stay a leaf",
						filepath.Base(name), path)
				}
			}
		}
	}
	if found == 0 {
		t.Fatal("no production files parsed; the leaf-import guard is not effective")
	}
}
