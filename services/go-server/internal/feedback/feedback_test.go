package feedback

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

type mockSnapshotFetcher struct {
	data []byte
	err  error
}

func (m *mockSnapshotFetcher) FetchSnapshot(ctx context.Context, cameraID string) ([]byte, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.data, nil
}

type mockCameraChecker struct {
	existingIDs map[string]bool
}

func (m *mockCameraChecker) CameraExists(cameraID string) bool {
	if m.existingIDs == nil {
		return true
	}
	return m.existingIDs[cameraID]
}

type mockStorageUploader struct {
	mu    sync.Mutex
	files map[string][]byte
}

func newMockStorageUploader() *mockStorageUploader {
	return &mockStorageUploader{
		files: make(map[string][]byte),
	}
}

func (m *mockStorageUploader) Put(ctx context.Context, path string, data io.Reader) error {
	b, err := io.ReadAll(data)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[path] = b
	return nil
}

func (m *mockStorageUploader) GetFile(path string) ([]byte, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.files[path]
	return b, ok
}

func TestArchiver_ArchiveWithSnapshotAndStorage(t *testing.T) {
	fakeJPEG := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x01, 0x02}
	fetcher := &mockSnapshotFetcher{data: fakeJPEG}
	storage := newMockStorageUploader()
	loc := time.FixedZone("ICT", 7*3600)
	archiver := NewArchiver(fetcher, storage, loc)

	now := time.Date(2026, 10, 17, 14, 30, 0, 0, loc)
	camID := "cam_flood_01"
	clientIP := "203.113.152.1"
	userAgent := "Mozilla/5.0 TestBrowser"

	err := archiver.Archive(context.Background(), camID, clientIP, userAgent, now)
	if err != nil {
		t.Fatalf("expected Archive to succeed, got error: %v", err)
	}

	imagePath, metaPath := archiver.BuildStoragePaths(camID, now)
	expectedImagePath := fmt.Sprintf("feedback/2026-10-17/%s_%d.jpg", camID, now.Unix())
	expectedMetaPath := fmt.Sprintf("feedback/2026-10-17/%s_%d.json", camID, now.Unix())

	if imagePath != expectedImagePath {
		t.Errorf("expected imagePath %s, got %s", expectedImagePath, imagePath)
	}
	if metaPath != expectedMetaPath {
		t.Errorf("expected metaPath %s, got %s", expectedMetaPath, metaPath)
	}

	// Verify JPEG content
	storedJPEG, ok := storage.GetFile(imagePath)
	if !ok {
		t.Fatalf("snapshot JPEG not found at %s", imagePath)
	}
	if !bytes.Equal(storedJPEG, fakeJPEG) {
		t.Errorf("stored JPEG mismatch")
	}

	// Verify JSON manifest
	storedMetaBytes, ok := storage.GetFile(metaPath)
	if !ok {
		t.Fatalf("metadata manifest not found at %s", metaPath)
	}

	var meta ReportMetadata
	if err := json.Unmarshal(storedMetaBytes, &meta); err != nil {
		t.Fatalf("failed to parse metadata manifest: %v", err)
	}

	if meta.CameraID != camID {
		t.Errorf("expected camera_id %s, got %s", camID, meta.CameraID)
	}
	if meta.Source != "user_reported" {
		t.Errorf("expected source user_reported, got %s", meta.Source)
	}
	if meta.Trust != "unverified" {
		t.Errorf("expected trust unverified, got %s", meta.Trust)
	}
	if !meta.HasSnapshot {
		t.Errorf("expected has_snapshot to be true")
	}
	expectedIPHash := HashClientIP(clientIP)
	if meta.ClientIPHash != expectedIPHash {
		t.Errorf("expected client_ip_hash %s, got %s", expectedIPHash, meta.ClientIPHash)
	}
	if meta.UserAgent != userAgent {
		t.Errorf("expected user_agent %s, got %s", userAgent, meta.UserAgent)
	}
}

func TestArchiver_ArchiveWithoutSnapshot(t *testing.T) {
	fetcher := &mockSnapshotFetcher{err: fmt.Errorf("camera offline")}
	storage := newMockStorageUploader()
	loc := time.FixedZone("ICT", 7*3600)
	archiver := NewArchiver(fetcher, storage, loc)

	now := time.Date(2026, 10, 17, 15, 0, 0, 0, loc)
	camID := "cam_offline_02"
	clientIP := "10.0.0.5"

	err := archiver.Archive(context.Background(), camID, clientIP, "Bot/1.0", now)
	if err != nil {
		t.Fatalf("Archive should succeed even when snapshot fetch fails, got: %v", err)
	}

	imagePath, metaPath := archiver.BuildStoragePaths(camID, now)
	if _, ok := storage.GetFile(imagePath); ok {
		t.Errorf("image should not be stored when snapshot fetch failed")
	}

	storedMetaBytes, ok := storage.GetFile(metaPath)
	if !ok {
		t.Fatalf("metadata manifest should still be stored")
	}

	var meta ReportMetadata
	if err := json.Unmarshal(storedMetaBytes, &meta); err != nil {
		t.Fatalf("failed to parse metadata manifest: %v", err)
	}

	if meta.HasSnapshot {
		t.Errorf("expected has_snapshot to be false")
	}
}

func TestHandler_ReportFlood_Success(t *testing.T) {
	checker := &mockCameraChecker{
		existingIDs: map[string]bool{"cam_123": true},
	}
	limiter := NewRateLimiter(5 * time.Minute)
	storage := newMockStorageUploader()
	archiver := NewArchiver(nil, storage, nil)
	handler := NewHandler(checker, limiter, archiver)

	router := gin.New()
	handler.RegisterRoutes(router.Group("/api"))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/cameras/cam_123/report-flood", nil)
	req.RemoteAddr = "192.168.1.50:12345"
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp ReportResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Status != "success" {
		t.Errorf("expected status success, got %s", resp.Status)
	}
	if resp.CameraID != "cam_123" {
		t.Errorf("expected camera_id cam_123, got %s", resp.CameraID)
	}
}

func TestHandler_ReportFlood_RateLimited(t *testing.T) {
	checker := &mockCameraChecker{
		existingIDs: map[string]bool{"cam_123": true},
	}
	limiter := NewRateLimiter(5 * time.Minute)
	handler := NewHandler(checker, limiter, nil)

	router := gin.New()
	handler.RegisterRoutes(router.Group("/api"))

	// First request succeeds
	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/cameras/cam_123/report-flood", nil)
	req1.RemoteAddr = "10.0.0.1:4567"
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("expected first request to return 200, got %d", w1.Code)
	}

	// Immediate repeat request fails with 429
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/cameras/cam_123/report-flood", nil)
	req2.RemoteAddr = "10.0.0.1:4567"
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)

	if w2.Code != http.StatusTooManyRequests {
		t.Fatalf("expected repeat request to return 429, got %d: %s", w2.Code, w2.Body.String())
	}

	retryHeader := w2.Header().Get("Retry-After")
	if retryHeader == "" {
		t.Errorf("expected Retry-After header on 429 response")
	}

	var errResp ErrorResponse
	if err := json.Unmarshal(w2.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if errResp.Error != "rate_limited" {
		t.Errorf("expected error rate_limited, got %s", errResp.Error)
	}
	if errResp.RetryAfterSeconds <= 0 {
		t.Errorf("expected retry_after_seconds > 0, got %d", errResp.RetryAfterSeconds)
	}
}

func TestHandler_ReportFlood_CameraNotFound(t *testing.T) {
	checker := &mockCameraChecker{
		existingIDs: map[string]bool{"cam_known": true},
	}
	handler := NewHandler(checker, nil, nil)

	router := gin.New()
	handler.RegisterRoutes(router.Group("/api"))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/cameras/cam_nonexistent/report-flood", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d: %s", w.Code, w.Body.String())
	}

	var errResp ErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if errResp.Error != "camera_not_found" {
		t.Errorf("expected error camera_not_found, got %s", errResp.Error)
	}
}

func TestHandler_ReportFlood_LegacyAliasRoute(t *testing.T) {
	checker := &mockCameraChecker{
		existingIDs: map[string]bool{"cam_alias": true},
	}
	handler := NewHandler(checker, nil, nil)

	router := gin.New()
	handler.RegisterRoutes(router.Group("/api"))

	// Test legacy alias without /v1 prefix
	req := httptest.NewRequest(http.MethodPost, "/api/cameras/cam_alias/report-flood", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected legacy alias route to return 200, got %d", w.Code)
	}
}
