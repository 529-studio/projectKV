package live

import (
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/529-studio/projectKV/services/go-server/internal/camera"
	"github.com/529-studio/projectKV/services/go-server/internal/hotset"
)

// FloodHexCell represents an aggregated H3 hexagonal spatial cell for urban rain and flood overlays.
type FloodHexCell struct {
	H3ID            string      `json:"h3_id"`
	Resolution      int         `json:"resolution"`
	FloodSeverity   string      `json:"flood_severity"`    // worst-of: FLOODED_DEEP > FLOODED_SHALLOW > WET
	CameraCount     int         `json:"camera_count"`      // qualifying cameras
	WorstCamera     string      `json:"worst_camera"`      // camera_id of worst flood verdict
	WorstCameraName string      `json:"worst_camera_name"` // display name
	Polygon         [][]float64 `json:"polygon"`           // [[lat,lng], ...] boundary for rendering
}

// FloodBroadcaster broadcasts flood hex updates to connected SSE clients.
type FloodBroadcaster interface {
	BroadcastFloodHex(cell FloodHexCell)
}

// FloodSeverityRank maps flood severity levels to an integer rank for worst-of comparison.
// Precedence: FLOODED_DEEP (3) > FLOODED_SHALLOW (2) > WET (1) > DRY/UNKNOWN (0).
func FloodSeverityRank(s string) int {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case hotset.SeverityFloodedDeep:
		return 3
	case hotset.SeverityFloodedShallow:
		return 2
	case hotset.SeverityWet:
		return 1
	default:
		return 0
	}
}

// FloodHexAggregator groups cameras by precomputed H3 cell ID and aggregates flood verdicts
// using worst-of logic with a safety-critical floor of >= 1 camera per ADR-004 §D4/D11.
type FloodHexAggregator struct {
	mu          sync.RWMutex
	resolution  int
	minCameras  int
	broadcaster FloodBroadcaster
	log         *slog.Logger
	cameras     map[string]camera.Camera
	cellCameras map[string][]string // h3_id -> list of camera IDs
	camToCell   map[string]string   // camera ID -> h3_id
	polygons    map[string][][]float64
	verdicts    map[string]hotset.Verdict // camera ID -> latest verdict
	activeCells map[string]bool           // h3_id -> currently active cell
	staleTTL    time.Duration             // maximum observation freshness TTL
	allowMock   bool                      // allow mock verdicts when running in dev mode
}

// SetAllowMock enables or disables aggregation of mock verdicts for local development testing.
func (f *FloodHexAggregator) SetAllowMock(allow bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.allowMock = allow
}

// SetStaleTTL configures the freshness TTL before an observation is considered stale.
func (f *FloodHexAggregator) SetStaleTTL(ttl time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if ttl > 0 {
		f.staleTTL = ttl
	}
}

// NewFloodHexAggregator constructs a FloodHexAggregator.
func NewFloodHexAggregator(
	cameras []camera.Camera,
	polygons map[string][][]float64,
	resolution int,
	minCameras int,
	broadcaster FloodBroadcaster,
	log *slog.Logger,
) *FloodHexAggregator {
	if resolution <= 0 {
		resolution = 7
	}
	if minCameras <= 0 {
		minCameras = 1 // Safety-critical floor for flood is >= 1 camera
	}
	if polygons == nil {
		polygons = make(map[string][][]float64)
	}
	if log == nil {
		log = slog.Default()
	}

	agg := &FloodHexAggregator{
		resolution:  resolution,
		minCameras:  minCameras,
		broadcaster: broadcaster,
		log:         log,
		cameras:     make(map[string]camera.Camera, len(cameras)),
		cellCameras: make(map[string][]string),
		camToCell:   make(map[string]string, len(cameras)),
		polygons:    polygons,
		verdicts:    make(map[string]hotset.Verdict),
		activeCells: make(map[string]bool),
		staleTTL:    180 * time.Second,
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

// OnVerdict updates internal verdict state and broadcasts a flood_hex_update event if
// the affected cell meets the qualification threshold.
func (f *FloodHexAggregator) OnVerdict(v hotset.Verdict) {
	_, _ = f.ProcessVerdict(v)
}

// ProcessVerdict processes a new verdict and broadcasts flood_hex_update if qualifying,
// or a clearance event if a previously active cell transitions to empty/dry.
func (f *FloodHexAggregator) ProcessVerdict(v hotset.Verdict) (*FloodHexCell, bool) {
	f.mu.Lock()
	h3ID, ok := f.camToCell[v.CameraID]
	if !ok || h3ID == "" {
		f.mu.Unlock()
		return nil, false
	}

	f.verdicts[v.CameraID] = v
	cell, hasData := f.aggregateCellLocked(h3ID)
	wasActive := f.activeCells[h3ID]
	if hasData {
		f.activeCells[h3ID] = true
	} else {
		delete(f.activeCells, h3ID)
	}
	f.mu.Unlock()

	if hasData && f.broadcaster != nil {
		f.broadcaster.BroadcastFloodHex(*cell)
	} else if wasActive && !hasData && f.broadcaster != nil {
		poly := f.polygons[h3ID]
		if poly == nil {
			poly = [][]float64{}
		}
		f.broadcaster.BroadcastFloodHex(FloodHexCell{
			H3ID:          h3ID,
			Resolution:    f.resolution,
			CameraCount:   0,
			FloodSeverity: hotset.SeverityDry,
			Polygon:       poly,
		})
	}

	return cell, hasData
}

// GetCell returns the current flood aggregate for a cell.
func (f *FloodHexAggregator) GetCell(h3ID string) (*FloodHexCell, bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.aggregateCellLocked(h3ID)
}

// GetAllCells returns current aggregates for all qualifying flood cells,
// filtering out cells whose observations are stale past staleTTL.
func (f *FloodHexAggregator) GetAllCells() []FloodHexCell {
	f.mu.Lock()
	defer f.mu.Unlock()
	var cells []FloodHexCell
	for h3ID := range f.cellCameras {
		if cell, ok := f.aggregateCellLocked(h3ID); ok {
			cells = append(cells, *cell)
			f.activeCells[h3ID] = true
		} else {
			delete(f.activeCells, h3ID)
		}
	}
	return cells
}

// ReapStaleCells sweeps active cells, clears expired cells, and broadcasts removal events to SSE clients.
func (f *FloodHexAggregator) ReapStaleCells() []string {
	f.mu.Lock()
	var cleared []string
	for h3ID := range f.activeCells {
		cell, hasData := f.aggregateCellLocked(h3ID)
		if !hasData || cell == nil {
			delete(f.activeCells, h3ID)
			cleared = append(cleared, h3ID)
		}
	}
	f.mu.Unlock()

	if f.broadcaster != nil {
		for _, h3ID := range cleared {
			poly := f.polygons[h3ID]
			if poly == nil {
				poly = [][]float64{}
			}
			f.broadcaster.BroadcastFloodHex(FloodHexCell{
				H3ID:          h3ID,
				Resolution:    f.resolution,
				CameraCount:   0,
				FloodSeverity: hotset.SeverityDry,
				Polygon:       poly,
			})
		}
	}
	return cleared
}

func (f *FloodHexAggregator) aggregateCellLocked(h3ID string) (*FloodHexCell, bool) {
	camIDs := f.cellCameras[h3ID]
	if len(camIDs) == 0 {
		return nil, false
	}

	var qualifyingVerdicts []hotset.Verdict
	var qualifyingCams []camera.Camera

	for _, camID := range camIDs {
		v, exists := f.verdicts[camID]
		if !exists {
			continue
		}
		// Floor criteria:
		// Fresh (stale == false and within staleTTL), non-mock (unless allowMock in dev), and valid flood severity rank > 0 (WET, FLOODED_SHALLOW, FLOODED_DEEP).
		floodSev := v.FloodSeverity
		if floodSev == "" {
			floodSev = v.Severity
		}
		obs := v.ObservedAt
		if obs.IsZero() {
			obs = v.Timestamp
		}
		if v.Stale || (!obs.IsZero() && f.staleTTL > 0 && time.Since(obs) > f.staleTTL) || (!f.allowMock && v.Source == "mock") || FloodSeverityRank(floodSev) <= 0 {
			continue
		}
		v.FloodSeverity = floodSev
		qualifyingVerdicts = append(qualifyingVerdicts, v)
		qualifyingCams = append(qualifyingCams, f.cameras[camID])
	}

	if len(qualifyingVerdicts) < f.minCameras {
		return nil, false
	}

	worstIdx := 0
	worstRank := FloodSeverityRank(qualifyingVerdicts[0].FloodSeverity)
	for i := 1; i < len(qualifyingVerdicts); i++ {
		r := FloodSeverityRank(qualifyingVerdicts[i].FloodSeverity)
		if r > worstRank {
			worstRank = r
			worstIdx = i
		}
	}

	worstVerdict := qualifyingVerdicts[worstIdx]
	worstCam := qualifyingCams[worstIdx]
	worstName := cameraDisplayName(worstCam)

	poly := f.polygons[h3ID]
	if poly == nil {
		poly = [][]float64{}
	}

	cell := &FloodHexCell{
		H3ID:            h3ID,
		Resolution:      f.resolution,
		FloodSeverity:   worstVerdict.FloodSeverity,
		CameraCount:     len(qualifyingVerdicts),
		WorstCamera:     worstVerdict.CameraID,
		WorstCameraName: worstName,
		Polygon:         poly,
	}

	return cell, true
}
