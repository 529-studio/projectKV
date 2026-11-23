package push_test

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/base64"
	"math/big"
	"strings"
	"testing"

	"github.com/529-studio/projectKV/services/go-server/internal/push"
)

func TestVAPIDKey_GenerateAndSign(t *testing.T) {
	key, err := push.GenerateVAPIDKeys()
	if err != nil {
		t.Fatalf("GenerateVAPIDKeys failed: %v", err)
	}

	pubB64 := key.PublicKeyBase64()
	if len(pubB64) == 0 {
		t.Fatalf("expected non-empty public key string")
	}

	// Verify uncompressed P-256 public key is 65 bytes
	pubBytes, err := base64.RawURLEncoding.DecodeString(pubB64)
	if err != nil {
		t.Fatalf("invalid base64url public key: %v", err)
	}
	if len(pubBytes) != 65 || pubBytes[0] != 0x04 {
		t.Fatalf("expected 65 bytes uncompressed point (0x04 prefix), got %d bytes", len(pubBytes))
	}

	// Sign a VAPID header
	aud := "https://fcm.googleapis.com"
	sub := "mailto:admin@529studio.site"
	authHeader, err := key.SignHeader(aud, sub)
	if err != nil {
		t.Fatalf("SignHeader failed: %v", err)
	}

	// Header format: "vapid t=<jwt>, k=<base64url_public_key>"
	if !strings.HasPrefix(authHeader, "vapid ") {
		t.Fatalf("expected header to start with 'vapid ', got %s", authHeader)
	}
	parts := strings.Split(strings.TrimPrefix(authHeader, "vapid "), ", ")
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts 't=..., k=...', got %d parts", len(parts))
	}

	var tokenStr, keyStr string
	for _, part := range parts {
		if strings.HasPrefix(part, "t=") {
			tokenStr = strings.TrimPrefix(part, "t=")
		} else if strings.HasPrefix(part, "k=") {
			keyStr = strings.TrimPrefix(part, "k=")
		}
	}

	if tokenStr == "" || keyStr != pubB64 {
		t.Fatalf("token or key mismatch in auth header: token=%s, key=%s", tokenStr, keyStr)
	}

	// Verify the JWT signature using ES256
	jwtParts := strings.Split(tokenStr, ".")
	if len(jwtParts) != 3 {
		t.Fatalf("expected 3 parts in JWT, got %d", len(jwtParts))
	}

	signingInput := jwtParts[0] + "." + jwtParts[1]
	sigBytes, err := base64.RawURLEncoding.DecodeString(jwtParts[2])
	if err != nil {
		t.Fatalf("decode signature failed: %v", err)
	}
	if len(sigBytes) != 64 {
		t.Fatalf("expected 64 bytes IEEE P1363 signature (R||S), got %d", len(sigBytes))
	}

	r := new(big.Int).SetBytes(sigBytes[:32])
	s := new(big.Int).SetBytes(sigBytes[32:])

	hash := sha256.Sum256([]byte(signingInput))
	ecdsaPub := key.ECDSAPublicKey()
	if !ecdsa.Verify(ecdsaPub, hash[:], r, s) {
		t.Fatalf("JWT signature verification failed")
	}
}

func TestVAPIDKey_LoadFromStrings(t *testing.T) {
	orig, err := push.GenerateVAPIDKeys()
	if err != nil {
		t.Fatalf("GenerateVAPIDKeys failed: %v", err)
	}

	pubB64 := orig.PublicKeyBase64()
	privB64 := orig.PrivateKeyBase64()

	loaded, err := push.NewVAPIDKeyFromBase64(pubB64, privB64)
	if err != nil {
		t.Fatalf("NewVAPIDKeyFromBase64 failed: %v", err)
	}

	if loaded.PublicKeyBase64() != pubB64 {
		t.Fatalf("public key mismatch: %s != %s", loaded.PublicKeyBase64(), pubB64)
	}
	if loaded.PrivateKeyBase64() != privB64 {
		t.Fatalf("private key mismatch: %s != %s", loaded.PrivateKeyBase64(), privB64)
	}
}
