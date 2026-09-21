package handlers

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/529-studio/projectKV/services/go-server/internal/cache"
	"github.com/529-studio/projectKV/services/go-server/internal/models"

	"github.com/gin-gonic/gin"
)

// CameraHandler handles HTTP requests relating to traffic cameras and snapshots.
type CameraHandler struct {
	store      *models.CameraStore
	cache      *cache.SnapshotCache
	httpClient *http.Client
}

// NewCameraHandler creates an instance of CameraHandler.
func NewCameraHandler(store *models.CameraStore, snapCache *cache.SnapshotCache) *CameraHandler {
	return &CameraHandler{
		store: store,
		cache: snapCache,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// GetCameras handles GET /api/cameras.
func (h *CameraHandler) GetCameras(c *gin.Context) {
	c.JSON(http.StatusOK, models.CamerasResponse{
		Total:       h.store.Total,
		OnlineCount: h.store.OnlineCount,
		Cameras:     h.store.Cameras,
	})
}

// GetSnapshot handles GET /api/cameras/snapshot/:id.
func (h *CameraHandler) GetSnapshot(c *gin.Context) {
	cameraID := c.Param("id")
	if cameraID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "camera id is required"})
		return
	}

	buf, contentType, isHit, err := h.cache.FetchOrGet(cameraID, func() ([]byte, string, error) {
		nowMillis := time.Now().UnixMilli()
		url := fmt.Sprintf("https://giaothong.hochiminhcity.gov.vn/render/ImageHandler.ashx?id=%s&t=%d", cameraID, nowMillis)

		req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, url, nil)
		if err != nil {
			return nil, "", fmt.Errorf("failed to create upstream request: %w", err)
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36")

		resp, err := h.httpClient.Do(req)
		if err != nil {
			return nil, "", fmt.Errorf("upstream request failed: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, "", fmt.Errorf("upstream returned non-200 status: %d", resp.StatusCode)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, "", fmt.Errorf("failed to read upstream body: %w", err)
		}

		cType := resp.Header.Get("Content-Type")
		if cType == "" {
			cType = "image/jpeg"
		}

		return body, cType, nil
	})

	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	cacheStatus := "MISS"
	if isHit {
		cacheStatus = "HIT"
	}

	c.Header("Content-Type", contentType)
	c.Header("Cache-Control", "public, max-age=10")
	c.Header("X-Cache-Status", cacheStatus)
	c.Data(http.StatusOK, contentType, buf)
}

// HealthCheck handles GET /health.
func (h *CameraHandler) HealthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":         "ok",
		"cameras_loaded": h.store.Total,
		"online_cameras": h.store.OnlineCount,
	})
}
