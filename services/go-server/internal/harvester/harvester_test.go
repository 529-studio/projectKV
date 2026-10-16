package harvester_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/529-studio/projectKV/services/go-server/internal/harvester"
)

// generateMockJPEG creates a valid JPEG byte slice with SOI marker of at least minBytes size.
func generateMockJPEG(size int) []byte {
	if size < 2 {
		size = 2
	}
	buf := make([]byte, size)
	buf[0] = 0xFF
	buf[1] = 0xD8
	for i := 2; i < size; i++ {
		buf[i] = 0xAA
	}
	return buf
}

// MockWeatherProvider implements harvester.WeatherProvider.
type mockWeatherProvider struct {
	mu   sync.Mutex
	info *harvester.WeatherInfo
	err  error
}

func (m *mockWeatherProvider) GetWeather(ctx context.Context) (*harvester.WeatherInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.info, m.err
}

func (m *mockWeatherProvider) setWeather(info *harvester.WeatherInfo, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.info = info
	m.err = err
}

// MockSnapshotFetcher implements harvester.SnapshotFetcher.
type mockSnapshotFetcher struct {
	mu     sync.Mutex
	frames map[string][]byte
	err    error
}

func (m *mockSnapshotFetcher) FetchSnapshot(ctx context.Context, cam harvester.Camera) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return nil, m.err
	}
	if data, ok := m.frames[cam.ID]; ok {
		return data, nil
	}
	return generateMockJPEG(25000), nil
}

// MockVisionPredictor implements harvester.VisionPredictor.
type mockVisionPredictor struct {
	mu        sync.Mutex
	result    *harvester.PredictionResult
	err       error
	callCount int64
}

func (m *mockVisionPredictor) Predict(ctx context.Context, cam harvester.Camera, frame []byte) (*harvester.PredictionResult, error) {
	atomic.AddInt64(&m.callCount, 1)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return nil, m.err
	}
	if m.result != nil {
		return m.result, nil
	}
	return &harvester.PredictionResult{
		TrafficSeverity:  "CLEAR",
		Confidence:       0.88,
		VehiclesDetected: 15,
		VehicleCounts: harvester.VehicleCounts{
			Motorcycle: 10,
			Car:        5,
		},
		SaturationIndex: 0.25,
		LatencyMs:       20.0,
	}, nil
}

// MockStorageUploader implements harvester.StorageUploader.
type mockStorageUploader struct {
	mu      sync.Mutex
	uploads map[string][]byte
	err     error
}

func newMockStorageUploader() *mockStorageUploader {
	return &mockStorageUploader{
		uploads: make(map[string][]byte),
	}
}

func (m *mockStorageUploader) Put(ctx context.Context, path string, data io.Reader) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	b, err := io.ReadAll(data)
	if err != nil {
		return err
	}
	m.uploads[path] = b
	return nil
}

func (m *mockStorageUploader) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.uploads)
}

func TestIsValidJPEG(t *testing.T) {
	t.Run("Valid JPEG >= 20KB", func(t *testing.T) {
		valid := generateMockJPEG(20480)
		if !harvester.IsValidJPEG(valid) {
			t.Errorf("expected valid JPEG to pass check")
		}
	})

	t.Run("Truncated JPEG < 20KB", func(t *testing.T) {
		truncated := generateMockJPEG(10000)
		if harvester.IsValidJPEG(truncated) {
			t.Errorf("expected truncated JPEG (< 20KB) to fail check")
		}
	})

	t.Run("Missing SOI marker", func(t *testing.T) {
		corrupt := make([]byte, 25000)
		corrupt[0] = 0x00
		corrupt[1] = 0x00
		if harvester.IsValidJPEG(corrupt) {
			t.Errorf("expected missing SOI marker to fail check")
		}
	})
}

func TestValidateDraft07(t *testing.T) {
	validManifest := func() *harvester.GroundTruthManifest {
		return &harvester.GroundTruthManifest{
			FrameID:     "cam_01_1729060000",
			CameraID:    "cam_01",
			CameraName:  "Võ Văn Kiệt - Ký Con",
			ImagePath:   "proofs/2026-10-16/cam_01/1729060000_sample.jpg",
			Annotator:   "auto_harvester",
			AnnotatedAt: time.Now().UTC().Format(time.RFC3339),
			Labels: harvester.LabelsPayload{
				Traffic:               "CLEAR",
				CarriagewayDirection:  "bidirectional",
				TrafficDirA:           "CLEAR",
				TrafficDirB:           "CLEAR",
				QueueReason:           "free_flow",
				Flood:                 "NONE",
				WaterlineDepth:        "DRY",
				FloodSubmersionAnchor: "dry",
				TimeOfDay:             "day",
				Weather:               "dry",
				QualityTags:           []string{"occluded"},
			},
		}
	}

	t.Run("Valid Manifest passes", func(t *testing.T) {
		m := validManifest()
		if err := harvester.ValidateDraft07(m); err != nil {
			t.Fatalf("expected valid manifest to pass, got: %v", err)
		}
	})

	t.Run("Missing root fields", func(t *testing.T) {
		m := validManifest()
		m.FrameID = ""
		if err := harvester.ValidateDraft07(m); err == nil {
			t.Errorf("expected error on missing frame_id")
		}

		m = validManifest()
		m.ImagePath = ""
		if err := harvester.ValidateDraft07(m); err == nil {
			t.Errorf("expected error on missing image_path")
		}

		m = validManifest()
		m.Annotator = ""
		if err := harvester.ValidateDraft07(m); err == nil {
			t.Errorf("expected error on missing annotator")
		}
	})

	t.Run("Invalid annotator", func(t *testing.T) {
		m := validManifest()
		m.Annotator = "human_user"
		if err := harvester.ValidateDraft07(m); err == nil {
			t.Errorf("expected error when annotator is not 'auto_harvester'")
		}
	})

	t.Run("Invalid timestamp", func(t *testing.T) {
		m := validManifest()
		m.AnnotatedAt = "not-a-timestamp"
		if err := harvester.ValidateDraft07(m); err == nil {
			t.Errorf("expected error on non-RFC3339 timestamp")
		}
	})

	t.Run("Invalid traffic enum", func(t *testing.T) {
		m := validManifest()
		m.Labels.Traffic = "SUPER_JAMMED"
		if err := harvester.ValidateDraft07(m); err == nil {
			t.Errorf("expected error on invalid traffic enum")
		}
	})

	t.Run("Invalid flood enum", func(t *testing.T) {
		m := validManifest()
		m.Labels.Flood = "WATER_EVERYWHERE"
		if err := harvester.ValidateDraft07(m); err == nil {
			t.Errorf("expected error on invalid flood enum")
		}
	})

	t.Run("Invalid quality tag enum", func(t *testing.T) {
		m := validManifest()
		m.Labels.QualityTags = []string{"blurry_camera"}
		if err := harvester.ValidateDraft07(m); err == nil {
			t.Errorf("expected error on invalid quality tag")
		}
	})
}

func TestDetermineTimeOfDay(t *testing.T) {
	loc := time.FixedZone("ICT", 7*3600)

	tests := []struct {
		hour int
		min  int
		want string
	}{
		{6, 0, "dawn_dusk"},  // 05:30 - 06:30
		{9, 30, "day"},       // 06:30 - 17:30
		{12, 0, "day"},       // midday
		{18, 0, "dawn_dusk"}, // 17:30 - 18:30
		{21, 0, "night"},     // night
		{2, 0, "night"},      // night
	}

	for _, tt := range tests {
		timestamp := time.Date(2026, 10, 16, tt.hour, tt.min, 0, 0, loc)
		got := harvester.DetermineTimeOfDay(timestamp)
		if got != tt.want {
			t.Errorf("DetermineTimeOfDay(%02d:%02d) = %q, want %q", tt.hour, tt.min, got, tt.want)
		}
	}
}

func TestDetermineWaterlineDepth(t *testing.T) {
	t.Run("Nil or dry weather", func(t *testing.T) {
		f, d, a, w := harvester.DetermineWaterlineDepth(nil)
		if f != "NONE" || d != "DRY" || a != "dry" || w != "dry" {
			t.Errorf("unexpected dry values: f=%s d=%s a=%s w=%s", f, d, a, w)
		}

		f, d, a, w = harvester.DetermineWaterlineDepth(&harvester.WeatherInfo{RainfallMm: 0.0, WeatherCode: 0})
		if f != "NONE" || d != "DRY" || a != "dry" || w != "dry" {
			t.Errorf("unexpected dry values: f=%s d=%s a=%s w=%s", f, d, a, w)
		}
	})

	t.Run("Light rain (< 5.0 mm/h)", func(t *testing.T) {
		f, d, a, w := harvester.DetermineWaterlineDepth(&harvester.WeatherInfo{RainfallMm: 2.5, WeatherCode: 61})
		if f != "NONE" || d != "WET_SURFACE" || a != "wet_asphalt_no_standing" || w != "rain" {
			t.Errorf("unexpected light rain values: f=%s d=%s a=%s w=%s", f, d, a, w)
		}
	})

	t.Run("Heavy rain (>= 5.0 mm/h)", func(t *testing.T) {
		f, d, a, w := harvester.DetermineWaterlineDepth(&harvester.WeatherInfo{RainfallMm: 12.0, WeatherCode: 65})
		if f != "SHALLOW" || d != "TIRE_QUARTER_SUBMERGED" || a != "tire_base_under_10cm" || w != "heavy_rain" {
			t.Errorf("unexpected heavy rain values: f=%s d=%s a=%s w=%s", f, d, a, w)
		}
	})
}

func TestDetermineTrafficSeverity(t *testing.T) {
	t.Run("Saturation Index fallbacks", func(t *testing.T) {
		sev, qr := harvester.DetermineTrafficSeverity(&harvester.PredictionResult{SaturationIndex: 0.20})
		if sev != "CLEAR" || qr != "free_flow" {
			t.Errorf("expected CLEAR/free_flow, got %s/%s", sev, qr)
		}

		sev, qr = harvester.DetermineTrafficSeverity(&harvester.PredictionResult{SaturationIndex: 0.45})
		if sev != "MODERATE" || qr != "free_flow" {
			t.Errorf("expected MODERATE/free_flow, got %s/%s", sev, qr)
		}

		sev, qr = harvester.DetermineTrafficSeverity(&harvester.PredictionResult{SaturationIndex: 0.70})
		if sev != "HEAVY" || qr != "bottleneck" {
			t.Errorf("expected HEAVY/bottleneck, got %s/%s", sev, qr)
		}

		sev, qr = harvester.DetermineTrafficSeverity(&harvester.PredictionResult{SaturationIndex: 0.90})
		if sev != "CONGESTED" || qr != "bottleneck" {
			t.Errorf("expected CONGESTED/bottleneck, got %s/%s", sev, qr)
		}
	})
}

func TestParseScheduleSlots(t *testing.T) {
	t.Run("Default slots", func(t *testing.T) {
		slots := harvester.ParseScheduleSlots("")
		if len(slots) != 3 {
			t.Fatalf("expected 3 slots, got %d", len(slots))
		}
		if slots[0].String() != "07:30" || slots[1].String() != "12:00" || slots[2].String() != "17:30" {
			t.Errorf("unexpected slots: %+v", slots)
		}
	})

	t.Run("Integer hours mapped to peak minutes", func(t *testing.T) {
		slots := harvester.ParseScheduleSlots("7,12,17")
		if len(slots) != 3 {
			t.Fatalf("expected 3 slots, got %d", len(slots))
		}
		if slots[0].String() != "07:30" || slots[1].String() != "12:00" || slots[2].String() != "17:30" {
			t.Errorf("unexpected slots: %+v", slots)
		}
	})

	t.Run("Custom HH:MM slots", func(t *testing.T) {
		slots := harvester.ParseScheduleSlots("08:15,13:45,19:00")
		if len(slots) != 3 {
			t.Fatalf("expected 3 slots, got %d", len(slots))
		}
		if slots[0].String() != "08:15" || slots[1].String() != "13:45" || slots[2].String() != "19:00" {
			t.Errorf("unexpected slots: %+v", slots)
		}
	})
}

func TestScheduler_Deduplication(t *testing.T) {
	loc := time.FixedZone("ICT", 7*3600)
	sched := harvester.NewScheduler("07:30,12:00", loc, nil)

	t1 := time.Date(2026, 10, 16, 7, 30, 10, 0, loc)
	slot, ok := sched.ShouldTrigger(t1)
	if !ok || slot != "07:30" {
		t.Errorf("expected slot 07:30 to trigger, got ok=%v, slot=%s", ok, slot)
	}

	// Immediate re-check at same slot should NOT trigger again today
	_, ok = sched.ShouldTrigger(t1.Add(10 * time.Second))
	if ok {
		t.Errorf("expected duplicate trigger on same day to be blocked")
	}

	// Next day at 07:30 should trigger
	t2 := time.Date(2026, 10, 17, 7, 30, 5, 0, loc)
	slot, ok = sched.ShouldTrigger(t2)
	if !ok || slot != "07:30" {
		t.Errorf("expected slot 07:30 on next day to trigger, got ok=%v, slot=%s", ok, slot)
	}
}

func TestWatcher_TriggerAndCooldown(t *testing.T) {
	weather := &mockWeatherProvider{}
	watcher := harvester.NewWatcher(weather, 60, 5*time.Minute, nil)

	ctx := context.Background()

	// 1. Dry weather -> should not trigger
	weather.setWeather(&harvester.WeatherInfo{RainfallMm: 0.0, WeatherCode: 0}, nil)
	triggered, _, err := watcher.CheckTrigger(ctx)
	if err != nil || triggered {
		t.Fatalf("expected no trigger in dry weather, got triggered=%v, err=%v", triggered, err)
	}

	// 2. Rain occurs (0.5 mm/h) -> should trigger
	weather.setWeather(&harvester.WeatherInfo{RainfallMm: 0.5, WeatherCode: 61}, nil)
	triggered, info, err := watcher.CheckTrigger(ctx)
	if err != nil || !triggered || info == nil {
		t.Fatalf("expected rain trigger, got triggered=%v, info=%v, err=%v", triggered, info, err)
	}

	// 3. Immediate subsequent check during downpour -> sliding cooldown must block re-trigger
	weather.setWeather(&harvester.WeatherInfo{RainfallMm: 2.0, WeatherCode: 63}, nil)
	triggered, _, err = watcher.CheckTrigger(ctx)
	if err != nil || triggered {
		t.Fatalf("expected sliding window cooldown to block re-trigger, got triggered=%v", triggered)
	}
}

func TestPipeline_HarvestCameras(t *testing.T) {
	loc := time.FixedZone("ICT", 7*3600)
	fetcher := &mockSnapshotFetcher{
		frames: map[string][]byte{
			"cam_good": generateMockJPEG(25000),
			"cam_bad":  generateMockJPEG(5000), // < 20KB, should be discarded
		},
	}
	predictor := &mockVisionPredictor{}
	storage := newMockStorageUploader()

	pipeline := harvester.NewPipeline(fetcher, predictor, storage, 5, loc)

	cameras := []harvester.Camera{
		{ID: "cam_good", DisplayName: "Cam Good", SnapshotURL: "https://example.com/good"},
		{ID: "cam_bad", DisplayName: "Cam Bad", SnapshotURL: "https://example.com/bad"},
	}

	weather := &harvester.WeatherInfo{
		RainfallMm:  1.2,
		WeatherCode: 61,
	}

	successCount, err := pipeline.HarvestCameras(context.Background(), cameras, harvester.ScopeRain, weather)
	if err != nil {
		t.Fatalf("pipeline execution failed: %v", err)
	}

	if successCount != 1 {
		t.Errorf("expected 1 successful camera harvested, got %d", successCount)
	}

	// Storage should contain 2 items for cam_good (1 frame JPEG + 1 label JSON)
	if storage.count() != 2 {
		t.Errorf("expected exactly 2 items uploaded to storage, got %d", storage.count())
	}

	// Verify label content
	today := time.Now().In(loc).Format("2006-01-02")
	var labelJSON []byte
	var foundLabel bool
	for k, v := range storage.uploads {
		if bytes.HasPrefix([]byte(k), []byte(fmt.Sprintf("labels/%s/cam_cam_good_", today))) {
			labelJSON = v
			foundLabel = true
			break
		}
	}

	if !foundLabel {
		t.Fatalf("expected label JSON file for cam_good not found in storage uploads")
	}

	var manifest harvester.GroundTruthManifest
	if err := json.Unmarshal(labelJSON, &manifest); err != nil {
		t.Fatalf("failed to unmarshal uploaded label manifest: %v", err)
	}

	if err := harvester.ValidateDraft07(&manifest); err != nil {
		t.Errorf("uploaded manifest failed Draft-07 schema validation: %v", err)
	}

	if manifest.Annotator != "auto_harvester" {
		t.Errorf("expected annotator 'auto_harvester', got %q", manifest.Annotator)
	}
	if manifest.Labels.FloodSubmersionAnchor != "wet_asphalt_no_standing" {
		t.Errorf("expected anchor 'wet_asphalt_no_standing', got %q", manifest.Labels.FloodSubmersionAnchor)
	}
}

func TestCoordinator_LifecycleAndManualHarvest(t *testing.T) {
	loc := time.FixedZone("ICT", 7*3600)
	weather := &mockWeatherProvider{
		info: &harvester.WeatherInfo{RainfallMm: 0.0, WeatherCode: 0},
	}
	fetcher := &mockSnapshotFetcher{
		frames: map[string][]byte{
			"c1": generateMockJPEG(25000),
			"c2": generateMockJPEG(25000),
		},
	}
	predictor := &mockVisionPredictor{}
	storage := newMockStorageUploader()

	hotsetCams := []harvester.Camera{
		{ID: "c1", RoadKeys: []string{"vo_van_kiet", "ky_con"}},
	}
	rainCams := []harvester.Camera{
		{ID: "c2", RoadKeys: []string{"huynh_tan_phat"}},
	}

	cfg := harvester.Config{
		Enabled:             true,
		ScheduleHours:       "07:30,12:00,17:30",
		RainCooldownMinutes: 60,
		Concurrency:         10,
		WatcherInterval:     100 * time.Millisecond,
		TimeLocation:        loc,
	}

	coord, err := harvester.New(cfg, harvester.Dependencies{
		Weather:       weather,
		Fetcher:       fetcher,
		Predictor:     predictor,
		Storage:       storage,
		HotsetCameras: hotsetCams,
		RainCameras:   rainCams,
	})
	if err != nil {
		t.Fatalf("failed to construct coordinator: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := coord.Start(ctx); err != nil {
		t.Fatalf("failed to start coordinator: %v", err)
	}

	// Trigger manual scheduled harvest
	count, err := coord.RunScheduledHarvest(ctx)
	if err != nil {
		t.Errorf("RunScheduledHarvest returned error: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 harvested camera, got %d", count)
	}

	// Trigger manual rain harvest
	count, err = coord.RunRainHarvest(ctx)
	if err != nil {
		t.Errorf("RunRainHarvest returned error: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 harvested camera, got %d", count)
	}

	coord.Stop()
}

func TestCameraFiltering(t *testing.T) {
	allCams := []harvester.Camera{
		{ID: "cam_hotset", RoadKeys: []string{"vo_van_kiet", "ky_con"}},
		{ID: "cam_not_hotset", RoadKeys: []string{"vo_van_kiet"}}, // len == 1, not intersection
		{ID: "cam_flood", RoadKeys: []string{"huynh_tan_phat"}},
		{ID: "cam_other", RoadKeys: []string{"random_street"}},
	}

	hotset := harvester.FilterHotsetCameras(allCams)
	if len(hotset) != 1 || hotset[0].ID != "cam_hotset" {
		t.Errorf("unexpected hotset filtered result: %+v", hotset)
	}

	flood := harvester.FilterFloodCorridorCameras(allCams)
	// cam_hotset (vo_van_kiet), cam_not_hotset (vo_van_kiet), and cam_flood (huynh_tan_phat)
	if len(flood) != 3 {
		t.Errorf("expected 3 flood corridor cameras, got %d", len(flood))
	}
}
