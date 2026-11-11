# S12 Handoff & Sprint 12 Execution Horizon

**Document**: `docs/HANDOFF-s12.md`  
**Date**: 2026-11-11  
**Repository**: `529-studio/projectKV`  
**Charter**: Transition ledger documenting Sprint 12 current state, delivered Task 1 capability, open backlog, settled architecture decisions, pitfalls, and next tasks for a fresh conversation.

---

## 1. Branch & Commit Status

- **Active branch**: `docs/s12-handoff`
- **Head commit**: `ce76d6b` (`docs(sprint): record sprint 12 task 1 handoff`)
- **Worktree**: clean, based on `origin/dev` (`3682af6`)
- **Merged PRs**:
  - PR #416 (`feat(forecast): automated cron sidecar and dynamic model reload (Close #413)`, commit `3682af6`)
  - PR #415 (`fix(ci): consolidate public-repository workflow gates`, commit `1a8eb05`)
  - PR #412 (`docs(sprint): record sprint 11 completion report and final handoff`, commit `5b7614c`)

---

## 2. Merged Deliverables

| PR | Task | Subsystem | Delivered capability |
| :--- | :--- | :--- | :------------------- |
| **#416** | **S12-T1** (#413) | `tools/forecast`, `go-server`, `deploy/` | **Automated Forecast Refit Cron & Dynamic Reload**: Advisory non-unlinking single-flight flock (`ForecastRefitLock`), versioned `SchedulerState` v1 with atomic quarantine, wall-clock slot scheduling in `Asia/Ho_Chi_Minh`, Go zero-restart `snapshot()` reloader, K8s CronJob, hardened systemd units with `SuccessExitStatus=4`, and Compose sidecar. Shipped L84. |

---

## 3. Open Issues & Backlog

- **#413 (S12-T1)**: `[ops + forecast] Automated Cron & Sidecar Scheduling for Forecast Live Refit Pipeline` — **Merged into dev in PR #416 and closed**.
- **#400 (S12-T2)**: `[frontend] Commuter Last-Mile Geolocation (GPS 'Near-Me' Flood Proximity View)` — next task on roadmap.
- **#401 (S12-T3)**: `[cv-engine + go-server] Motorbike Flood Stall Hazard Level Classifier (cm Depth)` — physical tire submersion geometry into Safe/Warning/Danger hazard bands.
- **#402 (S12-T4)**: `[ops + frontend] Telegram Alert Bot Live Provisioning & Citizen Subscription Web UI` — webhook live bot integration and citizen subscription modal.
- **#411 (S12-T5)**: `[cv-engine + infra] Off-box GPU YOLOv8n-seg Fine-Tuning on 550+ Frames & AVX2 Worker Latency Benchmark` — fine-tuning for held-out $\text{IoU} \ge 0.612$ and Linux AVX2 p95 $\le 25\text{ ms}$.

---

## 4. Settled Architectural Decisions

- **ADR-013 D1 (Evidence Integrity CI Gate)**: Synthetic fixtures and fake future timestamps are strictly forbidden (`tools/ci/check_evidence_integrity.py`). All release qualification requires `fit_source == "live_history"`.
- **ADR-013 D4 (Standard-Library Forecast Refit)**: In-process daemon and oneshot modes run pure Python standard library (`fcntl`, `json`, `zoneinfo`) with zero third-party scheduling dependencies.
- **Lesson L83 (Native GitHub Protection & CI Gate Consolidation)**: Public safety and branch guard consolidation into native GitHub branch rules.
- **Lesson L84 (Forecast Refit Daemon & Single-Flight Locking)**: Advisory `fcntl.flock` with zero unlink on release; atomic `os.replace` within the same directory; state quarantine on corrupt JSON.
- **Go Modulith Reloader Pattern**: Lock-free atomic `snapshot()` reader pattern in `internal/forecast`; dynamic pre-stat and post-stat validation prevents serving partial or corrupted weights.

---

## 5. Pitfalls Encountered

- **Concurrent Lesson Numbering Merge Conflict**: PR #415 claimed L83 concurrently while PR #416 was authored. Resolved by renumbering the refit scheduler lesson to L84 and trimming `docs/lessons/INDEX.md` to 20,473 B (strictly maintaining the 20,480 B cap per Rule 11).
- **Flock Inode Lifetime Trap**: Unlinking lock files upon release breaks POSIX advisory locking across concurrent invocations. `ForecastRefitLock` strictly preserves the file on disk and truncates/rewrites diagnostics upon acquisition.
- **Benign Concurrency Misclassified as Unit Failure**: Concurrent scheduler invocations correctly exit code 4 (`EXIT_FAIL_ON_LOCKED`), which systemd initially flagged as a unit failure. Resolved by configuring `SuccessExitStatus=4` in `projectkv-forecast-refit.service`.
- **Rootless Container Runtime Environment**: Container rootfs is strictly read-only. Setting `TMPDIR=/tmp`, `HOME=/tmp`, non-root user `10001:10001`, `fsGroup: 10001`, and `seccompProfile: {type: RuntimeDefault}` prevents permission crashes while satisfying restricted PSS.

---

## 6. Sprint 12 Horizon: next tasks

1. **S12-T2: Commuter Last-Mile Geolocation (GPS 'Near-Me' View)** — `apps/web/src/hooks/useGeolocation.ts`, `apps/web/src/components/NearMeModal.tsx` (Issue #400).
2. **S12-T3: Motorbike Flood Stall Hazard Level Classifier (cm Depth)** — `services/cv-engine`, `services/go-server/internal/flood` (Issue #401).
3. **S12-T4: Telegram Alert Bot Live Provisioning & Citizen Subscription Web UI** — `services/go-server/internal/notify`, `apps/web` (Issue #402).
4. **S12-T5: Off-box GPU YOLOv8n-seg Fine-Tuning & AVX2 Latency Benchmark** — `data/yolo_seg/`, `tools/train/` (Issue #411).
5. **S12-T6: Rain & Flood Feature Store Ingestion for Short-Horizon Forecasting** — `tools/forecast/train_live_model.py`.
6. **S12-T7: Multi-Camera Corridor Congestion Propagation Modeling** — `services/go-server/internal/corridor`.
7. **S12-T8: High-Availability SQLite Streaming Replication (Litestream / Cloudflare R2)** — `deploy/litestream/`.

---

## 7. Open Questions For The Human

- **GPS Distance Metric for S12-T2 (#400)**: In the "Near-Me" modal, should cameras within 1.5 km be sorted strictly by Haversine distance, or sorted by composite congestion/flood severity first?
