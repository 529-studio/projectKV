package cvclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/529-studio/projectKV/services/go-server/internal/hotset"
)

// cvPredictResponse is the JSON payload returned by POST /predict when the
// OpenVINO model is loaded and inference succeeds.
type cvPredictResponse struct {
	TrafficSeverity  string  `json:"traffic_severity"`
	Confidence       float64 `json:"confidence"`
	VehiclesDetected int     `json:"vehicles_detected"`
	LatencyMs        float64 `json:"latency_ms"`
	Model            string  `json:"model"`
	// Source is present only on standby responses; non-empty means no-model.
	Source string `json:"source"`
	Mode   string `json:"mode"`
}

// CVEngineAnalyzer calls the self-hosted OpenVINO cv-engine microservice at
// url (CV_ENGINE_URL). It implements hotset.Analyzer.
//
// Protocol:
//  1. GET cam.SnapshotURL → raw JPEG bytes.
//  2. POST url+"/predict" with body = JPEG bytes, Content-Type: application/octet-stream.
//  3. HTTP 503 or source=="standby_dummy" → error (no model loaded).
//  4. Non-200 → error.
//  5. 200 → parse JSON → hotset.Verdict{Source: "onnx"}.
type CVEngineAnalyzer struct {
	url    string
	client *http.Client
}

// Ensure CVEngineAnalyzer satisfies hotset.Analyzer at compile time.
var _ hotset.Analyzer = (*CVEngineAnalyzer)(nil)

// NewCVEngineAnalyzer constructs a CVEngineAnalyzer pointing at url.
// If client is nil, SharedClient is used.
func NewCVEngineAnalyzer(url string, client *http.Client) *CVEngineAnalyzer {
	if client == nil {
		client = SharedClient
	}
	return &CVEngineAnalyzer{url: url, client: client}
}

// unknownVerdict returns a zero-confidence UNKNOWN verdict for cam, used on
// all error paths so callers always receive a typed value even on failure.
func unknownVerdict(camID string) hotset.Verdict {
	now := time.Now()
	return hotset.Verdict{
		CameraID:      camID,
		Severity:      hotset.SeverityUnknown,
		FloodSeverity: hotset.SeverityUnknown,
		Confidence:    0,
		Source:        "onnx",
		Timestamp:     now,
		ObservedAt:    now,
	}
}

// Analyze fetches the camera snapshot via cam.SnapshotURL, then POSTs the
// raw JPEG bytes to the cv-engine /predict endpoint and maps the response to
// a hotset.Verdict. The caller-supplied ctx is propagated to both HTTP calls.
func (a *CVEngineAnalyzer) Analyze(ctx context.Context, cam hotset.Camera) (hotset.Verdict, error) {
	// Step 1: fetch the cached snapshot frame.
	snapReq, err := http.NewRequestWithContext(ctx, http.MethodGet, cam.SnapshotURL, nil)
	if err != nil {
		return unknownVerdict(cam.ID), fmt.Errorf("cv-engine: build snapshot request: %w", err)
	}
	snapResp, err := a.client.Do(snapReq)
	if err != nil {
		return unknownVerdict(cam.ID), fmt.Errorf("cv-engine: fetch snapshot: %w", err)
	}
	defer snapResp.Body.Close()

	if snapResp.StatusCode != http.StatusOK {
		return unknownVerdict(cam.ID), fmt.Errorf("cv-engine: snapshot returned status %d", snapResp.StatusCode)
	}

	jpegBytes, err := io.ReadAll(snapResp.Body)
	if err != nil {
		return unknownVerdict(cam.ID), fmt.Errorf("cv-engine: read snapshot body: %w", err)
	}
	if len(jpegBytes) == 0 {
		return unknownVerdict(cam.ID), fmt.Errorf("cv-engine: snapshot body is empty")
	}

	// Step 2: POST raw JPEG bytes to /predict.
	predictURL := a.url + "/predict"
	predReq, err := http.NewRequestWithContext(ctx, http.MethodPost, predictURL, readerFromBytes(jpegBytes))
	if err != nil {
		return unknownVerdict(cam.ID), fmt.Errorf("cv-engine: build predict request: %w", err)
	}
	predReq.Header.Set("Content-Type", "application/octet-stream")

	predResp, err := a.client.Do(predReq)
	if err != nil {
		return unknownVerdict(cam.ID), fmt.Errorf("cv-engine: POST /predict: %w", err)
	}
	defer predResp.Body.Close()

	// Step 3: standby guard — 503 means no model is loaded.
	if predResp.StatusCode == http.StatusServiceUnavailable {
		return unknownVerdict(cam.ID), fmt.Errorf("cv-engine standby: no model loaded (HTTP 503 from %s)", predictURL)
	}

	if predResp.StatusCode != http.StatusOK {
		return unknownVerdict(cam.ID), fmt.Errorf("cv-engine /predict: status %d", predResp.StatusCode)
	}

	// Step 4: parse JSON verdict.
	var resp cvPredictResponse
	if err := json.NewDecoder(predResp.Body).Decode(&resp); err != nil {
		return unknownVerdict(cam.ID), fmt.Errorf("cv-engine: decode predict response: %w", err)
	}

	// Guard: source=="standby_dummy" on a 200 response is also rejected.
	if resp.Source == "standby_dummy" {
		return unknownVerdict(cam.ID), fmt.Errorf("cv-engine standby: source=standby_dummy, no model loaded")
	}

	now := time.Now()
	return hotset.Verdict{
		CameraID:      cam.ID,
		Severity:      resp.TrafficSeverity,
		FloodSeverity: hotset.SeverityUnknown, // cv-engine v1 does not detect flood
		Confidence:    resp.Confidence,
		Source:        "onnx",
		Timestamp:     now,
		ObservedAt:    now,
	}, nil
}

// readerFromBytes returns an io.Reader backed by b using stdlib bytes.
func readerFromBytes(b []byte) io.Reader {
	return bytes.NewReader(b)
}
