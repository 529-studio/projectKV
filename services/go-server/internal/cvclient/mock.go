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

var mockFloodSeverities = []string{
	hotset.SeverityDry,
	hotset.SeverityDry,
	hotset.SeverityWet,
	hotset.SeverityDry,
	hotset.SeverityDry,
	hotset.SeverityFloodedShallow,
	hotset.SeverityDry,
	hotset.SeverityDry,
}

type MockAnalyzer struct {
	counter   atomic.Uint64
	mockFrame []byte
}

func NewMockAnalyzer(mockFrame ...[]byte) *MockAnalyzer {
	m := &MockAnalyzer{}
	if len(mockFrame) > 0 {
		m.mockFrame = mockFrame[0]
	}
	return m
}

// SetMockFrame configures mock JPEG frame bytes returned in Verdict.RawFrame.
func (m *MockAnalyzer) SetMockFrame(frame []byte) {
	m.mockFrame = frame
}

func (m *MockAnalyzer) Analyze(ctx context.Context, cam hotset.Camera) (hotset.Verdict, error) {
	idx := m.counter.Add(1) - 1
	severity := mockSeverities[idx%uint64(len(mockSeverities))]
	floodSeverity := mockFloodSeverities[idx%uint64(len(mockFloodSeverities))]

	var si float64
	var pcu float64
	switch severity {
	case hotset.SeverityClear:
		si = 0.15
		pcu = 10.0
	case hotset.SeverityModerate:
		si = 0.45
		pcu = 28.0
	case hotset.SeverityHeavy:
		si = 0.70
		pcu = 45.0
	case hotset.SeverityCongested:
		si = 0.92
		pcu = 65.0
	}

	return hotset.Verdict{
		CameraID:        cam.ID,
		Severity:        severity,
		FloodSeverity:   floodSeverity,
		Confidence:      0.99,
		Source:          "mock",
		Timestamp:       time.Now(),
		RawFrame:        m.mockFrame,
		SaturationIndex: si,
		PCUTotal:        pcu,
	}, nil
}
