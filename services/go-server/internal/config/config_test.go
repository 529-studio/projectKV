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
