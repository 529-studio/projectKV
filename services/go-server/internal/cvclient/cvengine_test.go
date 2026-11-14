package cvclient

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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
	predictBody := `{"traffic_severity":"MODERATE","confidence":0.88,"vehicles_detected":6,"latency_ms":44.1,"model":"yolov8n_openvino_320"}`
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
	// 20 motorcycles * 0.3 = 6.0 PCU. With default floor 15.0, SI = 6.0 / 15.0 = 0.40 -> MODERATE.
	// Even if server sends raw traffic_severity="CLEAR", Go PCU business logic re-maps to MODERATE.
	predictBody := `{"traffic_severity":"CLEAR","confidence":0.92,"vehicles_detected":20,"vehicle_counts":{"motorcycle":20,"car":0,"bus":0,"truck":0,"container":0}}`
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

// newFlakyPredictServer serves a normal snapshot and, on /predict, closes the
// connection for the first failCount requests to simulate the inference worker
// being replaced mid-deploy. Requests after that succeed.
func newFlakyPredictServer(t *testing.T, failCount int32) (*httptest.Server, *int32) {
	t.Helper()
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/snapshot/") {
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("\xff\xd8\xff\xe0minimal-jpeg"))
			return
		}
		if atomic.AddInt32(&attempts, 1) <= failCount {
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Errorf("response writer does not support hijacking")
				return
			}
			conn, _, err := hj.Hijack()
			if err != nil {
				t.Errorf("hijack failed: %v", err)
				return
			}
			_ = conn.Close()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"traffic_severity":"LOW","confidence":0.72,"vehicles_detected":3,"latency_ms":5,"model":"test"}`)
	}))
	t.Cleanup(srv.Close)
	return srv, &attempts
}

// TestCVEngineAnalyzer_RetriesTransientPredictFailure verifies that a socket
// closed by the inference worker is retried instead of failing the frame.
func TestCVEngineAnalyzer_RetriesTransientPredictFailure(t *testing.T) {
	srv, attempts := newFlakyPredictServer(t, 2)
	analyzer := NewCVEngineAnalyzer(srv.URL, srv.Client(), WithPredictRetry(3, time.Millisecond))

	cam := hotset.Camera{ID: "cam-retry", SnapshotURL: srv.URL + "/snapshot/cam-retry.jpg"}
	v, err := analyzer.Analyze(context.Background(), cam)
	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	if got := atomic.LoadInt32(attempts); got != 3 {
		t.Fatalf("expected 3 predict attempts, got %d", got)
	}
	if v.CameraID != cam.ID {
		t.Errorf("expected camera %q, got %q", cam.ID, v.CameraID)
	}
}

// TestCVEngineAnalyzer_StopsAfterMaxAttempts verifies the retry budget is honoured
// and the caller still receives a typed UNKNOWN verdict.
func TestCVEngineAnalyzer_StopsAfterMaxAttempts(t *testing.T) {
	srv, attempts := newFlakyPredictServer(t, 99)
	analyzer := NewCVEngineAnalyzer(srv.URL, srv.Client(), WithPredictRetry(2, time.Millisecond))

	cam := hotset.Camera{ID: "cam-down", SnapshotURL: srv.URL + "/snapshot/cam-down.jpg"}
	v, err := analyzer.Analyze(context.Background(), cam)
	if err == nil {
		t.Fatal("expected an error when every attempt fails")
	}
	if got := atomic.LoadInt32(attempts); got != 2 {
		t.Fatalf("expected 2 predict attempts, got %d", got)
	}
	if v.Severity != hotset.SeverityUnknown || v.CameraID != cam.ID {
		t.Errorf("expected UNKNOWN verdict for %q, got %+v", cam.ID, v)
	}
}

// TestCVEngineAnalyzer_DoesNotRetryOnStandby verifies HTTP responses are never
// retried: a 503 means no model is loaded, which retrying cannot fix.
func TestCVEngineAnalyzer_DoesNotRetryOnStandby(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/snapshot/") {
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("\xff\xd8\xff\xe0minimal-jpeg"))
			return
		}
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	analyzer := NewCVEngineAnalyzer(srv.URL, srv.Client(), WithPredictRetry(4, time.Millisecond))
	cam := hotset.Camera{ID: "cam-standby", SnapshotURL: srv.URL + "/snapshot/cam-standby.jpg"}
	if _, err := analyzer.Analyze(context.Background(), cam); err == nil {
		t.Fatal("expected standby error")
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Fatalf("expected exactly 1 predict attempt on HTTP 503, got %d", got)
	}
}

// newCVHealthServer serves GET /health with the given status and body, and
// records the paths it was asked for so tests can assert the probe URL.
func newCVHealthServer(t *testing.T, status int, body string) (*httptest.Server, *[]string) {
	t.Helper()
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, &paths
}

// TestCVEngineAnalyzer_ProbeHealth_Healthy covers the 200 + model_loaded=true
// case: the engine can serve real inference.
func TestCVEngineAnalyzer_ProbeHealth_Healthy(t *testing.T) {
	const body = `{"status":"ok","service":"cv-engine","openvino_available":true,` +
		`"model_loaded":true,"device":"CPU","model":"yolov8n_openvino_320"}`
	srv, paths := newCVHealthServer(t, http.StatusOK, body)
	analyzer := NewCVEngineAnalyzer(srv.URL, srv.Client())

	before := time.Now()
	h, err := analyzer.ProbeHealth(context.Background())
	if err != nil {
		t.Fatalf("expected no error for healthy engine, got: %v", err)
	}
	if h.Status != CVEngineStatusHealthy {
		t.Errorf("expected status %q, got %q", CVEngineStatusHealthy, h.Status)
	}
	if h.Status != "healthy" {
		t.Errorf("status wire value must be \"healthy\", got %q", h.Status)
	}
	if h.Service != "cv-engine" {
		t.Errorf("expected service %q, got %q", "cv-engine", h.Service)
	}
	if !h.OpenVINOAvailable {
		t.Error("expected openvino_available=true to be reported")
	}
	if !h.ModelLoaded {
		t.Error("expected model_loaded=true to be reported")
	}
	if h.Error != "" {
		t.Errorf("expected empty error on healthy engine, got %q", h.Error)
	}
	if h.LatencyMs < 0 {
		t.Errorf("expected non-negative latency, got %f", h.LatencyMs)
	}
	if h.CheckedAt.Before(before) || h.CheckedAt.After(time.Now().Add(time.Second)) {
		t.Errorf("expected CheckedAt around now, got %v", h.CheckedAt)
	}
	if len(*paths) != 1 || (*paths)[0] != "/health" {
		t.Errorf("expected exactly one GET /health, got %v", *paths)
	}
}

// TestCVEngineAnalyzer_ProbeHealth_Degraded covers a reachable engine that is
// up but has no model loaded: it must never be reported healthy.
func TestCVEngineAnalyzer_ProbeHealth_Degraded(t *testing.T) {
	const body = `{"status":"standby","service":"cv-engine","openvino_available":true,` +
		`"model_loaded":false,"mode":"standby_ready"}`
	srv, _ := newCVHealthServer(t, http.StatusOK, body)
	analyzer := NewCVEngineAnalyzer(srv.URL, srv.Client())

	h, err := analyzer.ProbeHealth(context.Background())
	if err != nil {
		t.Fatalf("expected no error for reachable standby engine, got: %v", err)
	}
	if h.Status != CVEngineStatusDegraded {
		t.Errorf("expected status %q when model_loaded=false, got %q", CVEngineStatusDegraded, h.Status)
	}
	if h.ModelLoaded {
		t.Error("expected model_loaded=false to be reported")
	}
	if !h.OpenVINOAvailable {
		t.Error("expected openvino_available=true to still be reported")
	}
	if h.CheckedAt.IsZero() {
		t.Error("expected non-zero CheckedAt")
	}
}

// TestCVEngineAnalyzer_ProbeHealth_Unhealthy covers every way a probe can fail:
// a non-200 status, an unreachable host, and a payload that is not JSON.
func TestCVEngineAnalyzer_ProbeHealth_Unhealthy(t *testing.T) {
	// A server that is closed before the probe: the port stays unused, so the
	// dial is refused immediately.
	deadSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	deadURL := deadSrv.URL
	deadSrv.Close()

	srv503, _ := newCVHealthServer(t, http.StatusServiceUnavailable,
		`{"status":"error","service":"cv-engine","model_loaded":false,"error":"model not loaded"}`)
	srvBadJSON, _ := newCVHealthServer(t, http.StatusOK, `{"model_loaded":`)

	tests := []struct {
		name    string
		url     string
		client  *http.Client
		wantErr string
	}{
		{name: "non-200 status", url: srv503.URL, client: srv503.Client(), wantErr: "503"},
		{name: "network error", url: deadURL, client: &http.Client{Timeout: 2 * time.Second}, wantErr: "cv-engine health"},
		{name: "malformed payload", url: srvBadJSON.URL, client: srvBadJSON.Client(), wantErr: "decode"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			analyzer := NewCVEngineAnalyzer(tc.url, tc.client)
			h, err := analyzer.ProbeHealth(context.Background())
			if err == nil {
				t.Fatal("expected a non-nil error from a failed probe")
			}
			if h.Status != CVEngineStatusUnhealthy {
				t.Errorf("expected status %q, got %q", CVEngineStatusUnhealthy, h.Status)
			}
			if h.Error == "" {
				t.Error("expected CVEngineHealth.Error to be populated")
			}
			if !strings.Contains(h.Error, tc.wantErr) {
				t.Errorf("expected health error to mention %q, got %q", tc.wantErr, h.Error)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("expected returned error to mention %q, got: %v", tc.wantErr, err)
			}
			// A failed probe must still identify the service and the time it ran.
			if h.Service != "cv-engine" {
				t.Errorf("expected service %q on failure, got %q", "cv-engine", h.Service)
			}
			if h.CheckedAt.IsZero() {
				t.Error("expected non-zero CheckedAt on failure")
			}
		})
	}
}

// TestCVEngineAnalyzer_ProbeHealth_TrailingSlashURL verifies the base URL is
// normalised so CV_ENGINE_URL ending in "/" does not produce "//health".
func TestCVEngineAnalyzer_ProbeHealth_TrailingSlashURL(t *testing.T) {
	srv, paths := newCVHealthServer(t, http.StatusOK, `{"model_loaded":true}`)
	analyzer := NewCVEngineAnalyzer(srv.URL+"/", srv.Client())

	if _, err := analyzer.ProbeHealth(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*paths) != 1 || (*paths)[0] != "/health" {
		t.Fatalf("expected GET /health, got %v", *paths)
	}
}

// TestCVEngineAnalyzer_ProbeHealth_DefaultsServiceName verifies a minimal
// payload still yields a usable report.
func TestCVEngineAnalyzer_ProbeHealth_DefaultsServiceName(t *testing.T) {
	srv, _ := newCVHealthServer(t, http.StatusOK, `{"model_loaded":true}`)
	analyzer := NewCVEngineAnalyzer(srv.URL, srv.Client())

	h, err := analyzer.ProbeHealth(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h.Service != "cv-engine" {
		t.Errorf("expected default service name %q, got %q", "cv-engine", h.Service)
	}
	if h.Status != CVEngineStatusHealthy {
		t.Errorf("expected status %q, got %q", CVEngineStatusHealthy, h.Status)
	}
}

// assertFeedError fails unless err is a *FeedError whose IsFeedError reports
// true, both directly and through the interface marker consumers should use.
func assertFeedError(t *testing.T, err error) {
	t.Helper()
	var feedErr *FeedError
	if !errors.As(err, &feedErr) {
		t.Fatalf("expected *FeedError, got %T: %v", err, err)
	}
	if feedErr.CameraID == "" {
		t.Error("expected FeedError.CameraID to be set")
	}
	if feedErr.URL == "" {
		t.Error("expected FeedError.URL to be set")
	}

	var marker interface{ IsFeedError() bool }
	if !errors.As(err, &marker) {
		t.Fatalf("expected error to expose IsFeedError(), got %T", err)
	}
	if !marker.IsFeedError() {
		t.Error("expected IsFeedError() == true")
	}
}

// assertNotFeedError fails if err is (or wraps) a feed error.
func assertNotFeedError(t *testing.T, err error) {
	t.Helper()
	var feedErr *FeedError
	if errors.As(err, &feedErr) {
		t.Fatalf("expected a cv-engine error, got a FeedError: %v", err)
	}
	var marker interface{ IsFeedError() bool }
	if errors.As(err, &marker) {
		t.Fatalf("expected a cv-engine error, got one exposing IsFeedError(): %v", err)
	}
}

// TestCVEngineAnalyzer_Analyze_SnapshotFailuresAreFeedErrors verifies every
// camera-feed failure is typed so consumers can park the camera rather than the
// whole pipeline.
func TestCVEngineAnalyzer_Analyze_SnapshotFailuresAreFeedErrors(t *testing.T) {
	t.Run("snapshot non-200", func(t *testing.T) {
		srv, analyzer := makeCVEngineTestServer(t, http.StatusNotFound, http.StatusOK,
			`{"traffic_severity":"CLEAR","confidence":0.9}`)
		cam := hotset.Camera{ID: "cam-feed-404", SnapshotURL: srv.URL + "/snapshot/cam-feed-404.jpg"}

		v, err := analyzer.Analyze(context.Background(), cam)
		if err == nil {
			t.Fatal("expected error for snapshot 404")
		}
		assertFeedError(t, err)
		if !strings.Contains(err.Error(), "404") {
			t.Errorf("expected error to mention the status, got: %v", err)
		}
		if v.Severity != hotset.SeverityUnknown {
			t.Errorf("expected UNKNOWN verdict, got %q", v.Severity)
		}
	})

	t.Run("snapshot empty body", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/snapshot/", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "image/jpeg")
			w.WriteHeader(http.StatusOK)
		})
		mux.HandleFunc("/predict", func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, `{"traffic_severity":"CLEAR"}`)
		})
		srv := httptest.NewServer(mux)
		t.Cleanup(srv.Close)

		analyzer := NewCVEngineAnalyzer(srv.URL, srv.Client())
		cam := hotset.Camera{ID: "cam-feed-empty", SnapshotURL: srv.URL + "/snapshot/empty.jpg"}

		_, err := analyzer.Analyze(context.Background(), cam)
		if err == nil {
			t.Fatal("expected error for empty snapshot body")
		}
		assertFeedError(t, err)
		if !strings.Contains(err.Error(), "empty") {
			t.Errorf("expected error to mention the empty body, got: %v", err)
		}
	})

	t.Run("snapshot host unreachable", func(t *testing.T) {
		deadSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		deadURL := deadSrv.URL
		deadSrv.Close()

		analyzer := NewCVEngineAnalyzer("http://127.0.0.1:1", &http.Client{Timeout: 2 * time.Second})
		cam := hotset.Camera{ID: "cam-feed-down", SnapshotURL: deadURL + "/snapshot/down.jpg"}

		_, err := analyzer.Analyze(context.Background(), cam)
		if err == nil {
			t.Fatal("expected error for unreachable snapshot host")
		}
		assertFeedError(t, err)
	})
}

// TestCVEngineAnalyzer_Analyze_PredictFailuresAreNotFeedErrors verifies the
// inverse: an inference-side failure keeps the camera feed "healthy" so the
// operator sees the engine as the culprit.
func TestCVEngineAnalyzer_Analyze_PredictFailuresAreNotFeedErrors(t *testing.T) {
	tests := []struct {
		name          string
		predictStatus int
		predictBody   string
	}{
		{
			name:          "predict 500",
			predictStatus: http.StatusInternalServerError,
			predictBody:   `{"error":"inference failed"}`,
		},
		{
			name:          "standby 503",
			predictStatus: http.StatusServiceUnavailable,
			predictBody:   `{"source":"standby_dummy","mode":"standby_ready"}`,
		},
		{
			name:          "standby source on 200",
			predictStatus: http.StatusOK,
			predictBody:   `{"source":"standby_dummy","confidence":0}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, analyzer := makeCVEngineTestServer(t, http.StatusOK, tc.predictStatus, tc.predictBody)
			cam := hotset.Camera{ID: "cam-engine-down", SnapshotURL: srv.URL + "/snapshot/cam-engine-down.jpg"}

			_, err := analyzer.Analyze(context.Background(), cam)
			if err == nil {
				t.Fatal("expected an error from the predict path")
			}
			assertNotFeedError(t, err)
		})
	}
}

// TestCVEngineAnalyzer_Analyze_DirectionalRegionsPredictFailureNoPanic ensures
// that if predict fails on a camera with directional regions, the error is returned
// without panicking on nil predResp (Issue #452 / shadowing bug regression).
func TestCVEngineAnalyzer_Analyze_DirectionalRegionsPredictFailureNoPanic(t *testing.T) {
	srv, analyzer := makeCVEngineTestServer(t, http.StatusOK, http.StatusInternalServerError, `{"error":"inference failed"}`)
	cam := hotset.Camera{
		ID:          "cam-directional-err",
		SnapshotURL: srv.URL + "/snapshot/cam-directional-err.jpg",
		Regions: []hotset.RoadwayRegion{
			{RegionID: "r1", Direction: "EB", CapacityShare: 0.5},
		},
	}

	verdict, err := analyzer.Analyze(context.Background(), cam)
	if err == nil {
		t.Fatal("expected an error when predict fails")
	}
	if verdict.Severity != hotset.SeverityUnknown {
		t.Errorf("expected unknown severity on error, got %s", verdict.Severity)
	}
}
