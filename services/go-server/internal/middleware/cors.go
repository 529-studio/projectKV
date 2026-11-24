package middleware

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

// DefaultAllowedOrigins defines the default whitelisted origins for cross-origin API requests in dev/test.
var DefaultAllowedOrigins = []string{
	"http://localhost:3000",
	"http://localhost:3001",
	"http://127.0.0.1:3000",
	"http://127.0.0.1:3001",
	"https://warded.529studio.site",
	"https://warded-stg.529studio.site",
}

// CORSConfig configures CORS behavior and environment gating.
type CORSConfig struct {
	AllowedOrigins []string
	AppEnv         string
}

// ValidateCORSOrigins validates that allowed origins satisfy production security constraints.
func ValidateCORSOrigins(origins []string, appEnv string) error {
	if appEnv != "production" {
		return nil
	}

	if len(origins) == 0 {
		return errors.New("production requires explicit CORS_ALLOWED_ORIGINS; cannot be empty")
	}

	for _, o := range origins {
		trimmed := strings.TrimSpace(o)
		if trimmed == "" {
			continue
		}
		if trimmed == "*" {
			return errors.New("production CORS rejects wildcard '*' origin")
		}
		u, err := url.Parse(trimmed)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return fmt.Errorf("invalid CORS origin format %q: must include scheme (http/https) and host", o)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return fmt.Errorf("invalid CORS origin scheme %q: must be http or https", u.Scheme)
		}
		hostname := strings.ToLower(u.Hostname())
		if hostname == "localhost" || hostname == "127.0.0.1" || hostname == "::1" || hostname == "0.0.0.0" {
			return fmt.Errorf("production CORS rejects loopback/localhost origin %q", o)
		}
	}
	return nil
}

// CORSMiddlewareWithConfig enables CORS for authorized API consumers with environment-aware gating.
func CORSMiddlewareWithConfig(cfg CORSConfig) gin.HandlerFunc {
	origins := cfg.AllowedOrigins
	if len(origins) == 0 && cfg.AppEnv != "production" {
		origins = DefaultAllowedOrigins
	}

	allowedSet := make(map[string]bool, len(origins))
	for _, o := range origins {
		trimmed := strings.TrimRight(strings.TrimSpace(o), "/")
		if trimmed != "" {
			allowedSet[trimmed] = true
		}
	}

	isProduction := cfg.AppEnv == "production"

	return func(c *gin.Context) {
		origin := strings.TrimRight(strings.TrimSpace(c.Request.Header.Get("Origin")), "/")
		isAllowed := origin != "" && allowedSet[origin]

		if origin != "" {
			if isAllowed {
				c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
				c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
				c.Writer.Header().Set("Vary", "Origin")
			}
		} else if !isProduction {
			c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		}

		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, HEAD, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With, X-Dev-Token, Cf-Access-Jwt-Assertion, Cf-Access-Authenticated-User-Email, CF-Access-Client-Id, CF-Access-Client-Secret")
		c.Writer.Header().Set("Access-Control-Expose-Headers", "Content-Length, Content-Type, X-Cache-Status, Cache-Control, X-OSD-Timestamp, X-OSD-Raw, X-Camera-Status")

		if c.Request.Method == http.MethodOptions {
			if origin != "" && !isAllowed {
				c.AbortWithStatus(http.StatusForbidden)
				return
			}
			c.AbortWithStatus(http.StatusOK)
			return
		}

		c.Next()
	}
}

// CORSMiddleware enables CORS for authorized API consumers while protecting credentials from unauthorized origins.
// If allowedOrigins is empty, DefaultAllowedOrigins is used as fallback.
func CORSMiddleware(allowedOrigins ...string) gin.HandlerFunc {
	return CORSMiddlewareWithConfig(CORSConfig{
		AllowedOrigins: allowedOrigins,
		AppEnv:         "dev",
	})
}
