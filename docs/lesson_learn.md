# Engineering Lessons Learned & Post-Mortem Register

**Project:** ProjectKV (Ho Chi Minh City Real-Time Urban Traffic & Flood Monitoring)  
**Document Version:** 1.0.5  
**Last Updated:** 2026-10-10  
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

---

## Lesson 14: Active Labeling Effort vs. Passive Environmental Data Accumulation

- **Incident & Tracking:** Issue #194, ADR-005 Finding B/E (`feat/194-label-real-dataset`).
- **Symptom:**
  Engineers and product teams frequently estimate dataset labeling tasks as a simple function of developer hours (e.g., "1 day to label 300 images"). However, for physical environmental events like urban flooding, positive ground-truth frames cannot be created on demand without fabricating data—they are strictly bounded by natural precipitation occurrences over Ho Chi Minh City.
- **Root Cause:**
  Conflating *active labeling effort* (tooling, human annotation, schema validation) with *passive calendar accumulation time* (waiting for rainstorms to submerge roads). In Sprint 3, this failure caused developers to accept 73k placeholder mock frames (107 bytes) just to satisfy a nominal file count metric.
- **Mandatory Takeaway:**
  1. **Strict Dataset Authenticity**: Never fabricate labels or substitute mock frames to pass a gate deadline. Invalidate and discard all synthetic data.
  2. **Decouple Active Tooling from Passive Accumulation**: Complete data ingestion and auditing tools (`collect_real_frames.py`, `dataset_progress.py`, `labeler.html`) during active work, while explicitly tracking the ongoing passive accumulation window.
  3. **Gate Integrity**: Safety-critical gates (ADR-004 §D6: FNR $\le 5\%$) must remain blocked until genuine environmental samples arrive, preventing premature deployment of untested models.
- **Resolution (2026-10-08 Rainstorm Event)**: During the active HCMC evening rainstorm on 2026-10-08, 764 authentic frames were collected and annotated across 206 unique cameras, including 120 flood-positive frames (98 shallow, 22 deep) across chronic inundation bottlenecks (Ung Văn Khiêm, Nguyễn Xí Cầu Đỏ, Hồ Học Lãm, Huỳnh Tấn Phát, Tô Ngọc Vân). This fully satisfied all ADR-004 §D6 dataset quotas without resorting to synthetic or mock frames.

---

## Lesson 15: Empirical Ground-Truth Auditing & Cold-Start Capacity Underestimation

- **Incident & Tracking:** Issue #195, ADR-005 Finding B/C/D6 (`feat/195-run-d6-accuracy-gate`).
- **Symptom:**
  Running the formal ADR-004 §D6 accuracy gate on real camera frames against live OpenVINO container inference revealed a severe performance drop: Traffic Macro F1 dropped to **0.224** (far below 0.70 target), and the known-state smoke-test set suffered an **80% discrepancy rate** (only 2/10 passed). Furthermore, the flood safety gate previously reported false "PASS" status on mock evaluations.
- **Root Cause:**
  1. **Uniform Cold-Start Capacity Underestimation**: A fixed global `capacityFloor = 25.0` PCU severely underestimates wide 6–8 lane arterial sections (Mai Chí Thọ, Võ Văn Kiệt), causing small platoons of 8–10 passing vehicles to trigger false-positive `HEAVY` or `CONGESTED` classifications during off-peak hours.
  2. **Gridlock Occlusion Degradation**: Under dense congestion on chronic bottlenecks (Cộng Hòa), severe vehicle bumper-to-bumper occlusion and rain distortion degrade bounding-box detections in stock 320x320 YOLOv8n, causing the detector to under-count stopped traffic and trigger false-negative `CLEAR` verdicts.
  3. **Zero-Sample Gate Illusion**: Mathematical division of $0/0$ false negatives previously defaulted FNR to $0.0\%$, producing an illusory passing badge when zero flood-positive frames existed in the dataset.
- **Mandatory Takeaway:**
  1. **Strict Gate Integrity for Zero Positives**: When evaluating a safety-critical class with zero ground-truth positive samples, the evaluation verdict must be strictly declared **INCOMPLETE**, never PASS.
  2. **Corridor-Specific Capacity Parameterization**: Road geometry varies drastically between multi-lane freeways ($50+$ PCU capacity) and narrow intersection bottlenecks ($15–20$ PCU capacity). Global constants cannot substitute for parameterized or calibrated capacity baselines (mandatory for Sprint 4 T5).
  3. **Model Decoupling & Hybrid Routing (T9)**: Single-model object detectors cannot detect environmental flood water. Safety-critical systems require decoupled multi-modal or hybrid routing architectures.

---

## Lesson 16: Heterogeneous Distributed Microservices & Multi-Endpoint Portainer Orchestration

- **Incident & Tracking:** Branch `feat/distributed-cv-cicd-pipeline`, Offloading CV Engine to `dev-box` worker VM.
- **Symptom:**
  Running OpenVINO neural network inference directly on staging edge hardware (`homesrv`, Intel Celeron N2930, 4 cores, no AVX2) resulted in 425ms P50 latency per frame and caused severe resource contention, starving CPU and memory for the Go API server and Traefik reverse proxy during peak hotset cycles.
- **Root Cause:**
  Workload asymmetry between lightweight network I/O orchestration (Go backend) and compute-intensive tensor operations (OpenVINO CV inference). Deploying both into a single Docker Compose stack on an underpowered edge host forced CPU saturation.
- **Mandatory Takeaway:**
  1. **Decoupled Workload Topology**: Distribute microservices based on hardware affinity: keep lightweight network I/O on edge nodes (`homesrv`) while offloading batch CV inference to compute-optimized worker nodes (`dev-box`, AMD Ryzen 7 6800HS) interconnected via a private WireGuard/Tailscale mesh.
  2. **Compose Profile Isolation**: Use Docker Compose `profiles: ["model-local"]` in `docker-compose.stg.yml` so that developer local workflows remain unified (`docker compose up`) without accidentally launching heavy inference containers on memory-constrained staging hosts.
  3. **Multi-Endpoint Portainer Orchestration in CI/CD**: In deployment automation (`docker-publish.yml`), route stack updates to their dedicated Portainer endpoints (`endpointId=3` for app services, `endpointId=4` for CV worker) and assert container health directly through the Portainer REST API.
  4. **Strict Cgroups & Disk Guard on Constrained Workers**: On quota-restricted worker hosts (e.g., 20 GB root volume), enforce strict container resource constraints (`cpus: "2.00"`, `memory: "2048M"`) and configure an automated Disk Guard daemon (`docker-disk-guard.sh`) to proactively prune builder cache and dangling images before root volumes exhaust.

---

## Lesson 17: Instantaneous CCTV Camera Viewport Physical Limits vs. Hourly Link Capacity (PCU Calibration)

- **Incident & Tracking:** Issue #196, ADR-005 Finding C / row T5 (`feat/196-calibrate-pcu-weights`).
- **Symptom:**
  During the Sprint 4 T4 benchmark, 94.4% of actual `HEAVY` frames and 94.5% of `CONGESTED` frames were erroneously classified as `CLEAR` by the PCU Saturation Index calculator.
- **Root Cause:**
  1. **Conflating Instantaneous Optical Viewport with Hourly Link Capacity**: The initial `CapacityFloor = 25.0` PCU was an uncalibrated estimate based on hourly road design throughput (1,500–2,000 PCU/hour). However, an intersection CCTV camera captures an instantaneous still snapshot of a 20–30 meter road segment. A typical 3-lane approach physically saturates bumper-to-bumper at 12–15 PCU (3 lanes x 4–5 queue vehicles). Requiring 25 PCU for cold-start capacity forced `CONGESTED` ($SI \ge 0.90$) to demand $>22.5$ PCU—an impossible vehicle volume in a 25-meter frame.
  2. **Western Single-File vs. Vietnamese Mixed Motorcycle Flow**: Western traffic models assume single-file lane alignment ($PCU \approx 0.20$). In HCMC, motorcycles ride 3–4 abreast per lane, filtering through gaps and generating lateral clearance buffers. Under Vietnamese transport standards (**TCVN 4054:2005** Table 4 and **TCVN 13592:2022** §6.2), mixed motorcycle streams require $PCU = 0.30$, and urban buses require $PCU = 2.50$ due to curbside dwelling friction.
- **Mandatory Takeaway:**
  1. **Optical Viewport Physical Geometry**: Always calibrate visual saturation floors based on physical camera field of view geometry ($N_{\text{lanes}} \times \text{FOV} / \text{Headway}$) rather than hourly highway link capacity.
  2. **Regional Standards Grounding**: Align vehicle coefficients with local engineering standards (TCVN 4054:2005 / TCVN 13592:2022) to account for regional modal split and driver behavior.
  3. **Empirical Parameterization**: Calibrated constants (`CapacityFloor = 15.0`, `Motorcycle = 0.30`, `Bus = 2.50`, `Moderate = 0.30`, `Heavy = 0.60`, `Congested = 0.80`) documented in `docs/pcu-calibration-report.md` ensure realistic saturation detection while runtime environment variables (`PCU_WEIGHT_*`, `PCU_CAPACITY_FLOOR`, `PCU_THRESHOLD_*`) provide operator override flexibility.

---

## Lesson 18: High-Frequency Edge Telemetry vs. Cloud VLM Economics (The Inference Platform Decision)

- **Incident & Tracking:** Issue #197, ADR-005 Finding B/D6 / row T9 (`docs/197-decision-openvino-vs-vlm`).
- **Symptom:**
  Following Sprint 4 T4's initial accuracy benchmark (Macro F1 = 0.209, Flood FNR = 100%), the project evaluated whether to keep self-hosted OpenVINO or revert traffic cameras back to Gemini VLM.
- **Root Cause:**
  Conflating the architectural requirements of continuous, high-frequency edge telemetry (polling 57 Hot Set cameras every 30s = 114 RPM, 164,160 RPD) with low-frequency multimodal reasoning. Reverting continuous polling to cloud VLM APIs creates an operational and economic landmine:
  1. **Free-Tier Impossibility**: Gemini's 15 RPM / 500 RPD quota exhausts within 4 hours on just 2 cameras.
  2. **Unsustainable Paid-Tier Economics**: Polling 57 cameras continuously costs ~$111/month; scaling citywide to 712 cameras costs ~$1,385/month.
  3. **Latency & SLA Breach**: Cloud VLM P50 latency (600–1200ms) over WAN causes the 57-camera batch to take 228 seconds, violating the 30-second cycle budget by 7.6x. In contrast, self-hosted OpenVINO on `dev-box` runs at **24.01 FPS** (18.2ms P50 latency, 2.24s total execution for 50 cameras, <100MB RAM, $0 API cost).
- **Mandatory Takeaway:**
  1. **Decoupled Hybrid Architecture (Option C)**: Retain self-hosted OpenVINO for continuous, automated traffic telemetry. Never use external cloud VLMs for high-frequency scheduled loops.
  2. **Targeted Multimodal Deployment**: Position VLMs strictly where their reasoning capabilities are needed and request volumes are bounded: on-demand user modal clicks and low-frequency rain-triggered flood audits.
  3. **Resolution Scaling Over Cloud Reversion**: Rather than offloading to cloud APIs to fix small-object recall, utilize host compute headroom on `dev-box` to scale model input resolution ($320 \times 320 \rightarrow 416 \times 416$), capturing distant motorcycles while maintaining $0 cost.

---

## Lesson 19: The Unit-Test Verification Trap & The Parameter Calibration Paradox

- **Incident & Tracking:** Sprint 4 Technical Audit, ADR-006, Issue #196 (PR #202), Issue #197.
- **Symptom:**
  Following the Sprint 4 T4 benchmark (Macro F1 = 0.209), the team diagnosed poor saturation detection as an uncalibrated cold-start `CapacityFloor = 25.0` and adjusted it to `15.0` in T5 (PR #202). PR #202 was merged with passing unit tests (`pcu_test.go`), and T9 concluded to keep OpenVINO based on this adjustment. However, a post-sprint audit revealed that the full 764-frame real benchmark was never re-run post-T5. A mathematical breakdown of the 2/10 smoke test showed that lowering `CapacityFloor` multiplied all Saturation Indices by $1.67\times$, worsening false-positive congestion on off-peak CLEAR cameras ($0.70 \rightarrow 1.17$, $0.84 \rightarrow 1.40$) while doing nothing for false-negative CONGESTED cameras with 0 vehicle detections.
- **Root Cause:**
  1. **The Verification Trap**: Mistaking unit test execution on synthetic vehicle counts for empirical validation against real-world labeled data.
  2. **The Perception Masking Trap**: Attempting to fix object detector errors (hallucinating vehicles on clear roads or missing occluded vehicles in dense jams) by hacking downstream scoring formulas. Tweaking mathematical thresholds cannot recover missed detections and directly amplifies hallucinations.
- **Mandatory Takeaway:**
  1. **Closed-Loop Empirical Gating**: Any calibration of algorithmic parameters or formulas must be evaluated against the full empirical ground-truth test suite (`tools/eval/benchmark.py`) before claiming victory or making architectural commitments.
  2. **Address Errors at the True Layer**: Fix perception failures at the perception layer (model resolution, training data, confidence filtering, temporal smoothing), rather than contorting scoring formulas.

---

## Lesson 20: VPN SPOF, Graceful Degradation & Decoupling Cloud Daily Budgets from Local Compute

- **Incident & Tracking:** Issue #210, Sprint 5 Task S5-T4 (`infra/s5-t4-tailscale-failover-drill`).
- **Symptom:**
  Offloading CV inference to remote worker host `dev-box` via Tailscale mesh VPN introduced a network single-point-of-failure (VPN SPOF). Furthermore, during live staging audit, `/api/traffic-status` was found degraded to `source: "budget_breaker"`, causing inference requests to never even attempt network transport to `dev-box`.
- **Root Cause:**
  1. **Cloud Daily Budget Leaked into Local Compute**: `AnalysisBudget` was originally introduced in ADR-004 to guard against runaway Gemini/Cloud Vision cloud spend. When `ANALYZER=onnx` was added, the default 5,000 daily limit remained in effect. Polling 57 hotset cameras every 30s (~114 calls/min) exhausted this 5,000 quota within 44 minutes of midnight UTC, silently blocking real worker calls behind `budget_breaker`.
  2. **Unvalidated Circuit Breaker Under Network Chaos**: Graceful degradation logic (`callCtx` 10s timeout $\rightarrow$ `Verdict.SeverityUnknown` $\rightarrow$ `Source: "fallback"` $\rightarrow$ `CircuitBreaker`) had never been chaos-tested under physical network partition on live Staging.
- **Mandatory Takeaway:**
  1. **Decouple Cloud Cost Budgets from Self-Hosted Compute**: Local and edge neural compute incurs $0 API cost and should never be suffocated by cloud quota limiters. In `docker-compose.stg.yml` and staging configurations, set `ANALYZER_DAILY_BUDGET=500000` when `ANALYZER=onnx`.
  2. **Validate Resilience via Physical Chaos Drills (Rule 4)**: The live Staging disconnection drill proved that `warded-backend` maintained HTTP 200 uptime, bounded memory (37.8 MB, 14.8%), zero goroutine leaks, and instantly degraded verdicts to `SeverityUnknown` with neutral gray UI badges.
  3. **Auto-Recovery Over Human Intervention**: Verified that the Circuit Breaker automatically cooled down, probed `dev-box` upon restoration, and transitioned from `OPEN` $\rightarrow$ `HALF-OPEN` $\rightarrow$ `CLOSED`, resuming live ONNX telemetry within 1 cycle (< 30s).
  4. **Document Emergency Failover Routes**: Maintain a disaster recovery runbook ([`docs/runbook_dev_box_failover.md`](runbook_dev_box_failover.md)) documenting how to quickly switch `CV_ENGINE_URL` to standby container `warded-model:8000` (`model-local`) if the remote worker suffers prolonged hardware outage.

---

## Lesson 21: S3-Compatible Cloudflare R2 Integration (Path-Style Routing & Timezone Partitioning)

- **Incident & Tracking:** Sprint 5 Plan / S5-T4, DevOps Guide integration (`feat/r2-storage-integration`).
- **Symptom:**
  When configuring third-party S3-compatible cloud object storage providers like Cloudflare R2, default AWS SDK settings attempt virtual-host addressing (`<bucket>.<endpoint>`), causing DNS resolution failures (`no such host`) and 404/403 routing errors. Furthermore, object keys partitioned with naive `.UTC()` dates generate day shifts across midnight in Vietnam (e.g., snapshots taken at 02:00 AM ICT on Oct 10th get filed under Oct 9th UTC).
- **Root Cause:**
  1. **Virtual-Host vs. Path-Style Addressing**: AWS S3 defaults to bucket-prefixed subdomains (`https://warded-proofs.bb67574163a065e4aad475fb3aa6bb92.r2.cloudflarestorage.com`), which Cloudflare R2 does not route without dedicated custom DNS setups. Cloudflare R2 requires S3 Path-Style addressing (`o.UsePathStyle = true` in Go AWS SDK v2, `addressing_style: path` in boto3).
  2. **Timezone Partitioning Mismatch**: Vietnam commuters and DOT incident logs operate in Indochina Time (`Asia/Ho_Chi_Minh`, UTC+7). Filing proof snapshots under UTC dates diverges snapshot directories from municipal traffic logs and ground-truth storm timelines.
- **Mandatory Takeaway:**
  1. **Mandatory Path-Style Flag for R2**: Always explicitly set `UsePathStyle = true` in AWS SDK Go v2 (`s3.NewFromConfig(cfg, func(o *s3.Options) { o.UsePathStyle = true })`) and `s3={'addressing_style': 'path'}` in Python boto3.
  2. **Timezone Normalization in Key Standards**: Standardize `StoragePath` date formatting to Vietnam ICT (`time.FixedZone("ICT", 7*3600)`) so snapshot folder partitions strictly match local calendar dates.
  3. **Dual Storage Driver Abstraction**: Maintain unified `storage.Storage` interface with pluggable drivers (`STORAGE_DRIVER=local|r2`), guaranteeing that local development and offline mock pipelines run without cloud credentials while staging and production stream snapshots directly to Cloudflare R2.

---

## Lesson 22: Plaintext Credential Exposure Prevention & Benchmark Methodology Rigor

- **Incident & Tracking:** Sprint 5 Plan / S5-T1 Audit & S5-T4 R2 Storage, Issues #207, #214, PR #212 (`feat/r2-storage-integration`).
- **Symptom:**
  1. **Credential Exposure in Tooling**: A verification script (`tools/storage/verify_r2.py`) included Cloudflare R2 API keys as default fallback strings in `os.getenv("R2_ACCESS_KEY_ID", "...")`. Even though documentation files in `docs/` remained free of credentials, committing fallback strings into git history compromised the token pair, necessitating immediate key revocation on Cloudflare dashboard.
  2. **Smoke-Test Evaluation Flaw**: Re-running the smoke test post-T5 showed a nominal pass rate increase (20% -> 50%), but camera `66b1c426...` flipped from `SI=1.00 PASS` to `SI=0.00 FAIL`. Because scaling a non-zero Saturation Index by $1.67\times$ cannot mathematically produce zero, the discrepancy revealed that the smoke test pulled live snapshots rather than frozen reference images, conflating traffic fluctuations with parameter calibration.
- **Root Cause:**
  1. Using convenience defaults for API credentials in diagnostic scripts instead of failing fast when environment variables are omitted.
  2. Conducting before/after benchmark comparisons on live, dynamic physical streams rather than anchored, versioned ground-truth image sets.
- **Mandatory Takeaway:**
  1. **Zero Secret Fallbacks in Code**: Never provide actual credentials as fallback arguments (`os.getenv("SECRET", "hardcoded_val")`). Verification scripts must strictly enforce `if not key: sys.exit(1)` and require `.env` or environment injection.
  2. **Mandatory Key Rotation on Exposure**: Any key committed to git or shared across plain text channels must be immediately revoked on the provider dashboard and rotated.
  3. **Frozen Image Anchors for Methodology Rigor**: When benchmarking model inference or scoring calibration, before/after evaluations are only valid when executed against identical, frozen image frames. Re-fetching live snapshots invalidates causal attribution. The 764-frame controlled benchmark is the authoritative baseline.

---

## Lesson 23: YOLO Anchor Activation Summation vs Object Counts (Missing NMS), and Ground-Truth Sanity Checks

- **Incident & Tracking:** Issue #208, Sprint 5 Task S5-T2 (`tooling/s5-t2-cv-detection-audit`).
- **Symptom:**
  In Sprint 5 evaluation, the Go PCU Saturation Index ($SI$) exhibited two extreme failure modes that could not be reconciled by formula adjustments:
  1. Wide-open arterial roads with only 3 to 5 physical cars (`Mai Chí Thọ - Nguyễn Cơ Thạch 1`, `Võ Văn Kiệt - Ký Con`, `Điện Biên Phủ - Pasteur`) were reported as $SI \ge 0.51 - 1.00$ (HEAVY / CONGESTED).
  2. Severe commuter bottlenecks with dense queues (`Cộng Hòa - Trường Chinh`) were reported as $SI = 0.00$ with 0 detected vehicles.
- **Root Cause:**
  1. **Direct Anchor Summation without Non-Maximum Suppression (NMS)**:
     In `services/cv-engine/server.py`, vehicle counts were calculated as `int(np.sum(data[class_idx, :] > 0.45))` directly over the raw YOLOv8 `(1, 84, 2100)` output tensor. In anchor-free multi-scale object detection, a single physical vehicle activates 5 to 12 overlapping feature cells across strides P3, P4, and P5. Without NMS deduplication, each physical car was multiplied by 2.7x to 4.7x, and large vehicles triggered both `truck` and `bus` classes, converting 3 to 5 cars into 28 to 39 detected objects.
  2. **Aspect Ratio Distortion Suppressing Detection Confidence**:
     Snapshots are $512 \times 288$ (16:9). Resizing directly to $320 \times 320$ stretched height by $1.78\times$ relative to width, de-tuning convolutional receptive fields and depressing peak detection confidences on real queues from $0.55+$ down to $0.37$, pushing real vehicles below the rigid $0.45$ threshold.
  3. **Synthetic Ground Truth Assumption Errors**:
     Camera `Mai Chí Thọ - Đồng Văn Cống 2` was assumed to be chronic CONGESTED during rush hours in smoke fixtures. Visual audit proved the physical road was an active construction zone blocked by barriers and completely empty. The model's prediction of 0 vehicles was physically accurate; the benchmark error was an ungrounded label.
- **Mandatory Takeaway:**
  1. **NMS is Non-Negotiable in Object Detection**: Raw neural network output tensors must NEVER be summed to count physical entities. Object detectors always require Non-Maximum Suppression (IoU $\le 0.45$) to collapse multi-anchor activations into discrete physical instances.
  2. **Preserve Input Aspect Ratios with Letterboxing**: Never stretch non-square images directly to square model dimensions. Always apply aspect-preserving letterboxing with neutral padding to maintain convolutional geometric priors.
  3. **Visual Inspection Before Formula Tuning (Rule 4)**: Never tune downstream math (e.g. lowering `CapacityFloor` or adjusting threshold curves) to compensate for upstream perception anomalies without visually examining raw image frames and ground-truth annotations.

---

## Lesson 24: Decoupled Multimodal Hybrid Architecture & Quota-Guarded Dual Triggers

- **Incident & Tracking:** Issue #209, Sprint 5 Task S5-T3, Branch `feat/s5-t3-hybrid-flood-pipeline`.
- **Symptom:**
  Attempting to run cloud VLM vision analysis continuously across 57+ cameras quickly exhausted the free-tier quota (15 RPM / 1,500 RPD), triggering upstream HTTP 429 throttling and incurring unacceptable latency and fragility. Conversely, relying strictly on standard YOLOv8n object detection (COCO weights) yielded 0% flood recall, violating the ADR-004 §D6 safety gate ($\text{FNR} \le 5.0\%$). Furthermore, domain definitions diverged: frontend spatial rain overlay used 1.0 mm (wet pavement onset), 4.0 mm (shallow flood), 15.0 mm (deep flood), whereas backend ADR documents referenced a 2.0 mm/h heuristic and conflated the 10 traffic smoke cameras with municipal flood chokepoints.
- **Root Cause:**
  1. Conflating continuous high-frequency traffic detection (which requires local high-FPS processing like OpenVINO on `dev-box` at 24.01 FPS and $0 API cost) with event-driven flood perception (which requires multimodal semantic scene understanding).
  2. Uncoordinated threshold heuristics between frontend spatial hex aggregation and backend architectural documentation.
  3. Lack of strict sliding-window rate limiting (10 RPM) and daily quota budgeting (300 RPD) in the application layer before calling external cloud VLMs.
- **Mandatory Takeaway:**
  1. **Decoupled Architecture Pattern (Option C)**: Retain high-throughput, $0-API-cost local inference (OpenVINO) for continuous 30-second vehicle scoring. Decouple cloud VLM into dual triggers:
     - *Trigger 1 (Environmental)*: Background polling of authentic municipal flood chokepoints (`tools/eval/fixtures/flood_chokepoints.json`) ONLY during active rain events ($\text{rain\_mm} \ge 1.0\text{ mm}$ or active rain WMO codes), consuming 0 calls during dry weather.
     - *Trigger 2 (User On-Demand)*: Commuter-initiated inspection via modal click in the web app (`POST /api/flood/verify`).
  2. **Domain Model Reconciliation**: Establish a unified source of truth across frontend, backend, and documentation for environmental events (1.0 mm wet onset, 4.0 mm shallow accumulation, 15.0 mm deep flood).
  3. **Strict In-Process Quota Enforcers**: Cloud API calls must always be guarded by application-level rate limiters (thread-safe sliding window and daily quota rollover) to prevent accidental quota exhaustion, cascade failures, or billing spikes.

---

## Lesson 25: CI Linter Schema Validation Flakiness & Toolchain Version Alignment

- **Incident & Tracking:** PR #216 (`feat/s5-t3-hybrid-flood-pipeline`), CI Run `38116173937`.
- **Symptom:**
  GitHub Actions workflow `CI - Code Verification` failed on step `Lint Go Backend` with an external network timeout error:
  ```text
  [.golangci.yml] validate: compile schema: failing loading "https://golangci-lint.run/jsonschema/golangci.v1.64.jsonschema.json": Get "https://golangci-lint.run/jsonschema/golangci.v1.64.jsonschema.json": context deadline exceeded (Client.Timeout exceeded while awaiting headers)
  ```
  Concurrently, the workflow emitted runner warnings: `! Restore cache failed: Dependencies file is not found in /home/runner/work/projectKV/projectKV. Supported file pattern: go.sum`.
- **Root Cause:**
  1. **External Schema Network Dependency**: In `golangci/golangci-lint-action@v6`, the action defaults to `verify: true`. If `.golangci.yml` exists, it invokes `golangci-lint config verify`, issuing an outbound HTTP GET to `https://golangci-lint.run`. Network latency or CDN rate limits on the external host abort the client deadline and fail the entire build, even when code is 100% clean and tests pass.
  2. **Monorepo Cache Path Blindness**: In `actions/setup-go@v5`, dependency caching searches for `go.sum` at the repository root by default. For monorepos with sub-packages (`services/go-server/go.sum`), caching fails unless `cache-dependency-path` is explicitly configured.
  3. **Toolchain Version Alignment**: When using precompiled binaries (`install-mode: "binary"`), binaries released on GitHub are compiled with older Go versions (e.g. Go 1.24). If the project specifies `go 1.26.0` in `go.mod`, `golangci-lint` fails with code 3 (`targeted Go version higher than build version`). Compiling via `install-mode: "goinstall"` ensures the linter binary matches the exact Go toolchain version installed by `actions/setup-go`.
- **Mandatory Takeaway:**
  1. **Disable Remote Schema Verification in CI**: Always configure `verify: false` in `golangci-lint-action` to eliminate brittle external HTTP dependencies during automated lint gates.
  2. **Codify Cache Dependency Paths in Monorepos**: Explicitly set `cache-dependency-path: 'services/go-server/go.sum'` in `actions/setup-go@v5` to enable dependency caching and prevent rebuild overhead.
  3. **Match Linter Build Toolchain with Target Go Version**: When projects adopt cutting-edge or custom Go toolchains, use `install-mode: "goinstall"` to compile `golangci-lint` against the runner's active Go runtime.

---

## Lesson 26: GitHub Issue Auto-Closing Keyword Semantics vs. Descriptive Prose

- **Incident & Tracking:** Issue #209, Pull Request #216 (`feat(flood): decoupled hybrid multimodal flood pipeline`).
- **Symptom:**
  PR #216 completed and verified all deliverables for Issue #209 and was approved and merged into `dev`. However, Issue #209 remained `OPEN` in the GitHub repository issue tracker, requiring manual closure.
- **Root Cause:**
  The PR description used informal explanatory language:
  ```markdown
  ## Summary & Context
  Addresses **Issue #209: [go-server + web] decoupled hybrid flood pipeline (Sprint 5 Task S5-T3)**.
  ```
  GitHub's issue linking and auto-closing automation exclusively recognizes strict action keywords (`close`, `closes`, `closed`, `fix`, `fixes`, `fixed`, `resolve`, `resolves`, `resolved` immediately followed by `#<issue-number>`). Descriptive or decorative phrasing such as `Addresses Issue #209`, `Addresses #209`, or `Implement #209` is parsed as arbitrary markdown prose and fails to create the native GitHub event hook link.
- **Mandatory Takeaway:**
  1. **Strict Format at PR Top**: Every PR resolving an issue MUST begin directly with `Close #<issue-number>` (e.g. `Close #209`). Never use `Addresses Issue #...` or `Implement #...`.
  2. **Codified PR Template**: Maintain a permanent `.github/pull_request_template.md` that embeds this format by default.
  3. **Non-Negotiable Rule**: Codified as Rule 9 in `AGENTS.md` and `docs/project_status.md` Section 5.

---

## Lesson 27: Centralized Collaborative Team Labeling Over Local Synchronization & Cloud Storage Justification

- **Incident & Tracking:** Issue #214, Pull Request for S5-T4 (`[infra + web] S5-T4 — R2 blob storage for archiver + shared team labeler`).
- **Symptom:**
  Collaborative ground-truth evaluation and training set annotation was friction-prone: team members had to manually synchronize JPEG proof frames from server disk to their local workstations via ad-hoc scripts or SFTP. Furthermore, accessing Cloudflare R2 public edge URLs (`https://file.529studio.site`) required navigating Cloudflare Access Zero Trust authentication walls, preventing smooth browser-based frame rendering for evaluation workflows.
- **Root Cause:**
  1. **Storage Abstraction Gap**: The `storage.Storage` interface only exposed `Put`, `Get`, `Delete`, and `Exists`, lacking a `List(ctx, prefix)` method. The server could write snapshots but could not enumerate partition dates or snapshot catalogs dynamically.
  2. **Zero Trust Access Friction**: Public CDN endpoints behind corporate Zero Trust redirects cannot be consumed as direct `<img>` tags by collaborative web tools without complex cookie sessions.
- **Mandatory Takeaway & Design Principles:**
  1. **Dual-Driver Listing Abstraction**: Extended `storage.Storage` with `List(ctx, prefix) ([]Item, error)` implemented for both `LocalStorage` (directory walk with traversal protection) and `R2Storage` (`s3.ListObjectsV2` pagination), preserving complete offline local dev/test autonomy.
  2. **Secure Proxy Streaming Route**: Implemented `GET /api/archiver/snapshots/raw?key=...` with strict path traversal validation (`ErrInvalidPath`), streaming JPEG binaries directly to authorized web clients with immutable caching headers.
  3. **Monochromatic Collaborative Web Labeler**: Built `apps/web/src/app/labeler` with standard rapid-fire keyboard shortcuts (`1-3` flood, `Q-W-E-R` traffic, `A-S-D` weather, `Z-X-C` time of day, `Enter` save/next), zero emojis, and bilingual support (`vi` default), saving ground truth directly to cloud storage.
  4. **Architecture & Scope Justification Rule**: Adoption of Cloudflare R2 blob storage is explicitly justified to enable centralized collaborative team labeling and eliminate manual synchronization friction, **NOT** because the system has breached the ADR-004 §D7 disk capacity threshold (100 MB / 5,000 files).

---

## Lesson 28: Zero Unauthenticated Dev Endpoints & Fail-Closed Developer Gatekeeping

- **Incident & Tracking:** Issue #214, Pull Request #218 (`feat/s5-t4-r2-shared-team-labeler`).
- **Symptom:**
  The shared team web labeler (`/labeler`) and snapshot archiver backend endpoints (`/api/archiver/*`) deployed in S5-T4 were completely exposed and publicly accessible without authentication. Any public visitor or external entity could view raw camera snapshot archives, browse captured frames, and submit forged ground-truth labels via `POST /api/archiver/labels`, creating a severe data poisoning risk for AI model evaluation and safety gates.
- **Root Cause:**
  Internal developer tooling was merged into the production web router under the assumption of development obscurity, without an explicit authorization boundary or production fail-closed security policy.
- **Mandatory Takeaway & Security Principles:**
  1. **Fail-Closed Backend Gatekeeper**: Endpoints managing training labels, raw archives, or internal developer utilities MUST enforce developer authorization via `middleware.RequireDevAuth`. In `production` and `staging` environments, if `DEV_ACCESS_TOKEN` is unset or empty, the backend must fail closed with `503 Service Unavailable`, preventing unauthenticated access under misconfiguration.
  2. **Multi-Channel Authentication**: To support both headless REST calls and browser image binary rendering, authentication must support:
     - `X-Dev-Token` header (standard custom header)
     - `Authorization: Bearer <token>` header (RFC 6750 standard)
     - `?token=` and `?dev_token=` query parameters (required for browser `<img>` elements which cannot set custom request headers)
  3. **Timing-Attack Resistance**: Token validation must use `crypto/subtle.ConstantTimeCompare` to eliminate side-channel timing attacks.
  4. **Monochromatic Client-Side Gatekeeper**: Web labeler UI (`/labeler`) displays an in-place high-contrast monochromatic lock screen until unlocked with a valid token, persisting credentials strictly in `sessionStorage` (cleared on tab close or explicit lock session), and automatically evicts cached tokens upon receiving `401 Unauthorized`.

---

## Lesson 29: Perception-Layer Fixes vs. Scoring Hacks & Empirical Validation of NMS and Letterboxing

- **Incident & Tracking:** Issue #221, Pull Request for S6-T1 (`tooling/s6-t1-cv-nms-letterbox-benchmark`).
- **Symptom:**
  In Sprint 4 and Sprint 5, traffic classification suffered from severe false alarm rates: 46 clear frames and 45 moderate frames were falsely classified as `CONGESTED`, while Macro F1 stagnated at 0.206. In Sprint 4 T5, the team attempted to fix this downstream by reducing `CapacityFloor` to 15.0, but that doubled false-positive CONGESTED classifications without fixing missed queues (Lesson 19).
- **Root Cause:**
  1. **Anchor Activation Explosion**: In `services/cv-engine/server.py`, vehicle counts were calculated using raw thresholded anchor summations (`np.sum(data[class_idx, :] > 0.45)`). A single real vehicle activated 5–12 overlapping anchor cells across feature levels, inflating 3 cars into 39 detections.
  2. **Non-Uniform Geometric Distortion**: Direct resizing from 16:9 (`512x288`) to square `320x320` distorted vehicles vertically by $1.78\times$, dropping confidence scores of real vehicle queues from $>0.55$ down below $0.37$.
  3. **Small-Object Blindness**: Coarse 320x320 resolution collapsed distant motorcycles (>25m) below the receptive field.
- **Mandatory Takeaway & Engineering Resolution:**
  1. **Fix Perception at the Perception Layer**: Implemented standard Non-Maximum Suppression (`nms_cpu` with $\text{IoU} \le 0.45, \text{conf} \ge 0.25$) and aspect-preserving letterboxing with neutral gray `(114, 114, 114)` padding, scaled to 416x416 resolution on `dev-box` (`[1, 3, 416, 416]` input tensor, `[1, 84, 3549]` output tensor).
  2. **Empirical Ground-Truth Verification (Rule 4 & Lesson 19)**: Re-ran the benchmark across the complete frozen 764-frame dataset (`tools/eval/dataset/labels_d6_real.json`):
     - **Traffic Macro F1** improved from **0.206 to 0.245** (+18.9% relative increase).
     - **Traffic Weighted F1** improved from **0.351 to 0.402** (+14.5% relative increase).
     - **CONGESTED False Alarms** on CLEAR frames dropped by **50.0%** (from 46 frames down to 23).
     - **MODERATE Detections** tripled from 21 to 63 correct classifications (+200%).
     - Proved that resolving perception layer artifacts directly provides a sound foundation for downstream corridor capacity calibration in S6-T2.

---

## Lesson 30: Active Environmental Opportunism & Continuous Cloud Label Synchronization

- **Incident & Tracking:** Branch `tooling/collect-and-label-rain-frames-2026-10-13`.
- **Symptom:**
  Gathering high-value real-world environmental edge cases (standing water reflections, tire spray, raincoat detection, ponding along curbs) cannot be artificially synthesized or simulated in laboratory settings. When active weather events occur (such as the afternoon rainfall in HCMC on 2026-10-13), passive waiting misses rare environmental windows, while manual SFTP-based file pulling creates operational friction for distributed team labeling.
- **Root Cause:**
  Ground-truth evaluation datasets traditionally relied on monolithic historical collection windows (e.g. the 2026-10-08 rainstorm). Without active ingestion tooling hooked to live meteorological conditions, datasets fail to accumulate fresh environmental variety.
- **Mandatory Takeaway & Engineering Resolution:**
  1. **Meteorological Event Ingestion Pipeline**: Leveraged real-time weather triggers (`rain >= 0.10 mm`, `weather_code 51`) to automatically execute targeted snapshot harvesting across 10 authentic municipal flood chokepoints and flood-prone corridors (201 frames captured in 3 minutes, 97.1% success rate).
  2. **Multi-Modal Ground-Truth Verification**: Automated high-throughput vehicle detection (OpenVINO YOLOv8n FP16 416x416 at 31.2 frames/sec) combined with visual road surface auditing (puddles, ponding, tire spray, raincoat detection).
  3. **Dual-Format Cloud Synchronization**: Concurrently emitted labels in both Team Web Labeler format (`labels/<YYYY-MM-DD>/<camera_id>_<annotator>_<timestamp>.json`) and Benchmark Evaluation format (`labels_today_benchmark/`), syncing 201 JPEGs and 201 JSONs directly to Cloudflare R2 blob storage via `rclone` with Zero Trust edge protection.








