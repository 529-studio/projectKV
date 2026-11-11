package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// DefaultAllowedOrigins defines the default whitelisted origins for cross-origin API requests.
var DefaultAllowedOrigins = []string{
	"http://localhost:3000",
	"http://localhost:3001",
	"http://127.0.0.1:3000",
	"http://127.0.0.1:3001",
	"https://warded.529studio.site",
	"https://warded-stg.529studio.site",
}

// CORSMiddleware enables CORS for authorized API consumers while protecting credentials from unauthorized origins.
// If allowedOrigins is empty, DefaultAllowedOrigins is used as fallback.
func CORSMiddleware(allowedOrigins ...string) gin.HandlerFunc {
	origins := allowedOrigins
	if len(origins) == 0 {
		origins = DefaultAllowedOrigins
	}

	allowedSet := make(map[string]bool, len(origins))
	for _, o := range origins {
		trimmed := strings.TrimRight(strings.TrimSpace(o), "/")
		if trimmed != "" {
			allowedSet[trimmed] = true
		}
	}

	return func(c *gin.Context) {
		origin := strings.TrimRight(strings.TrimSpace(c.Request.Header.Get("Origin")), "/")
		isAllowed := origin != "" && allowedSet[origin]

		if origin != "" {
			if isAllowed {
				c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
				c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
				c.Writer.Header().Set("Vary", "Origin")
			}
		} else {
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
