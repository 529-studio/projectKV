## Lesson 45: Crowdsourced Ground-Truth Ingestion, IP-Hashed Privacy, and In-Memory Rate Limiting

- **Incident & Tracking:** Issue #249, Kanban **[KV-3]**, Sprint 7 Task S7-T4 / Sprint 8 Task S8-T2 (`feat/s7-t4-feedback-r2-archiver`).
- **Symptom:**
  Commuters submitting flood observations from `CameraModal.tsx` (`[Báo cáo ngập tại đây]`) received HTTP 404 because the Go backend had no corresponding endpoint. Without server-side rate limiting and ingestion, malicious or rapid accidental submissions could exhaust backend storage, spam Cloudflare R2, and corrupt ground-truth ML training datasets.
- **Root Cause:**
  1. Frontend tactile reporting button was wired in S7-T3 with a 30-second client-side cooldown, but the backend lacked the corresponding `internal/feedback` module.
  2. Leaf Modulith Integrity: Ingesting feedback required checking camera existence, acquiring snapshot frames from proxycache, and persisting manifests to Cloudflare R2. Doing so naively by importing sibling packages (`camera`, `proxycache`, `storage`) would violate `GO_BACKEND_ARCHITECTURE.md`.
  3. Privacy vs. Abuse Prevention: Storing raw commuter IP addresses in long-term object storage violates privacy standards; omitting IP completely would preclude rate limiting and forensic spam detection.
- **Mandatory Takeaways & Engineering Resolutions:**
  1. **Pure Leaf Package Architecture (`internal/feedback/`)**:
     - Conforms strictly to Leaf Modulith principles with zero internal sibling imports.
     - Injected consumer-defined interfaces (`CameraChecker`, `SnapshotFetcher`, `StorageUploader`) allow `cmd/server/main.go` (composition root) to inject adapters querying `camCatalog.ByID(id)`, fetching snapshots from `proxyCache.Get(id)` with upstream fallback, and uploading to `storage.Storage`.
  2. **Concurrent In-Memory Sliding Rate Limiter**:
     - Implemented `RateLimiter` with `sync.RWMutex` enforcing max 1 report per client IP per camera per 5 minutes (`DefaultRateLimitCooldown = 5 * time.Minute`).
     - Returns HTTP 429 Too Many Requests with standard `Retry-After: <seconds>` header and structured JSON payload (`retry_after_seconds`). Verified under 50 concurrent goroutines with zero data races.
  3. **Privacy Preservation & Cloudflare R2 Ground-Truth Manifest**:
     - Hashed client IP with SHA-256 (`fmt.Sprintf("%x", sha256.Sum256([]byte(clientIP)))`). Raw client IPs are never written to Cloudflare R2 manifests or persistent logs.
     - Asynchronously fetches snapshot frame from cache/upstream, generates metadata JSON manifest (`trust: "unverified"`, `source: "user_reported"`, `has_snapshot: bool`), and archives both JPEG snapshot and JSON manifest to Cloudflare R2 under `feedback/<YYYY-MM-DD>/<camera_id>_<unix_ts>.jpg` and `.json`.
  4. **Dual Route & Swagger Synchronization**:
     - Mounted `POST /api/v1/cameras/:id/report-flood` and legacy alias `POST /api/cameras/:id/report-flood`.
     - Regenerated Swagger API documentation (`make swagger`), passing all `architecture-guardian` checks.
