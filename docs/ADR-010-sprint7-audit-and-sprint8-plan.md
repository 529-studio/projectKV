# ADR-010: Sprint 7 Audit Reconciliation & Sprint 8 Plan — Doppler Radar Overhaul, Active Learning & Surface Segmentation

**Status**: Accepted  
**Date**: 2026-10-17  
**Authors**: ProjectKV Engineering & Architecture Team  
**Related**:
- [`docs/sprint7_report.md`](sprint7_report.md) (Sprint 7 Audited Completion & Staging Scorecard)
- [`docs/ADR-009-self-hosted-flood-segmentation-and-directional-pcu.md`](ADR-009-self-hosted-flood-segmentation-and-directional-pcu.md) (Autonomous Harvester Architecture)
- [`docs/ADR-008-sprint6-audit-and-sprint7-plan.md`](ADR-008-sprint6-audit-and-sprint7-plan.md) (Sprint 7 Roadmap)
- [`docs/lesson_learn.md`](lesson_learn.md) (Lessons 38 through 41)
- [`AGENTS.md`](../AGENTS.md) (Core Rules 1 through 9)
- Issue [#269](https://github.com/529-studio/projectKV/issues/269) (`[rfc + web] Doppler Rain Radar Pipeline Overhaul`)

---

## 1. Context & Retrospective Audit Reconciliation

### 1.1. Sprint 7 Accomplishments
Sprint 7 delivered substantial upgrades across telemetry, data ingestion, and computer vision foundations:
1. **S7-T1 & S7-T2 (Issue #247, PR #248)**: Filtered 125 offline cameras (`NOT_IMAGE`/`NO_SIGNAL`) by default with local persistence and clear commuter toggle.
2. **S7-T3 (Issue #249 / #250, PR #251)**: Implemented `[Báo cáo ngập tại đây]` modal button with 30s debouncing and feedback telemetry hooks.
3. **S7-T5 (Issue #251, PR #256)**: Deployed autonomous on-premise background poller in `services/go-server/internal/hotset/` sweeping 57 arterial Hot Set cameras every 30s.
4. **S7-T6 (Issue #251, PR #257)**: Dynamic arterial marker saturation coloring (Green: $SI < 0.30$, Yellow: $0.30 \le SI < 0.60$, Red: $SI \ge 0.60$) with pulsating animation.
5. **S7-T7b (Issue #262, PR #268)**: Autonomous dual-trigger harvester in `internal/harvester/` (commute sweeps at 07:30, 12:00, 17:30 ICT and meteorological rain triggers at $\ge 0.10\text{ mm/h}$) streaming frames to Cloudflare R2 (`warded-proofs`).
6. **S7-T8 (Issue #253, PR #258)**: Rebuilt labeling taxonomy into Draft-07 multi-lane directional schema (`inbound`, `outbound`) with physical waterline depth anchors, curating the initial 100-frame gold-standard corpus (`tools/eval/labels_gold_standard/`).

### 1.2. The Reality Check: False-Positive Radar Inundation Alarms
During live staging testing on `homesrv` Stack 14, maintainers and commuters noted an empirical defect:
- The web explorer painted vast dark-blue flood/rain polygons across downtown Ho Chi Minh City, with urgent tooltips warning commuters of "Ngập sâu lòng đường, nguy cơ chết máy cao".
- Live inspection of city streets and monitored cameras (e.g. *Võ Văn Kiệt - An Bình*) showed bone-dry asphalt and clear, sunny skies.
- Root-cause investigation (codified in Lesson 41) revealed that `mapRadarPixelToSeverity` in `rainHexUtils.ts` completely ignored RGB channels and evaluated only alpha transparency ($A \ge 200 \rightarrow \text{FLOODED\_DEEP}$). Because RainViewer Universal Blue pixels are opaque ($A=255$), 100% of echoes were classified as deep flooding. Furthermore, Zoom 7 bilinear smoothing blurred cloud edges across provinces, single-centroid sampling caused single-pixel noise to light up entire $5.16\text{ km}^2$ districts, and the page defaulted to the radar lens.

In accordance with user direction and AGENTS.md Rule 2 (Sound Solutions, Never Fear Effort), Issue #269 is prioritized as **Sprint 8 Priority 1 (S8-T1)** before addressing new backlog tickets.

---

## 2. Sprint 8 Backlog Prioritization

| Priority | Ticket | Scope & Architecture | Dependencies | Active Effort | Tracking |
|:---:|---|---|:---:|:---:|:---:|
| **P1** | **S8-T1** | `[web+go] Doppler Rain Radar Pipeline Overhaul (RGB dBZ, 7-Point Hex Sampling & Gate)` | None | 12h | [#269](https://github.com/529-studio/projectKV/issues/269) |
| **P2** | **S8-T2** | `[go-server] internal/feedback Leaf Package & Cloudflare R2 Crowd Ingestion Archiver` | S7-T3 | 10h | [#249](https://github.com/529-studio/projectKV/issues/249) |
| **P3** | **S8-T3** | `[web+tooling] Web Labeler Active Learning Queue UI & Standalone Python Harvester CLI` | S7-T7b | 10h | [#264](https://github.com/529-studio/projectKV/issues/264), [#263](https://github.com/529-studio/projectKV/issues/263) |
| **P4** | **S8-T4** | `[cv-engine] On-Premise Road Surface Water Semantic Segmentation Training Spike` | ADR-009 | 16h | [#254](https://github.com/529-studio/projectKV/issues/254) |
| **P5** | **S8-T5** | `[ci+ops] Live Staging Telegram Flood Alert Token Deployment (homesrv Stack 14)` | S6-T5 | 4h | [#246](https://github.com/529-studio/projectKV/issues/246) |

---

## 3. Key Architectural Decisions for Sprint 8

### Decision 1: Doppler Rain Radar Pipeline Overhaul (S8-T1)
- **Non-Interpolated Zoom 8 Raw Tiles**: In `weather.Service`, upgrade tile template from smoothed `/2/1_1.png` to raw `/2/0_0.png` at Zoom 8. Proved that all H3 Resolution 7 cells in greater HCMC fit entirely within exactly two Zoom 8 tiles: `(203, 120)` and `(204, 120)`.
- **RGB dBZ Reflectivity Decoding**: Decode RainViewer Scheme 2 RGB palette:
  - $< 18\text{ dBZ}$ (or saturation $< 0.20$, alpha $< 120$): `null` (suppress haze and virga).
  - $18–29\text{ dBZ}$ (cyan, vibrant teal): `LIGHT_RAIN`.
  - $30–39\text{ dBZ}$ (saturated yellow/amber or deep navy): `MODERATE_RAIN`.
  - $\ge 40\text{ dBZ}$ (orange, red, purple storm cores): `HEAVY_RAIN`.
- **7-Point Contraction Spatial Sampling**: Sample centroid + 6 outer vertices contracted to 70% radius. Require $\ge 3/7$ point quorum and median intensity ranking.
- **Surface Cross-Validation Gate**: Cross-reference airborne radar echoes against ground truth. If Open-Meteo measured rain is $< 0.2\text{ mm}$ with clear skies and cameras in the cell observe dry pavement, the echo aloft is classified as `ADVISORY_CLOUD` (slate gray, fillOpacity 0.04) or suppressed, preventing street-level flood false alarms.
- **Commuter-First Lens Default & Monochromatic Styling**: Default lens set to `'traffic'` (`apps/web/src/app/page.tsx` and `HexOverlay.tsx`). Translucent atmospheric radar styling (`#38bdf8`, `#3b82f6`, `#6366f1` with dashed borders). Zero emojis (`⚠`, `✓`) across all tooltips per AGENTS.md Rule 1.

### Decision 2: Active Crowdsource Ingestion Archiver (S8-T2)
- Implement `services/go-server/internal/feedback/` as a pure leaf modulith package exposing `POST /api/v1/cameras/:id/report-flood`.
- Enforce IP-based rate limiting (1 submission per IP per camera per 5 minutes) returning HTTP 429.
- Stream contested frames and JSON metadata to Cloudflare R2 (`warded-proofs/feedback/`) with `trust: unverified`.

### Decision 3: Active Learning Labeler Queue & Standalone Harvester (S8-T3)
- Expand `/labeler` web UI to consume candidate frames directly from Cloudflare R2 storage.
- Implement standalone Python CLI (`tools/eval/harvester_cli.py`) for offline harvesting without starting the full Go daemon.

### Decision 4: Road Surface Water Semantic Segmentation Spike (S8-T4)
- Train lightweight segmentation model (e.g. SegFormer-B0 or Fast-SCNN) on curated road puddle/waterline masks to overcome YOLOv8n vehicle-only detection limitations (ADR-009).

### Decision 5: Live Staging Alert Verification (S8-T5)
- Deploy genuine Telegram bot tokens and chat IDs to `homesrv` Stack 14 environment to complete end-to-end verification of `internal/notify`.
