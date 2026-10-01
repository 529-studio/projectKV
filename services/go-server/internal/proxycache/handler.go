package proxycache

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// GhostPlaceholderSize is the byte size of the upstream "IMAGE NOT AVAILABLE" placeholder PNG.
const GhostPlaceholderSize = 2638

// ErrGhostCamera indicates that the upstream portal returned a placeholder error image.
var ErrGhostCamera = errors.New("upstream returned placeholder image: camera stream offline")

// IsGhostImage checks if the byte slice represents an upstream error placeholder.
func IsGhostImage(data []byte) bool {
	if len(data) == 0 {
		return true
	}
	if len(data) == GhostPlaceholderSize {
		return true
	}
	// The upstream DOT portal returns an "IMAGE NOT AVAILABLE" PNG file when offline
	if len(data) >= 8 && string(data[:8]) == "\x89PNG\r\n\x1a\n" {
		return true
	}
	return false
}

// TouchFunc is a callback invoked after a successful snapshot fetch so the
// caller can register the camera with hotset.Registry.Touch without creating
// a direct import dependency from proxycache → hotset.
// (Dependency Inversion Principle: the interface is defined here, the
// concrete registry is wired in cmd/*/main.go.)
type TouchFunc func(id, name, zone, snapshotURL string)

// Handler proxies snapshot images through Cache and triggers a HotSet touch
// on every successful request.
type Handler struct {
	cache       *Cache
	upstreamURL string // e.g. "https://giaothong.hochiminhcity.gov.vn/render/ImageHandler.ashx"
	httpClient  *http.Client
	onTouch     TouchFunc // optional; nil means no HotSet integration
}

// NewHandler constructs a Handler.
//
//   - upstreamURL: base URL of the DOT camera portal (no trailing slash).
//   - onTouch: optional callback; pass nil to disable HotSet integration.
func NewHandler(cache *Cache, upstreamURL string, onTouch TouchFunc) *Handler {
	return &Handler{
		cache:       cache,
		upstreamURL: upstreamURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		onTouch: onTouch,
	}
}

// RegisterRoutes mounts the snapshot proxy endpoint onto the provided router
// group.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/snapshot/:id", h.GetSnapshot)
	rg.HEAD("/snapshot/:id", h.GetSnapshot)
	rg.GET("/snapshot/:id/osd", h.GetSnapshotOSD)
}

// GetSnapshot godoc
// @Summary Get camera snapshot (proxy cache)
// @Description Proxy and cache a real-time traffic camera snapshot from the upstream DOT portal (TTL ≥ 10 s). Also triggers HotSet Touch for automatic AI analysis.
// @Tags Camera
// @Produce image/jpeg
// @Param id path string true "Camera ID"
// @Success 200 {string} binary "JPEG image data"
// @Failure 400 {object} map[string]string
// @Failure 502 {object} map[string]string
// @Failure 503 {object} map[string]string
// @Router /api/snapshot/{id} [get]
func (h *Handler) GetSnapshot(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "camera id is required"})
		return
	}

	data, contentType, isHit, err := h.cache.GetOrFetch(id, func() ([]byte, string, error) {
		return h.fetchUpstream(c.Request.Context(), id)
	})
	if err != nil {
		if errors.Is(err, ErrGhostCamera) {
			c.Header("X-Camera-Status", "GHOST_OFFLINE")
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error":    "camera stream offline (signal lost)",
				"code":     "GHOST_CAMERA",
				"is_ghost": true,
			})
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("upstream fetch failed: %v", err)})
		return
	}

	// Fire-and-forget HotSet touch — does not block the response.
	if h.onTouch != nil {
		snapshotURL := fmt.Sprintf("%s?id=%s", h.upstreamURL, id)
		h.onTouch(id, "", "", snapshotURL)
	}

	cacheStatus := "MISS"
	if isHit {
		cacheStatus = "HIT"
	}
	c.Header("X-Cache-Status", cacheStatus)
	c.Header("Cache-Control", "public, max-age=10")

	if osdTime, rawOSD, ok := h.cache.GetOSD(id); ok && osdTime != "" {
		c.Header("X-OSD-Timestamp", osdTime)
		c.Header("X-OSD-Raw", rawOSD)
	}

	if contentType == "" {
		contentType = "image/jpeg"
	}
	c.Data(http.StatusOK, contentType, data)
}

// GetSnapshotOSD returns the extracted OSD timestamp and camera information.
func (h *Handler) GetSnapshotOSD(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "camera id is required"})
		return
	}

	_, _, _, err := h.cache.GetOrFetch(id, func() ([]byte, string, error) {
		return h.fetchUpstream(c.Request.Context(), id)
	})
	if err != nil {
		if errors.Is(err, ErrGhostCamera) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "ghost camera offline", "is_ghost": true})
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("upstream fetch failed: %v", err)})
		return
	}

	osdTime, rawOSD, _ := h.cache.GetOSD(id)
	c.JSON(http.StatusOK, gin.H{
		"camera_id": id,
		"timestamp": osdTime,
		"raw_osd":   rawOSD,
	})
}

// fetchUpstream fetches the raw snapshot bytes from the upstream DOT portal.
// It retries once on transient network or 5xx upstream errors with a short backoff.
func (h *Handler) fetchUpstream(ctx context.Context, id string) ([]byte, string, error) {
	const maxAttempts = 2
	var lastErr error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			select {
			case <-ctx.Done():
				return nil, "", ctx.Err()
			case <-time.After(300 * time.Millisecond):
			}
		}

		url := fmt.Sprintf("%s?id=%s&t=%d", h.upstreamURL, id, time.Now().UnixMilli())

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, "", fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; CameraViewProxy/1.0)")
		req.Header.Set("Referer", "https://giaothong.hochiminhcity.gov.vn/")

		resp, err := h.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("do request: %w", err)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close() //nolint:errcheck
			lastErr = fmt.Errorf("upstream returned %d", resp.StatusCode)
			if resp.StatusCode >= 400 && resp.StatusCode < 500 {
				// Client-side errors from upstream (e.g. 404 Not Found) are non-transient; abort immediately.
				return nil, "", lastErr
			}
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close() //nolint:errcheck
		if err != nil {
			lastErr = fmt.Errorf("read body: %w", err)
			continue
		}

		if IsGhostImage(body) {
			return nil, "", ErrGhostCamera
		}

		ct := resp.Header.Get("Content-Type")
		if ct == "" {
			ct = "image/jpeg"
		}
		return body, ct, nil
	}

	return nil, "", lastErr
}
