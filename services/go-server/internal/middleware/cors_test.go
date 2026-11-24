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

func TestValidateCORSOrigins(t *testing.T) {
	// Dev/staging allows empty or localhost
	if err := middleware.ValidateCORSOrigins(nil, "dev"); err != nil {
		t.Fatalf("expected nil for empty origins in dev, got %v", err)
	}
	if err := middleware.ValidateCORSOrigins([]string{"http://localhost:3000"}, "dev"); err != nil {
		t.Fatalf("expected nil for localhost in dev, got %v", err)
	}

	// Production requires explicit origins
	if err := middleware.ValidateCORSOrigins(nil, "production"); err == nil {
		t.Fatal("expected error for empty origins in production, got nil")
	}
	if err := middleware.ValidateCORSOrigins([]string{}, "production"); err == nil {
		t.Fatal("expected error for empty slice in production, got nil")
	}

	// Production rejects wildcard
	if err := middleware.ValidateCORSOrigins([]string{"*"}, "production"); err == nil {
		t.Fatal("expected error for wildcard '*' in production, got nil")
	}

	// Production rejects localhost and loopback
	localOrigins := [][]string{
		{"http://localhost:3000"},
		{"http://127.0.0.1:3000"},
		{"http://[::1]:3000"},
		{"http://0.0.0.0:3000"},
	}
	for _, lo := range localOrigins {
		if err := middleware.ValidateCORSOrigins(lo, "production"); err == nil {
			t.Fatalf("expected error for loopback origin %v in production, got nil", lo)
		}
	}

	// Production accepts valid public domains
	validOrigins := []string{
		"https://warded.529studio.site",
		"https://app.example.com",
	}
	if err := middleware.ValidateCORSOrigins(validOrigins, "production"); err != nil {
		t.Fatalf("expected valid origins to pass in production, got error: %v", err)
	}
}

func TestCORSMiddlewareWithConfig_ProductionGating(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware.CORSMiddlewareWithConfig(middleware.CORSConfig{
		AllowedOrigins: []string{"https://warded.529studio.site"},
		AppEnv:         "production",
	}))
	router.GET("/api/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	router.POST("/api/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	// 1. Request with empty Origin in production must NOT receive wildcard Access-Control-Allow-Origin: *
	reqNoOrigin := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	wNoOrigin := httptest.NewRecorder()
	router.ServeHTTP(wNoOrigin, reqNoOrigin)
	if wNoOrigin.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("expected no Access-Control-Allow-Origin for empty origin in production, got %q", wNoOrigin.Header().Get("Access-Control-Allow-Origin"))
	}

	// 2. Request from allowed origin gets valid CORS headers
	reqAllowed := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	reqAllowed.Header.Set("Origin", "https://warded.529studio.site")
	wAllowed := httptest.NewRecorder()
	router.ServeHTTP(wAllowed, reqAllowed)
	if wAllowed.Header().Get("Access-Control-Allow-Origin") != "https://warded.529studio.site" {
		t.Errorf("expected allowed origin reflected, got %q", wAllowed.Header().Get("Access-Control-Allow-Origin"))
	}
	if wAllowed.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Errorf("expected credentials true, got %q", wAllowed.Header().Get("Access-Control-Allow-Credentials"))
	}

	// 3. Preflight OPTIONS from untrusted origin is rejected with 403 Forbidden
	reqOptionsUntrusted := httptest.NewRequest(http.MethodOptions, "/api/test", nil)
	reqOptionsUntrusted.Header.Set("Origin", "https://malicious.evil.example.com")
	wOptionsUntrusted := httptest.NewRecorder()
	router.ServeHTTP(wOptionsUntrusted, reqOptionsUntrusted)
	if wOptionsUntrusted.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for untrusted OPTIONS preflight, got %d", wOptionsUntrusted.Code)
	}

	// 4. Preflight OPTIONS from allowed origin returns 200 OK
	reqOptionsAllowed := httptest.NewRequest(http.MethodOptions, "/api/test", nil)
	reqOptionsAllowed.Header.Set("Origin", "https://warded.529studio.site")
	wOptionsAllowed := httptest.NewRecorder()
	router.ServeHTTP(wOptionsAllowed, reqOptionsAllowed)
	if wOptionsAllowed.Code != http.StatusOK {
		t.Errorf("expected 200 OK for allowed OPTIONS preflight, got %d", wOptionsAllowed.Code)
	}
}
