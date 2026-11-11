package middleware

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

// JWTHeader represents the header of a signed JWT assertion.
type JWTHeader struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	Typ string `json:"typ"`
}

// JWTPayload represents standard Cloudflare Access JWT claims.
type JWTPayload struct {
	Aud           interface{} `json:"aud"`
	Email         string      `json:"email"`
	Sub           string      `json:"sub"`
	Iss           string      `json:"iss"`
	Exp           int64       `json:"exp"`
	Nbf           int64       `json:"nbf"`
	Iat           int64       `json:"iat"`
	Type          string      `json:"type"`
	IdentityNonce string      `json:"identity_nonce"`
}

// AudienceMatches verifies that expectedAud is present in payload.Aud (string, []string, or []interface{}).
func (p *JWTPayload) AudienceMatches(expectedAud string) bool {
	if expectedAud == "" {
		return true
	}
	switch v := p.Aud.(type) {
	case string:
		return v == expectedAud
	case []interface{}:
		for _, item := range v {
			if s, ok := item.(string); ok && s == expectedAud {
				return true
			}
		}
	case []string:
		for _, s := range v {
			if s == expectedAud {
				return true
			}
		}
	}
	return false
}

// KeySet retrieves an RSA public key by kid for signature verification.
type KeySet interface {
	GetPublicKey(ctx context.Context, kid string) (*rsa.PublicKey, error)
}

// StaticKeySet satisfies KeySet with an in-memory map of kid to *rsa.PublicKey (ideal for unit testing).
type StaticKeySet struct {
	Keys map[string]*rsa.PublicKey
}

// GetPublicKey looks up the public key in the static map.
func (s *StaticKeySet) GetPublicKey(_ context.Context, kid string) (*rsa.PublicKey, error) {
	if s.Keys == nil {
		return nil, errors.New("no public keys configured in static key set")
	}
	key, ok := s.Keys[kid]
	if !ok {
		return nil, fmt.Errorf("public key with kid %q not found", kid)
	}
	return key, nil
}

// cfJWKSResponse models Cloudflare Access /cdn-cgi/access/certs response format.
type cfJWKSResponse struct {
	Keys []struct {
		Kid string `json:"kid"`
		Kty string `json:"kty"`
		Alg string `json:"alg"`
		N   string `json:"n"`
		E   string `json:"e"`
	} `json:"keys"`
	PublicCert *struct {
		Kid  string `json:"kid"`
		Cert string `json:"cert"`
	} `json:"public_cert"`
	PublicCerts []struct {
		Kid  string `json:"kid"`
		Cert string `json:"cert"`
	} `json:"public_certs"`
}

// JWKSKeySet dynamically fetches and caches Cloudflare Access public keys with anti-stampede synchronization.
type JWKSKeySet struct {
	certsURL   string
	httpClient *http.Client
	mu         sync.RWMutex
	keys       map[string]*rsa.PublicKey
	fetchedAt  time.Time
	ttl        time.Duration
}

// NewJWKSKeySet creates a JWKSKeySet pointing to the given certs URL.
func NewJWKSKeySet(certsURL string, httpClient *http.Client) *JWKSKeySet {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Second}
	}
	return &JWKSKeySet{
		certsURL:   certsURL,
		httpClient: httpClient,
		keys:       make(map[string]*rsa.PublicKey),
		ttl:        1 * time.Hour,
	}
}

// GetPublicKey returns the RSA public key for the requested kid, fetching or refreshing if needed.
func (ks *JWKSKeySet) GetPublicKey(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	ks.mu.RLock()
	key, ok := ks.keys[kid]
	fresh := !ks.fetchedAt.IsZero() && time.Since(ks.fetchedAt) < ks.ttl
	ks.mu.RUnlock()

	if ok && fresh {
		return key, nil
	}

	ks.mu.Lock()
	defer ks.mu.Unlock()

	// Double-check under write lock
	key, ok = ks.keys[kid]
	if ok && !ks.fetchedAt.IsZero() && time.Since(ks.fetchedAt) < ks.ttl {
		return key, nil
	}

	if err := ks.refreshKeys(ctx); err != nil {
		if ok {
			// Serve stale key if network fetch fails
			return key, nil
		}
		return nil, fmt.Errorf("failed to fetch public keys from %s: %w", ks.certsURL, err)
	}

	key, ok = ks.keys[kid]
	if !ok {
		return nil, fmt.Errorf("public key with kid %q not found in JWKS", kid)
	}
	return key, nil
}

func (ks *JWKSKeySet) refreshKeys(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ks.certsURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "projectKV-go-server/devauth")

	resp, err := ks.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code %d from certs endpoint", resp.StatusCode)
	}

	var payload cfJWKSResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return fmt.Errorf("failed to decode certs response: %w", err)
	}

	newKeys := make(map[string]*rsa.PublicKey)

	// 1. Parse JWKS format keys
	for _, k := range payload.Keys {
		if k.Kid == "" || k.N == "" || k.E == "" {
			continue
		}
		pubKey, err := parseRSAPublicKeyFromJWK(k.N, k.E)
		if err == nil && pubKey != nil {
			newKeys[k.Kid] = pubKey
		}
	}

	// 2. Parse PEM X.509 certs format if present
	for _, c := range payload.PublicCerts {
		if c.Kid == "" || c.Cert == "" {
			continue
		}
		if _, exists := newKeys[c.Kid]; !exists {
			pubKey, err := parseRSAPublicKeyFromCert(c.Cert)
			if err == nil && pubKey != nil {
				newKeys[c.Kid] = pubKey
			}
		}
	}
	if payload.PublicCert != nil && payload.PublicCert.Kid != "" && payload.PublicCert.Cert != "" {
		if _, exists := newKeys[payload.PublicCert.Kid]; !exists {
			pubKey, err := parseRSAPublicKeyFromCert(payload.PublicCert.Cert)
			if err == nil && pubKey != nil {
				newKeys[payload.PublicCert.Kid] = pubKey
			}
		}
	}

	if len(newKeys) == 0 {
		return errors.New("no valid RSA public keys found in certs response")
	}

	ks.keys = newKeys
	ks.fetchedAt = time.Now()
	return nil
}

// DecodeBase64URL decodes a base64url-encoded string (with or without padding).
func DecodeBase64URL(s string) ([]byte, error) {
	s = strings.TrimRight(s, "=")
	return base64.RawURLEncoding.DecodeString(s)
}

func parseRSAPublicKeyFromJWK(nStr, eStr string) (*rsa.PublicKey, error) {
	nBytes, err := DecodeBase64URL(nStr)
	if err != nil {
		return nil, fmt.Errorf("invalid modulus n: %w", err)
	}
	eBytes, err := DecodeBase64URL(eStr)
	if err != nil {
		return nil, fmt.Errorf("invalid exponent e: %w", err)
	}
	n := new(big.Int).SetBytes(nBytes)
	var e int
	for _, b := range eBytes {
		e = (e << 8) | int(b)
	}
	if e == 0 {
		return nil, errors.New("invalid exponent: 0")
	}
	return &rsa.PublicKey{
		N: n,
		E: e,
	}, nil
}

func parseRSAPublicKeyFromCert(certPEM string) (*rsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		return nil, errors.New("failed to parse PEM block from certificate")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate: %w", err)
	}
	rsaPub, ok := cert.PublicKey.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("certificate public key is not RSA")
	}
	return rsaPub, nil
}

// VerifyCloudflareJWT cryptographically verifies a Cloudflare Access RS256 JWT assertion.
func VerifyCloudflareJWT(ctx context.Context, tokenString string, expectedAud string, keySet KeySet) (*JWTPayload, error) {
	tokenString = strings.TrimSpace(tokenString)
	tokenString = strings.TrimPrefix(tokenString, "Bearer ")
	tokenString = strings.TrimPrefix(tokenString, "bearer ")
	tokenString = strings.TrimSpace(tokenString)

	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 {
		return nil, errors.New("jwt must consist of 3 parts separated by dots")
	}

	headerBytes, err := DecodeBase64URL(parts[0])
	if err != nil {
		return nil, fmt.Errorf("invalid jwt header encoding: %w", err)
	}
	var header JWTHeader
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return nil, fmt.Errorf("invalid jwt header json: %w", err)
	}

	if header.Alg != "RS256" {
		return nil, fmt.Errorf("unsupported jwt algorithm: %q, expected RS256", header.Alg)
	}
	if header.Kid == "" {
		return nil, errors.New("jwt header missing kid")
	}

	payloadBytes, err := DecodeBase64URL(parts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid jwt payload encoding: %w", err)
	}
	var payload JWTPayload
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return nil, fmt.Errorf("invalid jwt payload json: %w", err)
	}

	now := time.Now().Unix()
	const clockSkewLeeway = 60 // 60 seconds tolerance for clock skew
	if payload.Exp > 0 && now > (payload.Exp+clockSkewLeeway) {
		return nil, errors.New("jwt has expired")
	}
	if payload.Nbf > 0 && now < (payload.Nbf-clockSkewLeeway) {
		return nil, errors.New("jwt is not yet valid (nbf)")
	}

	if expectedAud != "" && !payload.AudienceMatches(expectedAud) {
		return nil, fmt.Errorf("jwt audience mismatch")
	}

	if keySet == nil {
		return nil, errors.New("no key set configured to verify jwt signature")
	}

	pubKey, err := keySet.GetPublicKey(ctx, header.Kid)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve public key for kid %q: %w", header.Kid, err)
	}

	signedContent := parts[0] + "." + parts[1]
	hasher := sha256.New()
	hasher.Write([]byte(signedContent))
	hashed := hasher.Sum(nil)

	sigBytes, err := DecodeBase64URL(parts[2])
	if err != nil {
		return nil, fmt.Errorf("invalid jwt signature encoding: %w", err)
	}

	if err := rsa.VerifyPKCS1v15(pubKey, crypto.SHA256, hashed, sigBytes); err != nil {
		return nil, fmt.Errorf("cryptographic signature verification failed: %w", err)
	}

	return &payload, nil
}
