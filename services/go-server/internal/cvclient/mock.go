package cvclient

import (
	"context"
	"github.com/529-studio/projectKV/services/go-server/internal/hotset"
	"time"
)

type MockAnalyzer struct{}

func NewMockAnalyzer() *MockAnalyzer { return &MockAnalyzer{} }
func (m *MockAnalyzer) Analyze(ctx context.Context, cam hotset.Camera) (hotset.Verdict, error) {
	return hotset.Verdict{CameraID: cam.ID, Severity: "CLEAR", Confidence: 0.99, Source: "mock", Timestamp: time.Now()}, nil
}
