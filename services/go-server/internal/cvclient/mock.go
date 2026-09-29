package cvclient

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/529-studio/projectKV/services/go-server/internal/hotset"
)

// mockSeverities is the ordered list of severities the mock analyzer rotates
// through on successive Analyze calls. This produces visible color changes on
// the map during dev smoke tests without calling any real inference backend.
var mockSeverities = []string{
	hotset.SeverityClear,
	hotset.SeverityModerate,
	hotset.SeverityHeavy,
	hotset.SeverityCongested,
}

type MockAnalyzer struct {
	counter atomic.Uint64
}

func NewMockAnalyzer() *MockAnalyzer { return &MockAnalyzer{} }
func (m *MockAnalyzer) Analyze(ctx context.Context, cam hotset.Camera) (hotset.Verdict, error) {
	idx := m.counter.Add(1) - 1
	severity := mockSeverities[idx%uint64(len(mockSeverities))]
	return hotset.Verdict{CameraID: cam.ID, Severity: severity, Confidence: 0.99, Source: "mock", Timestamp: time.Now()}, nil
}
