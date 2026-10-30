## Lesson 53: Active Learning Queue Desync & Dual-Storage Frame Routing in Web Labeler

- **Incident & Tracking:** Issue #287 / PR #292 (`fix/s8-t5-harvester-semaphore-deadlock`, targeting `dev`).
- **Symptom:**
  1. Reviewers visiting `http://localhost:3001/labeler` saw only `Tất cả (200)`, `Chờ duyệt (Auto) (0)`, and `Đã duyệt (Human) (0)` despite thousands of snapshots and auto-harvested candidates existing in Cloudflare R2 storage.
  2. Clicking `[Chờ duyệt (Auto)]` displayed an empty queue (`0 / 0`) with "Không tìm thấy ảnh chụp nào cho ngày đã chọn", completely locking annotators out of validating the 2,200+ auto-labeled candidates.
  3. When candidate frames were requested from the backend raw proxy (`/api/archiver/snapshots/raw`), images returned HTTP 404 Not Found.
- **Root Cause:**
  1. **Truncated Raw Frame Query & Local Array Counting**:
     - `apps/web/src/app/labeler/page.tsx` hardcoded `fetch('/api/archiver/snapshots?date=...&limit=200')` without pagination.
     - The UI derived `counts.all` directly from `snapshots.length` (clamped to 200) instead of `snapData.total`.
  2. **Timestamp Inversion & Temporal Gaps**:
     - Backend sorted snapshots by timestamp descending (newest frames first). Because the snapshot archiver continuously samples every 30s, the 200 items in `snapshots` were newly archived within the last 5 minutes (zero labels yet).
     - The 2,000+ auto-labeled frames were captured hours earlier. Because the frontend queue filtered only over `snapshots`, the intersection was 0, resulting in `pendingAuto = 0`.
  3. **Provenance Annotation Mismatch**:
     - `batch_auto_label.py` stamped `annotator="opencode_auto"`.
     - `labelerUtils.ts#isAutoHarvested` only checked `harvester/`, `auto_harvester`, and `auto`, failing to recognize `opencode_auto` or `*_auto`.
  4. **Dual-Prefix Key Mismatch**:
     - `internal/harvester/pipeline.go` uploaded images to `proofs/YYYY-MM-DD/<cam>/<ts>_sample.jpg`.
     - `internal/archiver/handler.go` stripped `proofs/` when serving `GET /api/archiver/labels`, storing `snapshot_key` as `YYYY-MM-DD/<cam>/<ts>_sample.jpg`.
     - When `GetSnapshotRaw` looked up the stripped key, R2 returned 404 because the actual S3 object key retained the `proofs/` prefix.
- **Mandatory Takeaways & Engineering Resolutions:**
  1. **Queue Merging & Candidate Snapshot Synthesis**:
     - Upgraded `filterSnapshots` in `labelerUtils.ts` to source `pending_auto` and `approved_human` directly from the full `labels` catalog, synthesizing `SnapshotItem` descriptors with authentic camera name resolution whenever frames fall outside the current raw page.
  2. **Comprehensive Provenance Matching**:
     - Expanded `isAutoHarvested` to support `opencode_auto`, `*_auto`, `auto_*`, and `/auto` patterns.
  3. **Backend Dual-Prefix Fallback Routing**:
     - In `handler.go#GetSnapshotRaw`, implemented automatic prefix fallback: if `key` is not found, retry with `"proofs/" + key` (and vice versa if `key` begins with `proofs/`), transparently bridging harvester and archiver frames.
  4. **Pagination Controls & Total Snapshot Telemetry**:
     - Added `page` state and pagination controls (`Trang trước`, `Trang sau`, `Trang X/Y`) for the `[Tất cả]` tab, accurately surfacing `snapData.total` (e.g. `19,607`).

---
