package cvclient

// Tests for the physical hazard layer on the cv-engine wire contract
// (Sprint 12 Task 3, Issue #401, spec §2.3 and §5.2/§5.3).
//
// The Go layer is a pure carrier for these five fields: it decodes them verbatim,
// preserves nil as "never measured" and 0.0 as "measured dry", and never derives a
// level from `water_ratio`. The one exception is the rolling-deploy fallback, which
// exists for the single deploy window in which cv-engine emits `depth_estimated_cm`
// but not the hazard keys.

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/529-studio/projectKV/services/go-server/internal/hotset"
)

// formatFloat renders a depth for embedding in a hand-written JSON body without
// exponent notation or a trailing ".0" surprise.
func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// analyzePredictBody drives one full Analyze cycle against a stub cv-engine that
// always serves a snapshot and returns predictBody from /predict. It returns the
// verdict the caller (the scheduler) would receive.
func analyzePredictBody(t *testing.T, predictBody string) hotset.Verdict {
	t.Helper()

	srv, analyzer := makeCVEngineTestServer(t, http.StatusOK, http.StatusOK, predictBody)
	cam := hotset.Camera{
		ID:          "cam-hazard-1",
		SnapshotURL: srv.URL + "/snapshot/cam-hazard-1.jpg",
	}

	got, err := analyzer.Analyze(context.Background(), cam)
	if err != nil {
		t.Fatalf("Analyze returned an unexpected error: %v", err)
	}
	return got
}

// requireLevel dereferences FloodHazardLevel, failing the test when it is nil.
func requireLevel(t *testing.T, v hotset.Verdict) int {
	t.Helper()
	if v.FloodHazardLevel == nil {
		t.Fatalf("FloodHazardLevel is nil, want an explicit level")
	}
	return *v.FloodHazardLevel
}

// requireDepth dereferences FloodDepthCm, failing the test when it is nil.
func requireDepth(t *testing.T, v hotset.Verdict) float64 {
	t.Helper()
	if v.FloodDepthCm == nil {
		t.Fatalf("FloodDepthCm is nil, want a measured depth")
	}
	return *v.FloodDepthCm
}

// marshalVerdict encodes v the way the HTTP handlers do and decodes it back into a
// generic map so key presence can be asserted (omitempty is observable only here).
func marshalVerdict(t *testing.T, v hotset.Verdict) map[string]any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal(hotset.Verdict): %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("json.Unmarshal(marshalled verdict): %v", err)
	}
	return decoded
}

// TestHazardDecode_FullPayloadVerbatim walks the §5.1 example payload and asserts
// that every field survives the trip through the Go client unchanged.
func TestHazardDecode_FullPayloadVerbatim(t *testing.T) {
	body := `{
		"traffic_severity":"MODERATE","flood_severity":"FLOODED_DEEP","water_ratio":0.34,
		"depth_estimated_cm":14.5,"confidence":0.78,
		"flood_hazard_level":3,"flood_hazard_basis":"AVOIDANCE_INFERRED",
		"flood_depth_confidence":"UNMEASURED","flood_depth_cm":null,
		"calibration_ref":"CURB_15CM"
	}`

	v := analyzePredictBody(t, body)

	if got := requireLevel(t, v); got != 3 {
		t.Errorf("FloodHazardLevel = %d, want 3", got)
	}
	if v.FloodHazardBasis != "AVOIDANCE_INFERRED" {
		t.Errorf("FloodHazardBasis = %q, want AVOIDANCE_INFERRED", v.FloodHazardBasis)
	}
	if v.FloodDepthConfidence != "UNMEASURED" {
		t.Errorf("FloodDepthConfidence = %q, want UNMEASURED", v.FloodDepthConfidence)
	}
	// AC10: an avoidance escalation must not manufacture a depth. The payload says
	// null and the Go layer must not fill it in from depth_estimated_cm (14.5).
	if v.FloodDepthCm != nil {
		t.Errorf("FloodDepthCm = %v, want nil for an unmeasured avoidance escalation", *v.FloodDepthCm)
	}
	if v.FloodCalibrationRef != "CURB_15CM" {
		t.Errorf("FloodCalibrationRef = %q, want CURB_15CM", v.FloodCalibrationRef)
	}
	if v.FloodSeverity != hotset.SeverityFloodedDeep {
		t.Errorf("FloodSeverity = %q, want %q (legacy key must survive)", v.FloodSeverity, hotset.SeverityFloodedDeep)
	}
}

// TestHazardDecode_MeasuredDryZeroIsPreserved pins the nil-vs-0.0 distinction that
// justifies the pointer fields (AC13, E3).
func TestHazardDecode_MeasuredDryZeroIsPreserved(t *testing.T) {
	body := `{"traffic_severity":"CLEAR","flood_severity":"DRY","confidence":0.9,
		"depth_estimated_cm":0.0,"flood_hazard_level":1,"flood_hazard_basis":"DEPTH_MEASURED",
		"flood_depth_confidence":"MEASURED","flood_depth_cm":0.0,
		"calibration_ref":"MOTORCYCLE_110CM"}`

	v := analyzePredictBody(t, body)

	if v.FloodDepthCm == nil {
		t.Fatal("FloodDepthCm is nil; a measured 0.0 must survive decode")
	}
	if *v.FloodDepthCm != 0.0 {
		t.Errorf("FloodDepthCm = %v, want 0.0", *v.FloodDepthCm)
	}
	if got := requireLevel(t, v); got != 1 {
		t.Errorf("FloodHazardLevel = %d, want 1", got)
	}
	if v.FloodDepthConfidence != "MEASURED" {
		t.Errorf("FloodDepthConfidence = %q, want MEASURED", v.FloodDepthConfidence)
	}
	if v.FloodHazardBasis != "DEPTH_MEASURED" {
		t.Errorf("FloodHazardBasis = %q, want DEPTH_MEASURED", v.FloodHazardBasis)
	}
}

// TestHazardDecode_AbsentKeysStayNil covers the pre-contract build (AC12/E13): a
// payload with none of the new keys must produce nil pointers and empty strings,
// never a fabricated Level 1.
func TestHazardDecode_AbsentKeysStayNil(t *testing.T) {
	body := `{"traffic_severity":"MODERATE","flood_severity":"WET","confidence":0.88,
		"vehicles_detected":6,"latency_ms":44.1,"model":"yolov8n_openvino_320"}`

	v := analyzePredictBody(t, body)

	if v.FloodDepthCm != nil {
		t.Errorf("FloodDepthCm = %v, want nil for a pre-contract payload", *v.FloodDepthCm)
	}
	if v.FloodHazardLevel != nil {
		t.Errorf("FloodHazardLevel = %d, want nil for a pre-contract payload", *v.FloodHazardLevel)
	}
	if v.FloodHazardBasis != "" {
		t.Errorf("FloodHazardBasis = %q, want empty", v.FloodHazardBasis)
	}
	if v.FloodDepthConfidence != "" {
		t.Errorf("FloodDepthConfidence = %q, want empty", v.FloodDepthConfidence)
	}
	if v.FloodCalibrationRef != "" {
		t.Errorf("FloodCalibrationRef = %q, want empty", v.FloodCalibrationRef)
	}
}

// TestHazardDecode_CalibrationRefIsRekeyedOnTheWire pins the two different spellings
// of the same value: cv-engine sends `calibration_ref` (§5.1), the API publishes
// `flood_calibration_ref` (§5.2/§5.5). A copied JSON tag would silently drop it.
func TestHazardDecode_CalibrationRefIsRekeyedOnTheWire(t *testing.T) {
	body := `{"traffic_severity":"CLEAR","flood_hazard_level":2,
		"flood_depth_confidence":"MEASURED","flood_depth_cm":12.0,
		"calibration_ref":"MOTORCYCLE_110CM"}`

	v := analyzePredictBody(t, body)
	if v.FloodCalibrationRef != "MOTORCYCLE_110CM" {
		t.Fatalf("FloodCalibrationRef = %q, want MOTORCYCLE_110CM", v.FloodCalibrationRef)
	}

	encoded := marshalVerdict(t, v)
	if _, ok := encoded["calibration_ref"]; ok {
		t.Error(`marshalled verdict contains "calibration_ref"; the API key is "flood_calibration_ref"`)
	}
	if got := encoded["flood_calibration_ref"]; got != "MOTORCYCLE_110CM" {
		t.Errorf(`flood_calibration_ref = %v, want MOTORCYCLE_110CM`, got)
	}
}

// TestHazardFallback_DerivesFromDepthEstimatedCm covers E12: the cv-engine image
// during a rolling deploy has no hazard keys, but it does report the legacy
// `depth_estimated_cm`. The client derives with the §2.3 bands and labels the result
// INFERRED, so the UI can tell a derived level from a measured one.
func TestHazardFallback_DerivesFromDepthEstimatedCm(t *testing.T) {
	cases := []struct {
		name  string
		depth float64
		want  int
	}{
		{"splash", 9.99, 1},
		{"warning-lower-bound", 10.0, 2},
		{"warning-just-below-exhaust", 19.99, 2},
		{"stall-threshold-inclusive", 20.0, 3},
		{"stall", 24.2, 3},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := `{"traffic_severity":"MODERATE","flood_severity":"FLOODED_SHALLOW",
				"confidence":0.85,"depth_estimated_cm":` + formatFloat(tc.depth) + `}`

			v := analyzePredictBody(t, body)

			if got := requireLevel(t, v); got != tc.want {
				t.Errorf("derived FloodHazardLevel = %d, want %d for %.2f cm", got, tc.want, tc.depth)
			}
			if v.FloodHazardBasis != "DEPTH_MEASURED" {
				t.Errorf("FloodHazardBasis = %q, want DEPTH_MEASURED", v.FloodHazardBasis)
			}
			if v.FloodDepthConfidence != "INFERRED" {
				t.Errorf("FloodDepthConfidence = %q, want INFERRED", v.FloodDepthConfidence)
			}
			if got := requireDepth(t, v); got != tc.depth {
				t.Errorf("FloodDepthCm = %v, want %v (verbatim legacy depth)", got, tc.depth)
			}
		})
	}
}

// TestHazardFallback_ExplicitUnmeasuredIsNotLegacy covers the regression the fallback
// gate exists for: a current cv-engine image publishes the hazard block and says
// "not measured" (level null, basis NONE, confidence UNMEASURED) while still emitting
// the legacy `depth_estimated_cm` of 0.0 for a frame with no through-traffic and no
// reference object. Treating that as a legacy payload would resurrect Level 1 from a
// legacy field and publish a fabricated measured 0.0 cm (AC10/AC13).
func TestHazardFallback_ExplicitUnmeasuredIsNotLegacy(t *testing.T) {
	cases := []struct {
		name            string
		levelFragment   string
		wantLevelAbsent bool
	}{
		{"null level", `"flood_hazard_level":null,`, true},
		{"absent level key", "", true},
		{"explicit level one", `"flood_hazard_level":1,`, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := `{"traffic_severity":"CLEAR","flood_severity":"DRY","confidence":0.88,
				"depth_estimated_cm":0.0,` + tc.levelFragment + `
				"flood_hazard_basis":"NONE","flood_depth_confidence":"UNMEASURED",
				"flood_depth_cm":null,"calibration_ref":"NONE"}`

			v := analyzePredictBody(t, body)

			if tc.wantLevelAbsent {
				if v.FloodHazardLevel != nil {
					t.Errorf("FloodHazardLevel = %d, want nil: the engine explicitly reported no measurement", *v.FloodHazardLevel)
				}
				if v.FloodDepthCm != nil {
					t.Errorf("FloodDepthCm = %v, want nil: the engine explicitly reported no measurement", *v.FloodDepthCm)
				}
				if v.FloodHazardBasis != "NONE" {
					t.Errorf("FloodHazardBasis = %q, want NONE (verbatim)", v.FloodHazardBasis)
				}
				if v.FloodDepthConfidence != "UNMEASURED" {
					t.Errorf("FloodDepthConfidence = %q, want UNMEASURED (verbatim)", v.FloodDepthConfidence)
				}
				return
			}
			if got := requireLevel(t, v); got != 1 {
				t.Errorf("FloodHazardLevel = %d, want the published 1", got)
			}
			if v.FloodDepthCm != nil {
				t.Errorf("FloodDepthCm = %v, want nil: a published level must not invent a depth", *v.FloodDepthCm)
			}
		})
	}
}

// TestHazardFallback_TotalAbsenceStaysUnmeasured: when the image sends neither the
// hazard keys nor a depth (VLM, mock, unknownVerdict), the pointers stay nil so the
// UI keeps rendering the legacy flood badge (AC14/E13).
func TestHazardFallback_TotalAbsenceStaysUnmeasured(t *testing.T) {
	body := `{"traffic_severity":"CLEAR","confidence":0.9,"source":"vlm"}`

	v := analyzePredictBody(t, body)

	if v.FloodDepthCm != nil || v.FloodHazardLevel != nil {
		t.Fatalf("hazard pointers = (%v, %v), want (nil, nil)", v.FloodDepthCm, v.FloodHazardLevel)
	}
	if v.FloodHazardBasis != "" {
		t.Errorf("FloodHazardBasis = %q, want empty when nothing was measured or inferred", v.FloodHazardBasis)
	}
}

// TestHazardFallback_ExplicitLevelWins: the fallback exists only for the deploy
// window. Once the image publishes a level, the client must not second-guess it.
func TestHazardFallback_ExplicitLevelWins(t *testing.T) {
	body := `{"traffic_severity":"CLEAR","depth_estimated_cm":24.2,
		"flood_hazard_level":1,"flood_hazard_basis":"DEPTH_MEASURED",
		"flood_depth_confidence":"MEASURED","flood_depth_cm":8.0,"calibration_ref":"CURB_15CM"}`

	v := analyzePredictBody(t, body)

	if got := requireLevel(t, v); got != 1 {
		t.Errorf("FloodHazardLevel = %d, want 1 (the published level, not a re-derivation)", got)
	}
	if got := requireDepth(t, v); got != 8.0 {
		t.Errorf("FloodDepthCm = %v, want 8.0", got)
	}
}

// TestHazard_NeverDerivesFromWaterRatio pins the defect this issue names. An area
// ratio is not a depth: manufacturing a level from `water_ratio` is what produced the
// false FLOODED_SHALLOW in the first place.
func TestHazard_NeverDerivesFromWaterRatio(t *testing.T) {
	body := `{"traffic_severity":"CLEAR","flood_severity":"FLOODED_DEEP",
		"water_ratio":0.42,"confidence":0.8}`

	v := analyzePredictBody(t, body)

	if v.FloodHazardLevel != nil {
		t.Errorf("FloodHazardLevel = %d, want nil: water_ratio is not a depth", *v.FloodHazardLevel)
	}
	if v.FloodDepthCm != nil {
		t.Errorf("FloodDepthCm = %v, want nil: water_ratio is not a depth", *v.FloodDepthCm)
	}
}

// TestHazard_PassThroughIsVerbatim: no clamping, no re-classification. If the engine
// publishes an unusual combination the client carries it, and the disagreement is
// visible to whoever investigates it.
func TestHazard_PassThroughIsVerbatim(t *testing.T) {
	body := `{"traffic_severity":"CLEAR","flood_hazard_level":1,
		"flood_hazard_basis":"DEPTH_MEASURED","flood_depth_confidence":"MEASURED",
		"flood_depth_cm":120.0,"calibration_ref":"CURB_15CM"}`

	v := analyzePredictBody(t, body)

	if got := requireLevel(t, v); got != 1 {
		t.Errorf("FloodHazardLevel = %d, want the published 1", got)
	}
	if got := requireDepth(t, v); got != 120.0 {
		t.Errorf("FloodDepthCm = %v, want the published 120.0", got)
	}
	if v.FloodHazardBasis != "DEPTH_MEASURED" {
		t.Errorf("FloodHazardBasis = %q, want DEPTH_MEASURED", v.FloodHazardBasis)
	}
}

// TestHazardMarshal_OmitsNilAndEmitsZero covers AC11/AC13 at the API boundary: the
// same struct is marshalled by all four endpoints, so these tags are the contract.
func TestHazardMarshal_OmitsNilAndEmitsZero(t *testing.T) {
	t.Run("nil fields are omitted", func(t *testing.T) {
		encoded := marshalVerdict(t, hotset.Verdict{CameraID: "cam-1", Severity: hotset.SeverityClear})
		for _, key := range []string{
			"flood_depth_cm",
			"flood_hazard_level",
			"flood_hazard_basis",
			"flood_depth_confidence",
			"flood_calibration_ref",
		} {
			if _, ok := encoded[key]; ok {
				t.Errorf("marshalled verdict contains %q for an unmeasured verdict", key)
			}
		}
	})

	t.Run("measured zero and level one are emitted", func(t *testing.T) {
		depth := 0.0
		level := 1
		encoded := marshalVerdict(t, hotset.Verdict{
			CameraID:             "cam-1",
			Severity:             hotset.SeverityClear,
			FloodDepthCm:         &depth,
			FloodHazardLevel:     &level,
			FloodHazardBasis:     "DEPTH_MEASURED",
			FloodDepthConfidence: "MEASURED",
			FloodCalibrationRef:  "MOTORCYCLE_110CM",
		})

		gotDepth, ok := encoded["flood_depth_cm"]
		if !ok {
			t.Fatal("flood_depth_cm is missing; omitempty on a bare float64 would erase a measured dry road")
		}
		if gotDepth != 0.0 {
			t.Errorf("flood_depth_cm = %v, want 0", gotDepth)
		}
		if gotLevel := encoded["flood_hazard_level"]; gotLevel != float64(1) {
			t.Errorf("flood_hazard_level = %v, want 1", gotLevel)
		}
		if encoded["flood_hazard_basis"] != "DEPTH_MEASURED" {
			t.Errorf("flood_hazard_basis = %v, want DEPTH_MEASURED", encoded["flood_hazard_basis"])
		}
		if encoded["flood_depth_confidence"] != "MEASURED" {
			t.Errorf("flood_depth_confidence = %v, want MEASURED", encoded["flood_depth_confidence"])
		}
		if encoded["flood_calibration_ref"] != "MOTORCYCLE_110CM" {
			t.Errorf("flood_calibration_ref = %v, want MOTORCYCLE_110CM", encoded["flood_calibration_ref"])
		}
	})
}

// TestHazardMarshal_EveryEndpointEnvelopeCarriesTheFields covers AC11 at the response
// envelope level: /api/traffic-status, /api/flood-status and /api/v1/cameras/hotset/status
// (the `/hotset/status` alias resolves to the same handler) all marshal hotset.Verdict,
// so one assertion per envelope proves the fields reach the wire on every endpoint.
func TestHazardMarshal_EveryEndpointEnvelopeCarriesTheFields(t *testing.T) {
	depth := 14.5
	level := 3
	verdict := hotset.Verdict{
		CameraID:             "cam_dbp_01",
		Severity:             hotset.SeverityClear,
		FloodSeverity:        hotset.SeverityFloodedDeep,
		FloodDepthCm:         &depth,
		FloodHazardLevel:     &level,
		FloodHazardBasis:     "AVOIDANCE_INFERRED",
		FloodDepthConfidence: "UNMEASURED",
		FloodCalibrationRef:  "CURB_15CM",
	}

	envelopes := []struct {
		endpoint string
		payload  any
		listKey  string
	}{
		{
			endpoint: "GET /api/traffic-status",
			payload:  hotset.TrafficStatusResponse{AnalysisEnabled: true, Verdicts: []hotset.Verdict{verdict}},
			listKey:  "verdicts",
		},
		{
			endpoint: "GET /api/flood-status",
			payload:  hotset.FloodStatusResponse{AnalysisEnabled: true, Verdicts: []hotset.Verdict{verdict}},
			listKey:  "verdicts",
		},
		{
			endpoint: "GET /api/v1/cameras/hotset/status",
			payload: hotset.HotsetStatusResponse{
				Total:     1,
				UpdatedAt: time.Date(2026, 11, 14, 9, 12, 30, 0, time.UTC),
				Cameras:   []hotset.Verdict{verdict},
			},
			listKey: "cameras",
		},
	}

	want := map[string]any{
		"flood_depth_cm":         14.5,
		"flood_hazard_level":     float64(3),
		"flood_hazard_basis":     "AVOIDANCE_INFERRED",
		"flood_depth_confidence": "UNMEASURED",
		"flood_calibration_ref":  "CURB_15CM",
	}

	for _, tc := range envelopes {
		t.Run(tc.endpoint, func(t *testing.T) {
			raw, err := json.Marshal(tc.payload)
			if err != nil {
				t.Fatalf("json.Marshal(%T): %v", tc.payload, err)
			}
			var decoded map[string]any
			if err := json.Unmarshal(raw, &decoded); err != nil {
				t.Fatalf("json.Unmarshal(%T): %v", tc.payload, err)
			}

			list, ok := decoded[tc.listKey].([]any)
			if !ok || len(list) != 1 {
				t.Fatalf("%s: verdict list %q is missing or empty", tc.endpoint, tc.listKey)
			}
			item, ok := list[0].(map[string]any)
			if !ok {
				t.Fatalf("%s: verdict entry is not an object", tc.endpoint)
			}

			for key, expected := range want {
				got, present := item[key]
				if !present {
					t.Errorf("%s: %q missing from the verdict payload", tc.endpoint, key)
					continue
				}
				if got != expected {
					t.Errorf("%s: %q = %v, want %v", tc.endpoint, key, got, expected)
				}
			}
		})
	}
}

// TestHazard_NonOnnxAnalyzersStayNil guards the trust boundary: mock and VLM
// analyzers cannot measure a depth, so they must never publish a level (AC14).
func TestHazard_NonOnnxAnalyzersStayNil(t *testing.T) {
	t.Run("unknownVerdict", func(t *testing.T) {
		v := unknownVerdict("cam-1")
		if v.FloodDepthCm != nil || v.FloodHazardLevel != nil {
			t.Errorf("unknownVerdict carries hazard data: (%v, %v)", v.FloodDepthCm, v.FloodHazardLevel)
		}
	})

	t.Run("mock analyzer", func(t *testing.T) {
		mock := NewMockAnalyzer()
		cam := hotset.Camera{ID: "cam-mock-1", SnapshotURL: "http://example.invalid/snapshot.jpg"}

		v, err := mock.Analyze(context.Background(), cam)
		if err != nil {
			t.Fatalf("mock Analyze returned an error: %v", err)
		}
		if v.FloodDepthCm != nil || v.FloodHazardLevel != nil {
			t.Errorf("mock verdict carries hazard data: (%v, %v)", v.FloodDepthCm, v.FloodHazardLevel)
		}
	})
}
