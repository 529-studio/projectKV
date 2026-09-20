package config

import (
	"bufio"
	"errors"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Port                    int
	SnapshotUpstreamBaseURL string
	SnapshotCacheTTLSeconds int
	HotsetIntervalSeconds   int
	HotsetMaxParallel       int
	HotsetActiveTTLSeconds  int
	TelegramBotToken        string
	CloudVisionAPIKey       string
	CVEngineURL             string
	SQLitePath              string
	DedupeCooldownMinutes   int
	LogLevel                string
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
		Port:                    3000,
		SnapshotCacheTTLSeconds: 10,
		HotsetIntervalSeconds:   30,
		HotsetMaxParallel:       10,
		HotsetActiveTTLSeconds:  120,
		CVEngineURL:             "http://localhost:8001",
		SQLitePath:              "./data/camera-view.db",
		DedupeCooldownMinutes:   15,
		LogLevel:                "info",
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

	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return cfg, nil
}
