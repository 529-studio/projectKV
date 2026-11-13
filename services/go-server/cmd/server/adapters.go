package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/529-studio/projectKV/services/go-server/internal/camera"
	"github.com/529-studio/projectKV/services/go-server/internal/config"
	"github.com/529-studio/projectKV/services/go-server/internal/cvclient"
	"github.com/529-studio/projectKV/services/go-server/internal/feedback"
	"github.com/529-studio/projectKV/services/go-server/internal/flood"
	"github.com/529-studio/projectKV/services/go-server/internal/forecast"
	"github.com/529-studio/projectKV/services/go-server/internal/harvester"
	"github.com/529-studio/projectKV/services/go-server/internal/hotset"
	"github.com/529-studio/projectKV/services/go-server/internal/httpserver"
	"github.com/529-studio/projectKV/services/go-server/internal/live"
	"github.com/529-studio/projectKV/services/go-server/internal/notify"
	"github.com/529-studio/projectKV/services/go-server/internal/proxycache"
	"github.com/529-studio/projectKV/services/go-server/internal/storage"
	"github.com/529-studio/projectKV/services/go-server/internal/store"
	"github.com/529-studio/projectKV/services/go-server/internal/subscription"
	"github.com/529-studio/projectKV/services/go-server/internal/telegrambot"
	"github.com/529-studio/projectKV/services/go-server/internal/weather"
)

// verdictStoreAdapter adapts *store.VerdictRepo to hotset.VerdictStorer
// (Dependency Inversion Principle: consumer package hotset defines the interface,
// composition root adapts the persistent store).
type verdictStoreAdapter struct {
	repo *store.VerdictRepo
}

// hotsetVerdictFromStore converts a persisted verdict into the in-memory/API shape.
//
// It is the single mapping used both by the GET endpoints (verdictStoreAdapter) and by
// the boot-time hydration of the in-memory store. Duplicating that mapping is how a new
// column silently reaches SQLite and never reaches the wire (Issue #401 AC11).
func hotsetVerdictFromStore(v store.Verdict) hotset.Verdict {
	return hotset.Verdict{
		CameraID:             v.CameraID,
		Severity:             v.Severity,
		FloodSeverity:        v.FloodSeverity,
		Confidence:           v.Confidence,
		Source:               v.Source,
		ObservedAt:           v.ObservedAt,
		Timestamp:            v.Timestamp,
		FloodDepthCm:         v.FloodDepthCm,
		FloodHazardLevel:     v.FloodHazardLevel,
		FloodHazardBasis:     v.FloodHazardBasis,
		FloodDepthConfidence: v.FloodDepthConfidence,
		FloodCalibrationRef:  v.FloodCalibrationRef,
	}
}

func (a *verdictStoreAdapter) GetLatest() []hotset.Verdict {
	if a.repo == nil {
		return []hotset.Verdict{}
	}
	verdicts, err := a.repo.GetLatest(context.Background())
	if err != nil {
		log.Printf("[WARN] Failed to query latest verdicts: %v", err)
		return []hotset.Verdict{}
	}
	out := make([]hotset.Verdict, len(verdicts))
	for i, v := range verdicts {
		out[i] = hotsetVerdictFromStore(v)
	}
	return out
}

// forecastHistoryAdapter adapts *store.VerdictHistoryRepo to forecast.HistoryReader
// (Dependency Inversion Principle: consumer package forecast defines the interface,
// composition root adapts the persistent store).
type forecastHistoryAdapter struct {
	repo *store.VerdictHistoryRepo
}

func (a *forecastHistoryAdapter) GetRecentByCamera(ctx context.Context, cameraID string, window time.Duration, limit int) ([]forecast.Observation, error) {
	return a.GetRecentByCameraBefore(ctx, cameraID, time.Now().UTC(), window, limit)
}

func (a *forecastHistoryAdapter) GetRecentByCameraBefore(ctx context.Context, cameraID string, before time.Time, window time.Duration, limit int) ([]forecast.Observation, error) {
	if a == nil || a.repo == nil {
		return nil, fmt.Errorf("verdict history repo is nil")
	}
	rows, err := a.repo.GetRecentByCameraBefore(ctx, cameraID, before, window, limit)
	if err != nil {
		return nil, err
	}
	obs := make([]forecast.Observation, len(rows))
	for i, r := range rows {
		obs[i] = forecast.Observation{
			CameraID:        r.CameraID,
			PCUTotal:        r.PCUTotal,
			SaturationIndex: r.SaturationIndex,
			Severity:        r.Severity,
			Confidence:      r.Confidence,
			Source:          r.Source,
			ObservedAt:      r.ObservedAt,
		}
	}
	return obs, nil
}

// forecastCameraAdapter adapts camera.Reader to forecast.CameraCatalog.
type forecastCameraAdapter struct {
	catalog camera.Reader
}

func (a *forecastCameraAdapter) GetCamera(id string) (forecast.CameraInfo, bool) {
	if a == nil || a.catalog == nil {
		return forecast.CameraInfo{}, false
	}
	cam, ok := a.catalog.ByID(id)
	if !ok || cam == nil {
		return forecast.CameraInfo{}, false
	}
	return forecast.CameraInfo{
		ID:           cam.ID,
		RoadKey:      cam.RoadKey,
		RoadKeys:     cam.RoadKeys,
		BaseCapacity: cam.BaseCapacity,
	}, true
}

// subscriptionTaxonomyAdapter adapts camera.Reader to subscription.TaxonomyProvider
// (Dependency Inversion Principle: consumer package subscription defines the interface,
// composition root adapts the catalog).
type subscriptionTaxonomyAdapter struct {
	catalog camera.Reader
}

func (a *subscriptionTaxonomyAdapter) Zones() []string {
	if a == nil || a.catalog == nil {
		return subscription.CanonicalZones
	}
	return a.catalog.Zones()
}

func (a *subscriptionTaxonomyAdapter) Wards() []subscription.WardInfo {
	if a == nil || a.catalog == nil {
		return nil
	}
	phuongs := a.catalog.Phuongs()
	out := make([]subscription.WardInfo, len(phuongs))
	for i, p := range phuongs {
		out[i] = subscription.WardInfo{Type: p.Type, Name: p.Name}
	}
	return out
}

func (a *subscriptionTaxonomyAdapter) Chokepoints() []subscription.ChokepointInfo {
	if a == nil || a.catalog == nil {
		return nil
	}
	cams := a.catalog.All()
	out := make([]subscription.ChokepointInfo, 0, len(cams))
	for _, c := range cams {
		name := c.DisplayName
		if name == "" {
			name = c.Title
		}
		out = append(out, subscription.ChokepointInfo{ID: c.ID, Name: name})
	}
	return out
}

func (a *subscriptionTaxonomyAdapter) HasZone(zone string) bool {
	if a == nil || a.catalog == nil {
		for _, z := range subscription.CanonicalZones {
			if strings.EqualFold(z, zone) {
				return true
			}
		}
		return false
	}
	for _, z := range a.catalog.Zones() {
		if strings.EqualFold(z, zone) {
			return true
		}
	}
	return false
}

func (a *subscriptionTaxonomyAdapter) HasWard(ward string) bool {
	if a == nil || a.catalog == nil {
		return false
	}
	trimmed := strings.TrimSpace(ward)
	if trimmed == "" {
		return false
	}
	for _, p := range a.catalog.Phuongs() {
		if strings.EqualFold(p.Name, trimmed) || strings.EqualFold(p.Type+" "+p.Name, trimmed) {
			return true
		}
	}
	// Prefix-less check
	target := trimmed
	for _, prefix := range []string{"phường ", "xã ", "thị trấn "} {
		if strings.HasPrefix(strings.ToLower(target), prefix) {
			target = strings.TrimSpace(target[len(prefix):])
			break
		}
	}
	for _, p := range a.catalog.Phuongs() {
		if strings.EqualFold(p.Name, target) {
			return true
		}
	}
	return false
}

func (a *subscriptionTaxonomyAdapter) HasChokepoint(id string) bool {
	if a == nil || a.catalog == nil {
		return false
	}
	_, ok := a.catalog.ByID(id)
	return ok
}

// telegramBotStoreAdapter adapts the subscription SQLite store to
// telegrambot.Store (Dependency Inversion: the bot package imports no internal
// package, so the composition root maps the shapes, S12-T4 spec §4.1).
type telegramBotStoreAdapter struct {
	store *subscription.SQLiteStore
}

func (a *telegramBotStoreAdapter) Subscribe(ctx context.Context, channelName, chatID string, rec telegrambot.Record) error {
	return a.store.Subscribe(ctx, subscription.Subscription{
		ChannelName: channelName,
		ChatID:      chatID,
		TargetType:  subscription.TargetType(rec.TargetType),
		TargetID:    rec.TargetID,
		CreatedAt:   rec.CreatedAt,
	})
}

func (a *telegramBotStoreAdapter) UnsubscribeAll(ctx context.Context, channelName, chatID string) (int64, error) {
	return a.store.UnsubscribeAll(ctx, channelName, chatID)
}

func (a *telegramBotStoreAdapter) List(ctx context.Context, channelName, chatID string) ([]telegrambot.Record, error) {
	subs, err := a.store.ListSubscriptions(ctx, channelName, chatID)
	if err != nil {
		return nil, err
	}
	out := make([]telegrambot.Record, len(subs))
	for i, sub := range subs {
		out[i] = telegrambot.Record{
			TargetType: string(sub.TargetType),
			TargetID:   sub.TargetID,
			CreatedAt:  sub.CreatedAt,
		}
	}
	return out, nil
}

// telegramBotTaxonomyAdapter adapts the camera catalog to telegrambot.Taxonomy.
// Canonicalisation is the security-relevant part: a deep link carries a raw id,
// and only a value that resolves against the live catalog may reach the store.
type telegramBotTaxonomyAdapter struct {
	catalog camera.Reader
}

func (a *telegramBotTaxonomyAdapter) CanonicalZone(raw string) (string, bool) {
	canonical, ok := subscription.NormalizeZone(raw)
	if !ok {
		return "", false
	}
	if a == nil || a.catalog == nil {
		return canonical, true
	}
	for _, zone := range a.catalog.Zones() {
		if strings.EqualFold(zone, canonical) {
			return zone, true
		}
	}
	return "", false
}

func (a *telegramBotTaxonomyAdapter) CanonicalWard(raw string) (string, bool) {
	if a == nil || a.catalog == nil {
		return "", false
	}
	name := stripWardPrefix(strings.TrimSpace(raw))
	if name == "" {
		return "", false
	}
	for _, phuong := range a.catalog.Phuongs() {
		if strings.EqualFold(phuong.Name, name) {
			return phuong.Name, true
		}
	}
	return "", false
}

func (a *telegramBotTaxonomyAdapter) CanonicalChokepoint(id string) (string, bool) {
	if a == nil || a.catalog == nil {
		return "", false
	}
	cam, ok := a.catalog.ByID(strings.TrimSpace(id))
	if !ok || cam == nil {
		return "", false
	}
	return cam.ID, true
}

func (a *telegramBotTaxonomyAdapter) Label(target telegrambot.Target) string {
	switch target.Type {
	case "all":
		return ""
	case "zone":
		if canonical, ok := a.CanonicalZone(target.ID); ok {
			return canonical
		}
	case "ward":
		if canonical, ok := a.CanonicalWard(target.ID); ok {
			return canonical
		}
	case "chokepoint":
		if a != nil && a.catalog != nil {
			if cam, ok := a.catalog.ByID(target.ID); ok && cam != nil {
				if cam.DisplayName != "" {
					return cam.DisplayName
				}
				if cam.Title != "" {
					return cam.Title
				}
				return cam.ID
			}
		}
	}
	return strings.TrimSpace(target.ID)
}

// stripWardPrefix tolerates "Phường ", "Xã " and "Thị trấn " prefixes from a
// deep link; the stored value is always the bare administrative name (spec E3).
func stripWardPrefix(raw string) string {
	lower := strings.ToLower(raw)
	for _, prefix := range []string{"phường ", "xã ", "thị trấn "} {
		if strings.HasPrefix(lower, prefix) {
			return strings.TrimSpace(raw[len(prefix):])
		}
	}
	return raw
}

// telegramAPIBaseURL redirects outbound Bot API calls to a local capture
// endpoint when the verifier links it in with -ldflags. Empty in every shipped
// binary, so production always talks to api.telegram.org.
var telegramAPIBaseURL string

// criticalKeywords are used to prioritise well-known traffic choke points
// (e.g. An Suong, Cat Lai port cluster, My Thuy, major corridors) when
// seeding the Hot Set at startup. Vietnamese keywords are normalised to
// lowercase for case-insensitive matching.
var criticalKeywords = []string{
	"an sương", "cát lái", "mỹ thủy", "ngã 4 ga", "quốc lộ 1",
	"nguyễn văn linh", "điện biên phủ", "võ văn kiệt", "mai chí thọ",
	"cộng hòa", "phạm văn đồng", "cầu sài gòn", "xô viết nghệ tĩnh",
	"hành trình", "bình thái", "trường chinh", "quốc lộ 1", "ql1",
}

// priorityCorridors contains the priority arterial corridors confirmed in S1 Option 1b and S7-T5.
// Reference: docs/spike-vehicle-capacity.md §5 and Issue #250.
var priorityCorridors = map[string]struct{}{
	"vo_van_kiet":   {},
	"mai_chi_tho":   {},
	"dien_bien_phu": {},
	"cong_hoa":      {},
	"truong_chinh":  {},
	"quoc_lo_1":     {},
}

// buildFixedHotSet selects a deterministic set of critical cameras from the
// loaded catalog to seed the Hot Set registry at startup. This guarantees the
// mock/dev scheduler has cameras to analyse immediately, instead of waiting
// for a user to view/pin a feed.
func buildFixedHotSet(reader camera.Reader, maxCameras int) []hotset.Camera {
	if reader == nil {
		return nil
	}
	all := reader.All()
	if len(all) == 0 {
		return nil
	}

	// Filter out offline, ghost (catalog-sourced via audit resync), and
	// duplicate streams so the Hot Set only contains feeds that can actually
	// produce snapshots.
	eligible := make([]camera.Camera, 0, len(all))
	for _, c := range all {
		if !c.IsOnline || c.IsGhost || c.DuplicateOf != "" {
			continue
		}
		eligible = append(eligible, c)
	}

	var critical []hotset.Camera
	used := make(map[string]struct{}, len(eligible))

	// Pass 1: Prioritize S2 curated intersection cameras on S1 priority corridors (Option 1b).
	// Exactly 2 road_keys ensures intersection choke points are selected over mid-block cameras.
	for _, c := range eligible {
		if len(c.RoadKeys) == 2 {
			isPriority := false
			for _, rk := range c.RoadKeys {
				if _, ok := priorityCorridors[rk]; ok {
					isPriority = true
					break
				}
			}
			if isPriority {
				critical = append(critical, cameraToHotSet(c))
				used[c.ID] = struct{}{}
				if maxCameras > 0 && len(critical) >= maxCameras {
					return critical
				}
			}
		}
	}

	// Pass 2: Prefer remaining known critical choke points via criticalKeywords.
	for _, c := range eligible {
		if _, ok := used[c.ID]; ok {
			continue
		}
		name := strings.ToLower(c.DisplayName)
		if name == "" {
			name = strings.ToLower(c.Title)
		}
		for _, kw := range criticalKeywords {
			if strings.Contains(name, kw) {
				critical = append(critical, cameraToHotSet(c))
				used[c.ID] = struct{}{}
				break
			}
		}
		if maxCameras > 0 && len(critical) >= maxCameras {
			return critical
		}
	}

	// Pass 3: Pad with the remaining eligible cameras up to maxCameras.
	for _, c := range eligible {
		if _, ok := used[c.ID]; ok {
			continue
		}
		critical = append(critical, cameraToHotSet(c))
		used[c.ID] = struct{}{}
		if maxCameras > 0 && len(critical) >= maxCameras {
			break
		}
	}

	return critical
}

func cameraToHotSet(c camera.Camera) hotset.Camera {
	name := strings.TrimSpace(c.DisplayName)
	if name == "" {
		name = strings.TrimSpace(c.Title)
	}
	var hotsetRegions []hotset.RoadwayRegion
	if len(c.Regions) > 0 {
		hotsetRegions = make([]hotset.RoadwayRegion, len(c.Regions))
		for i, r := range c.Regions {
			hotsetRegions[i] = hotset.RoadwayRegion{
				RegionID:      r.RegionID,
				Direction:     r.Direction,
				Polygon:       r.Polygon,
				CapacityShare: r.CapacityShare,
			}
		}
	}
	return hotset.Camera{
		ID:                c.ID,
		Name:              name,
		Zone:              c.Zone,
		SnapshotURL:       c.SnapshotURL,
		BaseCapacity:      c.BaseCapacity,
		Regions:           hotsetRegions,
		DirectionCoverage: c.DirectionCoverage,
	}
}

type cameraTitleAdapter struct {
	catalog *camera.Catalog
}

func (a cameraTitleAdapter) GetCameraTitle(id string) string {
	if a.catalog != nil {
		if cam, ok := a.catalog.ByID(id); ok {
			if cam.DisplayName != "" {
				return cam.DisplayName
			}
			return cam.Title
		}
	}
	return ""
}

// harvesterWeatherAdapter adapts *weather.Service to harvester.WeatherProvider.
type harvesterWeatherAdapter struct {
	service *weather.Service
}

func (a *harvesterWeatherAdapter) GetWeather(ctx context.Context) (*harvester.WeatherInfo, error) {
	if a.service == nil {
		return nil, nil
	}
	w, err := a.service.GetWeather(ctx)
	if err != nil {
		return nil, err
	}
	if w == nil {
		return nil, nil
	}
	return &harvester.WeatherInfo{
		RainfallMm:  w.RainMm,
		WeatherCode: w.WeatherCode,
	}, nil
}

// weatherStoreAdapter adapts *store.WeatherRepo to weather.Persister (ADR-012 D3).
type weatherStoreAdapter struct {
	repo *store.WeatherRepo
}

func (a *weatherStoreAdapter) RecordObservation(ctx context.Context, obs weather.ObservationRecord) error {
	if a.repo == nil {
		return fmt.Errorf("weather store adapter: repo is nil")
	}
	return a.repo.Record(ctx, &store.WeatherObservation{
		PrecipitationMm: obs.PrecipitationMm,
		WeatherCode:     obs.WeatherCode,
		TemperatureC:    obs.TemperatureC,
		HumidityPct:     obs.HumidityPct,
		RadarActive:     obs.RadarActive,
		RadarFrameAgeS:  obs.RadarFrameAgeS,
		Source:          obs.Source,
		PayloadHash:     obs.PayloadHash,
		ObservedAt:      obs.ObservedAt,
		RecordedAt:      obs.RecordedAt,
	})
}

// harvesterSnapshotFetcher adapts proxycache.Cache and HTTP client to harvester.SnapshotFetcher.
type harvesterSnapshotFetcher struct {
	cache        *proxycache.Cache
	upstreamBase string
	client       *http.Client
}

func (f *harvesterSnapshotFetcher) FetchSnapshot(ctx context.Context, cam harvester.Camera) ([]byte, error) {
	if f.cache != nil {
		if data, ok := f.cache.Get(cam.ID); ok && len(data) > 0 {
			return data, nil
		}
	}
	snapURL := cam.SnapshotURL
	if snapURL == "" {
		snapURL = fmt.Sprintf("%s?id=%s", f.upstreamBase, cam.ID)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, snapURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; CameraViewHarvester/1.0)")
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("snapshot returned status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// feedbackCameraChecker adapts *camera.Catalog to feedback.CameraChecker.
type feedbackCameraChecker struct {
	catalog *camera.Catalog
}

func (c *feedbackCameraChecker) CameraExists(cameraID string) bool {
	if c.catalog == nil {
		return false
	}
	_, ok := c.catalog.ByID(cameraID)
	return ok
}

// feedbackSnapshotFetcher adapts proxycache.Cache and HTTP client to feedback.SnapshotFetcher.
type feedbackSnapshotFetcher struct {
	cache        *proxycache.Cache
	catalog      *camera.Catalog
	upstreamBase string
	client       *http.Client
}

func (f *feedbackSnapshotFetcher) FetchSnapshot(ctx context.Context, cameraID string) ([]byte, error) {
	if f.cache != nil {
		if data, ok := f.cache.Get(cameraID); ok && len(data) > 0 {
			return data, nil
		}
	}
	var snapURL string
	if f.catalog != nil {
		if cam, ok := f.catalog.ByID(cameraID); ok && cam.SnapshotURL != "" {
			snapURL = cam.SnapshotURL
		}
	}
	if snapURL == "" {
		snapURL = fmt.Sprintf("%s?id=%s", f.upstreamBase, cameraID)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, snapURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; CameraViewFeedback/1.0)")
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("snapshot returned status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// harvesterPredictorAdapter adapts CVEngine /predict endpoint to harvester.VisionPredictor.
type harvesterPredictorAdapter struct {
	url    string
	client *http.Client
}

func (p *harvesterPredictorAdapter) Predict(ctx context.Context, cam harvester.Camera, frame []byte) (*harvester.PredictionResult, error) {
	if p.url == "" {
		return &harvester.PredictionResult{
			TrafficSeverity: "CLEAR",
			Confidence:      0.8,
		}, nil
	}
	predictURL := p.url + "/predict"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, predictURL, bytes.NewReader(frame))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("predict endpoint returned status %d", resp.StatusCode)
	}
	var parsed struct {
		TrafficSeverity  string                  `json:"traffic_severity"`
		Confidence       float64                 `json:"confidence"`
		VehiclesDetected int                     `json:"vehicles_detected"`
		VehicleCounts    harvester.VehicleCounts `json:"vehicle_counts"`
		LatencyMs        float64                 `json:"latency_ms"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	return &harvester.PredictionResult{
		TrafficSeverity:  parsed.TrafficSeverity,
		Confidence:       parsed.Confidence,
		VehiclesDetected: parsed.VehiclesDetected,
		VehicleCounts:    parsed.VehicleCounts,
		LatencyMs:        parsed.LatencyMs,
	}, nil
}

// hotsetCoverageAdapter adapts store.VerdictHistoryRepo to hotset.HistoryCoverageProvider (ADR-012 D2).
type hotsetCoverageAdapter struct {
	repo *store.VerdictHistoryRepo
}

func (a *hotsetCoverageAdapter) GetCoverage(ctx context.Context, cameraID string, window time.Duration, now time.Time) (*hotset.CoverageReport, error) {
	if a.repo == nil {
		return nil, fmt.Errorf("history repo unavailable")
	}
	r, err := a.repo.GetCoverage(ctx, cameraID, window, now)
	if err != nil {
		return nil, err
	}
	return &hotset.CoverageReport{
		CameraID:             r.CameraID,
		Window:               r.Window,
		BinIntervalMinutes:   r.BinIntervalMinutes,
		ExpectedBins:         r.ExpectedBins,
		ObservedBins:         r.ObservedBins,
		CoverageRatio:        r.CoverageRatio,
		TotalObservations:    r.TotalObservations,
		ObservationBounds:    hotset.ObservationBounds{Earliest: r.ObservationBounds.Earliest, Latest: r.ObservationBounds.Latest},
		LongestGapSeconds:    r.LongestGapSeconds,
		HorizonEligibility:   r.HorizonEligibility,
		HistoryCoverageHours: r.HistoryCoverageHours,
		Status:               r.Status,
		CamerasCount:         r.CamerasCount,
	}, nil
}

// initAnalyzer selects and configures the computer vision analyzer adapter (ADR-004 H0).
func initAnalyzer(cfg *config.ServerConfig, camCatalog *camera.Catalog, db *sql.DB) (hotset.Analyzer, bool) {
	var analyzer hotset.Analyzer
	analysisEnabled := false

	switch cfg.Analyzer {
	case "mock":
		if cfg.AppEnv != "dev" {
			log.Fatalf("[FATAL] Refusing to start: ANALYZER=mock is strictly forbidden when APP_ENV is %q (only allowed in 'dev'). Fabricated mock verdicts must never run on non-dev environments.", cfg.AppEnv)
		}
		analyzer = cvclient.NewMockAnalyzer()
		analysisEnabled = true
		log.Println("[INIT] ANALYZER=mock active in 'dev' environment. Hot Set scheduler will run mock analysis.")
	case "vlm":
		if cfg.CloudVisionAPIKey == "" {
			log.Fatalf("[FATAL] Refusing to start: ANALYZER=vlm requires CLOUD_VISION_API_KEY to be set.")
		}
		analyzer = cvclient.NewCloudVisionAnalyzer(
			cfg.CloudVisionAPIKey,
			cvclient.WithModel(cfg.GeminiModel),
		)
		analysisEnabled = true
		log.Printf("[INIT] ANALYZER=vlm active (model=%s, interval=%ds, maxParallel=%d, fixedCameras=%d). Hot Set scheduler will run Gemini multimodal vision analysis.",
			cfg.GeminiModel, cfg.HotsetIntervalSeconds, cfg.HotsetMaxParallel, cfg.HotsetFixedMaxCameras)
	case "onnx":
		if cfg.CVEngineURL == "" {
			log.Fatalf("[FATAL] Refusing to start: ANALYZER=onnx requires CV_ENGINE_URL to be set.")
		}
		pcuConfig := cvclient.PCUConfig{
			Weights: cvclient.PCUWeights{
				Motorcycle: cfg.PCUWeightMotorcycle,
				Car:        cfg.PCUWeightCar,
				Bus:        cfg.PCUWeightBus,
				Truck:      cfg.PCUWeightTruck,
				Container:  cfg.PCUWeightContainer,
			},
			Thresholds: cvclient.PCUBucketThresholds{
				Moderate:  cfg.PCUThresholdModerate,
				Heavy:     cfg.PCUThresholdHeavy,
				Congested: cfg.PCUThresholdCongested,
			},
			CapacityFloor: cfg.PCUCapacityFloor,
		}
		pcuCalc := cvclient.NewPCUCalculator(pcuConfig)
		if camCatalog != nil {
			for _, cam := range camCatalog.All() {
				if cam.BaseCapacity > 0 {
					pcuCalc.SetBaseCapacity(cam.ID, cam.BaseCapacity)
				}
			}
		}
		if db != nil {
			capacityRepo := store.NewCapacityRepo(db)
			pcuCalc.SetCapacityStore(capacityRepo)
			if err := pcuCalc.LoadLearnedCapacities(context.Background()); err != nil {
				log.Printf("[WARN] Failed to load learned camera capacities from SQLite: %v", err)
			} else {
				log.Printf("[INIT] Loaded persistent learned camera capacities from SQLite")
			}
		}
		analyzer = cvclient.NewCVEngineAnalyzer(cfg.CVEngineURL, nil, cvclient.WithPCUCalculator(pcuCalc))
		analysisEnabled = true
		log.Printf("[INIT] ANALYZER=onnx active (url=%s, interval=%ds, maxParallel=%d, fixedCameras=%d, pcuFloor=%.1f). Hot Set scheduler will run self-hosted OpenVINO inference with corridor-specific PCU capacity.",
			cfg.CVEngineURL, cfg.HotsetIntervalSeconds, cfg.HotsetMaxParallel, cfg.HotsetFixedMaxCameras, cfg.PCUCapacityFloor)
	case "none", "":
		log.Println("[INIT] ANALYZER=none: Analysis scheduler is disabled. No camera_verdicts will be recorded and no SSE camera_update will be emitted.")
	default:
		log.Fatalf("[FATAL] Unknown ANALYZER=%q (valid options: none, mock, vlm, onnx)", cfg.Analyzer)
	}

	return analyzer, analysisEnabled
}

// cvEngineHealthProbe is implemented by analyzers that can report the health of
// their downstream inference engine (currently *cvclient.CVEngineAnalyzer).
type cvEngineHealthProbe interface {
	ProbeHealth(ctx context.Context) (cvclient.CVEngineHealth, error)
}

// initCVEngineHealthChecker adapts the analyzer's cv-engine health probe into
// an httpserver.HealthChecker. It returns nil when the selected analyzer has no
// downstream engine to probe (ANALYZER=none, mock or vlm), so /health reports
// healthy without claiming an engine is up.
func initCVEngineHealthChecker(analyzer hotset.Analyzer) httpserver.HealthChecker {
	probe, ok := analyzer.(cvEngineHealthProbe)
	if !ok {
		return nil
	}
	return httpserver.NewProbeHealthChecker(func(ctx context.Context) (httpserver.CVEngineHealth, error) {
		health, err := probe.ProbeHealth(ctx)
		return httpserver.CVEngineHealth{
			Status:            health.Status,
			Service:           health.Service,
			OpenVINOAvailable: health.OpenVINOAvailable,
			ModelLoaded:       health.ModelLoaded,
			LatencyMs:         health.LatencyMs,
			Error:             health.Error,
			CheckedAt:         health.CheckedAt,
		}, err
	})
}

// initCVEngineHealthProbe adapts the cv-engine health checker into the
// hotset.Watchdog's engine liveness probe (Issue #441 scope 4).
//
// "degraded" (the engine is up but reports model_loaded=false) cannot serve
// inference and therefore counts as unhealthy for alerting, even though /health
// still distinguishes it from an unreachable engine for diagnostics.
//
// It returns nil when no checker is wired, which leaves engine alerting off and
// feed alerting fully operational.
func initCVEngineHealthProbe(checker httpserver.HealthChecker) hotset.EngineProbe {
	if checker == nil {
		return nil
	}
	return func(ctx context.Context) (bool, string) {
		health := checker.CheckCVEngineHealth(ctx)
		if health.Status == httpserver.HealthStatusHealthy {
			return true, ""
		}
		status := health.Status
		if status == "" {
			status = httpserver.HealthStatusUnhealthy
		}
		if health.Error != "" {
			return false, status + ": " + health.Error
		}
		return false, status
	}
}

// initStorageDriver initializes the Cloudflare R2 or local filesystem object storage driver.
func initStorageDriver(cfg *config.ServerConfig) (storage.Storage, string) {
	var storeDriver storage.Storage
	var retentionRootDir string

	switch strings.ToLower(cfg.StorageDriver) {
	case "r2", "s3":
		r2Store, err := storage.NewR2Storage(context.Background(), storage.R2Config{
			Endpoint:        cfg.R2Endpoint,
			AccountID:       cfg.R2AccountID,
			BucketName:      cfg.R2BucketName,
			Region:          cfg.R2Region,
			AccessKeyID:     cfg.R2AccessKeyID,
			SecretAccessKey: cfg.R2SecretAccessKey,
		})
		if err != nil {
			log.Fatalf("[FATAL] Refusing to start: failed to initialize Cloudflare R2 storage driver: %v", err)
		}
		storeDriver = r2Store
		retentionRootDir = ""
		log.Printf("[INIT] Cloudflare R2 storage driver initialized (bucket=%s, endpoint=%s)",
			cfg.R2BucketName, cfg.R2Endpoint)
	case "local":
		fallthrough
	default:
		localStore, err := storage.NewLocalStorage(cfg.StorageLocalRoot)
		if err != nil {
			log.Printf("[WARN] Failed to initialize storage driver (%s): %v", cfg.StorageLocalRoot, err)
		} else {
			storeDriver = localStore
			retentionRootDir = cfg.StorageLocalRoot
			log.Printf("[INIT] Local storage driver initialized (root=%s)", cfg.StorageLocalRoot)
		}
	}
	return storeDriver, retentionRootDir
}

// initFeedbackHandler initializes the crowdsourced flood feedback handler and wires live runtime event bridges.
func initFeedbackHandler(
	camCatalog *camera.Catalog,
	proxyCache *proxycache.Cache,
	upstreamBase string,
	storeDriver storage.Storage,
	floodHexAggregator *live.FloodHexAggregator,
	liveHub *live.Hub,
) *feedback.Handler {
	feedbackChecker := &feedbackCameraChecker{catalog: camCatalog}
	feedbackFetcher := &feedbackSnapshotFetcher{
		cache:        proxyCache,
		catalog:      camCatalog,
		upstreamBase: upstreamBase,
		client:       cvclient.SharedClient,
	}
	feedbackLimiter := feedback.NewRateLimiter(5 * time.Minute)
	feedbackArchiver := feedback.NewArchiver(feedbackFetcher, storeDriver, nil)
	feedbackHandler := feedback.NewHandler(feedbackChecker, feedbackLimiter, feedbackArchiver)

	if feedbackHandler != nil {
		feedbackHandler.SetOnReportAccepted(func(cameraID string, report feedback.Report) {
			reportCount, escalated := 1, false
			if floodHexAggregator != nil {
				reportCount, escalated = floodHexAggregator.OnUserReport(cameraID, report.ClientIPHash, report.Timestamp)
			}
			if liveHub != nil {
				liveHub.BroadcastUserFloodReport(live.UserFloodReport{
					CameraID:     cameraID,
					ClientIPHash: report.ClientIPHash,
					Timestamp:    report.Timestamp,
					ReportCount:  reportCount,
					Escalated:    escalated,
					TTLSeconds:   900,
				})
			}
			slog.Info("feedback: crowdsource flood report bridged to live stream",
				"camera_id", cameraID,
				"report_count", reportCount,
				"escalated", escalated)
		})
	}
	return feedbackHandler
}

// initHarvesterCoordinator configures the autonomous harvester coordinator.
func initHarvesterCoordinator(
	cfg *config.ServerConfig,
	camCatalog *camera.Catalog,
	weatherService *weather.Service,
	proxyCache *proxycache.Cache,
	upstreamBase string,
	storeDriver storage.Storage,
) *harvester.Coordinator {
	if storeDriver == nil {
		log.Printf("[INIT] Autonomous harvester disabled: storage driver is uninitialized")
		return nil
	}

	var allHarvesterCameras []harvester.Camera
	if camCatalog != nil {
		for _, c := range camCatalog.All() {
			if c.IsOnline && !c.IsGhost && c.DuplicateOf == "" {
				allHarvesterCameras = append(allHarvesterCameras, harvester.Camera{
					ID:          c.ID,
					DisplayName: c.DisplayName,
					Title:       c.Title,
					SnapshotURL: c.SnapshotURL,
					RoadKeys:    c.RoadKeys,
					IsOnline:    c.IsOnline,
					IsGhost:     c.IsGhost,
				})
			}
		}
	}
	harvesterHotsetCams := harvester.FilterHotsetCameras(allHarvesterCameras)
	harvesterRainCams := harvester.FilterFloodCorridorCameras(allHarvesterCameras)

	harvesterCfg := harvester.Config{
		Enabled:             cfg.HarvesterEnabled,
		ScheduleHours:       cfg.HarvesterScheduleHours,
		RainCooldownMinutes: cfg.HarvesterRainCooldownMinutes,
		Concurrency:         cfg.HarvesterConcurrency,
		CatchupWindow:       time.Duration(cfg.HarvesterCatchupMinutes) * time.Minute,
	}

	var weatherProv harvester.WeatherProvider
	if weatherService != nil {
		weatherProv = &harvesterWeatherAdapter{service: weatherService}
	}

	snapFetcher := &harvesterSnapshotFetcher{
		cache:        proxyCache,
		upstreamBase: upstreamBase,
		client:       cvclient.SharedClient,
	}

	predictor := &harvesterPredictorAdapter{
		url:    cfg.CVEngineURL,
		client: cvclient.SharedClient,
	}

	coord, err := harvester.New(harvesterCfg, harvester.Dependencies{
		Weather:       weatherProv,
		Fetcher:       snapFetcher,
		Predictor:     predictor,
		Storage:       storeDriver,
		HotsetCameras: harvesterHotsetCams,
		RainCameras:   harvesterRainCams,
	})
	if err != nil {
		log.Printf("[WARN] Failed to initialize autonomous harvester: %v", err)
		return nil
	}
	if cfg.HarvesterEnabled {
		log.Printf("[INIT] Autonomous harvester enabled (scheduleSlots=%s, rainCooldown=%dm, concurrency=%d, hotsetCams=%d, rainCams=%d)",
			cfg.HarvesterScheduleHours, cfg.HarvesterRainCooldownMinutes, cfg.HarvesterConcurrency,
			len(harvesterHotsetCams), len(harvesterRainCams))
	} else {
		log.Printf("[INIT] Autonomous harvester disabled via configuration (HARVESTER_ENABLED=false)")
	}
	return coord
}

// loadFloodChokepoints loads authentic municipal flood chokepoints fixture.
func loadFloodChokepoints() (map[string]flood.FloodChokepoint, []flood.FloodChokepoint) {
	floodChokepointsPath := config.ResolvePath("tools/eval/fixtures/flood_chokepoints.json")
	floodChokepoints, err := flood.LoadChokepoints(floodChokepointsPath)
	chokepointMap := make(map[string]flood.FloodChokepoint)
	if err != nil {
		log.Printf("[WARN] Failed to load authentic flood chokepoints from %s: %v", floodChokepointsPath, err)
	} else {
		log.Printf("[INIT] Loaded %d authentic municipal flood chokepoints from %s", len(floodChokepoints), floodChokepointsPath)
		for _, cp := range floodChokepoints {
			chokepointMap[cp.CameraID] = cp
		}
	}
	return chokepointMap, floodChokepoints
}

// makeFloodAlertDispatcher constructs the flood alert dispatcher closure.
func makeFloodAlertDispatcher(
	camCatalog *camera.Catalog,
	chokepointMap map[string]flood.FloodChokepoint,
	notifyManager *notify.Manager,
) func(v hotset.Verdict) {
	return func(v hotset.Verdict) {
		if !notify.IsFloodSeverity(v.FloodSeverity) {
			return
		}

		camName := v.CameraID
		zone := ""
		ward := ""
		snapshotURL := ""
		if camCatalog != nil {
			if cam, ok := camCatalog.ByID(v.CameraID); ok {
				if cam.DisplayName != "" {
					camName = cam.DisplayName
				} else if cam.Title != "" {
					camName = cam.Title
				}
				zone = cam.Zone
				ward = cam.PhuongName
				snapshotURL = cam.SnapshotURL
			}
		}

		var typicalDepth int
		if cp, ok := chokepointMap[v.CameraID]; ok {
			typicalDepth = cp.TypicalDepthCm
		}

		obs := v.ObservedAt
		if obs.IsZero() {
			obs = v.Timestamp
		}

		alert := notify.BuildFloodAlert(
			v.CameraID,
			camName,
			zone,
			ward,
			snapshotURL,
			typicalDepth,
			v.FloodSeverity,
			obs,
		)

		go func() {
			dispatchCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := notifyManager.Dispatch(dispatchCtx, alert); err != nil {
				log.Printf("[WARN] Failed to dispatch flood alert for %s: %v", v.CameraID, err)
			}
		}()
	}
}

// initFloodPipeline sets up the decoupled multimodal flood pipeline and starts environmental polling.
func initFloodPipeline(
	appCtx context.Context,
	cfg *config.ServerConfig,
	camCatalog *camera.Catalog,
	weatherService *weather.Service,
	floodChokepoints []flood.FloodChokepoint,
	verdictRepo *store.VerdictRepo,
	floodHexAggregator *live.FloodHexAggregator,
	liveHub *live.Hub,
	dispatchFloodAlert func(hotset.Verdict),
) *flood.Handler {
	var floodAnalyzer hotset.Analyzer
	if cfg.CloudVisionAPIKey != "" {
		floodAnalyzer = cvclient.NewCloudVisionAnalyzer(
			cfg.CloudVisionAPIKey,
			cvclient.WithModel(cfg.GeminiModel),
		)
		log.Printf("[INIT] Flood service initialized with Gemini Cloud Vision analyzer (model=%s)", cfg.GeminiModel)
	} else if cfg.AppEnv == "dev" || cfg.Analyzer == "mock" {
		floodAnalyzer = cvclient.NewMockAnalyzer()
		log.Println("[INIT] Flood service initialized with Mock analyzer (dev mode)")
	}

	if floodAnalyzer == nil || camCatalog == nil {
		return nil
	}

	floodLimiter := flood.NewQuotaLimiter(10, 300)
	onFloodVerdict := func(v hotset.Verdict) {
		if verdictRepo != nil {
			sv := store.Verdict{
				CameraID:             v.CameraID,
				Severity:             v.Severity,
				FloodSeverity:        v.FloodSeverity,
				Confidence:           v.Confidence,
				Source:               v.Source,
				ObservedAt:           v.ObservedAt,
				Timestamp:            v.Timestamp,
				FloodDepthCm:         v.FloodDepthCm,
				FloodHazardLevel:     v.FloodHazardLevel,
				FloodHazardBasis:     v.FloodHazardBasis,
				FloodDepthConfidence: v.FloodDepthConfidence,
				FloodCalibrationRef:  v.FloodCalibrationRef,
			}
			if err := verdictRepo.Save(context.Background(), sv); err != nil {
				log.Printf("[WARN] Failed to persist flood verdict for %s: %v", v.CameraID, err)
			}
		}
		if floodHexAggregator != nil {
			floodHexAggregator.OnVerdict(v)
		}
		if liveHub != nil {
			liveHub.Broadcast(v)
		}
		if dispatchFloodAlert != nil {
			dispatchFloodAlert(v)
		}
	}
	floodService := flood.NewService(
		floodAnalyzer,
		weatherService,
		camCatalog,
		floodChokepoints,
		floodLimiter,
		flood.WithOnVerdict(onFloodVerdict),
	)

	startFloodEnvironmentalTrigger(appCtx, floodService, 60*time.Second, onFloodVerdict)
	return flood.NewHandler(floodService)
}

// makeProxyTouchCallback builds the cache-hit touch callback bridging proxycache and hotset.
func makeProxyTouchCallback(camCatalog *camera.Catalog, hotsetRegistry *hotset.Registry, upstreamBase string) func(id, name, zone, snapshotURL string) {
	return func(id, name, zone, snapshotURL string) {
		if camCatalog != nil {
			if cam, ok := camCatalog.ByID(id); ok {
				hotsetRegistry.Touch(cameraToHotSet(*cam))
				return
			}
		}
		if snapshotURL == "" {
			snapshotURL = fmt.Sprintf("%s?id=%s", upstreamBase, id)
		}
		hotsetRegistry.Touch(hotset.Camera{ID: id, Name: name, Zone: zone, SnapshotURL: snapshotURL})
	}
}

// initForecastHandler configures the short-horizon congestion forecast service and starts its background reloader.
func initForecastHandler(ctx context.Context, cfg *config.ServerConfig, camCatalog *camera.Catalog, verdictHistoryRepo *store.VerdictHistoryRepo) *forecast.Handler {
	if camCatalog == nil {
		return nil
	}
	var forecastHistAdapter forecast.HistoryReader
	if verdictHistoryRepo != nil {
		forecastHistAdapter = &forecastHistoryAdapter{repo: verdictHistoryRepo}
	}
	forecastCamAdapter := &forecastCameraAdapter{catalog: camCatalog}

	envAllowSynth := strings.EqualFold(os.Getenv("FORECAST_ALLOW_SYNTHETIC"), "true") || os.Getenv("FORECAST_ALLOW_SYNTHETIC") == "1"
	if envAllowSynth {
		if cfg.AppEnv == "staging" || cfg.AppEnv == "production" {
			log.Fatalf("FATAL: FORECAST_ALLOW_SYNTHETIC is prohibited in %s environment", cfg.AppEnv)
		}
		slog.Warn("FORECAST_ALLOW_SYNTHETIC is enabled (dev escape hatch); predictions are marked experimental")
	}

	var forecastOpts []forecast.Option
	if modelPath := strings.TrimSpace(os.Getenv("FORECAST_MODEL_PATH")); modelPath != "" {
		forecastOpts = append(forecastOpts, forecast.WithModelPath(modelPath))
	}
	forecastService := forecast.NewService(forecastHistAdapter, forecastCamAdapter, forecastOpts...)
	startForecastReloaderJob(ctx, forecastService)
	return forecast.NewHandler(forecastService)
}

// initHexAggregators loads hex polygons and initializes both traffic and flood H3 aggregators.
func initHexAggregators(cfg *config.ServerConfig, camCatalog *camera.Catalog, liveHub *live.Hub) (*live.HexAggregator, *live.FloodHexAggregator) {
	var hexPolygons map[string][][]float64
	if hexPolygonsData, err := live.LoadPolygons(cfg.HexPolygonFilePath); err != nil {
		log.Printf("[WARN] Failed to load H3 hex polygon lookup from %s: %v", cfg.HexPolygonFilePath, err)
	} else {
		hexPolygons = hexPolygonsData
		log.Printf("[INIT] Loaded %d H3 hex polygon boundaries from %s", len(hexPolygons), cfg.HexPolygonFilePath)
	}

	var allCameras []camera.Camera
	if camCatalog != nil {
		allCameras = camCatalog.All()
	}
	hexAgg := live.NewHexAggregator(allCameras, hexPolygons, cfg.HexResolution, cfg.HexMinCameras, liveHub, nil)
	if cfg.AppEnv == "dev" && cfg.Analyzer == "mock" {
		hexAgg.SetAllowMock(true)
		log.Println("[INIT] Hex cell aggregation enabled for mock analyzer in 'dev' environment.")
	}

	floodHexAgg := live.NewFloodHexAggregator(allCameras, hexPolygons, cfg.HexResolution, 1, liveHub, nil)
	floodStaleTTL := time.Duration(cfg.VerdictStaleTTLSeconds) * time.Second
	if floodStaleTTL <= 0 {
		floodStaleTTL = 180 * time.Second
	}
	floodHexAgg.SetStaleTTL(floodStaleTTL)
	if cfg.AppEnv == "dev" && cfg.Analyzer == "mock" {
		floodHexAgg.SetAllowMock(true)
		log.Println("[INIT] Flood hex cell aggregation enabled for mock analyzer in 'dev' environment.")
	}
	liveHub.SetFloodHexAggregator(floodHexAgg)

	return hexAgg, floodHexAgg
}
