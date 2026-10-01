package live

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"

	"github.com/529-studio/projectKV/services/go-server/internal/camera"
	"github.com/529-studio/projectKV/services/go-server/internal/hotset"
)

// HexCell represents an aggregated H3 hexagonal spatial cell for map overlays.
type HexCell struct {
	H3ID            string      `json:"h3_id"`
	Resolution      int         `json:"resolution"`
	Severity        string      `json:"severity"`          // worst-of
	CameraCount     int         `json:"camera_count"`      // qualifying cameras
	WorstCamera     string      `json:"worst_camera"`      // camera_id of worst verdict
	WorstCameraName string      `json:"worst_camera_name"` // display name
	Polygon         [][]float64 `json:"polygon"`           // [[lat,lng], ...] boundary for rendering
}

// HexBroadcaster broadcasts hex updates to connected SSE clients.
type HexBroadcaster interface {
	BroadcastHex(cell HexCell)
}

// SeverityRank maps severity levels to an integer rank for worst-of comparison.
// Precedence: CONGESTED/FLOODED_DEEP (4) > HEAVY/FLOODED_SHALLOW (3) > MODERATE/WET (2) > CLEAR/DRY (1).
// UNKNOWN or unrecognized severities return 0 (non-qualifying).
func SeverityRank(s string) int {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case hotset.SeverityCongested, hotset.SeverityFloodedDeep:
		return 4
	case hotset.SeverityHeavy, hotset.SeverityFloodedShallow:
		return 3
	case hotset.SeverityModerate, hotset.SeverityWet:
		return 2
	case hotset.SeverityClear, hotset.SeverityDry:
		return 1
	default:
		return 0
	}
}

// HexAggregator groups cameras by precomputed H3 cell ID and aggregates verdicts
// using worst-of logic per ADR-004 §D11. Safe for concurrent use.
type HexAggregator struct {
	mu          sync.RWMutex
	resolution  int
	minCameras  int
	broadcaster HexBroadcaster
	log         *slog.Logger
	cameras     map[string]camera.Camera
	cellCameras map[string][]string // h3_id -> list of camera IDs
	camToCell   map[string]string   // camera ID -> h3_id
	polygons    map[string][][]float64
	verdicts    map[string]hotset.Verdict // camera ID -> latest verdict
	allowMock   bool                      // allow mock verdicts when running in dev mode
}

// SetAllowMock enables or disables aggregation of mock verdicts for local development testing.
func (h *HexAggregator) SetAllowMock(allow bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.allowMock = allow
}

// NewHexAggregator constructs a HexAggregator with indexed camera catalog and polygons.
func NewHexAggregator(
	cameras []camera.Camera,
	polygons map[string][][]float64,
	resolution int,
	minCameras int,
	broadcaster HexBroadcaster,
	log *slog.Logger,
) *HexAggregator {
	if resolution <= 0 {
		resolution = 7
	}
	if minCameras <= 0 {
		minCameras = 3
	}
	if polygons == nil {
		polygons = make(map[string][][]float64)
	}
	if log == nil {
		log = slog.Default()
	}

	agg := &HexAggregator{
		resolution:  resolution,
		minCameras:  minCameras,
		broadcaster: broadcaster,
		log:         log,
		cameras:     make(map[string]camera.Camera, len(cameras)),
		cellCameras: make(map[string][]string),
		camToCell:   make(map[string]string, len(cameras)),
		polygons:    polygons,
		verdicts:    make(map[string]hotset.Verdict),
	}

	for _, c := range cameras {
		agg.cameras[c.ID] = c
		h3ID := getCameraH3ID(c, resolution)
		if h3ID != "" {
			agg.camToCell[c.ID] = h3ID
			agg.cellCameras[h3ID] = append(agg.cellCameras[h3ID], c.ID)
		}
	}

	return agg
}

func getCameraH3ID(c camera.Camera, resolution int) string {
	switch resolution {
	case 9:
		return strings.TrimSpace(c.H3Res9)
	case 7:
		return strings.TrimSpace(c.H3Res7)
	default:
		if res := strings.TrimSpace(c.H3Res7); res != "" {
			return res
		}
		return strings.TrimSpace(c.H3Res9)
	}
}

// OnVerdict updates internal verdict state and broadcasts a hex_update event if
// the affected cell meets the qualification threshold.
func (h *HexAggregator) OnVerdict(v hotset.Verdict) {
	_, _ = h.ProcessVerdict(v)
}

// ProcessVerdict processes a new verdict, aggregates the affected cell, and broadcasts
// a hex_update event if the cell has >= minCameras qualifying verdicts.
// Returns the HexCell and true if qualifying; otherwise nil and false.
func (h *HexAggregator) ProcessVerdict(v hotset.Verdict) (*HexCell, bool) {
	h.mu.Lock()
	h3ID, ok := h.camToCell[v.CameraID]
	if !ok || h3ID == "" {
		h.mu.Unlock()
		return nil, false
	}

	h.verdicts[v.CameraID] = v
	cell, hasData := h.aggregateCellLocked(h3ID)
	h.mu.Unlock()

	if hasData && h.broadcaster != nil {
		h.broadcaster.BroadcastHex(*cell)
	}

	return cell, hasData
}

// GetCell returns the current aggregate for a cell if it has >= minCameras qualifying verdicts.
func (h *HexAggregator) GetCell(h3ID string) (*HexCell, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.aggregateCellLocked(h3ID)
}

// GetAllCells returns current aggregates for all qualifying cells.
func (h *HexAggregator) GetAllCells() []HexCell {
	h.mu.RLock()
	defer h.mu.RUnlock()
	var cells []HexCell
	for h3ID := range h.cellCameras {
		if cell, ok := h.aggregateCellLocked(h3ID); ok {
			cells = append(cells, *cell)
		}
	}
	return cells
}

func (h *HexAggregator) aggregateCellLocked(h3ID string) (*HexCell, bool) {
	camIDs := h.cellCameras[h3ID]
	if len(camIDs) < h.minCameras {
		return nil, false
	}

	var qualifyingVerdicts []hotset.Verdict
	var qualifyingCams []camera.Camera

	for _, camID := range camIDs {
		v, exists := h.verdicts[camID]
		if !exists {
			continue
		}
		// Floor criteria (ADR-004 §D11 Correction 1):
		// Fresh (stale == false), non-mock (source != "mock" unless allowMock is enabled in dev), and valid severity rank > 0.
		if v.Stale || (!h.allowMock && v.Source == "mock") || SeverityRank(v.Severity) <= 0 {
			continue
		}
		qualifyingVerdicts = append(qualifyingVerdicts, v)
		qualifyingCams = append(qualifyingCams, h.cameras[camID])
	}

	if len(qualifyingVerdicts) < h.minCameras {
		return nil, false
	}

	worstIdx := 0
	worstRank := SeverityRank(qualifyingVerdicts[0].Severity)
	for i := 1; i < len(qualifyingVerdicts); i++ {
		r := SeverityRank(qualifyingVerdicts[i].Severity)
		if r > worstRank {
			worstRank = r
			worstIdx = i
		}
	}

	worstVerdict := qualifyingVerdicts[worstIdx]
	worstCam := qualifyingCams[worstIdx]
	worstName := cameraDisplayName(worstCam)

	poly := h.polygons[h3ID]
	if poly == nil {
		poly = [][]float64{}
	}

	cell := &HexCell{
		H3ID:            h3ID,
		Resolution:      h.resolution,
		Severity:        worstVerdict.Severity,
		CameraCount:     len(qualifyingVerdicts),
		WorstCamera:     worstVerdict.CameraID,
		WorstCameraName: worstName,
		Polygon:         poly,
	}

	return cell, true
}

func cameraDisplayName(c camera.Camera) string {
	if name := strings.TrimSpace(c.DisplayName); name != "" {
		return name
	}
	if name := strings.TrimSpace(c.Title); name != "" {
		return name
	}
	return c.ID
}

// LoadPolygons reads precomputed H3 polygon coordinates from a JSON file.
// The JSON format is a map of H3 ID string to an array of [lat, lng] coordinate pairs.
func LoadPolygons(filePath string) (map[string][][]float64, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read polygons file: %w", err)
	}

	var polygons map[string][][]float64
	if err := json.Unmarshal(data, &polygons); err != nil {
		return nil, fmt.Errorf("unmarshal polygons json: %w", err)
	}

	return polygons, nil
}
