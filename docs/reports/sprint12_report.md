# Sprint 12 Completion, Reality-Check Audit & Technical Verification Report

**Project:** ProjectKV (Ho Chi Minh City Real-Time Urban Traffic & Flood Monitoring)  
**System Name:** Warded (`warded-backend`, `warded-frontend`, `warded-cv-engine`)  
**Document Version:** 1.0.0 (Reconciled with Git ledger, live test suites and manual evidence)  
**Date:** 2026-11-12  
**Target Audience:** Technical Leads, System Architects, Product Owners, AI Evaluation Agents, Developers  
**Commit range:** `5b7614c..28bef6e` (16 merges, 203 files changed, +34,446 / -2,588)  

**Primary References & Ground Truth (Sole Source of Truth):**
- [`docs/HANDOFF-s12.md`](../HANDOFF-s12.md) (sprint charter, settled decisions, open backlog)
- [`docs/sprints/s12_t1_issue.md`](../sprints/s12_t1_issue.md), [`s12_t2_spec.md`](../sprints/s12_t2_spec.md), [`s12_t3_spec.md`](../sprints/s12_t3_spec.md), [`s12_t4_spec.md`](../sprints/s12_t4_spec.md), [`s12_t5_spec.md`](../sprints/s12_t5_spec.md)
- [`docs/reports/s12_t1_forecast_sidecar_evidence.md`](s12_t1_forecast_sidecar_evidence.md) and `.json`
- [`docs/reports/s12_t2_manual_evidence.md`](s12_t2_manual_evidence.md)
- [`docs/reports/s12_t3_manual_evidence.md`](s12_t3_manual_evidence.md)
- [`docs/reports/s12_t4_manual_evidence.md`](s12_t4_manual_evidence.md)
- [`docs/lessons/INDEX.md`](../lessons/INDEX.md) (lessons L83-L91, plus issue-keyed 402, 411, 420, 425)
- GitHub PR ledger: #415, #416, #417, #418, #419, #426, #427, #428, #429, #430, #431, #433, #434, #435, #436

---

## Navigation Aid (Table of Contents)

1. [Executive Summary & Quantitative Scorecard](#1-executive-summary--quantitative-scorecard)
2. [Wave-by-Wave Audit of Delivered Engineering](#2-wave-by-wave-audit-of-delivered-engineering)
3. [Reality-Check Audit Findings (The Uncomfortable Truths F1-F5)](#3-reality-check-audit-findings-the-uncomfortable-truths-f1-f5)
4. [End-User Utility Audit (Saigon Commuter Reality)](#4-end-user-utility-audit-saigon-commuter-reality)
5. [Quantitative Verification Evidence & Test Ledger](#5-quantitative-verification-evidence--test-ledger)
6. [Engineering Lessons Learned (L83-L91, 402, 411, 420, 425)](#6-engineering-lessons-learned-l83-l91-402-411-420-425)
7. [Carry-Over Ledger & Strategic Bridge to Sprint 13](#7-carry-over-ledger--strategic-bridge-to-sprint-13)

---

## 1. Executive Summary & Quantitative Scorecard

Sprint 12 transformed Warded into an autonomous, self-scheduling, physically grounded system:
1. **Unattended Operations**: Forecast refit pipeline runs as daemon, Compose sidecar, systemd timer, and K8s CronJob with flock and zero-restart atomic reload (S12-T1, #413, PR #416).
2. **Physically Grounded Commuter Surface**: Last-mile GPS geolocation ranks cameras in 250 m bands with 48 px touch ergonomics (S12-T2, #400, PR #419); motorbike flood hazard levels classify physical cm tire submersion (S12-T3, #401, PR #430).
3. **Citizen Alerting Channel**: Telegram bot live-provisioning behind fail-closed webhook and bilingual subscription modal (S12-T4, #402, PR #431).
4. **Authentic Measurement Tooling**: Dataset v3 materializer, measured retraining gate, and host-guarded AVX2 latency benchmark harness (S12-T5, #411, PR #436).

### Delivered sprint tasks

| Task | Issue / PR | Delivered capability | Lesson | Verdict |
|:---|:---|:---|:---|:---|
| S12-T1 | #413 / #416 | Daemon, sidecar, systemd, CronJob, flock, atomic reload | L84 | **PASS** (9/9 scenarios) |
| S12-T2 | #400 / #419 | `useGeolocation`, `NearMeModal`, 250 m bands, 48 px targets | L85 | **PASS** (14/14 scenarios) |
| S12-T3 | #401 / #430 | cm hazard classifier, avoidance inference, honest UI | L90, L89 | **PARTIAL** (contract/UI pass; live blocked) |
| S12-T4 | #402 / #431 | Fail-closed webhook, per-chat limiter, deep links, modal | 402 | **PARTIAL** (local pass; live token DM pending) |
| S12-T5 | #411 / #436 | Dataset v3 materializer, measured gate, AVX2 harness | 411 | **PARTIAL** (tooling done; off-box GPU/AVX2 run pending) |

### Mid-sprint fixes (8 PRs)

| PR | Issue | What it corrected | Lesson |
|:---|:---|:---|:---|
| #415 | #414 | Consolidated public CI into one required gate; verified native secret scanning & push protection | L83 |
| #426 | #422 | Accepted 9-digit RFC3339Nano timestamps via microsecond pre-normalization across Python tools | L86 |
| #427 | #421 | Replaced header-trust auth with RS256/JWKS verification and CORS origin whitelisting | L87 |
| #428 | #424 | Bounded proxy reads at 5 MiB, query-escaped camera IDs, mounted gzip outside sanitizer | L88 |
| #429 | #423 | Consolidated dual SSE into one provider, Leaflet canvas rendering, visibility pause | L89 |
| #433 | #432 | Bound staging Traefik router to tunnel HTTP entrypoint, removed phantom cert resolver | L91 |
| #434 | #420 | Bypassed sanitizer buffering for `text/event-stream`; explicit `Flush` & `WriteString` | 420 |
| #435 | #425 | Decomposed 1,778-line `main.go` into adapters, jobs, routes (< 250 lines) | 425 |

### Sprint scorecard

| Dimension | Sprint 11 baseline | Sprint 12 achieved | Verdict |
|:---|:---|:---|:---|
| Forecast refit execution | Manual CLI only | 4 orchestration targets, one code path | **PASS** |
| Refit concurrency safety | Unenforced | `flock` non-blocking, exit 4, lock never unlinked | **PASS** |
| Model reload | Restart required | zero-restart, 4 concurrent readers | **PASS** |
| Commuter proximity | Manual map pinch | 1.5 km radius, 250 m bands, 48 px touch floor | **PASS** |
| Hazard semantics | Abstract `water_ratio` | cm depth, 20.0 cm inclusive, `null` when unmeasured | **PASS** |
| Hazard live observability | N/A | 0 of 8 live frames carried level (untrained head) | **BLOCKED** |
| Telegram channel | Simulation mode | Fail-closed webhook + citizen modal | **PARTIAL** |
| Segmented model tooling | Untrained head | Provenance-bound materializer & measured gate | **PASS** |
| AVX2 latency evidence | 13.7 ms p95 on M4 | Host-guarded harness; non-AVX2 refused | **PASS** (tool) |
| Backend composition root | 1,778 lines | 229 lines (`adapters.go`, `jobs.go`, `routes.go`) | **PASS** |
| Public API auth | Plain header presence | RS256 + audience + CORS whitelist | **PASS** |

---

## 2. Wave-by-Wave Audit of Delivered Engineering

### Wave 1: Unattended forecast refit (S12-T1)
- **Single-flight lock**: `ForecastRefitLock` acquires `fcntl.flock(LOCK_EX | LOCK_NB)` on permanent lock file without unlinking upon release.
- **Durable scheduler state**: `var/run/forecast_refit_state.json` tracks cycle counts, consecutive failures, and `last_slot_fired`. Atomic temp file swap via `os.replace`.
- **Wall-clock slots**: Slot evaluations at `02:00, 08:00, 14:00, 20:00` in `Asia/Ho_Chi_Minh` prevent double-firing across daemon restarts.
- **Row-delta gating**: Read-only connection (`mode=ro`, `PRAGMA query_only = ON`) skips refit if new history rows < `--min-new-rows`.
- **Zero-restart reload**: Lock-free atomic `snapshot()` swap in `internal/forecast`; pre/post stat mtime validation protects against partial weights.
- **Deployment parity**: Sidecar (`profiles: forecast-refit`), systemd oneshot (`SuccessExitStatus=4`), and K8s CronJob with restricted PSS.

### Wave 2: Commuter surface & Telegram alerts (S12-T2, S12-T4)
- **Geolocation**: SSR-safe `useGeolocation` wrapper with unmount teardown; coordinates never persisted, logged, or exfiltrated.
- **Banded ranking**: `geoDistance.ts` quantizes distances into 250 m bands, prioritizing severe hazards over trivial proximity without clock reads.
- **Kiosk ergonomics**: Minimum 48 px touch targets (64 px list rows), 390 px mobile modal, and reduced-motion instant camera snapping.
- **Telegram alerting**: Route answers 404 when unprovisioned. Token bucket limits chats to 6 cmds/min with 1 slow-down notice, update ID deduplication, and 256-slot queue.

### Wave 3: Physical hazard semantics (S12-T3)
- **Ground-truth ruler**: Motorbike bounding box height calibrates 110 cm scale factor; curb source (15 cm) defined for future expansion.
- **cm depth bands**: Safe (< 10 cm), Warning (10-20 cm), Danger (>= 20 cm exhaust waterline, inclusive).
- **Avoidance inference**: Lane-blocking pool with zero through-traffic escalates to Danger (`AVOIDANCE_INFERRED`, depth `null`).
- **Asymmetric hysteresis**: Immediate escalation; de-escalation requires 2 consecutive agreeing frames.

### Wave 4: Segmentation dataset v3 & AVX2 harness (S12-T5)
- **Dataset materializer**: `materialize_seg_dataset.py` builds dataset from live proof archive, verifying SHA-256 and rejecting orphans/CMYK.
- **Frozen group split**: Split binds `(camera_id, capture_date)` and manifest digest; different seed exits 2.
- **Measured retraining gate**: `train_yolov8_seg.py` enforces `--metrics` for `--verify-gate`, requiring >= 50 epochs and <= 0.02 class drop.
- **AVX2 host guard**: `bench_seg_latency.py` exits 2 on non-AVX2 hosts (and Darwin), producing zero evidence files.

### Wave 5: Operational hardening (Mid-sprint PRs)
- **Security & CI**: Unified CI evidence gate (#415); RS256 JWKS JWT authentication and CORS origin whitelisting (#427).
- **Performance**: Consolidated single SSE provider with Leaflet canvas (#429); ETag-cached 85.4% catalog size reduction (#428).
- **Reliability**: Streaming sanitizer Flusher bypass (#434); Traefik staging ingress alignment (#433); modular main.go decomposition (#435).

---

## 3. Reality-Check Audit Findings (The Uncomfortable Truths F1-F5)

### F1: Legacy segmentation labels were orphans, and 47% were synthetic
Joining 301 October labels (`data/yolo_seg`) against November proofs yielded 0/301 exact timestamp matches (retention dropped October frames). `curate_v2_dataset.py` had filled 261 skipped frames with fixed template polygons (47% synthetic). All prior accuracy metrics measured template artifacts, not roadways. Resolved by Dataset v3 materializer enforcing authentic pixel-label pairing.

### F2: Read-only container rootfs is a hard architectural constraint
Under restricted PSS, containers run with `readOnlyRootFilesystem: true`. Writing state or locks to default paths crashes after acquiring locks. Enforced writable `/tmp` mounts, non-root user `10001:10001`, and `mode=ro` SQLite connections in deploy manifests.

### F3: Secret sanitizer buffered and deadlocked SSE streams
`SecretSanitizer` middleware buffered response bodies, causing persistent SSE connections to never complete `c.Next()`, leaking unread frames in memory. Resolved by bypassing buffering for `text/event-stream` and explicitly delegating `http.Flusher`.

### F4: Composition root accreted into a 1,778-line monolith
`cmd/server/main.go` accumulated adapters, lifecycle tickers, and dead routes. Factored into `adapters.go`, `jobs.go`, and `routes.go` (< 250 lines in `main.go`), uncovering package compilation requirements and static asset traversal risks.

### F5: Latency benchmark on Apple M4 is invalid for Linux AVX2 target
Reported 13.7 ms p95 on M4 is irrelevant for AMD Ryzen AVX2 target. Implemented host guard in `bench_seg_latency.py` exiting 2 on non-AVX2 hosts. AC18/AC19 and GPU retraining (AC10-AC14) remain debt carried to Sprint 13.

---

## 4. End-User Utility Audit (Saigon Commuter Reality)

- **Commuter Geolocation**: 1.5 km near-me view with 250 m banded hazard sorting gives riders instant clarity on road impassability before departing. 14/14 manual test scenarios passed.
- **Physical Depth Hazard**: Replaced meaningless area ratios with actionable cm depth (exhaust threshold 20 cm). Live frames correctly returned `null` rather than simulated depths while model head is untrained.
- **Citizen Alerts**: Commuters can subscribe to specific wards via Telegram deep links; bot rate limiting ensures resilience against burst traffic.

---

## 5. Quantitative Verification Evidence & Test Ledger

Verified on merge head `28bef6e` on 2026-11-12:

| Gate | Command | Result | Verdict |
|:---|:---|:---|:---|
| Forecast suite | `python3 -m unittest discover -s tools/forecast -p "*_test.py"` | 145 tests, OK | **PASS** |
| CV engine suite | `python3 -m unittest discover -s services/cv-engine -p "test_*.py"` | 119 tests, OK | **PASS** |
| Go backend | `cd services/go-server && go test ./...` | 24 packages ok | **PASS** |
| Web suite | `cd apps/web && npm test` | 346 tests, 67 suites, 0 fail | **PASS** |
| Dataset materializer | `python3 -m unittest tools/eval/materialize_seg_dataset_test.py` | 46 tests, OK | **PASS** |
| Training gate | `python3 -m unittest tools/train/train_yolov8_seg_test.py` | 39 tests, 3 skipped | **PASS** |
| AVX2 harness | `python3 -m unittest tools/train/bench_seg_latency_test.py` | 24 tests, OK | **PASS** |
| Evidence integrity | `make evidence-lint` | 77 artifacts, 0 violations | **PASS** |
| Docs budget | `make docs-lint` | 20 baseline violations, 0 new | **PASS** |
| Deploy manifests | `make lint-deploy` | all invariants satisfied | **PASS** |

### Sprint-over-sprint metrics

| Metric | Sprint 11 | Sprint 12 | Change |
|:---|---:|---:|---:|
| Refit orchestration targets | 1 (manual) | 4 (daemon, Compose, systemd, k8s) | +3 |
| Concurrent refit protection | None | exit 4 on contention, lock preserved | Enforced |
| Model reload | Restart required | zero-restart, 4 concurrent readers | Verified |
| Near-me radius | N/A | 1,500 m (250 m bands) | New |
| Minimum touch target | 32-36 px | 48 px (64 px rows) | +12 px |
| Hazard depth unit | area ratio | cm depth, `null` when unmeasured | Replaced |
| Exhaust threshold | N/A | 20.0 cm inclusive | New |
| `cmd/server/main.go` | 1,778 lines | 229 lines | -87.1% |
| Catalog wire size | 548,012 B | 79,983 B | -85.4% |
| SSE connections per tab | 2 | 1 | -50% |
| Evidence artifacts under gate | 36 | 77 | +41 |
| Backend packages green | 23 | 24 | +1 |

---

## 6. Engineering Lessons Learned (L83-L91, 402, 411, 420, 425)

| Lesson | Core invariant | Record |
|:---|:---|:---|
| L83 | Verify native security controls before deleting CI workflows; required check != push protection. | [L83](../lessons/L83-native-github-protection-and-ci-gate-consolidation.md) |
| L84 | Never unlink advisory lock files; fail-closed read-only DB access; atomic telemetry writes. | [L84](../lessons/L84-forecast-refit-daemon-and-singleflight-locking.md) |
| L85 | Compare distance in bands; resolve staleness once; never exfiltrate coordinates; 48 px touch floor. | [L85](../lessons/L85-commuter-geolocation-banded-sorting.md) |
| L86 | 9-digit timestamps must be truncated to microseconds before parsing across heterogeneous runtimes. | [L86](../lessons/L86-rfc3339nano-9digit-timestamps-microsecond-truncation.md) |
| L87 | Identity headers are claims, not proof; verify RS256 & audience; never reflect arbitrary origins. | [L87](../lessons/L87-cryptographic-jwt-verification-and-cors-origin-whitelisting.md) |
| L88 | Bound upstream reads (io.LimitReader); query-escape camera IDs; mount gzip outside sanitizer. | [L88](../lessons/L88-proxy-oom-bounds-and-compression-middleware-ordering.md) |
| L89 | One EventSource per tab; Leaflet canvas rendering; visibility throttling for inactive tabs. | [L89](../lessons/L89-unified-sse-stream-provider-canvas-rendering-and-visibility-throttling.md) |
| L90 | Absence of traffic is evidence of impassability; classify physical cm depth; latch drops. | [L90](../lessons/L90-physical-depth-hazard-classification.md) |
| L91 | Reverse proxy must bind to upstream tunnel HTTP entrypoint; avoid phantom cert resolvers. | [L91](../lessons/L91-staging-reverse-proxy-entrypoint-and-tunnel-alignment.md) |
| 402 | Name lesson files by issue; fail-closed webhook route; rate-limit chat, not egress IP. | [402](../lessons/402-telegram-bot-and-web-subscription.md) |
| 411 | Labels without pixels are not a dataset; freeze splits before labeling; refuse unmeasured gates. | [411](../lessons/411-dataset-v3-provenance-and-avx2-evidence.md) |
| 420 | Response body buffering deadlocks SSE streams; explicit Flusher delegation is mandatory. | [420](../lessons/420-sse-streaming-sanitizer-buffering.md) |
| 425 | Factor composition root into adapters, jobs, routes (< 250 lines); test directory package. | [425](../lessons/425-decompose-monolithic-cmd-server-main.md) |

---

## 7. Carry-Over Ledger & Strategic Bridge to Sprint 13

| ID | Item | Current state | Blocker | Exit criterion |
|:---|:---|:---|:---|:---|
| C1 | S12-T5 GPU fine-tune | Materializer & gate shipped; no run | CUDA GPU (>=8GB VRAM) & 550+ frames | `val_metrics.json`, gate `PASSED`, `reinitialised_tensors: 0` |
| C2 | S12-T5 AVX2 soak | Harness host-guarded and verified | Linux x86-64 AVX2 worker | 3x200 runs, p95 <= 25.0 ms, `s12_t5_latency_avx2.json` |
| C3 | S12-T3 live re-verify | Contract/UI pass; live blocked | Trained segmentation head | 14/14 pass on `verify_s12_t3_manual.mjs` with live levels |
| C4 | T4 staging credentials | Local webhook paths verified | Real `TELEGRAM_BOT_TOKEN` | Staging bot DM screenshots & live subscription verification |
| C5 | S12-T6, T7, T8 | Not started | Sprint 13 charter | Feature store ingestion, corridor modeling, Litestream replication |

### Recommended Sprint 13 sequence
1. **Model Measurement & Soak (C1, C2)**: Execute off-box GPU fine-tuning and AVX2 benchmark soak to unblock real segmentation inference.
2. **Rain & Flood Feature Store (S12-T6)**: Append-only meteorological feature store for short-horizon forecasting (`tools/forecast/train_live_model.py`).
3. **Corridor Propagation (S12-T7)**: Multi-camera corridor congestion propagation modeling in `internal/corridor`.
4. **Litestream Replication (S12-T8)**: SQLite streaming replication to Cloudflare R2 (`deploy/litestream/`).
