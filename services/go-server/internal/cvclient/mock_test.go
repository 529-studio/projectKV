package cvclient

import (
	"context"
	"testing"

	"github.com/529-studio/projectKV/services/go-server/internal/hotset"
)

func TestMockAnalyzer_CyclesSeverities(t *testing.T) {
	ctx := context.Background()
	analyzer := NewMockAnalyzer()
	cam := hotset.Camera{ID: "cam-1", Name: "Test Camera", Zone: "Test"}

	// Call enough times to cover the full severity cycle twice.
	seen := make(map[string]int)
	for i := 0; i < len(mockSeverities)*2; i++ {
		v, err := analyzer.Analyze(ctx, cam)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if v.CameraID != cam.ID {
			t.Errorf("expected camera_id %q, got %q", cam.ID, v.CameraID)
		}
		if v.Source != "mock" {
			t.Errorf("expected source mock, got %q", v.Source)
		}
		if v.Confidence != 0.99 {
			t.Errorf("expected confidence 0.99, got %f", v.Confidence)
		}
		seen[v.Severity]++
	}

	for _, sev := range mockSeverities {
		if seen[sev] != 2 {
			t.Errorf("expected severity %q to appear exactly twice, got %d", sev, seen[sev])
		}
	}
}

func TestMockAnalyzer_DeterministicOrder(t *testing.T) {
	ctx := context.Background()
	analyzer := NewMockAnalyzer()
	cam := hotset.Camera{ID: "cam-2"}

	for i := 0; i < len(mockSeverities); i++ {
		v, err := analyzer.Analyze(ctx, cam)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if v.Severity != mockSeverities[i] {
			t.Errorf("call %d: expected %q, got %q", i, mockSeverities[i], v.Severity)
		}
	}
}

func TestMockAnalyzer_ThreadSafe(t *testing.T) {
	ctx := context.Background()
	analyzer := NewMockAnalyzer()
	cam := hotset.Camera{ID: "cam-3"}

	const workers = 50
	const callsPerWorker = 20
	errCh := make(chan error, workers*callsPerWorker)

	for i := 0; i < workers; i++ {
		go func() {
			for j := 0; j < callsPerWorker; j++ {
				_, err := analyzer.Analyze(ctx, cam)
				errCh <- err
			}
		}()
	}

	for i := 0; i < workers*callsPerWorker; i++ {
		if err := <-errCh; err != nil {
			t.Fatalf("concurrent analyze error: %v", err)
		}
	}
}
