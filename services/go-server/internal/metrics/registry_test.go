package metrics

import (
	"bytes"
	"strings"
	"sync"
	"testing"
)

func TestGauge(t *testing.T) {
	g := NewGauge()
	if g.Get() != 0 {
		t.Fatalf("expected 0, got %g", g.Get())
	}
	g.Set(42.5)
	if g.Get() != 42.5 {
		t.Fatalf("expected 42.5, got %g", g.Get())
	}
	g.Add(7.5)
	if g.Get() != 50.0 {
		t.Fatalf("expected 50.0, got %g", g.Get())
	}
	g.Add(-10.0)
	if g.Get() != 40.0 {
		t.Fatalf("expected 40.0, got %g", g.Get())
	}
}

func TestCounter(t *testing.T) {
	c := NewCounter()
	if c.Get() != 0 {
		t.Fatalf("expected 0, got %g", c.Get())
	}
	c.Inc()
	if c.Get() != 1 {
		t.Fatalf("expected 1, got %g", c.Get())
	}
	c.Add(10.5)
	if c.Get() != 11.5 {
		t.Fatalf("expected 11.5, got %g", c.Get())
	}
	// Negative addition is a no-op for monotonic counters
	c.Add(-5.0)
	if c.Get() != 11.5 {
		t.Fatalf("expected 11.5 after negative add, got %g", c.Get())
	}
}

func TestHistogram(t *testing.T) {
	h := NewHistogram([]float64{0.1, 0.5, 1.0})
	h.Observe(0.05)
	h.Observe(0.3)
	h.Observe(0.8)
	h.Observe(2.5)

	snap := h.Snapshot()
	if snap.Count != 4 {
		t.Fatalf("expected count 4, got %d", snap.Count)
	}
	if snap.Sum != 0.05+0.3+0.8+2.5 {
		t.Fatalf("expected sum %g, got %g", 0.05+0.3+0.8+2.5, snap.Sum)
	}
	// bucket <= 0.1: [0.05] -> 1
	if snap.Counts[0] != 1 {
		t.Errorf("bucket <= 0.1: expected 1, got %d", snap.Counts[0])
	}
	// bucket <= 0.5: [0.05, 0.3] -> 2
	if snap.Counts[1] != 2 {
		t.Errorf("bucket <= 0.5: expected 2, got %d", snap.Counts[1])
	}
	// bucket <= 1.0: [0.05, 0.3, 0.8] -> 3
	if snap.Counts[2] != 3 {
		t.Errorf("bucket <= 1.0: expected 3, got %d", snap.Counts[2])
	}
}

func TestRegistry_WriteOpenMetrics(t *testing.T) {
	reg := NewRegistry()

	g := reg.RegisterGauge("warded_corridor_active_count", "Active corridors", nil)
	g.Set(12)

	reg.RegisterGaugeFunc("warded_camera_offline_count", "Offline cameras", func() float64 {
		return 3.0
	}, nil)

	c := reg.RegisterCounter("warded_http_requests_total", "Total requests", map[string]string{
		"method": "GET",
		"status": "200",
	})
	c.Add(150)

	h := reg.RegisterHistogram("warded_http_request_duration_seconds", "Latency", []float64{0.1, 1.0}, map[string]string{
		"path": "/api/health",
	})
	h.Observe(0.05)
	h.Observe(0.5)

	var buf bytes.Buffer
	if err := reg.WriteOpenMetrics(&buf); err != nil {
		t.Fatalf("WriteOpenMetrics error: %v", err)
	}

	out := buf.String()

	// Verify headers and types
	if !strings.Contains(out, "# HELP warded_corridor_active_count Active corridors\n# TYPE warded_corridor_active_count gauge\nwarded_corridor_active_count 12\n") {
		t.Errorf("missing corridor active count in output:\n%s", out)
	}
	if !strings.Contains(out, "# HELP warded_camera_offline_count Offline cameras\n# TYPE warded_camera_offline_count gauge\nwarded_camera_offline_count 3\n") {
		t.Errorf("missing camera offline count in output:\n%s", out)
	}
	if !strings.Contains(out, `warded_http_requests_total{method="GET",status="200"} 150`) {
		t.Errorf("missing http requests total with labels in output:\n%s", out)
	}
	if !strings.Contains(out, `warded_http_request_duration_seconds_bucket{le="0.1",path="/api/health"} 1`) {
		t.Errorf("missing histogram bucket in output:\n%s", out)
	}
	if !strings.Contains(out, `warded_http_request_duration_seconds_bucket{le="+Inf",path="/api/health"} 2`) {
		t.Errorf("missing histogram +Inf bucket in output:\n%s", out)
	}
}

func TestRegistry_ZeroUnboundedGrowth(t *testing.T) {
	reg := NewRegistry()

	// Repeated registration of the same metric and labels must reuse existing collector
	for i := 0; i < 10000; i++ {
		c := reg.RegisterCounter("test_counter", "help text", map[string]string{"env": "test"})
		c.Inc()
	}

	reg.mu.RLock()
	defer reg.mu.RUnlock()

	collectors := reg.collectors["test_counter"]
	if len(collectors) != 1 {
		t.Fatalf("expected exactly 1 collector registered, got %d", len(collectors))
	}
}

func TestRegistry_ConcurrentAccess(t *testing.T) {
	reg := NewRegistry()
	g := reg.RegisterGauge("concurrent_gauge", "Concurrent test gauge", nil)
	c := reg.RegisterCounter("concurrent_counter", "Concurrent test counter", nil)
	h := reg.RegisterHistogram("concurrent_hist", "Concurrent hist", []float64{10, 50, 100}, nil)

	var wg sync.WaitGroup
	workers := 50
	iterations := 200

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				g.Set(float64(workerID * j))
				c.Inc()
				h.Observe(float64(j % 120))
				if j%20 == 0 {
					var buf bytes.Buffer
					_ = reg.WriteOpenMetrics(&buf)
				}
			}
		}(i)
	}

	wg.Wait()

	if c.Get() != float64(workers*iterations) {
		t.Fatalf("expected counter %f, got %f", float64(workers*iterations), c.Get())
	}
}
