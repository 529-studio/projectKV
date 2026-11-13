package main

import (
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	"github.com/529-studio/projectKV/services/go-server/internal/archiver"
	"github.com/529-studio/projectKV/services/go-server/internal/camera"
	"github.com/529-studio/projectKV/services/go-server/internal/config"
	"github.com/529-studio/projectKV/services/go-server/internal/corridor"
	"github.com/529-studio/projectKV/services/go-server/internal/feedback"
	"github.com/529-studio/projectKV/services/go-server/internal/flood"
	"github.com/529-studio/projectKV/services/go-server/internal/forecast"
	"github.com/529-studio/projectKV/services/go-server/internal/harvester"
	"github.com/529-studio/projectKV/services/go-server/internal/hotset"
	"github.com/529-studio/projectKV/services/go-server/internal/httpserver"
	"github.com/529-studio/projectKV/services/go-server/internal/live"
	"github.com/529-studio/projectKV/services/go-server/internal/middleware"
	"github.com/529-studio/projectKV/services/go-server/internal/proxycache"
	"github.com/529-studio/projectKV/services/go-server/internal/subscription"
	"github.com/529-studio/projectKV/services/go-server/internal/telegrambot"
	"github.com/529-studio/projectKV/services/go-server/internal/tilecache"
	"github.com/529-studio/projectKV/services/go-server/internal/weather"
	"github.com/529-studio/projectKV/services/go-server/internal/ws"
)

type routerConfig struct {
	cfg               *config.ServerConfig
	camCatalogHandler *camera.Handler
	proxyHandler      *proxycache.Handler
	hotsetHandler     *hotset.Handler
	corridorHandler   *corridor.Handler
	forecastHandler   *forecast.Handler
	weatherHandler    *weather.Handler
	tileHandler       *tilecache.Handler
	floodHandler      *flood.Handler
	subHandler        *subscription.Handler
	archiverHandler   *archiver.Handler
	harvesterCoord    *harvester.Coordinator
	feedbackHandler   *feedback.Handler
	wsHub             *ws.Hub
	liveHub           *live.Hub
	botHandler        *telegrambot.Handler
	webhookEnabled    bool
	healthHandler     *httpserver.HealthHandler
}

// setupRouter centralizes Gin router creation, middleware attachments, and route registrations.
func setupRouter(p routerConfig) *gin.Engine {
	cfg := p.cfg
	router := gin.Default()
	router.Use(middleware.CORSMiddleware(cfg.AllowedOrigins...))
	router.Use(gzip.Gzip(gzip.DefaultCompression,
		gzip.WithExcludedPaths([]string{"/api/snapshot", "/api/cameras/snapshot", "/api/live/stream", "/ws"}),
	))
	router.Use(middleware.SecretSanitizer())
	router.Use(middleware.RateLimitMiddleware(cfg.RateLimitRPS, cfg.RateLimitBurst))

	// Health check probes. The handler carries the optional cv-engine health
	// checker so orchestrators see inference readiness, not just process
	// liveness, on every alias.
	health := p.healthHandler
	if health == nil {
		health = httpserver.NewHealthHandler(nil)
	}
	router.GET("/health", health.Healthz)
	router.HEAD("/health", health.Healthz)
	router.GET("/healthz", health.Healthz)

	// WebSocket Live Broadcast (Issue #9: GET /ws/live) — kept until D1 FE migration verified.
	if p.wsHub != nil {
		p.wsHub.RegisterRoutes(router)
	}

	// SSE Live Stream (ADR-003 Decision 1: GET /api/live/stream).
	if p.liveHub != nil {
		p.liveHub.RegisterRoutes(router)
	}

	// Swagger documentation UI
	swaggerHandler := ginSwagger.WrapHandler(swaggerFiles.Handler)
	router.GET("/swagger", func(c *gin.Context) {
		c.Redirect(http.StatusMovedPermanently, "/swagger/index.html")
	})
	router.GET("/swagger/*any", func(c *gin.Context) {
		param := c.Param("any")
		if param == "" || param == "/" {
			c.Redirect(http.StatusMovedPermanently, "/swagger/index.html")
			return
		}
		swaggerHandler(c)
	})

	// Telegram bot webhook (S12-T4 / Issue #402). RegisterRoutes is a no-op
	// unless the handler is enabled, and the explicit token check closes the one
	// gap the handler cannot see: a valid handle and secret with no Bot API
	// token must still leave the endpoint unmounted (404, spec §2.2 AC10).
	if p.webhookEnabled && p.botHandler != nil {
		p.botHandler.RegisterRoutes(router)
	}

	// Dev authentication gate. Built before the API group because it is needed
	// by routes registered inside it (subscription audit listing, archiver,
	// harvester) — S12-T4 spec §4.4.
	devAuth := middleware.RequireDevAuth(cfg.DevAccessToken, cfg.AppEnv, middleware.DevAuthConfig{
		CFAccessAudience:     cfg.CFAccessAudience,
		CFAccessCertsURL:     cfg.CFAccessCertsURL,
		CFAccessClientID:     cfg.CFAccessClientID,
		CFAccessClientSecret: cfg.CFAccessClientSecret,
	})

	// API Group
	api := router.Group("/api")
	{
		// Health alias under the API prefix used by the web client and load
		// balancer health checks.
		api.GET("/health", health.Healthz)

		if p.camCatalogHandler != nil {
			api.GET("/cameras", p.camCatalogHandler.GetCameras)
			api.HEAD("/cameras", p.camCatalogHandler.GetCameras)
			api.GET("/cameras/:id", p.camCatalogHandler.GetCameraByID)
			api.GET("/zones", p.camCatalogHandler.GetZones)
			api.GET("/phuong", p.camCatalogHandler.GetPhuongs)
		}
		// Proxy cache snapshot routes (wraps proxycache.Handler)
		if p.proxyHandler != nil {
			p.proxyHandler.RegisterRoutes(api)
		}
		// Traffic and flood congestion status endpoints (Issue #8)
		if p.hotsetHandler != nil {
			p.hotsetHandler.RegisterRoutes(api)
		}
		// Traffic corridor status endpoints (ADR-004 §D1 / Issue #75)
		if p.corridorHandler != nil {
			p.corridorHandler.RegisterRoutes(api)
		}
		// Short-horizon congestion forecast endpoints (Issue #302 / S9-T6)
		if p.forecastHandler != nil {
			p.forecastHandler.RegisterRoutes(api)
		}
		// Live weather forecast & radar metadata endpoints (Issue #120)
		if p.weatherHandler != nil {
			p.weatherHandler.RegisterRoutes(api)
		}
		// Map tile proxy and local disk cache
		if p.tileHandler != nil {
			p.tileHandler.RegisterRoutes(api)
		}
		// Decoupled multimodal flood pipeline endpoints (Issue #209)
		if p.floodHandler != nil {
			p.floodHandler.RegisterRoutes(api)
		}
		// Flood alert subscriptions (Issue #225 / S6-T5, citizen UI S12-T4).
		// POST/DELETE stay public; GET enumerates chat ids and is dev-gated.
		if p.subHandler != nil {
			p.subHandler.RegisterRoutes(api, subscription.WithAuditAuth(devAuth))
		}
		// Shared archiver snapshots and collaborative team labeler endpoints (Issue #214)
		if p.archiverHandler != nil {
			p.archiverHandler.RegisterRoutes(api, devAuth)
		}
		// Harvester flywheel observability (Issue #299 / S9-T3)
		harvester.NewHandler(p.harvesterCoord, cfg.HarvesterEnabled).RegisterRoutes(api, devAuth)
		// Crowdsourced flood feedback & R2 archiver (Issue #249 / S7-T4 / S8-T2)
		if p.feedbackHandler != nil {
			p.feedbackHandler.RegisterRoutes(api)
		}
		// Legacy alias kept for backward compat with existing FE constants.js
		if p.proxyHandler != nil {
			api.GET("/cameras/snapshot/:id", p.proxyHandler.GetSnapshot)
			api.HEAD("/cameras/snapshot/:id", p.proxyHandler.GetSnapshot)
		}
	}

	// Static Web Serving (Next.js static export).
	// Purged obsolete vanilla JS routes: styles.css and app.js (Issue #425).
	if fi, err := os.Stat(cfg.StaticWebDir); err == nil && fi.IsDir() {
		log.Printf("[INIT] Serving static web UI from: %s", cfg.StaticWebDir)
		absStaticDir, err := filepath.Abs(cfg.StaticWebDir)
		if err != nil {
			absStaticDir = filepath.Clean(cfg.StaticWebDir)
		}
		fs := http.Dir(absStaticDir)
		indexFile := filepath.Join(absStaticDir, "index.html")

		router.StaticFile("/", indexFile)
		router.StaticFile("/index.html", indexFile)

		nextDir := filepath.Join(absStaticDir, "_next")
		if info, err := os.Stat(nextDir); err == nil && info.IsDir() {
			router.Static("/_next", nextDir)
		}

		// Handle any additional assets or SPA fallback using hardened http.Dir
		router.NoRoute(func(c *gin.Context) {
			reqPath := c.Request.URL.Path
			if strings.HasPrefix(reqPath, "/api/") || strings.HasPrefix(reqPath, "/swagger") || strings.HasPrefix(reqPath, "/ws") {
				c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
				return
			}
			cleanRel := strings.TrimPrefix(filepath.Clean("/"+reqPath), "/")
			if cleanRel == "" || cleanRel == "." {
				c.File(indexFile)
				return
			}
			if f, err := fs.Open(cleanRel); err == nil {
				defer f.Close()
				if info, err := f.Stat(); err == nil && !info.IsDir() {
					http.ServeContent(c.Writer, c.Request, filepath.Base(cleanRel), info.ModTime(), f)
					return
				}
			}
			c.File(indexFile)
		})
	} else {
		log.Printf("[WARN] Static web directory not found: %s", cfg.StaticWebDir)
	}

	return router
}

// runServer logs startup banner and serves HTTP until stopped.
func runServer(srv *http.Server, cfg *config.ServerConfig, camCatalog *camera.Catalog) {
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
	log.Printf(" [NOTIF] Telegram:   debounce=%dm, active=%t", cfg.TelegramDebounceMinutes, cfg.TelegramBotToken != "")
	log.Println("================================================================")

	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("[FATAL] Server listen failed: %v", err)
	}
}
