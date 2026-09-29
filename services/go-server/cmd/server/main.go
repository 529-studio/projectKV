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
	"github.com/529-studio/projectKV/services/go-server/internal/cache"
	"github.com/529-studio/projectKV/services/go-server/internal/camera"
	"github.com/529-studio/projectKV/services/go-server/internal/config"
	"github.com/529-studio/projectKV/services/go-server/internal/cvclient"
	"github.com/529-studio/projectKV/services/go-server/internal/handlers"
	"github.com/529-studio/projectKV/services/go-server/internal/hotset"
	"github.com/529-studio/projectKV/services/go-server/internal/httpserver"
	"github.com/529-studio/projectKV/services/go-server/internal/live"
	"github.com/529-studio/projectKV/services/go-server/internal/middleware"
	"github.com/529-studio/projectKV/services/go-server/internal/models"
	"github.com/529-studio/projectKV/services/go-server/internal/proxycache"
	"github.com/529-studio/projectKV/services/go-server/internal/storage"
	"github.com/529-studio/projectKV/services/go-server/internal/store"
	"github.com/529-studio/projectKV/services/go-server/internal/tilecache"
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
	verdicts, err := a.repo.GetLatest()
	if err != nil {
		log.Printf("[WARN] Failed to query latest verdicts: %v", err)
		return []hotset.Verdict{}
	}
	out := make([]hotset.Verdict, len(verdicts))
	for i, v := range verdicts {
		out[i] = hotset.Verdict{
			CameraID:   v.CameraID,
			Severity:   v.Severity,
			Confidence: v.Confidence,
			Source:     v.Source,
			Timestamp:  v.Timestamp,
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
	"nguyễn văn linh", "điện biên phủ", "võ văn kiệt", "phạm văn đồng",
	"cầu sài gòn", "xô viết nghệ tĩnh", "hành trình", "bình thái",
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

	// Filter out offline, ghost and duplicate streams so the Hot Set only
	// contains feeds that can actually produce snapshots.
	eligible := make([]camera.Camera, 0, len(all))
	for _, c := range all {
		if !c.IsOnline || c.IsGhost || c.DuplicateOf != "" {
			continue
		}
		eligible = append(eligible, c)
	}

	// First pass: prefer known critical choke points.
	var critical []hotset.Camera
	for _, c := range eligible {
		name := strings.ToLower(c.DisplayName)
		if name == "" {
			name = strings.ToLower(c.Title)
		}
		for _, kw := range criticalKeywords {
			if strings.Contains(name, kw) {
				critical = append(critical, cameraToHotSet(c))
				break
			}
		}
		if maxCameras > 0 && len(critical) >= maxCameras {
			break
		}
	}

	// Second pass: pad with the remaining eligible cameras up to maxCameras.
	used := make(map[string]struct{}, len(critical))
	for _, c := range critical {
		used[c.ID] = struct{}{}
	}
	for _, c := range eligible {
		if _, ok := used[c.ID]; ok {
			continue
		}
		critical = append(critical, cameraToHotSet(c))
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
	camStore, err := models.LoadCameraStore(cfg.CamerasFilePath)
	if err != nil {
		log.Fatalf("[FATAL] Failed to load camera store: %v", err)
	}

	camCatalog, err := camera.LoadCatalog(cfg.CamerasFilePath)
	if err != nil {
		log.Printf("[WARN] Failed to load structured camera catalog: %v", err)
	}
	var camCatalogHandler *camera.Handler
	if camCatalog != nil {
		camCatalogHandler = camera.NewHandler(camCatalog)
	}

	// 2. Initialize HotSet registry for active camera tracking.
	// activeTTL = 120 s; seed fixed critical cameras when analysis is enabled
	// (e.g. mock/dev mode) so the scheduler has work immediately at startup.
	var fixedCameras []hotset.Camera
	if camCatalog != nil {
		fixedCameras = buildFixedHotSet(camCatalog, 40)
	}
	hotsetRegistry := hotset.NewRegistry(fixedCameras, 120*time.Second)

	// 3. Initialize in-memory cache with singleflight dedup (legacy handler).
	snapCache := cache.NewSnapshotCache(cfg.CacheTTL)
	stopEvictCh := make(chan struct{})
	snapCache.StartEvictionRoutine(cfg.CacheEvictInterval, cfg.CacheMaxAge, stopEvictCh)

	// 4. proxycache handler — wraps the DOT portal proxy with TTL cache and
	// HotSet Touch (Dependency Inversion: Touch is injected as a callback so
	// proxycache does not import hotset directly).
	upstreamBase := os.Getenv("SNAPSHOT_UPSTREAM_BASE_URL")
	if upstreamBase == "" {
		upstreamBase = "https://giaothong.hochiminhcity.gov.vn/render/ImageHandler.ashx"
	}
	proxyCache := proxycache.NewCache(cfg.CacheTTL)
	proxyHandler := proxycache.NewHandler(proxyCache, upstreamBase, func(id, name, zone, snapshotURL string) {
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
	case "none", "":
		log.Println("[INIT] ANALYZER=none: Analysis scheduler is disabled. No camera_verdicts will be recorded and no SSE camera_update will be emitted.")
	default:
		log.Fatalf("[FATAL] Unknown ANALYZER=%q (valid options: none, mock)", cfg.Analyzer)
	}

	hotsetHandler := hotset.NewHandlerWithConfig(&verdictStoreAdapter{repo: verdictRepo}, analysisEnabled, cfg.AppEnv)

	// 6. Initialize WebSocket Hub for real-time verdict streaming (Issue #9).
	// Kept in place — will be retired after D1 (EventSource migration) is verified.
	wsHub := ws.NewHub()
	defer wsHub.Close()

	// 6b. Initialize SSE Hub (ADR-003 Decision 1 — replaces WebSocket for new clients).
	// internal/live broadcasts camera_update events to all EventSource subscribers.
	liveHub := live.NewHub()
	defer liveHub.Close()

	// 6c. Wire the Hot Set scheduler with OnVerdict only if analysis is enabled.
	if analysisEnabled && analyzer != nil {
		// Initialize object storage and snapshot archiver (Issue #62)
		var arch *archiver.Archiver
		localStorage, err := storage.NewLocalStorage(cfg.StorageLocalRoot)
		if err != nil {
			log.Printf("[WARN] Failed to initialize storage driver (%s): %v", cfg.StorageLocalRoot, err)
		} else {
			archCfg := archiver.Config{
				SampleInterval: time.Duration(cfg.ArchiveSampleIntervalSeconds) * time.Second,
				RetentionDays:  cfg.ArchiveRetentionDays,
				MaxDiskMB:      cfg.ArchiveMaxDiskMB,
				CheckInterval:  cfg.ArchiveRetentionCheckInterval,
			}
			arch = archiver.New(localStorage, cfg.StorageLocalRoot, archCfg, nil)

			retentionCtx, cancelRetention := context.WithCancel(context.Background())
			defer cancelRetention()
			go arch.StartRetention(retentionCtx)
			log.Printf("[INIT] Snapshot archiver enabled (root=%s, retention=%dd, maxDisk=%dMB, sampleInterval=%ds)",
				cfg.StorageLocalRoot, cfg.ArchiveRetentionDays, cfg.ArchiveMaxDiskMB, cfg.ArchiveSampleIntervalSeconds)
		}

		onVerdict := func(v hotset.Verdict) {
			// Fan-out 1: push to all SSE clients immediately.
			liveHub.Broadcast(v)

			// Fan-out 2: persist to SQLite for GET /api/traffic-status hydration.
			if verdictRepo != nil {
				sv := store.Verdict{
					CameraID:   v.CameraID,
					Severity:   v.Severity,
					Confidence: v.Confidence,
					Source:     v.Source,
					Timestamp:  v.Timestamp,
				}
				if err := verdictRepo.Save(context.Background(), sv); err != nil {
					log.Printf("[WARN] Failed to persist verdict for %s: %v", v.CameraID, err)
				}
			}

			// Fan-out 3: event-driven snapshot archiver with retention policy (Issue #62).
			if arch != nil {
				var frameReader io.Reader
				if cachedData, ok := proxyCache.Get(v.CameraID); ok && len(cachedData) > 0 {
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

		hotsetScheduler := hotset.NewScheduler(
			hotsetRegistry,
			analyzer,
			30*time.Second,
			10,
			onVerdict,
			nil, // uses slog.Default()
		)

		// Start the Hot Set scheduler in background; cancelled on graceful shutdown.
		schedulerCtx, cancelScheduler := context.WithCancel(context.Background())
		defer cancelScheduler()
		go hotsetScheduler.Run(schedulerCtx)
	}

	cameraHandler := handlers.NewCameraHandler(camStore, snapCache)

	// 8. Setup Gin Engine
	router := gin.Default()
	router.Use(middleware.CORSMiddleware())

	// Health check
	router.GET("/health", cameraHandler.HealthCheck)
	router.HEAD("/health", cameraHandler.HealthCheck)
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
		api.GET("/cameras", func(c *gin.Context) {
			if camCatalogHandler != nil {
				camCatalogHandler.GetCameras(c)
				return
			}
			cameraHandler.GetCameras(c)
		})
		api.HEAD("/cameras", cameraHandler.GetCameras)
		if camCatalogHandler != nil {
			api.GET("/cameras/:id", camCatalogHandler.GetCameraByID)
			api.GET("/zones", camCatalogHandler.GetZones)
			api.GET("/phuong", camCatalogHandler.GetPhuongs)
		}
		// Proxy cache snapshot routes (wraps proxycache.Handler)
		proxyHandler.RegisterRoutes(api)
		// Traffic and flood congestion status endpoints (Issue #8)
		hotsetHandler.RegisterRoutes(api)
		// Map tile proxy and local disk cache
		tileHandler.RegisterRoutes(api)
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
		log.Printf(" [CAM] Loaded %d cameras into memory (%d online)", camStore.Total, camStore.OnlineCount)
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
	close(stopEvictCh)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("[FATAL] Server forced to shutdown: %v", err)
	}

	log.Println("[SHUTDOWN] Server exited cleanly.")
}
