package config

import (
	"bufio"
	"errors"
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
	ArchiveSampleIntervalSeconds  int
	ArchiveRetentionDays          int
	ArchiveMaxDiskMB              int64
	ArchiveRetentionCheckInterval time.Duration
	VerdictStaleTTLSeconds        int
	FrozenFrameThreshold          int
	HexResolution                 int
	HexMinCameras                 int
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
		Port:                          3000,
		SnapshotCacheTTLSeconds:       10,
		HotsetIntervalSeconds:         30,
		HotsetMaxParallel:             10,
		HotsetActiveTTLSeconds:        120,
		CVEngineURL:                   "http://localhost:8001",
		SQLitePath:                    "./data/camera-view.db",
		DedupeCooldownMinutes:         15,
		LogLevel:                      "info",
		StorageDriver:                 "local",
		StorageLocalRoot:              "./data/proofs",
		ArchiveSampleIntervalSeconds:  300,
		ArchiveRetentionDays:          14,
		ArchiveMaxDiskMB:              20000,
		ArchiveRetentionCheckInterval: 1 * time.Hour,
		VerdictStaleTTLSeconds:        180,
		FrozenFrameThreshold:          3,
		HexResolution:                 7,
		HexMinCameras:                 3,
	}

	if portStr := os.Getenv("PORT"); portStr != "" {
		if p, err := strconv.Atoi(portStr); err == nil {
			cfg.Port = p
		}
	}

	cfg.SnapshotUpstreamBaseURL = os.Getenv("SNAPSHOT_UPSTREAM_BASE_URL")
	if cfg.SnapshotUpstreamBaseURL == "" {
		errs = append(errs, errors.New("SNAPSHOT_UPSTREAM_BASE_URL is required"))
	}

	if ttlStr := os.Getenv("SNAPSHOT_CACHE_TTL_SECONDS"); ttlStr != "" {
		if t, err := strconv.Atoi(ttlStr); err == nil {
			cfg.SnapshotCacheTTLSeconds = t
		}
	}

	if val := os.Getenv("HOTSET_INTERVAL_SECONDS"); val != "" {
		if v, err := strconv.Atoi(val); err == nil {
			cfg.HotsetIntervalSeconds = v
		}
	}

	if val := os.Getenv("HOTSET_MAX_PARALLEL"); val != "" {
		if v, err := strconv.Atoi(val); err == nil {
			cfg.HotsetMaxParallel = v
		}
	}

	if val := os.Getenv("HOTSET_ACTIVE_TTL_SECONDS"); val != "" {
		if v, err := strconv.Atoi(val); err == nil {
			cfg.HotsetActiveTTLSeconds = v
		}
	}

	cfg.TelegramBotToken = os.Getenv("TELEGRAM_BOT_TOKEN")
	cfg.CloudVisionAPIKey = os.Getenv("CLOUD_VISION_API_KEY")
	if cfg.CloudVisionAPIKey == "" {
		cfg.CloudVisionAPIKey = os.Getenv("GEMINI_API_KEY")
	}

	if val := os.Getenv("CV_ENGINE_URL"); val != "" {
		cfg.CVEngineURL = val
	}

	if val := os.Getenv("SQLITE_PATH"); val != "" {
		cfg.SQLitePath = val
	}

	if val := os.Getenv("DEDUPE_COOLDOWN_MINUTES"); val != "" {
		if v, err := strconv.Atoi(val); err == nil {
			cfg.DedupeCooldownMinutes = v
		}
	}

	if val := os.Getenv("LOG_LEVEL"); val != "" {
		cfg.LogLevel = val
	}

	if val := os.Getenv("STORAGE_DRIVER"); val != "" {
		cfg.StorageDriver = val
	}

	if val := os.Getenv("STORAGE_LOCAL_ROOT"); val != "" {
		cfg.StorageLocalRoot = val
	}

	if val := os.Getenv("ARCHIVE_SAMPLE_INTERVAL_SECONDS"); val != "" {
		if v, err := strconv.Atoi(val); err == nil {
			cfg.ArchiveSampleIntervalSeconds = v
		}
	}

	if val := os.Getenv("ARCHIVE_RETENTION_DAYS"); val != "" {
		if v, err := strconv.Atoi(val); err == nil {
			cfg.ArchiveRetentionDays = v
		}
	}

	if val := os.Getenv("ARCHIVE_MAX_DISK_MB"); val != "" {
		if v, err := strconv.ParseInt(val, 10, 64); err == nil {
			cfg.ArchiveMaxDiskMB = v
		}
	}

	if val := os.Getenv("ARCHIVE_RETENTION_CHECK_INTERVAL"); val != "" {
		if d, err := time.ParseDuration(val); err == nil {
			cfg.ArchiveRetentionCheckInterval = d
		}
	}

	if val := os.Getenv("VERDICT_STALE_TTL_SECONDS"); val != "" {
		if v, err := strconv.Atoi(val); err == nil {
			cfg.VerdictStaleTTLSeconds = v
		}
	}

	if val := os.Getenv("FROZEN_FRAME_THRESHOLD"); val != "" {
		if v, err := strconv.Atoi(val); err == nil {
			cfg.FrozenFrameThreshold = v
		}
	}

	if val := os.Getenv("HEX_RESOLUTION"); val != "" {
		if v, err := strconv.Atoi(val); err == nil && v > 0 {
			cfg.HexResolution = v
		}
	}

	if val := os.Getenv("HEX_MIN_CAMERAS"); val != "" {
		if v, err := strconv.Atoi(val); err == nil && v > 0 {
			cfg.HexMinCameras = v
		}
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
	GeminiModel                   string        // default: "gemini-3.5-flash-lite"
	CVEngineURL                   string        // default: "http://localhost:8000" or from CV_ENGINE_URL
	AppEnv                        string        // "dev" | "production" | "staging" (default: "dev" or read from APP_ENV)
	StorageDriver                 string        // default: "local"
	StorageLocalRoot              string        // default: "./data/proofs"
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
}

// resolvePath checks common relative paths to support running either from root or from services/go-server.
func resolvePath(rel string) string {
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

// LoadConfig reads configuration for the snapshot server from environment variables with sensible defaults.
func LoadConfig() *ServerConfig {
	loadDotEnv()

	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}

	camerasPath := os.Getenv("CAMERAS_FILE_PATH")
	if camerasPath == "" || filepath.Base(camerasPath) == "cameras_all.json" {
		// Prefer enriched catalog if available (ADR-003 Decision 3), otherwise fall back to base catalog
		enrichedPath := resolvePath("cameras_enriched.json")
		if fi, err := os.Stat(enrichedPath); err == nil && !fi.IsDir() {
			camerasPath = enrichedPath
		} else if camerasPath == "" {
			camerasPath = resolvePath("cameras_all.json")
		}
	}

	staticDir := os.Getenv("STATIC_WEB_DIR")
	if staticDir == "" {
		staticDir = resolvePath("apps/web")
	}

	sqlitePath := os.Getenv("SQLITE_PATH")
	if sqlitePath == "" {
		sqlitePath = resolvePath("data/camera-view.db")
	}

	tileCacheDir := os.Getenv("TILE_CACHE_DIR")
	if tileCacheDir == "" {
		tileCacheDir = resolvePath("data/tiles")
	}

	analyzer := strings.ToLower(strings.TrimSpace(os.Getenv("ANALYZER")))
	if analyzer == "" {
		analyzer = "none"
	}

	cloudVisionAPIKey := strings.TrimSpace(os.Getenv("CLOUD_VISION_API_KEY"))
	if cloudVisionAPIKey == "" {
		cloudVisionAPIKey = strings.TrimSpace(os.Getenv("GEMINI_API_KEY"))
	}

	geminiModel := strings.TrimSpace(os.Getenv("GEMINI_MODEL"))
	if geminiModel == "" {
		geminiModel = "gemini-3.5-flash-lite"
	}

	cvEngineURL := strings.TrimSpace(os.Getenv("CV_ENGINE_URL"))

	appEnv := strings.ToLower(strings.TrimSpace(os.Getenv("APP_ENV")))
	if appEnv == "" {
		if os.Getenv("GIN_MODE") == "release" {
			appEnv = "production"
		} else {
			appEnv = "dev"
		}
	}

	storageDriver := strings.ToLower(strings.TrimSpace(os.Getenv("STORAGE_DRIVER")))
	if storageDriver == "" {
		storageDriver = "local"
	}

	storageLocalRoot := os.Getenv("STORAGE_LOCAL_ROOT")
	if storageLocalRoot == "" {
		storageLocalRoot = resolvePath("data/proofs")
	}
	sampleInterval := 300
	if val := os.Getenv("ARCHIVE_SAMPLE_INTERVAL_SECONDS"); val != "" {
		if v, err := strconv.Atoi(val); err == nil {
			sampleInterval = v
		}
	}

	retentionDays := 14
	if val := os.Getenv("ARCHIVE_RETENTION_DAYS"); val != "" {
		if v, err := strconv.Atoi(val); err == nil {
			retentionDays = v
		}
	}

	maxDiskMB := int64(20000)
	if val := os.Getenv("ARCHIVE_MAX_DISK_MB"); val != "" {
		if v, err := strconv.ParseInt(val, 10, 64); err == nil {
			maxDiskMB = v
		}
	}

	retentionCheckInterval := 1 * time.Hour
	if val := os.Getenv("ARCHIVE_RETENTION_CHECK_INTERVAL"); val != "" {
		if d, err := time.ParseDuration(val); err == nil {
			retentionCheckInterval = d
		}
	}

	rateLimitRPS := 10
	if val := os.Getenv("RATE_LIMIT_RPS"); val != "" {
		if v, err := strconv.Atoi(val); err == nil {
			rateLimitRPS = v
		}
	}

	rateLimitBurst := 30
	if val := os.Getenv("RATE_LIMIT_BURST"); val != "" {
		if v, err := strconv.Atoi(val); err == nil {
			rateLimitBurst = v
		}
	}

	analyzerDailyBudget := 5000
	if val := os.Getenv("ANALYZER_DAILY_BUDGET"); val != "" {
		if v, err := strconv.Atoi(val); err == nil {
			analyzerDailyBudget = v
		}
	}

	verdictStaleTTLSeconds := 180
	if val := os.Getenv("VERDICT_STALE_TTL_SECONDS"); val != "" {
		if v, err := strconv.Atoi(val); err == nil {
			verdictStaleTTLSeconds = v
		}
	}

	frozenFrameThreshold := 3
	if val := os.Getenv("FROZEN_FRAME_THRESHOLD"); val != "" {
		if v, err := strconv.Atoi(val); err == nil {
			frozenFrameThreshold = v
		}
	}

	hotsetIntervalSeconds := 30
	if analyzer == "vlm" {
		hotsetIntervalSeconds = 60 // safe default for free tier
	}
	// onnx: 30s cycle matches S1 Option 1b capacity arithmetic (25.1s used, 16% headroom).
	if val := os.Getenv("HOTSET_INTERVAL_SECONDS"); val != "" {
		if v, err := strconv.Atoi(val); err == nil && v > 0 {
			hotsetIntervalSeconds = v
		}
	}

	hotsetMaxParallel := 10
	if analyzer == "vlm" {
		hotsetMaxParallel = 1 // 1 worker avoids concurrent burst calls against 15 RPM limit
	}
	if val := os.Getenv("HOTSET_MAX_PARALLEL"); val != "" {
		if v, err := strconv.Atoi(val); err == nil && v > 0 {
			hotsetMaxParallel = v
		}
	}

	hotsetFixedMaxCameras := 40
	if analyzer == "vlm" {
		hotsetFixedMaxCameras = 2 // 2 choke points prevents burning through 500 RPD daily quota
	} else if analyzer == "onnx" {
		hotsetFixedMaxCameras = 57 // S1 Option 1b: 4 priority corridors (DBP + CH + MCT + VVK = 57 cameras)
	} else if analyzer == "none" || analyzer == "" {
		hotsetFixedMaxCameras = 0
	}
	if val := os.Getenv("HOTSET_FIXED_MAX_CAMERAS"); val != "" {
		if v, err := strconv.Atoi(val); err == nil && v >= 0 {
			hotsetFixedMaxCameras = v
		}
	}

	hexResolution := 7
	if val := os.Getenv("HEX_RESOLUTION"); val != "" {
		if v, err := strconv.Atoi(val); err == nil && v > 0 {
			hexResolution = v
		}
	}

	hexMinCameras := 3
	if val := os.Getenv("HEX_MIN_CAMERAS"); val != "" {
		if v, err := strconv.Atoi(val); err == nil && v > 0 {
			hexMinCameras = v
		}
	}

	hexPolygonFilePath := os.Getenv("HEX_POLYGON_FILE_PATH")
	if hexPolygonFilePath == "" {
		hexPolygonFilePath = resolvePath("h3_hex_polygons.json")
	}

	pcuWeightMotorcycle := 0.3
	if val := os.Getenv("PCU_WEIGHT_MOTORCYCLE"); val != "" {
		if v, err := strconv.ParseFloat(val, 64); err == nil && v >= 0 {
			pcuWeightMotorcycle = v
		}
	}

	pcuWeightCar := 1.0
	if val := os.Getenv("PCU_WEIGHT_CAR"); val != "" {
		if v, err := strconv.ParseFloat(val, 64); err == nil && v >= 0 {
			pcuWeightCar = v
		}
	}

	pcuWeightBus := 2.5
	if val := os.Getenv("PCU_WEIGHT_BUS"); val != "" {
		if v, err := strconv.ParseFloat(val, 64); err == nil && v >= 0 {
			pcuWeightBus = v
		}
	}

	pcuWeightTruck := 2.0
	if val := os.Getenv("PCU_WEIGHT_TRUCK"); val != "" {
		if v, err := strconv.ParseFloat(val, 64); err == nil && v >= 0 {
			pcuWeightTruck = v
		}
	}

	// Default PCUWeightContainer matches PCUWeightTruck (2.0) per ADR-005 T6 / decision-container-class-gap.md.
	pcuWeightContainer := pcuWeightTruck
	if val := os.Getenv("PCU_WEIGHT_CONTAINER"); val != "" {
		if v, err := strconv.ParseFloat(val, 64); err == nil && v >= 0 {
			pcuWeightContainer = v
		}
	}

	pcuCapacityFloor := 15.0
	if val := os.Getenv("PCU_CAPACITY_FLOOR"); val != "" {
		if v, err := strconv.ParseFloat(val, 64); err == nil && v > 0 {
			pcuCapacityFloor = v
		}
	}

	pcuThresholdModerate := 0.30
	if val := os.Getenv("PCU_THRESHOLD_MODERATE"); val != "" {
		if v, err := strconv.ParseFloat(val, 64); err == nil && v > 0 {
			pcuThresholdModerate = v
		}
	}

	pcuThresholdHeavy := 0.60
	if val := os.Getenv("PCU_THRESHOLD_HEAVY"); val != "" {
		if v, err := strconv.ParseFloat(val, 64); err == nil && v > 0 {
			pcuThresholdHeavy = v
		}
	}

	pcuThresholdCongested := 0.80
	if val := os.Getenv("PCU_THRESHOLD_CONGESTED"); val != "" {
		if v, err := strconv.ParseFloat(val, 64); err == nil && v > 0 {
			pcuThresholdCongested = v
		}
	}

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
		StorageDriver:                 storageDriver,
		StorageLocalRoot:              storageLocalRoot,
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
	}
}
