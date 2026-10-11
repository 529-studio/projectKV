package flood_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/529-studio/projectKV/services/go-server/internal/camera"
	"github.com/529-studio/projectKV/services/go-server/internal/flood"
	"github.com/529-studio/projectKV/services/go-server/internal/hotset"
	"github.com/529-studio/projectKV/services/go-server/internal/weather"
)

// mockAnalyzer implements hotset.Analyzer for testing.
type mockAnalyzer struct {
	mu              sync.Mutex
	calls           []string
	verdictToReturn hotset.Verdict
	errToReturn     error
}

func (m *mockAnalyzer) Analyze(ctx context.Context, cam hotset.Camera) (hotset.Verdict, error) {
	m.mu.Lock()
	m.calls = append(m.calls, cam.ID)
	m.mu.Unlock()
	if m.errToReturn != nil {
		return hotset.Verdict{}, m.errToReturn
	}
	v := m.verdictToReturn
	v.CameraID = cam.ID
	if v.ObservedAt.IsZero() {
		v.ObservedAt = time.Now()
	}
	return v, nil
}

func (m *mockAnalyzer) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

// mockWeather implements flood.WeatherProvider.
type mockWeather struct {
	weatherData *weather.WeatherData
	err         error
}

func (m *mockWeather) GetWeather(ctx context.Context) (*weather.WeatherData, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.weatherData, nil
}

// mockCameraReader implements camera.Reader.
type mockCameraReader struct {
	cams map[string]*camera.Camera
}

func newMockCameraReader(cams ...camera.Camera) *mockCameraReader {
	m := &mockCameraReader{cams: make(map[string]*camera.Camera)}
	for i := range cams {
		c := cams[i]
		m.cams[c.ID] = &c
	}
	return m
}

func (m *mockCameraReader) All() []camera.Camera {
	res := make([]camera.Camera, 0, len(m.cams))
	for _, c := range m.cams {
		res = append(res, *c)
	}
	return res
}

func (m *mockCameraReader) ByID(id string) (*camera.Camera, bool) {
	c, ok := m.cams[id]
	return c, ok
}

func (m *mockCameraReader) Zones() []string                                  { return nil }
func (m *mockCameraReader) Phuongs() []camera.Phuong                         { return nil }
func (m *mockCameraReader) Filter(zone, corridor string) []camera.Camera     { return nil }
func (m *mockCameraReader) FilterByPhuong(phuongName string) []camera.Camera { return nil }

func sampleChokepoints() []flood.FloodChokepoint {
	return []flood.FloodChokepoint{
		{
			CameraID:       "cam-flood-1",
			CameraName:     "Ung Văn Khiêm - D2",
			District:       "Bình Thạnh",
			FloodMechanism: "compound",
			TypicalDepthCm: 35,
		},
		{
			CameraID:       "cam-flood-2",
			CameraName:     "Huỳnh Tấn Phát",
			District:       "District 7",
			FloodMechanism: "tidal",
			TypicalDepthCm: 45,
		},
	}
}

func TestLoadChokepoints(t *testing.T) {
	chokepoints, err := flood.LoadChokepoints("../../../../tools/eval/fixtures/flood_chokepoints.json")
	if err != nil {
		t.Fatalf("failed to load flood chokepoints: %v", err)
	}
	if len(chokepoints) != 10 {
		t.Fatalf("expected 10 chokepoints, got %d", len(chokepoints))
	}
	for _, cp := range chokepoints {
		if cp.CameraID == "" || cp.CameraName == "" || cp.Citation == "" {
			t.Errorf("chokepoint missing required fields: %+v", cp)
		}
	}
}

func TestService_VerifyCamera_Success(t *testing.T) {
	analyzer := &mockAnalyzer{
		verdictToReturn: hotset.Verdict{
			FloodSeverity: flood.SeverityFloodedShallow,
			Confidence:    0.92,
			Source:        "mock",
		},
	}
	weatherSvc := &mockWeather{weatherData: &weather.WeatherData{RainMm: 0.0}}
	camReader := newMockCameraReader(camera.Camera{
		ID:          "cam-flood-1",
		DisplayName: "Ung Văn Khiêm - D2",
		SnapshotURL: "https://example.com/snapshot.jpg",
	})
	limiter := flood.NewQuotaLimiter(10, 300)

	svc := flood.NewService(analyzer, weatherSvc, camReader, sampleChokepoints(), limiter)

	res, err := svc.VerifyCamera(context.Background(), "cam-flood-1")
	if err != nil {
		t.Fatalf("expected successful verification, got error: %v", err)
	}
	if res.CameraID != "cam-flood-1" {
		t.Errorf("expected CameraID cam-flood-1, got %s", res.CameraID)
	}
	if res.FloodSeverity != flood.SeverityFloodedShallow {
		t.Errorf("expected flood severity FLOODED_SHALLOW, got %s", res.FloodSeverity)
	}
	if res.Confidence != 0.92 {
		t.Errorf("expected confidence 0.92, got %f", res.Confidence)
	}
	if res.QuotaRemainingMinute != 9 {
		t.Errorf("expected quota remaining minute 9, got %d", res.QuotaRemainingMinute)
	}
	if res.QuotaRemainingDay != 299 {
		t.Errorf("expected quota remaining day 299, got %d", res.QuotaRemainingDay)
	}
	if analyzer.CallCount() != 1 {
		t.Errorf("expected analyzer to be called once, got %d", analyzer.CallCount())
	}
}

func TestService_VerifyCamera_NotFound(t *testing.T) {
	analyzer := &mockAnalyzer{}
	weatherSvc := &mockWeather{}
	camReader := newMockCameraReader()
	limiter := flood.NewQuotaLimiter(10, 300)

	svc := flood.NewService(analyzer, weatherSvc, camReader, sampleChokepoints(), limiter)

	_, err := svc.VerifyCamera(context.Background(), "non-existent")
	if err != flood.ErrCameraNotFound {
		t.Fatalf("expected ErrCameraNotFound, got %v", err)
	}
	if analyzer.CallCount() != 0 {
		t.Errorf("analyzer should not be called for non-existent camera")
	}
	if limiter.Status().RemainingRPM != 10 {
		t.Errorf("quota should not be consumed for non-existent camera")
	}
}

func TestService_VerifyCamera_RateLimited(t *testing.T) {
	analyzer := &mockAnalyzer{
		verdictToReturn: hotset.Verdict{FloodSeverity: flood.SeverityDry, Confidence: 0.9},
	}
	weatherSvc := &mockWeather{}
	camReader := newMockCameraReader(camera.Camera{
		ID:          "cam-flood-1",
		DisplayName: "Ung Văn Khiêm - D2",
	})
	limiter := flood.NewQuotaLimiter(1, 300) // RPM = 1

	svc := flood.NewService(analyzer, weatherSvc, camReader, sampleChokepoints(), limiter)

	// First call succeeds
	_, err := svc.VerifyCamera(context.Background(), "cam-flood-1")
	if err != nil {
		t.Fatalf("expected first call to succeed, got %v", err)
	}

	// Second call should fail with ErrRateLimited
	_, err = svc.VerifyCamera(context.Background(), "cam-flood-1")
	if err != flood.ErrRateLimited {
		t.Fatalf("expected ErrRateLimited, got %v", err)
	}
	if analyzer.CallCount() != 1 {
		t.Errorf("expected analyzer to be called only once, got %d", analyzer.CallCount())
	}
}

func TestService_EnvironmentalTrigger_DryWeather_ZeroCalls(t *testing.T) {
	analyzer := &mockAnalyzer{}
	weatherSvc := &mockWeather{
		weatherData: &weather.WeatherData{
			RainMm:      0.0,
			WeatherCode: 0, // Clear sky
		},
	}
	camReader := newMockCameraReader(
		camera.Camera{ID: "cam-flood-1", DisplayName: "Ung Văn Khiêm"},
		camera.Camera{ID: "cam-flood-2", DisplayName: "Huỳnh Tấn Phát"},
	)
	limiter := flood.NewQuotaLimiter(10, 300)

	svc := flood.NewService(analyzer, weatherSvc, camReader, sampleChokepoints(), limiter)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var verdictCount atomic.Int32
	onVerdict := func(v hotset.Verdict) {
		verdictCount.Add(1)
	}

	// Run environmental check for 100ms with 20ms poll interval
	go svc.StartEnvironmentalTrigger(ctx, 20*time.Millisecond, onVerdict)
	time.Sleep(80 * time.Millisecond)
	cancel()

	if analyzer.CallCount() != 0 {
		t.Errorf("expected 0 calls during dry weather, got %d", analyzer.CallCount())
	}
	if verdictCount.Load() != 0 {
		t.Errorf("expected 0 verdicts emitted during dry weather, got %d", verdictCount.Load())
	}
}

func TestService_EnvironmentalTrigger_RainWeather_PollsChokepoints(t *testing.T) {
	analyzer := &mockAnalyzer{
		verdictToReturn: hotset.Verdict{
			FloodSeverity: flood.SeverityWet,
			Confidence:    0.85,
			Source:        "mock",
		},
	}
	weatherSvc := &mockWeather{
		weatherData: &weather.WeatherData{
			RainMm:      2.5, // Exceeds 1.0mm rain threshold
			WeatherCode: 61,  // Rain
		},
	}
	camReader := newMockCameraReader(
		camera.Camera{ID: "cam-flood-1", DisplayName: "Ung Văn Khiêm"},
		camera.Camera{ID: "cam-flood-2", DisplayName: "Huỳnh Tấn Phát"},
	)
	limiter := flood.NewQuotaLimiter(10, 300)

	svc := flood.NewService(analyzer, weatherSvc, camReader, sampleChokepoints(), limiter)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var verdictCount atomic.Int32
	onVerdict := func(v hotset.Verdict) {
		verdictCount.Add(1)
	}

	go svc.StartEnvironmentalTrigger(ctx, 30*time.Millisecond, onVerdict)
	time.Sleep(50 * time.Millisecond)
	cancel()

	if analyzer.CallCount() == 0 {
		t.Errorf("expected analyzer calls during rain event, got 0")
	}
	if verdictCount.Load() == 0 {
		t.Errorf("expected verdicts emitted during rain event, got 0")
	}
}
