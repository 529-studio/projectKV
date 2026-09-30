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

func TestLoadConfig_DefaultAbuseGuardValues(t *testing.T) {
	_ = os.Unsetenv("RATE_LIMIT_RPS")
	_ = os.Unsetenv("RATE_LIMIT_BURST")
	_ = os.Unsetenv("ANALYZER_DAILY_BUDGET")

	cfg := config.LoadConfig()
	if cfg.RateLimitRPS != 10 {
		t.Errorf("expected RateLimitRPS=10, got %d", cfg.RateLimitRPS)
	}
	if cfg.RateLimitBurst != 30 {
		t.Errorf("expected RateLimitBurst=30, got %d", cfg.RateLimitBurst)
	}
	if cfg.AnalyzerDailyBudget != 5000 {
		t.Errorf("expected AnalyzerDailyBudget=5000, got %d", cfg.AnalyzerDailyBudget)
	}
}

func TestLoadConfig_CustomAbuseGuardValues(t *testing.T) {
	_ = os.Setenv("RATE_LIMIT_RPS", "20")
	_ = os.Setenv("RATE_LIMIT_BURST", "50")
	_ = os.Setenv("ANALYZER_DAILY_BUDGET", "10000")
	defer func() {
		_ = os.Unsetenv("RATE_LIMIT_RPS")
		_ = os.Unsetenv("RATE_LIMIT_BURST")
		_ = os.Unsetenv("ANALYZER_DAILY_BUDGET")
	}()

	cfg := config.LoadConfig()
	if cfg.RateLimitRPS != 20 {
		t.Errorf("expected RateLimitRPS=20, got %d", cfg.RateLimitRPS)
	}
	if cfg.RateLimitBurst != 50 {
		t.Errorf("expected RateLimitBurst=50, got %d", cfg.RateLimitBurst)
	}
	if cfg.AnalyzerDailyBudget != 10000 {
		t.Errorf("expected AnalyzerDailyBudget=10000, got %d", cfg.AnalyzerDailyBudget)
	}
}

func TestLoadConfig_VerdictProvenanceDefaults(t *testing.T) {
	_ = os.Unsetenv("VERDICT_STALE_TTL_SECONDS")
	_ = os.Unsetenv("FROZEN_FRAME_THRESHOLD")

	cfg := config.LoadConfig()
	if cfg.VerdictStaleTTLSeconds != 180 {
		t.Errorf("expected default VerdictStaleTTLSeconds=180, got %d", cfg.VerdictStaleTTLSeconds)
	}
	if cfg.FrozenFrameThreshold != 3 {
		t.Errorf("expected default FrozenFrameThreshold=3, got %d", cfg.FrozenFrameThreshold)
	}
}

func TestLoadConfig_CustomVerdictProvenanceValues(t *testing.T) {
	_ = os.Setenv("VERDICT_STALE_TTL_SECONDS", "300")
	_ = os.Setenv("FROZEN_FRAME_THRESHOLD", "5")
	defer func() {
		_ = os.Unsetenv("VERDICT_STALE_TTL_SECONDS")
		_ = os.Unsetenv("FROZEN_FRAME_THRESHOLD")
	}()

	cfg := config.LoadConfig()
	if cfg.VerdictStaleTTLSeconds != 300 {
		t.Errorf("expected VerdictStaleTTLSeconds=300, got %d", cfg.VerdictStaleTTLSeconds)
	}
	if cfg.FrozenFrameThreshold != 5 {
		t.Errorf("expected FrozenFrameThreshold=5, got %d", cfg.FrozenFrameThreshold)
	}
}

func TestLoadConfig_CloudVisionAPIKey(t *testing.T) {
	_ = os.Setenv("CLOUD_VISION_API_KEY", "test-gemini-key-123")
	_ = os.Setenv("ANALYZER", "vlm")
	defer func() {
		_ = os.Unsetenv("CLOUD_VISION_API_KEY")
		_ = os.Unsetenv("ANALYZER")
	}()

	cfg := config.LoadConfig()
	if cfg.CloudVisionAPIKey != "test-gemini-key-123" {
		t.Errorf("expected CloudVisionAPIKey='test-gemini-key-123', got %q", cfg.CloudVisionAPIKey)
	}
	if cfg.Analyzer != "vlm" {
		t.Errorf("expected Analyzer='vlm', got %q", cfg.Analyzer)
	}
}

func TestLoadConfig_GeminiAPIKey_Fallback(t *testing.T) {
	_ = os.Unsetenv("CLOUD_VISION_API_KEY")
	_ = os.Setenv("GEMINI_API_KEY", "fallback-gemini-key-456")
	defer func() {
		_ = os.Unsetenv("GEMINI_API_KEY")
	}()

	cfg := config.LoadConfig()
	if cfg.CloudVisionAPIKey != "fallback-gemini-key-456" {
		t.Errorf("expected fallback to GEMINI_API_KEY, got %q", cfg.CloudVisionAPIKey)
	}
}

func TestLoadConfig_GeminiModel_Default(t *testing.T) {
	_ = os.Unsetenv("GEMINI_MODEL")

	cfg := config.LoadConfig()
	if cfg.GeminiModel != "gemini-3.5-flash-lite" {
		t.Errorf("expected default GeminiModel='gemini-3.5-flash-lite', got %q", cfg.GeminiModel)
	}
}

func TestLoadConfig_GeminiModel_Custom(t *testing.T) {
	_ = os.Setenv("GEMINI_MODEL", "gemini-3.8-flash")
	defer func() {
		_ = os.Unsetenv("GEMINI_MODEL")
	}()

	cfg := config.LoadConfig()
	if cfg.GeminiModel != "gemini-3.8-flash" {
		t.Errorf("expected custom GeminiModel='gemini-3.8-flash', got %q", cfg.GeminiModel)
	}
}
