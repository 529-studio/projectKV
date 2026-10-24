# ADR-011: Sprint 8 Audit Reconciliation & Sprint 9 Execution Architecture — Ground-Truth Segmentation Flywheel & Live Model Retraining

**Status**: Accepted
**Date**: 2026-10-24
**Authors**: ProjectKV Engineering & Architecture Team
**Auditor**: Senior Technical Auditor & Devil's Advocate
**Governing Documents**: `AGENTS.md` (Rules 1–9), `docs/GO_BACKEND_ARCHITECTURE.md`, `docs/ADR-009`, `docs/ADR-010`
**Related**:
- [`docs/sprint8_report.md`](sprint8_report.md) (Sprint 8 Audited Completion & Reality-Check Findings)
- [`docs/sprint9_execution_guide.md`](sprint9_execution_guide.md) (Sprint 9 Multi-Wave Execution Guide)
- [`docs/mlops_automated_annotation_and_bulk_approval.md`](mlops_automated_annotation_and_bulk_approval.md) (Bulk annotation pipeline benchmarks)
- [`docs/lesson_learn.md`](lesson_learn.md) (Lessons 39, 52–57)
- 529 Studio Outline Wiki — hardware profiles & topology (`home-srv`, `dev-box`)

---

## 1. Context & Retrospective Reality-Check Audit

Sprint 8 delivered all five ADR-010 tickets (four merged, one code-complete pending operator) plus seven hardening PRs (#276–#295). The post-sprint audit ([`sprint8_report.md` §3](sprint8_report.md)) confirms the concurrency, crowdsource, and toolchain objectives — and exposes that the strategic goal "train and deploy our own flood segmentation model" is still blocked at exactly one layer: **geometry**.

The verified facts:

1. **Data**: 23,665 classification labels (provenance-audited) on R2; **zero** polygon/mask annotations. `LabelPayload` had no geometry field; `/labeler` has no drawing UI.
2. **Exporter hazard**: `export_yolo_seg.py` silently **synthesizes fake polygons** (fixed roadway trapezoid + generic puddles derived from classification) when real geometry is absent — a geometry-level repeat of Lesson 39's answer-key pollution.
3. **Runtime**: the staging cv-engine serves the Sprint 3 COCO vehicle model; `yolov8n_seg` has never been trained; production flood verdicts depend on the quota-limited Gemini VLM (10 RPM / 300 RPD).
4. **Harvesting**: the scheduled harvester (07:30/12:00/17:30 ICT + rain watcher) is complete but `HARVESTER_ENABLED=false` by default; staging data collection currently requires a developer terminal (`make dev-all`) instead of running autonomously on Stack 14.
5. **Deferred defects**: PR #296's verification script hardcodes the wrong district (fixture value) versus production `cam.Zone` (`Khu vực Nội thành TP.HCM`); forecasting capability is zero (nowcast only).
6. **Hardware ground truth** (Outline): dev-box (Ryzen 7 6800HS, AVX2; YOLOv8n p50 16.17 ms; hosts cv-engine + GHA runner) is the only viable host for assisted annotation inference; home-srv (Celeron N2930, non-AVX) is ingress + fallback only.
7. **Governance**: Plane Hub (source of `closes [KV-x]` automation) was decommissioned 10/2026; tracking moved to GitHub Projects v2 while AGENTS.md Rule 9 still mandates the dead format.

## 2. Architecture Decisions

### D1. Segmentation Polygon Annotation Contract (canonical, single source)

`LabelPayload` gains `annotations: []PolygonAnnotation` with:

- `class`: canonical 6-class taxonomy (`roadway`, `water_puddle`, `motorcycle`, `car`, `bus`, `truck`); alias mapping (`flood → water_puddle`, `road → roadway`, …) normalized **server-side** in `SaveLabel`, kept in lockstep with `CLASS_ALIASES` in `export_yolo_seg.py`.
- `polygon`: `[[x, y], …]` **normalized to [0, 1]** against image natural size (client normalizes; the server rejects pixel coordinates to eliminate resolution ambiguity).
- Limits: 3–64 vertices, ≤ 32 annotations per label.
- `source`: provenance string (`human`, `sam_prelabel`, …), ≤ 32 chars.
- Draft-07 label schema (`tools/eval/label_schema.json`, `internal/harvester/schema.go`) gains `annotations` as an optional property so harvester-generated labels can later carry model-proposed masks with provenance.

*Rationale*: one contract consumed by web labeler, harvester, SAM pre-labeler, and exporter — eliminating the drift class discovered in the PR #296 audit (hand-copied values diverging from the production data path).

### D2. Deprecate Synthetic Geometry in the Exporter

`synthesize_polygons_from_metadata` becomes opt-in (`--allow-synthetic`). Default behavior: labels without explicit annotations are **skipped and counted** (`skipped_no_annotations`), never silently fabricated. Extends Lesson 39 (never train on synthetic answer keys) from scalar labels to geometry.

### D3. SAM-Assisted Pre-Labeling, Offline on dev-box Only

Batch pre-labeling (`tools/seg/prelabel_sam.py`) streams R2 frames through a compact promptable segmentation model (EfficientViT-SAM / MobileSAM class, ONNX/OpenVINO, weights path configured via environment per Rule 8) on **dev-box only** (AVX2; never on the non-AVX Celeron fallback and never inside the live 30 s hot-set loop). Output: label JSON with `annotations` + `annotator: "sam_prelabel"` + `source: "sam_prelabel"` uploaded to R2 as candidates; promotion to ground truth requires the existing human bulk-approval path (provenance stamped). Roadway masks are proposed automatically (largest stable central region heuristic); water masks are proposals-only because specular wet asphalt cannot be distinguished from standing water without human judgment (ADR-009 §4.3).

### D4. First-Training Benchmark Gate

Before any segmentation weights are deployed: dataset v1 (target ≥ 500 masked frames; stratified across rain/dry, day/night), off-box training via `tools/train/train_yolov8_seg.py`, and a **quantitative evaluation report** (mask mAP50-95, per-class IoU on a held-out gold split) committed to `docs/`. Deployment proceeds only if the model beats the null baseline on `water_puddle` IoU; inference latency p95 must be re-measured on dev-box at 416 px against the 30 s / 57-camera budget, falling back to 320 px if exceeded. Weights are promoted into the dev-box Docker volume per the existing DEVOPS runbook; cv-engine already prefers `yolov8n_seg.xml` when present.

### D5. Staging Autonomous Data Flywheel

Enable `HARVESTER_ENABLED=true` on Portainer Stack 14 (staging) so fixed-hour collection + cv-engine auto-labeling run unattended — removing the `make dev-all` terminal dependency. Add `GET /api/harvester/status` (last/next slot, cameras harvested, labels uploaded, recent errors) so operators verify flywheel health from the browser. Deliverables include a staging verification runbook with empirical log/R2 evidence.

### D6. Governance: Kanban Linking Moves to GitHub Projects v2

With Plane Hub decommissioned, `closes [KV-x]` has no backend automation. Sprint 9 onward: issues and PRs link via GitHub's native `Close #<issue-number>` and are tracked on the org GitHub Projects v2 board. AGENTS.md Rule 9 is amended accordingly in the Sprint 9 documentation PR.

### D7. Forecasting Direction (stretch, S9-T6)

Congestion forecasting (10–30 min ahead) from the accumulated PCU/verdict time-series — precedent: DatapolisX's scikit-learn predictor over YOLO counts. Prototype must outperform a persistence baseline on backtest MAE before any UI exposure. Deliberately scheduled after the segmentation loop so it never competes with the critical path.

## 3. Sprint 9 Backlog

| Wave | ID | Issue | Title | Labels | Est. (Rule 3) | Depends |
|:---:|:---:|:---:|---|---|:---:|:---:|
| 0 | S9-T1 | (created) | `[go-server + web + tooling] Segmentation Polygon Annotation Contract & Canvas Editor in /labeler` | `backend, frontend, cross-cutting` | 16–20h | — |
| 1 | S9-T2 | (created) | `[tooling] SAM-Assisted Polygon Pre-Labeling Batch Toolchain (dev-box offline)` | `tooling` | 8–10h | S9-T1 |
| 1 | S9-T3 | (created) | `[ops + go-server] Staging Autonomous Data Flywheel: Scheduled Harvester, Auto-Label & Status Observability` | `tooling, backend, cross-cutting` | 6–8h | — |
| 2 | S9-T4 | (created) | `[tooling + cv-engine] First Off-Box YOLOv8-Seg Training, mAP Benchmark Gate & Staging Weights Deployment` | `tooling, cross-cutting` | 10–12h | S9-T1, T2, T3 |
| 3 | S9-T5 | (created) | `[go-server + ops] Telegram Verification Script Single-Source Parity Rework (notify-preview)` | `backend, tooling` | 4h | — |
| 3 | S9-T6 | (created) | `[go-server] Short-Horizon Congestion Forecast Prototype (PCU Time-Series, 10–30 min)` — stretch | `backend, enhancement` | 12h | — |

Total: ~56–70 h active effort (7–9 working days).

## 4. Risk Register

| Risk | Likelihood | Impact | Mitigation |
|---|:---:|:---:|---|
| Polygon annotation throughput too slow by hand | High | High | D3 SAM pre-labeling; bulk-approval pattern already proven at 78 labels/s |
| SAM weights too heavy for dev-box 2 vCPU cgroup | Medium | Medium | EfficientViT-SAM/MobileSAM class models (~40 MB ONNX); measure before batch; run off-peak; rate-limit like `batch_auto_label.py` |
| First model underperforms on water IoU | Medium | Medium | D4 gate prevents bad deploy; fallback remains VLM + crowdsource; iterate dataset |
| 416 px latency breaks 30 s/57-camera budget | Medium | High | D4 mandates latency re-measurement; 320 px fallback; hot-set interval is configurable |
| R2 label-key collision between human and SAM labels | Low | Medium | Annotator namespaced keys (`sam_prelabel`) + existing `resolveCanonicalLabels` precedence |
| Staging harvester floods R2 with low-value frames | Low | Medium | Fixed 3-slot schedule + rain cooldown already implemented; retention policy 7 days |

## 5. Consequences

- **Positive**: the training loop becomes closed (harvest → pre-label → human approve → export → train → benchmark → deploy) with every stage auditable; synthetic-data hazards eliminated by contract; staging runs unattended.
- **Negative / costs**: labeler gains UI complexity (canvas mode); exporter behavior change requires re-baselining any local datasets exported with synthesis; dev-box shares capacity between cv-engine, GHA runner, and batch pre-labeling (scheduled off-peak).
- **Neutral**: classification labels remain fully valid and untouched; the polygon contract is additive and backward compatible.
