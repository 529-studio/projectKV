package hotset

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
)

// DefaultFrozenThreshold is the default number of consecutive identical frames required to mark a feed as frozen.
const DefaultFrozenThreshold = 3

// FrozenDetector detects frozen camera feeds by comparing frame hashes across cycles.
// When a camera produces K identical frame hashes in a row (default K=3),
// the camera's feed is considered frozen, and Check returns true (stale).
type FrozenDetector struct {
	mu        sync.Mutex
	threshold int
	history   map[string]*cameraFrameHistory
}

type cameraFrameHistory struct {
	lastHash       string
	identicalCount int
}

// NewFrozenDetector creates a FrozenDetector with the given consecutive identical frame threshold K.
// If threshold <= 0, DefaultFrozenThreshold (3) is used.
func NewFrozenDetector(threshold int) *FrozenDetector {
	if threshold <= 0 {
		threshold = DefaultFrozenThreshold
	}
	return &FrozenDetector{
		threshold: threshold,
		history:   make(map[string]*cameraFrameHistory),
	}
}

// Check compares frameHash with the camera's previously recorded frame hash.
// If identical for K consecutive calls (including this one), returns true (stale).
// If a different hash is seen, the counter resets to 1 and returns false.
// If cameraID or frameHash is empty, returns false.
func (d *FrozenDetector) Check(cameraID string, frameHash string) bool {
	if cameraID == "" || frameHash == "" {
		return false
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	hist, exists := d.history[cameraID]
	if !exists {
		d.history[cameraID] = &cameraFrameHistory{
			lastHash:       frameHash,
			identicalCount: 1,
		}
		return 1 >= d.threshold
	}

	if hist.lastHash == frameHash {
		hist.identicalCount++
	} else {
		hist.lastHash = frameHash
		hist.identicalCount = 1
	}

	return hist.identicalCount >= d.threshold
}

// CheckBytes computes the SHA-256 hash of data and checks if the feed is frozen.
func (d *FrozenDetector) CheckBytes(cameraID string, data []byte) bool {
	if len(data) == 0 {
		return false
	}
	h := sha256.Sum256(data)
	return d.Check(cameraID, hex.EncodeToString(h[:]))
}

// Reset clears the frame history for a specific camera.
func (d *FrozenDetector) Reset(cameraID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.history, cameraID)
}
