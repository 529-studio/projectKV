package cvclient

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/529-studio/projectKV/services/go-server/internal/hotset"
)

// newCVEngineTestClient builds a CVEngineAnalyzer pointing at a test HTTP
// server that serves the snapshot and the predict endpoint on the same host.
// snapshotStatus / predictStatus control the response codes; bodies are JSON
// strings for the predict path and raw bytes for the snapshot.
func makeCVEngineTestServer(t *testing.T, snapshotStatus int, predictStatus int, predictBody string) (*httptest.Server, *CVEngineAnalyzer) {
	t.Helper()
	mux := http.NewServeMux()

	// Snapshot endpoint: returns minimal JPEG-like bytes.
	mux.HandleFunc("/snapshot/", func(w http.ResponseWriter, r *http.Request) {
		if snapshotStatus != http.StatusOK {
			http.Error(w, "not found", snapshotStatus)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("\xff\xd8\xff\xe0minimal-jpeg"))
	})

	// Predict endpoint.
	mux.HandleFunc("/predict", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(predictStatus)
		_, _ = io.WriteString(w, predictBody)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	cam := hotset.Camera{
		ID:          "cam-onnx-1",
		SnapshotURL: srv.URL + "/snapshot/cam-onnx-1.jpg",
	}
	_ = cam // used in test bodies directly

	analyzer := NewCVEngineAnalyzer(srv.URL, srv.Client())
	return srv, analyzer
}

func TestCVEngineAnalyzer_Analyze_Success(t *testing.T) {
	predictBody := `{"traffic_severity":"MODERATE","confidence":0.88,"vehicles_detected":12,"latency_ms":44.1,"model":"yolov8n_openvino_320"}`
	srv, analyzer := makeCVEngineTestServer(t, http.StatusOK, http.StatusOK, predictBody)

	cam := hotset.Camera{
		ID:          "cam-onnx-1",
		SnapshotURL: srv.URL + "/snapshot/cam-onnx-1.jpg",
	}

	v, err := analyzer.Analyze(context.Background(), cam)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if v.Source != "onnx" {
		t.Errorf("expected source 'onnx', got %q", v.Source)
	}
	if v.Severity != hotset.SeverityModerate {
		t.Errorf("expected severity MODERATE, got %q", v.Severity)
	}
	if v.Confidence != 0.88 {
		t.Errorf("expected confidence 0.88, got %f", v.Confidence)
	}
	if v.CameraID != cam.ID {
		t.Errorf("expected camera_id %q, got %q", cam.ID, v.CameraID)
	}
	if v.Timestamp.IsZero() {
		t.Error("expected non-zero timestamp")
	}
}

func TestCVEngineAnalyzer_Analyze_StandbyRejected(t *testing.T) {
	predictBody := `{"source":"standby_dummy","confidence":0.0,"mode":"standby_ready"}`
	srv, analyzer := makeCVEngineTestServer(t, http.StatusOK, http.StatusServiceUnavailable, predictBody)

	cam := hotset.Camera{
		ID:          "cam-onnx-2",
		SnapshotURL: srv.URL + "/snapshot/cam-onnx-2.jpg",
	}

	v, err := analyzer.Analyze(context.Background(), cam)
	if err == nil {
		t.Fatal("expected error for standby 503 response, got nil")
	}
	if v.Severity != hotset.SeverityUnknown {
		t.Errorf("expected severity UNKNOWN on standby, got %q", v.Severity)
	}
	if !strings.Contains(err.Error(), "standby") {
		t.Errorf("expected error to mention 'standby', got: %v", err)
	}
}

func TestCVEngineAnalyzer_Analyze_SnapshotFetchFails(t *testing.T) {
	predictBody := `{"traffic_severity":"CLEAR","confidence":0.9,"vehicles_detected":0}`
	srv, analyzer := makeCVEngineTestServer(t, http.StatusNotFound, http.StatusOK, predictBody)

	cam := hotset.Camera{
		ID:          "cam-onnx-3",
		SnapshotURL: srv.URL + "/snapshot/cam-onnx-3.jpg",
	}

	v, err := analyzer.Analyze(context.Background(), cam)
	if err == nil {
		t.Fatal("expected error when snapshot returns 404, got nil")
	}
	if v.Severity != hotset.SeverityUnknown {
		t.Errorf("expected severity UNKNOWN on snapshot failure, got %q", v.Severity)
	}
}

func TestCVEngineAnalyzer_Analyze_CVEngineNon200(t *testing.T) {
	predictBody := `{"error":"inference failed"}`
	srv, analyzer := makeCVEngineTestServer(t, http.StatusOK, http.StatusInternalServerError, predictBody)

	cam := hotset.Camera{
		ID:          "cam-onnx-4",
		SnapshotURL: srv.URL + "/snapshot/cam-onnx-4.jpg",
	}

	v, err := analyzer.Analyze(context.Background(), cam)
	if err == nil {
		t.Fatal("expected error when cv-engine returns 500, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected error to mention '500', got: %v", err)
	}
	if v.Severity != hotset.SeverityUnknown {
		t.Errorf("expected severity UNKNOWN on engine error, got %q", v.Severity)
	}
}

func TestCVEngineAnalyzer_Analyze_ContextCancelled(t *testing.T) {
	// Use a slow server that blocks longer than the cancelled context allows.
	blocked := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-blocked // never unblocks in this test
	}))
	t.Cleanup(func() {
		close(blocked)
		srv.Close()
	})

	analyzer := NewCVEngineAnalyzer(srv.URL, srv.Client())
	cam := hotset.Camera{
		ID:          "cam-onnx-5",
		SnapshotURL: srv.URL + "/snapshot/cam-onnx-5.jpg",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	v, err := analyzer.Analyze(ctx, cam)
	if err == nil {
		t.Fatal("expected error on context cancellation, got nil")
	}
	if v.Severity != hotset.SeverityUnknown {
		t.Errorf("expected severity UNKNOWN on timeout, got %q", v.Severity)
	}
}
