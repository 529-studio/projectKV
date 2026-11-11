package middleware_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/529-studio/projectKV/services/go-server/internal/middleware"
)

func generateTestCert(t *testing.T, key *rsa.PrivateKey) string {
	t.Helper()
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"529 Studio Test"},
		},
		NotBefore: time.Now().Add(-1 * time.Hour),
		NotAfter:  time.Now().Add(24 * time.Hour),
	}
	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("failed to create certificate: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	return string(certPEM)
}

func TestJWKSKeySet_FetchAndCache(t *testing.T) {
	key1 := generateTestRSAKey(t)
	key2 := generateTestRSAKey(t)
	cert2 := generateTestCert(t, key2)

	nBytes := key1.PublicKey.N.Bytes()
	eBytes := big.NewInt(int64(key1.PublicKey.E)).Bytes()

	mockJWKS := map[string]interface{}{
		"keys": []map[string]interface{}{
			{
				"kid": "jwk-key-01",
				"kty": "RSA",
				"alg": "RS256",
				"n":   base64.RawURLEncoding.EncodeToString(nBytes),
				"e":   base64.RawURLEncoding.EncodeToString(eBytes),
			},
		},
		"public_certs": []map[string]interface{}{
			{
				"kid":  "cert-key-02",
				"cert": cert2,
			},
		},
	}

	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(mockJWKS)
	}))
	defer server.Close()

	keySet := middleware.NewJWKSKeySet(server.URL, server.Client())

	// 1. Fetch jwk-key-01
	pubKey1, err := keySet.GetPublicKey(context.Background(), "jwk-key-01")
	if err != nil {
		t.Fatalf("unexpected error fetching jwk key: %v", err)
	}
	if pubKey1.N.Cmp(key1.PublicKey.N) != 0 {
		t.Errorf("modulus mismatch for jwk key")
	}

	// 2. Fetch cert-key-02 (should be cached from same response, zero extra HTTP requests)
	pubKey2, err := keySet.GetPublicKey(context.Background(), "cert-key-02")
	if err != nil {
		t.Fatalf("unexpected error fetching cert key: %v", err)
	}
	if pubKey2.N.Cmp(key2.PublicKey.N) != 0 {
		t.Errorf("modulus mismatch for cert key")
	}

	if requestCount != 1 {
		t.Errorf("expected 1 HTTP request due to caching, got %d", requestCount)
	}

	// 3. Request unknown kid (triggers refresh attempt)
	_, err = keySet.GetPublicKey(context.Background(), "unknown-kid")
	if err == nil {
		t.Errorf("expected error for unknown kid, got nil")
	}
}

func TestVerifyCloudflareJWT_TamperedSignature(t *testing.T) {
	key := generateTestRSAKey(t)
	kid := "test-kid"

	token := signTestJWT(t, key, kid, "RS256", middleware.JWTPayload{
		Email: "user@example.com",
		Exp:   time.Now().Add(1 * time.Hour).Unix(),
	})

	// Tamper with the payload part
	tokenParts := append([]byte(token)[:len(token)-5], []byte("AAAAA")...)

	keySet := &middleware.StaticKeySet{
		Keys: map[string]*rsa.PublicKey{kid: &key.PublicKey},
	}

	_, err := middleware.VerifyCloudflareJWT(context.Background(), string(tokenParts), "", keySet)
	if err == nil {
		t.Errorf("expected error for tampered token, got nil")
	}
}

func TestVerifyCloudflareJWT_UnsupportedAlgorithm(t *testing.T) {
	key := generateTestRSAKey(t)
	kid := "test-kid"

	// Attacker attempts "none" or "HS256"
	token := signTestJWT(t, key, kid, "none", middleware.JWTPayload{
		Email: "attacker@example.com",
		Exp:   time.Now().Add(1 * time.Hour).Unix(),
	})

	keySet := &middleware.StaticKeySet{
		Keys: map[string]*rsa.PublicKey{kid: &key.PublicKey},
	}

	_, err := middleware.VerifyCloudflareJWT(context.Background(), token, "", keySet)
	if err == nil {
		t.Errorf("expected error for 'none' algorithm, got nil")
	}
}

func TestVerifyCloudflareJWT_AudienceArrayMatching(t *testing.T) {
	key := generateTestRSAKey(t)
	kid := "test-kid"

	payload := middleware.JWTPayload{
		Aud:   []interface{}{"aud-1", "aud-target", "aud-3"},
		Email: "aud-user@example.com",
		Exp:   time.Now().Add(1 * time.Hour).Unix(),
	}

	token := signTestJWT(t, key, kid, "RS256", payload)
	keySet := &middleware.StaticKeySet{
		Keys: map[string]*rsa.PublicKey{kid: &key.PublicKey},
	}

	// 1. Matches target in array
	parsed, err := middleware.VerifyCloudflareJWT(context.Background(), token, "aud-target", keySet)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if parsed.Email != "aud-user@example.com" {
		t.Errorf("expected email aud-user@example.com, got %s", parsed.Email)
	}

	// 2. Mismatch
	_, err = middleware.VerifyCloudflareJWT(context.Background(), token, "missing-aud", keySet)
	if err == nil {
		t.Errorf("expected audience mismatch error, got nil")
	}
}
