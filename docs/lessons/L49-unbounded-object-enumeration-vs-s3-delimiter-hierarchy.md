## Lesson 49: Unbounded Object Enumeration vs. S3 Delimiter Hierarchy (Cloudflare 524 Edge Timeout Elimination)

- **Incident & Tracking:** Issue #279, Kanban **[KV-14]** (`feat/s8-optimize-list-dates`, PR targeting `dev`).
- **Symptom:**
  1. Frontend labeler (`/labeler`) and developer API clients querying `GET /api/archiver/dates` suffered catastrophic latency degradation and frequent failures on Staging (`warded-stg.529studio.site`).
  2. Successful requests took **2 minutes 40 seconds (160s)**:
     ```text
     [GIN] 2026/10/21 - 09:35:57 | 200 | 2m40s | 127.0.0.1 | GET "/api/archiver/dates"
     ```
  3. Subsequent requests exceeded proxy context deadlines, crashing with HTTP 500 (`failed to list storage items`) and Cloudflare HTTP 524 (*A timeout occurred*):
     ```text
     [GIN] 2026/10/21 - 10:07:49 | 500 | 2m0s | 172.20.0.12 | GET "/api/archiver/dates"
     [GIN] 2026/10/21 - 10:10:04 | 500 | 1m0s | 172.20.0.12 | GET "/api/archiver/dates"
     ```
- **Root Cause:**
  1. **Unbounded Storage Traversal**: In `services/go-server/internal/archiver/handler.go`, `ListDates` invoked `h.store.List(c.Request.Context(), "")` to discover available snapshot partition dates.
  2. **Missing S3 Delimiter Semantics**: When `STORAGE_DRIVER=r2`, `R2Storage.List` executed unconstrained `s3.ListObjectsV2` pagination over the entire `warded-proofs` bucket without specifying a `Delimiter`.
  3. **Data Accumulation Threshold Breach**: With over 186,000 objects in the bucket, this issued **186+ sequential HTTP round-trips** from `edge host` to Cloudflare R2 APAC, exceeding the 100s Cloudflare Edge Proxy timeout and the Gin context deadline.
- **Mandatory Takeaways & Engineering Resolutions:**
  1. **Hierarchical Directory Discovery (`storage.ListDirectories`)**:
     - Extended core `storage.Storage` interface with `ListDirectories(ctx context.Context, prefix string) ([]string, error)` separating shallow directory discovery from deep object listing.
     - In `R2Storage`, configured `s3.ListObjectsV2Input{Delimiter: aws.String("/")}` and extracted `CommonPrefixes`. A single API request groups 186,000+ objects into top-level date partitions in **< 700ms**, delivering a **> 220x speedup** (from 160s down to 0.6s) with zero edge proxy timeouts.
     - In `LocalStorage`, replaced recursive `filepath.WalkDir` with shallow `os.ReadDir`, maintaining 100% offline autonomy without disk walking overhead.
  2. **Strict Verification Harness Expansion**:
     - Updated standalone verification utility `cmd/verify-r2/main.go` with Test 7/7 validating live sub-second `ListDirectories` execution against Cloudflare R2.
     - Added comprehensive unit tests in `internal/storage/storage_test.go`, `internal/storage/r2_test.go`, and `internal/archiver/handler_test.go` ensuring path traversal safety and non-date directory exclusion.
