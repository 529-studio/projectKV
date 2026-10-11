package archiver

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/529-studio/projectKV/services/go-server/internal/storage"
	"github.com/gin-gonic/gin"
)

var datePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// CameraLookup defines the minimal camera metadata lookup needed by the archiver handler.
type CameraLookup interface {
	GetCameraTitle(id string) string
}

// SnapshotItem represents a single archived snapshot frame.
type SnapshotItem struct {
	Key        string `json:"key"`
	CameraID   string `json:"camera_id"`
	CameraName string `json:"camera_name,omitempty"`
	Timestamp  int64  `json:"timestamp"`
	Reason     string `json:"reason"`
	Size       int64  `json:"size"`
	RawURL     string `json:"raw_url"`
	PublicURL  string `json:"public_url,omitempty"`
}

// SnapshotListResponse represents the paginated list of archived snapshots.
type SnapshotListResponse struct {
	Date      string         `json:"date"`
	Total     int            `json:"total"`
	Offset    int            `json:"offset"`
	Limit     int            `json:"limit"`
	Snapshots []SnapshotItem `json:"snapshots"`
}

// LabelPayload represents human-annotated ground truth for a snapshot.
type LabelPayload struct {
	Date         string `json:"date" binding:"required"`
	CameraID     string `json:"camera_id" binding:"required"`
	SnapshotKey  string `json:"snapshot_key" binding:"required"`
	Annotator    string `json:"annotator"`
	FloodLevel   string `json:"flood_level"`   // none, puddle, deep
	TrafficLevel string `json:"traffic_level"` // clear, moderate, heavy, gridlock
	Weather      string `json:"weather"`       // clear, rain, fog
	TimeOfDay    string `json:"time_of_day"`   // day, dusk, night
	Notes        string `json:"notes,omitempty"`
	Timestamp    int64  `json:"timestamp"`
}

// LabelListResponse represents a collection of ground truth labels for a date.
type LabelListResponse struct {
	Date   string         `json:"date"`
	Total  int            `json:"total"`
	Labels []LabelPayload `json:"labels"`
}

// Handler handles HTTP requests for archiver snapshot browsing, streaming, and labeling.
type Handler struct {
	store     storage.Storage
	camLookup CameraLookup
}

// NewHandler creates a new archiver Handler.
func NewHandler(store storage.Storage, camLookup CameraLookup) *Handler {
	return &Handler{
		store:     store,
		camLookup: camLookup,
	}
}

// RegisterRoutes mounts archiver routes onto the provided gin.RouterGroup with optional middlewares.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup, middlewares ...gin.HandlerFunc) {
	archiverGroup := rg.Group("/archiver", middlewares...)
	{
		archiverGroup.GET("/dates", h.ListDates)
		archiverGroup.GET("/snapshots", h.ListSnapshots)
		archiverGroup.GET("/snapshots/raw", h.GetSnapshotRaw)
		archiverGroup.HEAD("/snapshots/raw", h.GetSnapshotRaw)
		archiverGroup.GET("/labels", h.GetLabels)
		archiverGroup.POST("/labels", h.SaveLabel)
	}
}

// ListDates godoc
// @Summary List available partition dates
// @Description Retrieve a list of dates that contain archived snapshot proof frames
// @Tags archiver
// @Produce json
// @Success 200 {array} string
// @Failure 500 {object} map[string]string
// @Router /api/archiver/dates [get]
func (h *Handler) ListDates(c *gin.Context) {
	items, err := h.store.List(c.Request.Context(), "")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list storage items"})
		return
	}

	dateSet := make(map[string]struct{})
	for _, it := range items {
		parts := strings.Split(it.Key, "/")
		if len(parts) >= 2 && datePattern.MatchString(parts[0]) {
			dateSet[parts[0]] = struct{}{}
		}
	}

	dates := make([]string, 0, len(dateSet))
	for d := range dateSet {
		dates = append(dates, d)
	}

	sort.Sort(sort.Reverse(sort.StringSlice(dates)))
	c.JSON(http.StatusOK, dates)
}

// ListSnapshots godoc
// @Summary List archived snapshot frames
// @Description Browse archived frames for a specific partition date with optional camera filter and pagination
// @Tags archiver
// @Produce json
// @Param date query string false "Partition date in YYYY-MM-DD format (defaults to today or latest)"
// @Param camera_id query string false "Filter by camera ID"
// @Param limit query int false "Maximum number of snapshots to return (1-200, default 50)"
// @Param offset query int false "Offset for pagination (default 0)"
// @Success 200 {object} SnapshotListResponse
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /api/archiver/snapshots [get]
func (h *Handler) ListSnapshots(c *gin.Context) {
	date := strings.TrimSpace(c.Query("date"))
	if date != "" && !datePattern.MatchString(date) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid date format, expected YYYY-MM-DD"})
		return
	}

	if date == "" {
		date = time.Now().In(ICT).Format("2006-01-02")
	}

	camFilter := strings.TrimSpace(c.Query("camera_id"))
	limit := 50
	if l := c.Query("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
			if limit > 200 {
				limit = 200
			}
		}
	}

	offset := 0
	if o := c.Query("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	items, err := h.store.List(c.Request.Context(), date)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list snapshots"})
		return
	}

	var snapshots []SnapshotItem
	for _, it := range items {
		lower := strings.ToLower(it.Key)
		if !strings.HasSuffix(lower, ".jpg") && !strings.HasSuffix(lower, ".jpeg") && !strings.HasSuffix(lower, ".png") {
			continue
		}

		parts := strings.Split(it.Key, "/")
		if len(parts) < 3 {
			continue
		}

		camID := parts[1]
		if camFilter != "" && camID != camFilter {
			continue
		}

		filename := parts[2]
		dotIdx := strings.LastIndex(filename, ".")
		nameNoExt := filename
		if dotIdx != -1 {
			nameNoExt = filename[:dotIdx]
		}

		fileParts := strings.SplitN(nameNoExt, "_", 2)
		var ts int64
		var reason string
		if len(fileParts) >= 1 {
			ts, _ = strconv.ParseInt(fileParts[0], 10, 64)
		}
		if len(fileParts) >= 2 {
			reason = fileParts[1]
		}

		camName := ""
		if h.camLookup != nil {
			camName = h.camLookup.GetCameraTitle(camID)
		}

		rawURL := "/api/archiver/snapshots/raw?key=" + url.QueryEscape(it.Key)
		var publicURL string
		type publicURLProvider interface {
			PublicURL(relPath string) string
		}
		if p, ok := h.store.(publicURLProvider); ok {
			publicURL = p.PublicURL(it.Key)
		}

		snapshots = append(snapshots, SnapshotItem{
			Key:        it.Key,
			CameraID:   camID,
			CameraName: camName,
			Timestamp:  ts,
			Reason:     reason,
			Size:       it.Size,
			RawURL:     rawURL,
			PublicURL:  publicURL,
		})
	}

	// Sort newest frames first
	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].Timestamp > snapshots[j].Timestamp
	})

	total := len(snapshots)
	pageSnapshots := []SnapshotItem{}
	if offset < total {
		end := offset + limit
		if end > total {
			end = total
		}
		pageSnapshots = snapshots[offset:end]
	}

	c.JSON(http.StatusOK, SnapshotListResponse{
		Date:      date,
		Total:     total,
		Offset:    offset,
		Limit:     limit,
		Snapshots: pageSnapshots,
	})
}

// GetSnapshotRaw godoc
// @Summary Stream snapshot image binary
// @Description Secure proxy stream of raw JPEG snapshot binary, protecting against path traversal and bypassing Zero Trust redirects
// @Tags archiver
// @Produce image/jpeg
// @Param key query string true "Snapshot storage key"
// @Success 200 {string} binary
// @Failure 400 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /api/archiver/snapshots/raw [get]
func (h *Handler) GetSnapshotRaw(c *gin.Context) {
	key := strings.TrimSpace(c.Query("key"))
	if key == "" || strings.Contains(key, "..") || strings.Contains(key, ":") || strings.Contains(key, "\\") || strings.HasPrefix(key, "/") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid snapshot key"})
		return
	}

	rc, err := h.store.Get(c.Request.Context(), key)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "snapshot not found"})
			return
		}
		if errors.Is(err, storage.ErrInvalidPath) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid path traversal"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to retrieve snapshot"})
		return
	}
	defer rc.Close()

	c.Header("Content-Type", "image/jpeg")
	c.Header("Cache-Control", "public, max-age=86400, immutable")
	_, _ = io.Copy(c.Writer, rc)
}

// SaveLabel godoc
// @Summary Save human annotation label for a snapshot
// @Description Record ground truth label for flood, traffic, weather, and time of day
// @Tags archiver
// @Accept json
// @Produce json
// @Param payload body LabelPayload true "Label annotation payload"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /api/archiver/labels [post]
func (h *Handler) SaveLabel(c *gin.Context) {
	var payload LabelPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid label payload: " + err.Error()})
		return
	}

	payload.Date = strings.TrimSpace(payload.Date)
	if !datePattern.MatchString(payload.Date) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "date must be in YYYY-MM-DD format"})
		return
	}

	annotator := strings.TrimSpace(payload.Annotator)
	if annotator == "" {
		annotator = "anonymous"
	}
	// Sanitize annotator string
	cleanAnnotator := path.Base(annotator)
	cleanAnnotator = strings.ReplaceAll(cleanAnnotator, "/", "_")
	cleanAnnotator = strings.ReplaceAll(cleanAnnotator, "\\", "_")
	payload.Annotator = cleanAnnotator

	if payload.Timestamp == 0 {
		payload.Timestamp = time.Now().Unix()
	}

	cleanCam := path.Base(strings.TrimSpace(payload.CameraID))
	labelKey := fmt.Sprintf("labels/%s/%s_%s_%d.json", payload.Date, cleanCam, cleanAnnotator, payload.Timestamp)

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to encode label json"})
		return
	}

	if err := h.store.Put(c.Request.Context(), labelKey, bytes.NewReader(data)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to persist label: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"path":   labelKey,
		"label":  payload,
	})
}

// GetLabels godoc
// @Summary List ground truth labels for date
// @Description Retrieve saved human labels for a given partition date
// @Tags archiver
// @Produce json
// @Param date query string false "Partition date in YYYY-MM-DD format"
// @Param camera_id query string false "Filter by camera ID"
// @Success 200 {object} LabelListResponse
// @Failure 500 {object} map[string]string
// @Router /api/archiver/labels [get]
func (h *Handler) GetLabels(c *gin.Context) {
	date := strings.TrimSpace(c.Query("date"))
	if date == "" {
		date = time.Now().In(ICT).Format("2006-01-02")
	}

	camFilter := strings.TrimSpace(c.Query("camera_id"))
	prefix := "labels/" + date

	items, err := h.store.List(c.Request.Context(), prefix)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list labels"})
		return
	}

	var labels []LabelPayload
	for _, it := range items {
		if !strings.HasSuffix(it.Key, ".json") {
			continue
		}

		rc, err := h.store.Get(c.Request.Context(), it.Key)
		if err != nil {
			continue
		}

		data, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			continue
		}

		var lp LabelPayload
		if err := json.Unmarshal(data, &lp); err != nil {
			continue
		}

		if camFilter != "" && lp.CameraID != camFilter {
			continue
		}

		labels = append(labels, lp)
	}

	if labels == nil {
		labels = []LabelPayload{}
	}

	c.JSON(http.StatusOK, LabelListResponse{
		Date:   date,
		Total:  len(labels),
		Labels: labels,
	})
}
