# S12 Handoff & Sprint 13 Horizon

**Document**: `docs/HANDOFF-s12.md`  
**Date**: 2026-11-12  
**Repository**: `529-studio/projectKV`  
**Charter**: Transition ledger documenting Sprint 12 completion state, delivered capabilities, settled architecture decisions, pitfalls, and next tasks for a fresh conversation.

---

## 1. Branch & Commit Status

- **Active branch**: `docs/s12-close`
- **Head commit**: `28bef6e` (`Merge pull request #436 from 529-studio/feat/411-gpu-finetune-and-avx2-benchmark`)
- **Worktree**: clean on `origin/dev` base
- **Merged in Sprint 12**:
  - PR #415 (`fix(ci): consolidate public-repository workflow gates`, `1a8eb05`)
  - PR #416 (`feat(forecast): automated cron sidecar and dynamic model reload (Close #413)`, `3682af6`) — S12-T1
  - PR #417 (`chore(catalog): camera catalog resync 20261111-32`, `0711989`)
  - PR #418 (`docs(sprint): record sprint 12 task 1 handoff`, `818be75`)
  - PR #419 (`feat(web): commuter last-mile geolocation and banded proximity view (Close #400)`, `0671ea0`) — S12-T2
  - PR #426 (`fix(forecast): parse 9-digit nanosecond RFC3339 timestamps`, `1ae902c`)
  - PR #427 (`feat(auth): enforce cryptographic JWT verification in dev proxy`, `5a91059`)
  - PR #428 (`fix(server): harden snapshot proxy with OOM bounds and gzip middleware ordering`, `fcfd11b`)
  - PR #429 (`refactor(web): consolidate dual SSE connections into single provider`, `85a0dc8`)
  - PR #430 (`feat(cv-engine,go-server,web): motorbike flood stall hazard level classifier (Close #401)`, `5579759`) — S12-T3
  - PR #431 (`feat(ops+web): Telegram alert bot live provisioning and citizen subscription modal (Close #402)`, `58f925b`) — S12-T4
  - PR #433 (`fix(ops): align staging Traefik router entrypoint with gateway tunnel HTTP port (Close #432)`, `865eafe`)
  - PR #434 (`fix(sanitizer): bypass buffering for text/event-stream and delegate Flusher (Close #420)`, `aaa5794`)
  - PR #435 (`refactor(server): decompose monolithic cmd/server/main.go into modular bootstrap components (Close #425)`, `9023f7d`)
  - PR #436 (`feat(cv-engine+infra): S11-T5b GPU fine-tuning dataset v3 materializer and AVX2 latency benchmark (Close #411)`, `28bef6e`) — S12-T5

---

## 2. Merged Deliverables

| PR | Task | Subsystem | Delivered capability |
| :--- | :--- | :--- | :------------------- |
| **#416** | **S12-T1** (#413) | `tools/forecast`, `go-server`, `deploy/` | **Forecast Refit Daemon & Atomic Dynamic Reload**: Advisory non-unlinked flock, durable scheduler state v1, wall-clock slots in `Asia/Ho_Chi_Minh`, lock-free zero-restart reload, Compose sidecar, systemd units (`SuccessExitStatus=4`), and K8s CronJob. |
| **#419** | **S12-T2** (#400) | `apps/web` | **Commuter Last-Mile Geolocation & Banded Sorting**: SSR-safe `useGeolocation`, 250 m distance quantization, severe flood prioritization over trivial proximity, zero coordinate exfiltration, and >=48 px touch ergonomics. |
| **#430** | **S12-T3** (#401) | `services/cv-engine`, `go-server`, `apps/web` | **Motorbike Flood Stall Hazard Level Classifier**: Physical cm depth classification based on 110 cm motorbike bounding box height, 20 cm exhaust waterline threshold, avoidance inference for empty flooded roads, asymmetric hysteresis, and honest null rendering. |
| **#431** | **S12-T4** (#402) | `services/go-server`, `apps/web` | **Telegram Alert Bot Live Provisioning & Citizen Subscription**: Fail-closed webhook route (404 when unprovisioned), per-chat token bucket rate limiter (6 cmds/min + 1 notice), 64-char deep-link codec, and bilingual subscription modal. |
| **#436** | **S12-T5** (#411) | `tools/eval`, `tools/train` | **Dataset v3 Materializer & AVX2 Latency Benchmark Harness**: Provenance-bound materializer rejecting orphaned/synthetic labels, frozen group split `(camera_id, capture_date)`, measured retraining gate (>=50 epochs), and Linux x86-64 AVX2 host guardrail. |
| **#434** | **#420** | `services/go-server/internal/middleware` | **SSE Sanitizer Buffering Bypass & Flusher**: Direct passthrough for `text/event-stream` preventing response buffering deadlocks, and explicit compile-time `http.Flusher` assertions. |
| **#435** | **#425** | `services/go-server/cmd/server` | **Modular Composition Root Decomposition**: Factored monolithic 1,778-line `main.go` into `adapters.go`, `jobs.go`, and `routes.go` (<250 lines in `main.go`), with rooted SPA asset confinement. |

---

## 3. Open Issues & Backlog

- **All Sprint 12 issues closed**: Issues #400, #401, #402, #411, #413, #414, #420, #421, #422, #424, #425, #432 are 100% merged and closed.
- **Carry-over technical debt from S12-T5**:
  - Off-box CUDA GPU fine-tuning execution (AC10-AC14) on 550+ frames to generate measured `val_metrics.json` and train segmentation head.
  - Linux x86-64 AVX2 latency soak execution (AC18-AC19) to produce `s12_t5_latency_avx2.json`.
  - Re-verifying S12-T3 live hazard endpoints once trained segmentation weights are mounted in `MODEL_DIR`.

---

## 4. Settled Architectural Decisions

- **ADR-013 D1 (Evidence Integrity CI Gate)**: Synthetic fixtures and fake future timestamps are strictly forbidden (`tools/ci/check_evidence_integrity.py`). All release qualification requires `fit_source == "live_history"`.
- **ADR-013 D4 (Standard-Library Forecast Refit)**: In-process daemon and oneshot modes run pure Python standard library (`fcntl`, `json`, `zoneinfo`) with zero third-party scheduling dependencies.
- **Lesson L84 (Forecast Refit Daemon & Single-Flight Locking)**: Advisory `fcntl.flock` with zero unlink on release; atomic `os.replace` within same directory; state quarantine on corrupt JSON.
- **Lesson L85 (Commuter Geolocation & Banded Sorting)**: 250 m distance bands prevent dry nearby cameras from outranking severe downstream flood hazards; clock-free pure comparator.
- **Lesson L90 (Physical Depth Hazard Classification)**: Physical cm depth derived from motorbike tire geometry; avoidance of empty roads is treated as impassability; latch prevents flickering.
- **Lesson 402 (Issue-Keyed Lessons & Fail-Closed Webhooks)**: Name lesson files by issue number; webhook route is absent (404) unless valid credentials exist; rate-limit chat ID, not shared Telegram egress IP.
- **Lesson 411 (Dataset Provenance & Retraining Gate)**: Exact timestamp and hash join between labels and proof frames; frozen split; refuse unmeasured retraining gates.
- **Lesson 420 (SSE Streaming Sanitizer Bypass)**: Body-buffering middleware must bypass `text/event-stream`; explicit compile-time `http.Flusher` delegation.
- **Lesson 425 (Composition Root Decomposition)**: Split `cmd/server/main.go` into `adapters.go`, `jobs.go`, `routes.go` (<250 lines in `main.go`); package tests invoke `go run .`.

---

## 5. Pitfalls Encountered

- **Orphaned Dataset Fallacy**: Staged labels without corresponding pixel frames produce empty joins; 47% of v2 dataset was template-jittered synthetic data. Resolved by building Dataset v3 from live proof archive with strict hash validation.
- **Streaming Response Deadlock in Buffering Middleware**: `SecretSanitizer` response buffering deadlocked persistent SSE connections. Solved by sniffing `Content-Type` before buffering and passing streams directly to wire.
- **Flock Inode Lifetime Trap**: Unlinking advisory lock files breaks POSIX locking across concurrent processes. Preserved lock file permanently on disk.
- **Monolithic `main.go` Package Integration**: Splitting `package main` broke integration test harnesses that invoked `go run ./main.go`. Replaced with `go run .` across all test harnesses.
- **Host Architecture Latency Distortion**: Benchmarking OpenVINO on Apple M4 yields numbers irrelevant to AMD Ryzen Linux AVX2 target. Implemented strict CPU feature host guard.

---

## 6. Sprint 13 Horizon: next tasks

1. **S13-T1: Off-box GPU YOLOv8n-seg Retraining & AVX2 Latency Soak** — `tools/train/train_yolov8_seg.py`, `tools/train/bench_seg_latency.py`
2. **S13-T2: Live Flood Hazard Re-Verification with Trained Weights** — `services/cv-engine`, `tools/test/verify_s12_t3_manual.mjs`
3. **S13-T3: Rain & Flood Feature Store Ingestion for Short-Horizon Forecasting** — `tools/forecast/train_live_model.py`, `internal/store`
4. **S13-T4: Multi-Camera Corridor Congestion Propagation Modeling** — `services/go-server/internal/corridor`
5. **S13-T5: High-Availability SQLite Streaming Replication (Litestream / R2)** — `deploy/litestream/`, `docker-compose.stg.yml`
6. **S13-T6: Telegram Bot Staging Provisioning & End-to-End Live Verification** — `services/go-server/internal/telegrambot`
7. **S13-T7: Curb Reference Geometry Segmentation for Pedestrian Sidewalk Flood Assessment** — `services/cv-engine/water_level.py`

---

## 7. Open Questions For The Human

- **GPU Fine-Tuning Execution Environment**: Should off-box GPU fine-tuning be executed on an external cloud GPU instance (e.g. RunPod / LambdaLabs with NVIDIA A10G/T4) or on local dedicated hardware with CUDA support?
- **Litestream Replication Target**: For S13-T5, should Litestream replicate exclusively to Cloudflare R2, or maintain a secondary replica on MinIO/S3 for local development environments?
