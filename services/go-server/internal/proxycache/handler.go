package proxycache

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

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
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("upstream fetch failed: %v", err)})
		return
	}

	// Fire-and-forget HotSet touch — does not block the response.
	if h.onTouch != nil {
		h.onTouch(id, "", "", "")
	}

	cacheStatus := "MISS"
	if isHit {
		cacheStatus = "HIT"
	}
	c.Header("X-Cache-Status", cacheStatus)
	c.Header("Cache-Control", "public, max-age=10")

	if contentType == "" {
		contentType = "image/jpeg"
	}
	c.Data(http.StatusOK, contentType, data)
}

// fetchUpstream fetches the raw snapshot bytes from the upstream DOT portal.
func (h *Handler) fetchUpstream(ctx context.Context, id string) ([]byte, string, error) {
	url := fmt.Sprintf("%s?id=%s&t=%d", h.upstreamURL, id, time.Now().UnixMilli())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; CameraViewProxy/1.0)")
	req.Header.Set("Referer", "https://giaothong.hochiminhcity.gov.vn/")

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("upstream returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("read body: %w", err)
	}

	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "image/jpeg"
	}
	return body, ct, nil
}
