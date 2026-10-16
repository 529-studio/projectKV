package hotset

import (
	"sync"
	"time"
)

// Store provides a thread-safe in-memory cache for Hot Set verdicts and saturation indexes.
// It fulfills the sub-5ms latency requirement for GET /api/v1/cameras/hotset/status.
type Store struct {
	mu       sync.RWMutex
	verdicts map[string]Verdict
	staleTTL time.Duration
}

// NewStore initializes a new in-memory Store with a staleness threshold.
func NewStore(staleTTL time.Duration) *Store {
	if staleTTL <= 0 {
		staleTTL = 180 * time.Second
	}
	return &Store{
		verdicts: make(map[string]Verdict),
		staleTTL: staleTTL,
	}
}

// Set stores or updates a verdict for a camera.
func (s *Store) Set(v Verdict) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.verdicts[v.CameraID] = v
}

// Get returns the latest cached verdict for a camera, recalculating Stale based on staleTTL.
func (s *Store) Get(cameraID string) (Verdict, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.verdicts[cameraID]
	if !ok {
		return Verdict{}, false
	}
	v.Stale = s.isStale(v)
	return v, true
}

// GetLatest returns a slice of all cached verdicts, satisfying the VerdictStorer interface.
func (s *Store) GetLatest() []Verdict {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Verdict, 0, len(s.verdicts))
	for _, v := range s.verdicts {
		v.Stale = s.isStale(v)
		out = append(out, v)
	}
	return out
}

// Size returns the count of currently cached camera verdicts.
func (s *Store) Size() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.verdicts)
}

func (s *Store) isStale(v Verdict) bool {
	if v.Stale {
		return true
	}
	obs := v.ObservedAt
	if obs.IsZero() {
		obs = v.Timestamp
	}
	if obs.IsZero() {
		return false
	}
	return time.Since(obs) > s.staleTTL
}
