package hotset

import "testing"

// TestRecordableObservation pins the single production history gate (Issue #441
// scope 3). Each exclusion guards a distinct data-quality defect; if any of them
// regresses, the 24h coverage harness would still pass while production
// silently records fabricated or degraded observations.
func TestRecordableObservation(t *testing.T) {
	tests := []struct {
		name string
		v    Verdict
		want bool
	}{
		{
			name: "onnx traffic observation is recordable",
			v:    Verdict{Severity: SeverityModerate, Source: "onnx", PCUTotal: 18.5, SaturationIndex: 0.42},
			want: true,
		},
		{
			name: "frozen or stale observation is rejected",
			v:    Verdict{Severity: SeverityModerate, Source: "onnx", PCUTotal: 18.5, Stale: true},
			want: false,
		},
		{
			name: "unknown verdict is rejected even from onnx",
			v:    Verdict{Severity: SeverityUnknown, Source: "onnx"},
			want: false,
		},
		{
			name: "vlm label without a pcu series is rejected",
			v:    Verdict{Severity: SeverityHeavy, Source: "vlm"},
			want: false,
		},
		{
			name: "vlm label with a measured pcu series is recordable",
			v:    Verdict{Severity: SeverityHeavy, Source: "vlm", PCUTotal: 40},
			want: true,
		},
		{
			name: "mock verdict without pcu is rejected",
			v:    Verdict{Severity: SeverityClear, Source: "mock"},
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := RecordableObservation(tc.v); got != tc.want {
				t.Fatalf("RecordableObservation(%+v) = %v, want %v", tc.v, got, tc.want)
			}
		})
	}
}
