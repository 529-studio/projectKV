package config

import (
	"bufio"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Port                          int
	SnapshotUpstreamBaseURL       string
	SnapshotCacheTTLSeconds       int
	HotsetIntervalSeconds         int
	HotsetMaxParallel             int
	HotsetActiveTTLSeconds        int
	TelegramBotToken              string
	CloudVisionAPIKey             string
	CVEngineURL                   string
	SQLitePath                    string
	DedupeCooldownMinutes         int
	LogLevel                      string
	StorageDriver                 string
	StorageLocalRoot              string
	R2AccountID                   string
	R2Endpoint                    string
	R2BucketName                  string
	R2Region                      string
	R2AccessKeyID                 string
	R2SecretAccessKey             string
	R2PublicURL                   string
	ArchiveSampleIntervalSeconds  int
	ArchiveRetentionDays          int
	ArchiveMaxDiskMB              int64
	ArchiveRetentionCheckInterval time.Duration
	VerdictStaleTTLSeconds        int
	FrozenFrameThreshold          int
	HexResolution                 int
	HexMinCameras                 int
	DevAccessToken                string
	HarvesterEnabled              bool
	HarvesterScheduleHours        string
	HarvesterRainCooldownMinutes  int
	HarvesterConcurrency          int
	HarvesterCatchupMinutes       int
}

// CleanEnv returns the environment variable value with surrounding whitespace and quotes (`"`, `'`) stripped.
func CleanEnv(key string) string {
	val := os.Getenv(key)
	return strings.Trim(strings.TrimSpace(val), `"'`)
}

// parseEnvInt reads an int environment variable, trimming quotes and whitespace.
// If unset or empty, it returns fallback. If parsing fails, it logs a structured warning and returns fallback.
// If min is specified and parsed value < min[0], it logs a structured warning and returns fallback.
func parseEnvInt(key string, fallback int, min ...int) int {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	clean := strings.Trim(strings.TrimSpace(val), `"'`)
	if clean == "" {
		return fallback
	}
	v, err := strconv.Atoi(clean)
	if err != nil {
		slog.Warn("config: invalid numeric format, using fallback default",
			"key", key,
			"raw_value", val,
			"default", fallback,
			"error", err,
		)
		return fallback
	}
	if len(min) > 0 && v < min[0] {
		slog.Warn("config: numeric value below minimum allowed, using fallback default",
			"key", key,
			"raw_value", val,
			"min", min[0],
			"default", fallback,
		)
		return fallback
	}
	return v
}

// parseEnvInt64 reads an int64 environment variable, trimming quotes and whitespace.
func parseEnvInt64(key string, fallback int64, min ...int64) int64 {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	clean := strings.Trim(strings.TrimSpace(val), `"'`)
	if clean == "" {
		return fallback
	}
	v, err := strconv.ParseInt(clean, 10, 64)
	if err != nil {
		slog.Warn("config: invalid numeric format, using fallback default",
			"key", key,
			"raw_value", val,
			"default", fallback,
			"error", err,
		)
		return fallback
	}
	if len(min) > 0 && v < min[0] {
		slog.Warn("config: numeric value below minimum allowed, using fallback default",
			"key", key,
			"raw_value", val,
			"min", min[0],
			"default", fallback,
		)
		return fallback
	}
	return v
}

// parseEnvFloat reads a float64 environment variable, trimming quotes and whitespace.
func parseEnvFloat(key string, fallback float64, min ...float64) float64 {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	clean := strings.Trim(strings.TrimSpace(val), `"'`)
	if clean == "" {
		return fallback
	}
	v, err := strconv.ParseFloat(clean, 64)
	if err != nil {
		slog.Warn("config: invalid float format, using fallback default",
			"key", key,
			"raw_value", val,
			"default", fallback,
			"error", err,
		)
		return fallback
	}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		slog.Warn("config: invalid float value (NaN or Inf), using fallback default",
			"key", key,
			"raw_value", val,
			"default", fallback,
		)
		return fallback
	}
	if len(min) > 0 && v < min[0] {
		slog.Warn("config: float value below minimum allowed, using fallback default",
			"key", key,
			"raw_value", val,
			"min", min[0],
			"default", fallback,
		)
		return fallback
	}
	return v
}

// parseEnvBool reads a boolean environment variable, trimming quotes and whitespace.
func parseEnvBool(key string, fallback bool) bool {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	clean := strings.Trim(strings.TrimSpace(val), `"'`)
	if clean == "" {
		return fallback
	}
	b, err := strconv.ParseBool(clean)
	if err != nil {
		slog.Warn("config: invalid boolean format, using fallback default",
			"key", key,
			"raw_value", val,
			"default", fallback,
			"error", err,
		)
		return fallback
	}
	return b
}

// parseEnvDuration reads a duration environment variable, trimming quotes and whitespace.
func parseEnvDuration(key string, fallback time.Duration) time.Duration {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	clean := strings.Trim(strings.TrimSpace(val), `"'`)
	if clean == "" {
		return fallback
	}
	d, err := time.ParseDuration(clean)
	if err != nil {
		slog.Warn("config: invalid duration format, using fallback default",
			"key", key,
			"raw_value", val,
			"default", fallback.String(),
			"error", err,
		)
		return fallback
	}
	return d
}

// parseEnvString reads a string environment variable, trimming quotes and whitespace.
func parseEnvString(key string, fallback string) string {
	clean := CleanEnv(key)
	if clean == "" {
		return fallback
	}
	return clean
}

func loadDotEnv() {
	paths := []string{".env", "../../.env", "../.env"}
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		defer f.Close()

		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				key := strings.TrimSpace(parts[0])
				val := strings.TrimSpace(parts[1])
				val = strings.Trim(val, `"'`)
				if _, exists := os.LookupEnv(key); !exists {
					_ = os.Setenv(key, val)
				}
			}
		}
		break
	}
}

func LoadFromEnv() (*Config, error) {
	loadDotEnv()

	var errs []error
	cfg := &Config{
		Port:                          parseEnvInt("PORT", 3000, 1),
		SnapshotCacheTTLSeconds:       parseEnvInt("SNAPSHOT_CACHE_TTL_SECONDS", 10, 1),
		HotsetIntervalSeconds:         parseEnvInt("HOTSET_INTERVAL_SECONDS", 30, 1),
		HotsetMaxParallel:             parseEnvInt("HOTSET_MAX_PARALLEL", 10, 1),
		HotsetActiveTTLSeconds:        parseEnvInt("HOTSET_ACTIVE_TTL_SECONDS", 120, 1),
		CVEngineURL:                   parseEnvString("CV_ENGINE_URL", "http://localhost:8001"),
		SQLitePath:                    parseEnvString("SQLITE_PATH", "./data/camera-view.db"),
		DedupeCooldownMinutes:         parseEnvInt("DEDUPE_COOLDOWN_MINUTES", 15, 1),
		LogLevel:                      parseEnvString("LOG_LEVEL", "info"),
		StorageDriver:                 parseEnvString("STORAGE_DRIVER", "local"),
		StorageLocalRoot:              parseEnvString("STORAGE_LOCAL_ROOT", "./data/proofs"),
		R2AccountID:                   parseEnvString("R2_ACCOUNT_ID", ""),
		R2Endpoint:                    parseEnvString("R2_ENDPOINT", ""),
		R2BucketName:                  parseEnvString("R2_BUCKET_NAME", "warded-proofs"),
		R2Region:                      parseEnvString("R2_REGION", "auto"),
		R2AccessKeyID:                 parseEnvString("R2_ACCESS_KEY_ID", ""),
		R2SecretAccessKey:             parseEnvString("R2_SECRET_ACCESS_KEY", ""),
		R2PublicURL:                   parseEnvString("R2_PUBLIC_URL", "https://file.529studio.site"),
		ArchiveSampleIntervalSeconds:  parseEnvInt("ARCHIVE_SAMPLE_INTERVAL_SECONDS", 300, 1),
		ArchiveRetentionDays:          parseEnvInt("ARCHIVE_RETENTION_DAYS", 14, 1),
		ArchiveMaxDiskMB:              parseEnvInt64("ARCHIVE_MAX_DISK_MB", 20000, 1),
		ArchiveRetentionCheckInterval: parseEnvDuration("ARCHIVE_RETENTION_CHECK_INTERVAL", 1*time.Hour),
		VerdictStaleTTLSeconds:        parseEnvInt("VERDICT_STALE_TTL_SECONDS", 180, 1),
		FrozenFrameThreshold:          parseEnvInt("FROZEN_FRAME_THRESHOLD", 3, 1),
		HexResolution:                 parseEnvInt("HEX_RESOLUTION", 7, 1),
		HexMinCameras:                 parseEnvInt("HEX_MIN_CAMERAS", 3, 1),
		HarvesterEnabled:              parseEnvBool("HARVESTER_ENABLED", false),
		HarvesterScheduleHours:        parseEnvString("HARVESTER_SCHEDULE_HOURS", "7,12,17"),
		HarvesterRainCooldownMinutes:  parseEnvInt("HARVESTER_RAIN_COOLDOWN_MINUTES", 60, 1),
		HarvesterConcurrency:          parseEnvInt("HARVESTER_CONCURRENCY", 15, 1),
		HarvesterCatchupMinutes:       parseEnvInt("HARVESTER_CATCHUP_MINUTES", 120, 0),
	}

	cfg.SnapshotUpstreamBaseURL = parseEnvString("SNAPSHOT_UPSTREAM_BASE_URL", "")
	if cfg.SnapshotUpstreamBaseURL == "" {
		errs = append(errs, errors.New("SNAPSHOT_UPSTREAM_BASE_URL is required"))
	}

	cfg.TelegramBotToken = parseEnvString("TELEGRAM_BOT_TOKEN", "")
	cfg.CloudVisionAPIKey = parseEnvString("CLOUD_VISION_API_KEY", "")
	if cfg.CloudVisionAPIKey == "" {
		cfg.CloudVisionAPIKey = parseEnvString("GEMINI_API_KEY", "")
	}

	if cfg.R2Endpoint == "" && cfg.R2AccountID != "" {
		cfg.R2Endpoint = fmt.Sprintf("https://%s.r2.cloudflarestorage.com", cfg.R2AccountID)
	}

	cfg.DevAccessToken = parseEnvString("DEV_ACCESS_TOKEN", "")
	if cfg.DevAccessToken == "" {
		cfg.DevAccessToken = parseEnvString("LABELER_SECRET_TOKEN", "")
	}

	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return cfg, nil
}

// ServerConfig represents runtime configuration for the snapshot/web server.
type ServerConfig struct {
	Port                          string
	CamerasFilePath               string
	StaticWebDir                  string
	SQLitePath                    string
	TileCacheDir                  string
	CacheTTL                      time.Duration
	CacheEvictInterval            time.Duration
	CacheMaxAge                   time.Duration
	Analyzer                      string // "none" | "mock" | "vlm" | "onnx" (default: "none")
	CloudVisionAPIKey             string
	GeminiModel                   string // default: "gemini-3.5-flash-lite"
	CVEngineURL                   string // default: "http://localhost:8000" or from CV_ENGINE_URL
	AppEnv                        string // "dev" | "production" | "staging" (default: "dev" or read from APP_ENV)
	DevAccessToken                string // DEV_ACCESS_TOKEN or LABELER_SECRET_TOKEN
	TelegramBotToken              string // TELEGRAM_BOT_TOKEN
	TelegramDebounceMinutes       int    // default: 30 (TELEGRAM_DEBOUNCE_MINUTES)
	StorageDriver                 string // default: "local"
	StorageLocalRoot              string // default: "./data/proofs"
	R2AccountID                   string
	R2Endpoint                    string
	R2BucketName                  string
	R2Region                      string
	R2AccessKeyID                 string
	R2SecretAccessKey             string
	R2PublicURL                   string
	RateLimitRPS                  int           // default: 10
	RateLimitBurst                int           // default: 30
	AnalyzerDailyBudget           int           // default: 5000
	ArchiveSampleIntervalSeconds  int           // default: 300
	ArchiveRetentionDays          int           // default: 14
	ArchiveMaxDiskMB              int64         // default: 20000
	ArchiveRetentionCheckInterval time.Duration // default: 1h
	VerdictStaleTTLSeconds        int           // default: 180
	FrozenFrameThreshold          int           // default: 3
	HotsetIntervalSeconds         int           // default: 30 (mock) or 60 (vlm)
	HotsetMaxParallel             int           // default: 10 (mock) or 1 (vlm)
	HotsetFixedMaxCameras         int           // default: 40 (mock) or 2 (vlm)
	HexResolution                 int           // default: 7 (HEX_RESOLUTION)
	HexMinCameras                 int           // default: 3 (HEX_MIN_CAMERAS)
	HexPolygonFilePath            string        // default: resolvePath("h3_hex_polygons.json")
	PCUWeightMotorcycle           float64       // default: 0.3 (PCU_WEIGHT_MOTORCYCLE)
	PCUWeightCar                  float64       // default: 1.0 (PCU_WEIGHT_CAR)
	PCUWeightBus                  float64       // default: 2.5 (PCU_WEIGHT_BUS)
	PCUWeightTruck                float64       // default: 2.0 (PCU_WEIGHT_TRUCK)
	PCUWeightContainer            float64       // default: 2.0 (PCU_WEIGHT_CONTAINER, folded into Truck per ADR-005 T6)
	PCUCapacityFloor              float64       // default: 15.0 (PCU_CAPACITY_FLOOR)
	PCUThresholdModerate          float64       // default: 0.30 (PCU_THRESHOLD_MODERATE)
	PCUThresholdHeavy             float64       // default: 0.60 (PCU_THRESHOLD_HEAVY)
	PCUThresholdCongested         float64       // default: 0.80 (PCU_THRESHOLD_CONGESTED)
	HarvesterEnabled              bool          // default: false (HARVESTER_ENABLED)
	HarvesterScheduleHours        string        // default: "7,12,17" (HARVESTER_SCHEDULE_HOURS)
	HarvesterRainCooldownMinutes  int           // default: 60 (HARVESTER_RAIN_COOLDOWN_MINUTES)
	HarvesterConcurrency          int           // default: 15 (HARVESTER_CONCURRENCY)
	HarvesterCatchupMinutes       int           // default: 120 (HARVESTER_CATCHUP_MINUTES; 0 disables boot-time catch-up)
}

// ResolvePath checks common relative paths to support running either from root or from services/go-server.
func ResolvePath(rel string) string {
	p1 := filepath.Clean(rel)
	if _, err := os.Stat(p1); err == nil {
		return p1
	}

	p2 := filepath.Join("../..", rel)
	if _, err := os.Stat(p2); err == nil {
		return p2
	}

	p3 := filepath.Join("..", rel)
	if _, err := os.Stat(p3); err == nil {
		return p3
	}

	return rel
}

func resolvePath(rel string) string {
	return ResolvePath(rel)
}

// LoadConfig reads configuration for the snapshot server from environment variables with sensible defaults.
func LoadConfig() *ServerConfig {
	loadDotEnv()

	port := parseEnvString("PORT", "3000")

	camerasPath := CleanEnv("CAMERAS_FILE_PATH")
	if camerasPath == "" || filepath.Base(camerasPath) == "cameras_all.json" {
		// Prefer enriched catalog if available (ADR-003 Decision 3), otherwise fall back to base catalog
		enrichedPath := resolvePath("cameras_enriched.json")
		if fi, err := os.Stat(enrichedPath); err == nil && !fi.IsDir() {
			camerasPath = enrichedPath
		} else if camerasPath == "" {
			camerasPath = resolvePath("cameras_all.json")
		}
	}

	staticDir := parseEnvString("STATIC_WEB_DIR", resolvePath("apps/web"))
	sqlitePath := parseEnvString("SQLITE_PATH", resolvePath("data/camera-view.db"))
	tileCacheDir := parseEnvString("TILE_CACHE_DIR", resolvePath("data/tiles"))

	analyzer := strings.ToLower(parseEnvString("ANALYZER", "none"))

	cloudVisionAPIKey := parseEnvString("CLOUD_VISION_API_KEY", "")
	if cloudVisionAPIKey == "" {
		cloudVisionAPIKey = parseEnvString("GEMINI_API_KEY", "")
	}

	geminiModel := parseEnvString("GEMINI_MODEL", "gemini-3.5-flash-lite")
	cvEngineURL := parseEnvString("CV_ENGINE_URL", "")

	appEnv := strings.ToLower(parseEnvString("APP_ENV", ""))
	if appEnv == "" {
		if CleanEnv("GIN_MODE") == "release" {
			appEnv = "production"
		} else {
			appEnv = "dev"
		}
	}

	devAccessToken := parseEnvString("DEV_ACCESS_TOKEN", "")
	if devAccessToken == "" {
		devAccessToken = parseEnvString("LABELER_SECRET_TOKEN", "")
	}

	telegramBotToken := parseEnvString("TELEGRAM_BOT_TOKEN", "")
	telegramDebounceMinutes := parseEnvInt("TELEGRAM_DEBOUNCE_MINUTES", 30, 1)

	storageDriver := strings.ToLower(parseEnvString("STORAGE_DRIVER", "local"))
	storageLocalRoot := parseEnvString("STORAGE_LOCAL_ROOT", resolvePath("data/proofs"))

	r2AccountID := parseEnvString("R2_ACCOUNT_ID", "")
	r2Endpoint := parseEnvString("R2_ENDPOINT", "")
	if r2Endpoint == "" && r2AccountID != "" {
		r2Endpoint = fmt.Sprintf("https://%s.r2.cloudflarestorage.com", r2AccountID)
	}

	r2BucketName := parseEnvString("R2_BUCKET_NAME", "warded-proofs")
	r2Region := parseEnvString("R2_REGION", "auto")
	r2AccessKeyID := parseEnvString("R2_ACCESS_KEY_ID", "")
	r2SecretAccessKey := parseEnvString("R2_SECRET_ACCESS_KEY", "")
	r2PublicURL := parseEnvString("R2_PUBLIC_URL", "https://file.529studio.site")

	sampleInterval := parseEnvInt("ARCHIVE_SAMPLE_INTERVAL_SECONDS", 300, 1)
	retentionDays := parseEnvInt("ARCHIVE_RETENTION_DAYS", 14, 1)
	maxDiskMB := parseEnvInt64("ARCHIVE_MAX_DISK_MB", 20000, 1)
	retentionCheckInterval := parseEnvDuration("ARCHIVE_RETENTION_CHECK_INTERVAL", 1*time.Hour)

	rateLimitRPS := parseEnvInt("RATE_LIMIT_RPS", 10, 1)
	rateLimitBurst := parseEnvInt("RATE_LIMIT_BURST", 30, 1)
	analyzerDailyBudget := parseEnvInt("ANALYZER_DAILY_BUDGET", 5000, 1)
	verdictStaleTTLSeconds := parseEnvInt("VERDICT_STALE_TTL_SECONDS", 180, 1)
	frozenFrameThreshold := parseEnvInt("FROZEN_FRAME_THRESHOLD", 3, 1)

	hotsetIntervalSeconds := 30
	if analyzer == "vlm" {
		hotsetIntervalSeconds = 60 // safe default for free tier
	}
	// onnx: 30s cycle matches S1 Option 1b capacity arithmetic (25.1s used, 16% headroom).
	hotsetIntervalSeconds = parseEnvInt("HOTSET_INTERVAL_SECONDS", hotsetIntervalSeconds, 1)

	hotsetMaxParallel := 10
	if analyzer == "vlm" {
		hotsetMaxParallel = 1 // 1 worker avoids concurrent burst calls against 15 RPM limit
	}
	hotsetMaxParallel = parseEnvInt("HOTSET_MAX_PARALLEL", hotsetMaxParallel, 1)

	hotsetFixedMaxCameras := 40
	if analyzer == "vlm" {
		hotsetFixedMaxCameras = 2 // 2 choke points prevents burning through 500 RPD daily quota
	} else if analyzer == "onnx" {
		hotsetFixedMaxCameras = 57 // S1 Option 1b: 4 priority corridors (DBP + CH + MCT + VVK = 57 cameras)
	} else if analyzer == "none" || analyzer == "" {
		hotsetFixedMaxCameras = 0
	}
	hotsetFixedMaxCameras = parseEnvInt("HOTSET_FIXED_MAX_CAMERAS", hotsetFixedMaxCameras, 0)

	hexResolution := parseEnvInt("HEX_RESOLUTION", 7, 1)
	hexMinCameras := parseEnvInt("HEX_MIN_CAMERAS", 3, 1)
	hexPolygonFilePath := parseEnvString("HEX_POLYGON_FILE_PATH", resolvePath("h3_hex_polygons.json"))

	pcuWeightMotorcycle := parseEnvFloat("PCU_WEIGHT_MOTORCYCLE", 0.3, 0.0)
	pcuWeightCar := parseEnvFloat("PCU_WEIGHT_CAR", 1.0, 0.0)
	pcuWeightBus := parseEnvFloat("PCU_WEIGHT_BUS", 2.5, 0.0)
	pcuWeightTruck := parseEnvFloat("PCU_WEIGHT_TRUCK", 2.0, 0.0)
	pcuWeightContainer := parseEnvFloat("PCU_WEIGHT_CONTAINER", pcuWeightTruck, 0.0)

	pcuCapacityFloor := parseEnvFloat("PCU_CAPACITY_FLOOR", 15.0, 0.0001)
	pcuThresholdModerate := parseEnvFloat("PCU_THRESHOLD_MODERATE", 0.30, 0.0001)
	pcuThresholdHeavy := parseEnvFloat("PCU_THRESHOLD_HEAVY", 0.60, 0.0001)
	pcuThresholdCongested := parseEnvFloat("PCU_THRESHOLD_CONGESTED", 0.80, 0.0001)

	harvesterEnabled := parseEnvBool("HARVESTER_ENABLED", false)
	harvesterScheduleHours := parseEnvString("HARVESTER_SCHEDULE_HOURS", "7,12,17")
	harvesterRainCooldownMinutes := parseEnvInt("HARVESTER_RAIN_COOLDOWN_MINUTES", 60, 1)
	harvesterConcurrency := parseEnvInt("HARVESTER_CONCURRENCY", 15, 1)
	harvesterCatchupMinutes := parseEnvInt("HARVESTER_CATCHUP_MINUTES", 120, 0)

	return &ServerConfig{
		Port:                          port,
		CamerasFilePath:               camerasPath,
		StaticWebDir:                  staticDir,
		SQLitePath:                    sqlitePath,
		TileCacheDir:                  tileCacheDir,
		CacheTTL:                      10 * time.Second,
		CacheEvictInterval:            60 * time.Second,
		CacheMaxAge:                   15 * time.Second,
		Analyzer:                      analyzer,
		CloudVisionAPIKey:             cloudVisionAPIKey,
		GeminiModel:                   geminiModel,
		CVEngineURL:                   cvEngineURL,
		AppEnv:                        appEnv,
		DevAccessToken:                devAccessToken,
		TelegramBotToken:              telegramBotToken,
		TelegramDebounceMinutes:       telegramDebounceMinutes,
		StorageDriver:                 storageDriver,
		StorageLocalRoot:              storageLocalRoot,
		R2AccountID:                   r2AccountID,
		R2Endpoint:                    r2Endpoint,
		R2BucketName:                  r2BucketName,
		R2Region:                      r2Region,
		R2AccessKeyID:                 r2AccessKeyID,
		R2SecretAccessKey:             r2SecretAccessKey,
		R2PublicURL:                   r2PublicURL,
		RateLimitRPS:                  rateLimitRPS,
		RateLimitBurst:                rateLimitBurst,
		AnalyzerDailyBudget:           analyzerDailyBudget,
		ArchiveSampleIntervalSeconds:  sampleInterval,
		ArchiveRetentionDays:          retentionDays,
		ArchiveMaxDiskMB:              maxDiskMB,
		ArchiveRetentionCheckInterval: retentionCheckInterval,
		VerdictStaleTTLSeconds:        verdictStaleTTLSeconds,
		FrozenFrameThreshold:          frozenFrameThreshold,
		HotsetIntervalSeconds:         hotsetIntervalSeconds,
		HotsetMaxParallel:             hotsetMaxParallel,
		HotsetFixedMaxCameras:         hotsetFixedMaxCameras,
		HexResolution:                 hexResolution,
		HexMinCameras:                 hexMinCameras,
		HexPolygonFilePath:            hexPolygonFilePath,
		PCUWeightMotorcycle:           pcuWeightMotorcycle,
		PCUWeightCar:                  pcuWeightCar,
		PCUWeightBus:                  pcuWeightBus,
		PCUWeightTruck:                pcuWeightTruck,
		PCUWeightContainer:            pcuWeightContainer,
		PCUCapacityFloor:              pcuCapacityFloor,
		PCUThresholdModerate:          pcuThresholdModerate,
		PCUThresholdHeavy:             pcuThresholdHeavy,
		PCUThresholdCongested:         pcuThresholdCongested,
		HarvesterEnabled:              harvesterEnabled,
		HarvesterScheduleHours:        harvesterScheduleHours,
		HarvesterRainCooldownMinutes:  harvesterRainCooldownMinutes,
		HarvesterConcurrency:          harvesterConcurrency,
		HarvesterCatchupMinutes:       harvesterCatchupMinutes,
	}
}
