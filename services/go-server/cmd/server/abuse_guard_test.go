package main_test

import (
	"bytes"
	"net/http"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestServer_RateLimiting_BlocksBurstAndExemptsHealth(t *testing.T) {
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
		"CORS_ALLOWED_ORIGINS=https://warded.529studio.site",
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

	// Wait for server ready (allow up to 15s for compilation under -race on CI)
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

func TestServer_RateLimiting_ForgedXForwardedForBlocked(t *testing.T) {
	port := getFreePort(t)
	absCameras, err := filepath.Abs("../../../../cameras_enriched.json")
	if err != nil {
		t.Fatalf("resolve cameras path: %v", err)
	}

	// Server configured with TRUSTED_PROXIES="10.254.0.1" (loopback 127.0.0.1 is untrusted)
	cmd := exec.Command("go", "run", ".")
	cmd.Dir = "."
	var outputBuf bytes.Buffer
	cmd.Stdout = &outputBuf
	cmd.Stderr = &outputBuf
	cmd.Env = append(cmd.Environ(),
		"PORT="+port,
		"ANALYZER=none",
		"APP_ENV=production",
		"CORS_ALLOWED_ORIGINS=https://warded.529studio.site",
		"CAMERAS_FILE_PATH="+absCameras,
		"SQLITE_PATH=:memory:",
		"RATE_LIMIT_RPS=1",
		"RATE_LIMIT_BURST=2",
		"TRUSTED_PROXIES=10.254.0.1",
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

	client := &http.Client{Timeout: 2 * time.Second}

	// Request 1: Spoofed X-Forwarded-For: 203.0.113.1 -> 200 OK (Burst 1)
	req1, _ := http.NewRequest(http.MethodGet, baseURL+"/api/cameras", nil)
	req1.Header.Set("X-Forwarded-For", "203.0.113.1")
	resp1, err := client.Do(req1)
	if err != nil {
		t.Fatalf("req 1 failed: %v", err)
	}
	_ = resp1.Body.Close()
	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("req 1 expected 200, got %d", resp1.StatusCode)
	}

	// Request 2: Spoofed X-Forwarded-For: 203.0.113.2 -> 200 OK (Burst 2)
	req2, _ := http.NewRequest(http.MethodGet, baseURL+"/api/cameras", nil)
	req2.Header.Set("X-Forwarded-For", "203.0.113.2")
	resp2, err := client.Do(req2)
	if err != nil {
		t.Fatalf("req 2 failed: %v", err)
	}
	_ = resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("req 2 expected 200, got %d", resp2.StatusCode)
	}

	// Request 3: Spoofed X-Forwarded-For: 203.0.113.3 -> Must be BLOCKED with 429 Too Many Requests
	// Because 127.0.0.1 is not trusted, Gin ignores X-Forwarded-For and limits by actual remote addr!
	req3, _ := http.NewRequest(http.MethodGet, baseURL+"/api/cameras", nil)
	req3.Header.Set("X-Forwarded-For", "203.0.113.3")
	resp3, err := client.Do(req3)
	if err != nil {
		t.Fatalf("req 3 failed: %v", err)
	}
	_ = resp3.Body.Close()
	if resp3.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("req 3 expected 429 Too Many Requests due to forged IP rejection, got %d", resp3.StatusCode)
	}
}
