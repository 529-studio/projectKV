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

// Analyze fetches the camera snapshot via cam.SnapshotURL, then POSTs the
// frame bytes to the cv-engine /predict endpoint and maps the response to
// a hotset.Verdict. When cam has registered directional regions, it forwards
// them via multipart metadata and pins overall severity to the worst direction.
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

	// Step 2: POST bytes to /predict. If cam carries directional regions, send
	// multipart metadata; otherwise keep legacy application/octet-stream body.
	predictURL := a.url + "/predict"
	var predResp *http.Response
	if len(cam.Regions) > 0 {
		mpBody, contentType, err := buildMultipartBody(jpegBytes, cam.Regions)
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
