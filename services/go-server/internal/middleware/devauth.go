package middleware

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	// ContextUserEmailKey stores the verified identity or email of the caller in Gin context.
	ContextUserEmailKey = "auth_user_email"
	// ContextAuthMethodKey stores the authentication mechanism used ("cloudflare_access", "dev_token", or "local_bypass").
	ContextAuthMethodKey = "auth_method"
)

// RequireDevAuth enforces that requests to sensitive internal endpoints (such as ground-truth labeling
// and snapshot archives) are authenticated either via Cloudflare Zero Trust Access or a valid developer access token.
//
//  1. Primary Gate: Cloudflare Zero Trust Access
//     If request arrives via Cloudflare Access (Cf-Access-Authenticated-User-Email or Cf-Access-Jwt-Assertion header),
//     it is considered authenticated and caller identity is bound to context.
//
//  2. Fallback Gate: Developer Access Token (DEV_ACCESS_TOKEN)
//     If Cloudflare Access headers are absent, caller must provide a valid dev token via:
//     - X-Dev-Token header
//     - Authorization: Bearer <token>
//     - token or dev_token query parameter
//
// On production/staging without CF Access headers, it fails closed (503 if secret is not configured, 401 if token is invalid/missing).
// On local dev (appEnv == "dev"), it allows bypass only if devToken is explicitly unconfigured and CF headers are absent.
func RequireDevAuth(devToken string, appEnv string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 1. Check Cloudflare Zero Trust Access headers first (Primary)
		cfEmail := strings.TrimSpace(c.GetHeader("Cf-Access-Authenticated-User-Email"))
		cfJwt := strings.TrimSpace(c.GetHeader("Cf-Access-Jwt-Assertion"))
		cfClientId := strings.TrimSpace(c.GetHeader("CF-Access-Client-Id"))
		if cfEmail != "" || cfJwt != "" || cfClientId != "" {
			if cfEmail == "" {
				if cfClientId != "" {
					cfEmail = "service-token-" + cfClientId
				} else {
					cfEmail = "cf-access-user"
				}
			}
			c.Set(ContextUserEmailKey, cfEmail)
			c.Set(ContextAuthMethodKey, "cloudflare_access")
			c.Next()
			return
		}

		// 2. Fallback to Developer Access Token
		isProd := appEnv == "production" || appEnv == "staging"
		token := strings.TrimSpace(devToken)

		// Fail-closed on production/staging if secret is unconfigured
		if isProd && token == "" {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
				"error": "archiver developer endpoints are disabled on production without configured DEV_ACCESS_TOKEN or Cloudflare Access",
			})
			return
		}

		// Local dev bypass if secret is unconfigured
		if !isProd && token == "" {
			c.Set(ContextUserEmailKey, "dev-bypass")
			c.Set(ContextAuthMethodKey, "local_bypass")
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
				"error": "unauthorized: valid Cloudflare Access session or developer access token required",
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

		c.Set(ContextUserEmailKey, "dev-token-user")
		c.Set(ContextAuthMethodKey, "dev_token")
		c.Next()
	}
}
