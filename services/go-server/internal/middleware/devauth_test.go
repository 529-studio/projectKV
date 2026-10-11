package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/529-studio/projectKV/services/go-server/internal/middleware"
	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func setupAuthRouter(devToken string, appEnv string) *gin.Engine {
	r := gin.New()
	r.Use(middleware.RequireDevAuth(devToken, appEnv))
	r.GET("/protected", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	return r
}

func TestRequireDevAuth_ValidXDevToken(t *testing.T) {
	r := setupAuthRouter("secret-123", "production")

	req, _ := http.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("X-Dev-Token", "secret-123")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d (body: %s)", w.Code, w.Body.String())
	}
}

func TestRequireDevAuth_ValidBearerToken(t *testing.T) {
	r := setupAuthRouter("secret-123", "production")

	req, _ := http.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer secret-123")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d (body: %s)", w.Code, w.Body.String())
	}
}

func TestRequireDevAuth_InvalidToken(t *testing.T) {
	r := setupAuthRouter("secret-123", "production")

	req, _ := http.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("X-Dev-Token", "wrong-token")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized, got %d", w.Code)
	}
}

func TestRequireDevAuth_MissingToken_Production(t *testing.T) {
	r := setupAuthRouter("secret-123", "production")

	req, _ := http.NewRequest(http.MethodGet, "/protected", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized, got %d", w.Code)
	}
}

func TestRequireDevAuth_MissingToken_Staging(t *testing.T) {
	r := setupAuthRouter("secret-123", "staging")

	req, _ := http.NewRequest(http.MethodGet, "/protected", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized, got %d", w.Code)
	}
}

func TestRequireDevAuth_EmptySecret_Production_FailsClosed(t *testing.T) {
	r := setupAuthRouter("", "production")

	req, _ := http.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("X-Dev-Token", "any-token")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 Service Unavailable when secret is empty in production, got %d", w.Code)
	}
}

func TestRequireDevAuth_EmptySecret_Dev_Bypass(t *testing.T) {
	r := setupAuthRouter("", "dev")

	req, _ := http.NewRequest(http.MethodGet, "/protected", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK in dev with unconfigured secret, got %d", w.Code)
	}
}

func TestRequireDevAuth_ConfiguredSecret_Dev_Enforced(t *testing.T) {
	r := setupAuthRouter("local-dev-token", "dev")

	// Missing token should fail even in dev if explicitly configured
	req, _ := http.NewRequest(http.MethodGet, "/protected", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized in dev when secret is configured, got %d", w.Code)
	}
}

func TestRequireDevAuth_ValidQueryParamToken(t *testing.T) {
	r := setupAuthRouter("secret-query-123", "production")

	// Test ?token=...
	req1, _ := http.NewRequest(http.MethodGet, "/protected?token=secret-query-123", nil)
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("expected 200 OK with ?token=..., got %d", w1.Code)
	}

	// Test ?dev_token=...
	req2, _ := http.NewRequest(http.MethodGet, "/protected?dev_token=secret-query-123", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200 OK with ?dev_token=..., got %d", w2.Code)
	}
}
