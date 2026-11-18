# Sprint 16 Audit Fixes Plan

## Priority 1: Critical Stability & Security Fixes
1. **[Backend/Frontend] Fix N+1 Road Status Calls (HTTP 429)**
   - **Scope**: `services/go-server/internal/roads`, `apps/web/src/components/Sidebar.tsx`
   - **Task**: Aggregate all road status calls into a single batch endpoint `GET /api/roads/status-summary`.
   - **Impact**: Eliminates HTTP 429 errors and API flooding.

2. **[UI/Desktop] Fix Corridor Overview Card Positioning**
   - **Scope**: `apps/web/src/components/CorridorOverviewCard.tsx`
   - **Task**: Reposition the card to `md:left-[400px]` or anchor it inside the desktop sidebar.
   - **Impact**: Restores functionality of search input and sidebar on desktop.

3. **[Security] Gate Swagger UI & OpenAPI in Production (SEC-01)**
   - **Scope**: `services/go-server/cmd/server/routes.go`
   - **Task**: Gate `ginSwagger` with `AppEnv != "production"` check.
   - **Impact**: Prevents information disclosure of internal API contracts.

4. **[Security] Implement HTTP Security Headers (SEC-02, SEC-03)**
   - **Scope**: `apps/web/nginx.conf`, `services/go-server/internal/middleware/headers.go`
   - **Task**: Add HSTS, CSP, and `Permissions-Policy`. Consolidate Nginx header logic into a shared snippet.
   - **Impact**: Mitigates XSS, Clickjacking, and SSL stripping risks.

5. **[Mobile] Fix Dynamic Viewport Occlusion (S15-T3 Follow-up)**
   - **Scope**: `apps/web/src/app/page.tsx`
   - **Task**: Replace `h-screen` with `h-[100dvh]`.
   - **Impact**: Ensures UI buttons (especially bottom ones) are not hidden by browser chrome.

## Execution Workflow
- **Standard**: Follow `AGENTS.md` (no hardcoded secrets, parameterized SQL).
- **TDD**: Write tests for the new aggregated status endpoint before implementation.
- **Verification**: Use Playwright to verify the desktop layout and ensure the N+1 issue is resolved on the production-equivalent staging environment.
