package tilecache

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateCoords(t *testing.T) {
	tests := []struct {
		z, x, y int
		valid   bool
	}{
		{0, 0, 0, true},
		{1, 1, 1, true},
		{1, 2, 1, false}, // 2^(1) = 2, max index is 1
		{15, 26096, 15397, true},
		{-1, 0, 0, false},
		{20, 0, 0, false},
		{10, -1, 5, false},
		{10, 5, -1, false},
	}

	for _, tt := range tests {
		got := ValidateCoords(tt.z, tt.x, tt.y)
		if got != tt.valid {
			t.Errorf("ValidateCoords(%d, %d, %d) = %v; want %v", tt.z, tt.x, tt.y, got, tt.valid)
		}
	}
}

func TestTileCacheHitAndMiss(t *testing.T) {
	t.Setenv(UpstreamEnvVar, "")
	t.Setenv(DarkenEnvVar, "0")
	// Setup mock upstream HTTP server
	mockTileData := []byte("FAKE_PNG_DATA")
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(mockTileData)
	}))
	defer server.Close()

	// Temp cache directory
	tmpDir, err := os.MkdirTemp("", "tilecache_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mgr := NewManager(tmpDir)
	mgr.upstreamURL = server.URL // override upstream URL for test

	ctx := context.Background()

	// 1st request: Cache MISS -> upstream hit
	data1, hit1, err := mgr.GetTile(ctx, 12, 3200, 1900)
	if err != nil {
		t.Fatalf("unexpected error on 1st fetch: %v", err)
	}
	if hit1 {
		t.Errorf("expected 1st fetch to be MISS, got HIT")
	}
	if string(data1) != string(mockTileData) {
		t.Errorf("expected data %s, got %s", mockTileData, data1)
	}
	if requestCount != 1 {
		t.Errorf("expected upstream requestCount=1, got %d", requestCount)
	}

	// Verify file saved to disk, namespaced by provider so a theme change cannot
	// serve light tiles under a dark basemap.
	expectedFile := filepath.Join(tmpDir, providerCacheDir(defaultUpstream, false), "12", "3200", "1900.png")
	diskData, err := os.ReadFile(expectedFile)
	if err != nil {
		t.Fatalf("tile was not saved to disk at %s: %v", expectedFile, err)
	}
	if string(diskData) != string(mockTileData) {
		t.Errorf("disk data mismatch: got %s, want %s", diskData, mockTileData)
	}

	// 2nd request: Cache HIT -> read from disk, no upstream hit
	data2, hit2, err := mgr.GetTile(ctx, 12, 3200, 1900)
	if err != nil {
		t.Fatalf("unexpected error on 2nd fetch: %v", err)
	}
	if !hit2 {
		t.Errorf("expected 2nd fetch to be HIT, got MISS")
	}
	if string(data2) != string(mockTileData) {
		t.Errorf("expected data %s, got %s", mockTileData, data2)
	}
	if requestCount != 1 {
		t.Errorf("expected upstream requestCount to remain 1 on HIT, got %d", requestCount)
	}
}

func TestResilientHTTPClient(t *testing.T) {
	client := newResilientHTTPClient()
	if client == nil {
		t.Fatal("expected non-nil resilient HTTP client")
	}
	if client.Timeout != defaultTimeout {
		t.Errorf("expected timeout %v, got %v", defaultTimeout, client.Timeout)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport == nil {
		t.Fatal("expected *http.Transport configured on client")
	}
	if !transport.ForceAttemptHTTP2 {
		t.Errorf("expected ForceAttemptHTTP2=true")
	}
}

func TestNewManagerUsesKeylessOSMAndDarkensByDefault(t *testing.T) {
	t.Setenv(UpstreamEnvVar, "")
	t.Setenv(DarkenEnvVar, "")
	t.Setenv(APIKeyEnvVar, "")
	tmpDir := t.TempDir()

	mgr := NewManager(tmpDir)
	if mgr.upstreamURL != defaultUpstream {
		t.Errorf("upstreamURL = %q; want %q", mgr.upstreamURL, defaultUpstream)
	}
	if !mgr.darken {
		t.Error("expected the light keyless upstream to be darkened by default")
	}
	if want := filepath.Join(tmpDir, "osm-dark"); mgr.cacheDir != want {
		t.Errorf("cacheDir = %q; want %q", mgr.cacheDir, want)
	}
	if mgr.apiKey != "" {
		t.Errorf("apiKey = %q; want empty when MAP_TILE_API_KEY is unset", mgr.apiKey)
	}
}

func TestNewManagerHonoursUpstreamOverride(t *testing.T) {
	t.Setenv(UpstreamEnvVar, "  https://mirror.internal/osm/  ")
	t.Setenv(DarkenEnvVar, "")
	tmpDir := t.TempDir()

	mgr := NewManager(tmpDir)
	if mgr.upstreamURL != "https://mirror.internal/osm" {
		t.Errorf("upstreamURL = %q; want trimmed override", mgr.upstreamURL)
	}
	// A non-CARTO provider must not share the OSM cache directory.
	if mgr.cacheDir == filepath.Join(tmpDir, "osm-dark") {
		t.Errorf("cacheDir = %q; want a provider-specific directory", mgr.cacheDir)
	}
}

func TestDarkenSkipsNativeDarkProviders(t *testing.T) {
	t.Setenv(DarkenEnvVar, "")
	if !darkenEnabled(defaultUpstream) {
		t.Error("OSM raster is light cartography and must be darkened")
	}
	if darkenEnabled("https://basemaps.cartocdn.com/dark_all") {
		t.Error("a provider already serving a dark style must not be inverted again")
	}

	t.Setenv(DarkenEnvVar, "0")
	if darkenEnabled(defaultUpstream) {
		t.Error("MAP_TILE_DARKEN=0 must disable the renderer")
	}
	t.Setenv(DarkenEnvVar, "1")
	if !darkenEnabled("https://basemaps.cartocdn.com/dark_all") {
		t.Error("MAP_TILE_DARKEN=1 must force the renderer")
	}
}

func TestProviderCacheDirIsolatesProviderAndTheme(t *testing.T) {
	osmDark := providerCacheDir(defaultUpstream, true)
	osmoLight := providerCacheDir(defaultUpstream, false)
	carto := providerCacheDir("https://basemaps.cartocdn.com/dark_all", false)

	if osmDark == osmoLight {
		t.Fatalf("dark and light themes share cache dir %q", osmDark)
	}
	if osmDark == carto {
		t.Fatalf("OSM and CARTO share cache dir %q", osmDark)
	}
	if osmDark != "osm-dark" {
		t.Errorf("osm dark cache dir = %q; want osm-dark", osmDark)
	}
	if carto != "carto-dark_all" {
		t.Errorf("carto cache dir = %q; want carto-dark_all", carto)
	}
}

func TestUpstreamTileURLExpandsTemplate(t *testing.T) {
	tests := []struct {
		name     string
		template string
		x, y     int
		want     string
	}{
		{
			name:     "carto template picks a deterministic subdomain",
			template: "https://{s}.basemaps.cartocdn.com/dark_all/{r}",
			x:        3200,
			y:        1900,
			want:     "https://a.basemaps.cartocdn.com/dark_all/12/3200/1900.png",
		},
		{
			name:     "plain base url appends the slippy path",
			template: defaultUpstream,
			x:        1,
			y:        2,
			want:     "https://tile.openstreetmap.org/12/1/2.png",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := upstreamTileURL(tt.template, 12, tt.x, tt.y); got != tt.want {
				t.Errorf("upstreamTileURL = %q; want %q", got, tt.want)
			}
		})
	}
	// {s} must rotate instead of pinning one subdomain.
	seen := map[string]bool{}
	for x := 0; x < 4; x++ {
		seen[upstreamTileURL("https://{s}.basemaps.cartocdn.com/dark_all/{r}", 12, x, 0)] = true
	}
	if len(seen) != 4 {
		t.Errorf("expected 4 distinct subdomains, got %d", len(seen))
	}
}

func TestManagerAppendsAPIKeyWithoutLoggingIt(t *testing.T) {
	t.Setenv(UpstreamEnvVar, "https://basemaps.cartocdn.com/dark_all")
	t.Setenv(APIKeyEnvVar, "test-key-with spaces/+")
	t.Setenv(DarkenEnvVar, "")

	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("PNG"))
	}))
	defer server.Close()

	mgr := NewManager(t.TempDir())
	mgr.upstreamURL = server.URL
	if _, err := mgr.fetchUpstream(context.Background(), 12, 3200, 1900); err != nil {
		t.Fatalf("fetchUpstream: %v", err)
	}
	if gotQuery != "api_key=test-key-with+spaces%2F%2B" {
		t.Errorf("query = %q; want the URL-escaped api_key", gotQuery)
	}
}

func TestDarkenPNGProducesDarkHighContrastTiles(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	// Light paper background with dark "ink" and a blue water pixel.
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			src.SetNRGBA(x, y, color.NRGBA{R: 247, G: 245, B: 240, A: 255})
		}
	}
	src.SetNRGBA(0, 0, color.NRGBA{R: 20, G: 20, B: 20, A: 255})    // label ink
	src.SetNRGBA(1, 0, color.NRGBA{R: 170, G: 211, B: 223, A: 255}) // water

	var light bytes.Buffer
	if err := png.Encode(&light, src); err != nil {
		t.Fatalf("encode fixture: %v", err)
	}

	darkBytes, err := darkenPNG(light.Bytes())
	if err != nil {
		t.Fatalf("darkenPNG: %v", err)
	}
	dark, err := png.Decode(bytes.NewReader(darkBytes))
	if err != nil {
		t.Fatalf("decode darkened tile: %v", err)
	}

	mean := func(img image.Image) float64 {
		var sum float64
		b := img.Bounds()
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				r, g, bb, _ := img.At(x, y).RGBA()
				sum += (float64(r) + float64(g) + float64(bb)) / 3 / 257
			}
		}
		return sum / float64(b.Dx()*b.Dy())
	}

	if got := mean(dark); got >= darkenLumaThreshold {
		t.Errorf("darkened mean luma = %.1f; want below %d", got, darkenLumaThreshold)
	}
	// Ink must become light so labels survive on the dark surface.
	inkR, inkG, inkB, _ := dark.At(0, 0).RGBA()
	if (inkR+inkG+inkB)/3/257 < 180 {
		t.Errorf("dark ink was not lifted to a light label: %d %d %d", inkR/257, inkG/257, inkB/257)
	}
	// Water must stay blue-dominant, which is what the hue rotation preserves.
	waterR, waterG, waterB, _ := dark.At(1, 0).RGBA()
	if waterB <= waterR || waterB <= waterG {
		t.Errorf("water lost its blue hue: r=%d g=%d b=%d", waterR/257, waterG/257, waterB/257)
	}
}

func TestDarkenPNGRejectsNonPNG(t *testing.T) {
	if _, err := darkenPNG([]byte("not a png")); err == nil {
		t.Fatal("expected a decode error for non-PNG input")
	}
}

func TestRedactErrorNeverLeaksTheProviderKey(t *testing.T) {
	mgr := &Manager{apiKey: "test-key with/slash"}
	err := errors.New(`Get "https://provider.example/dark_all/12/1/2.png?api_key=test-key+with%2Fslash": dial tcp: timeout`)

	redacted := mgr.redactError(err)
	for _, secret := range []string{"test-key with/slash", "test-key+with%2Fslash"} {
		if strings.Contains(redacted, secret) {
			t.Errorf("redacted message still contains the key (%q): %s", secret, redacted)
		}
	}
	if !strings.Contains(redacted, "<redacted>") {
		t.Errorf("redacted message lost the placeholder: %s", redacted)
	}
	if !strings.Contains(redacted, "timeout") {
		t.Errorf("redacted message lost the diagnostic: %s", redacted)
	}
	if mgr.redactError(nil) != "" {
		t.Error("nil error must redact to an empty string")
	}
}
