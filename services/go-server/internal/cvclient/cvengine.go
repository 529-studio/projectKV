package cvclient

import (
	"context"
	"time"

	"github.com/529-studio/projectKV/services/go-server/internal/hotset"
)

type CVEngineAnalyzer struct{ url string }

func NewCVEngineAnalyzer(url string) *CVEngineAnalyzer { return &CVEngineAnalyzer{url: url} }
func (a *CVEngineAnalyzer) Analyze(ctx context.Context, cam hotset.Camera) (hotset.Verdict, error) {
	return hotset.Verdict{CameraID: cam.ID, Severity: "CLEAR", Confidence: 0.8, Source: "onnx", Timestamp: time.Now()}, nil
}
