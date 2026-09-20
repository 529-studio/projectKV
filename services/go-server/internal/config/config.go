package config

import (
	"os"
	"path/filepath"
	"time"

	"github.com/joho/godotenv"
)

// Config represents runtime configuration for the Go backend server.
type Config struct {
	Port               string
	CamerasFilePath    string
	StaticWebDir       string
	CacheTTL           time.Duration
	CacheEvictInterval time.Duration
	CacheMaxAge        time.Duration
}

// resolvePath checks common relative paths to support running either from root or from services/go-server.
func resolvePath(rel string) string {
	// 1. Try relative to current working directory
	p1 := filepath.Clean(rel)
	if _, err := os.Stat(p1); err == nil {
		return p1
	}

	// 2. Try relative to parent directory (if run from services/go-server)
	p2 := filepath.Join("../..", rel)
	if _, err := os.Stat(p2); err == nil {
		return p2
	}

	// 3. Try one level up
	p3 := filepath.Join("..", rel)
	if _, err := os.Stat(p3); err == nil {
		return p3
	}

	return rel
}

// LoadConfig reads configuration from environment variables with sensible defaults.
func LoadConfig() *Config {
	// Attempt to load .env from repository root or current directory
	envPaths := []string{".env", "../../.env", "../.env"}
	for _, envPath := range envPaths {
		if _, err := os.Stat(envPath); err == nil {
			_ = godotenv.Load(envPath)
			break
		}
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}

	camerasPath := os.Getenv("CAMERAS_FILE_PATH")
	if camerasPath == "" {
		camerasPath = resolvePath("cameras_all.json")
	}

	staticDir := os.Getenv("STATIC_WEB_DIR")
	if staticDir == "" {
		staticDir = resolvePath("apps/web")
	}

	return &Config{
		Port:               port,
		CamerasFilePath:    camerasPath,
		StaticWebDir:       staticDir,
		CacheTTL:           10 * time.Second,
		CacheEvictInterval: 60 * time.Second,
		CacheMaxAge:        15 * time.Second,
	}
}
