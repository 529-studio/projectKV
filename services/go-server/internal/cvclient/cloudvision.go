package cvclient

import (
	"context"
	"github.com/529-studio/projectKV/services/go-server/internal/hotset"
)

type CloudVisionAnalyzer struct{ apiKey string }

func NewCloudVisionAnalyzer(apiKey string) *CloudVisionAnalyzer {
	return &CloudVisionAnalyzer{apiKey: apiKey}
}
func (a *CloudVisionAnalyzer) Analyze(ctx context.Context, cam hotset.Camera) (hotset.Verdict, error) {
	return hotset.Verdict{CameraID: cam.ID, Severity: "CLEAR", Confidence: 0.8}, nil
}
