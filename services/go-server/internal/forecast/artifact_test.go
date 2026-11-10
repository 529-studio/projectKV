package forecast_test

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/529-studio/projectKV/services/go-server/internal/forecast"
)

func TestArtifact_DefaultEmbedded(t *testing.T) {
	art := forecast.DefaultArtifact()
	if art == nil {
		t.Fatal("expected non-nil default artifact")
	}

	if art.SchemaVersion != 1 {
		t.Errorf("expected schema_version 1, got %d", art.SchemaVersion)
	}

	if len(art.SHA256) != 64 {
		t.Errorf("expected 64-char sha256 checksum, got %q", art.SHA256)
	}

	entry, ok := art.Entry(forecast.ModelRidgeDelta)
	if !ok {
		t.Fatalf("expected ridge_delta entry in default artifact")
	}

	if entry.FitSource != forecast.FitSourceLiveHistory {
		t.Errorf("expected fit_source live_history, got %s", entry.FitSource)
	}

	for _, h := range []int{10, 20, 30} {
		p, exists := entry.Params(h)
		if !exists {
			t.Errorf("missing params for horizon %d", h)
		}
		if p.HorizonMinutes != h {
			t.Errorf("expected horizon %d, got %d", h, p.HorizonMinutes)
		}
		if p.MAE <= 0 {
			t.Errorf("expected positive MAE for horizon %d, got %v", h, p.MAE)
		}
		if p.RMSE <= 0 {
			t.Errorf("expected positive RMSE for horizon %d, got %v", h, p.RMSE)
		}
	}

	meta := art.DefaultMetadata()
	if meta.Name != forecast.ModelRidgeDelta {
		t.Errorf("expected meta name %q, got %q", forecast.ModelRidgeDelta, meta.Name)
	}
	if !meta.Validated {
		t.Errorf("default embedded prototype should be validated (true)")
	}
	if meta.ArtifactSHA256 != art.SHA256 {
		t.Errorf("expected artifact sha256 match, got %q vs %q", meta.ArtifactSHA256, art.SHA256)
	}
}

func TestArtifact_LoadFromFile(t *testing.T) {
	tmpDir := t.TempDir()

	validJSON := `{
  "schema_version": 1,
  "models": [
    {
      "name": "ridge_delta",
      "version": "test-v1",
      "target_mode": "delta",
      "fit_source": "live_history",
      "fit_rows": 500,
      "fit_cameras": 10,
      "fit_data_sha256": "data_hash_test",
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
            "intercept": 0.01,
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
            "intercept": 0.01,
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
            "intercept": 0.01,
            "means": [0.1, 0.1, 0.1, 0.1, 10.0, 2.0],
            "stds": [1.0, 1.0, 1.0, 1.0, 5.0, 1.0],
            "coeffs": [0.03, -0.01, -0.02, 0.0, 0.0, 0.0]
          }
        }
      ]
    }
  ]
}`

	validPath := filepath.Join(tmpDir, "model.json")
	if err := os.WriteFile(validPath, []byte(validJSON), 0o600); err != nil {
		t.Fatalf("failed to write valid json: %v", err)
	}

	art, err := forecast.LoadArtifact(validPath)
	if err != nil {
		t.Fatalf("expected successful load, got: %v", err)
	}

	expectedHash := fmt.Sprintf("%x", sha256.Sum256([]byte(validJSON)))
	if art.SHA256 != expectedHash {
		t.Errorf("expected hash %q, got %q", expectedHash, art.SHA256)
	}

	meta := art.DefaultMetadata()
	if !meta.Validated {
		t.Errorf("expected live_history to be marked validated (true)")
	}
	if meta.Version != "test-v1" {
		t.Errorf("expected version test-v1, got %q", meta.Version)
	}

	// Empty path falls back to default
	defaultArt, err := forecast.LoadArtifact("")
	if err != nil {
		t.Fatalf("expected default artifact on empty path, got: %v", err)
	}
	if defaultArt.DefaultMetadata().Version != forecast.DefaultModelMetadata.Version {
		t.Errorf("expected default metadata version match")
	}

	// Invalid extension guard
	txtPath := filepath.Join(tmpDir, "model.txt")
	_ = os.WriteFile(txtPath, []byte("{}"), 0o600)
	_, err = forecast.LoadArtifact(txtPath)
	if err == nil || !strings.Contains(err.Error(), "untrusted or unsafe") {
		t.Errorf("expected path untrusted error for non-json, got %v", err)
	}

	// File not found guard
	_, err = forecast.LoadArtifact(filepath.Join(tmpDir, "missing.json"))
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected not found error, got %v", err)
	}
}

func TestArtifact_ExternalFileLoad(t *testing.T) {
	path := os.Getenv("FORECAST_TEST_ARTIFACT_PATH")
	if path == "" {
		t.Skip("FORECAST_TEST_ARTIFACT_PATH not set; skipping external artifact verification")
	}

	art, err := forecast.LoadArtifact(path)
	if err != nil {
		t.Fatalf("LoadArtifact failed on %q: %v", path, err)
	}
	if art == nil {
		t.Fatal("parsed artifact is nil")
	}

	entry, ok := art.Entry(forecast.ModelRidgeDelta)
	if !ok {
		t.Fatal("missing ridge_delta model entry")
	}
	if entry.FitSource != forecast.FitSourceLiveHistory {
		t.Fatalf("expected live_history, got %v", entry.FitSource)
	}
	for _, h := range []int{10, 20, 30} {
		p, exists := entry.Params(h)
		if !exists {
			t.Fatalf("missing params for horizon %d", h)
		}
		if p.HorizonMinutes != h {
			t.Fatalf("expected horizon %d, got %d", h, p.HorizonMinutes)
		}
		if len(p.Means) != 6 || len(p.Stds) != 6 || len(p.Coeffs) != 6 {
			t.Fatal("expected 6 features in means/stds/coeffs")
		}
	}
}

func TestArtifact_ValidationFailures(t *testing.T) {
	// Missing ridge_delta model
	noRidgeJSON := []byte(`{"schema_version": 1, "models": [{"name": "persistence", "fit_source": "synthetic_fixture", "horizons": [{"horizon_minutes": 10, "mae": 0.01, "rmse": 0.02}]}]}`)
	_, err := forecast.LoadArtifactFromBytes(noRidgeJSON)
	if err == nil {
		t.Errorf("expected error for missing ridge_delta candidate model")
	}

	// Invalid fit_source
	badSourceJSON := []byte(`{"schema_version": 1, "models": [{"name": "ridge_delta", "fit_source": "unknown_source", "horizons": [{"horizon_minutes": 10, "mae": 0.01, "rmse": 0.02}]}]}`)
	_, err = forecast.LoadArtifactFromBytes(badSourceJSON)
	if err == nil {
		t.Errorf("expected error for invalid fit_source")
	}

	// Unsupported horizon
	badHorizonJSON := []byte(`{"schema_version": 1, "models": [{"name": "ridge_delta", "fit_source": "synthetic_fixture", "horizons": [{"horizon_minutes": 45, "mae": 0.01, "rmse": 0.02}]}]}`)
	_, err = forecast.LoadArtifactFromBytes(badHorizonJSON)
	if err == nil {
		t.Errorf("expected error for unsupported horizon")
	}

	// Schema version zero
	badSchemaJSON := []byte(`{"schema_version": 0, "models": [{"name": "ridge_delta", "fit_source": "synthetic_fixture", "horizons": [{"horizon_minutes": 10, "mae": 0.01, "rmse": 0.02}]}]}`)
	_, err = forecast.LoadArtifactFromBytes(badSchemaJSON)
	if err == nil {
		t.Errorf("expected error for non-positive schema version")
	}

	// RB1: Partial horizon coverage (missing horizon 20 and 30)
	partialHorizonJSON := []byte(`{"schema_version": 1, "models": [{"name": "ridge_delta", "fit_source": "synthetic_fixture", "horizons": [{"horizon_minutes": 10, "mae": 0.01, "rmse": 0.02, "params": {"horizon_minutes": 10, "means": [0.1], "stds": [1.0], "coeffs": [0.0]}}]}]}`)
	_, err = forecast.LoadArtifactFromBytes(partialHorizonJSON)
	if err == nil || !strings.Contains(err.Error(), "missing required horizon") {
		t.Errorf("expected error for partial horizon coverage, got: %v", err)
	}

	// RB1: Missing params on a horizon
	missingParamsJSON := []byte(`{"schema_version": 1, "models": [{"name": "ridge_delta", "fit_source": "synthetic_fixture", "horizons": [{"horizon_minutes": 10, "mae": 0.01, "rmse": 0.02}, {"horizon_minutes": 20, "mae": 0.01, "rmse": 0.02}, {"horizon_minutes": 30, "mae": 0.01, "rmse": 0.02}]}]}`)
	_, err = forecast.LoadArtifactFromBytes(missingParamsJSON)
	if err == nil || !strings.Contains(err.Error(), "missing parameters") {
		t.Errorf("expected error for nil params on horizon, got: %v", err)
	}

	// N7: live_history with fit_rows = 0
	zeroFitRowsJSON := []byte(`{"schema_version": 1, "models": [{"name": "ridge_delta", "fit_source": "live_history", "fit_rows": 0, "fit_window": {"start": "2026-11-01T00:00:00Z", "end": "2026-11-02T00:00:00Z"}, "horizons": [{"horizon_minutes": 10, "mae": 0.01, "rmse": 0.02, "params": {"horizon_minutes": 10, "means": [0.1], "stds": [1.0], "coeffs": [0.0]}}, {"horizon_minutes": 20, "mae": 0.01, "rmse": 0.02, "params": {"horizon_minutes": 20, "means": [0.1], "stds": [1.0], "coeffs": [0.0]}}, {"horizon_minutes": 30, "mae": 0.01, "rmse": 0.02, "params": {"horizon_minutes": 30, "means": [0.1], "stds": [1.0], "coeffs": [0.0]}}]}]}`)
	_, err = forecast.LoadArtifactFromBytes(zeroFitRowsJSON)
	if err == nil || !strings.Contains(err.Error(), "positive fit_rows") {
		t.Errorf("expected error for live_history with fit_rows=0, got: %v", err)
	}

	// N7: live_history with nil or empty fit_window
	nilFitWindowJSON := []byte(`{"schema_version": 1, "models": [{"name": "ridge_delta", "fit_source": "live_history", "fit_rows": 100, "fit_window": null, "horizons": [{"horizon_minutes": 10, "mae": 0.01, "rmse": 0.02, "params": {"horizon_minutes": 10, "means": [0.1], "stds": [1.0], "coeffs": [0.0]}}, {"horizon_minutes": 20, "mae": 0.01, "rmse": 0.02, "params": {"horizon_minutes": 20, "means": [0.1], "stds": [1.0], "coeffs": [0.0]}}, {"horizon_minutes": 30, "mae": 0.01, "rmse": 0.02, "params": {"horizon_minutes": 30, "means": [0.1], "stds": [1.0], "coeffs": [0.0]}}]}]}`)
	_, err = forecast.LoadArtifactFromBytes(nilFitWindowJSON)
	if err == nil || !strings.Contains(err.Error(), "specify fit_window") {
		t.Errorf("expected error for live_history with nil fit_window, got: %v", err)
	}

	// N7: live_history with empty-string fit_window fields
	emptyWindowJSON := []byte(`{"schema_version": 1, "models": [{"name": "ridge_delta", "fit_source": "live_history", "fit_rows": 100, "fit_window": {"start": "", "end": ""}, "horizons": [{"horizon_minutes": 10, "mae": 0.01, "rmse": 0.02, "params": {"horizon_minutes": 10, "means": [0.1], "stds": [1.0], "coeffs": [0.0]}}, {"horizon_minutes": 20, "mae": 0.01, "rmse": 0.02, "params": {"horizon_minutes": 20, "means": [0.1], "stds": [1.0], "coeffs": [0.0]}}, {"horizon_minutes": 30, "mae": 0.01, "rmse": 0.02, "params": {"horizon_minutes": 30, "means": [0.1], "stds": [1.0], "coeffs": [0.0]}}]}]}`)
	_, err = forecast.LoadArtifactFromBytes(emptyWindowJSON)
	if err == nil || !strings.Contains(err.Error(), "specify fit_window") {
		t.Errorf("expected error for live_history with empty fit_window fields, got: %v", err)
	}

	// N12: conflicting top-level fit_source
	conflictSourceJSON := []byte(`{"schema_version": 1, "fit_source": "live_history", "models": [{"name": "ridge_delta", "fit_source": "synthetic_fixture", "horizons": [{"horizon_minutes": 10, "mae": 0.01, "rmse": 0.02, "params": {"horizon_minutes": 10, "means": [0.1], "stds": [1.0], "coeffs": [0.0]}}, {"horizon_minutes": 20, "mae": 0.01, "rmse": 0.02, "params": {"horizon_minutes": 20, "means": [0.1], "stds": [1.0], "coeffs": [0.0]}}, {"horizon_minutes": 30, "mae": 0.01, "rmse": 0.02, "params": {"horizon_minutes": 30, "means": [0.1], "stds": [1.0], "coeffs": [0.0]}}]}]}`)
	_, err = forecast.LoadArtifactFromBytes(conflictSourceJSON)
	if err == nil || !strings.Contains(err.Error(), "conflicts with top-level fit_source") {
		t.Errorf("expected error for conflicting fit_source, got: %v", err)
	}
}
