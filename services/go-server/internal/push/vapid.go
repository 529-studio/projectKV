package push

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"
)

// VAPIDKey holds a NIST P-256 keypair used to sign RFC 8292 VAPID headers.
type VAPIDKey struct {
	privateKey *ecdsa.PrivateKey
}

// GenerateVAPIDKeys creates a new random P-256 keypair for VAPID signing.
func GenerateVAPIDKeys() (*VAPIDKey, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate vapid p-256 key: %w", err)
	}
	return &VAPIDKey{privateKey: priv}, nil
}

// NewVAPIDKeyFromBase64 parses Base64URL-encoded uncompressed public key and private key scalar.
func NewVAPIDKeyFromBase64(pubB64, privB64 string) (*VAPIDKey, error) {
	privBytes, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(privB64))
	if err != nil {
		return nil, fmt.Errorf("decode vapid private key: %w", err)
	}
	if len(privBytes) != 32 {
		return nil, fmt.Errorf("invalid private key length: expected 32 bytes, got %d", len(privBytes))
	}

	d := new(big.Int).SetBytes(privBytes)
	curve := elliptic.P256()
	x, y := curve.ScalarBaseMult(d.Bytes())

	priv := &ecdsa.PrivateKey{
		PublicKey: ecdsa.PublicKey{
			Curve: curve,
			X:     x,
			Y:     y,
		},
		D: d,
	}

	if pubB64 != "" {
		expectedPubBytes, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(pubB64))
		if err != nil {
			return nil, fmt.Errorf("decode vapid public key: %w", err)
		}
		computedPub := elliptic.Marshal(curve, x, y)
		if len(expectedPubBytes) != len(computedPub) || !slicesEqual(expectedPubBytes, computedPub) {
			return nil, errors.New("vapid public key does not match private key")
		}
	}

	return &VAPIDKey{privateKey: priv}, nil
}

// PublicKeyBase64 exports the uncompressed P-256 public key (65 bytes) in Base64URL format without padding.
func (k *VAPIDKey) PublicKeyBase64() string {
	pubBytes := elliptic.Marshal(k.privateKey.Curve, k.privateKey.X, k.privateKey.Y)
	return base64.RawURLEncoding.EncodeToString(pubBytes)
}

// PrivateKeyBase64 exports the private key scalar D (32 bytes) in Base64URL format without padding.
func (k *VAPIDKey) PrivateKeyBase64() string {
	dBytes := k.privateKey.D.Bytes()
	// Pad to 32 bytes if necessary
	if len(dBytes) < 32 {
		padded := make([]byte, 32)
		copy(padded[32-len(dBytes):], dBytes)
		dBytes = padded
	}
	return base64.RawURLEncoding.EncodeToString(dBytes)
}

// ECDSAPublicKey returns the underlying crypto/ecdsa public key pointer.
func (k *VAPIDKey) ECDSAPublicKey() *ecdsa.PublicKey {
	return &k.privateKey.PublicKey
}

type jwtHeader struct {
	Typ string `json:"typ"`
	Alg string `json:"alg"`
}

type jwtPayload struct {
	Aud string `json:"aud"`
	Exp int64  `json:"exp"`
	Sub string `json:"sub"`
}

// SignHeader creates the RFC 8292 "Authorization: vapid t=..., k=..." header value.
func (k *VAPIDKey) SignHeader(audience, subject string) (string, error) {
	audience = strings.TrimSpace(audience)
	if audience == "" {
		return "", errors.New("vapid audience cannot be empty")
	}
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return "", errors.New("vapid subject cannot be empty")
	}

	headerJSON, _ := json.Marshal(jwtHeader{Typ: "JWT", Alg: "ES256"})
	payloadJSON, _ := json.Marshal(jwtPayload{
		Aud: audience,
		Exp: time.Now().Add(12 * time.Hour).Unix(),
		Sub: subject,
	})

	signingInput := base64.RawURLEncoding.EncodeToString(headerJSON) + "." + base64.RawURLEncoding.EncodeToString(payloadJSON)
	hash := sha256.Sum256([]byte(signingInput))

	r, s, err := ecdsa.Sign(rand.Reader, k.privateKey, hash[:])
	if err != nil {
		return "", fmt.Errorf("sign vapid jwt: %w", err)
	}

	// JWS ES256 signature format is 64-byte IEEE P1363 (32-byte R || 32-byte S)
	rBytes := r.Bytes()
	sBytes := s.Bytes()

	sigBytes := make([]byte, 64)
	copy(sigBytes[32-len(rBytes):32], rBytes)
	copy(sigBytes[64-len(sBytes):64], sBytes)

	jwt := signingInput + "." + base64.RawURLEncoding.EncodeToString(sigBytes)
	return fmt.Sprintf("vapid t=%s, k=%s", jwt, k.PublicKeyBase64()), nil
}

func slicesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
