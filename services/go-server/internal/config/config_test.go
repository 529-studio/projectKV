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

func TestLoadConfig_HotSet_Defaults_Mock(t *testing.T) {
	_ = os.Setenv("ANALYZER", "mock")
	_ = os.Unsetenv("HOTSET_INTERVAL_SECONDS")
	_ = os.Unsetenv("HOTSET_MAX_PARALLEL")
	_ = os.Unsetenv("HOTSET_FIXED_MAX_CAMERAS")
	defer func() {
		_ = os.Unsetenv("ANALYZER")
	}()

	cfg := config.LoadConfig()
	if cfg.HotsetIntervalSeconds != 30 {
		t.Errorf("expected HotsetIntervalSeconds=30 for mock, got %d", cfg.HotsetIntervalSeconds)
	}
	if cfg.HotsetMaxParallel != 10 {
		t.Errorf("expected HotsetMaxParallel=10 for mock, got %d", cfg.HotsetMaxParallel)
	}
	if cfg.HotsetFixedMaxCameras != 40 {
		t.Errorf("expected HotsetFixedMaxCameras=40 for mock, got %d", cfg.HotsetFixedMaxCameras)
	}
}

func TestLoadConfig_HotSet_Defaults_VLM(t *testing.T) {
	_ = os.Setenv("ANALYZER", "vlm")
	_ = os.Unsetenv("HOTSET_INTERVAL_SECONDS")
	_ = os.Unsetenv("HOTSET_MAX_PARALLEL")
	_ = os.Unsetenv("HOTSET_FIXED_MAX_CAMERAS")
	defer func() {
		_ = os.Unsetenv("ANALYZER")
	}()

	cfg := config.LoadConfig()
	if cfg.HotsetIntervalSeconds != 60 {
		t.Errorf("expected HotsetIntervalSeconds=60 for vlm, got %d", cfg.HotsetIntervalSeconds)
	}
	if cfg.HotsetMaxParallel != 1 {
		t.Errorf("expected HotsetMaxParallel=1 for vlm, got %d", cfg.HotsetMaxParallel)
	}
	if cfg.HotsetFixedMaxCameras != 2 {
		t.Errorf("expected HotsetFixedMaxCameras=2 for vlm, got %d", cfg.HotsetFixedMaxCameras)
	}
}

func TestLoadConfig_HotSet_Custom(t *testing.T) {
	_ = os.Setenv("HOTSET_INTERVAL_SECONDS", "45")
	_ = os.Setenv("HOTSET_MAX_PARALLEL", "4")
	_ = os.Setenv("HOTSET_FIXED_MAX_CAMERAS", "5")
	defer func() {
		_ = os.Unsetenv("HOTSET_INTERVAL_SECONDS")
		_ = os.Unsetenv("HOTSET_MAX_PARALLEL")
		_ = os.Unsetenv("HOTSET_FIXED_MAX_CAMERAS")
	}()

	cfg := config.LoadConfig()
	if cfg.HotsetIntervalSeconds != 45 {
		t.Errorf("expected HotsetIntervalSeconds=45, got %d", cfg.HotsetIntervalSeconds)
	}
	if cfg.HotsetMaxParallel != 4 {
		t.Errorf("expected HotsetMaxParallel=4, got %d", cfg.HotsetMaxParallel)
	}
	if cfg.HotsetFixedMaxCameras != 5 {
		t.Errorf("expected HotsetFixedMaxCameras=5, got %d", cfg.HotsetFixedMaxCameras)
	}
}

func TestLoadConfig_Hex_Defaults(t *testing.T) {
	_ = os.Unsetenv("HEX_RESOLUTION")
	_ = os.Unsetenv("HEX_MIN_CAMERAS")
	_ = os.Unsetenv("HEX_POLYGON_FILE_PATH")

	cfg := config.LoadConfig()
	if cfg.HexResolution != 7 {
		t.Errorf("expected default HexResolution=7, got %d", cfg.HexResolution)
	}
	if cfg.HexMinCameras != 3 {
		t.Errorf("expected default HexMinCameras=3, got %d", cfg.HexMinCameras)
	}
	if cfg.HexPolygonFilePath == "" {
		t.Errorf("expected default HexPolygonFilePath not empty, got %q", cfg.HexPolygonFilePath)
	}
}

func TestLoadConfig_Hex_Custom(t *testing.T) {
	_ = os.Setenv("HEX_RESOLUTION", "9")
	_ = os.Setenv("HEX_MIN_CAMERAS", "5")
	_ = os.Setenv("HEX_POLYGON_FILE_PATH", "/tmp/custom_hex.json")
	defer func() {
		_ = os.Unsetenv("HEX_RESOLUTION")
		_ = os.Unsetenv("HEX_MIN_CAMERAS")
		_ = os.Unsetenv("HEX_POLYGON_FILE_PATH")
	}()

	cfg := config.LoadConfig()
	if cfg.HexResolution != 9 {
		t.Errorf("expected HexResolution=9, got %d", cfg.HexResolution)
	}
	if cfg.HexMinCameras != 5 {
		t.Errorf("expected HexMinCameras=5, got %d", cfg.HexMinCameras)
	}
	if cfg.HexPolygonFilePath != "/tmp/custom_hex.json" {
		t.Errorf("expected HexPolygonFilePath=/tmp/custom_hex.json, got %q", cfg.HexPolygonFilePath)
	}
}

func TestLoadFromEnv_Hex_DefaultsAndCustom(t *testing.T) {
	_ = os.Setenv("SNAPSHOT_UPSTREAM_BASE_URL", "http://example.com")
	_ = os.Unsetenv("HEX_RESOLUTION")
	_ = os.Unsetenv("HEX_MIN_CAMERAS")
	defer func() {
		_ = os.Unsetenv("SNAPSHOT_UPSTREAM_BASE_URL")
	}()

	cfg, err := config.LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.HexResolution != 7 {
		t.Errorf("expected default HexResolution=7, got %d", cfg.HexResolution)
	}
	if cfg.HexMinCameras != 3 {
		t.Errorf("expected default HexMinCameras=3, got %d", cfg.HexMinCameras)
	}

	_ = os.Setenv("HEX_RESOLUTION", "9")
	_ = os.Setenv("HEX_MIN_CAMERAS", "4")
	defer func() {
		_ = os.Unsetenv("HEX_RESOLUTION")
		_ = os.Unsetenv("HEX_MIN_CAMERAS")
	}()

	cfg2, err := config.LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg2.HexResolution != 9 {
		t.Errorf("expected HexResolution=9, got %d", cfg2.HexResolution)
	}
	if cfg2.HexMinCameras != 4 {
		t.Errorf("expected HexMinCameras=4, got %d", cfg2.HexMinCameras)
	}
}
