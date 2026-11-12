# S13 Handoff & Sprint 14 Horizon

**Document**: `docs/HANDOFF-s13.md`  
**Date**: 2026-11-12  
**Repository**: `529-studio/projectKV`  
**Charter**: Transition ledger documenting Sprint 13 completion state, delivered capabilities, settled architecture decisions, pitfalls, and next tasks for a fresh conversation.

---

## 1. Branch & Commit Status

- **Active branch**: `docs/s13-report-and-audit`
- **Head commit**: `9418d29` (`Merge pull request #440 from 529-studio/feat/s13-t1-gpu-finetune-and-avx2-soak`)
- **Worktree**: clean on `origin/dev` base
- **Merged in Sprint 13**:
  - PR #439 (`chore(data): resync camera catalog from health audit (2026-11-12)`, `8ff8fab`)
  - PR #440 (`feat(cv-engine+infra): S13-T1 geometry tracing fix, Colab training runner and AVX2 Docker soak (Close #438)`, `911ffb5`)

---

## 2. Merged Deliverables

| PR | Task | Subsystem | Delivered capability |
| :--- | :--- | :--- | :------------------- |
| **#440** | **S13-T1** (#438) | `tools/seg`, `tools/train`, `deploy/benchmark` | **Moore Boundary Tracing Fix, Colab GPU Retraining Pipeline & AVX2 Docker Soak**: Fixed premature 3-step loop termination via state-based Jacob's stopping criterion `(pixel, backtrack)` and 8-connected merge, restoring candidate extraction yield from 370 to 6,386 polygons across 744 live frames (0 empty payloads). Created reproducible Colab runner `colab_train_yolov8_seg.ipynb` + `colab_pipeline.py` enforcing $\ge 50$ epochs and mask-pixel IoU. Delivered pinned `deploy/benchmark/Dockerfile.avx2` and `bench_avx2_docker.sh` with host refusal guards. |
| **#439** | Catalog | `data/` | **Camera Catalog Resync**: Updated camera registry from live health audit. |

---

## 3. Open Issues & Backlog for Sprint 14

- **All Sprint 13 issues closed**: Issue #438 is 100% merged and closed.
- **Sprint 14 Issues Opened**:
  - **Issue #441**: `[backend + ops] S14-T1: Fix Live Inference Pipeline Darkness, 24h History Coverage & Health Observability`
  - **Issue #442**: `[infra + db] S14-T2: High-Availability SQLite Streaming Replication (Litestream / R2) & Automated Restore Drill`
  - **Issue #443**: `[frontend + ux] S14-T3: Commuter Map Sunlight Readability, Marker Clustering & Ubiquitous Hazard Depth Rendering`
  - **Issue #444**: `[forecast + cv] S14-T4: Forecast Weather Feature Parity, Minimum Skill Gating & Off-box Model Promotion`

---

## 4. Settled Architectural Decisions

- **ADR-013 D1 (Evidence Integrity CI Gate)**: Synthetic fixtures and fake future timestamps are strictly forbidden. All release qualification requires `fit_source == "live_history"`.
- **Lesson 438 (Moore Boundary Tracing & Jacob's Stopping Criterion)**: State tracking `(pixel, backtrack)` is required for Moore contour tracing. Re-visiting start pixel without backtrack verification terminates loops prematurely on protrusion tips and diagonal antennas.
- **Lesson 411 (Dataset Provenance & Retraining Gate)**: Exact timestamp and hash join between labels and proof frames; frozen split; refuse unmeasured retraining gates.

---

## 5. Pitfalls Encountered

- **Premature Moore-Neighbor Termination**: `trace_boundary` exited on `cur == start and len(path) > 2`, collapsing vehicle polygons into 2-vertex degenerate segments. Solved by indexing state as `(cur, backtrack)` and terminating only on repeated states.
- **Component Run-Merge 4-Connectivity Trap**: Slicing `[s:e]` in union-find row merges implemented 4-connectivity, severing 1-pixel diagonal antennas from vehicle bodies. Widened bounds to `[s-1, e+1]`.
- **Live Data Plane Invisibility**: Backend unit tests passed 100%, but live production feeds suffered 100% UNKNOWN flood states and 3.5% history continuity due to unmonitored CV engine dropouts and slow-feed rejection.

---

## 6. Sprint 14 Horizon: next tasks

1. **S14-T1: Fix Live Inference Pipeline Darkness, 24h History Coverage & Health Observability** (#441) — `services/go-server/internal/cvc`, `internal/camera`
2. **S14-T2: High-Availability SQLite Streaming Replication (Litestream / R2) & Automated Restore Drill** (#442) — `deploy/litestream`, `Makefile`
3. **S14-T3: Commuter Map Sunlight Readability, Marker Clustering & Ubiquitous Hazard Depth Rendering** (#443) — `apps/web/src/components/MapComponent.tsx`, `apps/web/src/components/PinnedDock.tsx`
4. **S14-T4: Forecast Weather Feature Parity, Minimum Skill Gating & Off-box Model Promotion** (#444) — `tools/forecast/train_live_model.py`, `services/go-server/internal/forecast/model.go`

---

## 7. Open Questions For The Human

- **External GPU Execution**: Shall we execute the 100-epoch Colab fine-tuning run (`tools/train/colab_train_yolov8_seg.ipynb`) on Google Colab or on a dedicated remote cloud GPU instance (RunPod/Lambda)?
- **Basemap Tile Licensing**: For S14-T3, should we switch the dark basemap to CARTO Dark Matter or host custom dark vector tiles via our proxy cache to eliminate third-party rate limits?
