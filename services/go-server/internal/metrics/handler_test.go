package metrics

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestHandler_NonProduction_OpenAccess(t *testing.T) {
	reg := NewRegistry()
	reg.RegisterGauge("warded_corridor_active_count", "Active corridors", nil).Set(5)

	h := NewHandler(reg, HandlerConfig{
		AppEnv: "development",
	})

	r := gin.New()
	h.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK in dev, got %d", w.Code)
	}
	if !strings.Contains(w.Header().Get("Content-Type"), "text/plain") {
		t.Errorf("expected text/plain Content-Type, got %s", w.Header().Get("Content-Type"))
	}
	if !strings.Contains(w.Body.String(), "warded_corridor_active_count 5") {
		t.Errorf("expected metric in response body, got:\n%s", w.Body.String())
	}
}

func TestHandler_Production_AllowedCIDR(t *testing.T) {
	reg := NewRegistry()
	reg.RegisterGauge("test_val", "Test", nil).Set(100)

	cidrs, err := ParseCIDRBlocks([]string{"192.168.1.0/24", "127.0.0.1/32"})
	if err != nil {
		t.Fatalf("ParseCIDRBlocks: %v", err)
	}

	h := NewHandler(reg, HandlerConfig{
		AppEnv:       "production",
		AllowedCIDRs: cidrs,
	})

	r := gin.New()
	// Set trusted proxies so c.ClientIP() returns RemoteAddr
	_ = r.SetTrustedProxies(nil)
	h.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from allowed CIDR, got %d", w.Code)
	}
}

func TestHandler_Production_DeniedExternalIP(t *testing.T) {
	reg := NewRegistry()
	cidrs, _ := ParseCIDRBlocks([]string{"127.0.0.1/32"})

	h := NewHandler(reg, HandlerConfig{
		AppEnv:       "production",
		AllowedCIDRs: cidrs,
		AdminToken:   "super-secret-metrics-token",
	})

	r := gin.New()
	_ = r.SetTrustedProxies(nil)
	h.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.RemoteAddr = "203.0.113.10:54321" // External public IP
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for untrusted IP without token, got %d", w.Code)
	}
}

func TestHandler_Production_BearerTokenAuth(t *testing.T) {
	reg := NewRegistry()
	cidrs, _ := ParseCIDRBlocks([]string{"127.0.0.1/32"})

	h := NewHandler(reg, HandlerConfig{
		AppEnv:       "production",
		AllowedCIDRs: cidrs,
		AdminToken:   "valid-token-12345",
	})

	r := gin.New()
	_ = r.SetTrustedProxies(nil)
	h.RegisterRoutes(r)

	// External IP with valid bearer token
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.RemoteAddr = "198.51.100.42:11223"
	req.Header.Set("Authorization", "Bearer valid-token-12345")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK with valid bearer token, got %d", w.Code)
	}

	// External IP with invalid bearer token
	reqBad := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	reqBad.RemoteAddr = "198.51.100.42:11223"
	reqBad.Header.Set("Authorization", "Bearer wrong-token")
	wBad := httptest.NewRecorder()
	r.ServeHTTP(wBad, reqBad)

	if wBad.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden with invalid bearer token, got %d", wBad.Code)
	}
}

func TestParseCIDRBlocks(t *testing.T) {
	// Standard CIDRs
	ipNets, err := ParseCIDRBlocks([]string{"10.0.0.0/8", "192.168.1.1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ipNets) != 2 {
		t.Fatalf("expected 2 parsed blocks, got %d", len(ipNets))
	}
	if !ipNets[0].Contains(net.ParseIP("10.5.6.7")) {
		t.Errorf("expected 10.5.6.7 in 10.0.0.0/8")
	}
	if !ipNets[1].Contains(net.ParseIP("192.168.1.1")) {
		t.Errorf("expected 192.168.1.1 in host block")
	}

	// Invalid CIDR returns error
	_, err = ParseCIDRBlocks([]string{"invalid-cidr"})
	if err == nil {
		t.Errorf("expected error for invalid CIDR")
	}
}
