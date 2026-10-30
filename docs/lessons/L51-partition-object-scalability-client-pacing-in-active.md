## Lesson 51: Partition Object Scalability & Client Pacing in Active Learning Data Engines

- **Incident & Tracking:** Issue #285, Kanban **[KV-16]** (`feat/archiver-labeler-perf-and-resilience`, PR targeting `dev`).
- **Symptom:**
  1. As automated and manual labeling progressed, partition date folders under Cloudflare R2 (`labels/YYYY-MM-DD/`) grew to 900+ JSON documents. When annotators refreshed or reloaded the Web Labeler UI (`localhost:3001/labeler`), the backend response for `GET /api/archiver/labels` stalled for 7.92 seconds.
  2. Rapid automated label ingestion via batch tools triggered HTTP 429 Too Many Requests when bursting against the Go backend.
  3. Environment variables configured with wrapping quotes (e.g. `HARVESTER_SCHEDULE_HOURS="12:28,15:00"`) failed slot parsing, skipping scheduled automated harvesting runs.
  4. Web Labeler UI metadata panel overflowed smaller laptop viewports, pushing submission action buttons off-screen.
- **Root Cause:**
  1. **Uncached Sequential R2 Storage Fetching**: In `services/go-server/internal/archiver/handler.go`, `GetLabels` retrieved the S3 key manifest and then fetched object payloads sequentially across the public internet, multiplying latency by hundreds of round-trips.
  2. **Unpaced Batch Ingestion**: CLI scripts dispatched requests in tight loops without token bucket rate limiting or automatic backoff against backend rate limits.
  3. **Literal Quote Preservation in Config**: Standard `.env` loaders can preserve literal quote characters if values are quoted, causing naive string splitters to fail time parsing (e.g., `"12:28"` vs `12:28`).
- **Mandatory Takeaways & Engineering Resolutions:**
  1. **In-Memory Cache with Write Invalidation**:
     - Implemented thread-safe `labelCache` with a 60-second TTL in `services/go-server/internal/archiver/handler.go`.
     - Read misses populate via a 25-goroutine worker pool, dropping reload times from 7.92s to < 10ms for subsequent requests.
     - Any label mutation (`POST /api/archiver/labels`) immediately invalidates the corresponding partition cache, guaranteeing read-after-write consistency.
  2. **Client-Side Token Bucket Rate Limiting**:
     - Upgraded `tools/eval/batch_auto_label.py` with an 8.0 RPS Token Bucket rate limiter, exponential backoff respecting HTTP 429 `Retry-After` headers, and frame deduplication (`--unique-cameras`).
     - Exposed standard shortcut `make batch-auto-label` in root `Makefile`.
  3. **Config Quote & Whitespace Sanitization**:
     - Added `strings.Trim(slot, "\"' \t")` in `internal/harvester/scheduler.go` and `internal/config/config.go` with unit test validation.
     - Stripped trailing whitespace in `config.go` before `strconv.ParseBool` and `strconv.Atoi`, preventing invisible trailing spaces in `.env` from silently discarding `HARVESTER_ENABLED=true` into `false`.
  4. **Boot Visibility & Observability**:
     - Added explicit `[INIT] Autonomous harvester enabled (...)` and `[INIT] Autonomous harvester disabled (...)` logging in `cmd/server/main.go` on server boot, eliminating silent subsystems.
  5. **Ergonomic Web Labeler UI**:
     - Constrained layout to `100vh - 49px` with independent sidebar scrolling in `apps/web/src/app/labeler/page.tsx`.
     - Added dedicated "Làm Mới (Refresh)" button allowing instant catalog reload without browser F5.
