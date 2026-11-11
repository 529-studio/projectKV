package middleware_test

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/529-studio/projectKV/services/go-server/internal/middleware"
	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func setupAuthRouter(devToken string, appEnv string, cfConfigs ...middleware.DevAuthConfig) *gin.Engine {
	r := gin.New()
	r.Use(middleware.RequireDevAuth(devToken, appEnv, cfConfigs...))
	r.GET("/protected", func(c *gin.Context) {
		email, _ := c.Get(middleware.ContextUserEmailKey)
		method, _ := c.Get(middleware.ContextAuthMethodKey)
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
			"email":  email,
			"method": method,
		})
	})
	return r
}

func generateTestRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}
	return key
}

func signTestJWT(t *testing.T, key *rsa.PrivateKey, kid string, headerAlg string, payload middleware.JWTPayload) string {
	t.Helper()
	header := map[string]string{
		"alg": headerAlg,
		"kid": kid,
		"typ": "JWT",
	}
	hBytes, _ := json.Marshal(header)
	pBytes, _ := json.Marshal(payload)

	hB64 := base64.RawURLEncoding.EncodeToString(hBytes)
	pB64 := base64.RawURLEncoding.EncodeToString(pBytes)

	signedContent := hB64 + "." + pB64
	hashed := sha256.Sum256([]byte(signedContent))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hashed[:])
	if err != nil {
		t.Fatalf("failed to sign test JWT: %v", err)
	}
	sigB64 := base64.RawURLEncoding.EncodeToString(sig)
	return signedContent + "." + sigB64
}

func TestRequireDevAuth_ValidXDevToken(t *testing.T) {
	r := setupAuthRouter("secret-123", "production")

	req, _ := http.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("X-Dev-Token", "secret-123")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", w.Code)
	}
}

func TestRequireDevAuth_ValidBearerToken(t *testing.T) {
	r := setupAuthRouter("secret-123", "production")

	req, _ := http.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer secret-123")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", w.Code)
	}
}

func TestRequireDevAuth_InvalidToken(t *testing.T) {
	r := setupAuthRouter("secret-123", "production")

	req, _ := http.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("X-Dev-Token", "wrong-secret")
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
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 Service Unavailable, got %d", w.Code)
	}
}

func TestRequireDevAuth_EmptySecret_Dev_Bypass(t *testing.T) {
	r := setupAuthRouter("", "dev")

	req, _ := http.NewRequest(http.MethodGet, "/protected", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on local bypass, got %d", w.Code)
	}
}

func TestRequireDevAuth_ConfiguredSecret_Dev_Enforced(t *testing.T) {
	r := setupAuthRouter("local-dev-token", "dev")

	// Missing token should fail even in dev if explicitly configured
	req, _ := http.NewRequest(http.MethodGet, "/protected", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized when secret is set in dev, got %d", w.Code)
	}

	// Correct token should succeed
	req2, _ := http.NewRequest(http.MethodGet, "/protected", nil)
	req2.Header.Set("X-Dev-Token", "local-dev-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200 OK with correct token in dev, got %d", w2.Code)
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

// Security Regression Test: Header Injection Auth Bypass (Issue #421)
// An unverified plain Cf-Access-Authenticated-User-Email header must fail closed with 401.
func TestRequireDevAuth_ForgedEmailHeader_FailsClosed401(t *testing.T) {
	r := setupAuthRouter("secret-fallback", "production")

	req, _ := http.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Cf-Access-Authenticated-User-Email", "attacker@example.com")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized for unverified email header injection, got %d", w.Code)
	}
}

// Security Regression Test: Forged JWT Assertion (Issue #421)
func TestRequireDevAuth_ForgedJwtHeader_FailsClosed401(t *testing.T) {
	r := setupAuthRouter("secret-fallback", "production")

	req, _ := http.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Cf-Access-Jwt-Assertion", "forged.bogus.jwt.token")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized for forged JWT, got %d", w.Code)
	}
}

// Security Regression Test: Plain Client ID without Secret (Issue #421)
func TestRequireDevAuth_ForgedServiceToken_FailsClosed401(t *testing.T) {
	r := setupAuthRouter("secret-fallback", "production")

	req, _ := http.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("CF-Access-Client-Id", "service-client-id-xyz")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized for plain service client id without secret, got %d", w.Code)
	}
}

// Test Cryptographically Verified Cloudflare Access JWT Assertion (Issue #421)
func TestRequireDevAuth_ValidCryptographicJWT_Succeeds(t *testing.T) {
	privKey := generateTestRSAKey(t)
	kid := "test-key-id-01"
	expectedAud := "audience-test-tag"

	keySet := &middleware.StaticKeySet{
		Keys: map[string]*rsa.PublicKey{
			kid: &privKey.PublicKey,
		},
	}

	cfCfg := middleware.DevAuthConfig{
		DevAccessToken:   "secret-fallback",
		AppEnv:           "production",
		CFAccessAudience: expectedAud,
		KeySet:           keySet,
	}

	r := setupAuthRouter("secret-fallback", "production", cfCfg)

	token := signTestJWT(t, privKey, kid, "RS256", middleware.JWTPayload{
		Aud:   expectedAud,
		Email: "engineer@529studio.site",
		Exp:   time.Now().Add(1 * time.Hour).Unix(),
		Nbf:   time.Now().Add(-1 * time.Minute).Unix(),
	})

	req, _ := http.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Cf-Access-Jwt-Assertion", token)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for cryptographically verified JWT, got %d (body: %s)", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid json response: %v", err)
	}
	if resp["email"] != "engineer@529studio.site" {
		t.Errorf("expected email 'engineer@529studio.site', got %v", resp["email"])
	}
	if resp["method"] != "cloudflare_access" {
		t.Errorf("expected method 'cloudflare_access', got %v", resp["method"])
	}
}

// Test Expired Cloudflare Access JWT Assertion
func TestRequireDevAuth_ExpiredJWT_FailsClosed401(t *testing.T) {
	privKey := generateTestRSAKey(t)
	kid := "test-key-id-02"

	keySet := &middleware.StaticKeySet{
		Keys: map[string]*rsa.PublicKey{
			kid: &privKey.PublicKey,
		},
	}

	cfCfg := middleware.DevAuthConfig{
		DevAccessToken: "secret-fallback",
		AppEnv:         "production",
		KeySet:         keySet,
	}

	r := setupAuthRouter("secret-fallback", "production", cfCfg)

	token := signTestJWT(t, privKey, kid, "RS256", middleware.JWTPayload{
		Email: "expired@529studio.site",
		Exp:   time.Now().Add(-10 * time.Minute).Unix(),
	})

	req, _ := http.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Cf-Access-Jwt-Assertion", token)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized for expired JWT, got %d", w.Code)
	}
}

// Test Mismatched Audience Cloudflare Access JWT Assertion
func TestRequireDevAuth_AudienceMismatchJWT_FailsClosed401(t *testing.T) {
	privKey := generateTestRSAKey(t)
	kid := "test-key-id-03"

	keySet := &middleware.StaticKeySet{
		Keys: map[string]*rsa.PublicKey{
			kid: &privKey.PublicKey,
		},
	}

	cfCfg := middleware.DevAuthConfig{
		DevAccessToken:   "secret-fallback",
		AppEnv:           "production",
		CFAccessAudience: "expected-aud-123",
		KeySet:           keySet,
	}

	r := setupAuthRouter("secret-fallback", "production", cfCfg)

	token := signTestJWT(t, privKey, kid, "RS256", middleware.JWTPayload{
		Aud:   "attacker-aud-999",
		Email: "user@529studio.site",
		Exp:   time.Now().Add(1 * time.Hour).Unix(),
	})

	req, _ := http.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Cf-Access-Jwt-Assertion", token)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized for audience mismatch, got %d", w.Code)
	}
}

// Test Email Header Spoofing mismatch against verified JWT
func TestRequireDevAuth_EmailHeaderMismatchWithJWT_FailsClosed401(t *testing.T) {
	privKey := generateTestRSAKey(t)
	kid := "test-key-id-04"

	keySet := &middleware.StaticKeySet{
		Keys: map[string]*rsa.PublicKey{
			kid: &privKey.PublicKey,
		},
	}

	cfCfg := middleware.DevAuthConfig{
		DevAccessToken: "secret-fallback",
		AppEnv:         "production",
		KeySet:         keySet,
	}

	r := setupAuthRouter("secret-fallback", "production", cfCfg)

	token := signTestJWT(t, privKey, kid, "RS256", middleware.JWTPayload{
		Email: "legit@529studio.site",
		Exp:   time.Now().Add(1 * time.Hour).Unix(),
	})

	req, _ := http.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Cf-Access-Jwt-Assertion", token)
	req.Header.Set("Cf-Access-Authenticated-User-Email", "impostor@529studio.site")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized when plain email header does not match verified token, got %d", w.Code)
	}
}

// Test Valid Service Token Credentials (ID + Secret)
func TestRequireDevAuth_ValidServiceTokenCredentials_Succeeds(t *testing.T) {
	cfCfg := middleware.DevAuthConfig{
		DevAccessToken:       "secret-fallback",
		AppEnv:               "production",
		CFAccessClientID:     "my-cf-client-id.access",
		CFAccessClientSecret: "my-cf-client-secret-secure-token-999",
	}

	r := setupAuthRouter("secret-fallback", "production", cfCfg)

	req, _ := http.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("CF-Access-Client-Id", "my-cf-client-id.access")
	req.Header.Set("CF-Access-Client-Secret", "my-cf-client-secret-secure-token-999")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for valid service token credentials, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid json response: %v", err)
	}
	if resp["email"] != "service-token-my-cf-client-id.access" {
		t.Errorf("expected email 'service-token-my-cf-client-id.access', got %v", resp["email"])
	}
	if resp["method"] != "cloudflare_access" {
		t.Errorf("expected method 'cloudflare_access', got %v", resp["method"])
	}
}

// Test Invalid Service Token Credentials (Wrong Secret)
func TestRequireDevAuth_InvalidServiceTokenCredentials_FailsClosed401(t *testing.T) {
	cfCfg := middleware.DevAuthConfig{
		DevAccessToken:       "secret-fallback",
		AppEnv:               "production",
		CFAccessClientID:     "my-cf-client-id.access",
		CFAccessClientSecret: "my-cf-client-secret-secure-token-999",
	}

	r := setupAuthRouter("secret-fallback", "production", cfCfg)

	req, _ := http.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("CF-Access-Client-Id", "my-cf-client-id.access")
	req.Header.Set("CF-Access-Client-Secret", "wrong-secret")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized for wrong service token secret, got %d", w.Code)
	}
}

func TestRequireDevAuth_FallbackToDevToken_WhenCfMissing(t *testing.T) {
	r := setupAuthRouter("secret-fallback", "production")

	// Request has no CF headers, but provides valid fallback token
	req, _ := http.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("X-Dev-Token", "secret-fallback")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK via Dev Token fallback, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid json response: %v", err)
	}
	if resp["method"] != "dev_token" {
		t.Fatalf("expected method 'dev_token', got '%v'", resp["method"])
	}
	if resp["email"] != "dev-token-user" {
		t.Fatalf("expected email 'dev-token-user', got '%v'", resp["email"])
	}
}
