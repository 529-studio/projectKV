package main_test

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func getFreePort(t *testing.T) string {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("get free port: %v", err)
	}
	defer l.Close()
	return fmt.Sprintf("%d", l.Addr().(*net.TCPAddr).Port)
}

func TestStartup_RefusesMockOutsideDev(t *testing.T) {
	cmd := exec.Command("go", "run", ".")
	cmd.Dir = "."
	absCameras, err := filepath.Abs("../../../../cameras_enriched.json")
	if err != nil {
		t.Fatalf("resolve cameras path: %v", err)
	}

	cmd.Env = append(cmd.Environ(),
		"ANALYZER=mock",
		"APP_ENV=production",
		"CAMERAS_FILE_PATH="+absCameras,
	)

	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected server startup to fail when ANALYZER=mock and APP_ENV=production, but it succeeded: %s", string(out))
	}

	outStr := string(out)
	if !strings.Contains(outStr, "strictly forbidden") && !strings.Contains(outStr, "Refusing to start") {
		t.Errorf("expected fatal error message about mock in non-dev, got output:\n%s", outStr)
	}
}

func TestStartup_AnalyzerNone_EmptyVerdictsAndNoSSE(t *testing.T) {
	port := "38921"
	absCameras, err := filepath.Abs("../../../../cameras_enriched.json")
	if err != nil {
		t.Fatalf("resolve cameras path: %v", err)
	}

	cmd := exec.Command("go", "run", ".")
	cmd.Dir = "."
	var outputBuf bytes.Buffer
	cmd.Stdout = &outputBuf
	cmd.Stderr = &outputBuf
	cmd.Env = append(cmd.Environ(),
		"PORT="+port,
		"ANALYZER=none",
		"APP_ENV=production",
		"CAMERAS_FILE_PATH="+absCameras,
		"SQLITE_PATH=:memory:",
	)

	if err := cmd.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
	}()

	baseURL := "http://localhost:" + port

	// Wait for server to be ready (allow up to 15s for compilation under -race on CI)
	ready := false
	for i := 0; i < 150; i++ {
		time.Sleep(100 * time.Millisecond)
		resp, err := http.Get(baseURL + "/health")
		if err == nil && resp.StatusCode == http.StatusOK {
			_ = resp.Body.Close()
			ready = true
			break
		}
	}
	if !ready {
		t.Fatalf("server failed to become ready on %s; output:\n%s", baseURL, outputBuf.String())
	}

	// 1. Verify /api/traffic-status
	resp, err := http.Get(baseURL + "/api/traffic-status")
	if err != nil {
		t.Fatalf("GET /api/traffic-status: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var statusResp struct {
		AnalysisEnabled bool          `json:"analysis_enabled"`
		Verdicts        []interface{} `json:"verdicts"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&statusResp); err != nil {
		t.Fatalf("decode traffic-status: %v", err)
	}

	if statusResp.AnalysisEnabled {
		t.Errorf("expected analysis_enabled false when ANALYZER=none")
	}
	if len(statusResp.Verdicts) != 0 {
		t.Errorf("expected 0 verdicts when ANALYZER=none, got %d", len(statusResp.Verdicts))
	}

	// 2. Connect to /api/live/stream SSE endpoint and verify no camera_update is received
	sseResp, err := http.Get(baseURL + "/api/live/stream")
	if err != nil {
		t.Fatalf("connect SSE /api/live/stream: %v", err)
	}
	defer sseResp.Body.Close()

	// Wait a moment listening to SSE stream
	eventReceived := make(chan string, 1)
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := sseResp.Body.Read(buf)
			if n > 0 {
				data := string(buf[:n])
				if strings.Contains(data, "event: camera_update") {
					eventReceived <- data
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	select {
	case event := <-eventReceived:
		t.Fatalf("received unexpected camera_update SSE event when ANALYZER=none: %s", event)
	case <-time.After(1 * time.Second):
		// No event received within timeout, as expected
	}
}

func TestStartup_HotsetStatusEndpoint_ServesCachedState(t *testing.T) {
	port := getFreePort(t)
	absCameras, err := filepath.Abs("../../../../cameras_enriched.json")
	if err != nil {
		t.Fatalf("resolve cameras path: %v", err)
	}

	cmd := exec.Command("go", "run", ".")
	cmd.Dir = "."
	serverBuf := &bytes.Buffer{}
	cmd.Stdout = serverBuf
	cmd.Stderr = serverBuf
	cmd.Env = append(cmd.Environ(),
		"PORT="+port,
		"ANALYZER=mock",
		"APP_ENV=dev",
		"CAMERAS_FILE_PATH="+absCameras,
		"SQLITE_PATH=:memory:",
		"HOTSET_FIXED_MAX_CAMERAS=10",
		"HOTSET_INTERVAL_SECONDS=1",
	)

	if err := cmd.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
	}()

	baseURL := "http://localhost:" + port

	// Wait for server readiness (allow up to 15s for compilation under -race on CI)
	ready := false
	for i := 0; i < 150; i++ {
		time.Sleep(100 * time.Millisecond)
		resp, err := http.Get(baseURL + "/health")
		if err == nil && resp.StatusCode == http.StatusOK {
			_ = resp.Body.Close()
			ready = true
			break
		}
	}
	if !ready {
		t.Fatalf("server failed to become ready on %s: %s", baseURL, serverBuf.String())
	}

	// Give the mock scheduler a moment to run the initial cycle and populate hotset.Store
	time.Sleep(500 * time.Millisecond)

	endpoints := []string{"/api/v1/cameras/hotset/status", "/api/cameras/hotset/status"}
	for _, ep := range endpoints {
		start := time.Now()
		resp, err := http.Get(baseURL + ep)
		latency := time.Since(start)

		if err != nil {
			t.Fatalf("GET %s failed: %v", ep, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("%s: expected 200, got %d (body: %s)\nServer output:\n%s", ep, resp.StatusCode, string(body), serverBuf.String())
		}

		if latency >= 50*time.Millisecond {
			t.Errorf("%s: cold request latency too high: %v", ep, latency)
		}

		var statusResp struct {
			Total     int    `json:"total"`
			UpdatedAt string `json:"updated_at"`
			Cameras   []struct {
				CameraID        string  `json:"camera_id"`
				Severity        string  `json:"severity"`
				SaturationIndex float64 `json:"saturation_index"`
				PCUTotal        float64 `json:"pcu_total"`
				Source          string  `json:"source"`
			} `json:"cameras"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&statusResp); err != nil {
			t.Fatalf("%s: decode response: %v", ep, err)
		}

		if statusResp.Total == 0 || len(statusResp.Cameras) == 0 {
			t.Errorf("%s: expected populated cameras in hotset status, got %d", ep, statusResp.Total)
		}

		t.Logf("%s: successfully returned %d cameras in %v", ep, statusResp.Total, latency)
	}
}

func TestStartup_CatalogGzipCompressionAndCachingHeaders(t *testing.T) {
	port := getFreePort(t)
	absCameras, err := filepath.Abs("../../../../cameras_enriched.json")
	if err != nil {
		t.Fatalf("resolve cameras path: %v", err)
	}

	cmd := exec.Command("go", "run", ".")
	cmd.Dir = "."
	var outputBuf bytes.Buffer
	cmd.Stdout = &outputBuf
	cmd.Stderr = &outputBuf
	cmd.Env = append(cmd.Environ(),
		"PORT="+port,
		"ANALYZER=none",
		"APP_ENV=production",
		"CAMERAS_FILE_PATH="+absCameras,
		"SQLITE_PATH=:memory:",
	)

	if err := cmd.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
	}()

	baseURL := "http://localhost:" + port

	// Wait for server to become ready
	ready := false
	for range 150 {
		time.Sleep(100 * time.Millisecond)
		resp, err := http.Get(baseURL + "/health")
		if err == nil && resp.StatusCode == http.StatusOK {
			_ = resp.Body.Close()
			ready = true
			break
		}
	}
	if !ready {
		t.Fatalf("server failed to become ready on %s; output:\n%s", baseURL, outputBuf.String())
	}

	client := &http.Client{Timeout: 10 * time.Second}

	// 1. Test /api/cameras with Accept-Encoding: gzip
	req, err := http.NewRequest(http.MethodGet, baseURL+"/api/cameras", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Accept-Encoding", "gzip")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET /api/cameras: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Check Cache-Control header
	cc := resp.Header.Get("Cache-Control")
	expectedCC := "public, max-age=300, stale-while-revalidate=600"
	if cc != expectedCC {
		t.Errorf("expected Cache-Control %q, got %q", expectedCC, cc)
	}

	// Check ETag header
	etag := resp.Header.Get("ETag")
	if etag == "" {
		t.Fatalf("expected non-empty ETag header")
	}

	// Check Content-Encoding: gzip
	ce := resp.Header.Get("Content-Encoding")
	if ce != "gzip" {
		t.Fatalf("expected Content-Encoding: gzip, got %q", ce)
	}

	// Read compressed body and verify wire size
	compressedBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}

	wireSize := len(compressedBytes)
	t.Logf("/api/cameras headers: %+v", resp.Header)
	t.Logf("/api/cameras gzipped wire size: %d bytes (%.1f KB), last 10 bytes: %x", wireSize, float64(wireSize)/1024, compressedBytes[wireSize-10:])
	if wireSize > 120*1024 {
		t.Errorf("expected gzipped wire size to be under 120 KB, got %d bytes", wireSize)
	}

	// Decompress and verify content
	gzReader, err := gzip.NewReader(bytes.NewReader(compressedBytes))
	if err != nil {
		t.Fatalf("new gzip reader: %v", err)
	}
	defer gzReader.Close()

	uncompressedBytes, err := io.ReadAll(gzReader)
	t.Logf("gzReader read: %d uncompressed bytes, err: %v", len(uncompressedBytes), err)
	if err != nil {
		t.Fatalf("decompress gzip body: %v", err)
	}

	t.Logf("/api/cameras uncompressed size: %d bytes (%.1f KB)", len(uncompressedBytes), float64(len(uncompressedBytes))/1024)
	if len(uncompressedBytes) < 300*1024 {
		t.Errorf("expected uncompressed catalog size > 300 KB, got %d", len(uncompressedBytes))
	}

	var cameras []map[string]interface{}
	if err := json.Unmarshal(uncompressedBytes, &cameras); err != nil {
		t.Fatalf("unmarshal decompressed JSON: %v", err)
	}
	if len(cameras) == 0 {
		t.Fatalf("expected cameras list to not be empty")
	}

	// 2. Test conditional request with If-None-Match
	reqCond, err := http.NewRequest(http.MethodGet, baseURL+"/api/cameras", nil)
	if err != nil {
		t.Fatalf("new conditional request: %v", err)
	}
	reqCond.Header.Set("If-None-Match", etag)
	reqCond.Header.Set("Accept-Encoding", "gzip")

	respCond, err := client.Do(reqCond)
	if err != nil {
		t.Fatalf("GET /api/cameras conditional: %v", err)
	}
	defer respCond.Body.Close()

	if respCond.StatusCode != http.StatusNotModified {
		t.Fatalf("expected 304 Not Modified, got %d", respCond.StatusCode)
	}

	// 3. Test /api/zones caching and conditional request
	reqZones, err := http.NewRequest(http.MethodGet, baseURL+"/api/zones", nil)
	if err != nil {
		t.Fatalf("new zones request: %v", err)
	}
	respZones, err := client.Do(reqZones)
	if err != nil {
		t.Fatalf("GET /api/zones: %v", err)
	}
	defer respZones.Body.Close()

	if respZones.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", respZones.StatusCode)
	}
	if respZones.Header.Get("Cache-Control") != expectedCC {
		t.Errorf("expected zones Cache-Control %q, got %q", expectedCC, respZones.Header.Get("Cache-Control"))
	}
	zonesETag := respZones.Header.Get("ETag")
	if zonesETag == "" {
		t.Fatalf("expected non-empty ETag for zones")
	}

	reqZonesCond, err := http.NewRequest(http.MethodGet, baseURL+"/api/zones", nil)
	if err != nil {
		t.Fatalf("new zones cond request: %v", err)
	}
	reqZonesCond.Header.Set("If-None-Match", zonesETag)
	respZonesCond, err := client.Do(reqZonesCond)
	if err != nil {
		t.Fatalf("GET /api/zones cond: %v", err)
	}
	defer respZonesCond.Body.Close()

	if respZonesCond.StatusCode != http.StatusNotModified {
		t.Fatalf("expected 304 Not Modified for zones, got %d", respZonesCond.StatusCode)
	}
}

func TestStartup_StaticWebServing_NextJSExportAndPurgedVanillaJS(t *testing.T) {
	port := getFreePort(t)
	absCameras, err := filepath.Abs("../../../../cameras_enriched.json")
	if err != nil {
		t.Fatalf("resolve cameras path: %v", err)
	}

	tmpStaticDir := t.TempDir()
	indexContent := "<html><body>NextJS App Root</body></html>"
	if err := os.WriteFile(filepath.Join(tmpStaticDir, "index.html"), []byte(indexContent), 0644); err != nil {
		t.Fatalf("write index.html: %v", err)
	}
	nextDir := filepath.Join(tmpStaticDir, "_next")
	if err := os.MkdirAll(nextDir, 0755); err != nil {
		t.Fatalf("mkdir _next: %v", err)
	}
	chunkContent := "console.log('next-chunk');"
	if err := os.WriteFile(filepath.Join(nextDir, "chunk.js"), []byte(chunkContent), 0644); err != nil {
		t.Fatalf("write chunk.js: %v", err)
	}

	cmd := exec.Command("go", "run", ".")
	cmd.Dir = "."
	var outputBuf bytes.Buffer
	cmd.Stdout = &outputBuf
	cmd.Stderr = &outputBuf
	cmd.Env = append(cmd.Environ(),
		"PORT="+port,
		"ANALYZER=none",
		"APP_ENV=production",
		"CAMERAS_FILE_PATH="+absCameras,
		"SQLITE_PATH=:memory:",
		"STATIC_WEB_DIR="+tmpStaticDir,
	)

	if err := cmd.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
	}()

	baseURL := "http://localhost:" + port
	ready := false
	for range 150 {
		time.Sleep(100 * time.Millisecond)
		resp, err := http.Get(baseURL + "/health")
		if err == nil && resp.StatusCode == http.StatusOK {
			_ = resp.Body.Close()
			ready = true
			break
		}
	}
	if !ready {
		t.Fatalf("server failed to become ready on %s; output:\n%s", baseURL, outputBuf.String())
	}

	client := &http.Client{Timeout: 5 * time.Second}

	// 1. Root / serves index.html
	respRoot, err := client.Get(baseURL + "/")
	if err != nil {
		t.Fatalf("GET / failed: %v", err)
	}
	defer respRoot.Body.Close()
	rootBody, _ := io.ReadAll(respRoot.Body)
	if respRoot.StatusCode != http.StatusOK || string(rootBody) != indexContent {
		t.Errorf("GET / expected 200 with index.html, got %d: %s", respRoot.StatusCode, string(rootBody))
	}

	// 2. Next.js static asset /_next/chunk.js is served
	respNext, err := client.Get(baseURL + "/_next/chunk.js")
	if err != nil {
		t.Fatalf("GET /_next/chunk.js failed: %v", err)
	}
	defer respNext.Body.Close()
	nextBody, _ := io.ReadAll(respNext.Body)
	if respNext.StatusCode != http.StatusOK || string(nextBody) != chunkContent {
		t.Errorf("GET /_next/chunk.js expected 200 with chunk.js, got %d: %s", respNext.StatusCode, string(nextBody))
	}

	// 3. Obsolete vanilla JS routes /styles.css and /app.js are not static files:
	// when absent from disk, SPA fallback returns index.html rather than looking for a missing static file
	respCSS, err := client.Get(baseURL + "/styles.css")
	if err != nil {
		t.Fatalf("GET /styles.css failed: %v", err)
	}
	defer respCSS.Body.Close()
	cssBody, _ := io.ReadAll(respCSS.Body)
	if string(cssBody) != indexContent {
		t.Errorf("expected SPA fallback to index.html for unmapped /styles.css, got: %s", string(cssBody))
	}
}
