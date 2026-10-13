package cvclient_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/529-studio/projectKV/services/go-server/internal/cvclient"
	"github.com/529-studio/projectKV/services/go-server/internal/hotset"
)

type roundTripperFunc func(req *http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func mockGeminiResponse(text string) string {
	resp := map[string]any{
		"candidates": []map[string]any{
			{
				"content": map[string]any{
					"parts": []map[string]any{
						{"text": text},
					},
				},
				"finishReason": "STOP",
			},
		},
	}
	b, _ := json.Marshal(resp)
	return string(b)
}

func TestCloudVisionAnalyzer_Analyze_Success(t *testing.T) {
	ctx := context.Background()
	cam := hotset.Camera{
		ID:          "cam-101",
		Name:        "Nguyen Van Linh",
		Zone:        "District 7",
		SnapshotURL: "http://proxy.test/snapshot/cam-101.jpg",
	}

	geminiJSON := `{"traffic_severity": "CONGESTED", "flood_severity": "DRY", "confidence": 0.95, "description": "Severe traffic jam with bumper to bumper vehicles."}`

	mockClient := &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.String(), "snapshot") {
				if req.Header.Get("User-Agent") == "" {
					t.Error("expected User-Agent header on snapshot request")
				}
				if req.Header.Get("Referer") == "" {
					t.Error("expected Referer header on snapshot request")
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"image/jpeg"}},
					Body:       io.NopCloser(bytes.NewReader([]byte("\xff\xd8\xff\xe0mock-jpeg-bytes"))),
				}, nil
			}

			if strings.Contains(req.URL.Host, "googleapis.com") {
				// Verify API key in x-goog-api-key header (Rule 8 secret shielding)
				if req.Header.Get("x-goog-api-key") != "test-api-key" && req.URL.Query().Get("key") != "test-api-key" {
					t.Errorf("expected api key 'test-api-key' in header or query, got header %q", req.Header.Get("x-goog-api-key"))
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(mockGeminiResponse(geminiJSON))),
				}, nil
			}

			return nil, errors.New("unexpected URL: " + req.URL.String())
		}),
	}

	analyzer := cvclient.NewCloudVisionAnalyzer(
		"test-api-key",
		cvclient.WithHTTPClient(mockClient),
		cvclient.WithModel("gemini-2.0-flash-lite"),
	)

	v, err := analyzer.Analyze(ctx, cam)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if v.CameraID != cam.ID {
		t.Errorf("expected camera ID %q, got %q", cam.ID, v.CameraID)
	}
	if v.Severity != hotset.SeverityCongested {
		t.Errorf("expected severity %q, got %q", hotset.SeverityCongested, v.Severity)
	}
	if v.FloodSeverity != hotset.SeverityDry {
		t.Errorf("expected flood severity %q, got %q", hotset.SeverityDry, v.FloodSeverity)
	}
	if v.Confidence != 0.95 {
		t.Errorf("expected confidence 0.95, got %f", v.Confidence)
	}
	if v.Source != "vlm" {
		t.Errorf("expected source 'vlm', got %q", v.Source)
	}
	if v.ObservedAt.IsZero() {
		t.Error("expected non-zero ObservedAt")
	}
	expectedBytes := []byte("\xff\xd8\xff\xe0mock-jpeg-bytes")
	if !bytes.Equal(v.RawFrame, expectedBytes) {
		t.Errorf("expected RawFrame %q, got %q", expectedBytes, v.RawFrame)
	}
}

func TestCloudVisionAnalyzer_Analyze_LowConfidence_DegradesToUnknown(t *testing.T) {
	ctx := context.Background()
	cam := hotset.Camera{
		ID:          "cam-102",
		SnapshotURL: "http://proxy.test/snapshot/cam-102.jpg",
	}

	// Severity is CONGESTED but confidence is 0.35 (< 0.5 threshold)
	geminiJSON := `{"traffic_severity": "CONGESTED", "flood_severity": "DRY", "confidence": 0.35, "description": "Too dark to see clearly."}`

	mockClient := &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.String(), "snapshot") {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"image/jpeg"}},
					Body:       io.NopCloser(bytes.NewReader([]byte("jpeg-bytes"))),
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(mockGeminiResponse(geminiJSON))),
			}, nil
		}),
	}

	analyzer := cvclient.NewCloudVisionAnalyzer(
		"test-api-key",
		cvclient.WithHTTPClient(mockClient),
	)

	v, err := analyzer.Analyze(ctx, cam)
	if err != nil {
		t.Fatalf("low confidence should degrade gracefully without error, got: %v", err)
	}

	if v.Severity != hotset.SeverityUnknown {
		t.Errorf("expected severity %q, got %q", hotset.SeverityUnknown, v.Severity)
	}
	if v.Confidence != 0.35 {
		t.Errorf("expected confidence 0.35, got %f", v.Confidence)
	}
	if v.Source != "vlm" {
		t.Errorf("expected source 'vlm', got %q", v.Source)
	}
}

func TestCloudVisionAnalyzer_Analyze_MarkdownWrappedJSON(t *testing.T) {
	ctx := context.Background()
	cam := hotset.Camera{
		ID:          "cam-103",
		SnapshotURL: "http://proxy.test/snapshot/cam-103.jpg",
	}

	wrapped := "```json\n{\"traffic_severity\": \"MODERATE\", \"flood_severity\": \"WET\", \"confidence\": 0.88, \"description\": \"Slow moving traffic on wet road.\"}\n```"

	mockClient := &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.String(), "snapshot") {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewReader([]byte("jpeg-bytes"))),
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(mockGeminiResponse(wrapped))),
			}, nil
		}),
	}

	analyzer := cvclient.NewCloudVisionAnalyzer("test-key", cvclient.WithHTTPClient(mockClient))
	v, err := analyzer.Analyze(ctx, cam)
	if err != nil {
		t.Fatalf("unexpected error parsing markdown-wrapped JSON: %v", err)
	}

	if v.Severity != hotset.SeverityModerate {
		t.Errorf("expected MODERATE, got %q", v.Severity)
	}
	if v.Confidence != 0.88 {
		t.Errorf("expected 0.88, got %f", v.Confidence)
	}
}

func TestCloudVisionAnalyzer_Analyze_UnknownSeverityString(t *testing.T) {
	ctx := context.Background()
	cam := hotset.Camera{
		ID:          "cam-104",
		SnapshotURL: "http://proxy.test/snapshot/cam-104.jpg",
	}

	geminiJSON := `{"traffic_severity": "CHAOTIC", "flood_severity": "UNKNOWN", "confidence": 0.85, "description": "Unrecognized classification."}`

	mockClient := &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.String(), "snapshot") {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewReader([]byte("jpeg-bytes"))),
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(mockGeminiResponse(geminiJSON))),
			}, nil
		}),
	}

	analyzer := cvclient.NewCloudVisionAnalyzer("test-key", cvclient.WithHTTPClient(mockClient))
	v, err := analyzer.Analyze(ctx, cam)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if v.Severity != hotset.SeverityUnknown {
		t.Errorf("expected UNKNOWN for unmapped severity, got %q", v.Severity)
	}
}

func TestCloudVisionAnalyzer_Analyze_SnapshotFetchError(t *testing.T) {
	ctx := context.Background()
	cam := hotset.Camera{
		ID:          "cam-105",
		SnapshotURL: "http://proxy.test/snapshot/cam-105.jpg",
	}

	mockClient := &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.String(), "snapshot") {
				return &http.Response{
					StatusCode: http.StatusNotFound,
					Body:       io.NopCloser(strings.NewReader("404 Not Found")),
				}, nil
			}
			return nil, errors.New("should not reach gemini")
		}),
	}

	analyzer := cvclient.NewCloudVisionAnalyzer("test-key", cvclient.WithHTTPClient(mockClient))
	v, err := analyzer.Analyze(ctx, cam)
	if err == nil {
		t.Fatal("expected error on snapshot 404, got nil")
	}

	if v.Severity != hotset.SeverityUnknown {
		t.Errorf("expected UNKNOWN on error, got %q", v.Severity)
	}
	if v.Source != "vlm" {
		t.Errorf("expected source 'vlm', got %q", v.Source)
	}
}

func TestCloudVisionAnalyzer_Analyze_EmptySnapshotBody(t *testing.T) {
	ctx := context.Background()
	cam := hotset.Camera{
		ID:          "cam-106",
		SnapshotURL: "http://proxy.test/snapshot/cam-106.jpg",
	}

	mockClient := &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader([]byte{})),
			}, nil
		}),
	}

	analyzer := cvclient.NewCloudVisionAnalyzer("test-key", cvclient.WithHTTPClient(mockClient))
	v, err := analyzer.Analyze(ctx, cam)
	if err == nil {
		t.Fatal("expected error on empty snapshot, got nil")
	}
	if v.Severity != hotset.SeverityUnknown {
		t.Errorf("expected UNKNOWN, got %q", v.Severity)
	}
}

func TestCloudVisionAnalyzer_Analyze_GeminiAPIError(t *testing.T) {
	ctx := context.Background()
	cam := hotset.Camera{
		ID:          "cam-107",
		SnapshotURL: "http://proxy.test/snapshot/cam-107.jpg",
	}

	mockClient := &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.String(), "snapshot") {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewReader([]byte("jpeg-data"))),
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusForbidden,
				Body:       io.NopCloser(strings.NewReader(`{"error": {"code": 403, "message": "API key not valid"}}`)),
			}, nil
		}),
	}

	analyzer := cvclient.NewCloudVisionAnalyzer("test-key", cvclient.WithHTTPClient(mockClient))
	v, err := analyzer.Analyze(ctx, cam)
	if err == nil {
		t.Fatal("expected error on Gemini 403, got nil")
	}
	if v.Severity != hotset.SeverityUnknown {
		t.Errorf("expected UNKNOWN on error, got %q", v.Severity)
	}
}

func TestCloudVisionAnalyzer_Analyze_GeminiRateLimit429(t *testing.T) {
	ctx := context.Background()
	cam := hotset.Camera{
		ID:          "cam-429",
		SnapshotURL: "http://proxy.test/snapshot/cam-429.jpg",
	}

	mockClient := &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.String(), "snapshot") {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewReader([]byte("jpeg-data"))),
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Body:       io.NopCloser(strings.NewReader(`{"error": {"code": 429, "message": "Resource has been exhausted (e.g. check quota)."}}`)),
			}, nil
		}),
	}

	analyzer := cvclient.NewCloudVisionAnalyzer("test-key", cvclient.WithHTTPClient(mockClient))
	v, err := analyzer.Analyze(ctx, cam)
	if err == nil {
		t.Fatal("expected error on Gemini 429, got nil")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("expected error to mention 429 rate limit, got %v", err)
	}
	if v.Severity != hotset.SeverityUnknown {
		t.Errorf("expected UNKNOWN on 429 error, got %q", v.Severity)
	}
}

func TestCloudVisionAnalyzer_Analyze_GeminiMalformedJSON(t *testing.T) {
	ctx := context.Background()
	cam := hotset.Camera{
		ID:          "cam-108",
		SnapshotURL: "http://proxy.test/snapshot/cam-108.jpg",
	}

	mockClient := &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.String(), "snapshot") {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewReader([]byte("jpeg-data"))),
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(mockGeminiResponse("this is not valid json"))),
			}, nil
		}),
	}

	analyzer := cvclient.NewCloudVisionAnalyzer("test-key", cvclient.WithHTTPClient(mockClient))
	v, err := analyzer.Analyze(ctx, cam)
	if err == nil {
		t.Fatal("expected error on malformed candidate JSON, got nil")
	}
	if v.Severity != hotset.SeverityUnknown {
		t.Errorf("expected UNKNOWN, got %q", v.Severity)
	}
}

func TestCloudVisionAnalyzer_Analyze_EmptyCandidates(t *testing.T) {
	ctx := context.Background()
	cam := hotset.Camera{
		ID:          "cam-109",
		SnapshotURL: "http://proxy.test/snapshot/cam-109.jpg",
	}

	mockClient := &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.String(), "snapshot") {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewReader([]byte("jpeg-data"))),
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"candidates": []}`)),
			}, nil
		}),
	}

	analyzer := cvclient.NewCloudVisionAnalyzer("test-key", cvclient.WithHTTPClient(mockClient))
	v, err := analyzer.Analyze(ctx, cam)
	if err == nil {
		t.Fatal("expected error on empty candidates, got nil")
	}
	if v.Severity != hotset.SeverityUnknown {
		t.Errorf("expected UNKNOWN, got %q", v.Severity)
	}
}

func TestCloudVisionAnalyzer_Analyze_GhostImagePlaceholder(t *testing.T) {
	ctx := context.Background()
	cam := hotset.Camera{
		ID:          "cam-110",
		SnapshotURL: "http://proxy.test/snapshot/cam-110.jpg",
	}

	mockClient := &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.String(), "snapshot") {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewReader([]byte("\x89PNG\r\n\x1a\nplaceholder-bytes"))),
				}, nil
			}
			return nil, errors.New("should not reach gemini")
		}),
	}

	analyzer := cvclient.NewCloudVisionAnalyzer("test-key", cvclient.WithHTTPClient(mockClient))
	v, err := analyzer.Analyze(ctx, cam)
	if err == nil {
		t.Fatal("expected error on ghost image placeholder, got nil")
	}
	if v.Severity != hotset.SeverityUnknown {
		t.Errorf("expected UNKNOWN, got %q", v.Severity)
	}
}
