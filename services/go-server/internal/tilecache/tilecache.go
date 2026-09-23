// Package tilecache implements a local disk cache and upstream proxy for
// map tiles (OpenStreetMap Slippy Map format), with singleflight request
// collapsing and immutable HTTP cache headers.
//
// Leaf package — zero internal/ imports allowed.
package tilecache

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sync/singleflight"
)

const (
	defaultUserAgent = "WardedTraffic/1.0 (https://github.com/529-studio/projectKV; contact@529studio.site)"
	defaultUpstream  = "https://tile.openstreetmap.org"
	defaultTimeout   = 10 * time.Second
)

// Manager manages reading, fetching, and persisting map tiles.
type Manager struct {
	cacheDir    string
	upstreamURL string
	httpClient  *http.Client
	group       singleflight.Group
}

// NewManager creates a tilecache Manager with the given local cache directory.
func NewManager(cacheDir string) *Manager {
	if cacheDir == "" {
		cacheDir = "data/tiles"
	}
	return &Manager{
		cacheDir:    cacheDir,
		upstreamURL: defaultUpstream,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
	}
}

// ValidateCoords checks whether z, x, y form valid Slippy Map tile coordinates.
func ValidateCoords(z, x, y int) bool {
	if z < 0 || z > 19 {
		return false
	}
	maxCoord := (1 << z) - 1
	return x >= 0 && x <= maxCoord && y >= 0 && y <= maxCoord
}

// tileResult is passed through singleflight.
type tileResult struct {
	data []byte
}

// GetTile retrieves a tile by (z, x, y). If present on disk, it returns the cached
// bytes immediately (hit=true). Otherwise, it fetches the tile from the upstream
// tile server, saves it to disk atomically, and returns the bytes (hit=false).
// Concurrent requests for the same tile are collapsed via singleflight.
func (m *Manager) GetTile(ctx context.Context, z, x, y int) ([]byte, bool, error) {
	tilePath := filepath.Join(m.cacheDir, fmt.Sprintf("%d/%d/%d.png", z, x, y))

	// Fast path: check local disk cache
	if data, err := os.ReadFile(tilePath); err == nil && len(data) > 0 {
		return data, true, nil
	}

	// Slow path: deduplicated upstream fetch
	flightKey := fmt.Sprintf("%d/%d/%d", z, x, y)
	val, err, _ := m.group.Do(flightKey, func() (interface{}, error) {
		// Double check disk cache in case a previous singleflight just saved it
		if data, err := os.ReadFile(tilePath); err == nil && len(data) > 0 {
			return tileResult{data: data}, nil
		}

		data, err := m.fetchUpstream(ctx, z, x, y)
		if err != nil {
			return nil, err
		}

		// Persist tile to disk asynchronously or immediately
		_ = m.saveToDisk(tilePath, data)

		return tileResult{data: data}, nil
	})

	if err != nil {
		return nil, false, err
	}

	res := val.(tileResult)
	return res.data, false, nil
}

func (m *Manager) fetchUpstream(ctx context.Context, z, x, y int) ([]byte, error) {
	url := fmt.Sprintf("%s/%d/%d/%d.png", m.upstreamURL, z, x, y)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build upstream request: %w", err)
	}
	req.Header.Set("User-Agent", defaultUserAgent)
	req.Header.Set("Accept", "image/png,image/*;q=0.8")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("upstream tile fetch failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upstream tile server returned status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read upstream tile body: %w", err)
	}

	if len(data) == 0 {
		return nil, fmt.Errorf("empty upstream tile body")
	}

	return data, nil
}

func (m *Manager) saveToDisk(tilePath string, data []byte) error {
	dir := filepath.Dir(tilePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create tile dir: %w", err)
	}

	tmpFile := fmt.Sprintf("%s.tmp.%d", tilePath, time.Now().UnixNano())
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		return fmt.Errorf("write temp tile: %w", err)
	}

	if err := os.Rename(tmpFile, tilePath); err != nil {
		_ = os.Remove(tmpFile)
		return fmt.Errorf("rename tile: %w", err)
	}

	return nil
}
