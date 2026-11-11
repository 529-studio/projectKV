package forecast_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/529-studio/projectKV/services/go-server/internal/forecast"
)

func makeTestArtifactJSON(version string, intercept float64) string {
	// Pad content based on version to guarantee file size changes across versions (M10)
	padding := ""
	for i := 0; i < len(version); i++ {
		padding += " "
	}
	return fmt.Sprintf(`{
  "schema_version": 1,
  "models": [
    {
      "name": "ridge_delta",
      "version": "%s",
      "target_mode": "delta",
      "fit_source": "live_history",
      "fit_rows": 500,
      "fit_cameras": 10,
      "fit_data_sha256": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
      "fit_window": {
        "start": "2026-11-01T00:00:00Z",
        "end": "2026-11-02T00:00:00Z"
      },
      "horizons": [
        {
          "horizon_minutes": 10,
          "mae": 0.005,
          "rmse": 0.006,
          "sample_count": 500,
          "params": {
            "horizon_minutes": 10,
            "intercept": %f,
            "means": [0.1, 0.1, 0.1, 0.1, 10.0, 2.0],
            "stds": [1.0, 1.0, 1.0, 1.0, 5.0, 1.0],
            "coeffs": [0.05, -0.01, -0.04, 0.0, 0.0, 0.0]
          }
        },
        {
          "horizon_minutes": 20,
          "mae": 0.007,
          "rmse": 0.008,
          "sample_count": 500,
          "params": {
            "horizon_minutes": 20,
            "intercept": %f,
            "means": [0.1, 0.1, 0.1, 0.1, 10.0, 2.0],
            "stds": [1.0, 1.0, 1.0, 1.0, 5.0, 1.0],
            "coeffs": [0.04, -0.01, -0.03, 0.0, 0.0, 0.0]
          }
        },
        {
          "horizon_minutes": 30,
          "mae": 0.009,
          "rmse": 0.010,
          "sample_count": 500,
          "params": {
            "horizon_minutes": 30,
            "intercept": %f,
            "means": [0.1, 0.1, 0.1, 0.1, 10.0, 2.0],
            "stds": [1.0, 1.0, 1.0, 1.0, 5.0, 1.0],
            "coeffs": [0.03, -0.01, -0.02, 0.0, 0.0, 0.0]
          }
        }
      ]
    }
  ]
%s}`, version, intercept, intercept, intercept, padding)
}

func writeAtomic(t *testing.T, path, content string) {
	t.Helper()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatalf("rename file: %v", err)
	}
}

func TestReloader_FileModification(t *testing.T) {
	tmpDir := t.TempDir()
	artPath := filepath.Join(tmpDir, "prototype_v1.json")

	writeAtomic(t, artPath, makeTestArtifactJSON("v1", 0.01))

	svc := forecast.NewService(nil, nil, forecast.WithModelPath(artPath))

	if got := svc.Model().Version; got != "v1" {
		t.Fatalf("initial version: got %q, want %q", got, "v1")
	}

	// No modification -> ReloadIfModified reports false
	reloaded, err := svc.ReloadIfModified()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reloaded {
		t.Fatal("expected no reload when file unchanged")
	}

	// Wait briefly so mtime advances
	time.Sleep(10 * time.Millisecond)

	// Write new valid version
	writeAtomic(t, artPath, makeTestArtifactJSON("v2", 0.02))

	reloaded, err = svc.ReloadIfModified()
	if err != nil {
		t.Fatalf("unexpected error on reload: %v", err)
	}
	if !reloaded {
		t.Fatal("expected reload when file changed")
	}
	if got := svc.Model().Version; got != "v2" {
		t.Fatalf("updated version: got %q, want %q", got, "v2")
	}
}

func TestReloader_CorruptSwapPreservesPreviousModel(t *testing.T) {
	tmpDir := t.TempDir()
	artPath := filepath.Join(tmpDir, "prototype_v1.json")

	writeAtomic(t, artPath, makeTestArtifactJSON("v1", 0.01))

	svc := forecast.NewService(nil, nil, forecast.WithModelPath(artPath))
	if got := svc.Model().Version; got != "v1" {
		t.Fatalf("initial version: got %q, want %q", got, "v1")
	}

	time.Sleep(10 * time.Millisecond)

	// Write corrupt JSON
	writeAtomic(t, artPath, "{ invalid json content truncated")

	reloaded, err := svc.ReloadIfModified()
	if err == nil {
		t.Fatal("expected error on corrupt file reload")
	}
	if reloaded {
		t.Fatal("expected reloaded = false on corrupt file")
	}

	// Model version must remain v1 (graceful degradation)
	if got := svc.Model().Version; got != "v1" {
		t.Fatalf("model degraded to corrupted state: got %q, want %q", got, "v1")
	}

	// Repair file to v3
	time.Sleep(10 * time.Millisecond)
	writeAtomic(t, artPath, makeTestArtifactJSON("v3", 0.03))

	reloaded, err = svc.ReloadIfModified()
	if err != nil {
		t.Fatalf("unexpected error on recovered reload: %v", err)
	}
	if !reloaded {
		t.Fatal("expected reload after file recovered")
	}
	if got := svc.Model().Version; got != "v3" {
		t.Fatalf("recovered version: got %q, want %q", got, "v3")
	}
}

func TestReloader_ConcurrentReadsAndReloads(t *testing.T) {
	tmpDir := t.TempDir()
	artPath := filepath.Join(tmpDir, "prototype_v1.json")

	writeAtomic(t, artPath, makeTestArtifactJSON("v0", 0.01))

	svc := forecast.NewService(nil, nil, forecast.WithModelPath(artPath))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	var readsCount atomic.Int64

	// Concurrent readers
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
					m := svc.Model()
					if m.Name == "" || m.FitSource == "" {
						t.Errorf("observed incomplete model metadata: %+v", m)
					}
					readsCount.Add(1)
					time.Sleep(1 * time.Millisecond)
				}
			}
		}()
	}

	// Concurrent reloader updating version
	wg.Add(1)
	go func() {
		defer wg.Done()
		for ver := 1; ver <= 10; ver++ {
			select {
			case <-ctx.Done():
				return
			default:
				time.Sleep(10 * time.Millisecond)
				writeAtomic(t, artPath, makeTestArtifactJSON(fmt.Sprintf("v%d", ver), 0.01*float64(ver)))
				_, _ = svc.ReloadIfModified()
			}
		}
	}()

	wg.Wait()

	if readsCount.Load() == 0 {
		t.Fatal("no reads occurred during concurrent reload test")
	}
}

func TestReloader_BackgroundTickerStops(t *testing.T) {
	tmpDir := t.TempDir()
	artPath := filepath.Join(tmpDir, "prototype_v1.json")

	writeAtomic(t, artPath, makeTestArtifactJSON("v1", 0.01))

	svc := forecast.NewService(nil, nil, forecast.WithModelPath(artPath))

	ctx, cancel := context.WithCancel(context.Background())
	svc.StartReloader(ctx, 20*time.Millisecond)

	// Idempotent start test
	svc.StartReloader(ctx, 20*time.Millisecond)

	time.Sleep(10 * time.Millisecond)
	writeAtomic(t, artPath, makeTestArtifactJSON("v2", 0.02))

	// Wait for background ticker to pick up changes
	time.Sleep(60 * time.Millisecond)

	if got := svc.Model().Version; got != "v2" {
		t.Fatalf("background reloader did not pick up v2: got %q, want %q", got, "v2")
	}

	// Cancel context to stop ticker
	cancel()
	time.Sleep(30 * time.Millisecond)

	// Subsequent file modification should not be picked up after cancel
	writeAtomic(t, artPath, makeTestArtifactJSON("v3", 0.03))
	time.Sleep(60 * time.Millisecond)

	if got := svc.Model().Version; got != "v2" {
		t.Fatalf("background reloader kept running after cancel: got %q, want %q", got, "v2")
	}

	// Verify restart after cancellation works
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	svc.StartReloader(ctx2, 20*time.Millisecond)
	time.Sleep(10 * time.Millisecond)
	writeAtomic(t, artPath, makeTestArtifactJSON("v4", 0.04))
	time.Sleep(60 * time.Millisecond)
	if got := svc.Model().Version; got != "v4" {
		t.Fatalf("reloader restart failed: got %q, want %q", got, "v4")
	}
}

func TestReloader_EnvironmentVariables(t *testing.T) {
	tmpDir := t.TempDir()
	artPath := filepath.Join(tmpDir, "prototype_env.json")
	writeAtomic(t, artPath, makeTestArtifactJSON("v_env", 0.05))

	t.Run("default_reload_interval", func(t *testing.T) {
		svc := forecast.NewService(nil, nil)
		if got := svc.ReloadInterval(); got != forecast.DefaultReloadInterval {
			t.Fatalf("expected default reload interval %v, got %v", forecast.DefaultReloadInterval, got)
		}
	})

	t.Run("env_model_path_fallback", func(t *testing.T) {
		t.Setenv(forecast.EnvModelPath, artPath)
		t.Setenv(forecast.EnvReloadIntervalSec, "30")

		svc := forecast.NewService(nil, nil)
		if got := svc.ModelPath(); got != artPath {
			t.Fatalf("expected model path %q, got %q", artPath, got)
		}
		if got := svc.Model().Version; got != "v_env" {
			t.Fatalf("expected version %q, got %q", "v_env", got)
		}
		if got := svc.ReloadInterval(); got != 30*time.Second {
			t.Fatalf("expected reload interval 30s, got %v", got)
		}
	})

	t.Run("env_reload_zero_disables", func(t *testing.T) {
		t.Setenv(forecast.EnvReloadIntervalSec, "0")
		svc := forecast.NewService(nil, nil)
		if got := svc.ReloadInterval(); got != 0 {
			t.Fatalf("expected reload interval 0, got %v", got)
		}
	})

	t.Run("env_reload_seconds_fallback_alias", func(t *testing.T) {
		t.Setenv(forecast.EnvReloadIntervalSec, "")
		t.Setenv(forecast.EnvReloadSeconds, "45")
		svc := forecast.NewService(nil, nil)
		if got := svc.ReloadInterval(); got != 45*time.Second {
			t.Fatalf("expected reload interval 45s, got %v", got)
		}
	})
}
