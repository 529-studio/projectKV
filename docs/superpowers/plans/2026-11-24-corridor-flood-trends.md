# Corridor Flood Depth Trends & Tidal Correlation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement 24-hour historical flood depth downsampling and tidal correlation for arterial corridors with a glanceable, zero-dependency commuter Spark-Ribbon UI.

**Architecture:** An append-only SQLite time series (`flood_verdict_history`) stores camera observations on arrival. A corridor aggregator (`corridor.Service.GetHistory`) downsamples 5-minute readings into 1-hour average and peak depth buckets, fuses them with harmonic astronomical tide levels from `tide.PredictTide`, and exposes `GET /api/roads/:road_key/history?hours=24`. The web frontend renders a compact ~72px Spark-Ribbon inside `CorridorOverviewCard` with touch scrubbing and bilingual copy.

**Tech Stack:** Go 1.24 (standard library `database/sql`, `net/http`, Gin), SQLite, TypeScript, React, Next.js, Tailwind CSS, Inline SVG.

**Spec:** [`docs/superpowers/specs/2026-11-24-corridor-flood-trends-spec.md`](file:///Users/thanhdatvu/repos/projectKV/docs/superpowers/specs/2026-11-24-corridor-flood-trends-spec.md)

---

## Global Constraints

- **AGENTS.md Rule 1**: 100% English code/commits/docs; bilingual `vi`/`en` in user-facing UI; strictly zero emoji.
- **AGENTS.md Rule 2 & 10**: Sound solutions over shortcuts; empirical manual test scenarios executed and verified on live services.
- **AGENTS.md Rule 5 & 9**: Target branch `dev`; PR description must strictly start with `Close #526`.
- **AGENTS.md Rule 7**: Leaf modulith pattern in `services/go-server`; hand-written SQL only (no ORM); update swagger on API changes.
- **AGENTS.md Rule 8**: Zero hardcoded secrets; environment variables loaded via `.env`.
- **AGENTS.md Rule 11**: Maintain context budget; zero new violations on `make docs-lint`.
- **AGENTS.md Rule 14**: 100% green CI and zero GitHub Advanced Security CodeQL alerts.
- **Zero Heavy Dependencies**: Pure inline SVG and CSS classes, zero chart.js, d3, or recharts.
- **Ergonomics**: Minimum 48px touch targets for mobile scrubbing.

---

## Review Focus

1. **Empty / Sparse Observations**: Corridors with zero historical flood readings must return a clean dry timeline (`avg_depth_cm: 0.0, max_depth_cm: 0.0, hazard_level: CLEAR`) without nil-pointer panics or NaN values.
2. **Window & Hour Clamping**: Invalid or extreme `hours` query parameters (e.g. `hours=-5`, `hours=0`, `hours=5000`) must clamp safely to `[1, 72]` (defaulting to 24).
3. **Station Association Fallback**: Corridors not explicitly declared in `LowLyingRoads` must safely default to `StationPhuAn` without failing the request.
4. **404 for Unknown Corridors**: Querying `/api/roads/unknown_road/history` must return HTTP 404 with standard JSON `{"error": "corridor not found"}`.
5. **Mobile Touch Scrub Isolation**: Touch scrubbing across the 24-hour Spark-Ribbon on touchscreens must update the tooltip without triggering unwanted map pans or window scroll jitter.

---

## Task Decomposition

### Task 1: SQLite Migration & Flood History Repository

**Files:**
- Create: `services/go-server/internal/store/migrations/0015_flood_verdict_history.sql`
- Modify: `services/go-server/internal/store/constants.go`
- Modify: `services/go-server/internal/store/sqlite.go:160-190`
- Create: `services/go-server/internal/store/flood_history_repo.go`
- Test: `services/go-server/internal/store/flood_history_repo_test.go`

- [ ] **Step 1: Write migration SQL**
  Create `services/go-server/internal/store/migrations/0015_flood_verdict_history.sql` with table `flood_verdict_history` and indexes on `(camera_id, observed_at)` and `(observed_at)`.
- [ ] **Step 2: Update schema assertions**
  In `services/go-server/internal/store/constants.go`, declare `TableFloodVerdictHistory = "flood_verdict_history"`. In `services/go-server/internal/store/sqlite.go`, add `flood_verdict_history` to `CriticalTables`.
- [ ] **Step 3: Write failing repository test**
  Create `services/go-server/internal/store/flood_history_repo_test.go` testing:
  - Idempotent `Record` (ignoring duplicate `(camera_id, observed_at)`).
  - `GetRecentByCameraIDs` returning observations ordered by `observed_at ASC` within `[startTime, endTime]`.
- [ ] **Step 4: Run test to confirm failure**
  Run: `cd services/go-server && go test -v ./internal/store -run TestFloodHistoryRepo`.
- [ ] **Step 5: Implement `FloodHistoryRepo`**
  Implement `FloodHistoryRecord`, `NewFloodHistoryRepo(db)`, `Record`, and `GetRecentByCameraIDs` in `services/go-server/internal/store/flood_history_repo.go`.
- [ ] **Step 6: Run test to confirm pass**
  Run: `cd services/go-server && go test -v ./internal/store -run TestFloodHistoryRepo`.
- [ ] **Step 7: Commit Task 1**
  Commit: `git commit -am "feat(store): add flood_verdict_history migration and repository"`

---

### Task 2: Live Verdict Ingestion Hook

**Files:**
- Modify: `services/go-server/cmd/server/adapters.go:1200-1235`
- Test: `services/go-server/internal/store/flood_history_repo_test.go`

- [ ] **Step 1: Write integration test**
  Add test verifying that when flood verdicts are received, they are appended to `flood_verdict_history`.
- [ ] **Step 2: Wire `onFloodVerdict` in adapters**
  In `services/go-server/cmd/server/adapters.go`, update `onFloodVerdict`:
  If `floodHistoryRepo != nil`, record `FloodHistoryRecord` containing `v.CameraID`, `v.FloodDepthCm`, `v.FloodHazardLevel`, `v.FloodSeverity`, `v.FloodDepthConfidence`, `v.FloodCalibrationRef`, and `v.ObservedAt`.
- [ ] **Step 3: Wire repository in `cmd/server/main.go`**
  Initialize `floodHistoryRepo := store.NewFloodHistoryRepo(db)` and pass into `wireFloodModule`.
- [ ] **Step 4: Verify build and test**
  Run: `cd services/go-server && go test -v ./cmd/server && go vet ./...`.
- [ ] **Step 5: Commit Task 2**
  Commit: `git commit -am "feat(server): wire flood verdict stream into flood_verdict_history persistence"`

---

### Task 3: Corridor Historical Aggregator & API Endpoint

**Files:**
- Modify: `services/go-server/internal/corridor/corridor.go`
- Modify: `services/go-server/internal/corridor/handler.go`
- Modify: `services/go-server/internal/corridor/routes.go`
- Test: `services/go-server/internal/corridor/corridor_test.go`

- [ ] **Step 1: Define DTOs in `corridor.go`**
  Define `HourlyHistoryItem`, `HistorySummary`, and `CorridorHistoryResponse`.
- [ ] **Step 2: Define `FloodHistoryProvider` interface in `corridor.go`**
  `type FloodHistoryProvider interface { GetRecentByCameraIDs(ctx context.Context, cameraIDs []string, start, end time.Time) ([]store.FloodHistoryRecord, error) }`.
- [ ] **Step 3: Write failing unit test for `GetHistory`**
  In `services/go-server/internal/corridor/corridor_test.go`, write test cases for:
  - 1-hour bucket aggregation (`avg_depth_cm` and `max_depth_cm`).
  - Zero-observation dry road fallback.
  - Correct astronomical tide prediction from `tide.PredictTide`.
  - Non-existent `road_key` returns false / 404.
- [ ] **Step 4: Run test to confirm failure**
  Run: `cd services/go-server && go test -v ./internal/corridor -run TestService_GetHistory`.
- [ ] **Step 5: Implement `GetHistory` in `corridor.go`**
  Implement downsampling algorithm, bucket sorting, peak tide & depth summary calculation.
- [ ] **Step 6: Expose `GET /api/roads/:road_key/history` in `handler.go`**
  Implement `GetHistory(c *gin.Context)` in `handler.go` with `@Summary`, `@Description`, `@Param hours query int false "Hours of history (default 24, max 72)"`.
- [ ] **Step 7: Regenerate swagger**
  Run: `make swagger`.
- [ ] **Step 8: Verify corridor tests pass**
  Run: `cd services/go-server && go test -v -race ./internal/corridor/...`.
- [ ] **Step 9: Commit Task 3**
  Commit: `git commit -am "feat(corridor): implement historical flood downsampling and GET /api/roads/:road_key/history"`

---

### Task 4: Frontend Commuter Spark-Ribbon Component

**Files:**
- Create: `apps/web/src/types/corridorHistory.ts`
- Create: `apps/web/src/hooks/useCorridorHistory.ts`
- Create: `apps/web/src/components/CorridorTrends.tsx`
- Modify: `apps/web/src/components/CorridorOverviewCard.tsx`
- Modify: `apps/web/src/config/constants.ts`
- Test: `apps/web/src/components/CorridorTrends.test.ts`
- Test: `apps/web/src/hooks/useCorridorHistory.test.ts`

- [ ] **Step 1: Create TypeScript DTOs**
  Create `apps/web/src/types/corridorHistory.ts` matching backend `CorridorHistoryResponse`.
- [ ] **Step 2: Create hook `useCorridorHistory.ts`**
  Fetch `/api/roads/:road_key/history?hours=24` with loading, error, and data states.
- [ ] **Step 3: Write failing unit test for `CorridorTrends`**
  Create `apps/web/src/components/CorridorTrends.test.ts` verifying:
  - Default collapsed state showing 1-line headline.
  - Expand toggle displays the 24-hour SVG ribbon.
  - Proper color mapping (zinc for dry, amber for caution, rose for impassable).
  - Micro-tooltip renders on scrub/touch.
  - Bilingual copy (`vi` and `en`) with zero emojis.
- [ ] **Step 4: Run test to confirm failure**
  Run: `cd apps/web && npm test -- src/components/CorridorTrends.test.ts`.
- [ ] **Step 5: Implement `CorridorTrends.tsx`**
  Implement the SVG spark-ribbon with zero external charting dependencies, responsive coordinate viewBox, dashed tide curve, and touch scrubbing handlers.
- [ ] **Step 6: Embed in `CorridorOverviewCard.tsx`**
  Place `CorridorTrends` beneath the passability badge in `CorridorOverviewCard.tsx`.
- [ ] **Step 7: Run frontend tests and typecheck**
  Run: `cd apps/web && npm test && npx tsc --noEmit && npm run lint`.
- [ ] **Step 8: Commit Task 4**
  Commit: `git commit -am "feat(web): add compact 24h CorridorTrends Spark-Ribbon component"`

---

### Task 5: End-to-End Verification, Documentation & PR

**Files:**
- Create: `docs/lessons/526-corridor-flood-trends.md`
- Modify: `README.md` (if needed)

- [ ] **Step 1: Execute manual testing checklist (Rule 10)**
  - Scenario 1: Query API for `vo_van_kiet`, `nguyen_huu_canh`, `tran_xuan_soan`. Verify payload size < 12 KB and valid downsampled hourly buckets.
  - Scenario 2: Test API with `hours=12`, `hours=48`, and invalid `hours=-1`. Confirm safe boundary clamping.
  - Scenario 3: Verify frontend UI in mobile viewport (375px width), test touch scrubbing, check bilingual toggle (`vi`/`en`).
- [ ] **Step 4: Author engineering lesson**
  Create `docs/lessons/526-corridor-flood-trends.md` documenting downsampling performance, SVG zero-dependency rendering, and mobile scrub isolation.
- [ ] **Step 5: Run repository linting gates**
  Run: `make docs-lint && make test-go && cd apps/web && npm test`.
- [ ] **Step 6: Commit and push feature branch**
  Branch: `feat/526-corridor-flood-trends`.
  Open PR targeting `dev` with description starting with `Close #526`.
- [ ] **Step 7: Verify CI and CodeQL security status**
  Ensure all checks pass with 100% green status and 0 CodeQL alerts.
