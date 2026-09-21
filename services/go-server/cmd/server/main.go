package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/529-studio/projectKV/services/go-server/internal/cache"
	"github.com/529-studio/projectKV/services/go-server/internal/config"
	"github.com/529-studio/projectKV/services/go-server/internal/handlers"
	"github.com/529-studio/projectKV/services/go-server/internal/middleware"
	"github.com/529-studio/projectKV/services/go-server/internal/models"

	"github.com/gin-gonic/gin"
)

func main() {
	cfg := config.LoadConfig()

	// Release mode in production
	if os.Getenv("GIN_MODE") == "release" {
		gin.SetMode(gin.ReleaseMode)
	}

	// 1. Load camera catalog into memory
	log.Printf("[INIT] Loading cameras from: %s", cfg.CamerasFilePath)
	store, err := models.LoadCameraStore(cfg.CamerasFilePath)
	if err != nil {
		log.Fatalf("[FATAL] Failed to load camera store: %v", err)
	}

	// 2. Initialize in-memory cache with singleflight dedup
	snapCache := cache.NewSnapshotCache(cfg.CacheTTL)
	stopEvictCh := make(chan struct{})
	snapCache.StartEvictionRoutine(cfg.CacheEvictInterval, cfg.CacheMaxAge, stopEvictCh)

	// 3. Initialize HTTP Handlers
	cameraHandler := handlers.NewCameraHandler(store, snapCache)

	// 4. Setup Gin Engine
	router := gin.Default()
	router.Use(middleware.CORSMiddleware())

	// Health check
	router.GET("/health", cameraHandler.HealthCheck)
	router.HEAD("/health", cameraHandler.HealthCheck)

	// API Group
	api := router.Group("/api")
	{
		api.GET("/cameras", cameraHandler.GetCameras)
		api.HEAD("/cameras", cameraHandler.GetCameras)
		api.GET("/cameras/snapshot/:id", cameraHandler.GetSnapshot)
		api.HEAD("/cameras/snapshot/:id", cameraHandler.GetSnapshot)
	}

	// 5. Static Web Serving
	if fi, err := os.Stat(cfg.StaticWebDir); err == nil && fi.IsDir() {
		log.Printf("[INIT] Serving static web UI from: %s", cfg.StaticWebDir)
		router.StaticFile("/", filepath.Join(cfg.StaticWebDir, "index.html"))
		router.StaticFile("/index.html", filepath.Join(cfg.StaticWebDir, "index.html"))
		router.StaticFile("/styles.css", filepath.Join(cfg.StaticWebDir, "styles.css"))
		router.StaticFile("/app.js", filepath.Join(cfg.StaticWebDir, "app.js"))

		// Handle any additional assets or SPA fallback
		router.NoRoute(func(c *gin.Context) {
			reqPath := c.Request.URL.Path
			if strings.HasPrefix(reqPath, "/api/") {
				c.JSON(http.StatusNotFound, gin.H{"error": "api endpoint not found"})
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
		log.Printf(" [CAM] Loaded %d cameras into memory (%d online)", store.Total, store.OnlineCount)
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
