# Lesson 530: Strict Production CORS Gating and Trusted Proxy Hardening (SEC-04, SEC-05)

## Navigation Aid & Quick Reference
- **Domain**: Web security, CORS origin gating, Gin trusted proxy configuration, client IP spoofing protection, rate limiting integrity.
- **Relevant Issues**: Issue #530 (S18-T7), SEC-04 (Permissive CORS Origins), SEC-05 (Untrusted Proxy Defaults).
- **Key Files**:
  - `services/go-server/internal/middleware/cors.go` (`ValidateCORSOrigins`, `CORSConfig`, `CORSMiddlewareWithConfig`, `CORSMiddleware`)
  - `services/go-server/internal/config/config.go` (`ValidateCORSOrigins`, `TrustedProxies` loading, `CORS_ALLOWED_ORIGINS` fail-fast validation in production)
  - `services/go-server/cmd/server/routes.go` (`router.SetTrustedProxies(cfg.TrustedProxies)`, `CORSMiddlewareWithConfig` wiring)
  - `services/go-server/internal/middleware/cors_test.go` (Unit tests for production CORS validation, empty origin behavior, untrusted origin preflight 403)
  - `services/go-server/internal/config/config_test.go` (Unit tests for `CORS_ALLOWED_ORIGINS` precedence, `TRUSTED_PROXIES` defaults, production fail-fast error checks)
  - `services/go-server/cmd/server/server_test.go` (`TestStartup_ProductionRequiresValidCORS` verifying startup termination when origins are empty, wildcard, or loopback)
  - `services/go-server/cmd/server/abuse_guard_test.go` (`TestServer_RateLimiting_ForgedXForwardedForBlocked` verifying spoofed `X-Forwarded-For` cannot bypass rate limits)
- **Core Invariants**:
  - **Mandatory Explicit Production Origins**: In `AppEnv == "production"`, `CORS_ALLOWED_ORIGINS` (or `ALLOWED_ORIGINS`) must be explicitly provided. Omitting origins triggers an immediate fatal startup error (`log.Fatalf`).
  - **Rejection of Wildcard and Loopback in Production**: Production CORS strictly rejects `*` and loopback/localhost hostnames (`localhost`, `127.0.0.1`, `::1`, `0.0.0.0`). Every origin must be a valid `http://` or `https://` URL.
  - **Safe Empty Origin Handling**: In production, requests without an `Origin` header (direct cURL, server-to-server) do NOT receive `Access-Control-Allow-Origin: *`.
  - **Explicit Trusted Proxy Enforcement**: Gin's dangerous `0.0.0.0/0` proxy default is eliminated. If `TRUSTED_PROXIES` is unset, the router defaults to loopback only (`127.0.0.1`, `::1`), preventing client IP spoofing via crafted `X-Forwarded-For` headers.

---

## 1. Context & Vulnerability Analysis

Security audit findings SEC-04 and SEC-05 cataloged two systemic network vulnerabilities in the Go HTTP server:
1. **SEC-04 (Permissive CORS Origins & Wildcard Reflection)**:
   - When `CORS_ALLOWED_ORIGINS` was left unset in production, the server defaulted to `DefaultAllowedOrigins`, exposing endpoints to cross-origin requests from `http://localhost:3000` and `http://127.0.0.1:3000`.
   - Furthermore, non-browser requests without an `Origin` header received `Access-Control-Allow-Origin: *`, creating proxy/CDN cache poisoning hazards where cached responses with wildcard headers could be served to credentialed browsers.
2. **SEC-05 (Untrusted Proxy Defaults & Rate Limit Bypass)**:
   - Gin's `Engine.SetTrustedProxies` was never invoked. By default, Gin trusts all upstream proxies (`0.0.0.0/0`) and emits a warning log.
   - Because all proxies were trusted, `c.ClientIP()` unconditionally trusted the leftmost IP in the `X-Forwarded-For` header. An attacker sending rotating `X-Forwarded-For: <random-ip>` headers completely bypassed the per-IP token bucket rate limiter (`middleware.RateLimitMiddleware`).

---

## 2. Root Cause Discovery & Architectural Resolution

### A. Strict Production CORS Gating
- Implemented `ValidateCORSOrigins(origins []string, appEnv string) error`:
  - When `appEnv == "production"`, fails fast if `origins` is empty, contains `*`, contains localhost/loopback (`127.0.0.1`, `localhost`, `0.0.0.0`), or fails URL parsing.
  - Boot-time gating in `LoadConfig()`: logs fatal and terminates process with exit code 1 if misconfigured.
- `CORSMiddlewareWithConfig`:
  - In production, empty `Origin` requests receive zero `Access-Control-Allow-Origin` headers.
  - Cross-origin preflight `OPTIONS` requests from untrusted origins are immediately aborted with `403 Forbidden`.

### B. Trusted Proxy Hardening
- Added `TRUSTED_PROXIES` environment variable supporting comma-separated CIDRs and IP addresses.
- Defaulted when unset to loopback only (`[]string{"127.0.0.1", "::1"}`).
- Wired into `setupRouter` via `router.SetTrustedProxies(cfg.TrustedProxies)`:
  - If a direct connection originates from an untrusted remote IP (e.g. `198.51.100.1`), Gin ignores any forged `X-Forwarded-For` headers, locking `c.ClientIP()` to `198.51.100.1`.
  - When rotating spoofed headers are sent, requests accumulate in the single bucket for `198.51.100.1` and are blocked with `429 Too Many Requests`.

---

## 3. Verification & Empirical Proof

1. **Unit Tests**:
   - `internal/middleware/cors_test.go`: `TestValidateCORSOrigins` asserts production rejection of empty, wildcard, and localhost origins. `TestCORSMiddlewareWithConfig_ProductionGating` asserts empty origins get no wildcard, untrusted preflights get 403, and allowed origins get credentials.
   - `internal/config/config_test.go`: `TestLoadConfig_CORSAllowedOrigins_Precedence`, `TestLoadConfig_TrustedProxies_DefaultAndCustom`, and `TestLoadFromEnv_ProductionCORSValidation` verify configuration parsing and validation.
2. **Startup & Spoofing Integration Tests**:
   - `cmd/server/server_test.go`: `TestStartup_ProductionRequiresValidCORS` asserts process termination on empty, wildcard, and localhost CORS in production.
   - `cmd/server/abuse_guard_test.go`: `TestServer_RateLimiting_ForgedXForwardedForBlocked` demonstrates that rotating `X-Forwarded-For` from an untrusted connection fails to bypass rate limits and trips HTTP 429.
3. **Repository Gates**:
   - `make test-go && make build-go`: 100% green with `-race`.
   - `make docs-lint`: 0 new violations.
   - `make evidence-lint`: 135/135 artifacts verified.
   - `make test-model`: 146 tests passed.
