package harvester

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// Minimum JPEG size: 20 KB (20,480 bytes) per ADR-009 / Issue #262.
const MinJPEGSizeBytes = 20480

// IsValidJPEG checks SOI magic bytes (0xFF, 0xD8) and minimum size constraint.
func IsValidJPEG(data []byte) bool {
	if len(data) < MinJPEGSizeBytes {
		return false
	}
	return data[0] == 0xFF && data[1] == 0xD8
}

// IsRainWeatherCode returns true if code indicates drizzle, rain, or thunderstorm.
func IsRainWeatherCode(code int) bool {
	switch code {
	case 51, 53, 55, 56, 57, 61, 63, 65, 66, 67, 80, 81, 82, 95, 96, 99:
		return true
	default:
		return false
	}
}

// DetermineTimeOfDay maps an ICT timestamp to day, night, or dawn_dusk.
func DetermineTimeOfDay(t time.Time) string {
	mins := t.Hour()*60 + t.Minute()
	switch {
	case mins >= 330 && mins < 390: // 05:30 to 06:30
		return "dawn_dusk"
	case mins >= 390 && mins < 1050: // 06:30 to 17:30
		return "day"
	case mins >= 1050 && mins < 1110: // 17:30 to 18:30
		return "dawn_dusk"
	default:
		return "night"
	}
}

// DetermineWaterlineDepth maps meteorological condition to physical waterline depth anchors.
func DetermineWaterlineDepth(weather *WeatherInfo) (flood, depth, anchor, weatherCond string) {
	if weather == nil || (weather.RainfallMm < 0.10 && !IsRainWeatherCode(weather.WeatherCode)) {
		return "NONE", "DRY", "dry", "dry"
	}

	if weather.RainfallMm < 5.0 {
		return "NONE", "WET_SURFACE", "wet_asphalt_no_standing", "rain"
	}

	return "SHALLOW", "TIRE_QUARTER_SUBMERGED", "tire_base_under_10cm", "heavy_rain"
}

// DetermineTrafficSeverity maps inference prediction to traffic density and queue reason.
func DetermineTrafficSeverity(res *PredictionResult) (severity, queueReason string) {
	if res == nil {
		return "CLEAR", "free_flow"
	}

	severity = res.TrafficSeverity
	if severity == "" {
		switch {
		case res.SaturationIndex >= 0.80:
			severity = "CONGESTED"
		case res.SaturationIndex >= 0.60:
			severity = "HEAVY"
		case res.SaturationIndex >= 0.30:
			severity = "MODERATE"
		default:
			severity = "CLEAR"
		}
	}

	switch severity {
	case "HEAVY", "CONGESTED":
		queueReason = "bottleneck"
	default:
		queueReason = "free_flow"
	}

	return severity, queueReason
}

// Pipeline executes concurrent frame ingestion, local inference, schema validation, and R2 streaming.
type Pipeline struct {
	fetcher     SnapshotFetcher
	predictor   VisionPredictor
	storage     StorageUploader
	concurrency int
	loc         *time.Location
}

// NewPipeline constructs a new ingestion Pipeline.
func NewPipeline(fetcher SnapshotFetcher, predictor VisionPredictor, storage StorageUploader, concurrency int, loc *time.Location) *Pipeline {
	if concurrency <= 0 {
		concurrency = 15
	}
	if loc == nil {
		loc = time.FixedZone("ICT", 7*3600)
	}
	return &Pipeline{
		fetcher:     fetcher,
		predictor:   predictor,
		storage:     storage,
		concurrency: concurrency,
		loc:         loc,
	}
}

// ProcessCamera executes the full single-camera harvesting pipeline:
// download -> JPEG validation -> inference -> PCU / waterline mapping -> Draft-07 validation -> R2 upload.
func (p *Pipeline) ProcessCamera(ctx context.Context, cam Camera, scope Scope, weather *WeatherInfo) error {
	// Step 1: Download frame
	frameData, err := p.fetcher.FetchSnapshot(ctx, cam)
	if err != nil {
		return fmt.Errorf("fetch frame: %w", err)
	}

	// Step 2: Validate frame integrity (magic bytes 0xFFD8 and size >= 20 KB)
	if !IsValidJPEG(frameData) {
		return fmt.Errorf("invalid JPEG frame: size %d bytes (min %d bytes) or missing SOI marker", len(frameData), MinJPEGSizeBytes)
	}

	// Step 3: Run OpenVINO inference
	pred, err := p.predictor.Predict(ctx, cam, frameData)
	if err != nil {
		slog.Warn("harvester: prediction failed, using fallback clear", "camera_id", cam.ID, "err", err)
		pred = &PredictionResult{
			TrafficSeverity: "CLEAR",
			Confidence:      0.5,
		}
	}

	// Step 4: Map directional PCU and environmental anchors
	trafficSev, queueReason := DetermineTrafficSeverity(pred)
	floodSev, waterlineDepth, floodAnchor, weatherCond := DetermineWaterlineDepth(weather)

	nowICT := time.Now().In(p.loc)
	timeOfDay := DetermineTimeOfDay(nowICT)
	dateStr := nowICT.Format("2006-01-02")
	unixTs := nowICT.Unix()

	frameID := fmt.Sprintf("%s_%d", cam.ID, unixTs)
	framePath := fmt.Sprintf("proofs/%s/%s/%d_sample.jpg", dateStr, cam.ID, unixTs)
	labelPath := fmt.Sprintf("labels/%s/cam_%s_%d.json", dateStr, cam.ID, unixTs)

	manifest := GroundTruthManifest{
		FrameID:     frameID,
		CameraID:    cam.ID,
		CameraName:  cam.Name(),
		ImagePath:   framePath,
		Annotator:   "auto_harvester",
		AnnotatedAt: nowICT.UTC().Format(time.RFC3339),
		Notes:       fmt.Sprintf("Triggered by %s; vehicles_detected=%d", scope, pred.VehiclesDetected),
		Labels: LabelsPayload{
			Traffic:               trafficSev,
			CarriagewayDirection:  "bidirectional",
			TrafficDirA:           trafficSev,
			TrafficDirB:           trafficSev,
			IsWaitingRedLight:     false,
			QueueReason:           queueReason,
			Flood:                 floodSev,
			WaterlineDepth:        waterlineDepth,
			FloodSubmersionAnchor: floodAnchor,
			TimeOfDay:             timeOfDay,
			Weather:               weatherCond,
		},
	}

	// Step 5: Draft-07 schema validation
	if err := ValidateDraft07(&manifest); err != nil {
		return fmt.Errorf("manifest schema validation failed: %w", err)
	}

	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}

	// Step 6: Stream to Cloudflare R2 object storage
	if err := p.storage.Put(ctx, framePath, bytes.NewReader(frameData)); err != nil {
		return fmt.Errorf("upload frame to %s: %w", framePath, err)
	}

	if err := p.storage.Put(ctx, labelPath, bytes.NewReader(manifestBytes)); err != nil {
		return fmt.Errorf("upload label to %s: %w", labelPath, err)
	}

	return nil
}

// HarvestCameras runs the pipeline across all given cameras concurrently bounded by semaphore.
func (p *Pipeline) HarvestCameras(ctx context.Context, cameras []Camera, scope Scope, weather *WeatherInfo) (int, error) {
	if len(cameras) == 0 {
		return 0, nil
	}

	sem := make(chan struct{}, p.concurrency)
	var wg sync.WaitGroup
	var successCount int64

	for _, cam := range cameras {
		if ctx.Err() != nil {
			break
		}

		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			break
		}

		wg.Add(1)
		go func(c Camera) {
			defer func() {
				<-sem
				wg.Done()
			}()

			if err := p.ProcessCamera(ctx, c, scope, weather); err != nil {
				slog.Warn("harvester: camera harvest failed", "camera_id", c.ID, "scope", scope, "err", err)
				return
			}
			atomic.AddInt64(&successCount, 1)
		}(cam)
	}

	wg.Wait()
	return int(atomic.LoadInt64(&successCount)), ctx.Err()
}
