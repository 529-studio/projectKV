package tilecache

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

	// Verify file saved to disk
	expectedFile := filepath.Join(tmpDir, "12", "3200", "1900.png")
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
