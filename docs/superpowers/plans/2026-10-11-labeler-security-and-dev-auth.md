# Labeler Security & Developer Authorization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Secure the shared team web labeler (`/labeler`) and all snapshot archiver endpoints (`/api/archiver/*`) with a multi-tier Developer Access Token gate to prevent unauthorized access and training data poisoning on production.

**Architecture:**
- Backend Gate: Add `RequireDevAuth` middleware in `services/go-server/internal/middleware` guarding `/api/archiver/*` via `X-Dev-Token` / Bearer token, with fail-closed behavior on staging/production.
- Frontend Gate: Add Monochromatic Dev Lock Gatekeeper in `apps/web/src/app/labeler/page.tsx` storing credentials in `sessionStorage` and transmitting `X-Dev-Token`.
- CI Lint Fix: Format `services/go-server/internal/archiver/handler.go` with `gofmt` to resolve the current red CI failure on PR #218.

**Tech Stack:** Go 1.23+, Gin, Next.js 15, React 19, TypeScript.

---

## Tasks

### Task 1: Backend Developer Auth Middleware & Route Protection
**Files:**
- Create: `services/go-server/internal/middleware/devauth.go`
- Create: `services/go-server/internal/middleware/devauth_test.go`
- Modify: `services/go-server/internal/config/config.go`
- Modify: `services/go-server/internal/middleware/cors.go`
- Modify: `services/go-server/internal/archiver/handler.go`
- Modify: `services/go-server/cmd/server/main.go`

- [ ] **Step 1: Write unit tests for `RequireDevAuth` in `middleware/devauth_test.go`**
- [ ] **Step 2: Run test to verify it fails**
- [ ] **Step 3: Implement `RequireDevAuth` in `middleware/devauth.go` and wire in `config.go` and `cors.go`**
- [ ] **Step 4: Run test to verify it passes**
- [ ] **Step 5: Guard `/api/archiver/*` routes in `cmd/server/main.go` and format `handler.go` with `gofmt`**

### Task 2: Frontend Monochromatic Dev Key Gatekeeper
**Files:**
- Modify: `apps/web/src/app/labeler/page.tsx`
- Modify: `apps/web/src/utils/labelerUtils.ts`
- Modify: `apps/web/src/utils/labelerUtils.test.ts`

- [ ] **Step 1: Add dev token helper functions and unit tests in `labelerUtils.ts` and `labelerUtils.test.ts`**
- [ ] **Step 2: Run `npm test` to verify**
- [ ] **Step 3: Implement Monochromatic Dev Lock UI and fetch header injection in `page.tsx`**
- [ ] **Step 4: Verify Next.js build and test suite**

### Task 3: End-to-End Security Simulation & CI Verification
- [ ] **Step 1: Run unauthenticated cURL to verify 401 Unauthorized**
- [ ] **Step 2: Run authenticated cURL to verify 200 OK**
- [ ] **Step 3: Run `make test-go` and `npm test`**
- [ ] **Step 4: Update living docs `docs/project_status.md` and `docs/lesson_learn.md`**
- [ ] **Step 5: Push commits to `feat/s5-t4-r2-shared-team-labeler` to update PR #218 and turn CI green**
