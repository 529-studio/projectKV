package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// CORSMiddleware enables CORS for API consumers, allowing the frontend dev server to connect smoothly.
func CORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, HEAD, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With, X-Dev-Token, Cf-Access-Jwt-Assertion, Cf-Access-Authenticated-User-Email, CF-Access-Client-Id, CF-Access-Client-Secret")
		c.Writer.Header().Set("Access-Control-Expose-Headers", "Content-Length, Content-Type, X-Cache-Status, Cache-Control, X-OSD-Timestamp, X-OSD-Raw, X-Camera-Status")

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusOK)
			return
		}

		c.Next()
	}
}
