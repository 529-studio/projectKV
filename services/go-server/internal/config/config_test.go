package config_test

import (
	"os"
	"testing"

	"github.com/529-studio/projectKV/services/go-server/internal/config"
)

func TestLoadConfig_DefaultAnalyzerIsNone(t *testing.T) {
	_ = os.Unsetenv("ANALYZER")
	_ = os.Unsetenv("APP_ENV")

	cfg := config.LoadConfig()
	if cfg.Analyzer != "none" {
		t.Errorf("expected default Analyzer=none, got %q", cfg.Analyzer)
	}
}

func TestLoadConfig_CustomAnalyzer(t *testing.T) {
	_ = os.Setenv("ANALYZER", "mock")
	_ = os.Setenv("APP_ENV", "dev")
	defer func() {
		_ = os.Unsetenv("ANALYZER")
		_ = os.Unsetenv("APP_ENV")
	}()

	cfg := config.LoadConfig()
	if cfg.Analyzer != "mock" {
		t.Errorf("expected Analyzer=mock, got %q", cfg.Analyzer)
	}
	if cfg.AppEnv != "dev" {
		t.Errorf("expected AppEnv=dev, got %q", cfg.AppEnv)
	}
}
