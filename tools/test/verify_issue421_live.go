package main

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func base64urlEncode(b []byte) string {
	return strings.TrimRight(base64.URLEncoding.EncodeToString(b), "=")
}

func main() {
	fmt.Println("=== LIVE E2E VERIFICATION: ISSUE #421 ===")

	// 1. Generate RSA Key Pair for Mock Cloudflare Access
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		fmt.Printf("Failed to generate RSA key: %v\n", err)
		os.Exit(1)
	}

	kid := "live-test-cf-kid-2026"
	nBytes := privKey.PublicKey.N.Bytes()
	eBytes := big.NewInt(int64(privKey.PublicKey.E)).Bytes()

	jwksData := map[string]interface{}{
		"keys": []map[string]interface{}{
			{
				"kid": kid,
				"kty": "RSA",
				"alg": "RS256",
				"use": "sig",
				"n":   base64urlEncode(nBytes),
				"e":   base64urlEncode(eBytes),
			},
		},
	}

	// 2. Start Mock JWKS Server
	jwksServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/cdn-cgi/access/certs" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(jwksData)
			return
		}
		http.NotFound(w, r)
	}))
	defer jwksServer.Close()
	fmt.Printf("✓ Mock Cloudflare Access certs endpoint running on %s/cdn-cgi/access/certs\n", jwksServer.URL)

	// 3. Build & Run Backend Server
	cmdDir, _ := filepath.Abs("services/go-server")
	binPath := "/tmp/warded-backend"

	buildCmd := exec.Command("go", "build", "-o", binPath, "./cmd/server")
	buildCmd.Dir = cmdDir
	if out, err := buildCmd.CombinedOutput(); err != nil {
		fmt.Printf("Failed to build backend: %v\nOutput: %s\n", err, string(out))
		os.Exit(1)
	}

	backendPort := "3098"
	baseURL := fmt.Sprintf("http://127.0.0.1:%s", backendPort)

	backendCmd := exec.Command(binPath)
	backendCmd.Dir = cmdDir
	backendCmd.Env = append(os.Environ(),
		"PORT="+backendPort,
		"APP_ENV=production",
		"GIN_MODE=release",
		"DEV_ACCESS_TOKEN=test-super-secret-token",
		"CF_ACCESS_AUDIENCE=test-cf-audience-tag-2026",
		"CF_ACCESS_CERTS_URL="+jwksServer.URL+"/cdn-cgi/access/certs",
		"CF_ACCESS_CLIENT_ID=valid-cf-client-id",
		"CF_ACCESS_CLIENT_SECRET=valid-cf-client-secret",
		"ALLOWED_ORIGINS=http://localhost:3000,http://localhost:3001,https://warded.529studio.site",
		"ANALYZER=none",
		"STORAGE_DRIVER=local",
		"STORAGE_LOCAL_ROOT=/tmp/test-proofs",
		"CAMERAS_FILE_PATH=cameras_all.json",
		"SNAPSHOT_UPSTREAM_BASE_URL=http://127.0.0.1:3098",
	)

	_ = os.MkdirAll("/tmp/test-proofs", 0755)

	if err := backendCmd.Start(); err != nil {
		fmt.Printf("Failed to start backend: %v\n", err)
		os.Exit(1)
	}
	defer func() {
		_ = backendCmd.Process.Kill()
		_ = backendCmd.Wait()
	}()

	// Wait for healthz probe
	fmt.Printf("Waiting for live backend at %s/healthz ...\n", baseURL)
	ready := false
	httpClient := &http.Client{Timeout: 1 * time.Second}
	for range 30 {
		resp, err := httpClient.Get(baseURL + "/healthz")
		if err == nil && resp.StatusCode == http.StatusOK {
			_ = resp.Body.Close()
			ready = true
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !ready {
		fmt.Println("❌ Backend failed to start within timeout")
		os.Exit(1)
	}
	fmt.Println("✓ Backend is alive and serving on port " + backendPort + " in production mode\n")

	// Execute Test Scenarios
	type Scenario struct {
		Name           string
		Req            *http.Request
		ExpectedStatus int
		Check          func(status int, headers http.Header, body string) bool
	}

	// Helper to make JWT
	makeJWT := func(aud, email string, expDelta time.Duration) string {
		header := map[string]string{
			"alg": "RS256",
			"kid": kid,
			"typ": "JWT",
		}
		payload := map[string]interface{}{
			"aud":   aud,
			"email": email,
			"exp":   time.Now().Add(expDelta).Unix(),
			"nbf":   time.Now().Add(-1 * time.Minute).Unix(),
		}
		hBytes, _ := json.Marshal(header)
		pBytes, _ := json.Marshal(payload)
		hB64 := base64urlEncode(hBytes)
		pB64 := base64urlEncode(pBytes)
		signingInput := hB64 + "." + pB64
		hashed := sha256.Sum256([]byte(signingInput))
		sig, _ := rsa.SignPKCS1v15(rand.Reader, privKey, crypto.SHA256, hashed[:])
		return signingInput + "." + base64urlEncode(sig)
	}

	validJWT := makeJWT("test-cf-audience-tag-2026", "verified-engineer@529studio.site", 1*time.Hour)
	wrongAudJWT := makeJWT("wrong-aud-tag", "user@529studio.site", 1*time.Hour)

	req1, _ := http.NewRequest(http.MethodGet, baseURL+"/api/archiver/snapshots", nil)
	req1.Header.Set("Cf-Access-Authenticated-User-Email", "attacker@evil.com")

	req2, _ := http.NewRequest(http.MethodGet, baseURL+"/api/archiver/snapshots", nil)
	req2.Header.Set("Cf-Access-Jwt-Assertion", "forged.header.signature")

	req3, _ := http.NewRequest(http.MethodGet, baseURL+"/api/archiver/snapshots", nil)
	req3.Header.Set("CF-Access-Client-Id", "attacker-client-id")

	req4, _ := http.NewRequest(http.MethodGet, baseURL+"/api/archiver/snapshots", nil)
	req4.Header.Set("Cf-Access-Jwt-Assertion", validJWT)

	req5, _ := http.NewRequest(http.MethodGet, baseURL+"/api/archiver/snapshots", nil)
	req5.Header.Set("Cf-Access-Jwt-Assertion", wrongAudJWT)

	req6, _ := http.NewRequest(http.MethodGet, baseURL+"/api/archiver/snapshots", nil)
	req6.Header.Set("CF-Access-Client-Id", "valid-cf-client-id")
	req6.Header.Set("CF-Access-Client-Secret", "valid-cf-client-secret")

	req7, _ := http.NewRequest(http.MethodGet, baseURL+"/api/archiver/snapshots", nil)
	req7.Header.Set("X-Dev-Token", "test-super-secret-token")

	req8, _ := http.NewRequest(http.MethodGet, baseURL+"/api/cameras", nil)
	req8.Header.Set("Origin", "http://evil-phishing-site.example.com")

	req9, _ := http.NewRequest(http.MethodOptions, baseURL+"/api/cameras", nil)
	req9.Header.Set("Origin", "http://evil-phishing-site.example.com")

	req10, _ := http.NewRequest(http.MethodGet, baseURL+"/api/cameras", nil)
	req10.Header.Set("Origin", "http://localhost:3000")

	scenarios := []Scenario{
		{
			Name:           "Scenario 1: Plain Cf-Access-Authenticated-User-Email header injection fails closed (401)",
			Req:            req1,
			ExpectedStatus: http.StatusUnauthorized,
			Check: func(status int, h http.Header, body string) bool {
				return strings.Contains(body, "unverified Cloudflare Access email header rejected")
			},
		},
		{
			Name:           "Scenario 2: Forged Cloudflare JWT assertion rejected (401)",
			Req:            req2,
			ExpectedStatus: http.StatusUnauthorized,
			Check: func(status int, h http.Header, body string) bool {
				return strings.Contains(body, "unauthorized")
			},
		},
		{
			Name:           "Scenario 3: Forged CF-Access-Client-Id header without secret rejected (401)",
			Req:            req3,
			ExpectedStatus: http.StatusUnauthorized,
			Check: func(status int, h http.Header, body string) bool {
				return strings.Contains(body, "invalid or unverified Cloudflare Access service token credentials")
			},
		},
		{
			Name:           "Scenario 4: Cryptographically valid Cloudflare Access JWT succeeds (200 OK)",
			Req:            req4,
			ExpectedStatus: http.StatusOK,
			Check: func(status int, h http.Header, body string) bool {
				return true
			},
		},
		{
			Name:           "Scenario 5: JWT with mismatched audience rejected (401)",
			Req:            req5,
			ExpectedStatus: http.StatusUnauthorized,
			Check: func(status int, h http.Header, body string) bool {
				return strings.Contains(body, "jwt audience mismatch")
			},
		},
		{
			Name:           "Scenario 6: Valid Service Token credentials (ID + Secret) succeeds (200 OK)",
			Req:            req6,
			ExpectedStatus: http.StatusOK,
			Check: func(status int, h http.Header, body string) bool {
				return true
			},
		},
		{
			Name:           "Scenario 7: Fallback X-Dev-Token authentication succeeds (200 OK)",
			Req:            req7,
			ExpectedStatus: http.StatusOK,
			Check: func(status int, h http.Header, body string) bool {
				return true
			},
		},
		{
			Name:           "Scenario 8: Untrusted Origin receives NO Access-Control-Allow-Origin or Credentials",
			Req:            req8,
			ExpectedStatus: http.StatusOK,
			Check: func(status int, h http.Header, body string) bool {
				return h.Get("Access-Control-Allow-Origin") == "" && h.Get("Access-Control-Allow-Credentials") == ""
			},
		},
		{
			Name:           "Scenario 9: Untrusted Origin preflight OPTIONS aborted with 403 Forbidden",
			Req:            req9,
			ExpectedStatus: http.StatusForbidden,
			Check: func(status int, h http.Header, body string) bool {
				return h.Get("Access-Control-Allow-Origin") == ""
			},
		},
		{
			Name:           "Scenario 10: Whitelisted Origin (http://localhost:3000) reflected with credentials",
			Req:            req10,
			ExpectedStatus: http.StatusOK,
			Check: func(status int, h http.Header, body string) bool {
				return h.Get("Access-Control-Allow-Origin") == "http://localhost:3000" &&
					h.Get("Access-Control-Allow-Credentials") == "true" &&
					h.Get("Vary") == "Origin"
			},
		},
	}

	testClient := &http.Client{Timeout: 5 * time.Second}
	allPassed := true

	for _, s := range scenarios {
		resp, err := testClient.Do(s.Req)
		if err != nil {
			fmt.Printf("❌ %s: HTTP request failed: %v\n", s.Name, err)
			allPassed = false
			continue
		}
		bodyBytes, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		body := string(bodyBytes)

		passed := (resp.StatusCode == s.ExpectedStatus) && s.Check(resp.StatusCode, resp.Header, body)
		if passed {
			fmt.Printf("✅ PASS: %s (Status: %d)\n", s.Name, resp.StatusCode)
		} else {
			fmt.Printf("❌ FAIL: %s (Expected %d, Got %d, Body: %s)\n", s.Name, s.ExpectedStatus, resp.StatusCode, body)
			allPassed = false
		}
	}

	// Remove the temporary python file
	_ = os.Remove("tools/test/verify_issue421_live.py")

	fmt.Println("\n=== SUMMARY OF LIVE MANUAL TEST RESULTS ===")
	if allPassed {
		fmt.Println("🎉 ALL 10 E2E LIVE SCENARIOS PASSED VERIFIABLY!")
	} else {
		fmt.Println("❌ SOME SCENARIOS FAILED!")
		os.Exit(1)
	}
}
