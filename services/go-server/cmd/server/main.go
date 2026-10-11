package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	_ "github.com/529-studio/projectKV/services/go-server/docs"
	"github.com/529-studio/projectKV/services/go-server/internal/archiver"
	"github.com/529-studio/projectKV/services/go-server/internal/camera"
	"github.com/529-studio/projectKV/services/go-server/internal/config"
	"github.com/529-studio/projectKV/services/go-server/internal/corridor"
	"github.com/529-studio/projectKV/services/go-server/internal/cvclient"
	"github.com/529-studio/projectKV/services/go-server/internal/flood"
	"github.com/529-studio/projectKV/services/go-server/internal/hotset"
	"github.com/529-studio/projectKV/services/go-server/internal/httpserver"
	"github.com/529-studio/projectKV/services/go-server/internal/live"
	"github.com/529-studio/projectKV/services/go-server/internal/middleware"
	"github.com/529-studio/projectKV/services/go-server/internal/proxycache"
	"github.com/529-studio/projectKV/services/go-server/internal/storage"
	"github.com/529-studio/projectKV/services/go-server/internal/store"
	"github.com/529-studio/projectKV/services/go-server/internal/tilecache"
	"github.com/529-studio/projectKV/services/go-server/internal/weather"
	"github.com/529-studio/projectKV/services/go-server/internal/ws"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

// verdictStoreAdapter adapts *store.VerdictRepo to hotset.VerdictStorer
// (Dependency Inversion Principle: consumer package hotset defines the interface,
// composition root adapts the persistent store).
type verdictStoreAdapter struct {
	repo *store.VerdictRepo
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
		out[i] = hotset.Verdict{
			CameraID:      v.CameraID,
			Severity:      v.Severity,
			FloodSeverity: v.FloodSeverity,
			Confidence:    v.Confidence,
			Source:        v.Source,
			ObservedAt:    v.ObservedAt,
			Timestamp:     v.Timestamp,
		}
	}
	return out
}

// criticalKeywords are used to prioritise well-known traffic choke points
// (e.g. An Suong, Cat Lai port cluster, My Thuy, major corridors) when
// seeding the Hot Set at startup. Vietnamese keywords are normalised to
// lowercase for case-insensitive matching.
var criticalKeywords = []string{
	"an sương", "cát lái", "mỹ thủy", "ngã 4 ga", "quốc lộ 1",
	"nguyễn văn linh", "điện biên phủ", "võ văn kiệt", "mai chí thọ",
	"cộng hòa", "phạm văn đồng", "cầu sài gòn", "xô viết nghệ tĩnh",
	"hành trình", "bình thái",
}

// priorityCorridors contains the 4 priority corridors confirmed in Sprint 3 S1 (Option 1b).
// Reference: docs/spike-vehicle-capacity.md §5 and Issue #136.
var priorityCorridors = map[string]struct{}{
	"vo_van_kiet":   {},
	"mai_chi_tho":   {},
	"dien_bien_phu": {},
	"cong_hoa":      {},
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
	return hotset.Camera{
		ID:          c.ID,
		Name:        name,
		Zone:        c.Zone,
		SnapshotURL: c.SnapshotURL,
	}
}

func main() {
	cfg := config.LoadConfig()

	// Release mode in production
	if os.Getenv("GIN_MODE") == "release" {
		gin.SetMode(gin.ReleaseMode)
	}

	// 1. Load camera catalog into memory
	log.Printf("[INIT] Loading cameras from: %s", cfg.CamerasFilePath)
	camCatalog, err := camera.LoadCatalog(cfg.CamerasFilePath)
	if err != nil {
		log.Fatalf("[FATAL] Failed to load camera catalog: %v", err)
	}
	camCatalogHandler := camera.NewHandler(camCatalog)

	// 2. Initialize HotSet registry for active camera tracking.
	// activeTTL = 120 s; seed fixed critical cameras when analysis is enabled
	// (e.g. mock/dev mode) so the scheduler has work immediately at startup.
	var fixedCameras []hotset.Camera
	if camCatalog != nil && cfg.HotsetFixedMaxCameras > 0 {
		fixedCameras = buildFixedHotSet(camCatalog, cfg.HotsetFixedMaxCameras)
	}
	hotsetRegistry := hotset.NewRegistry(fixedCameras, 120*time.Second)

	// 3. Initialize proxycache with TTL cache and periodic background purge (Issue #124)
	proxyCache := proxycache.NewCache(cfg.CacheTTL)
	stopPurgeCh := make(chan struct{})
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stopPurgeCh:
				return
			case <-ticker.C:
				proxyCache.Purge()
			}
		}
	}()

	// 4. proxycache handler — wraps the DOT portal proxy with TTL cache and
	// HotSet Touch (Dependency Inversion: Touch is injected as a callback so
	// proxycache does not import hotset directly).
	upstreamBase := os.Getenv("SNAPSHOT_UPSTREAM_BASE_URL")
	if upstreamBase == "" {
		upstreamBase = "https://giaothong.hochiminhcity.gov.vn/render/ImageHandler.ashx"
	}
	proxyHandler := proxycache.NewHandler(proxyCache, upstreamBase, func(id, name, zone, snapshotURL string) {
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
	})

	// Initialize tilecache Manager and Handler for self-hosted map tiles
	tileManager := tilecache.NewManager(cfg.TileCacheDir)
	tileHandler := tilecache.NewHandler(tileManager)

	// 5. Initialize SQLite DB and Verdict Repository
	db, err := store.NewDB(cfg.SQLitePath)
	var verdictRepo *store.VerdictRepo
	if err != nil {
		log.Printf("[WARN] Failed to initialize SQLite database (%s): %v", cfg.SQLitePath, err)
	} else {
		verdictRepo = store.NewVerdictRepo(db)
		defer db.Close()
	}

	// 5b. Analyzer selection and environment validation (ADR-004 H0)
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
		analyzer = cvclient.NewCVEngineAnalyzer(cfg.CVEngineURL, nil, cvclient.WithPCUCalculator(pcuCalc))
		analysisEnabled = true
		log.Printf("[INIT] ANALYZER=onnx active (url=%s, interval=%ds, maxParallel=%d, fixedCameras=%d, pcuFloor=%.1f). Hot Set scheduler will run self-hosted OpenVINO inference with PCU-weighted Saturation Index.",
			cfg.CVEngineURL, cfg.HotsetIntervalSeconds, cfg.HotsetMaxParallel, cfg.HotsetFixedMaxCameras, cfg.PCUCapacityFloor)
	case "none", "":
		log.Println("[INIT] ANALYZER=none: Analysis scheduler is disabled. No camera_verdicts will be recorded and no SSE camera_update will be emitted.")
	default:
		log.Fatalf("[FATAL] Unknown ANALYZER=%q (valid options: none, mock, vlm, onnx)", cfg.Analyzer)
	}

	frozenDetector := hotset.NewFrozenDetector(cfg.FrozenFrameThreshold)
	hotsetHandler := hotset.NewHandlerWithConfig(&verdictStoreAdapter{repo: verdictRepo}, analysisEnabled, cfg.AppEnv)
	hotsetHandler.SetStaleTTL(time.Duration(cfg.VerdictStaleTTLSeconds) * time.Second)

	var corridorHandler *corridor.Handler
	if camCatalog != nil {
		corridorService := corridor.NewService(
			camCatalog,
			&verdictStoreAdapter{repo: verdictRepo},
			cfg.AppEnv,
			time.Duration(cfg.VerdictStaleTTLSeconds)*time.Second,
		)
		corridorHandler = corridor.NewHandler(corridorService)
	}

	// 5c. Initialize live weather forecast and RainViewer radar metadata service (Issue #120).
	weatherService := weather.NewService()
	weatherHandler := weather.NewHandler(weatherService)

	// 6. Initialize WebSocket Hub for real-time verdict streaming (Issue #9).
	// Kept in place — will be retired after D1 (EventSource migration) is verified.
	wsHub := ws.NewHub()
	defer wsHub.Close()

	// 6b. Initialize SSE Hub (ADR-003 Decision 1 — replaces WebSocket for new clients).
	// internal/live broadcasts camera_update events to all EventSource subscribers.
	liveHub := live.NewHub()
	defer liveHub.Close()

	// 6c. Initialize H3 Hex Aggregator for aggregated spatial overlay (Sprint 2 Issue #73).
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
	hexAggregator := live.NewHexAggregator(allCameras, hexPolygons, cfg.HexResolution, cfg.HexMinCameras, liveHub, nil)
	if cfg.AppEnv == "dev" && cfg.Analyzer == "mock" {
		hexAggregator.SetAllowMock(true)
		log.Println("[INIT] Hex cell aggregation enabled for mock analyzer in 'dev' environment.")
	}

	floodHexAggregator := live.NewFloodHexAggregator(allCameras, hexPolygons, cfg.HexResolution, 1, liveHub, nil)
	if cfg.AppEnv == "dev" && cfg.Analyzer == "mock" {
		floodHexAggregator.SetAllowMock(true)
		log.Println("[INIT] Flood hex cell aggregation enabled for mock analyzer in 'dev' environment.")
	}
	liveHub.SetFloodHexAggregator(floodHexAggregator)

	// Pre-seed aggregators with latest stored verdicts from SQLite so GET /api/flood-hex
	// and GET /api/traffic-status return cached data immediately on cold boot.
	if verdictRepo != nil {
		if storedVerdicts, err := verdictRepo.GetLatest(context.Background()); err == nil && len(storedVerdicts) > 0 {
			staleThreshold := time.Duration(cfg.VerdictStaleTTLSeconds) * time.Second
			if staleThreshold <= 0 {
				staleThreshold = 180 * time.Second
			}
			now := time.Now()
			for _, sv := range storedVerdicts {
				obs := sv.ObservedAt
				if obs.IsZero() {
					obs = sv.Timestamp
				}
				isStale := false
				if !obs.IsZero() && now.Sub(obs) > staleThreshold {
					isStale = true
				}
				hv := hotset.Verdict{
					CameraID:      sv.CameraID,
					Severity:      sv.Severity,
					FloodSeverity: sv.FloodSeverity,
					Confidence:    sv.Confidence,
					Source:        sv.Source,
					ObservedAt:    obs,
					Timestamp:     sv.Timestamp,
					Stale:         isStale,
				}
				hexAggregator.OnVerdict(hv)
				floodHexAggregator.OnVerdict(hv)
			}
			log.Printf("[INIT] Pre-seeded hex aggregators with %d stored verdicts from database", len(storedVerdicts))
		}
	}

	// 6c-2. Initialize Decoupled Multimodal Flood Pipeline (Issue #209 / S5-T3).
	// Strictly quota-guarded (10 RPM / 300 RPD) with dual triggers:
	// Trigger 1: background environmental rain event polling on authentic municipal chokepoints.
	// Trigger 2: on-demand commuter verification via POST /api/flood/verify.
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

	var floodHandler *flood.Handler
	if floodAnalyzer != nil && camCatalog != nil {
		floodChokepointsPath := config.ResolvePath("tools/eval/fixtures/flood_chokepoints.json")
		chokepoints, err := flood.LoadChokepoints(floodChokepointsPath)
		if err != nil {
			log.Printf("[WARN] Failed to load authentic flood chokepoints from %s: %v", floodChokepointsPath, err)
		} else {
			log.Printf("[INIT] Loaded %d authentic municipal flood chokepoints from %s", len(chokepoints), floodChokepointsPath)
		}
		floodLimiter := flood.NewQuotaLimiter(10, 300)
		onFloodVerdict := func(v hotset.Verdict) {
			if verdictRepo != nil {
				sv := store.Verdict{
					CameraID:      v.CameraID,
					Severity:      v.Severity,
					FloodSeverity: v.FloodSeverity,
					Confidence:    v.Confidence,
					Source:        v.Source,
					ObservedAt:    v.ObservedAt,
					Timestamp:     v.Timestamp,
				}
				if err := verdictRepo.Save(context.Background(), sv); err != nil {
					log.Printf("[WARN] Failed to persist flood verdict for %s: %v", v.CameraID, err)
				}
			}
			floodHexAggregator.OnVerdict(v)
			liveHub.Broadcast(v)
		}
		floodService := flood.NewService(
			floodAnalyzer,
			weatherService,
			camCatalog,
			chokepoints,
			floodLimiter,
			flood.WithOnVerdict(onFloodVerdict),
		)
		floodHandler = flood.NewHandler(floodService)

		// Start background environmental rain polling (Trigger 1)
		floodCtx, cancelFlood := context.WithCancel(context.Background())
		defer cancelFlood()
		go floodService.StartEnvironmentalTrigger(floodCtx, 60*time.Second, onFloodVerdict)
	}

	// 6d. Wire the Hot Set scheduler with OnVerdict only if analysis is enabled.
	var hotsetScheduler *hotset.Scheduler
	if analysisEnabled && analyzer != nil {
		// Initialize object storage and snapshot archiver (Issue #62, Cloudflare R2 integration)
		var arch *archiver.Archiver
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
				PublicBaseURL:   cfg.R2PublicURL,
			})
			if err != nil {
				log.Fatalf("[FATAL] Refusing to start: failed to initialize Cloudflare R2 storage driver: %v", err)
			}
			storeDriver = r2Store
			retentionRootDir = ""
			log.Printf("[INIT] Cloudflare R2 storage driver initialized (bucket=%s, endpoint=%s, publicURL=%s)",
				cfg.R2BucketName, cfg.R2Endpoint, cfg.R2PublicURL)
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

		if storeDriver != nil {
			archCfg := archiver.Config{
				SampleInterval: time.Duration(cfg.ArchiveSampleIntervalSeconds) * time.Second,
				RetentionDays:  cfg.ArchiveRetentionDays,
				MaxDiskMB:      cfg.ArchiveMaxDiskMB,
				CheckInterval:  cfg.ArchiveRetentionCheckInterval,
			}
			arch = archiver.New(storeDriver, retentionRootDir, archCfg, nil)

			if retentionRootDir != "" {
				retentionCtx, cancelRetention := context.WithCancel(context.Background())
				defer cancelRetention()
				go arch.StartRetention(retentionCtx)
				log.Printf("[INIT] Snapshot archiver enabled with local disk retention (root=%s, retention=%dd, maxDisk=%dMB, sampleInterval=%ds)",
					retentionRootDir, cfg.ArchiveRetentionDays, cfg.ArchiveMaxDiskMB, cfg.ArchiveSampleIntervalSeconds)
			} else {
				log.Printf("[INIT] Snapshot archiver enabled with Cloudflare R2 cloud storage (bucket=%s, sampleInterval=%ds)",
					cfg.R2BucketName, cfg.ArchiveSampleIntervalSeconds)
			}
		}

		onVerdict := func(v hotset.Verdict) {
			now := time.Now()
			if v.ObservedAt.IsZero() {
				v.ObservedAt = now
			}
			if v.Timestamp.IsZero() {
				v.Timestamp = v.ObservedAt
			}

			// Check frozen frame detector if cached frame data is available
			if cachedData, ok := proxyCache.Get(v.CameraID); ok && len(cachedData) > 0 {
				if frozenDetector.CheckBytes(v.CameraID, cachedData) {
					v.Stale = true
				}
			}

			// Fan-out 1: push to all SSE clients immediately.
			liveHub.Broadcast(v)

			// Fan-out 1b: push hex cell update if floor of qualifying cameras is met.
			if hexAggregator != nil {
				hexAggregator.OnVerdict(v)
			}

			// Fan-out 1c: push flood hex cell update if safety floor (>= 1 camera) is met.
			if floodHexAggregator != nil {
				floodHexAggregator.OnVerdict(v)
			}

			// Fan-out 2: persist to SQLite for GET /api/traffic-status hydration.
			if verdictRepo != nil {
				sv := store.Verdict{
					CameraID:      v.CameraID,
					Severity:      v.Severity,
					Confidence:    v.Confidence,
					Source:        v.Source,
					ObservedAt:    v.ObservedAt,
					Timestamp:     v.Timestamp,
					FloodSeverity: v.FloodSeverity,
				}
				if err := verdictRepo.Save(context.Background(), sv); err != nil {
					log.Printf("[WARN] Failed to persist verdict for %s: %v", v.CameraID, err)
				}
			}

			// Fan-out 3: event-driven snapshot archiver with retention policy (Issue #62, #182).
			if arch != nil {
				var frameReader io.Reader
				if len(v.RawFrame) > 0 {
					// Direct frame delivery from analysis pipeline (T2b / Issue #182)
					frameReader = bytes.NewReader(v.RawFrame)
				} else if cachedData, ok := proxyCache.Get(v.CameraID); ok && len(cachedData) > 0 {
					// Preserved fallback for non-HotSet or viewer-touched flows
					frameReader = bytes.NewReader(cachedData)
				} else if cfg.Analyzer == "mock" {
					// In mock analysis mode without pre-fetched proxy cache, provide a minimal mock JPEG
					// so dev environment starts collecting proof frames immediately.
					frameReader = bytes.NewReader(archiver.MockJPEGData)
				}
				if frameReader != nil {
					if err := arch.OnVerdict(v, frameReader); err != nil {
						log.Printf("[WARN] Archiver failed for %s: %v", v.CameraID, err)
					}
				}
			}
		}

		hotsetScheduler = hotset.NewScheduler(
			hotsetRegistry,
			analyzer,
			time.Duration(cfg.HotsetIntervalSeconds)*time.Second,
			cfg.HotsetMaxParallel,
			onVerdict,
			nil, // uses slog.Default()
		)

		// Global daily analyzer budget (ADR-004 §D9). When exhausted the
		// scheduler degrades to UNKNOWN verdicts instead of invoking the analyzer.
		analysisBudget := hotset.NewAnalysisBudget(cfg.AnalyzerDailyBudget)
		hotsetScheduler.SetBudget(analysisBudget)

		// Start the Hot Set scheduler in background; cancelled on graceful shutdown.
		schedulerCtx, cancelScheduler := context.WithCancel(context.Background())
		defer cancelScheduler()
		go hotsetScheduler.Run(schedulerCtx)
	}

	// 8. Setup Gin Engine
	router := gin.Default()
	router.Use(middleware.CORSMiddleware())
	router.Use(middleware.RateLimitMiddleware(cfg.RateLimitRPS, cfg.RateLimitBurst))

	// Health check probes
	router.GET("/health", httpserver.Healthz)
	router.HEAD("/health", httpserver.Healthz)
	router.GET("/healthz", httpserver.Healthz)

	// WebSocket Live Broadcast (Issue #9: GET /ws/live) — kept until D1 FE migration verified.
	wsHub.RegisterRoutes(router)

	// SSE Live Stream (ADR-003 Decision 1: GET /api/live/stream).
	liveHub.RegisterRoutes(router)

	// Swagger documentation UI
	swaggerHandler := ginSwagger.WrapHandler(swaggerFiles.Handler)
	router.GET("/swagger", func(c *gin.Context) {
		c.Redirect(http.StatusMovedPermanently, "/swagger/index.html")
	})
	router.GET("/swagger/*any", func(c *gin.Context) {
		p := c.Param("any")
		if p == "" || p == "/" {
			c.Redirect(http.StatusMovedPermanently, "/swagger/index.html")
			return
		}
		swaggerHandler(c)
	})

	// API Group
	api := router.Group("/api")
	{
		api.GET("/cameras", camCatalogHandler.GetCameras)
		api.HEAD("/cameras", camCatalogHandler.GetCameras)
		api.GET("/cameras/:id", camCatalogHandler.GetCameraByID)
		api.GET("/zones", camCatalogHandler.GetZones)
		api.GET("/phuong", camCatalogHandler.GetPhuongs)
		// Proxy cache snapshot routes (wraps proxycache.Handler)
		proxyHandler.RegisterRoutes(api)
		// Traffic and flood congestion status endpoints (Issue #8)
		hotsetHandler.RegisterRoutes(api)
		// Traffic corridor status endpoints (ADR-004 §D1 / Issue #75)
		if corridorHandler != nil {
			corridorHandler.RegisterRoutes(api)
		}
		// Live weather forecast & radar metadata endpoints (Issue #120)
		weatherHandler.RegisterRoutes(api)
		// Map tile proxy and local disk cache
		tileHandler.RegisterRoutes(api)
		// Decoupled multimodal flood pipeline endpoints (Issue #209)
		if floodHandler != nil {
			floodHandler.RegisterRoutes(api)
		}
		// Legacy alias kept for backward compat with existing FE constants.js
		api.GET("/cameras/snapshot/:id", proxyHandler.GetSnapshot)
		api.HEAD("/cameras/snapshot/:id", proxyHandler.GetSnapshot)
	}

	// 5. Static Web Serving
	if fi, err := os.Stat(cfg.StaticWebDir); err == nil && fi.IsDir() {
		log.Printf("[INIT] Serving static web UI from: %s", cfg.StaticWebDir)
		router.StaticFile("/", filepath.Join(cfg.StaticWebDir, "index.html"))
		router.StaticFile("/index.html", filepath.Join(cfg.StaticWebDir, "index.html"))
		router.StaticFile("/styles.css", filepath.Join(cfg.StaticWebDir, "styles.css"))
		router.StaticFile("/app.js", filepath.Join(cfg.StaticWebDir, "app.js"))

		nextDir := filepath.Join(cfg.StaticWebDir, "_next")
		if info, err := os.Stat(nextDir); err == nil && info.IsDir() {
			router.Static("/_next", nextDir)
		}

		// Handle any additional assets or SPA fallback
		router.NoRoute(func(c *gin.Context) {
			reqPath := c.Request.URL.Path
			if strings.HasPrefix(reqPath, "/api/") || strings.HasPrefix(reqPath, "/swagger") || strings.HasPrefix(reqPath, "/ws") {
				c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
				return
			}
			filePath := filepath.Join(cfg.StaticWebDir, filepath.Clean(reqPath))
			if info, err := os.Stat(filePath); err == nil && !info.IsDir() {
				c.File(filePath)
				return
			}
			c.File(filepath.Join(cfg.StaticWebDir, "index.html"))
		})
	} else {
		log.Printf("[WARN] Static web directory not found: %s", cfg.StaticWebDir)
	}

	// 6. HTTP Server with graceful shutdown
	serverAddr := fmt.Sprintf(":%s", cfg.Port)
	srv := &http.Server{
		Addr:    serverAddr,
		Handler: router,
	}

	go func() {
		log.Println("================================================================")
		log.Printf(" [OK] CAMERA VIEW GO-GIN SERVER RUNNING")
		log.Printf(" [URL] Available at: http://localhost:%s", cfg.Port)
		log.Printf(" [DOC] Swagger UI:   http://localhost:%s/swagger/index.html", cfg.Port)
		log.Printf(" [WS]  WebSocket:    ws://localhost:%s%s", cfg.Port, ws.RouteLive)
		log.Printf(" [SSE] Live stream:  http://localhost:%s%s", cfg.Port, live.RouteStream)
		totalCams := 0
		onlineCams := 0
		if camCatalog != nil {
			all := camCatalog.All()
			totalCams = len(all)
			for _, c := range all {
				if c.IsOnline {
					onlineCams++
				}
			}
		}
		log.Printf(" [CAM] Loaded %d cameras into memory (%d online)", totalCams, onlineCams)
		log.Println("================================================================")

		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("[FATAL] Server listen failed: %v", err)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("[SHUTDOWN] Shutting down Camera View server...")
	close(stopPurgeCh)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("[FATAL] Server forced to shutdown: %v", err)
	}

	log.Println("[SHUTDOWN] Server exited cleanly.")
}
