package archiver

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/529-studio/projectKV/services/go-server/internal/hotset"
)

// ArchiveReason specifies why a frame was selected for archiving.
type ArchiveReason string

const (
	ReasonStateChange ArchiveReason = "state_change"
	ReasonSample      ArchiveReason = "sample"
	ReasonEval        ArchiveReason = "eval"
	ReasonNone        ArchiveReason = ""
)

// PolicyEngine determines whether an incoming verdict should trigger snapshot archiving.
type PolicyEngine struct {
	mu             sync.Mutex
	sampleInterval time.Duration
	lastSeverities map[string]string
	lastSampled    map[string]time.Time
	evalTagged     map[string]bool
}

// NewPolicyEngine creates a new policy engine with the specified sample interval.
func NewPolicyEngine(sampleInterval time.Duration) *PolicyEngine {
	if sampleInterval <= 0 {
		sampleInterval = 300 * time.Second
	}
	return &PolicyEngine{
		sampleInterval: sampleInterval,
		lastSeverities: make(map[string]string),
		lastSampled:    make(map[string]time.Time),
		evalTagged:     make(map[string]bool),
	}
}

// IsJamOrDeep checks whether the severity represents a jam or deep flood condition.
func IsJamOrDeep(severity string) bool {
	s := strings.ToUpper(strings.TrimSpace(severity))
	switch s {
	case hotset.SeverityCongested, "JAM", hotset.SeverityFloodedDeep, "DEEP", hotset.SeverityHeavy:
		return true
	default:
		return false
	}
}

// ICT defines the Vietnam Standard Timezone (Asia/Ho_Chi_Minh / UTC+7) per DevOps object key standard.
var ICT = time.FixedZone("ICT", 7*3600)

// StoragePath generates the standard storage path: {date}/{camera_id}/{timestamp}_{reason}.jpg
// The date directory is strictly formatted according to Vietnam ICT (Asia/Ho_Chi_Minh).
func StoragePath(t time.Time, cameraID string, timestamp int64, reason ArchiveReason) string {
	if t.IsZero() {
		t = time.Unix(timestamp, 0).In(ICT)
	} else {
		t = t.In(ICT)
	}
	if timestamp == 0 {
		timestamp = t.Unix()
	}
	dateStr := t.Format("2006-01-02")
	cleanID := filepath.Base(strings.TrimSpace(cameraID))
	if cleanID == "" || cleanID == "." {
		cleanID = "unknown"
	}
	return fmt.Sprintf("%s/%s/%d_%s.jpg", dateStr, cleanID, timestamp, reason)
}

// TagForEval tags a camera feed to be archived for training/evaluation dataset.
func (p *PolicyEngine) TagForEval(cameraID string, tagged bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if tagged {
		p.evalTagged[cameraID] = true
	} else {
		delete(p.evalTagged, cameraID)
	}
}

// ShouldArchive decides whether a frame for the verdict should be archived,
// and returns the archive reason.
func (p *PolicyEngine) ShouldArchive(v hotset.Verdict, now time.Time) (bool, ArchiveReason) {
	if now.IsZero() {
		if !v.Timestamp.IsZero() {
			now = v.Timestamp
		} else {
			now = time.Now().UTC()
		}
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// 1. Eval set: frames explicitly tagged or marked for eval dataset
	if v.Source == "eval" || p.evalTagged[v.CameraID] {
		p.lastSampled[v.CameraID] = now
		return true, ReasonEval
	}

	currSev := strings.ToUpper(strings.TrimSpace(v.Severity))
	prevSev, hasPrev := p.lastSeverities[v.CameraID]
	p.lastSeverities[v.CameraID] = currSev

	// 2. Event-driven: state change to jam/deep (traffic or flood)
	if IsJamOrDeep(currSev) {
		if !hasPrev || prevSev != currSev {
			p.lastSampled[v.CameraID] = now
			return true, ReasonStateChange
		}
	}

	// 3. Sampled: 1 frame per sampleInterval per Hot Set camera
	lastSampleTime, hasSampled := p.lastSampled[v.CameraID]
	if !hasSampled || now.Sub(lastSampleTime) >= p.sampleInterval {
		p.lastSampled[v.CameraID] = now
		return true, ReasonSample
	}

	// Everything else is not stored
	return false, ReasonNone
}
