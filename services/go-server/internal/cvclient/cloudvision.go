package cvclient

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/529-studio/projectKV/services/go-server/internal/hotset"
)

// geminiEndpoint is the Gemini multimodal generateContent REST endpoint.
// Model can be overridden at construction time via WithModel.
const geminiEndpointTemplate = "https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s"

// defaultGeminiModel is the production default.
const defaultGeminiModel = "gemini-3.5-flash-lite"

// confidenceThreshold — verdicts below this are degraded to SeverityUnknown.
const confidenceThreshold = 0.5

// trafficPrompt instructs the VLM to return machine-readable JSON only.
// The prompt is intentionally concise to minimise token cost while still
// providing enough schema context so the model reliably produces valid JSON.
const trafficPrompt = `You are a traffic analyst AI. Analyze the traffic camera image and return a JSON object ONLY — no markdown, no explanation.

JSON schema (all fields required):
{
  "traffic_severity": "CLEAR" | "MODERATE" | "HEAVY" | "CONGESTED" | "UNKNOWN",
  "flood_severity": "DRY" | "WET" | "FLOODED_SHALLOW" | "FLOODED_DEEP" | "UNKNOWN",
  "confidence": <float 0.0–1.0>,
  "description": "<one sentence in English>"
}

Rules:
- traffic_severity = CLEAR: free-flowing traffic, no congestion.
- traffic_severity = MODERATE: some slowdowns, vehicles moving.
- traffic_severity = HEAVY: significant slowdown, queues forming.
- traffic_severity = CONGESTED: gridlock or near-standstill.
- flood_severity = DRY: no visible water on road surface.
- flood_severity = WET: wet road, rain, shallow puddles only.
- flood_severity = FLOODED_SHALLOW: ankle-deep to wheel-height water on road.
- flood_severity = FLOODED_DEEP: waist-high or deeper water, impassable.
- If the image is too dark, blurry, obscured, or otherwise unanalyzable, set confidence < 0.5 and use UNKNOWN for both severity fields.
- confidence must reflect how certain you are about BOTH severity fields combined.
- Return ONLY the JSON object. No other text.`

// vlmResponse is the parsed Gemini API top-level response.
type vlmResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// verdictJSON is the structured JSON the VLM is expected to produce.
type verdictJSON struct {
	TrafficSeverity string  `json:"traffic_severity"`
	FloodSeverity   string  `json:"flood_severity"`
	Confidence      float64 `json:"confidence"`
	Description     string  `json:"description"`
}

// CloudVisionAnalyzer calls the Gemini multimodal API with a base64-encoded
// JPEG snapshot fetched from cam.SnapshotURL (via SharedClient), parses the
// structured JSON verdict, and maps it to a hotset.Verdict.
//
// It satisfies the hotset.Analyzer interface.
type CloudVisionAnalyzer struct {
	apiKey     string
	model      string
	httpClient *http.Client
	log        *slog.Logger
}

// NewCloudVisionAnalyzer creates a CloudVisionAnalyzer using the given Gemini API key.
// Use the With* option functions to override defaults.
func NewCloudVisionAnalyzer(apiKey string, opts ...VLMOption) *CloudVisionAnalyzer {
	a := &CloudVisionAnalyzer{
		apiKey:     apiKey,
		model:      defaultGeminiModel,
		httpClient: SharedClient,
		log:        slog.Default(),
	}
	for _, o := range opts {
		o(a)
	}
	return a
}

// VLMOption is a functional option for CloudVisionAnalyzer.
type VLMOption func(*CloudVisionAnalyzer)

// WithModel overrides the Gemini model (e.g. "gemini-2.0-flash").
func WithModel(model string) VLMOption {
	return func(a *CloudVisionAnalyzer) { a.model = model }
}

// WithHTTPClient overrides the HTTP client (used in tests to inject a mock transport).
func WithHTTPClient(c *http.Client) VLMOption {
	return func(a *CloudVisionAnalyzer) { a.httpClient = c }
}

// WithLogger overrides the structured logger.
func WithLogger(l *slog.Logger) VLMOption {
	return func(a *CloudVisionAnalyzer) { a.log = l }
}

// Analyze implements hotset.Analyzer.
//
// Steps:
//  1. Fetch the snapshot image from cam.SnapshotURL.
//  2. Base64-encode the image bytes.
//  3. Send a multimodal Gemini request (image + text prompt).
//  4. Parse the structured JSON response.
//  5. Degrade to SeverityUnknown when confidence < 0.5 or on any error.
func (a *CloudVisionAnalyzer) Analyze(ctx context.Context, cam hotset.Camera) (hotset.Verdict, error) {
	now := time.Now()
	unknown := hotset.Verdict{
		CameraID:   cam.ID,
		Severity:   hotset.SeverityUnknown,
		Confidence: 0,
		Source:     "vlm",
		ObservedAt: now,
		Timestamp:  now,
	}

	// 1. Fetch snapshot image.
	imgBytes, contentType, err := a.fetchSnapshot(ctx, cam.SnapshotURL)
	if err != nil {
		a.log.Warn("vlm: failed to fetch snapshot", "camera", cam.ID, "error", err)
		return unknown, fmt.Errorf("vlm fetch snapshot %s: %w", cam.ID, err)
	}
	if len(imgBytes) == 0 {
		a.log.Warn("vlm: empty snapshot body", "camera", cam.ID)
		return unknown, fmt.Errorf("vlm: empty snapshot for camera %s", cam.ID)
	}

	// 2. Call Gemini API.
	vj, err := a.callGemini(ctx, imgBytes, contentType)
	if err != nil {
		a.log.Warn("vlm: Gemini API error", "camera", cam.ID, "error", err)
		return unknown, fmt.Errorf("vlm gemini call %s: %w", cam.ID, err)
	}

	// 3. Validate confidence — never guess below threshold.
	if vj.Confidence < confidenceThreshold {
		a.log.Info("vlm: low confidence, degrading to UNKNOWN",
			"camera", cam.ID, "confidence", vj.Confidence)
		unknown.Confidence = vj.Confidence
		return unknown, nil
	}

	// 4. Normalise severity string (the VLM may return lowercase).
	sev := normaliseSeverity(vj.TrafficSeverity)

	return hotset.Verdict{
		CameraID:   cam.ID,
		Severity:   sev,
		Confidence: vj.Confidence,
		Source:     "vlm",
		ObservedAt: now,
		Timestamp:  now,
	}, nil
}

// fetchSnapshot downloads the image at url and returns the raw bytes and MIME type.
func (a *CloudVisionAnalyzer) fetchSnapshot(ctx context.Context, url string) ([]byte, string, error) {
	if url == "" {
		return nil, "", fmt.Errorf("empty snapshot URL")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; CameraViewProxy/1.0)")
	req.Header.Set("Referer", "https://giaothong.hochiminhcity.gov.vn/")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("upstream status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20)) // 10 MiB max
	if err != nil {
		return nil, "", fmt.Errorf("read body: %w", err)
	}

	if len(data) >= 8 && string(data[:8]) == "\x89PNG\r\n\x1a\n" {
		return nil, "", fmt.Errorf("upstream returned placeholder image: camera stream offline")
	}

	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "image/jpeg"
	}
	// Strip params like "; charset=utf-8"
	if idx := strings.Index(ct, ";"); idx >= 0 {
		ct = strings.TrimSpace(ct[:idx])
	}

	return data, ct, nil
}

// callGemini sends a multimodal request to the Gemini API and parses the JSON verdict.
func (a *CloudVisionAnalyzer) callGemini(ctx context.Context, imgBytes []byte, mimeType string) (*verdictJSON, error) {
	encoded := base64.StdEncoding.EncodeToString(imgBytes)

	// Build the Gemini generateContent request body.
	reqBody := map[string]any{
		"contents": []map[string]any{
			{
				"parts": []map[string]any{
					{
						"inline_data": map[string]string{
							"mime_type": mimeType,
							"data":      encoded,
						},
					},
					{
						"text": trafficPrompt,
					},
				},
			},
		},
		"generationConfig": map[string]any{
			"temperature":      0.1, // low temperature for deterministic structured output
			"maxOutputTokens":  256,
			"responseMimeType": "application/json",
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	endpoint := fmt.Sprintf(geminiEndpointTemplate, a.model, a.apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build gemini request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gemini http: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MiB max
	if err != nil {
		return nil, fmt.Errorf("read gemini response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gemini status %d: %s", resp.StatusCode, string(respBytes))
	}

	var vlmResp vlmResponse
	if err := json.Unmarshal(respBytes, &vlmResp); err != nil {
		return nil, fmt.Errorf("unmarshal gemini response: %w", err)
	}

	if vlmResp.Error != nil {
		return nil, fmt.Errorf("gemini API error %d: %s", vlmResp.Error.Code, vlmResp.Error.Message)
	}

	if len(vlmResp.Candidates) == 0 {
		return nil, fmt.Errorf("gemini returned no candidates")
	}

	parts := vlmResp.Candidates[0].Content.Parts
	if len(parts) == 0 {
		return nil, fmt.Errorf("gemini candidate has no content parts")
	}

	// Strip potential markdown fences (defensive: should not occur with responseMimeType=application/json).
	text := strings.TrimSpace(parts[0].Text)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	text = strings.TrimSpace(text)

	var vj verdictJSON
	if err := json.Unmarshal([]byte(text), &vj); err != nil {
		return nil, fmt.Errorf("unmarshal VLM verdict JSON (%q): %w", text, err)
	}

	return &vj, nil
}

// normaliseSeverity maps the VLM's string to one of the canonical hotset
// severity constants (uppercase). Falls back to SeverityUnknown for any
// unrecognised value so the UI always receives a valid enum.
func normaliseSeverity(s string) string {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case hotset.SeverityClear:
		return hotset.SeverityClear
	case hotset.SeverityModerate:
		return hotset.SeverityModerate
	case hotset.SeverityHeavy:
		return hotset.SeverityHeavy
	case hotset.SeverityCongested:
		return hotset.SeverityCongested
	default:
		return hotset.SeverityUnknown
	}
}
