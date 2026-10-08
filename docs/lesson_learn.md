# Engineering Lessons Learned & Post-Mortem Register

**Project:** ProjectKV (Ho Chi Minh City Real-Time Urban Traffic & Flood Monitoring)  
**Document Version:** 1.0.3  
**Last Updated:** 2026-10-07  
**Governing Rule:** Codebase, git history, and PR list are the sole sources of truth. All lessons recorded here represent verified technical failures, root causes, and mandatory operational guardrails.

---

## Lesson 1: Passing Unit Tests is Never Sufficient (Upstream WAF & Header Blackholing)

- **Incident & Tracking:** Issue #161, Pull Request #162 (`fix/cvengine-snapshot-user-agent`).
- **Symptom:**
  During live staging audit (`local-homesrv` with `ANALYZER=onnx`), `warded-backend` logged recurring timeout warnings:
  ```
  WARN analysis failed, degrading to unknown camera=... error="cv-engine: fetch snapshot: Get \"https://giaothong.hochiminhcity.gov.vn/render/ImageHandler.ashx?id=...\": context deadline exceeded"
  ```
  Meanwhile, the Go backend unit test suite (`go test -race ./...`) passed 100% with zero failures.
- **Root Cause:**
  In Go standard library `net/http`, when `http.NewRequestWithContext()` is called without explicitly setting a `User-Agent` header, Go's transport layer defaults to sending `User-Agent: Go-http-client/1.1`. The upstream traffic portal (`https://giaothong.hochiminhcity.gov.vn`) is protected by a WAF that silently drops inbound TCP packets containing `Go-http-client/*` without sending a TCP RST or HTTP error response. The connection hung until the 10-second client timeout expired.  
  The unit test suite passed because `httptest.NewServer` in `cvengine_test.go` mocked `/snapshot/` without asserting the presence of a `User-Agent` header, creating a false sense of security.
- **Mandatory Takeaway:**
  1. **End-to-End Simulation**: When investigating any bug or regression, you MUST simulate and reproduce the failure end-to-end against the real network/user interface before writing fixes.
  2. **Mock Contract Enforcement**: Unit test mocks must strictly enforce upstream contract constraints (e.g. rejecting requests that lack required headers) to prevent blind tests.

---

## Lesson 2: Sequence Inversion Risk & Invisible Downstream Divergence

- **Incident & Tracking:** Issue #136 (S2) deferred past Issue #137 (S3), #138 (S4), #140 (S5b); resolved via PR #159.
- **Symptom:**
  S2 (Hot Set camera curation) was scheduled in Wave 1 between S1 (capacity spike) and S3 (OpenVINO analyzer). S3 bypassed S2 by hardcoding numerical capacity targets (`HotsetFixedMaxCameras = 57`, `HotsetIntervalSeconds = 30`) in `config.go`.
- **Root Cause:**
  While decoupling allowed rapid development of S3, S4, and S5b, the Go backend's temporary fallback seeder in `main.go` selected cameras via crude keyword matching (`"điện biên phủ"`, `"võ văn kiệt"`) without verifying `len(road_keys) == 2`. Consequently, the OpenVINO model was running inference on straight-road mid-block cameras and arbitrary padding rather than genuine intersection choke points.
- **Mandatory Takeaway:**
  1. **Document Sequence Inversions**: If a dependency sequence is inverted to unblock velocity, the architectural divergence and its downstream implications must be explicitly disclosed in sprint documentation.
  2. **Catalog Reconciliation**: Verify that hardcoded assumptions exactly match empirical catalog data as soon as the deferred task is completed (PR #159 reconciled exactly 57 intersection cameras).

---

## Lesson 3: Completion Claims vs. Production Reality Gap

- **Incident & Tracking:** ADR-004 §8 (Sprint 2 A4 carryover omission), ADR-005 (Sprint 3 production audit).
- **Symptom:**
  Sprint completion reports declared milestones as "100% completed" and "production-grade," yet live staging inspections showed pending deployment tasks (e.g., physical model weights had not yet been ingested into the `warded-model-weights` volume on `local-homesrv`, and the D6 evaluation accuracy gate had never been run against real labeled camera frames).
- **Root Cause:**
  Equating local development completion, test passes, or PR merges with live production deployment and verified operational readiness.
- **Mandatory Takeaway:**
  1. **Definition of Done Hardening (ADR-005 T8)**: Any issue claiming that a feature delivers real (non-mock) data to users must include a verified live production check (an actual API response or screenshot from the real deployment host, not just a passing unit test) before it can be closed.
  2. **Truthful Reporting**: Never describe unverified or undeployed code as production-ready in project documentation.

---

## Lesson 4: Tooling & Environment Discrepancies on Host Platforms

- **Incident & Tracking:** Local Colima & Docker Compose onboarding (`make dev-model`).
- **Symptom:**
  Executing `make dev-model` (`docker compose -f docker-compose.local.yml up -d`) failed on developer macOS machines with:
  ```
  unknown shorthand flag: 'f' in -f
  Usage: docker [OPTIONS] COMMAND [ARG...]
  make: *** [dev-model] Error 125
  ```
- **Root Cause:**
  On macOS systems using Homebrew without Docker Desktop, `docker-compose` is installed as a standalone binary in `/opt/homebrew/bin/docker-compose`, rather than as a Docker CLI plugin in `~/.docker/cli-plugins/docker-compose`. As a result, the Docker CLI did not recognize the subcommand `compose` (with space) and attempted to parse `-f` as a root flag.
- **Mandatory Takeaway:**
  1. **Multi-Environment Resilience**: Tooling scripts and Makefiles should support both standalone binaries and CLI plugins, or document exact plugin symlinking:
     ```bash
     mkdir -p ~/.docker/cli-plugins && ln -sfn $(which docker-compose) ~/.docker/cli-plugins/docker-compose
     ```

---

## Lesson 5: Zero-Emoji Standard & Telemetry Stream Corruption

- **Incident & Tracking:** PR #152 (`fix(web): replace hourglass emoji with svg and enforce zero-emoji rule`).
- **Symptom:**
  Unicode emojis (such as hourglass, pin, warning, and rain characters) were originally used in loading states, map chips, and backend logging.
- **Root Cause:**
  Emojis rendered inconsistently across operating systems (iOS vs Android vs Windows vs macOS), violated the monochromatic high-contrast design philosophy, caused multi-byte UTF-8 encoding corruption in headless environments, and broke regex parsing in CI/CD telemetry aggregators (e.g. Loki, Datadog).
- **Mandatory Takeaway:**
  1. **Strictly Zero Emoji**: All UI controls and indicators MUST use monochromatic, scalable vector SVGs (`fill="none" stroke="currentColor"`).
  2. **Structured Logging**: Backend logs MUST strictly use standardized uppercase prefix tags (`[INIT]`, `[INFO]`, `[WARN]`, `[ERROR]`, `[FATAL]`) and key-value attributes.

---

## Lesson 6: Single-Threaded HTTP Servers vs. Concurrent Go Dispatchers (Queue Buildup & BrokenPipeError)

- **Incident & Tracking:** Issue #167, Pull Request #168 (`fix/167-cvengine-threading-httpserver`).
- **Symptom:**
  During hotset analysis cycles, `go-server` logged recurrent timeout failures:
  ```
  WARN analysis failed, degrading to unknown camera=... error="cv-engine: POST /predict: Post \"http://localhost:8000/predict\": context deadline exceeded"
  ```
  Concurrently, `cv-engine` logged unhandled socket error tracebacks:
  ```
  BrokenPipeError: [Errno 32] Broken pipe
  ```
- **Root Cause:**
  `services/cv-engine/server.py` instantiated the default single-threaded `http.server.HTTPServer`. Meanwhile, `go-server`'s `hotset.Scheduler` dispatched up to 10 parallel goroutines (`HotsetMaxParallel = 10`) with a 10s per-call context timeout (`context.WithTimeout(ctx, 10*time.Second)`).  
  Because `HTTPServer` processed incoming requests strictly sequentially, requests piled up in the OS TCP backlog. Later requests in the batch exceeded the 10s client deadline while waiting in the queue. The Go client aborted the context and closed the TCP connection. When Python finally dequeued the socket and called `self.wfile.write(body)`, writing to the closed connection raised `BrokenPipeError: [Errno 32]`. Python's `BaseHTTPRequestHandler` did not catch `BrokenPipeError`, dumping an unhandled stack trace to `stderr`.
- **Mandatory Takeaway:**
  1. **Threaded Microservice Servers**: Any internal service receiving concurrent requests from Go worker pools MUST use multi-threaded or asynchronous server architectures (`ThreadingHTTPServer`, FastAPI/Uvicorn, or worker pools) rather than single-threaded sequential servers.
  2. **Defensive Socket Error Handling**: Network services must catch `(BrokenPipeError, ConnectionResetError)` on client write operations to gracefully handle client timeouts and connection drops without dumping traceback noise.
  3. **Inference Concurrency Verification**: Verify runtime thread-safety (such as OpenVINO `create_infer_request()`) under multi-threaded load to ensure parallelism does not introduce race conditions or segfaults.

---

## Lesson 7: Upstream Timestamp Resolution Mismatch & Mock Contract Blind Spots

- **Incident & Tracking:** Issue #172, Pull Request (`fix/172-weather-hourly-forecast-current-hour`).
- **Symptom:**
  At local daytime (e.g. 14:20 GMT+7 in Vietnam), the Weather Widget popup displayed the next 6-hour rain forecast as `00:00` to `05:00` (midnight to early morning) instead of `14:00` to `19:00`, and the current precipitation probability displayed the 00:00 slot value (64%) instead of the current hour's probability.
- **Root Cause:**
  1. **Resolution Divergence**: Upstream Open-Meteo returns `current.time` at 15-minute intervals (e.g. `"2026-10-06T14:15"`), whereas `hourly.time` slots are strictly on-the-hour (`"2026-10-06T14:00"`, `"2026-10-06T15:00"`).
  2. **Naive String Equality**: In `services/go-server/internal/weather/service.go`, exact string comparison `if h.Time == currentTimeStr` always evaluated to `false`. Consequently, `currentIdx` defaulted to `0` (midnight), and `prob` fell back to `hourlyList[0].PrecipitationProbability`.
  3. **Single-Day Lookahead Truncation**: Querying `forecast_days=1` only yielded 24 hours of data. If queried late in the evening (e.g. 20:00), the 12-hour slice capped at 24:00, providing only 4 hours instead of a full lookahead into tomorrow morning.
  4. **Mock Blind Spot (Reaffirming Lesson 1)**: Unit test fixtures hardcoded `current.time` and `hourly.time` both to `"2026-10-02T15:00"`, hiding the upstream API contract constraint during automated testing.
- **Mandatory Takeaway:**
  1. **Timestamp Normalization**: When matching time series with varying sample intervals (15-min current vs. 60-min hourly), normalize timestamps to the target resolution bucket (`truncateToHour`) before index lookup.
  2. **Cross-Midnight Horizon Buffering**: Multi-hour rolling lookaheads across midnight require querying at least 2 forecast days (`forecast_days=2`) to guarantee an unconstrained rolling window.
  3. **Realistic Mock Contracts**: Test fixtures must simulate real-world API responses with non-zero minute offsets and multi-day arrays to prevent false positives.

---

## Lesson 8: GitHub Actions Billing Ceiling Rounding & Duplicate Verification Jobs

- **Incident & Tracking:** Issue #173 (GitHub Actions compute audit, 2026-10-01 to 2026-10-06).
- **Symptom:**
  Within the first 6 days of October 2026 the `529-studio` organization consumed 971 of its 2,000 monthly GitHub Free minutes (48.55%). Actual runner execution time across 563 active jobs was only 556m 50s; the remaining 414 minutes (74.5%) were pure billing overhead.
- **Root Cause:**
  1. **Per-job 1-minute ceiling rounding**: GitHub bills every job rounded up to the nearest whole minute. Micro-jobs (`Auto Request Reviewers`, `Branch Protection`) ran 5–10s each but were billed a full minute, turning ~8 minutes of real work into 87 billable minutes.
  2. **Duplicate component verification**: `go-ci.yml` and `ci.yml`'s `verify-backend` job both triggered on `pull_request` for the same `services/go-server/**` paths and both executed `go build`/`go vet`/`go test`, doubling backend verification minutes.
  3. **Redundant push re-verification**: `ci.yml` also carried a `push` trigger, re-running the exact verification already performed on the (mandatory, branch-guard-enforced) pull request after every merge into `dev`/`main`.
  4. **No concurrency cancellation**: rapid push iterations spawned parallel pipeline runs on superseded commits (245 runs over 2026-10-01 to 2026-10-04).
- **Mandatory Takeaway:**
  1. **Steps are free, jobs are not**: whenever logic can be folded into an already-running job as a step, do that instead of creating a new job — each additional job adds a 1-minute minimum.
  2. **One component, one verification workflow/event**: never define two workflows that verify the same component on the same trigger; consolidate to a single job to avoid paying twice.
  3. **Verify on `pull_request`, build/deploy on `push`**: since direct pushes to protected branches are forbidden, post-merge verification re-runs are pure waste.
  4. **Always set `concurrency: { group, cancel-in-progress: true }`** on PR/push workflows so superseded runs are cancelled instead of consuming quota.
  5. **Free-tier private repos have no branch protection**: `dev`/`main` report `protected: false` and the branch-protection API returns 403, so custom enforcement (`branch-guard.yml`) is required and an aggregation check like `ci-gate` has no gating power.

---

## Lesson 9: Competing Sources of Truth & Silent Drift (Ghost Camera Catalog Resync)

- **Incident & Tracking:** Issue #178, Pull Request #179 (`fix/178-camera-catalog-ghost-resync`).
- **Symptom:**
  Users clicking cameras that the map showed as online were greeted with the snapshot failure modal (`KHÔNG THỂ TẢI HÌNH ẢNH SNAPSHOT — Máy chủ proxy báo lỗi 502/503`). A full live audit of all 837 feeds found **712 live** and **125 placeholder/ghost** at scan time, while the committed catalog still advertised 711 cameras as `UP` — including **21 cameras returning the 2,638-byte "IMAGE NOT AVAILABLE" placeholder**.
- **Root Cause:**
  1. **Two competing sources of truth**: static liveness lived both in the JSON catalog (`isOnline`/`status`, sourced from the DOT portal's own — unreliable — status field) and in a hardcoded `KnownGhostCameras` map in `internal/camera/catalog.go`. The two drifted independently: the map marked several *recovered* cameras (`5a607e298576340017d0666b`, `5deb576d1dc17d7c5515ad02`, …) as ghost (false positives) while missing the actual failing set.
  2. **Trusting an upstream status field**: the portal reported `UP` for cameras that only served placeholder frames, so the catalog's `isOnline` could not be trusted without byte-level probing.
  3. **A verifier that could not detect the failure**: the audit script's `brokenOnlineCams` filter only matched `TIMEOUT` / `HTTP_*` / `NETWORK_ERROR`, never `GHOST_PLACEHOLDER`, so its report computed `brokenOnlineCamerasCount: 0` — silently masking the 21 real failures.
- **Mandatory Takeaway:**
  1. **One source of truth for derived state**: never hardcode a second copy of state that can be derived (e.g. ghost membership). Derive it from a generated catalog produced by live probes, and keep a single authoritative path.
  2. **Verify the verifier**: any audit/telemetry filter must be exercised against a known-positive case. A report claiming "0 failures" is a red flag when field reality contradicts it.
  3. **Classify deterministic vs. transient outcomes**: only promote deterministic signals (placeholder/empty body, live JPEG) into persisted state; transient network faults must not churn the catalog.
  4. **Feedback runtime truth into shared state**: the web client now promotes a `503 GHOST_CAMERA` response into shared UI state so the map, sidebar, dock and modal converge immediately, instead of waiting for the next catalog resync.

---

## Lesson 10: CD Pipeline Configuration Overwrite & Environment Propagation

- **Incident & Tracking:** Issue #168, Pull Request (`fix/staging-compose-app-env`).
- **Symptom:**
  Manual updates made directly to Portainer Stack 14 environment settings were at risk of being completely overwritten whenever the GitHub Actions CD pipeline (`docker-publish.yml`) executed a deploy step (`PUT /api/stacks/${STACK_ID}` using `docker-compose.stg.yml`). Furthermore, omitting `APP_ENV` from the compose service block caused Go backend to fall back to `production` whenever `GIN_MODE=release`, triggering safety crashes (`main.go:268-271`) during mock/dev testing.
- **Root Cause:**
  Discrepancy between live Portainer UI configuration and the git repository. When CD redeploys stacks directly from repository files, any environment variable not explicitly declared in `docker-compose.stg.yml` defaults to empty or gets dropped during the payload PUT.
- **Mandatory Takeaway:**
  1. **Git as the Single Source of Truth for CD**: Never rely solely on ad-hoc UI or host edits on Portainer. Every environment variable used by container services must be codified in `docker-compose.stg.yml` and `.env.stg.example`.
  2. **Safe Fallbacks in Compose**: Always provide defensive compose fallbacks (`APP_ENV=${APP_ENV:-staging}`) to ensure predictable container behavior even if host variables are temporarily omitted.

---

## Lesson 11: Decoupled Scheduled Pipelines vs. On-Demand Proxy Caches (Archiver Integration Gap)

- **Incident & Tracking:** Issue #182, Pull Request (`feat/182-archiver-direct-frame-delivery`).
- **Symptom:**
  Following the production migration to OpenVINO (`ANALYZER=onnx`, 2026-10-06), real frame archiving dropped to ~40 frames/day (54 on 10-06, 26 on 10-07), far below the expected ~57 cameras / 5-minute sampling rate (~16,000 frames/day). This severely choked dataset accumulation needed for model evaluation (ADR-004 D6 gate).
---

## Lesson 12: Machine Learning Model Classes vs. Domain Business Formulas (The Container Class Gap)

- **Incident & Tracking:** Issue #186, ADR-005 Finding D (`feat/186-resolve-container-class-gap`).
- **Symptom:**
  In `services/go-server/internal/cvclient/pcu.go`, the Passenger Car Unit (PCU) Saturation Index formula defined `Container = 3.0`, distinct from `Truck = 2.0`. However, in production, `services/cv-engine/server.py` permanently returned `container: 0`, meaning the `Container = 3.0` weight was dead code that never influenced traffic congestion scores.
- **Root Cause:**
  Discrepancy between early business domain formulas (specified in ADR-004 before real models were deployed) and the actual capabilities of the chosen inference model. The project adopted stock COCO-pretrained YOLOv8n, whose 80 standard classes include only `car`, `motorcycle`, `bus`, and `truck`. In COCO ground truth, heavy commercial freight vehicles (including container trucks) are labeled under `truck`.
- **Mandatory Takeaway:**
  1. **Model-to-Domain Synchronization**: Business logic and scoring formulas must be strictly synchronized with the output schema of the active machine learning model. Never leave speculative weights or dead code branches in production code.
  2. **Documented Architectural Decision Before Implementation**: Rather than making silent ad-hoc code changes, evaluate tradeoffs explicitly (Option a: fold into parent class; Option b: heuristics; Option c: multi-model complexity) and record the decision in `docs/decision-container-class-gap.md`.
  3. **Domain Justification**: Folding container trucks into `Truck = 2.0` aligns with both COCO labeling practices and HCMC daytime peak-hour heavy freight regulations, eliminating dead code while maintaining API backward compatibility.

---

## Lesson 13: Ground-Truth Smoke-Test Baselines vs. Anecdotal Spot-Checking

- **Incident & Tracking:** Issue #187, ADR-005 Finding B (`feat/187-known-state-smoke-set`).
- **Symptom:**
  Historically in ProjectKV, model accuracy verification on staging relied on opportunistic spot-checks of web UI maps or ad-hoc screenshots. Without fixed reference cameras or synchronized observation windows, regressions (such as completely clear expressways flagged as `CONGESTED` due to sensitive PCU floor boundaries, or chronic bottlenecks flagged as `CLEAR`) repeatedly slipped into staging unnoticed until manual user audits.
- **Root Cause:**
  Lack of a standardized, deterministic test harness. Traffic density varies dynamically by time and weather, making unanchored observations subjective and non-reproducible.
- **Mandatory Takeaway:**
  1. **Curate Deterministic Operational Extremes**: Isolate cameras with mathematically extreme, predictable behaviors (e.g., 5 wide 12-lane express sections that are reliably `CLEAR` during midday off-peak with $SI < 0.35$, and 5 chronic convergence bottlenecks that are reliably `CONGESTED` during peak rush hour with $SI \ge 0.90$).
  2. **Codify Ground-Truth Baselines as Versioned Fixtures**: Never keep baseline lists in developer notes or ad-hoc chats. Store them in versioned JSON fixtures (`tools/eval/fixtures/smoke_test_cameras.json`) with documented validation windows and road geometry rationale.
  3. **Automate Discrepancy Gating**: Provide automated verification scripts (`tools/eval/verify_smoke_set.py`) that can test mock verdicts, stored predictions, or live backend APIs before declaring model changes ready.



