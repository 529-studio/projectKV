package cvclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/529-studio/projectKV/services/go-server/internal/hotset"
)

// Default retry policy for POST /predict. A redeploy of the inference worker
// closes the listening socket for a second or two; without retries a single
// missed frame per camera is enough to mark the whole hot set UNKNOWN.
const (
	defaultPredictAttempts = 4
	defaultPredictBackoff  = 300 * time.Millisecond
)

// cvPredictResponse is the JSON payload returned by POST /predict when the
// OpenVINO model is loaded and inference succeeds.
type cvPredictResponse struct {
	TrafficSeverity  string        `json:"traffic_severity"`
	Confidence       float64       `json:"confidence"`
	VehiclesDetected int           `json:"vehicles_detected"`
	VehicleCounts    VehicleCounts `json:"vehicle_counts,omitempty"`
	LatencyMs        float64       `json:"latency_ms"`
	Model            string        `json:"model"`
	// Source is present only on standby responses; non-empty means no-model.
	Source string `json:"source"`
	Mode   string `json:"mode"`
}

// CVEngineOption configures an optional parameter for CVEngineAnalyzer.
type CVEngineOption func(*CVEngineAnalyzer)

// WithPCUCalculator configures a custom PCUCalculator on CVEngineAnalyzer.
func WithPCUCalculator(calc *PCUCalculator) CVEngineOption {
	return func(a *CVEngineAnalyzer) {
		a.calculator = calc
	}
}

// WithPredictRetry overrides the retry policy used when POST /predict fails at
// the transport layer (connection refused, reset, timeout). Non-positive values
// keep the current setting, so callers can tune only one dimension.
func WithPredictRetry(attempts int, backoff time.Duration) CVEngineOption {
	return func(a *CVEngineAnalyzer) {
		if attempts > 0 {
			a.attempts = attempts
		}
		if backoff > 0 {
			a.backoff = backoff
		}
	}
}

// CVEngineAnalyzer calls the self-hosted OpenVINO cv-engine microservice at
// url (CV_ENGINE_URL). It implements hotset.Analyzer.
//
// Protocol:
//  1. GET cam.SnapshotURL → raw JPEG bytes.
//  2. POST url+"/predict" with body = JPEG bytes, Content-Type: application/octet-stream.
//  3. HTTP 503 or source=="standby_dummy" → error (no model loaded).
//  4. Non-200 → error.
//  5. 200 → parse JSON → calculate PCU Saturation Index → hotset.Verdict{Source: "onnx"}.
type CVEngineAnalyzer struct {
	url        string
	client     *http.Client
	calculator *PCUCalculator
	attempts   int
	backoff    time.Duration
}

// Ensure CVEngineAnalyzer satisfies hotset.Analyzer at compile time.
var _ hotset.Analyzer = (*CVEngineAnalyzer)(nil)

// NewCVEngineAnalyzer constructs a CVEngineAnalyzer pointing at url.
// If client is nil, SharedClient is used. If WithPCUCalculator is omitted,
// a default PCUCalculator with baseline weights (ADR-004 §9 D15) is initialized.
func NewCVEngineAnalyzer(url string, client *http.Client, opts ...CVEngineOption) *CVEngineAnalyzer {
	if client == nil {
		client = SharedClient
	}
	a := &CVEngineAnalyzer{
		url:        url,
		client:     client,
		calculator: NewPCUCalculator(DefaultPCUConfig()),
		attempts:   defaultPredictAttempts,
		backoff:    defaultPredictBackoff,
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
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
	snapReq.Header.Set("User-Agent", "Mozilla/5.0 (compatible; CameraViewProxy/1.0)")
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

	// Step 2: POST raw JPEG bytes to /predict, retrying transient transport
	// failures such as the inference worker restarting mid-deploy.
	predictURL := a.url + "/predict"
	predResp, err := a.postPredict(ctx, predictURL, jpegBytes)
	if err != nil {
		return unknownVerdict(cam.ID), err
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

	// Step 5: Compute PCU-weighted Saturation Index severity.
	severity := resp.TrafficSeverity
	var pcuTotal float64
	var saturationIndex float64
	if a.calculator != nil {
		if cam.BaseCapacity > 0 && a.calculator.GetBaseCapacity(cam.ID) == a.calculator.CapacityFloor() {
			a.calculator.SetBaseCapacity(cam.ID, cam.BaseCapacity)
		}
		pcuSev, pcuTot, satIdx := a.calculator.Calculate(cam.ID, resp.VehicleCounts, resp.VehiclesDetected)
		if pcuSev != "" {
			severity = pcuSev
		}
		pcuTotal = pcuTot
		saturationIndex = satIdx
	}

	now := time.Now()
	return hotset.Verdict{
		CameraID:        cam.ID,
		Severity:        severity,
		FloodSeverity:   hotset.SeverityUnknown, // cv-engine v1 does not detect flood
		Confidence:      resp.Confidence,
		Source:          "onnx",
		Timestamp:       now,
		ObservedAt:      now,
		RawFrame:        jpegBytes,
		SaturationIndex: saturationIndex,
		PCUTotal:        pcuTotal,
	}, nil
}

// postPredict sends the JPEG bytes to the predict endpoint, retrying only
// transport-level failures with linear backoff. HTTP responses (including 503
// standby) are returned to the caller unretried because they are deterministic.
func (a *CVEngineAnalyzer) postPredict(ctx context.Context, url string, jpeg []byte) (*http.Response, error) {
	attempts := a.attempts
	if attempts < 1 {
		attempts = 1
	}

	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jpeg))
		if err != nil {
			return nil, fmt.Errorf("cv-engine: build predict request: %w", err)
		}
		req.Header.Set("Content-Type", "application/octet-stream")

		resp, err := a.client.Do(req)
		if err == nil {
			return resp, nil
		}
		lastErr = err

		if attempt == attempts || !isRetryableTransportError(ctx, err) {
			break
		}

		wait := time.Duration(attempt) * a.backoff
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("cv-engine: POST /predict: %w", ctx.Err())
		case <-time.After(wait):
		}
	}

	return nil, fmt.Errorf("cv-engine: POST /predict failed after %d attempt(s): %w", attempts, lastErr)
}

// isRetryableTransportError reports whether err is a transient network failure
// worth another attempt. Context cancellation and deadlines are never retried.
func isRetryableTransportError(ctx context.Context, err error) bool {
	if err == nil || ctx.Err() != nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var netErr net.Error
	return errors.As(err, &netErr)
}
