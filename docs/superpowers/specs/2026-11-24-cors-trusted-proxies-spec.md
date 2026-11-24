# Specification: Production CORS Gating and Trusted Proxy Hardening (SEC-04, SEC-05)

## 1. Problem Statement & Motivation
Security audit findings SEC-04 and SEC-05 identified two critical network exposure vulnerabilities in `services/go-server`:
1. **Permissive CORS Origins in Production (SEC-04)**: When `CORS_ALLOWED_ORIGINS` / `ALLOWED_ORIGINS` is omitted or misconfigured in production (`APP_ENV=production`), the backend defaulted to `DefaultAllowedOrigins` containing `http://localhost:3000`, `http://localhost:3001`, and `http://127.0.0.1:3000`. Furthermore, unvalidated wildcards (`*`) or non-origin requests reflected wildcard headers, risking credential leakage and cache poisoning.
2. **Unrestricted Trusted Proxy Defaults (SEC-05)**: Gin's `Engine.SetTrustedProxies` was never configured, causing Gin to trust all upstream IPs (`0.0.0.0/0`) and emit runtime security warnings. Consequently, `c.ClientIP()` unconditionally trusted incoming `X-Forwarded-For` headers from any direct client, allowing attackers to forge arbitrary client IPs and completely bypass rate limiting and CIDR metrics controls.

## 2. Technical Scope & Architecture

### A. Strict Production CORS Gating (`internal/middleware/cors.go` & `internal/config/config.go`)
1. **Validation & Fail-Fast Startup**:
   - Introduce `ValidateCORSOrigins(origins []string, appEnv string) error`.
   - In `APP_ENV=production`:
     - Explicit origins are mandatory: empty origin list fails fast.
     - Wildcard `*` is strictly forbidden.
     - Localhost/loopback origins (`localhost`, `127.0.0.1`, `0.0.0.0`) are strictly forbidden.
     - Origins must be well-formed URLs with `http://` or `https://` schemes.
   - In non-production (`dev`, `staging`, `test`):
     - Unset origins fall back safely to `DefaultAllowedOrigins`.
2. **CORS Middleware Configuration (`CORSConfig`)**:
   - `CORSMiddlewareWithConfig(cfg CORSConfig)`:
     - In production: empty `Origin` headers do not receive `Access-Control-Allow-Origin: *`.
     - Untrusted origins receive no `Access-Control-Allow-Origin` or credentials headers.
     - Untrusted preflight `OPTIONS` requests are rejected with `403 Forbidden`.
   - Backward compatibility: `CORSMiddleware(allowedOrigins ...string)` delegates with `AppEnv: "dev"`.

### B. Trusted Proxy CIDR Configuration (`cmd/server/routes.go` & `internal/config/config.go`)
1. **Configuration**:
   - Add `TRUSTED_PROXIES` environment variable parsed into `cfg.TrustedProxies []string`.
   - Default when unset: `[]string{"127.0.0.1", "::1"}` (loopback only), ensuring `0.0.0.0/0` is never trusted.
2. **Gin Integration**:
   - `router.SetTrustedProxies(cfg.TrustedProxies)` called during `setupRouter()`.
   - If empty/disabled, `router.SetTrustedProxies(nil)` disables proxy header trust entirely.

## 3. Verification Matrix
1. **CORS Unit Tests (`cors_test.go`)**:
   - Production validation: rejects empty, wildcard `*`, localhost `http://localhost:3000`.
   - Production execution: untrusted origin preflight returns 403 Forbidden; empty origin does not reflect `*`.
   - Dev execution: backward-compatible default origins and wildcard for empty origin.
2. **Trusted Proxy & Rate Limit Spoofing Tests (`abuse_guard_test.go` & `ratelimit_test.go`)**:
   - Forged `X-Forwarded-For` from untrusted remote IP does not change `c.ClientIP()`.
   - Rotating `X-Forwarded-For` headers from single remote IP triggers `429 Too Many Requests`.
   - Legitimate proxy requests from trusted CIDR forward authentic client IP correctly.
3. **Repository Regression Gates**:
   - `make test-go && make build-go` clean with `-race`.
   - `make docs-lint`, `make evidence-lint`, `make test-model`.
