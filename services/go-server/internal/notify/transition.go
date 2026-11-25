package notify

import (
	"strings"
	"sync"
	"time"
)

// StateTransition records the evaluated transition between previous and current hazard states.
type StateTransition struct {
	EntityID     string
	Previous     string
	Current      string
	Transitioned bool
	IsEscalation bool
	IsRecovery   bool
	ObservedAt   time.Time
}

// StateTracker manages past known hazard severities and passabilities for cameras and corridors.
// Zero emojis per Rule 1.
type StateTracker struct {
	mu             sync.RWMutex
	cameraStates   map[string]string // cameraID -> last severity
	corridorStates map[string]string // roadKey -> last passability
}

// NewStateTracker initializes a new thread-safe StateTracker.
func NewStateTracker() *StateTracker {
	return &StateTracker{
		cameraStates:   make(map[string]string),
		corridorStates: make(map[string]string),
	}
}

// cameraSeverityRank returns numerical rank for camera flood severity:
// FLOODED_DEEP (3) > FLOODED_SHALLOW (2) > CLEAR/WET/DRY (1) > UNKNOWN (0).
func cameraSeverityRank(sev string) int {
	switch strings.ToUpper(strings.TrimSpace(sev)) {
	case "FLOODED_DEEP":
		return 3
	case "FLOODED_SHALLOW":
		return 2
	case "CLEAR", "WET", "DRY", "NORMAL":
		return 1
	default:
		return 0
	}
}

// corridorPassabilityRank returns numerical rank for corridor passability:
// IMPASSABLE (3) > CAUTION (2) > PASSABLE/CLEAR (1) > UNKNOWN (0).
func corridorPassabilityRank(pass string) int {
	switch strings.ToUpper(strings.TrimSpace(pass)) {
	case "IMPASSABLE":
		return 3
	case "CAUTION":
		return 2
	case "PASSABLE", "CLEAR":
		return 1
	default:
		return 0
	}
}

// RecordCamera records a camera observation and computes the state transition.
func (st *StateTracker) RecordCamera(cameraID string, severity string, observedAt time.Time) StateTransition {
	st.mu.Lock()
	defer st.mu.Unlock()

	normSev := strings.ToUpper(strings.TrimSpace(severity))
	prev, exists := st.cameraStates[cameraID]
	st.cameraStates[cameraID] = normSev

	prevRank := cameraSeverityRank(prev)
	currRank := cameraSeverityRank(normSev)

	transitioned := !exists || prev != normSev
	isEscalation := currRank >= 2 && currRank > prevRank
	isRecovery := prevRank >= 2 && currRank < 2

	return StateTransition{
		EntityID:     cameraID,
		Previous:     prev,
		Current:      normSev,
		Transitioned: transitioned,
		IsEscalation: isEscalation,
		IsRecovery:   isRecovery,
		ObservedAt:   observedAt,
	}
}

// RecordCorridor records a corridor passability observation and computes the state transition.
func (st *StateTracker) RecordCorridor(roadKey string, passability string, observedAt time.Time) StateTransition {
	st.mu.Lock()
	defer st.mu.Unlock()

	normPass := strings.ToUpper(strings.TrimSpace(passability))
	prev, exists := st.corridorStates[roadKey]
	st.corridorStates[roadKey] = normPass

	prevRank := corridorPassabilityRank(prev)
	currRank := corridorPassabilityRank(normPass)

	transitioned := !exists || prev != normPass
	isEscalation := currRank >= 2 && currRank > prevRank
	isRecovery := prevRank >= 2 && currRank < 2

	return StateTransition{
		EntityID:     roadKey,
		Previous:     prev,
		Current:      normPass,
		Transitioned: transitioned,
		IsEscalation: isEscalation,
		IsRecovery:   isRecovery,
		ObservedAt:   observedAt,
	}
}

// GetCameraState returns the last recorded severity for a camera.
func (st *StateTracker) GetCameraState(cameraID string) string {
	st.mu.RLock()
	defer st.mu.RUnlock()
	return st.cameraStates[cameraID]
}

// GetCorridorState returns the last recorded passability for a corridor.
func (st *StateTracker) GetCorridorState(roadKey string) string {
	st.mu.RLock()
	defer st.mu.RUnlock()
	return st.corridorStates[roadKey]
}

// Reset clears tracked camera and corridor states (primarily for testing).
func (st *StateTracker) Reset() {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.cameraStates = make(map[string]string)
	st.corridorStates = make(map[string]string)
}
