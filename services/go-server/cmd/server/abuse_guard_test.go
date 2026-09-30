package main_test

import (
	"net/http"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestServer_RateLimiting_BlocksBurstAndExemptsHealth(t *testing.T) {
	port := "38922"
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
		"RATE_LIMIT_RPS=1",
		"RATE_LIMIT_BURST=2",
	)

	if err := cmd.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
	}()

	baseURL := "http://localhost:" + port

	// Wait for server ready
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

	client := &http.Client{Timeout: 2 * time.Second}

	// Request 1: Burst 1 -> 200 OK
	resp1, err := client.Get(baseURL + "/api/cameras")
	if err != nil {
		t.Fatalf("req 1 failed: %v", err)
	}
	_ = resp1.Body.Close()
	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("req 1 expected 200, got %d", resp1.StatusCode)
	}

	// Request 2: Burst 2 -> 200 OK
	resp2, err := client.Get(baseURL + "/api/cameras")
	if err != nil {
		t.Fatalf("req 2 failed: %v", err)
	}
	_ = resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("req 2 expected 200, got %d", resp2.StatusCode)
	}

	// Request 3: Exceeds burst -> 429 Too Many Requests
	resp3, err := client.Get(baseURL + "/api/cameras")
	if err != nil {
		t.Fatalf("req 3 failed: %v", err)
	}
	_ = resp3.Body.Close()
	if resp3.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("req 3 expected 429 Too Many Requests, got %d", resp3.StatusCode)
	}
	if retryAfter := resp3.Header.Get("Retry-After"); retryAfter != "1" {
		t.Errorf("req 3 expected Retry-After: 1, got %q", retryAfter)
	}

	// Verify /health is EXEMPT even when rate limit is currently tripped
	respHealth, err := client.Get(baseURL + "/health")
	if err != nil {
		t.Fatalf("req health failed: %v", err)
	}
	_ = respHealth.Body.Close()
	if respHealth.StatusCode != http.StatusOK {
		t.Fatalf("/health expected 200 OK despite rate limit, got %d", respHealth.StatusCode)
	}
}
