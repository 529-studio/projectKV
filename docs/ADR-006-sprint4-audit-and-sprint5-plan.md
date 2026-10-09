# ADR-006: Sprint 4 Reality-Check Audit & Sprint 5 Plan (The Ground-Truth Reckoning)

**Status**: Proposed  
**Date**: 2026-10-10  
**Authors**: Warded Architecture Team & System Reviewers (Claude / Antigravity)  
**Related**: 
- [ADR-004](ADR-004-route-check-and-snapshot-archive.md) (Accuracy Gate §D6, Storage Interface §D7, Standby Guard Finding 1)
- [ADR-005](ADR-005-sprint3-audit-and-sprint4-plan.md) (Sprint 3 Production Audit & Sprint 4 Plan)
- [`docs/sprint4_report.md`](sprint4_report.md) (Sprint 4 Completion & Verification Audit)
- [`docs/sprint4_audit_qa.md`](sprint4_audit_qa.md) (Sprint 4 Technical Audit & Ground-Truth Verification Q&A)
- [`docs/reports/eval_report_d6_real.md`](reports/eval_report_d6_real.md) (T4 Benchmark: Macro F1 = 0.209, Flood FNR = 100%)
- [`docs/reports/smoke_eval_d6_real.md`](reports/smoke_eval_d6_real.md) (T7 Smoke Test: 2/10 Pass Rate = 20.0%)
- [`docs/pcu-calibration-report.md`](pcu-calibration-report.md) (T5 Parameter Realignment)
- [`docs/decision-openvino-vs-vlm.md`](decision-openvino-vs-vlm.md) (T9 Decision: Decoupled Hybrid Architecture)

---

## 1. Context & Executive Summary

Sprint 4 successfully accomplished its primary mission of **closing the physical reporting gap**:
1. Synthetic mock frames (73,214 files of 107-byte placeholders) were completely eliminated from evaluation pipelines.
2. The archiver was decoupled from coincidental viewer cache hits (Gate T2b / PR #185), unlocking authentic frame accumulation at **733+ real frames/hour**.
3. Real ground-truth datasets were gathered during the severe HCMC rainstorm of 2026-10-08: **764 real labeled frames** across 206 cameras, including **120 flood-positive frames** (exceeding ADR-004 §D6 quotas by 254%).
4. The first real D6 accuracy evaluation was executed (T4), and PCU parameters were calibrated against Vietnamese transport standards (T5 / PR #202).
5. OpenVINO was retained as the continuous traffic telemetry foundation, supported by a dedicated worker node (`dev-box` AMD Ryzen 7 6800HS) achieving **24.01 FPS** (T9 / PR #205).

However, a rigorous technical audit conducted between the engineering team and system evaluation agents (Claude / Antigravity) revealed **three critical structural flaws and two operational blind spots** in Sprint 4's closing decisions. ADR-006 records these audit findings and defines the strict, evidence-first execution roadmap for Sprint 5.

---

## 2. Audit Findings & Reality-Check Discoveries

### Finding 1 — The T9 Decision Premise Gap: T4 Was Never Re-Measured Post-T5 Calibration
- **The Incident**: In Sprint 4 T4 ([Issue #195](https://github.com/529-studio/projectKV/issues/195)), the real D6 benchmark produced a **Macro F1 of 0.209** (nominal target $\ge 0.70$). T5 ([Issue #196](https://github.com/529-studio/projectKV/issues/196)) diagnosed this as an uncalibrated `CapacityFloor = 25.0` and unweighted motorcycle coefficients, adjusting them to `CapacityFloor = 15.0`, `Motorcycle = 0.30`, and `Bus = 2.50`.
- **The Flaw**: Following PR #202 (T5), the team verified PCU calculation logic purely through mock unit tests (`services/go-server/internal/cvclient/pcu_test.go`). **The benchmark (`tools/eval/benchmark.py`) was NEVER re-run against the 764 labeled real frames.**
- **The Violation**: T9 formally concluded to retain OpenVINO based on the assumption that T5 resolved the traffic classification issue. Deciding an architecture based on unmeasured parameter calibration violates the project's core rule: *"Unit tests passing in isolation is never acceptable proof of correctness."*

---

### Finding 2 — The Mathematical Paradox of T5: Lowering Capacity Floor Exacerbates False Positives
- **The Discovery**: A granular breakdown of the 2/10 (20.0%) smoke-test result in [`docs/reports/smoke_eval_d6_real.md`](reports/smoke_eval_d6_real.md) exposed an alarming mathematical contradiction:
  - **4 out of 5 expected `CLEAR` cameras failed by falsely detecting congestion**:
    - `Mai Chí Thọ - Nguyễn Cơ Thạch 1`: Expected CLEAR $\rightarrow$ Actual **HEAVY** ($SI = 0.70$)
    - `Mai Chí Thọ - Tố Hữu`: Expected CLEAR $\rightarrow$ Actual **MODERATE** ($SI = 0.51$)
    - `Võ Văn Kiệt - Ký Con`: Expected CLEAR $\rightarrow$ Actual **HEAVY** ($SI = 0.84$)
    - `Điện Biên Phủ - Pasteur`: Expected CLEAR $\rightarrow$ Actual **CONGESTED** ($SI = 1.00$)
  - **4 out of 5 expected `CONGESTED` cameras failed by missing congestion entirely**:
    - `Cộng Hòa - Trường Chinh`: Expected CONGESTED $\rightarrow$ Actual **CLEAR** ($SI = 0.00$, zero vehicles detected)
    - `Mai Chí Thọ - Đồng Văn Cống 2`: Expected CONGESTED $\rightarrow$ Actual **CLEAR** ($SI = 0.00$, zero vehicles detected)
    - `Điện Biên Phủ - CMT8`: Expected CONGESTED $\rightarrow$ Actual **CLEAR** ($SI = 0.32$)
    - `Cộng Hòa - Tân Kỳ Tân Quý`: Expected CONGESTED $\rightarrow$ Actual **MODERATE** ($SI = 0.40$)
- **The Mathematical Reality**:
  The Saturation Index formula is:
  $$SI = \frac{PCU_{\text{current}}}{CapacityFloor}$$
  By lowering `CapacityFloor` from $25.0$ down to $15.0$, every calculated Saturation Index is multiplied by $\frac{25}{15} \approx 1.67\times$.
  - For the 4 CLEAR cameras that already produced high vehicle counts ($SI = 0.51, 0.70, 0.84, 1.00$), the new SIs become **$0.85, 1.17, 1.40, 1.67$**. Instead of fixing the error, **T5 escalates false-positive congestion into gridlock errors!**
  - For the 2 CONGESTED cameras with $SI = 0.00$, no capacity floor adjustment can help: $\frac{0.00}{15.0} = 0.00$.
- **The Root Cause**: The primary problem is **not a scoring formula defect**; it is rooted in the **object detection perception layer**:
  1. *Vehicle Hallucination / Background False Positives*: Distant headlights, road markings, or roadside structures are falsely counted as vehicles during off-peak hours.
  2. *Severe Congestion Occlusion*: In dense bumper-to-bumper queues, the model fails to detect overlapping vehicles, reporting zero or near-zero counts.
  3. *Sampling Window Skew*: Smoke testing was executed at 20:19 (nighttime), violating the designated off-peak (13:00–15:00) and peak (17:00–18:30) validation windows.

---

### Finding 3 — Documentation Discrepancies: Fictitious Flood Chokepoints & Rain Threshold Divergence
- **The Discrepancies**:
  1. *Fictitious 10 Flood Chokepoints*: `docs/decision-openvino-vs-vlm.md` repeatedly references "10 known flood chokepoints" to justify daily VLM API quota calculations ($10\text{ cams} \times \text{calls} < 500\text{ RPD}$). Audit confirms **no list or fixture of 10 flood chokepoints exists in the repository**. The author conflated the 10 traffic smoke-test cameras with hypothetical flood points.
  2. *Rainfall Threshold Divergence*: The decision document specifies triggering VLM flood audits when precipitation exceeds $\ge 2.0\text{ mm/h}$. However, actual frontend code (`apps/web/src/utils/rainHexUtils.ts`) uses discrete thresholds: $1.0\text{ mm}$ (wet), $4.0\text{ mm}$ (moderate), and $15.0\text{ mm}$ (deep), while meteorological standards define moderate rain at $\ge 2.5\text{ mm/h}$. Furthermore, urban flooding in HCMC is dictated by 30–60 minute cumulative downpours ($\ge 30–50\text{ mm}$) and tidal river crests, not instantaneous hourly rates.

---

### Finding 4 — Infrastructure Trade-offs: `dev-box` Network Dependency & Operational Reality
- **The Audit**:
  - The offloading of CV inference to worker host `dev-box` (AMD Ryzen 7 6800HS, 8 vCPU, 8 GB RAM) via Tailscale (PR #203) delivered massive throughput gains (**24.01 FPS vs 2.27 FPS**, P50 dropped from 425ms to 18ms).
  - *Trade-off*: Colocated inference on `homesrv` had 0ms network latency and zero external dependency. `dev-box` introduces a **Network Single-Point-of-Failure (VPN SPOF)**. If the Tailscale mesh or residential internet fails, traffic telemetry drops.
  - *Code vs. Practice*: Go backend code implements graceful fallback (`callCtx` 10s timeout $\rightarrow$ `Verdict.SeverityUnknown` $\rightarrow$ `Source: "fallback"` $\rightarrow$ `CircuitBreaker`). However, **this failure mode has never been physically chaos-tested on live staging**.
  - *Hardware Class*: `dev-box` is an isolated KVM hypervisor on a personal workstation/mini-PC APU, not an enterprise cloud instance with 99.99% uptime guarantees.

---

### Finding 5 — Storage Evolution: Centralized Blob Storage (`file.529studio.site`) as Team Dataset Hub
- **The Audit & Rationale**:
  - In ADR-004 §D7, transitioning from local edge disk to remote blob storage was originally planned only when disk storage exceeded $>200\text{ GB}$ or $>1\text{M}$ files. Reviewers questioned whether introducing blob storage in Sprint 5 violates this deferral threshold given current low disk utilization.
  - However, the team has already successfully self-hosted and verified the S3-compatible blob storage infrastructure: **Dufs File Storage at `https://file.529studio.site`** (protected by Cloudflare Access), actively accumulating daily proof partitions (`2026-10-06` to `2026-10-09`).
  - *Strategic Paradigm Shift (Team Collaboration Over Pure Disk Savings)*: Offloading frames to blob storage is **not merely an emergency disk-saving tactic**—it removes the single biggest operational bottleneck in the model improvement cycle:
    1. Currently, labeling frames via `tools/eval/labeler.html` requires each developer or annotator to manually rsync gigabytes of JPEGs into their local `services/go-server/data/proofs/` directory. Annotations and proofs become fragmented across developer machines.
    2. By streaming all snapshots directly to `https://file.529studio.site`, the team can deploy a centralized, lightweight web labeling app / HTML viewer connected directly to the blob storage API.
    3. The entire team can collaboratively sample, inspect, and label ground truth frames remotely from any browser, generating a single, unified, version-controlled ground-truth dataset for continuous model evaluation and benchmarking.

---

## 3. Sprint 5 Core Principles

1. **Gate S5-T1 Blocks Everything**: Zero new feature implementation until the real D6 benchmark (T4) and smoke test (T7) are physically executed with calibrated PCU parameters on the 764 real frames.
2. **Sound Perception Over Parameter Hacking**: Do not attempt to mask object detector failures by repeatedly hacking capacity floors. Solve the root perception cause: scale model resolution to $416 \times 416$ to resolve distant motorcycle clusters.
3. **Decoupled Multimodal Integrity**: Implement the Option (c) Decoupled Hybrid Flood Analyzer with authentic HCMC municipal flood chokepoints (`flood_chokepoints.json`) and verified cumulative rain triggers.
4. **Centralized Data Hub**: Implement `storage.BlobStorage` for `file.529studio.site` to unlock centralized, collaborative team-wide labeling and audit access.
5. **Chaos-Tested Resilience**: Perform physical network partition drills on Staging (killing Tailscale on `dev-box`) to verify that the frontend and backend degrade gracefully without hangs.

---

## 4. Sprint 5 Backlog & Execution Sequence

```
┌────────────────────────────────────────────────────────────────────────────────────────┐
│                                SPRINT 5 EXECUTION WAVES                                │
└────────────────────────────────────────────────────────────────────────────────────────┘

  [WAVE 0: HARD GATE]
  S5-T1: Re-run Real D6 Benchmark & Smoke Test with Calibrated PCU (The Reckoning)
         │
         ▼
  [WAVE 1: PERCEPTION & DATASET FOUNDATIONS]
  S5-T2: Deep Object Detection Error Audit (Hallucination vs Occlusion Analysis)
  S5-T3: Upgrade Model Resolution to 416x416 OpenVINO IR on dev-box
  S5-T4: Centralized Blob Storage Driver (file.529studio.site) & Shared Web Labeler
         │
         ▼
  [WAVE 2: HYBRID MULTIMODAL & RESILIENCE]
  S5-T5: Decoupled Hybrid Flood Pipeline with Curated Flood Chokepoints
  S5-T6: Tailscale Network Partition Chaos Test & Staging ANALYZER=onnx Default
         │
         ▼
  [WAVE 3: EXPLORATORY ROUTE ENGINE (GATED)]
  S5-T7: Zero-Click Route Condition Explorer Spike (route.Engine Interface)
```

### Detailed Ticket Breakdown

| # | Ticket Title | Scope & Technical Implementation | Dependencies | Est. (Active) | Delivery Gate / Verification |
|---|---|---|---|:---:|---|
| **S5-T1** | **Re-run Real D6 Benchmark & Smoke Test with Calibrated PCU** | Execute `python3 tools/eval/benchmark.py --analyzer cv-engine` against all 764 real frames using calibrated PCU defaults (`CapacityFloor=15.0`, `Motorcycle=0.30`, `Bus=2.50`). Re-run `tools/eval/verify_smoke_set.py` within proper time windows. Commit `docs/reports/eval_report_d6_post_calibration.md` and `docs/reports/smoke_eval_post_calibration.md`. | None (**Hard Gate**) | **0.5 day** | **MANDATORY GATE**: Report real Macro F1 and Smoke Pass Rate. Blocks S5-T5 and S5-T7. |
| **S5-T2** | **Deep Object Detection Error Audit (CLEAR Hallucination vs CONGESTION Blindness)** | Analyze why 4/5 CLEAR cameras produced high vehicle counts in smoke testing. Inspect raw bounding boxes to determine if false detections stem from pavement markings, streetlights, or time-of-day mismatch. Analyze why CONGESTED cameras produced 0 vehicles. Document findings in `docs/reports/cv_detection_error_audit.md`. | S5-T1 | **0.5–1 day** | Root-cause diagnosis with visual bounding-box evidence across the 8 failed smoke cameras. |
| **S5-T3** | **Upgrade Model Resolution to 416x416 OpenVINO IR on `dev-box`** | Export stock YOLOv8n to OpenVINO IR FP16 at $416 \times 416$ resolution. Deploy to `dev-box` Stack 17. Benchmark sequential and concurrent latency (verifying $\text{P50} \le 35\text{ms}$ and throughput $\ge 20\text{ FPS}$). Re-evaluate small-motorcycle recall improvement on the 764 real frames. | S5-T1, S5-T2 | **1 day** | Latency benchmark report on `dev-box` + comparative recall metrics ($320\text{px}$ vs $416\text{px}$). |
| **S5-T4** | **Centralized Blob Storage Driver (`file.529studio.site`) & Collaborative Web Labeler** | Implement `storage.BlobStorage` implementing `storage.Storage` interface for `https://file.529studio.site` using Cloudflare Access Service Token authentication. Update `tools/eval/labeler.html` / build web viewer to fetch frames directly from blob storage, enabling team-wide remote labeling without local JPEG syncing. | None | **1 day** | Frames archived directly to remote blob storage; web labeler loads remote proofs seamlessly. |
| **S5-T5** | **Decoupled Hybrid Flood Pipeline with Curated `flood_chokepoints.json`** | Curate authentic municipal flood chokepoint camera fixture (`tools/eval/fixtures/flood_chokepoints.json`). Wire Go backend hotset scheduler to trigger low-frequency Gemini VLM flood audits on these cameras when Open-Meteo precipitation reaches $\ge 4.0\text{ mm}$ (moderate rain) or radar reflectivity $\ge 140$. Guard with strict 10 RPM / 300 RPD daily quota limiter. | S5-T1 | **1–1.5 days** | Live staging simulation showing VLM triggered on flood chokepoints during rain while vehicle detector handles traffic. |
| **S5-T6** | **Staging Infrastructure Hardening, Chaos Test & `ANALYZER=onnx` Git Default** | Update `docker-compose.stg.yml` to default `ANALYZER=${ANALYZER:-onnx}` in git. Execute physical network partition drill: take down Tailscale interface on `dev-box` and assert that staging backend degrades cleanly to `SeverityUnknown` within 10s without hanging or crashing. Add automated inference curl check in `docker-publish.yml`. | None | **0.5 day** | Video / terminal log proof of clean degrade during Tailscale drop attached to PR per Rule 4. |
| **S5-T7** | **Zero-Click Route Condition Explorer Architecture Spike (`route.Engine`)** | *Gated behind S5-T1.* Design the `route.Engine` interface per ADR-004 §2b/2c. Define spatial Corridor/H3 polygon path matching algorithm that traverses user routes and aggregates real-time PCU saturation and flood hazards. | S5-T1 (Macro F1 acceptable) | **1 day** | Architectural spike document in `docs/spike-route-engine.md` with interface definitions and unit tests. |

---

## 5. Quantitative Targets & Success Criteria for Sprint 5

| Metric / Gate | Baseline (Sprint 4 Reality) | Target for Sprint 5 Closure |
|---|:---:|:---:|
| **Traffic Macro F1 (Real D6 Dataset)** | **0.209** (T4 real benchmark) | **$\ge 0.50$** (calibrated) / **$\ge 0.65$** ($416\text{px}$) |
| **Known-State Smoke Test Pass Rate** | **20.0%** (2/10 pass) | **$\ge 70.0%$** ($\ge 7/10$ pass within valid time windows) |
| **Flood Safety False-Negative Rate (FNR)** | **100.0%** (0/120 detected) | **$\le 5.0%$** on active rain events via Hybrid VLM |
| **System Telemetry Throughput (`dev-box`)** | **24.01 FPS** (320px) | **$\ge 18.00\text{ FPS}$** ($416\text{px}$, under 2 vCPU limit) |
| **Staging Tailscale Resilience** | Untested in production | **100% Graceful Degrade to `unknown`** under partition |
| **Camera Proof Storage** | Local edge disk (`/data/proofs/`) | **Centralized Blob Storage** (`https://file.529studio.site`) |

---

## 6. Process Fix & Audit Compliance

1. **Mandatory Production Evidence**: Every pull request that claims model improvement or architectural resilience MUST attach real terminal logs or screenshots from Staging per Sprint 4 T8 rules. Passing unit tests alone will be rejected during review.
2. **No Speculative Parameter Hacking**: All PCU weights and capacity thresholds must be accompanied by empirical confusion matrix evidence from `tools/eval/benchmark.py`.
3. **Strict Zero Auto-Merge**: AI agents must never auto-merge pull requests targeting `dev` or `main`. All PRs require explicit peer code review.

---

*Certified and adopted by the Warded Engineering & Architecture Team for Sprint 5 execution.*
