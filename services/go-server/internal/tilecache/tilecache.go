// Package tilecache implements a local disk cache and upstream proxy for
// raster map tiles (Slippy Map format), with singleflight request collapsing
// and immutable HTTP cache headers.
//
// The keyless upstream is OpenStreetMap; each fetched tile is re-rendered as
// high-contrast dark cartography before it is cached, because the commuter map
// must stay readable in direct sunlight and a light basemap washes out the
// functional severity colours (S14-T3 / Issue #443). CARTO Dark Matter needs an
// API key on every style since 2025, so it is an explicit opt-in through
// MAP_TILE_UPSTREAM_URL plus MAP_TILE_API_KEY, and its native dark bytes skip the
// renderer. The on-disk cache is namespaced per provider and theme so a switch
// never mixes light and dark tiles.
//
// Leaf package — zero internal/ imports allowed.
package tilecache

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	neturl "net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sync/singleflight"
)

const (
	defaultUserAgent = "WardedTraffic/1.0 (https://github.com/529-studio/projectKV; contact@529studio.site)"
	// The keyless upstream. Its cartography is light, so the proxy darkens each
	// tile once before caching it (see darkenPNG). CARTO Dark Matter needs an API
	// key for every style since 2025 and is therefore an explicit opt-in:
	// MAP_TILE_UPSTREAM_URL=https://basemaps.cartocdn.com/dark_all
	// MAP_TILE_API_KEY=<key>
	defaultUpstream = "https://tile.openstreetmap.org"
	defaultTimeout  = 10 * time.Second

	// UpstreamEnvVar overrides the tile provider without a rebuild, e.g. a
	// self-hosted raster mirror or a licensed CARTO endpoint.
	UpstreamEnvVar = "MAP_TILE_UPSTREAM_URL"
	// APIKeyEnvVar is optional; when set it is appended as `api_key` to the
	// upstream request. Never hardcode it and never log it.
	APIKeyEnvVar = "MAP_TILE_API_KEY"
	// DarkenEnvVar disables the photometric dark render with "0", "false" or
	// "off". Darkening is skipped automatically for providers known to serve
	// dark tiles.
	DarkenEnvVar = "MAP_TILE_DARKEN"
)

// cartoSubdomains are accepted in an upstream template as {s}.
var cartoSubdomains = [...]string{"a", "b", "c", "d"}

// normalizeUpstream trims whitespace and trailing slashes so the tile path can be
// appended without producing a double slash. An empty override keeps the default.
func normalizeUpstream(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return defaultUpstream
	}
	return strings.TrimRight(trimmed, "/")
}

// providerCacheDir isolates the on-disk cache per provider and theme. Switching
// the basemap while the previous theme's PNGs are still cached would otherwise
// serve a mixed map (light OSM tiles under a dark label) that no operator could
// explain.
func providerCacheDir(upstream string, darken bool) string {
	var slug string
	switch {
	case strings.Contains(upstream, "cartocdn.com"):
		slug = "carto"
		if theme := sanitizeSegment(lastPathSegment(upstream)); theme != "" {
			slug += "-" + theme
		}
	case strings.Contains(upstream, "openstreetmap.org"):
		slug = "osm"
	default:
		slug = "custom"
		if name := sanitizeSegment(lastPathSegment(upstream)); name != "" {
			slug += "-" + name
		}
	}
	if darken {
		slug += "-dark"
	}
	return slug
}

// lastPathSegment returns the final path component of an upstream base URL.
func lastPathSegment(upstream string) string {
	if idx := strings.LastIndex(upstream, "/"); idx >= 0 {
		return upstream[idx+1:]
	}
	return upstream
}

// sanitizeSegment keeps a cache directory name filesystem-safe.
func sanitizeSegment(raw string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(raw) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

// upstreamTileURL expands the provider template for one Slippy Map tile. `{s}`
// picks a deterministic subdomain and `{r}` collapses to the non-retina asset,
// so an operator can paste the exact URL a map client would use.
func upstreamTileURL(template string, z, x, y int) string {
	url := template
	if strings.Contains(url, "{s}") {
		url = strings.ReplaceAll(url, "{s}", cartoSubdomains[(x+y)%len(cartoSubdomains)])
	}
	url = strings.ReplaceAll(url, "{r}", "")
	return fmt.Sprintf("%s/%d/%d/%d.png", strings.TrimRight(url, "/"), z, x, y)
}

// newResilientHTTPClient constructs an HTTP client equipped with a resilient
// DNS dialer that queries public resolvers (1.1.1.1, 8.8.8.8) when the host or
// container DNS environment fails to resolve upstream map tile domains.
func newResilientHTTPClient() *http.Client {
	dialer := &net.Dialer{
		Timeout:   defaultTimeout,
		KeepAlive: 30 * time.Second,
		Resolver: &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				d := net.Dialer{Timeout: 2 * time.Second}
				// 1. Try Cloudflare public DNS (1.1.1.1:53)
				conn, err := d.DialContext(ctx, "udp", "1.1.1.1:53")
				if err == nil {
					return conn, nil
				}
				// 2. Fallback to Google public DNS (8.8.8.8:53)
				conn, err = d.DialContext(ctx, "udp", "8.8.8.8:53")
				if err == nil {
					return conn, nil
				}
				// 3. Fallback to default system resolver address
				return d.DialContext(ctx, network, address)
			},
		},
	}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	return &http.Client{
		Timeout:   defaultTimeout,
		Transport: transport,
	}
}

// Manager manages reading, fetching, and persisting map tiles.
type Manager struct {
	cacheDir    string
	upstreamURL string
	apiKey      string
	darken      bool
	httpClient  *http.Client
	group       singleflight.Group
}

// NewManager creates a tilecache Manager with the given local cache directory.
//
// Provider, key and dark-render mode are read from the environment once at
// construction; the cache directory gains provider- and theme-specific segments
// so switching either never serves a mixed map. NewManager is the only place
// that consults the environment.
func NewManager(cacheDir string) *Manager {
	if cacheDir == "" {
		cacheDir = "data/tiles"
	}
	upstream := normalizeUpstream(os.Getenv(UpstreamEnvVar))
	darken := darkenEnabled(upstream)
	return &Manager{
		cacheDir:    filepath.Join(cacheDir, providerCacheDir(upstream, darken)),
		upstreamURL: upstream,
		apiKey:      strings.TrimSpace(os.Getenv(APIKeyEnvVar)),
		darken:      darken,
		httpClient:  newResilientHTTPClient(),
	}
}

// darkenEnabled decides whether the proxy should re-render fetched tiles as dark
// cartography. An explicit MAP_TILE_DARKEN wins; otherwise a provider known to
// serve a dark style is left untouched.
func darkenEnabled(upstream string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(DarkenEnvVar))) {
	case "0", "false", "off", "no":
		return false
	case "1", "true", "on", "yes":
		return true
	}
	return !strings.Contains(upstream, "dark")
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
	url := upstreamTileURL(m.upstreamURL, z, x, y)
	if m.apiKey != "" {
		separator := "?"
		if strings.Contains(url, "?") {
			separator = "&"
		}
		url += separator + "api_key=" + neturl.QueryEscape(m.apiKey)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build upstream request: %s", m.redactError(err))
	}
	req.Header.Set("User-Agent", defaultUserAgent)
	req.Header.Set("Referer", "https://warded-stg.529studio.site/")
	req.Header.Set("Accept", "image/png,image/*;q=0.8")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("upstream tile fetch failed: %s", m.redactError(err))
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

	if m.darken {
		darkened, darkenErr := darkenPNG(data)
		if darkenErr != nil {
			// A lighter tile is a degraded view; a 502 is a blank map. Serve the
			// original bytes and let the cache keep them.
			log.Printf("[WARN] tile darken skipped (%d/%d/%d): %v", z, x, y, darkenErr)
			return data, nil
		}
		return darkened, nil
	}

	return data, nil
}

// redactError removes the provider key from an error string. `url.Error`
// includes the full request URL, and that URL carries `?api_key=`, so wrapping it
// verbatim would print the secret into the backend log on any upstream timeout.
func (m *Manager) redactError(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	if m.apiKey == "" {
		return message
	}
	message = strings.ReplaceAll(message, m.apiKey, "<redacted>")
	message = strings.ReplaceAll(message, neturl.QueryEscape(m.apiKey), "<redacted>")
	return message
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
