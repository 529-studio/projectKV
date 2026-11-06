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

	if entry.FitSource != forecast.FitSourceSyntheticFixture {
		t.Errorf("expected fit_source synthetic_fixture, got %s", entry.FitSource)
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
	if meta.Validated {
		t.Errorf("default embedded prototype should be unvalidated (false)")
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
}
