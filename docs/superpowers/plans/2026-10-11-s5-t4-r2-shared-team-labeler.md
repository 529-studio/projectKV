# S5-T4 R2 Blob Storage Archiver + Shared Team Labeler Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement storage listing abstraction, backend archiver proxy routes, and a collaborative shared web labeler interface reading snapshot frames from Cloudflare R2 / local storage to eliminate manual frame copying across the team.

**Architecture:** Extend `storage.Storage` with a `List(ctx, prefix)` method implemented for both `LocalStorage` (filesystem walk) and `R2Storage` (`ListObjectsV2`), expose HTTP routes `/api/archiver/snapshots`, `/api/archiver/snapshots/raw`, and `/api/archiver/labels` in `services/go-server/internal/archiver`, build a monochromatic keyboard-driven Next.js labeler at `apps/web/src/app/labeler/page.tsx`, and enhance `tools/eval/labeler.html` to support API-driven remote labeling.

**Tech Stack:** Go 1.24, Gin, AWS SDK Go v2 (Cloudflare R2), Next.js 15, React 19, Tailwind CSS.

**Spec:** Issue #214 (`[infra + web] S5-T4 — R2 blob storage for archiver + shared team labeler`), ADR-006 §3b & §4.

## Effort & Delivery Window Estimate (Rule 2 Mandate)
- **Active Effort:** 1.5 hours.
- **Expected Delivery Window:** ~2 hours (including full unit tests, swagger generation, and live R2 verification).

## Global Constraints
- **AGENTS.md Rule 1**: Minimalist, monochromatic, zero-emoji in user-facing UI and backend logs. Bilingual i18n (`vi` default with full diacritics, `en` secondary). 100% English code, docs, and tooling.
- **AGENTS.md Rule 3**: Sound solutions over shortcuts. Support dual storage drivers (`STORAGE_DRIVER=local|r2`).
- **AGENTS.md Rule 5**: Zero direct push to `main` or `dev`. Feature branch `feat/s5-t4-r2-shared-team-labeler` targeting `dev`.
- **AGENTS.md Rule 7**: Leaf modulith pattern in `services/go-server`. `storage` remains leaf; `archiver` imports `storage, hotset`. Run `make swagger` on API route changes.
- **AGENTS.md Rule 8**: Zero hardcoded secrets. Load R2 credentials strictly from `.env` or environment.
- **AGENTS.md Rule 9**: PR description MUST start with `Close #214`. Follow `.github/pull_request_template.md`.
- **Justification Note**: PR description and docs MUST explicitly state that R2 adoption is justified for shared team labeling, NOT because the system reached the ADR-004 §D7 disk capacity threshold.

## Review Focus
1. `storage.Storage` interface extension (`List`) must be backward-compatible and implemented for both `LocalStorage` and `R2Storage`.
2. `/api/archiver/snapshots/raw?key=...` must sanitize paths against directory traversal (`ErrInvalidPath`), serve valid JPEG headers, and bypass Cloudflare Access Zero Trust redirects for team members.
3. `/api/archiver/labels` must persist label JSON to storage without overwriting raw image frames.
4. Next.js `/labeler` UI must support standard keyboard hotkeys (1-3 flood, Q-W-E-R traffic, A-S-D weather, Z-X-C time of day, Enter save/next) with zero emojis.
5. All automated test suites (`make test-go`, `npm test`) must pass with 100% clean check.

---

### Task 1: Extend Storage Abstraction with Object Listing (`internal/storage`)

**Files:**
- Modify: `services/go-server/internal/storage/storage.go`
- Modify: `services/go-server/internal/storage/local.go`
- Modify: `services/go-server/internal/storage/r2.go`
- Test: `services/go-server/internal/storage/storage_test.go`
- Test: `services/go-server/internal/storage/r2_test.go`

**Interfaces:**
- Produces:
  ```go
  type Item struct {
      Key          string    `json:"key"`
      Size         int64     `json:"size"`
      LastModified time.Time `json:"last_modified,omitempty"`
  }

  type Storage interface {
      Put(ctx context.Context, path string, data io.Reader) error
      Get(ctx context.Context, path string) (io.ReadCloser, error)
      Delete(ctx context.Context, path string) error
      Exists(ctx context.Context, path string) (bool, error)
      List(ctx context.Context, prefix string) ([]Item, error)
  }
  ```

- [ ] **Step 1: Write failing tests for `List` on `LocalStorage` and `R2Storage`**

In `services/go-server/internal/storage/storage_test.go` and `r2_test.go`:
Add test `TestLocalStorage_List` checking prefix filtering, empty prefix, subfolder keys, and sorting.
Add test `TestR2Storage_List` with `mockS3Client` implementing `ListObjectsV2`.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd services/go-server && go test ./internal/storage -run TestLocalStorage_List`
Expected: FAIL with `store.List undefined`.

- [ ] **Step 3: Implement `List` in `storage.go`, `local.go`, and `r2.go`**

1. Define `Item` struct and add `List(ctx context.Context, prefix string) ([]Item, error)` to `Storage` interface in `storage.go`.
2. Implement `List` on `LocalStorage` using `filepath.WalkDir` with relative path resolution.
3. Add `ListObjectsV2` to `S3API` interface and implement `List` on `R2Storage` using `s.client.ListObjectsV2`.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd services/go-server && go test -v ./internal/storage`
Expected: PASS for all storage tests.

- [ ] **Step 5: Commit**

```bash
git add services/go-server/internal/storage/
git commit -m "feat(storage): add List method to Storage interface for local and R2 drivers (#214)"
```

---

### Task 2: Implement Archiver HTTP Endpoints & Swagger Docs (`internal/archiver`)

**Files:**
- Create: `services/go-server/internal/archiver/handler.go`
- Test: `services/go-server/internal/archiver/handler_test.go`
- Modify: `services/go-server/cmd/server/main.go`
- Modify: `services/go-server/docs/docs.go`, `services/go-server/docs/swagger.json`, `services/go-server/docs/swagger.yaml`

**Interfaces:**
- Consumes: `storage.Storage`, `camera.Reader`
- Produces:
  - `GET /api/archiver/dates`: returns list of available partition dates (e.g. `["2026-10-11", "2026-10-10"]`)
  - `GET /api/archiver/snapshots`: returns snapshot items with metadata (`key`, `camera_id`, `timestamp`, `reason`, `size`, `raw_url`, `public_url`)
  - `GET /api/archiver/snapshots/raw?key=...`: streams image bytes with `Content-Type: image/jpeg`
  - `POST /api/archiver/labels`: saves label annotation JSON to storage
  - `GET /api/archiver/labels?date=...`: retrieves saved labels for date

- [ ] **Step 1: Write failing tests for Archiver Handler**

Create `services/go-server/internal/archiver/handler_test.go` using Gin test context and `storage.LocalStorage` with mock files.
Tests:
- `TestHandler_ListDates`: returns sorted partition dates.
- `TestHandler_ListSnapshots`: lists snapshots with correct camera metadata, `raw_url`, and `public_url`.
- `TestHandler_GetSnapshotRaw`: serves image binary, returns 404 on missing, 400 on traversal.
- `TestHandler_SaveAndGetLabels`: saves JSON annotation and retrieves it.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd services/go-server && go test ./internal/archiver -run TestHandler_`
Expected: FAIL with handler not defined.

- [ ] **Step 3: Implement `Handler` in `internal/archiver/handler.go`**

Implement `Handler` struct, `NewHandler(store storage.Storage, catalog camera.Reader) *Handler`, `RegisterRoutes(rg *gin.RouterGroup)`.
Implement `ListDates`, `ListSnapshots`, `GetSnapshotRaw`, `SaveLabel`, `GetLabels`.
Add Swagger annotations (`@Summary`, `@Tags archiver`, `@Router /archiver/...`).

- [ ] **Step 4: Wire Handler into `cmd/server/main.go` and generate Swagger**

In `cmd/server/main.go`:
Instantiate `archiverHandler := archiver.NewHandler(storeDriver, camCatalog)` and call `archiverHandler.RegisterRoutes(api)`.
Run `make swagger`.

- [ ] **Step 5: Run tests and verify swagger**

Run: `cd services/go-server && go test -v -race ./internal/archiver && make swagger`
Expected: PASS with 100% coverage on handler endpoints and clean swagger generation.

- [ ] **Step 6: Commit**

```bash
git add services/go-server/internal/archiver/ services/go-server/cmd/server/main.go services/go-server/docs/
git commit -m "feat(archiver): add HTTP snapshot listing, proxy streaming, and label endpoints (#214)"
```

---

### Task 3: Build Shared Team Web Labeler (`apps/web` & `tools/eval`)

**Files:**
- Create: `apps/web/src/app/labeler/page.tsx`
- Create: `apps/web/src/app/labeler/page.test.tsx`
- Modify: `tools/eval/labeler.html`

**Interfaces:**
- Consumes: `/api/archiver/dates`, `/api/archiver/snapshots`, `/api/archiver/snapshots/raw`, `/api/archiver/labels`
- Produces: Collaborative interactive labeling UI for commuters and developers

- [ ] **Step 1: Write failing unit test for Web Labeler component**

Create `apps/web/src/app/labeler/page.test.tsx` verifying:
- Renders labeler header, date selector, progress indicator.
- Loads snapshots from `/api/archiver/snapshots`.
- Updates selection and active classes on keyboard shortcuts (1, 2, 3, Q, W, E, R).
- Submits label to `/api/archiver/labels`.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd apps/web && npm test -- src/app/labeler/page.test.tsx`
Expected: FAIL with component not found.

- [ ] **Step 3: Implement `apps/web/src/app/labeler/page.tsx`**

Features:
1. Monochromatic, zero-emoji, high-contrast dark UI.
2. Bilingual (`vi` / `en`) toggle.
3. Date dropdown populated from `/api/archiver/dates` or direct input.
4. Frame thumbnail list and high-res main viewer streaming from `/api/archiver/snapshots/raw?key=...`.
5. Hotkeys: `1/2/3` (Flood), `Q/W/E/R` (Traffic), `A/S/D` (Weather), `Z/X/C` (Time of Day), `ArrowLeft/Right` (Prev/Next), `Enter` (Save & Next).
6. Annotator name input (persists in `localStorage`).
7. Save status indicator and Export JSON button.

- [ ] **Step 4: Update `tools/eval/labeler.html`**

Add an "API Source" configuration bar in `tools/eval/labeler.html` so developers can enter backend API URL (`http://localhost:3000`) and fetch snapshot manifests without downloading images locally.

- [ ] **Step 5: Run tests and lint**

Run: `cd apps/web && npm test && npm run lint`
Expected: PASS with 0 lint errors and all tests passing.

- [ ] **Step 6: Commit**

```bash
git add apps/web/src/app/labeler/ tools/eval/labeler.html
git commit -m "feat(web): add shared team web labeler with keyboard shortcuts and API sync (#214)"
```

---

### Task 4: Live Verification, Living Documentation & Pull Request

**Files:**
- Modify: `docs/lesson_learn.md` (Add Lesson 27)
- Modify: `docs/project_status.md` (Update Version & Metrics)

- [ ] **Step 1: Execute live operational verification against Cloudflare R2**

Run `rclone lsd R2:warded-proofs/` and verify listing of recent dates (`2026-10-11`).
Run live backend in mock mode and verify:
`curl -s "http://localhost:3000/api/archiver/dates"`
`curl -s "http://localhost:3000/api/archiver/snapshots?date=2026-10-11&limit=5"`
Verify image proxy delivery through `/api/archiver/snapshots/raw`.

- [ ] **Step 2: Record Lesson 27 and update Project Status**

Record Lesson 27 in `docs/lesson_learn.md`:
"Shared Collaborative Labeling Over Local Synchronization & Cloud Storage Justification"
Update `docs/project_status.md` to Version 1.1.2.

- [ ] **Step 3: Commit and Push**

```bash
git add docs/lesson_learn.md docs/project_status.md
git commit -m "docs: record Lesson 27 and update project status for S5-T4 labeler (#214)"
git push -u origin feat/s5-t4-r2-shared-team-labeler
```

- [ ] **Step 4: Open Pull Request targeting `dev`**

Use `gh pr create` with body starting with `Close #214`.
Explicitly include the mandatory justification statement:
"Adoption of Cloudflare R2 blob storage is justified to enable centralized collaborative team labeling and eliminate local manual synchronization friction, NOT because the system reached the ADR-004 §D7 disk capacity threshold."
