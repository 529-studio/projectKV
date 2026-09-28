package main_test

import (
	"encoding/json"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStartup_RefusesMockOutsideDev(t *testing.T) {
	cmd := exec.Command("go", "run", "./main.go")
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

	cmd := exec.Command("go", "run", "./main.go")
	cmd.Dir = "."
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

	// Wait for server to be ready
	ready := false
	for i := 0; i < 40; i++ {
		time.Sleep(100 * time.Millisecond)
		resp, err := http.Get(baseURL + "/health")
		if err == nil && resp.StatusCode == http.StatusOK {
			_ = resp.Body.Close()
			ready = true
			break
		}
	}
	if !ready {
		t.Fatalf("server failed to become ready on %s", baseURL)
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
