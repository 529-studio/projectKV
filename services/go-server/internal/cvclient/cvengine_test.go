package cvclient

import (
	"bytes"
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
		if r.Header.Get("User-Agent") != "Mozilla/5.0 (compatible; CameraViewProxy/1.0)" {
			http.Error(w, "forbidden: missing or invalid user-agent", http.StatusForbidden)
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
	expectedFrame := []byte("\xff\xd8\xff\xe0minimal-jpeg")
	if !bytes.Equal(v.RawFrame, expectedFrame) {
		t.Errorf("expected RawFrame %q, got %q", expectedFrame, v.RawFrame)
	}
}

func TestCVEngineAnalyzer_Analyze_PCUCalculation(t *testing.T) {
	// 50 motorcycles * 0.2 = 10.0 PCU. With default floor 25.0, SI = 0.40 -> MODERATE.
	// Even if server sends raw traffic_severity="CLEAR", Go PCU business logic re-maps to MODERATE.
	predictBody := `{"traffic_severity":"CLEAR","confidence":0.92,"vehicles_detected":50,"vehicle_counts":{"motorcycle":50,"car":0,"bus":0,"truck":0,"container":0}}`
	srv, analyzer := makeCVEngineTestServer(t, http.StatusOK, http.StatusOK, predictBody)

	cam := hotset.Camera{
		ID:          "cam-pcu-test",
		SnapshotURL: srv.URL + "/snapshot/cam-pcu-test.jpg",
	}

	v, err := analyzer.Analyze(context.Background(), cam)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if v.Severity != hotset.SeverityModerate {
		t.Errorf("expected PCU re-mapped severity MODERATE, got %q", v.Severity)
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

func TestCVEngineAnalyzer_Analyze_SetsUserAgentHeader(t *testing.T) {
	var capturedUA string
	mux := http.NewServeMux()
	mux.HandleFunc("/snapshot/", func(w http.ResponseWriter, r *http.Request) {
		capturedUA = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("\xff\xd8\xff\xe0minimal-jpeg"))
	})
	mux.HandleFunc("/predict", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"traffic_severity":"CLEAR","confidence":0.9,"latency_ms":10}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	analyzer := NewCVEngineAnalyzer(srv.URL, srv.Client())
	cam := hotset.Camera{
		ID:          "cam-ua-test",
		SnapshotURL: srv.URL + "/snapshot/cam-ua-test.jpg",
	}

	_, err := analyzer.Analyze(context.Background(), cam)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	const expectedUA = "Mozilla/5.0 (compatible; CameraViewProxy/1.0)"
	if capturedUA != expectedUA {
		t.Errorf("expected User-Agent %q on snapshot fetch, got %q", expectedUA, capturedUA)
	}
}
