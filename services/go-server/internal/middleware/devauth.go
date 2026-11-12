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

// DevAuthConfig holds configuration parameters for RequireDevAuth.
type DevAuthConfig struct {
	DevAccessToken       string
	AppEnv               string
	CFAccessAudience     string
	CFAccessCertsURL     string
	CFAccessClientID     string
	CFAccessClientSecret string
	KeySet               KeySet
}

// RequireDevAuth enforces that requests to sensitive internal endpoints (such as ground-truth labeling
// and snapshot archives) are authenticated either via Cloudflare Zero Trust Access or a valid developer access token.
//
// 1. Primary Gate: Cloudflare Zero Trust Access
//   - Cryptographically verifies Cf-Access-Jwt-Assertion via RS256 against Cloudflare public keys.
//   - Validates expected Audience (aud) against CFAccessAudience (if configured).
//   - Or validates Service Token headers (CF-Access-Client-Id & CF-Access-Client-Secret) via constant-time comparison.
//   - Plain unverified Cf-Access-* headers without cryptographic proof are rejected with 401 Unauthorized.
//
// 2. Fallback Gate: Developer Access Token (DEV_ACCESS_TOKEN)
//
//	If Cloudflare Access headers are absent, caller must provide a valid dev token via:
//	- X-Dev-Token header
//	- Authorization: Bearer <token>
//	- token or dev_token query parameter
//
// On production/staging without CF Access headers, it fails closed (503 if secret is not configured, 401 if token is invalid/missing).
// On local dev (appEnv == "dev"), it allows bypass only if devToken is explicitly unconfigured and CF headers are absent.
func RequireDevAuth(devToken string, appEnv string, cfConfigs ...DevAuthConfig) gin.HandlerFunc {
	cfg := DevAuthConfig{
		DevAccessToken: devToken,
		AppEnv:         appEnv,
	}
	if len(cfConfigs) > 0 {
		c := cfConfigs[0]
		if c.DevAccessToken != "" {
			cfg.DevAccessToken = c.DevAccessToken
		}
		if c.AppEnv != "" {
			cfg.AppEnv = c.AppEnv
		}
		cfg.CFAccessAudience = c.CFAccessAudience
		cfg.CFAccessCertsURL = c.CFAccessCertsURL
		cfg.CFAccessClientID = c.CFAccessClientID
		cfg.CFAccessClientSecret = c.CFAccessClientSecret
		cfg.KeySet = c.KeySet
	}
	return RequireDevAuthWithConfig(cfg)
}

// RequireDevAuthWithConfig enforces developer authentication using the provided DevAuthConfig.
func RequireDevAuthWithConfig(cfg DevAuthConfig) gin.HandlerFunc {
	keySet := cfg.KeySet
	if keySet == nil && cfg.CFAccessCertsURL != "" {
		keySet = NewJWKSKeySet(cfg.CFAccessCertsURL, nil)
	}

	return func(c *gin.Context) {
		cfJwt := strings.TrimSpace(c.GetHeader("Cf-Access-Jwt-Assertion"))
		cfEmail := strings.TrimSpace(c.GetHeader("Cf-Access-Authenticated-User-Email"))
		cfClientId := strings.TrimSpace(c.GetHeader("CF-Access-Client-Id"))
		cfClientSecret := strings.TrimSpace(c.GetHeader("CF-Access-Client-Secret"))

		// 1. Check Cloudflare Zero Trust Access
		// If JWT assertion is present, cryptographically verify it
		if cfJwt != "" {
			payload, err := VerifyCloudflareJWT(c.Request.Context(), cfJwt, cfg.CFAccessAudience, keySet)
			if err != nil {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
					"error": "unauthorized: invalid or unverified Cloudflare Access JWT: " + err.Error(),
				})
				return
			}

			user := payload.Email
			if user == "" {
				user = payload.Sub
			}
			if user == "" {
				user = "cf-access-user"
			}

			// If plain email header was also supplied, prevent header spoofing mismatch
			if cfEmail != "" && payload.Email != "" && cfEmail != payload.Email {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
					"error": "unauthorized: Cf-Access-Authenticated-User-Email mismatch with verified token",
				})
				return
			}

			c.Set(ContextUserEmailKey, user)
			c.Set(ContextAuthMethodKey, "cloudflare_access")
			c.Next()
			return
		}

		// Service token header validation (CF-Access-Client-Id & CF-Access-Client-Secret)
		if cfClientId != "" || cfClientSecret != "" {
			if cfg.CFAccessClientID != "" && cfg.CFAccessClientSecret != "" && cfClientId != "" && cfClientSecret != "" {
				idMatch := subtle.ConstantTimeCompare([]byte(cfClientId), []byte(cfg.CFAccessClientID)) == 1
				secretMatch := subtle.ConstantTimeCompare([]byte(cfClientSecret), []byte(cfg.CFAccessClientSecret)) == 1
				if idMatch && secretMatch {
					c.Set(ContextUserEmailKey, "service-token-"+cfClientId)
					c.Set(ContextAuthMethodKey, "cloudflare_access")
					c.Next()
					return
				}
			}
			// Invalid or unconfigured service token credentials
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "unauthorized: invalid or unverified Cloudflare Access service token credentials",
			})
			return
		}

		// If caller provided plain unverified email header without cryptographic proof, fail closed
		if cfEmail != "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "unauthorized: unverified Cloudflare Access email header rejected without valid JWT assertion",
			})
			return
		}

		// 2. Fallback to Developer Access Token
		isProd := cfg.AppEnv == "production" || cfg.AppEnv == "staging"
		token := strings.TrimSpace(cfg.DevAccessToken)

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
