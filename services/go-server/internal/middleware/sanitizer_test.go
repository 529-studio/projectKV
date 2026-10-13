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
