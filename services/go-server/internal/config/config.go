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
	Analyzer                      string        // "none" | "mock" (default: "none")
	AppEnv                        string        // "dev" | "production" | "staging" (default: "dev" or read from APP_ENV)
	StorageDriver                 string        // default: "local"
	StorageLocalRoot              string        // default: "./data/proofs"
	ArchiveSampleIntervalSeconds  int           // default: 300
	ArchiveRetentionDays          int           // default: 14
	ArchiveMaxDiskMB              int64         // default: 20000
	ArchiveRetentionCheckInterval time.Duration // default: 1h
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
		AppEnv:                        appEnv,
		StorageDriver:                 storageDriver,
		StorageLocalRoot:              storageLocalRoot,
		ArchiveSampleIntervalSeconds:  sampleInterval,
		ArchiveRetentionDays:          retentionDays,
		ArchiveMaxDiskMB:              maxDiskMB,
		ArchiveRetentionCheckInterval: retentionCheckInterval,
	}
}
