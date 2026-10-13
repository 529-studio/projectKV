package middleware

import (
	"bytes"
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
)

var (
	// googleAPIKeyRegex matches standard 39-character Google/Gemini API keys starting with AIza.
	googleAPIKeyRegex = regexp.MustCompile(`AIza[0-9A-Za-z-_]{35}`)
	// awsAccessKeyRegex matches AWS / Cloudflare R2 standard 20-character Access Key IDs.
	awsAccessKeyRegex = regexp.MustCompile(`\b(A3T[A-Z0-9]|AKIA|AGPA|AIDA|AROA|AIPA|ANPA|ANVA|ASIA)[A-Z0-9]{16}\b`)
	// queryKeyRegex matches URL query parameter keys (e.g., ?key=SECRET or &key=SECRET).
	queryKeyRegex = regexp.MustCompile(`(?i)([?&](?:key|api_key|token|secret)=)[^&\s"\'\\}]+`)
)

// SanitizeBytes scrubs all known API key and credential patterns from b.
func SanitizeBytes(b []byte) []byte {
	if len(b) == 0 {
		return b
	}
	out := googleAPIKeyRegex.ReplaceAll(b, []byte("[REDACTED_KEY]"))
	out = awsAccessKeyRegex.ReplaceAll(out, []byte("[REDACTED_KEY]"))
	out = queryKeyRegex.ReplaceAll(out, []byte("${1}[REDACTED]"))
	return out
}

type sanitizingResponseWriter struct {
	gin.ResponseWriter
	buf        bytes.Buffer
	statusCode int
}

func (w *sanitizingResponseWriter) Write(b []byte) (int, error) {
	// If it's a binary media stream (image/jpeg, image/png, tiles), bypass buffering
	// to avoid extra memory allocation on high-frequency snapshot proxying.
	ct := w.Header().Get("Content-Type")
	if strings.HasPrefix(ct, "image/") || strings.HasPrefix(ct, "application/octet-stream") {
		return w.ResponseWriter.Write(b)
	}
	return w.buf.Write(b)
}

func (w *sanitizingResponseWriter) WriteString(s string) (int, error) {
	return w.Write([]byte(s))
}

func (w *sanitizingResponseWriter) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

// SecretSanitizer returns a Gin middleware that intercepts outgoing HTTP responses
// (JSON, text, error responses) and redacts any recognized secret or API key patterns
// before sending bytes over the wire. Defense-in-depth shield for Rule 8 compliance.
func SecretSanitizer() gin.HandlerFunc {
	return func(c *gin.Context) {
		w := &sanitizingResponseWriter{
			ResponseWriter: c.Writer,
			statusCode:     http.StatusOK,
		}
		c.Writer = w

		c.Next()

		// If buffer has content (JSON, text, HTML, or error), sanitize before flushing.
		if w.buf.Len() > 0 {
			sanitized := SanitizeBytes(w.buf.Bytes())
			// Update Content-Length header if it was previously set
			if w.Header().Get("Content-Length") != "" {
				w.Header().Del("Content-Length")
			}
			_, _ = w.ResponseWriter.Write(sanitized)
		}
	}
}
