package cvclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"strings"
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

// cvPredictRegion represents per-direction inference metrics returned by cv-engine (ADR-012 D1).
type cvPredictRegion struct {
	RegionID         string        `json:"region_id"`
	Direction        string        `json:"direction"`
	CapacityShare    float64       `json:"capacity_share"`
	VehicleCounts    VehicleCounts `json:"vehicle_counts"`
	VehiclesDetected int           `json:"vehicles_detected"`
	WaterRatio       float64       `json:"water_ratio"`
}

// cvPredictResponse is the JSON payload returned by POST /predict when the
// OpenVINO model is loaded and inference succeeds.
type cvPredictResponse struct {
	TrafficSeverity  string        `json:"traffic_severity"`
	FloodSeverity    string        `json:"flood_severity,omitempty"`
	Confidence       float64       `json:"confidence"`
	VehiclesDetected int           `json:"vehicles_detected"`
	VehicleCounts    VehicleCounts `json:"vehicle_counts,omitempty"`
	LatencyMs        float64       `json:"latency_ms"`
	Model            string        `json:"model"`
	// Source is present only on standby responses; non-empty means no-model.
	Source  string            `json:"source"`
	Mode    string            `json:"mode"`
	Regions []cvPredictRegion `json:"regions,omitempty"`

	// Physical hazard layer (Issue #401, spec §5.1/§5.3). Every field is optional:
	// an image from before #401 omits them, and `DepthEstimatedCm` is the only
	// legacy key the rolling-deploy fallback may read. Pointers keep "absent"
	// distinguishable from a measured 0.0 (AC13).
	DepthEstimatedCm     *float64 `json:"depth_estimated_cm"`
	FloodDepthCm         *float64 `json:"flood_depth_cm"`
	FloodHazardLevel     *int     `json:"flood_hazard_level"`
	FloodHazardBasis     string   `json:"flood_hazard_basis"`
	FloodDepthConfidence string   `json:"flood_depth_confidence"`
	// cv-engine publishes `calibration_ref`; the API key is `flood_calibration_ref`
	// (§5.2), so this decode tag and the hotset field deliberately differ.
	CalibrationRef string `json:"calibration_ref"`
}

// Hazard basis and confidence vocabularies published by cv-engine (spec §5.1).
// The Go layer never invents a value outside these literals.
const (
	hazardBasisDepthMeasured = "DEPTH_MEASURED"

	hazardDepthConfidenceInferred = "INFERRED"
)

// Hazard bands in centimetres (spec §2.3). 20.0 is inclusive into level 3 because
// the exhaust outlet sits at the waterline; the values are classified unrounded.
const (
	hazardLevelTwoMinCm   = 10.0
	hazardLevelThreeMinCm = 20.0
)

// hazardLevelFromDepthCm derives a level from a centimetre depth. It exists only
// for the rolling-deploy fallback described in spec §5.3: when cv-engine reports
// `depth_estimated_cm` but not the hazard block.
func hazardLevelFromDepthCm(depthCm float64) int {
	switch {
	case depthCm < hazardLevelTwoMinCm:
		return 1
	case depthCm < hazardLevelThreeMinCm:
		return 2
	default:
		return 3
	}
}

// resolveHazardFields applies the §5.3 pass-through contract.
//
//   - A published level is carried verbatim, with every companion field exactly
//     as the engine sent it (no clamping, no re-classification).
//   - Level absent, the whole hazard block absent, and `depth_estimated_cm` present
//     (older cv-engine image during a rolling deploy) derives the level from that
//     measured depth, labelled DEPTH_MEASURED/INFERRED so the UI can tell it from a
//     measured level.
//   - Both absent (VLM, mock, unknownVerdict) yields nil pointers and empty
//     strings, keeping the legacy flood badge rendering (AC14).
//   - A current cv-engine image that explicitly declines to measure (level null with
//     basis `NONE` and confidence `UNMEASURED`) is carried verbatim: nil stays nil.
//
// `water_ratio` is never an input: without a calibrated scale it is an area
// ratio, not a depth, and deriving a level from it is the defect being fixed.
func resolveHazardFields(
	resp *cvPredictResponse,
) (depthCm *float64, level *int, basis, confidence, calibrationRef string) {
	level = resp.FloodHazardLevel
	depthCm = resp.FloodDepthCm
	basis = resp.FloodHazardBasis
	confidence = resp.FloodDepthConfidence
	calibrationRef = resp.CalibrationRef

	if level == nil && resp.DepthEstimatedCm != nil && hazardBlockAbsent(resp) {
		derived := hazardLevelFromDepthCm(*resp.DepthEstimatedCm)
		level = &derived
		verbatimDepth := *resp.DepthEstimatedCm
		depthCm = &verbatimDepth
		basis = hazardBasisDepthMeasured
		confidence = hazardDepthConfidenceInferred
	}
	return depthCm, level, basis, confidence, calibrationRef
}

// hazardBlockAbsent reports whether resp carries no §5.1 hazard block at all.
//
// The rolling-deploy fallback exists for one situation only: a cv-engine image that
// predates the hazard contract and therefore sends `depth_estimated_cm` without any
// of the companion keys. A current image always publishes the block, so a level of
// null together with a published basis, confidence and calibration reference is an
// explicit "not measured" verdict (an avoidance frame or a frame with no reference
// object) and must be preserved as nil. Deriving a level there would turn the
// engine's deliberate silence into a fabricated 0 cm reading (AC10/AC13).
func hazardBlockAbsent(resp *cvPredictResponse) bool {
	return resp.FloodHazardBasis == "" &&
		resp.FloodDepthConfidence == "" &&
		resp.CalibrationRef == ""
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

// Health statuses reported by CVEngineAnalyzer.ProbeHealth. They describe the
// cv-engine process itself, never a camera feed (see FeedError for that).
const (
	// CVEngineStatusHealthy means the engine answered 200 and has an OpenVINO
	// model loaded, so POST /predict can serve real inference.
	CVEngineStatusHealthy = "healthy"
	// CVEngineStatusDegraded means the engine answered 200 but reports
	// model_loaded=false: it is up in standby mode and cannot serve inference.
	CVEngineStatusDegraded = "degraded"
	// CVEngineStatusUnhealthy means the engine could not be probed at all
	// (network failure, non-200 status, or an unparseable payload).
	CVEngineStatusUnhealthy = "unhealthy"

	// defaultCVEngineServiceName is reported when /health omits the service
	// name; the analyzer already knows which service it is talking to.
	defaultCVEngineServiceName = "cv-engine"

	// snapshotUserAgent identifies the proxy on camera snapshot fetches and on
	// health probes. Cameras and WAFs in front of them drop requests without a
	// browser-like User-Agent.
	snapshotUserAgent = "Mozilla/5.0 (compatible; CameraViewProxy/1.0)"
)

// CVEngineHealth is a point-in-time health report for the cv-engine service,
// as returned by CVEngineAnalyzer.ProbeHealth.
type CVEngineHealth struct {
	// Status is one of CVEngineStatusHealthy, CVEngineStatusDegraded or
	// CVEngineStatusUnhealthy. It is derived from ModelLoaded, never copied
	// verbatim from the /health payload, so a standby engine can never be
	// reported as healthy.
	Status string `json:"status"`
	// Service is the service name reported by /health, defaulting to
	// "cv-engine" when the payload omits it.
	Service string `json:"service"`
	// OpenVINOAvailable reports whether the engine can access its OpenVINO
	// runtime.
	OpenVINOAvailable bool `json:"openvino_available"`
	// ModelLoaded reports whether an inference model is loaded. False means
	// the engine is running in standby.
	ModelLoaded bool `json:"model_loaded"`
	// LatencyMs is the round-trip duration of the health probe itself,
	// measured in milliseconds.
	LatencyMs float64 `json:"latency_ms"`
	// Error carries the failure reason when Status is unhealthy (or the reason
	// the engine gave for being degraded). Empty on a healthy engine.
	Error string `json:"error,omitempty"`
	// CheckedAt is when the probe started.
	CheckedAt time.Time `json:"checked_at"`
}

// cvHealthResponse is the subset of the cv-engine GET /health payload the API
// consumes. Unknown keys are ignored so the engine may add diagnostics freely.
type cvHealthResponse struct {
	Service           string `json:"service"`
	OpenVINOAvailable bool   `json:"openvino_available"`
	ModelLoaded       bool   `json:"model_loaded"`
	Error             string `json:"error,omitempty"`
}

// ProbeHealth performs GET url+"/health" and maps the response onto
// CVEngineHealth.
//
// Status derivation follows the payload, not the engine's own opinion:
// model_loaded=true is healthy, model_loaded=false is degraded. A transport
// failure, a non-200 status, or a payload that cannot be decoded is unhealthy
// and the CVEngineHealth is returned populated (Status, Error, CheckedAt,
// LatencyMs) together with a non-nil error, so callers may either log the
// structured report or short-circuit on err.
func (a *CVEngineAnalyzer) ProbeHealth(ctx context.Context) (CVEngineHealth, error) {
	healthURL := strings.TrimSuffix(a.url, "/") + "/health"
	health := CVEngineHealth{
		Service:   defaultCVEngineServiceName,
		CheckedAt: time.Now(),
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
	if err != nil {
		return unhealthyHealth(health, fmt.Errorf("cv-engine health: build request for %s: %w", healthURL, err))
	}
	req.Header.Set("User-Agent", snapshotUserAgent)

	start := time.Now()
	resp, err := a.client.Do(req)
	health.LatencyMs = float64(time.Since(start).Microseconds()) / 1000.0
	if err != nil {
		return unhealthyHealth(health, fmt.Errorf("cv-engine health: GET %s: %w", healthURL, err))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return unhealthyHealth(health, fmt.Errorf("cv-engine health: GET %s returned status %d", healthURL, resp.StatusCode))
	}

	var payload cvHealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return unhealthyHealth(health, fmt.Errorf("cv-engine health: decode response: %w", err))
	}

	if payload.Service != "" {
		health.Service = payload.Service
	}
	health.OpenVINOAvailable = payload.OpenVINOAvailable
	health.ModelLoaded = payload.ModelLoaded
	health.Error = payload.Error

	if payload.ModelLoaded {
		health.Status = CVEngineStatusHealthy
	} else {
		health.Status = CVEngineStatusDegraded
	}
	return health, nil
}

// unhealthyHealth stamps base with the unhealthy status and the cause text,
// returning it alongside cause so the caller gets both report and error.
func unhealthyHealth(base CVEngineHealth, cause error) (CVEngineHealth, error) {
	base.Status = CVEngineStatusUnhealthy
	base.Error = cause.Error()
	return base, cause
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

// buildMultipartBody constructs a multipart/form-data payload containing the
// frame image bytes and directional roadway regions metadata (ADR-012 D1).
func buildMultipartBody(jpeg []byte, regions []hotset.RoadwayRegion) ([]byte, string, error) {
	var b bytes.Buffer
	w := multipart.NewWriter(&b)

	part, err := w.CreateFormFile("frame", "frame.jpg")
	if err != nil {
		return nil, "", fmt.Errorf("create frame form file: %w", err)
	}
	if _, err := part.Write(jpeg); err != nil {
		return nil, "", fmt.Errorf("write frame bytes: %w", err)
	}

	metaJSON, err := json.Marshal(map[string]any{"regions": regions})
	if err != nil {
		return nil, "", fmt.Errorf("marshal regions metadata: %w", err)
	}
	metaPart, err := w.CreateFormField("meta")
	if err != nil {
		return nil, "", fmt.Errorf("create meta form field: %w", err)
	}
	if _, err := metaPart.Write(metaJSON); err != nil {
		return nil, "", fmt.Errorf("write meta bytes: %w", err)
	}

	if err := w.Close(); err != nil {
		return nil, "", fmt.Errorf("close multipart writer: %w", err)
	}

	return b.Bytes(), w.FormDataContentType(), nil
}

// FeedError marks a failure that originated in the camera feed (fetching or
// reading cam.SnapshotURL) rather than in the cv-engine service. Callers that
// sample verdicts can therefore tell "this camera is unreachable" from "the
// inference worker is down" without string matching, and park only the camera
// that is actually broken.
//
// The value receivers keep both FeedError and *FeedError assignable to error;
// Analyze wraps a *FeedError, so `var fe *FeedError; errors.As(err, &fe)` and
// `var marker interface{ IsFeedError() bool }; errors.As(err, &marker)` both
// succeed.
type FeedError struct {
	// CameraID is the camera whose snapshot could not be obtained.
	CameraID string
	// URL is the snapshot URL that failed.
	URL string
	// Err is the underlying cause.
	Err error
}

// Error implements error.
func (e FeedError) Error() string {
	where := e.URL
	if e.CameraID != "" {
		where = fmt.Sprintf("%s (%s)", e.CameraID, e.URL)
	}
	switch {
	case where == "":
		return fmt.Sprintf("cv-engine: camera feed failure: %v", e.Err)
	case e.Err == nil:
		return fmt.Sprintf("cv-engine: camera feed failure: %s", where)
	default:
		return fmt.Sprintf("cv-engine: camera feed failure: %s: %v", where, e.Err)
	}
}

// Unwrap exposes the underlying cause to errors.Is and errors.As.
func (e FeedError) Unwrap() error { return e.Err }

// IsFeedError marks the error as a camera feed failure. Consumers may assert
// with errors.As against an interface{ IsFeedError() bool } target, which keeps
// this package's callers decoupled from the concrete type.
func (e FeedError) IsFeedError() bool { return true }

// newFeedError wraps a snapshot-phase failure for cam.
func newFeedError(camID, snapshotURL string, err error) *FeedError {
	return &FeedError{CameraID: camID, URL: snapshotURL, Err: err}
}

// Analyze fetches the camera snapshot via cam.SnapshotURL, then POSTs the
// frame bytes to the cv-engine /predict endpoint and maps the response to
// a hotset.Verdict. When cam has registered directional regions, it forwards
// them via multipart metadata and pins overall severity to the worst direction.
//
// Every failure while obtaining the frame is returned as a *FeedError; failures
// from /predict are plain errors, so a caller can distinguish a broken camera
// from a broken inference service.
func (a *CVEngineAnalyzer) Analyze(ctx context.Context, cam hotset.Camera) (hotset.Verdict, error) {
	// Step 1: fetch the cached snapshot frame. Every failure here belongs to the
	// camera feed, so each one is wrapped in a *FeedError.
	snapReq, err := http.NewRequestWithContext(ctx, http.MethodGet, cam.SnapshotURL, nil)
	if err != nil {
		return unknownVerdict(cam.ID), newFeedError(cam.ID, cam.SnapshotURL,
			fmt.Errorf("build snapshot request: %w", err))
	}
	snapReq.Header.Set("User-Agent", snapshotUserAgent)
	snapResp, err := a.client.Do(snapReq)
	if err != nil {
		return unknownVerdict(cam.ID), newFeedError(cam.ID, cam.SnapshotURL,
			fmt.Errorf("fetch snapshot: %w", err))
	}
	defer snapResp.Body.Close()

	if snapResp.StatusCode != http.StatusOK {
		return unknownVerdict(cam.ID), newFeedError(cam.ID, cam.SnapshotURL,
			fmt.Errorf("snapshot returned status %d", snapResp.StatusCode))
	}

	jpegBytes, err := io.ReadAll(snapResp.Body)
	if err != nil {
		return unknownVerdict(cam.ID), newFeedError(cam.ID, cam.SnapshotURL,
			fmt.Errorf("read snapshot body: %w", err))
	}
	if len(jpegBytes) == 0 {
		return unknownVerdict(cam.ID), newFeedError(cam.ID, cam.SnapshotURL,
			errors.New("snapshot body is empty"))
	}

	// Step 2: POST bytes to /predict. If cam carries directional regions, send
	// multipart metadata; otherwise keep legacy application/octet-stream body.
	predictURL := a.url + "/predict"
	var predResp *http.Response
	if len(cam.Regions) > 0 {
		var mpBody []byte
		var contentType string
		mpBody, contentType, err = buildMultipartBody(jpegBytes, cam.Regions)
		if err != nil {
			return unknownVerdict(cam.ID), err
		}
		predResp, err = a.postPredict(ctx, predictURL, mpBody, contentType, cam.ID)
	} else {
		predResp, err = a.postPredict(ctx, predictURL, jpegBytes, "application/octet-stream", cam.ID)
	}
	if err != nil {
		return unknownVerdict(cam.ID), err
	}
	if predResp == nil || predResp.Body == nil {
		return unknownVerdict(cam.ID), errors.New("cv-engine: nil predict response")
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
	var directions []hotset.DirectionalVerdict

	if len(resp.Regions) > 0 && a.calculator != nil {
		dirInputs := make([]DirectionalInput, len(resp.Regions))
		for i, r := range resp.Regions {
			share := r.CapacityShare
			if share <= 0 {
				for _, cr := range cam.Regions {
					if cr.RegionID == r.RegionID || cr.Direction == r.Direction {
						share = cr.CapacityShare
						break
					}
				}
			}
			dirInputs[i] = DirectionalInput{
				RegionID:      r.RegionID,
				Direction:     r.Direction,
				CapacityShare: share,
				Counts:        r.VehicleCounts,
				WaterRatio:    r.WaterRatio,
			}
		}
		dirOut := a.calculator.CalculateDirectional(cam.ID, cam.BaseCapacity, dirInputs)
		if dirOut.WorstSeverity != "" {
			severity = dirOut.WorstSeverity
		}
		saturationIndex = dirOut.WorstSI
		pcuTotal = dirOut.TotalPCU
		directions = dirOut.Directions
	} else if a.calculator != nil {
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

	floodSev := resp.FloodSeverity
	if floodSev == "" {
		floodSev = hotset.SeverityUnknown
	}

	hazardDepthCm, hazardLevel, hazardBasis, hazardConfidence, hazardCalibrationRef := resolveHazardFields(&resp)

	now := time.Now()
	return hotset.Verdict{
		CameraID:             cam.ID,
		Severity:             severity,
		FloodSeverity:        floodSev,
		Confidence:           resp.Confidence,
		Source:               "onnx",
		Timestamp:            now,
		ObservedAt:           now,
		RawFrame:             jpegBytes,
		SaturationIndex:      saturationIndex,
		PCUTotal:             pcuTotal,
		Directions:           directions,
		FloodDepthCm:         hazardDepthCm,
		FloodHazardLevel:     hazardLevel,
		FloodHazardBasis:     hazardBasis,
		FloodDepthConfidence: hazardConfidence,
		FloodCalibrationRef:  hazardCalibrationRef,
	}, nil
}

// postPredict sends body to the predict endpoint with contentType, retrying
// only transport-level failures with linear backoff. `cameraID` travels as
// X-Camera-ID so cv-engine can apply that camera's operator calibration
// (spec §2.2); an empty id simply falls back to the raw scale.
func (a *CVEngineAnalyzer) postPredict(ctx context.Context, url string, body []byte, contentType, cameraID string) (*http.Response, error) {
	attempts := a.attempts
	if attempts < 1 {
		attempts = 1
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("cv-engine: build predict request: %w", err)
		}
		req.Header.Set("Content-Type", contentType)
		if cameraID != "" {
			req.Header.Set("X-Camera-ID", cameraID)
		}

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
