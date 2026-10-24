package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/529-studio/projectKV/services/go-server/internal/middleware"
	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestSecretSanitizer_RedactsGoogleAPIKey(t *testing.T) {
	r := gin.New()
	r.Use(middleware.SecretSanitizer())

	r.GET("/test-leak", func(c *gin.Context) {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "upstream call failed: Post \"https://generativelanguage.googleapis.com/v1beta/models/gemini:generateContent?key=AIzaSyD1234567890abcdef1234567890ab\": context deadline exceeded",
		})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/test-leak", nil)
	r.ServeHTTP(w, req)

	body := w.Body.String()
	if strings.Contains(body, "AIzaSyD1234567890abcdef1234567890ab") {
		t.Fatalf("expected Google API key to be redacted, got raw body: %s", body)
	}
	if !strings.Contains(body, "[REDACTED_KEY]") && !strings.Contains(body, "?key=[REDACTED]") {
		t.Errorf("expected redacted token marker in response, got: %s", body)
	}
}

func TestSecretSanitizer_RedactsAWSAccessKey(t *testing.T) {
	r := gin.New()
	r.Use(middleware.SecretSanitizer())

	r.GET("/test-aws", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": "configured with key AKIAIOSFODNN7EXAMPLE successfully",
		})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/test-aws", nil)
	r.ServeHTTP(w, req)

	body := w.Body.String()
	if strings.Contains(body, "AKIAIOSFODNN7EXAMPLE") {
		t.Fatalf("expected AWS access key to be redacted, got raw body: %s", body)
	}
	if !strings.Contains(body, "[REDACTED_KEY]") {
		t.Errorf("expected [REDACTED_KEY] marker, got: %s", body)
	}
}

func TestSecretSanitizer_PreservesCleanResponses(t *testing.T) {
	r := gin.New()
	r.Use(middleware.SecretSanitizer())

	r.GET("/clean", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
			"count":  42,
		})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/clean", nil)
	r.ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, `"status":"ok"`) || !strings.Contains(body, `"count":42`) {
		t.Errorf("expected clean response body preserved, got: %s", body)
	}
}

func TestSecretSanitizer_BypassesImageStreams(t *testing.T) {
	r := gin.New()
	r.Use(middleware.SecretSanitizer())

	imageData := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46} // Fake JPEG header
	r.GET("/snapshot.jpg", func(c *gin.Context) {
		c.Data(http.StatusOK, "image/jpeg", imageData)
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/snapshot.jpg", nil)
	r.ServeHTTP(w, req)

	if w.Body.Len() != len(imageData) {
		t.Errorf("expected byte length %d, got %d", len(imageData), w.Body.Len())
	}
}

func TestSecretSanitizer_PreservesStorageObjectKeys(t *testing.T) {
	r := gin.New()
	r.Use(middleware.SecretSanitizer())

	r.GET("/snapshots", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"raw_url": "/api/archiver/snapshots/raw?key=2026-10-21%2Fcam1%2F123_sample.jpg",
			"key":     "2026-10-21/cam1/123_sample.jpg",
		})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/snapshots", nil)
	r.ServeHTTP(w, req)

	body := w.Body.String()
	if strings.Contains(body, "[REDACTED]") {
		t.Errorf("expected storage object key in raw_url to NOT be redacted, got: %s", body)
	}
	if !strings.Contains(body, "2026-10-21%2Fcam1%2F123_sample.jpg") {
		t.Errorf("expected original storage object key preserved, got: %s", body)
	}
}

// TestSecretSanitizer_RedactsTelegramBotToken verifies the Issue #246 / S8-T9
// defense-in-depth requirement: a Telegram bot token echoed back in any HTTP
// response body (e.g. a misconfigured upstream error) is scrubbed to
// [REDACTED_BOT_TOKEN] before the bytes leave the server.
func TestSecretSanitizer_RedactsTelegramBotToken(t *testing.T) {
	r := gin.New()
	r.Use(middleware.SecretSanitizer())

	rawToken := "7359930142:ZHdqTcvCH1vGWJxfSeofSAs0K5PALDsaw"
	r.GET("/notify-status", func(c *gin.Context) {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "telegram http post: dial tcp: lookup api.telegram.org: Post /bot" + rawToken + "/sendMessage failed",
		})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/notify-status", nil)
	r.ServeHTTP(w, req)

	body := w.Body.String()
	if strings.Contains(body, rawToken) {
		t.Fatalf("expected Telegram bot token to be redacted, got raw body: %s", body)
	}
	if !strings.Contains(body, "[REDACTED_BOT_TOKEN]") {
		t.Errorf("expected [REDACTED_BOT_TOKEN] marker in response, got: %s", body)
	}
}

// TestSecretSanitizer_PreservesClockTimes guards against false positives: the
// token pattern requires 8-10 digits followed by a 35-character base64url tail,
// so ordinary clock times, ratios, and short tails must survive untouched.
func TestSecretSanitizer_PreservesClockTimes(t *testing.T) {
	r := gin.New()
	r.Use(middleware.SecretSanitizer())

	r.GET("/timings", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"observed_at": "12:34:56",
			"ratio":       "10:1",
			"duration":    "12345678:shorttail",
			"message_id":  7359930142,
		})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/timings", nil)
	r.ServeHTTP(w, req)

	body := w.Body.String()
	for _, want := range []string{"12:34:56", "10:1", "12345678:shorttail", "7359930142"} {
		if !strings.Contains(body, want) {
			t.Errorf("expected benign value %q preserved, got: %s", want, body)
		}
	}
}
