# Application Security Audit Assessment: ProjectKV (Warded)

**Date**: 2026-11-17  
**Scope**: Live Production Deployment (`https://warded.529studio.site`) & Codebase (`services/go-server/`, `apps/web/`, deployment manifests)  
**Auditor**: Independent Application Security Auditor  
**Status**: Defensive Audit Complete  

---

## 1. Executive Security Posture Summary

- **Overall Security Posture Rating**: **MEDIUM RISK**
- **Architecture & Codebase Rigor**: **STRONG**
  - Parameterized hand-written SQL queries eliminate SQL injection across all stores (`subscription`, `verdict`, `history`, `capacity`, `weather`).
  - Zero Trust Access (`RequireDevAuth`) strictly enforces RS256 JWT cryptographic validation against Cloudflare keysets and prevents spoofed email headers.
  - Snapshot proxying and tile proxy enforce strict query escaping and coordinate bounds, preventing SSRF and path traversal.
  - Response scrubbing middleware (`SecretSanitizer`) proactively redacts Google API keys, AWS/R2 credentials, and Telegram bot tokens from JSON and text streams.
  - Zero hardcoded secrets detected in git tracking; `scripts/check-public-safe.sh` passed 100%.
  - Zero NPM package vulnerabilities reported across client dependencies.
- **Primary Attack Surface Deficits**:
  - **Reconnaissance Exposure**: Swagger UI and full OpenAPI 2.0 specifications are publicly accessible in production without authentication.
  - **Transport & Client Hardening Deficits**: Missing `Strict-Transport-Security` (HSTS), missing `Content-Security-Policy` (CSP), and missing `Permissions-Policy`.
  - **Nginx Header Inheritance Defect**: `add_header` in static cache location blocks suppresses server-level security headers for static JS chunks and assets.
  - **Overly Permissive Production CORS Fallback**: Unconfigured `ALLOWED_ORIGINS` defaults to permitting `http://localhost:3000` with `Access-Control-Allow-Credentials: true`.
  - **Gin Untrusted Proxy Configuration**: `gin.Default()` lacks explicit trusted proxy declarations, posing potential IP spoofing risks to the token-bucket rate limiter.

---

## 2. Findings Matrix

| ID | Severity | Category | Target / Component | Impact | Remediation |
|---|---|---|---|---|---|
| **SEC-01** | **Medium** | Information Disclosure | Production Endpoint (`/swagger/*`, `doc.json`) | Unauthenticated enumeration of internal endpoints, data contracts, and schemas. | Restrict Swagger routes to non-production environments or gate behind `RequireDevAuth`. |
| **SEC-02** | **Medium** | Defense-in-Depth | Production Infrastructure / Nginx & Gin | Absence of HSTS and CSP leaves client open to SSL stripping and script injection risks. | Add `Strict-Transport-Security`, `Content-Security-Policy`, and `Permissions-Policy` headers. |
| **SEC-03** | **Medium** | Configuration Defect | `apps/web/nginx.conf` | Location-level `add_header Cache-Control` silently drops security headers on all static JS/media. | Consolidate security headers into a shared snippet included in all `location` blocks. |
| **SEC-04** | **Low** | Cross-Origin Security | `services/go-server/internal/middleware/cors.go` | Default CORS whitelist trusts `localhost:3000` with credentials in production if unset. | Disallow localhost origins when `APP_ENV=production` or enforce explicit production domain list. |
| **SEC-05** | **Low** | Rate Limiting / Spoofing | `services/go-server/cmd/server/routes.go` | Untrusted proxy default in Gin may allow spoofing `ClientIP()` to evade rate limits. | Call `router.SetTrustedProxies()` with Cloudflare CIDR blocks or read `CF-Connecting-IP`. |
| **SEC-06** | **Low** | API Hardening | `services/go-server/cmd/server/routes.go` | Backend `/api/*` routes lack baseline defensive headers (`nosniff`, `frame-ancestors`). | Attach security header middleware to the Gin root engine. |

---

## 3. Detailed Technical Findings

### SEC-01: Public Exposure of Swagger UI & OpenAPI Specification in Production
- **Severity**: **Medium** (CVSS: 5.3 - `CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:L/I:N/A:N`)
- **Target**: `https://warded.529studio.site/swagger/index.html` & `/swagger/doc.json`
- **Description**: The interactive Swagger documentation UI and its underlying raw JSON specification (`/swagger/doc.json`) are enabled by default and exposed publicly without any authentication gate.
- **Empirical Evidence**:
  ```bash
  $ curl -sI "https://warded.529studio.site/swagger/index.html"
  HTTP/2 200
  content-type: text/html; charset=utf-8
  server: cloudflare

  $ curl -s "https://warded.529studio.site/swagger/doc.json" | head -n 12
  {
      "swagger": "2.0",
      "info": {
          "description": "Ho Chi Minh City Traffic Camera & Flood Alert API",
          "title": "Camera View API",
          "version": "1.0"
      }, ...
  ```
- **Code Trace**: In `services/go-server/cmd/server/routes.go` (lines 90-101), `router.GET("/swagger/*any", ...)` is mounted unconditionally on the root engine regardless of `cfg.AppEnv`.
- **Risk Impact**: Facilitates automated reconnaissance. Attackers can effortlessly discover sensitive endpoints (e.g., `/api/archiver/*`, `/api/subscriptions/*`), query schemas, and internal naming conventions.
- **Recommended Defensive Fix**:
  Guard Swagger route registration with environment checks (`if cfg.AppEnv != "production"`), or protect the endpoint behind `devAuth` middleware.

---

### SEC-02: Missing Critical Transport & Content Security Headers (HSTS, CSP, Permissions-Policy)
- **Severity**: **Medium** (CVSS: 4.8 - `CVSS:3.1/AV:N/AC:H/PR:N/UI:R/S:U/C:L/I:L/A:N`)
- **Target**: `https://warded.529studio.site/` & all API sub-paths
- **Description**: While Cloudflare automatically redirects HTTP to HTTPS, the server never instructs the browser to enforce HTTPS exclusively via `Strict-Transport-Security` (HSTS). Additionally, no `Content-Security-Policy` (CSP) or `Permissions-Policy` headers are returned.
- **Empirical Evidence**:
  ```bash
  $ curl -sI "https://warded.529studio.site/"
  HTTP/2 200
  strict-transport-security: [ABSENT]
  content-security-policy: [ABSENT]
  permissions-policy: [ABSENT]
  x-content-type-options: nosniff
  x-frame-options: SAMEORIGIN
  ```
- **Risk Impact**: First-time visitors over compromised networks (e.g., public Wi-Fi) could be vulnerable to SSL stripping before the 301 redirect occurs. Lack of CSP removes browser-side containment against unexpected script execution or clickjacking in modern browsers.
- **Recommended Defensive Fix**:
  1. Add HSTS: `Strict-Transport-Security: max-age=31536000; includeSubDomains; preload`.
  2. Implement a strict CSP tailored for Next.js static export:
     `Content-Security-Policy: default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data: https: blob:; connect-src 'self' https: wss:; frame-ancestors 'none';`.
  3. Add `Permissions-Policy: camera=(), microphone=(), geolocation=()`.

---

### SEC-03: Nginx `add_header` Masking Drops Security Headers on Static Assets
- **Severity**: **Medium** (CVSS: 4.3 - `CVSS:3.1/AV:N/AC:L/PR:N/UI:R/S:U/C:N/I:L/A:N`)
- **Target**: `apps/web/nginx.conf` (`/_next/static/*`, `*.(ico|css|js|png)`)
- **Description**: In Nginx, defining an `add_header` directive within a nested `location` block completely disables inheritance of any `add_header` directives declared in the parent `server` block. As a result, static chunks, scripts, and images lack all baseline security headers.
- **Empirical Evidence**:
  ```bash
  $ curl -sI "https://warded.529studio.site/wared-icon.png"
  HTTP/2 200
  content-type: image/png
  cache-control: public, max-age=604800
  x-content-type-options: [MISSING]
  x-frame-options: [MISSING]
  referrer-policy: [MISSING]
  ```
- **Code Trace**: In `apps/web/nginx.conf` (lines 33-44), `location /_next/static/` and `location ~* \.(ico|css|js...)` declare `add_header Cache-Control ...`, silently stripping lines 27-30 (`X-Frame-Options`, `X-Content-Type-Options`, `Referrer-Policy`).
- **Risk Impact**: Static assets and JavaScript bundles are served without MIME-sniffing protection (`nosniff`), exposing clients to MIME-type confusion attacks in older user-agents.
- **Recommended Defensive Fix**:
  Create an Nginx configuration snippet (e.g. `/etc/nginx/security-headers.conf`) and `include` it inside every `location` block, or repeat the security headers explicitly.

---

### SEC-04: Default CORS Policy Trusts Localhost with Credentials in Production
- **Severity**: **Low** (CVSS: 3.7 - `CVSS:3.1/AV:N/AC:H/PR:N/UI:R/S:U/C:L/I:N/A:N`)
- **Target**: `services/go-server/internal/middleware/cors.go`
- **Description**: `DefaultAllowedOrigins` includes `http://localhost:3000` and `http://127.0.0.1:3000`. When `ALLOWED_ORIGINS` environment variable is not explicitly populated in production, these development origins remain active and permit credentialed cross-origin requests.
- **Empirical Evidence**:
  ```bash
  $ curl -sI -H "Origin: http://localhost:3000" "https://warded.529studio.site/api/cameras"
  HTTP/2 200
  access-control-allow-origin: http://localhost:3000
  access-control-allow-credentials: true
  vary: Origin
  ```
- **Code Trace**: In `services/go-server/internal/middleware/cors.go` (lines 11-18, 23-26), `DefaultAllowedOrigins` fallback is applied unconditionally without inspecting `AppEnv`.
- **Risk Impact**: If an authenticated operator accesses a malicious web page while running a local development server on port 3000, cross-origin authenticated queries could theoretically read sensitive API responses.
- **Recommended Defensive Fix**:
  In `CORSMiddleware`, check `cfg.AppEnv`: if production or staging and `ALLOWED_ORIGINS` is unset, restrict allowed origins strictly to the canonical domain (`https://warded.529studio.site`).

---

### SEC-05: Missing Gin Trusted Proxies & Rate Limiter IP Spoofing Risk
- **Severity**: **Low** (CVSS: 3.1 - `CVSS:3.1/AV:N/AC:H/PR:N/UI:N/S:U/C:N/I:N/A:L`)
- **Target**: `services/go-server/cmd/server/routes.go` (`RateLimitMiddleware`)
- **Description**: `gin.Default()` is initialized without specifying `router.SetTrustedProxies(...)`. Gin trusts all upstream proxies by default. When `c.ClientIP()` is called by `RateLimitMiddleware`, Gin reads `X-Forwarded-For`.
- **Code Trace**: `services/go-server/internal/middleware/ratelimit.go:147` relies on `c.ClientIP()`. Without trusted proxy bounds, an attacker might craft arbitrary headers to rotate perceived IPs and evade token-bucket rate limiting.
- **Recommended Defensive Fix**:
  Configure `router.SetTrustedProxies([]string{"127.0.0.1", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"})` or bind trusted proxies to Cloudflare IP ranges. Alternatively, read the immutable `CF-Connecting-IP` header set by Cloudflare edge.

---

### SEC-06: Backend API Endpoints Omit Baseline HTTP Security Headers
- **Severity**: **Low** (CVSS: 2.6 - `CVSS:3.1/AV:N/AC:H/PR:N/UI:R/S:U/C:N/I:L/A:N`)
- **Target**: Traefik & Go Server `/api/*` routes
- **Description**: Requests matching `/api/*` are routed directly to `warded-backend`. Unlike Nginx, the Gin router does not set `X-Content-Type-Options: nosniff` or `X-Frame-Options: DENY` on API responses.
- **Empirical Evidence**:
  API responses have `content-type: application/json; charset=utf-8` but lack `x-content-type-options: nosniff`.
- **Recommended Defensive Fix**:
  Add a lightweight Gin middleware in `services/go-server/internal/middleware/headers.go` setting `X-Content-Type-Options: nosniff` and `X-Frame-Options: DENY` on all responses.

---

## 4. Concrete Backlog Issues for PO Sprint 16 Planning

To resolve these findings systematically in Sprint 16, the following tasks are proposed:

### Issue 1: [SEC] Gate Swagger UI & OpenAPI Specification in Production
- **Scope**: `services/go-server/cmd/server/routes.go`, `services/go-server/internal/config/`
- **Objective**: Prevent unauthenticated discovery of system APIs.
- **Acceptance Criteria**:
  - If `APP_ENV == "production"`, `/swagger` and `/swagger/*any` must return `404 Not Found` unless the request passes `RequireDevAuth` (Cloudflare Access or `DEV_ACCESS_TOKEN`).
  - Unit tests verify Swagger endpoints return 404 in production mode and 200 in development mode.

### Issue 2: [SEC] Implement Robust Security Headers (HSTS, CSP, Permissions-Policy)
- **Scope**: `apps/web/nginx.conf`, `services/go-server/internal/middleware/headers.go`
- **Objective**: Harden browser-side transport and injection protections.
- **Acceptance Criteria**:
  - Add `Strict-Transport-Security: max-age=31536000; includeSubDomains; preload` to all Nginx and backend responses.
  - Add restrictive `Content-Security-Policy` and `Permissions-Policy`.
  - Fix Nginx nested `location` header dropping by creating `include /etc/nginx/security-headers.conf` or duplicating headers in static blocks.
  - Verify static assets (`/_next/static/*`, `.png`) return `X-Content-Type-Options: nosniff`.

### Issue 3: [SEC] Harden Production CORS and Eliminate Localhost Trust
- **Scope**: `services/go-server/internal/middleware/cors.go`
- **Objective**: Prevent cross-origin access from arbitrary local ports in production.
- **Acceptance Criteria**:
  - In `production` / `staging`, `DefaultAllowedOrigins` must NOT include `localhost` or `127.0.0.1`.
  - When `ALLOWED_ORIGINS` is unset in production, only the canonical production domain (`https://warded.529studio.site`) is allowed.
  - Automated test verifying `http://localhost:3000` is rejected with 403 on OPTIONS when `APP_ENV=production`.

### Issue 4: [SEC] Configure Gin Trusted Proxies & Real-IP Resolution
- **Scope**: `services/go-server/cmd/server/routes.go`, `services/go-server/internal/middleware/ratelimit.go`
- **Objective**: Protect rate limiting against client IP spoofing behind reverse proxies.
- **Acceptance Criteria**:
  - `router.SetTrustedProxies()` is configured with private subnet ranges and Cloudflare proxies.
  - `RateLimitMiddleware` prefers `CF-Connecting-IP` when present, falling back to verified `c.ClientIP()`.
  - Unit test verifying spoofed `X-Forwarded-For` cannot manipulate rate limit buckets when behind trusted proxies.
