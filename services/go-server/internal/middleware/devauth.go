package middleware

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// RequireDevAuth enforces that only authorized requests presenting a valid developer access token
// can access sensitive internal endpoints (such as ground-truth labeling and snapshot archives).
// On production/staging, it fails closed (503 if secret is not configured, 401 if token is invalid or missing).
// On local dev (appEnv == "dev"), it allows bypass only if devToken is explicitly unconfigured.
func RequireDevAuth(devToken string, appEnv string) gin.HandlerFunc {
	return func(c *gin.Context) {
		isProd := appEnv == "production" || appEnv == "staging"
		token := strings.TrimSpace(devToken)

		// Fail-closed on production/staging if secret is unconfigured
		if isProd && token == "" {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
				"error": "archiver developer endpoints are disabled on production without configured DEV_ACCESS_TOKEN",
			})
			return
		}

		// Local dev bypass if secret is unconfigured
		if !isProd && token == "" {
			c.Next()
			return
		}

		// Extract token from X-Dev-Token header, Authorization Bearer header, or token/dev_token query param
		reqToken := strings.TrimSpace(c.GetHeader("X-Dev-Token"))
		if reqToken == "" {
			authHeader := c.GetHeader("Authorization")
			if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
				reqToken = strings.TrimSpace(authHeader[7:])
			}
		}
		if reqToken == "" {
			reqToken = strings.TrimSpace(c.Query("token"))
			if reqToken == "" {
				reqToken = strings.TrimSpace(c.Query("dev_token"))
			}
		}

		// If no token was provided, abort 401
		if reqToken == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "unauthorized: developer access token required",
			})
			return
		}

		// Constant-time comparison to prevent timing attacks
		if subtle.ConstantTimeCompare([]byte(reqToken), []byte(token)) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "unauthorized: invalid developer access token",
			})
			return
		}

		c.Next()
	}
}
