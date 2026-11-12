# Lesson 87: Cryptographic JWT Verification and CORS Origin Whitelisting

## Navigation Aid & Quick Reference
- **Domain**: Backend security architecture, Zero Trust identity verification, cryptographic JWT signature validation, CORS origin whitelisting.
- **Relevant Issues**: Issue #421, Issue #214, Issue #281, ADR-010, Lesson L28.
- **Key Files**:
  - `services/go-server/internal/middleware/jwt.go` (RS256 JWT validation, JWKS fetching/caching, PEM/JWK parsing, clock skew tolerance)
  - `services/go-server/internal/middleware/devauth.go` (RequireDevAuth gatekeeper, cryptographic JWT assertion verification, service token constant-time comparison, fail-closed unverified header rejection)
  - `services/go-server/internal/middleware/cors.go` (CORSMiddleware with allowed origins whitelist, credential protection, 403 preflight abort)
  - `services/go-server/internal/config/config.go` (ServerConfig AllowedOrigins, CFAccessAudience, CFAccessCertsURL, CFAccessClientID, CFAccessClientSecret)
- **Core Invariants**:
  - **Zero Trust Invariant**: Never trust identity claims from plain HTTP headers (`Cf-Access-Authenticated-User-Email`, `CF-Access-Client-Id`). All Cloudflare Access identities MUST be validated via RS256 signature against public JWKS keys and verified for expected Audience (`aud`) and non-expired timestamp (`exp`).
  - **Fail-Closed Gatekeeper**: Any request presenting unverified `Cf-Access-*` headers without a cryptographically valid JWT or authentic service token secret MUST fail closed with HTTP 401 Unauthorized.
  - **CORS SOP Integrity**: Origin reflection with `Access-Control-Allow-Credentials: true` is strictly prohibited for arbitrary origins. Only origins present in `ALLOWED_ORIGINS` (or defaults) may receive credentials and Allow-Origin reflection with `Vary: Origin`.

---

## 1. Context & Problem Statement

Prior implementations of `RequireDevAuth` (added in Sprint 7 / Issue #281) checked for the presence of Cloudflare Zero Trust headers:
```go
cfEmail := strings.TrimSpace(c.GetHeader("Cf-Access-Authenticated-User-Email"))
cfJwt := strings.TrimSpace(c.GetHeader("Cf-Access-Jwt-Assertion"))
cfClientId := strings.TrimSpace(c.GetHeader("CF-Access-Client-Id"))
if cfEmail != "" || cfJwt != "" || cfClientId != "" { ... }
```
Because the backend did not cryptographically verify `Cf-Access-Jwt-Assertion` or check the application audience tag (`aud`), any caller reaching the origin IP could forge `Cf-Access-Authenticated-User-Email: admin@example.com` to bypass authentication and access protected archiver, ground-truth labeler, and harvester flywheel endpoints.

Simultaneously, `CORSMiddleware` reflected arbitrary incoming `Origin` headers while setting `Access-Control-Allow-Credentials: true`, completely bypassing the Same-Origin Policy (SOP) and permitting malicious cross-origin origins to execute authenticated API requests.

---

## 2. Core Architectural Decisions

### A. Cryptographic RS256 JWT Verification & JWKS KeySet
In `middleware/jwt.go`:
- Built using 100% Go standard library (`crypto/rsa`, `crypto/sha256`, `crypto/x509`, `encoding/base64`, `encoding/json`).
- Parses and enforces `alg == "RS256"` (rejecting `"none"`, `"HS256"`, or symmetric algorithm substitution attacks).
- Implemented `KeySet` interface with `JWKSKeySet` providing thread-safe caching (1h TTL) and automatic refresh against Cloudflare Access certs endpoint (`/cdn-cgi/access/certs`). Supports both standard JWKS `keys` (`n`, `e`) and X.509 PEM certificate chains.
- Validates expiration (`exp`) and not-before (`nbf`) claims with 60-second clock skew leeway.
- Validates expected Audience (`aud`) against `CF_ACCESS_AUDIENCE`.
- Extracts caller identity from cryptographically verified payload claims (`payload.Email` or `payload.Sub`), rejecting plain email header mismatches.

### B. Fail-Closed Authentication & Service Token Verification
In `middleware/devauth.go`:
- **Primary Gate**: If `Cf-Access-Jwt-Assertion` is present, it is cryptographically verified against the configured JWKS key set.
- **Service Token Gate**: If machine-to-machine service token headers are supplied (`CF-Access-Client-Id` & `CF-Access-Client-Secret`), both are verified against configured secrets via `subtle.ConstantTimeCompare`.
- **Fail-Closed Header Injection Rejection**: If plain `Cf-Access-Authenticated-User-Email` or `CF-Access-Client-Id` headers are supplied without cryptographic proof or valid credentials, the request is immediately rejected with HTTP 401 Unauthorized.
- **Fallback Gate**: If all CF headers are absent, the developer access token fallback gate (`X-Dev-Token`, `Authorization: Bearer`, or query parameter) remains intact with constant-time verification.

### C. CORS Origin Whitelisting
In `middleware/cors.go`:
- Incoming `Origin` is evaluated against `ALLOWED_ORIGINS` (defaulting to `localhost:3000`, `localhost:3001`, `127.0.0.1:3000`, `127.0.0.1:3001`, `warded.529studio.site`, `warded-stg.529studio.site`).
- Whitelisted origins receive `Access-Control-Allow-Origin: <origin>`, `Access-Control-Allow-Credentials: true`, and `Vary: Origin`.
- Untrusted origins receive NO `Access-Control-Allow-Origin` or credentials headers.
- Untrusted preflight `OPTIONS` requests abort with HTTP 403 Forbidden.

---

## 3. Verification & Compliance
- **Unit Test Coverage**: `devauth_test.go`, `jwt_test.go`, `cors_test.go`, and `config_test.go` exhaustively test forged header rejection, cryptographic RSA verification, expired token rejection, audience mismatch rejection, untrusted origin CORS rejection, and service token authentication.
- **Live Empirical Verification**: `tools/test/verify_issue421_live.go` executed 10 live end-to-end scenarios against the compiled binary in production mode with a live mock JWKS endpoint, proving 100% pass rate.
