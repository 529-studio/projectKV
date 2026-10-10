# Cloudflare R2 Storage Integration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement Cloudflare R2 object storage integration (`warded-proofs` bucket) for `services/go-server` snapshot archiving and downstream labeling tooling, providing seamless cloud storage while preserving local storage fallback.

**Architecture:** Implement `R2Storage` inside `services/go-server/internal/storage/` fulfilling the existing `storage.Storage` interface via AWS SDK for Go v2 with S3 Path-Style addressing. Decouple via configuration (`STORAGE_DRIVER=local|r2`), support public edge URL mapping (`https://file.529studio.site`), align snapshot key timestamps with Vietnam ICT (`Asia/Ho_Chi_Minh`), and provide both mock unit tests and end-to-end verification tooling.

**Tech Stack:** Go 1.26, AWS SDK for Go v2 (`github.com/aws/aws-sdk-go-v2`, `aws-sdk-go-v2/service/s3`), Cloudflare R2 (S3 API compatible), Python 3 (`boto3` for CV tooling).

**Spec:** DevOps Guide: "Hướng Dẫn Tích Hợp Cloudflare R2 Storage (Bucket: warded-proofs)" and `services/go-server/internal/storage/storage.go`.

---

## Issue Estimation (AGENTS.md Rule 2)
- **Active Effort:** ~8 active hours (1 engineer day)
- **Expected Delivery Window:** 2026-10-11
- **Target Branch:** `feat/r2-storage-integration`
- **Target PR:** Targeting `dev` (Zero direct push to `dev`/`main`)

---

## Global Constraints

- **Monochromatic & Zero Emoji:** AGENTS.md Rule 1 prohibits emojis in UI and backend logs. All commit messages and documentation must be 100% English.
- **Leaf Modulith Pattern:** `services/go-server/internal/storage/` must remain a leaf package with ZERO imports of other sibling packages under `internal/`.
- **Approved Dependencies:** External dependencies in `go.mod` must be strictly managed. AWS SDK v2 is flagged for explicit architecture review. Banned packages (ORM, Viper, Cobra) are strictly prohibited.
- **Backward Compatibility:** Default `STORAGE_DRIVER` remains `local` to ensure local development environments without R2 credentials continue functioning out of the box.
- **Key Naming Convention:** Object key pattern must strictly match `YYYY-MM-DD/<camera_id>/<unix_timestamp>_<reason>.jpg` using Vietnam ICT (`UTC+7`, `Asia/Ho_Chi_Minh`) for the date hierarchy.
- **Content-Type:** Snapshots uploaded to R2 must include `Content-Type: image/jpeg`.

---

## Review Focus

1. **Path Traversal & Malformed Keys:** Ensure `R2Storage` sanitizes paths, rejecting directory traversal (`../`), null bytes, or leading slashes that could cause malformed S3 object keys.
2. **Error Translation:** Ensure S3 404 / `NoSuchKey` errors are mapped directly to `storage.ErrNotFound` so callers (like `archiver`) handle missing objects uniformly without knowing the driver.
3. **Context Cancellation & Timeouts:** Verify that cancelled contexts abort in-flight uploads/downloads without hanging or leaking goroutines/resources.
4. **Streaming & Memory Efficiency:** Handle `io.Reader` without buffering unbounded files into memory while ensuring S3 payload size / content length requirements are met.
5. **Fail-Fast Startup:** When `STORAGE_DRIVER=r2` is explicitly configured, invalid or missing credentials must trigger a clear fatal error at startup rather than silent failure during runtime traffic events.

---

## Task Decomposition

### Task 1: Add AWS SDK for Go v2 Dependencies

**Files:**
- Modify: `services/go-server/go.mod`
- Modify: `services/go-server/go.sum`

**Interfaces:**
- Consumes: Go toolchain
- Produces: AWS SDK v2 modules in `go.mod`

- [ ] **Step 1: Add AWS SDK v2 packages to `services/go-server`**
  Run:
  ```bash
  cd services/go-server
  go get github.com/aws/aws-sdk-go-v2
  go get github.com/aws/aws-sdk-go-v2/config
  go get github.com/aws/aws-sdk-go-v2/credentials
  go get github.com/aws/aws-sdk-go-v2/service/s3
  ```

- [ ] **Step 2: Verify `go.mod` tidy and buildability**
  Run: `cd services/go-server && go mod tidy && go vet ./...`
  Expected: Clean exit code 0.

- [ ] **Step 3: Commit**
  ```bash
  git add services/go-server/go.mod services/go-server/go.sum
  git commit -m "build(go-server): add AWS SDK v2 dependencies for R2 storage"
  ```

---

### Task 2: Extend Configuration for R2 Storage

**Files:**
- Modify: `services/go-server/internal/config/config.go`
- Modify: `services/go-server/.env.example`
- Modify: `services/go-server/internal/config/config_test.go` (or add tests)

**Interfaces:**
- Consumes: Environment variables (`R2_ACCOUNT_ID`, `R2_ENDPOINT`, `R2_BUCKET_NAME`, `R2_REGION`, `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY`, `R2_PUBLIC_URL`, `STORAGE_DRIVER`)
- Produces: `cfg.R2Endpoint`, `cfg.R2BucketName`, `cfg.R2AccessKeyID`, `cfg.R2SecretAccessKey`, `cfg.R2Region`, `cfg.R2PublicURL`, `cfg.StorageDriver` in `ServerConfig` and `Config`

- [ ] **Step 1: Write test for R2 configuration parsing**
  Add unit test in `services/go-server/internal/config/config_test.go`:
  Verify that setting R2 environment variables populates `ServerConfig` fields correctly, and defaults to `STORAGE_DRIVER=local` when unset.

- [ ] **Step 2: Run test to verify it fails**
  Run: `go test ./internal/config/... -v`
  Expected: FAIL on undefined R2 fields.

- [ ] **Step 3: Implement R2 configuration fields in `internal/config/config.go`**
  Add fields to `ServerConfig` and `Config`:
  - `R2AccountID string`
  - `R2Endpoint string`
  - `R2BucketName string`
  - `R2Region string`
  - `R2AccessKeyID string`
  - `R2SecretAccessKey string`
  - `R2PublicURL string`
  Parse from environment in `LoadConfig()` and `LoadFromEnv()`, setting defaults:
  - `R2_BUCKET_NAME`: default `"warded-proofs"`
  - `R2_REGION`: default `"auto"`
  - `R2_PUBLIC_URL`: default `"https://file.529studio.site"`
  - If `R2_ENDPOINT` is empty but `R2_ACCOUNT_ID` is set, format: `https://<R2_ACCOUNT_ID>.r2.cloudflarestorage.com`.

- [ ] **Step 4: Update `.env.example` with R2 configuration template**
  Add commented or documented entries matching the DevOps guide.

- [ ] **Step 5: Run tests to verify they pass**
  Run: `go test ./internal/config/... -v`
  Expected: PASS

- [ ] **Step 6: Commit**
  ```bash
  git add services/go-server/internal/config/ services/go-server/.env.example
  git commit -m "feat(config): add Cloudflare R2 storage parameters"
  ```

---

### Task 3: Implement `R2Storage` satisfying `storage.Storage`

**Files:**
- Create: `services/go-server/internal/storage/r2.go`

**Interfaces:**
- Consumes: AWS SDK v2 `s3.Client`, `io.Reader`, `context.Context`
- Produces: `R2Storage` struct implementing `storage.Storage`:
  ```go
  type R2Storage struct { ... }
  func NewR2Storage(ctx context.Context, cfg R2Config) (*R2Storage, error)
  func (s *R2Storage) Put(ctx context.Context, path string, data io.Reader) error
  func (s *R2Storage) Get(ctx context.Context, path string) (io.ReadCloser, error)
  func (s *R2Storage) Delete(ctx context.Context, path string) error
  func (s *R2Storage) Exists(ctx context.Context, path string) (bool, error)
  func (s *R2Storage) PublicURL(path string) string
  ```

- [ ] **Step 1: Define `S3API` interface for dependency injection and mocking**
  Define minimal `S3API` interface matching `s3.Client` methods:
  - `PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)`
  - `GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)`
  - `DeleteObject(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)`
  - `HeadObject(ctx context.Context, params *s3.HeadObjectInput, optFns ...func(*s3.Options)) (*s3.HeadObjectOutput, error)`

- [ ] **Step 2: Implement path sanitization and validation**
  Reject path traversal (`..`), absolute paths, null bytes. Strip leading slashes to produce clean S3 object keys.

- [ ] **Step 3: Implement `Put` with MIME detection and S3 client call**
  - Read stream or buffer safely. Camera snapshots are 20KB-500KB. For streaming readers, buffer or read payload to provide `io.ReadSeeker` or known length if required by S3 SDK.
  - Set `ContentType: "image/jpeg"` for `.jpg`/`.jpeg` keys.
  - Put object to configured bucket.

- [ ] **Step 4: Implement `Get`, `Delete`, and `Exists` with error translation**
  - Translate S3 `NoSuchKey` and 404 responses into `storage.ErrNotFound`.
  - Return `output.Body` on `Get`.
  - Check existence before returning `true` on `Exists`.
  - Support `PublicURL(path)` returning `<PublicBaseURL>/<cleanKey>`.

- [ ] **Step 5: Implement `NewR2Storage` constructor**
  - Use `credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, "")`.
  - Configure `s3.Options`: `BaseEndpoint = aws.String(cfg.Endpoint)` and `UsePathStyle = true` (critical for Cloudflare R2).
  - Region set to `cfg.Region` (default `"auto"`).

---

### Task 4: Unit Testing `R2Storage` with Mock S3 Client

**Files:**
- Create: `services/go-server/internal/storage/r2_test.go`

**Interfaces:**
- Consumes: `R2Storage`, `S3API` mock implementation
- Produces: Test coverage for `Put`, `Get`, `Delete`, `Exists`, `PublicURL`, path traversal rejection, error translation

- [ ] **Step 1: Create mock S3 client in test**
  Implement in-memory mock implementing `S3API`.

- [ ] **Step 2: Test `Put` and `Get` roundtrip**
  Verify content matches and `ContentType` is set to `"image/jpeg"`.

- [ ] **Step 3: Test `Exists` for present and missing keys**
  Verify `Exists` returns `true, nil` when present and `false, nil` when 404.

- [ ] **Step 4: Test `Delete` and missing key handling**
  Verify missing key on `Get` returns `storage.ErrNotFound`.

- [ ] **Step 5: Test path traversal rejection**
  Verify `../../secret.jpg` and `/root/file.jpg` return `storage.ErrInvalidPath`.

- [ ] **Step 6: Test context cancellation**
  Verify cancelled context returns `context.Canceled`.

- [ ] **Step 7: Run storage tests**
  Run: `go test ./internal/storage/... -v`
  Expected: PASS

- [ ] **Step 8: Commit**
  ```bash
  git add services/go-server/internal/storage/
  git commit -m "feat(storage): implement R2Storage with unit tests"
  ```

---

### Task 5: Wire Storage Driver Selection in `main.go` and Update `archiver` Timezone

**Files:**
- Modify: `services/go-server/cmd/server/main.go`
- Modify: `services/go-server/internal/archiver/policy.go`
- Modify: `services/go-server/internal/archiver/archiver_test.go`

**Interfaces:**
- Consumes: `cfg.StorageDriver`, `storage.NewLocalStorage`, `storage.NewR2Storage`
- Produces: Pluggable storage backend in snapshot archiver; Vietnam ICT (`Asia/Ho_Chi_Minh`) timestamp alignment

- [ ] **Step 1: Update `StoragePath` in `archiver/policy.go` to Vietnam ICT (`UTC+7`)**
  Ensure date directory formatting strictly conforms to Vietnam time `Asia/Ho_Chi_Minh` (`time.FixedZone("ICT", 7*3600)`), matching the DevOps specification.
  Update `TestStoragePath` in `archiver_test.go` to verify ICT date calculation across midnight boundaries.

- [ ] **Step 2: Run archiver tests**
  Run: `go test ./internal/archiver/... -v`
  Expected: PASS

- [ ] **Step 3: Update `cmd/server/main.go` storage instantiation**
  - Check `cfg.StorageDriver`:
    - If `"r2"` or `"s3"`: initialize `storage.NewR2Storage(...)`. If initialization fails, log fatal error to prevent misconfigured production boots.
    - If `"local"`: initialize `storage.NewLocalStorage(...)`.
  - Pass the instantiated `storage.Storage` to `archiver.New`.
  - If `StorageDriver` is `"r2"`, skip local filesystem retention walk or configure cloud retention mode.

- [ ] **Step 4: Run server tests and compile check**
  Run: `go build -o /dev/null ./cmd/server`
  Expected: Clean build with code 0.

- [ ] **Step 5: Commit**
  ```bash
  git add services/go-server/cmd/server/main.go services/go-server/internal/archiver/
  git commit -m "feat(server): wire R2 storage driver and align archiver timezone with ICT"
  ```

---

### Task 6: Build Verification Tooling & E2E Validation

**Files:**
- Create: `tools/storage/verify_r2.go` (or `tools/storage/verify_r2.py`)
- Test against live Cloudflare R2 bucket `warded-proofs`

**Interfaces:**
- Consumes: `.env` R2 credentials
- Produces: Verified upload, head, read, and delete on Cloudflare R2 edge

- [ ] **Step 1: Implement verification script**
  Create `tools/storage/verify_r2.go`:
  - Loads `.env`
  - Uploads a test snapshot JPEG to `test-connectivity/<timestamp>_sample.jpg`
  - Verifies `Exists` returns true
  - Reads object bytes back and checks byte integrity
  - Checks public edge URL accessibility via HTTP GET `https://file.529studio.site/test-connectivity/...`
  - Deletes test object and verifies cleanup

- [ ] **Step 2: Execute verification script against live R2**
  Run: `go run tools/storage/verify_r2.go`
  Expected: Successful upload, public URL check, and cleanup verification.

- [ ] **Step 3: Commit**
  ```bash
  git add tools/storage/
  git commit -m "chore(tools): add R2 storage verification CLI script"
  ```

---

### Task 7: Architecture Review & Living Documentation Maintenance

**Files:**
- Modify: `docs/project_status.md`
- Modify: `docs/lesson_learn.md`

- [ ] **Step 1: Run Architecture Guardian checks**
  Run:
  ```bash
  cd services/go-server
  gofmt -l .
  go vet ./...
  go test ./...
  ```
  Verify `storage` remains a leaf package with zero imports of other `internal/` packages.

- [ ] **Step 2: Update `docs/project_status.md` and `docs/lesson_learn.md`**
  - Record Cloudflare R2 integration, dual storage driver architecture, and ICT timestamp standard.
  - Record lesson learned regarding S3 Path-Style addressing for Cloudflare R2 and timezone normalization.

- [ ] **Step 3: Commit**
  ```bash
  git add docs/project_status.md docs/lesson_learn.md
  git commit -m "docs: record Cloudflare R2 integration in living documentation"
  ```

---

## Verification Plan

### Automated Tests
1. **Unit Tests:**
   ```bash
   cd services/go-server
   go test -v ./internal/storage/...
   go test -v ./internal/config/...
   go test -v ./internal/archiver/...
   ```
2. **Whole Server Test Suite:**
   ```bash
   cd services/go-server
   go test ./...
   ```
3. **Format & Vet:**
   ```bash
   cd services/go-server
   gofmt -l .
   go vet ./...
   ```

### Live End-to-End Verification
1. Run the R2 verification script:
   ```bash
   go run tools/storage/verify_r2.go
   ```
2. Start the server with `STORAGE_DRIVER=r2 ANALYZER=mock APP_ENV=dev`:
   ```bash
   cd services/go-server
   STORAGE_DRIVER=r2 go run cmd/server/main.go
   ```
   Verify logs confirm: `[INIT] Cloudflare R2 storage driver initialized (bucket=warded-proofs)` and snapshots are saved without errors.
