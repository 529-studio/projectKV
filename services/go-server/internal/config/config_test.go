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

func TestLoadConfig_HotSet_Defaults_ONNX(t *testing.T) {
	_ = os.Setenv("ANALYZER", "onnx")
	_ = os.Setenv("CV_ENGINE_URL", "http://warded-model:8000")
	_ = os.Unsetenv("HOTSET_INTERVAL_SECONDS")
	_ = os.Unsetenv("HOTSET_MAX_PARALLEL")
	_ = os.Unsetenv("HOTSET_FIXED_MAX_CAMERAS")
	defer func() {
		_ = os.Unsetenv("ANALYZER")
		_ = os.Unsetenv("CV_ENGINE_URL")
	}()

	cfg := config.LoadConfig()
	if cfg.HotsetIntervalSeconds != 30 {
		t.Errorf("expected HotsetIntervalSeconds=30 for onnx, got %d", cfg.HotsetIntervalSeconds)
	}
	if cfg.HotsetMaxParallel != 10 {
		t.Errorf("expected HotsetMaxParallel=10 for onnx, got %d", cfg.HotsetMaxParallel)
	}
	if cfg.HotsetFixedMaxCameras != 57 {
		t.Errorf("expected HotsetFixedMaxCameras=57 (S1 Option 1b) for onnx, got %d", cfg.HotsetFixedMaxCameras)
	}
	if cfg.CVEngineURL != "http://warded-model:8000" {
		t.Errorf("expected CVEngineURL='http://warded-model:8000', got %q", cfg.CVEngineURL)
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

func TestLoadConfig_PCU_Defaults(t *testing.T) {
	_ = os.Unsetenv("PCU_WEIGHT_MOTORCYCLE")
	_ = os.Unsetenv("PCU_WEIGHT_CAR")
	_ = os.Unsetenv("PCU_WEIGHT_BUS")
	_ = os.Unsetenv("PCU_WEIGHT_TRUCK")
	_ = os.Unsetenv("PCU_WEIGHT_CONTAINER")
	_ = os.Unsetenv("PCU_CAPACITY_FLOOR")
	_ = os.Unsetenv("PCU_THRESHOLD_MODERATE")
	_ = os.Unsetenv("PCU_THRESHOLD_HEAVY")
	_ = os.Unsetenv("PCU_THRESHOLD_CONGESTED")

	cfg := config.LoadConfig()
	if cfg.PCUWeightMotorcycle != 0.3 {
		t.Errorf("expected default PCUWeightMotorcycle=0.3, got %f", cfg.PCUWeightMotorcycle)
	}
	if cfg.PCUWeightCar != 1.0 {
		t.Errorf("expected default PCUWeightCar=1.0, got %f", cfg.PCUWeightCar)
	}
	if cfg.PCUWeightBus != 2.5 {
		t.Errorf("expected default PCUWeightBus=2.5, got %f", cfg.PCUWeightBus)
	}
	if cfg.PCUWeightTruck != 2.0 {
		t.Errorf("expected default PCUWeightTruck=2.0, got %f", cfg.PCUWeightTruck)
	}
	if cfg.PCUWeightContainer != 2.0 {
		t.Errorf("expected default PCUWeightContainer=2.0, got %f", cfg.PCUWeightContainer)
	}
	if cfg.PCUCapacityFloor != 15.0 {
		t.Errorf("expected default PCUCapacityFloor=15.0, got %f", cfg.PCUCapacityFloor)
	}
	if cfg.PCUThresholdModerate != 0.30 {
		t.Errorf("expected default PCUThresholdModerate=0.30, got %f", cfg.PCUThresholdModerate)
	}
	if cfg.PCUThresholdHeavy != 0.60 {
		t.Errorf("expected default PCUThresholdHeavy=0.60, got %f", cfg.PCUThresholdHeavy)
	}
	if cfg.PCUThresholdCongested != 0.80 {
		t.Errorf("expected default PCUThresholdCongested=0.80, got %f", cfg.PCUThresholdCongested)
	}
}

func TestLoadConfig_PCU_Custom(t *testing.T) {
	_ = os.Setenv("PCU_WEIGHT_MOTORCYCLE", "0.25")
	_ = os.Setenv("PCU_WEIGHT_CAR", "1.1")
	_ = os.Setenv("PCU_WEIGHT_BUS", "2.2")
	_ = os.Setenv("PCU_WEIGHT_TRUCK", "2.5")
	_ = os.Setenv("PCU_WEIGHT_CONTAINER", "3.5")
	_ = os.Setenv("PCU_CAPACITY_FLOOR", "30.0")
	_ = os.Setenv("PCU_THRESHOLD_MODERATE", "0.40")
	_ = os.Setenv("PCU_THRESHOLD_HEAVY", "0.75")
	_ = os.Setenv("PCU_THRESHOLD_CONGESTED", "0.95")
	defer func() {
		_ = os.Unsetenv("PCU_WEIGHT_MOTORCYCLE")
		_ = os.Unsetenv("PCU_WEIGHT_CAR")
		_ = os.Unsetenv("PCU_WEIGHT_BUS")
		_ = os.Unsetenv("PCU_WEIGHT_TRUCK")
		_ = os.Unsetenv("PCU_WEIGHT_CONTAINER")
		_ = os.Unsetenv("PCU_CAPACITY_FLOOR")
		_ = os.Unsetenv("PCU_THRESHOLD_MODERATE")
		_ = os.Unsetenv("PCU_THRESHOLD_HEAVY")
		_ = os.Unsetenv("PCU_THRESHOLD_CONGESTED")
	}()

	cfg := config.LoadConfig()
	if cfg.PCUWeightMotorcycle != 0.25 {
		t.Errorf("expected PCUWeightMotorcycle=0.25, got %f", cfg.PCUWeightMotorcycle)
	}
	if cfg.PCUWeightCar != 1.1 {
		t.Errorf("expected PCUWeightCar=1.1, got %f", cfg.PCUWeightCar)
	}
	if cfg.PCUWeightBus != 2.2 {
		t.Errorf("expected PCUWeightBus=2.2, got %f", cfg.PCUWeightBus)
	}
	if cfg.PCUWeightTruck != 2.5 {
		t.Errorf("expected PCUWeightTruck=2.5, got %f", cfg.PCUWeightTruck)
	}
	if cfg.PCUWeightContainer != 3.5 {
		t.Errorf("expected PCUWeightContainer=3.5, got %f", cfg.PCUWeightContainer)
	}
	if cfg.PCUCapacityFloor != 30.0 {
		t.Errorf("expected PCUCapacityFloor=30.0, got %f", cfg.PCUCapacityFloor)
	}
	if cfg.PCUThresholdModerate != 0.40 {
		t.Errorf("expected PCUThresholdModerate=0.40, got %f", cfg.PCUThresholdModerate)
	}
	if cfg.PCUThresholdHeavy != 0.75 {
		t.Errorf("expected PCUThresholdHeavy=0.75, got %f", cfg.PCUThresholdHeavy)
	}
	if cfg.PCUThresholdCongested != 0.95 {
		t.Errorf("expected PCUThresholdCongested=0.95, got %f", cfg.PCUThresholdCongested)
	}
}

func TestLoadConfig_R2_Defaults(t *testing.T) {
	_ = os.Unsetenv("STORAGE_DRIVER")
	_ = os.Unsetenv("R2_ACCOUNT_ID")
	_ = os.Unsetenv("R2_ENDPOINT")
	_ = os.Unsetenv("R2_BUCKET_NAME")
	_ = os.Unsetenv("R2_REGION")
	_ = os.Unsetenv("R2_ACCESS_KEY_ID")
	_ = os.Unsetenv("R2_SECRET_ACCESS_KEY")
	_ = os.Unsetenv("R2_PUBLIC_URL")

	cfg := config.LoadConfig()
	if cfg.StorageDriver != "local" {
		t.Errorf("expected default StorageDriver='local', got %q", cfg.StorageDriver)
	}
	if cfg.R2BucketName != "warded-proofs" {
		t.Errorf("expected default R2BucketName='warded-proofs', got %q", cfg.R2BucketName)
	}
	if cfg.R2Region != "auto" {
		t.Errorf("expected default R2Region='auto', got %q", cfg.R2Region)
	}
	if cfg.R2PublicURL != "https://file.529studio.site" {
		t.Errorf("expected default R2PublicURL='https://file.529studio.site', got %q", cfg.R2PublicURL)
	}
}

func TestLoadConfig_R2_Custom(t *testing.T) {
	_ = os.Setenv("STORAGE_DRIVER", "r2")
	_ = os.Setenv("R2_ACCOUNT_ID", "acc123")
	_ = os.Setenv("R2_ENDPOINT", "https://custom.r2.endpoint")
	_ = os.Setenv("R2_BUCKET_NAME", "my-bucket")
	_ = os.Setenv("R2_REGION", "apac")
	_ = os.Setenv("R2_ACCESS_KEY_ID", "key123")
	_ = os.Setenv("R2_SECRET_ACCESS_KEY", "sec123")
	_ = os.Setenv("R2_PUBLIC_URL", "https://cdn.example.com")
	defer func() {
		_ = os.Unsetenv("STORAGE_DRIVER")
		_ = os.Unsetenv("R2_ACCOUNT_ID")
		_ = os.Unsetenv("R2_ENDPOINT")
		_ = os.Unsetenv("R2_BUCKET_NAME")
		_ = os.Unsetenv("R2_REGION")
		_ = os.Unsetenv("R2_ACCESS_KEY_ID")
		_ = os.Unsetenv("R2_SECRET_ACCESS_KEY")
		_ = os.Unsetenv("R2_PUBLIC_URL")
	}()

	cfg := config.LoadConfig()
	if cfg.StorageDriver != "r2" {
		t.Errorf("expected StorageDriver='r2', got %q", cfg.StorageDriver)
	}
	if cfg.R2AccountID != "acc123" {
		t.Errorf("expected R2AccountID='acc123', got %q", cfg.R2AccountID)
	}
	if cfg.R2Endpoint != "https://custom.r2.endpoint" {
		t.Errorf("expected R2Endpoint='https://custom.r2.endpoint', got %q", cfg.R2Endpoint)
	}
	if cfg.R2BucketName != "my-bucket" {
		t.Errorf("expected R2BucketName='my-bucket', got %q", cfg.R2BucketName)
	}
	if cfg.R2Region != "apac" {
		t.Errorf("expected R2Region='apac', got %q", cfg.R2Region)
	}
	if cfg.R2AccessKeyID != "key123" {
		t.Errorf("expected R2AccessKeyID='key123', got %q", cfg.R2AccessKeyID)
	}
	if cfg.R2SecretAccessKey != "sec123" {
		t.Errorf("expected R2SecretAccessKey='sec123', got %q", cfg.R2SecretAccessKey)
	}
	if cfg.R2PublicURL != "https://cdn.example.com" {
		t.Errorf("expected R2PublicURL='https://cdn.example.com', got %q", cfg.R2PublicURL)
	}
}

func TestLoadConfig_DevAccessToken(t *testing.T) {
	_ = os.Setenv("DEV_ACCESS_TOKEN", "test-dev-token-999")
	defer func() {
		_ = os.Unsetenv("DEV_ACCESS_TOKEN")
	}()

	cfg := config.LoadConfig()
	if cfg.DevAccessToken != "test-dev-token-999" {
		t.Errorf("expected DevAccessToken='test-dev-token-999', got %q", cfg.DevAccessToken)
	}
}

func TestLoadConfig_LabelerSecretTokenFallback(t *testing.T) {
	_ = os.Unsetenv("DEV_ACCESS_TOKEN")
	_ = os.Setenv("LABELER_SECRET_TOKEN", "fallback-secret-888")
	defer func() {
		_ = os.Unsetenv("LABELER_SECRET_TOKEN")
	}()

	cfg := config.LoadConfig()
	if cfg.DevAccessToken != "fallback-secret-888" {
		t.Errorf("expected DevAccessToken='fallback-secret-888', got %q", cfg.DevAccessToken)
	}
}

func TestLoadConfig_HarvesterDefaults(t *testing.T) {
	_ = os.Unsetenv("HARVESTER_ENABLED")
	_ = os.Unsetenv("HARVESTER_SCHEDULE_HOURS")
	_ = os.Unsetenv("HARVESTER_RAIN_COOLDOWN_MINUTES")
	_ = os.Unsetenv("HARVESTER_CONCURRENCY")

	cfg := config.LoadConfig()
	if cfg.HarvesterEnabled != false {
		t.Errorf("expected HarvesterEnabled=false, got %v", cfg.HarvesterEnabled)
	}
	if cfg.HarvesterScheduleHours != "7,12,17" {
		t.Errorf("expected HarvesterScheduleHours='7,12,17', got %q", cfg.HarvesterScheduleHours)
	}
	if cfg.HarvesterRainCooldownMinutes != 60 {
		t.Errorf("expected HarvesterRainCooldownMinutes=60, got %d", cfg.HarvesterRainCooldownMinutes)
	}
	if cfg.HarvesterConcurrency != 15 {
		t.Errorf("expected HarvesterConcurrency=15, got %d", cfg.HarvesterConcurrency)
	}
}

func TestLoadConfig_HarvesterCustomValues(t *testing.T) {
	_ = os.Setenv("HARVESTER_ENABLED", "true")
	_ = os.Setenv("HARVESTER_SCHEDULE_HOURS", "8,13,18")
	_ = os.Setenv("HARVESTER_RAIN_COOLDOWN_MINUTES", "45")
	_ = os.Setenv("HARVESTER_CONCURRENCY", "10")
	defer func() {
		_ = os.Unsetenv("HARVESTER_ENABLED")
		_ = os.Unsetenv("HARVESTER_SCHEDULE_HOURS")
		_ = os.Unsetenv("HARVESTER_RAIN_COOLDOWN_MINUTES")
		_ = os.Unsetenv("HARVESTER_CONCURRENCY")
	}()

	cfg := config.LoadConfig()
	if cfg.HarvesterEnabled != true {
		t.Errorf("expected HarvesterEnabled=true, got %v", cfg.HarvesterEnabled)
	}
	if cfg.HarvesterScheduleHours != "8,13,18" {
		t.Errorf("expected HarvesterScheduleHours='8,13,18', got %q", cfg.HarvesterScheduleHours)
	}
	if cfg.HarvesterRainCooldownMinutes != 45 {
		t.Errorf("expected HarvesterRainCooldownMinutes=45, got %d", cfg.HarvesterRainCooldownMinutes)
	}
	if cfg.HarvesterConcurrency != 10 {
		t.Errorf("expected HarvesterConcurrency=10, got %d", cfg.HarvesterConcurrency)
	}
}
