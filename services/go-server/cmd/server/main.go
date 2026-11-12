package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/529-studio/projectKV/services/go-server/internal/archiver"
	"github.com/529-studio/projectKV/services/go-server/internal/camera"
	"github.com/529-studio/projectKV/services/go-server/internal/config"
	"github.com/529-studio/projectKV/services/go-server/internal/corridor"
	"github.com/529-studio/projectKV/services/go-server/internal/hotset"
	"github.com/529-studio/projectKV/services/go-server/internal/httpserver"
	"github.com/529-studio/projectKV/services/go-server/internal/live"
	"github.com/529-studio/projectKV/services/go-server/internal/notify"
	"github.com/529-studio/projectKV/services/go-server/internal/proxycache"
	"github.com/529-studio/projectKV/services/go-server/internal/store"
	"github.com/529-studio/projectKV/services/go-server/internal/subscription"
	"github.com/529-studio/projectKV/services/go-server/internal/telegrambot"
	"github.com/529-studio/projectKV/services/go-server/internal/tilecache"
	"github.com/529-studio/projectKV/services/go-server/internal/weather"
	"github.com/529-studio/projectKV/services/go-server/internal/ws"
)

func main() {
	cfg := config.LoadConfig()
	if os.Getenv("GIN_MODE") == "release" {
		gin.SetMode(gin.ReleaseMode)
	}

	appCtx, appCancel := context.WithCancel(context.Background())
	defer appCancel()

	// 1. Load camera catalog into memory
	log.Printf("[INIT] Loading cameras from: %s", cfg.CamerasFilePath)
	camCatalog, err := camera.LoadCatalog(cfg.CamerasFilePath)
	if err != nil {
		log.Fatalf("[FATAL] Failed to load camera catalog: %v", err)
	}
	camCatalogHandler := camera.NewHandler(camCatalog)

	// 2. Initialize HotSet registry & proxycache with TTL purge job
	var fixedCameras []hotset.Camera
	if camCatalog != nil && cfg.HotsetFixedMaxCameras > 0 {
		fixedCameras = buildFixedHotSet(camCatalog, cfg.HotsetFixedMaxCameras)
	}
	hotsetRegistry := hotset.NewRegistry(fixedCameras, 120*time.Second)
	proxyCache := proxycache.NewCache(cfg.CacheTTL)
	startProxyCachePurge(appCtx, proxyCache)

	upstreamBase := os.Getenv("SNAPSHOT_UPSTREAM_BASE_URL")
	if upstreamBase == "" {
		upstreamBase = "https://giaothong.hochiminhcity.gov.vn/render/ImageHandler.ashx"
	}
	proxyHandler := proxycache.NewHandler(proxyCache, upstreamBase, makeProxyTouchCallback(camCatalog, hotsetRegistry, upstreamBase))
	tileHandler := tilecache.NewHandler(tilecache.NewManager(cfg.TileCacheDir))

	// 3. Initialize SQLite DB and Repositories
	db, err := store.NewDB(cfg.SQLitePath)
	if err != nil {
		log.Fatalf("[FATAL] Failed to initialize SQLite database (%s): %v", cfg.SQLitePath, err)
	}
	if err := store.AssertSchemaReady(db); err != nil {
		log.Fatalf("[FATAL] Database schema assertion failed (ADR-012 D2): %v", err)
	}
	defer db.Close()

	verdictRepo := store.NewVerdictRepo(db)
	verdictHistoryRepo := store.NewVerdictHistoryRepo(db)
	weatherRepo := store.NewWeatherRepo(db)
	subStore := subscription.NewSQLiteStore(db)
	startRetentionPurge(appCtx, verdictHistoryRepo)

	// 4. Notifications & Inbound Telegram bot
	debounceMins := cfg.TelegramDebounceMinutes
	if debounceMins <= 0 {
		debounceMins = 30
	}
	notifyManager := notify.NewManager(notify.NewDebouncer(time.Duration(debounceMins) * time.Minute))
	subFinder := notify.SubscriberFinderFunc(func(ctx context.Context, channelName string, alert notify.FloodAlert) ([]string, error) {
		if subStore == nil {
			return nil, nil
		}
		return subStore.GetSubscribersForChokepoint(ctx, channelName, alert.CameraID, alert.Zone, alert.Ward)
	})
	telegramOpts := make([]notify.TelegramOption, 0, 1)
	if telegramAPIBaseURL != "" {
		telegramOpts = append(telegramOpts, notify.WithBaseURL(telegramAPIBaseURL))
	}
	telegramChan := notify.NewTelegramChannel(cfg.TelegramBotToken, subFinder, telegramOpts...)
	notifyManager.Register(telegramChan)

	botUsername := cfg.TelegramBotUsername
	webhookEnabled := cfg.TelegramBotToken != "" &&
		telegrambot.ValidBotUsername(botUsername) &&
		telegrambot.ValidWebhookSecret(cfg.TelegramWebhookSecret)

	var taxonomyProv subscription.TaxonomyProvider
	if camCatalog != nil {
		taxonomyProv = &subscriptionTaxonomyAdapter{catalog: camCatalog}
	}
	subOpts := []subscription.Option{subscription.WithBotInfo(botUsername, webhookEnabled)}
	if cfg.TelegramBotToken != "" {
		subOpts = append(subOpts, subscription.WithConfirmationSender(telegramChan))
	}
	subHandler := subscription.NewHandler(subStore, taxonomyProv, subOpts...)
	botHandler := telegrambot.NewHandler(
		telegrambot.Config{BotUsername: botUsername, WebhookSecret: cfg.TelegramWebhookSecret},
		&telegramBotStoreAdapter{store: subStore},
		&telegramBotTaxonomyAdapter{catalog: camCatalog},
		telegramChan,
		slog.Default(),
	)
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := botHandler.Shutdown(shutdownCtx); err != nil {
			log.Printf("[WARN] Telegram bot shutdown: %v", err)
		}
	}()

	// 5. Flood chokepoints & real-time alert dispatcher
	chokepointMap, floodChokepoints := loadFloodChokepoints()
	dispatchFloodAlert := makeFloodAlertDispatcher(camCatalog, chokepointMap, notifyManager)

	// 6. Analyzer selection & Core Handlers (Hotset, Corridor, Forecast, Weather)
	analyzer, analysisEnabled := initAnalyzer(cfg, camCatalog, db)
	healthChecker := initCVEngineHealthChecker(analyzer)
	opsWatchdog := hotset.NewWatchdog(
		hotset.WithFeedDropoutThreshold(cfg.OpsAlertFeedDropoutThreshold),
		hotset.WithCVEngineFailureThreshold(cfg.OpsAlertCVEngineThreshold),
		hotset.WithWatchdogEvaluateInterval(cfg.OpsAlertEvaluateInterval),
	)
	opsWatchdog.SetEngineProbe(initCVEngineHealthProbe(healthChecker))
	startOpsWatchdogJob(appCtx, opsWatchdog)
	hotsetStore := hotset.NewStore(time.Duration(cfg.VerdictStaleTTLSeconds) * time.Second)
	hotsetHandler := hotset.NewHandlerWithConfig(hotsetStore, analysisEnabled, cfg.AppEnv)
	hotsetHandler.SetStaleTTL(time.Duration(cfg.VerdictStaleTTLSeconds) * time.Second)
	if verdictHistoryRepo != nil {
		hotsetHandler.SetCoverageProvider(&hotsetCoverageAdapter{repo: verdictHistoryRepo})
	}

	var corridorHandler *corridor.Handler
	if camCatalog != nil {
		corridorHandler = corridor.NewHandler(corridor.NewService(
			camCatalog, &verdictStoreAdapter{repo: verdictRepo}, cfg.AppEnv, time.Duration(cfg.VerdictStaleTTLSeconds)*time.Second,
		))
	}

	forecastHandler := initForecastHandler(appCtx, cfg, camCatalog, verdictHistoryRepo)
	weatherService := weather.NewService()
	weatherHandler := weather.NewHandler(weatherService)
	startWeatherSamplerJob(appCtx, weather.NewSampler(weatherService, &weatherStoreAdapter{repo: weatherRepo}, slog.Default()), 10*time.Minute)

	// 7. Live Hubs (WS + SSE) & Hex Spatial Aggregators
	wsHub := ws.NewHub()
	defer wsHub.Close()
	liveHub := live.NewHub()
	defer liveHub.Close()

	hexAggregator, floodHexAggregator := initHexAggregators(cfg, camCatalog, liveHub)
	startFloodHexReaper(appCtx, floodHexAggregator)
	preseedHotsetFromStore(verdictRepo, hotsetStore, hexAggregator, floodHexAggregator, time.Duration(cfg.VerdictStaleTTLSeconds)*time.Second)

	// 8. Decoupled Multimodal Flood Pipeline & Storage/Archiver
	floodHandler := initFloodPipeline(appCtx, cfg, camCatalog, weatherService, floodChokepoints, verdictRepo, floodHexAggregator, liveHub, dispatchFloodAlert)
	storeDriver, retentionRootDir := initStorageDriver(cfg)
	var archiverHandler *archiver.Handler
	if storeDriver != nil {
		archiverHandler = archiver.NewHandler(storeDriver, cameraTitleAdapter{catalog: camCatalog})
	}
	feedbackHandler := initFeedbackHandler(camCatalog, proxyCache, upstreamBase, storeDriver, floodHexAggregator, liveHub)

	// 9. Hot Set Scheduler & Harvester Coordinator
	var arch *archiver.Archiver
	if storeDriver != nil {
		arch = archiver.New(storeDriver, retentionRootDir, archiver.Config{
			SampleInterval: time.Duration(cfg.ArchiveSampleIntervalSeconds) * time.Second,
			RetentionDays:  cfg.ArchiveRetentionDays,
			MaxDiskMB:      cfg.ArchiveMaxDiskMB,
			CheckInterval:  cfg.ArchiveRetentionCheckInterval,
		}, nil)
		startArchiverRetention(appCtx, arch, retentionRootDir)
	}

	if analysisEnabled && analyzer != nil {
		onVerdict := buildHotsetOnVerdict(hotsetDispatcherParams{
			proxyCache: proxyCache, frozenDetector: hotset.NewFrozenDetector(cfg.FrozenFrameThreshold),
			hotsetStore: hotsetStore, liveHub: liveHub, hexAggregator: hexAggregator,
			floodHexAggregator: floodHexAggregator, dispatchFloodAlert: dispatchFloodAlert,
			verdictRepo: verdictRepo, verdictHistoryRepo: verdictHistoryRepo, arch: arch, analyzerMode: cfg.Analyzer,
		})
		sched := hotset.NewScheduler(hotsetRegistry, analyzer, time.Duration(cfg.HotsetIntervalSeconds)*time.Second, cfg.HotsetMaxParallel, onVerdict, nil)
		sched.SetStore(hotsetStore)
		sched.SetBudget(hotset.NewAnalysisBudget(cfg.AnalyzerDailyBudget))
		sched.SetWatchdog(opsWatchdog)
		startHotsetScheduler(appCtx, sched)
	}

	harvesterCoord := initHarvesterCoordinator(cfg, camCatalog, weatherService, proxyCache, upstreamBase, storeDriver)
	startHarvesterJob(appCtx, harvesterCoord, cfg.HarvesterEnabled)

	// 10. Gin Router & HTTP Server
	router := setupRouter(routerConfig{
		cfg: cfg, camCatalogHandler: camCatalogHandler, proxyHandler: proxyHandler,
		hotsetHandler: hotsetHandler, corridorHandler: corridorHandler, forecastHandler: forecastHandler,
		weatherHandler: weatherHandler, tileHandler: tileHandler, floodHandler: floodHandler,
		subHandler: subHandler, archiverHandler: archiverHandler, harvesterCoord: harvesterCoord,
		feedbackHandler: feedbackHandler, wsHub: wsHub, liveHub: liveHub, botHandler: botHandler,
		webhookEnabled: webhookEnabled, healthHandler: httpserver.NewHealthHandler(healthChecker),
	})

	srv := &http.Server{Addr: fmt.Sprintf(":%s", cfg.Port), Handler: router}
	go runServer(srv, cfg, camCatalog)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("[SHUTDOWN] Shutting down Camera View server...")
	appCancel()
	if harvesterCoord != nil {
		harvesterCoord.Stop()
	}
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("[FATAL] Server forced to shutdown: %v", err)
	}
	log.Println("[SHUTDOWN] Server exited cleanly.")
}
