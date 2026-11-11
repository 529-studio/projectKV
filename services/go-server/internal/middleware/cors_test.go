package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/529-studio/projectKV/services/go-server/internal/middleware"
	"github.com/gin-gonic/gin"
)

func TestCORSMiddleware_WildcardWithoutOrigin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware.CORSMiddleware())
	router.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("expected Access-Control-Allow-Origin: *, got %q", w.Header().Get("Access-Control-Allow-Origin"))
	}
	if w.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Errorf("expected no Access-Control-Allow-Credentials header without origin, got %q", w.Header().Get("Access-Control-Allow-Credentials"))
	}
}

func TestCORSMiddleware_EchoOriginWithCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware.CORSMiddleware())
	router.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Origin", "http://localhost:3001")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Header().Get("Access-Control-Allow-Origin") != "http://localhost:3001" {
		t.Errorf("expected Access-Control-Allow-Origin: http://localhost:3001, got %q", w.Header().Get("Access-Control-Allow-Origin"))
	}
	if w.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Errorf("expected Access-Control-Allow-Credentials: true, got %q", w.Header().Get("Access-Control-Allow-Credentials"))
	}
	if w.Header().Get("Vary") != "Origin" {
		t.Errorf("expected Vary: Origin, got %q", w.Header().Get("Vary"))
	}
}

func TestCORSMiddleware_OptionsPreflight(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware.CORSMiddleware())
	router.POST("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodOptions, "/test", nil)
	req.Header.Set("Origin", "http://localhost:3001")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected OPTIONS status 200, got %d", w.Code)
	}
	if w.Header().Get("Access-Control-Allow-Origin") != "http://localhost:3001" {
		t.Errorf("expected Access-Control-Allow-Origin: http://localhost:3001, got %q", w.Header().Get("Access-Control-Allow-Origin"))
	}
	if w.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Errorf("expected Access-Control-Allow-Credentials: true, got %q", w.Header().Get("Access-Control-Allow-Credentials"))
	}
}

// Security Regression Test: Untrusted Origin Reflection (Issue #421)
// Arbitrary origins must not be reflected and must not receive credentials.
func TestCORSMiddleware_UntrustedOrigin_NoCredentialsOrReflection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware.CORSMiddleware())
	router.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Origin", "http://malicious.evil.example.com")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("expected empty Access-Control-Allow-Origin for untrusted origin, got %q", w.Header().Get("Access-Control-Allow-Origin"))
	}
	if w.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Errorf("expected empty Access-Control-Allow-Credentials for untrusted origin, got %q", w.Header().Get("Access-Control-Allow-Credentials"))
	}
}

// Security Regression Test: Untrusted Origin Preflight Abort (Issue #421)
func TestCORSMiddleware_UntrustedOrigin_PreflightForbidden(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware.CORSMiddleware())
	router.POST("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodOptions, "/test", nil)
	req.Header.Set("Origin", "https://unauthorized-domain.com")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected OPTIONS status 403 Forbidden for untrusted origin, got %d", w.Code)
	}
	if w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("expected empty Access-Control-Allow-Origin on forbidden preflight, got %q", w.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestCORSMiddleware_CustomAllowedOrigins(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware.CORSMiddleware("https://custom.app.internal"))
	router.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	// 1. Custom origin allowed
	req1 := httptest.NewRequest(http.MethodGet, "/test", nil)
	req1.Header.Set("Origin", "https://custom.app.internal")
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)

	if w1.Header().Get("Access-Control-Allow-Origin") != "https://custom.app.internal" {
		t.Errorf("expected custom origin to be allowed, got %q", w1.Header().Get("Access-Control-Allow-Origin"))
	}
	if w1.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Errorf("expected credentials for custom origin, got %q", w1.Header().Get("Access-Control-Allow-Credentials"))
	}

	// 2. Default origin now rejected since custom list was provided
	req2 := httptest.NewRequest(http.MethodGet, "/test", nil)
	req2.Header.Set("Origin", "http://localhost:3001")
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)

	if w2.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("expected default origin rejected when custom list specified, got %q", w2.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestCORSMiddleware_ProductionDomainsAllowedByDefault(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware.CORSMiddleware())
	router.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	domains := []string{
		"https://warded.529studio.site",
		"https://warded-stg.529studio.site",
	}

	for _, domain := range domains {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Origin", domain)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Header().Get("Access-Control-Allow-Origin") != domain {
			t.Errorf("expected %s allowed by default, got %q", domain, w.Header().Get("Access-Control-Allow-Origin"))
		}
		if w.Header().Get("Access-Control-Allow-Credentials") != "true" {
			t.Errorf("expected credentials allowed for %s, got %q", domain, w.Header().Get("Access-Control-Allow-Credentials"))
		}
	}
}
