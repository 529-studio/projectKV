# Bulk Auto-Approval from AI Candidates to Human-Verified Ground Truth Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Automate the bulk promotion and approval of 23,665 AI candidate labels (`opencode_auto`) into human-verified ground truth labels (`annotator="reviewer"` or custom reviewer identity) while preserving data provenance, preventing Cloudflare R2 storage bloat, hardening label precedence in the frontend, and updating the Web Labeler catalog counts.

**Architecture:**
- **High-Throughput CLI Promotion Pipeline (`tools/eval/batch_approve_labels.py`)**: Multi-threaded Python worker pool using `boto3` to scan R2 `labels/{date}/`, read auto-harvested labels, inject approval audit metadata (`approved_by`, `approved_at`, `original_annotator`), upload new human label keys (`labels/{date}/{cam}_{reviewer}_{ts}.json`), and atomically purge obsolete auto keys.
- **Frontend Precedence Hardening (`apps/web/src/utils/labelerUtils.ts`)**: Introduce `resolveCanonicalLabels` to guarantee human-reviewed labels strictly take precedence over AI candidates regardless of R2 key collation order, resolving key collision edge cases.
- **Cache Invalidation Endpoint (`services/go-server/internal/archiver/handler.go`)**: Implement `POST /api/archiver/cache/invalidate` to flush in-memory snapshot and label caches, allowing immediate UI synchronization without waiting for the 60s TTL or restarting backend services.
- **Web Labeler Interactive Page Bulk-Approve (`apps/web/src/app/labeler/page.tsx`)**: Add a zero-emoji `[Duyệt tất cả trang]` button in the Web Labeler control bar for continuous batch verification of 200 items per page by human annotators.

**Tech Stack:** Python 3 (`boto3`, `concurrent.futures`), Go 1.24 (`gin-gonic`), Next.js 15 (`React`, `TypeScript`), Cloudflare R2 S3 API.

**Spec:** Issue #287 / Sprint 8 Task 5 (Batch Auto-Labeling Pipeline & Team Labeler Verification).

## Global Constraints
- Minimalist, monochromatic, zero-emoji in user-facing UI and backend logs (AGENTS.md Rule 1).
- Bilingual web UI (`vi` default with full diacritics, `en` secondary); Codebase, tooling, and documentation 100% English (AGENTS.md Rule 1).
- Sound solutions over shortcuts: Never leave duplicate unreferenced objects in R2 storage; preserve audit provenance in label notes and metadata (AGENTS.md Rule 2).
- Explicit effort estimation: 4.5 active effort hours, delivery window within Sprint 8 (AGENTS.md Rule 3).
- Zero hardcoded secrets: All R2 credentials and API tokens dynamically loaded from `.env` with fail-fast or empty fallbacks (AGENTS.md Rule 8).
- Git Flow: PR targeting `dev` branch; never direct push to `main` or `dev` (AGENTS.md Rule 5).
- Mandatory manual testing scenarios checklist with live empirical proof before PR (AGENTS.md Rule 10).

## Review Focus
1. **R2 Collation Overwrite Order**: In `labeler/page.tsx`, object mapping previously used raw array iteration where `opencode_auto` could overwrite a human review if the human reviewer name sorted alphabetically before "opencode" (e.g., "alex" < "opencode"). `resolveCanonicalLabels` must strictly prioritize human approval over auto candidates.
2. **Atomic Write-Before-Delete**: When deleting old `opencode_auto` keys in R2, the script must verify the new human label object was successfully stored (HTTP 200) before issuing the delete call, preventing data loss in transient network blips.
3. **Storage Bloat Elimination**: Creating 23,665 new human label keys without deleting obsolete auto keys would double R2 objects to 47,330 files, doubling cold-start read latencies in `GetLabels`. Old keys must be deleted by default (with optional `--keep-old-labels` flag for archival).
4. **Rate Limiting & Cloudflare R2 Concurrency**: Firing thousands of concurrent S3 PUT/DELETE requests can encounter Cloudflare R2 429 rate limits. The CLI tool must use a bounded concurrency pool (25-30 threads) with exponential backoff retries.
5. **Cache Invalidation & Real-Time Sync**: Go backend caches labels for 60s in memory. Without an invalidation endpoint, bulk approvals run in background would cause stale UI counts. The endpoint `POST /api/archiver/cache/invalidate` must immediately clear the cache.

---

## Tasks

### Task 1: Canonical Label Precedence and Provenance Resolution in Frontend

**Files:**
- Modify: `apps/web/src/utils/labelerUtils.ts`
- Modify: `apps/web/src/app/labeler/page.tsx:197-206`
- Test: `apps/web/src/utils/labelerUtils.test.ts`

**Interfaces:**
- Produces: `export function resolveCanonicalLabels(labels: LabelPayload[]): Record<string, LabelPayload>`
- Consumes: `LabelPayload`, `isAutoHarvested`

- [ ] **Step 1: Write failing test for `resolveCanonicalLabels`**

In `apps/web/src/utils/labelerUtils.test.ts`:
```typescript
test('resolveCanonicalLabels prioritizes human labels over auto candidates and sorts by latest timestamp', () => {
  const rawLabels: LabelPayload[] = [
    // Auto candidate for snap_1
    {
      snapshot_key: 'snap_1.jpg',
      camera_id: 'cam_1',
      date: '2026-10-23',
      annotator: 'opencode_auto',
      timestamp: 100,
      flood_level: 'none',
      traffic_level: 'moderate',
      weather: 'clear',
      time_of_day: 'day',
    },
    // Human approval for snap_1 with reviewer name alphabetically before "opencode"
    {
      snapshot_key: 'snap_1.jpg',
      camera_id: 'cam_1',
      date: '2026-10-23',
      annotator: 'alice',
      timestamp: 105,
      flood_level: 'puddle',
      traffic_level: 'heavy',
      weather: 'rain',
      time_of_day: 'day',
    },
    // Auto candidate for snap_2
    {
      snapshot_key: 'snap_2.jpg',
      camera_id: 'cam_2',
      date: '2026-10-23',
      annotator: 'harvester/auto',
      timestamp: 200,
      flood_level: 'none',
      traffic_level: 'clear',
      weather: 'clear',
      time_of_day: 'day',
    },
  ];

  const resolved = resolveCanonicalLabels(rawLabels);
  assert.equal(Object.keys(resolved).length, 2);
  assert.equal(resolved['snap_1.jpg'].annotator, 'alice');
  assert.equal(resolved['snap_1.jpg'].flood_level, 'puddle');
  assert.equal(resolved['snap_2.jpg'].annotator, 'harvester/auto');
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `node --test apps/web/src/utils/labelerUtils.test.ts`
Expected: FAIL with "resolveCanonicalLabels is not a function"

- [ ] **Step 3: Implement `resolveCanonicalLabels` in `apps/web/src/utils/labelerUtils.ts`**

Export function `resolveCanonicalLabels(labels: LabelPayload[]): Record<string, LabelPayload>`:
```typescript
export function resolveCanonicalLabels(labels: LabelPayload[]): Record<string, LabelPayload> {
  const result: Record<string, LabelPayload> = {};
  for (const label of labels) {
    if (!label.snapshot_key) continue;
    const existing = result[label.snapshot_key];
    if (!existing) {
      result[label.snapshot_key] = label;
      continue;
    }
    const existingIsAuto = isAutoHarvested(existing);
    const newIsAuto = isAutoHarvested(label);

    // Rule 1: Human approved label strictly overrides AI auto candidate
    if (existingIsAuto && !newIsAuto) {
      result[label.snapshot_key] = label;
    } else if (!existingIsAuto && newIsAuto) {
      // Keep existing human label
      continue;
    } else {
      // Both are human or both are auto: latest timestamp wins
      if ((label.timestamp || 0) >= (existing.timestamp || 0)) {
        result[label.snapshot_key] = label;
      }
    }
  }
  return result;
}
```

Update `apps/web/src/app/labeler/page.tsx:197-206` to use `resolveCanonicalLabels(labelData.labels || [])` instead of simple iteration.

- [ ] **Step 4: Run test to verify it passes**

Run: `node --test apps/web/src/utils/labelerUtils.test.ts`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add apps/web/src/utils/labelerUtils.ts apps/web/src/utils/labelerUtils.test.ts apps/web/src/app/labeler/page.tsx
git commit -m "fix(web): harden label canonical precedence favoring human verification over auto"
```

---

### Task 2: Go Backend Archiver Cache Invalidation Endpoint

**Files:**
- Modify: `services/go-server/internal/archiver/handler.go:110-120`
- Modify: `services/go-server/internal/archiver/handler.go` (implement `InvalidateCache`)
- Test: `services/go-server/internal/archiver/handler_test.go`

**Interfaces:**
- Produces: `POST /api/archiver/cache/invalidate` -> `{"status":"ok","message":"archiver caches invalidated"}`
- Consumes: `h.cacheMu`, `h.labelCache`, `h.snapshotCache`

- [ ] **Step 1: Write failing unit test for `InvalidateCache`**

In `services/go-server/internal/archiver/handler_test.go`:
```go
func TestHandler_InvalidateCache(t *testing.T) {
	router, store := setupTestRouter(t)
	ctx := context.Background()

	// Seed dummy label to populate cache
	_ = store.Put(ctx, "labels/2026-10-23/cam1_reviewer_1729600000.json", bytes.NewReader([]byte(`{"snapshot_key":"snap1","date":"2026-10-23","camera_id":"cam1","annotator":"reviewer"}`)))

	// First call primes cache
	w1 := httptest.NewRecorder()
	req1, _ := http.NewRequest("GET", "/api/archiver/labels?date=2026-10-23", nil)
	router.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w1.Code)
	}

	// Call invalidation endpoint
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("POST", "/api/archiver/cache/invalidate", nil)
	router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200 on cache invalidate, got %d", w2.Code)
	}

	var resp map[string]interface{}
	_ = json.Unmarshal(w2.Body.Bytes(), &resp)
	if resp["status"] != "ok" {
		t.Errorf("expected status ok, got %v", resp["status"])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd services/go-server && go test -v -run TestHandler_InvalidateCache ./internal/archiver`
Expected: FAIL (404 on `/api/archiver/cache/invalidate`)

- [ ] **Step 3: Implement `InvalidateCache` in `services/go-server/internal/archiver/handler.go`**

Register route:
```go
archiverGroup.POST("/cache/invalidate", h.InvalidateCache)
```

Implement handler:
```go
// InvalidateCache godoc
// @Summary Invalidate archiver in-memory caches
// @Description Purges snapshotCache and labelCache to force fresh fetch from storage
// @Tags archiver
// @Produce json
// @Success 200 {object} map[string]string
// @Router /api/archiver/cache/invalidate [post]
func (h *Handler) InvalidateCache(c *gin.Context) {
	h.cacheMu.Lock()
	h.snapshotCache = make(map[string]snapshotCacheEntry)
	h.labelCache = make(map[string]labelCacheEntry)
	h.cacheMu.Unlock()

	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"message": "archiver caches invalidated",
	})
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd services/go-server && go test -v -run TestHandler_InvalidateCache ./internal/archiver`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add services/go-server/internal/archiver/handler.go services/go-server/internal/archiver/handler_test.go
git commit -m "feat(archiver): add cache invalidation endpoint for synchronized catalog refresh"
```

---

### Task 3: Standalone High-Throughput Bulk Label Approval CLI Tool (`batch_approve_labels.py`)

**Files:**
- Create: `tools/eval/batch_approve_labels.py`
- Create: `tools/eval/test_batch_approve_labels.py`

**Interfaces:**
- CLI Flags:
  - `--date YYYY-MM-DD` (partition date, default: today/latest)
  - `--reviewer NAME` (reviewer username/email stamped on approved label, default: `reviewer`)
  - `--concurrency N` (number of worker threads, default: 25)
  - `--limit N` (maximum labels to process, 0 = all, default: 0)
  - `--dry-run` (simulate without modifying R2)
  - `--keep-old-labels` (do not delete old auto-label JSONs in R2, default: False)
  - `--invalidate-cache` (trigger POST `/api/archiver/cache/invalidate` upon completion)
  - `--api-url URL` (Go backend URL, default: `http://localhost:3000`)
- Produces: Promoted R2 objects `labels/{date}/{cam}_{reviewer}_{ts}.json` and deletes `labels/{date}/{cam}_{auto}_{ts}.json`.

- [ ] **Step 1: Write unit test for CLI transformation logic in `tools/eval/test_batch_approve_labels.py`**

```python
import unittest
from tools.eval.batch_approve_labels import transform_label_payload, is_auto_annotator

class TestBatchApproveLabels(unittest.TestCase):
    def test_is_auto_annotator(self):
        self.assertTrue(is_auto_annotator("opencode_auto"))
        self.assertTrue(is_auto_annotator("harvester/auto"))
        self.assertTrue(is_auto_annotator("auto_harvester"))
        self.assertFalse(is_auto_annotator("reviewer"))
        self.assertFalse(is_auto_annotator("thdat"))
        self.assertFalse(is_auto_annotator("human_reviewer"))

    def test_transform_label_payload(self):
        raw = {
            "date": "2026-10-23",
            "camera_id": "cam_10",
            "snapshot_key": "2026-10-23/cam_10/1729600000_sample.jpg",
            "annotator": "opencode_auto",
            "flood_level": "none",
            "traffic_level": "moderate",
            "notes": "Auto-labeled by opencode_auto",
            "timestamp": 1729600000,
        }
        promoted = transform_label_payload(raw, reviewer="reviewer", approved_timestamp=1729700000)
        self.assertEqual(promoted["annotator"], "reviewer")
        self.assertEqual(promoted["original_annotator"], "opencode_auto")
        self.assertEqual(promoted["approved_by"], "reviewer")
        self.assertEqual(promoted["approved_at"], 1729700000)
        self.assertIn("Approved by reviewer", promoted["notes"])
        self.assertEqual(promoted["traffic_level"], "moderate")
        self.assertEqual(promoted["flood_level"], "none")

if __name__ == "__main__":
    unittest.main()
```

- [ ] **Step 2: Run test to verify it fails**

Run: `python3 -m unittest tools/eval/test_batch_approve_labels.py`
Expected: FAIL (module `tools.eval.batch_approve_labels` does not exist)

- [ ] **Step 3: Implement `tools/eval/batch_approve_labels.py`**

Features:
- Dynamically loads `.env` for R2 credentials (`R2_ENDPOINT`, `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY`, `R2_BUCKET_NAME`).
- Uses `boto3.client('s3')` with connection pooling (`botocore.config.Config(max_pool_connections=50)`).
- Paginates through `labels/{date}/` via `get_paginator('list_objects_v2')`.
- Identifies keys matching auto annotators (`opencode_auto`, `_auto`, `harvester/`, `auto_`).
- In `ThreadPoolExecutor`:
  - Fetches existing JSON (`s3.get_object`).
  - Calls `transform_label_payload`.
  - Puts new object `labels/{date}/{cam}_{reviewer}_{ts}.json`.
  - Verifies write succeeded.
  - Deletes old object (`s3.delete_object`) unless `--keep-old-labels`.
- Prints continuous progress, speed (promotions/sec), and estimated time remaining.
- If `--invalidate-cache`, posts to `api_url/api/archiver/cache/invalidate`.

- [ ] **Step 4: Run test to verify it passes**

Run: `python3 -m unittest tools/eval/test_batch_approve_labels.py`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add tools/eval/batch_approve_labels.py tools/eval/test_batch_approve_labels.py
git commit -m "feat(eval): add high-throughput bulk label approval CLI with atomic R2 promotion"
```

---

### Task 4: Web Labeler "Approve All on Page" Batch Action

**Files:**
- Modify: `apps/web/src/utils/labelerUtils.ts` (add `batchApprovePageAction`, `batchApproveSuccess` to `LABELER_I18N`)
- Modify: `apps/web/src/app/labeler/page.tsx`
- Test: `apps/web/src/utils/labelerUtils.test.ts`

**Interfaces:**
- Produces: UI Button `[Duyệt tất cả trang (200)]` on `/labeler`.
- Consumes: `filteredSnapshots`, `handleFastApprove` / batch POST API.

- [ ] **Step 1: Add bilingual zero-emoji strings and verify via test**

In `apps/web/src/utils/labelerUtils.ts`:
```typescript
vi: {
  // ...
  batchApprovePage: 'Duyệt tất cả trang',
  batchApprovePageConfirm: 'Duyệt toàn bộ {count} ảnh trên trang này?',
  batchApprovePageSuccess: 'Đã duyệt {count} ảnh thành công',
},
en: {
  // ...
  batchApprovePage: 'Approve page',
  batchApprovePageConfirm: 'Approve all {count} images on this page?',
  batchApprovePageSuccess: 'Successfully approved {count} images',
}
```

Verify in `labelerUtils.test.ts` that zero emojis exist in new strings.

- [ ] **Step 2: Implement batch page approval handler in `apps/web/src/app/labeler/page.tsx`**

Add `handleBatchApproveCurrentPage`:
- Gathers all candidate snapshots on the current 200-item page that are in `pending_auto` or unlabeled state.
- Constructs `LabelPayload` for each snapshot with `annotator = annotator.trim() || 'reviewer'`.
- Posts each payload to `/api/archiver/labels` with bounded concurrency (e.g., 10 concurrent requests).
- Optimistically updates `labels` state using `resolveCanonicalLabels`.
- Shows feedback status and refreshes catalog.

- [ ] **Step 3: Run web tests and lint check**

Run: `node --test apps/web/src/utils/labelerUtils.test.ts`
Run: `npm --prefix apps/web run lint`
Expected: PASS with 0 errors.

- [ ] **Step 4: Commit**

```bash
git add apps/web/src/utils/labelerUtils.ts apps/web/src/app/labeler/page.tsx apps/web/src/utils/labelerUtils.test.ts
git commit -m "feat(web): add interactive batch page approval action in web labeler"
```

---

### Task 5: End-to-End Verification & Bulk Execution on 23,665 Labels

**Files:**
- Execute: `tools/eval/batch_approve_labels.py`
- Verify: Live running services (`http://localhost:3000`, `http://localhost:3001/labeler`)

- [ ] **Step 1: Execute Dry-Run Mode**

Run: `python3 tools/eval/batch_approve_labels.py --date 2026-10-23 --dry-run --limit 20`
Verify:
- Successfully connects to Cloudflare R2 bucket `warded-proofs`.
- Identifies candidate `opencode_auto` keys.
- Shows planned key renaming and payload transformation without mutating R2.

- [ ] **Step 2: Execute Small Batch Test (10 items)**

Run: `python3 tools/eval/batch_approve_labels.py --date 2026-10-23 --limit 10 --reviewer reviewer --invalidate-cache`
Verify:
- 10 objects promoted to `reviewer`.
- 10 old `opencode_auto` objects deleted.
- Web UI at `http://localhost:3001/labeler` shows:
  - `Đã duyệt (Human)` increases by 10 (from 5 to 15).
  - `Chờ duyệt (Auto)` decreases by 10 (from 23,665 to 23,655).

- [ ] **Step 3: Full Bulk Execution on All 23,655 Remaining Labels**

Run: `python3 tools/eval/batch_approve_labels.py --date 2026-10-23 --concurrency 30 --reviewer reviewer --invalidate-cache`
Verify:
- Processes all remaining labels at ~50-80 labels/sec.
- 0 failed items.
- Invalidation signal received by Go backend.

- [ ] **Step 4: Web UI Visual Inspection & Empirical Proof**

Navigate to `http://localhost:3001/labeler` via Playwright MCP:
- Verify Tab counts:
  - `Tất cả (21,336)`
  - `Chờ duyệt (Auto) (0)`
  - `Đã duyệt (Human) (23,665)`
- Capture screenshot proof and store in artifacts directory.
- Verify clicking on items in `Đã duyệt (Human)` shows `Người gán nhãn: reviewer` with complete labels and physical submersion anchors.

- [ ] **Step 5: Commit and Update Documentation**

Update `docs/project_status.md` and related docs with empirical statistics.
Commit changes:
```bash
git commit -m "chore: record bulk label approval empirical results and catalog synchronization"
```

---

## Manual Testing Scenarios (AGENTS.md Rule 10 Checklist)

Before considering this feature complete and ready for PR:

1. **Scenario 1: Dry-Run Safety Check**
   - Command: `python3 tools/eval/batch_approve_labels.py --date 2026-10-23 --dry-run --limit 50`
   - Empirical Expectation: CLI displays 50 detected candidate labels, prints dry-run transformation sample with audit notes, and R2 object counts remain exactly unchanged.
2. **Scenario 2: Atomicity & Rollback Protection**
   - Simulate a network or S3 error during PUT to verify the script aborts before issuing DELETE, guaranteeing no data loss.
3. **Scenario 3: Go Server Cache Invalidation**
   - Command: `curl -i -X POST http://localhost:3000/api/archiver/cache/invalidate`
   - Empirical Expectation: HTTP 200 OK with `{"status":"ok","message":"archiver caches invalidated"}` and backend logs confirm cache flush.
4. **Scenario 4: Web UI Precedence Collation Test**
   - Check that human approval takes precedence over auto candidate for the same snapshot key, even if human annotator name begins with letters 'a' through 'n'.
5. **Scenario 5: Full Execution & Catalog Metric Verification**
   - Full run: `python3 tools/eval/batch_approve_labels.py --date 2026-10-23 --concurrency 30 --reviewer reviewer --invalidate-cache`
   - Empirical Proof: Web Labeler at `http://localhost:3001/labeler` displays:
     - `Chờ duyệt (Auto)` = 0
     - `Đã duyệt (Human)` = 23,665
     - Zero console errors in browser telemetry.
