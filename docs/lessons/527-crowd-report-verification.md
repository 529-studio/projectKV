# Lesson 527: Automated Crowd-Sourced Hazard Corroboration Against Nearby Camera Feeds

## Navigation Aid & Quick Reference

- **Domain**: Spatial proximity verification, Bayesian multi-sensor confidence fusion, monotonic state transitions, crowd-sourced ground truth corroboration, minimalist commuter UI badging.
- **Relevant Issues**: Issue #527 (S18-T4), Issue #249 (S7-T4), Issue #511 (S17-T2), Issue #526 (S18-T3), ADR-004, ADR-012, Lesson L45, Lesson L55, Lesson 526.
- **Key Files**:
  - `services/go-server/internal/reports/types.go` (ReportStatus enum, monotonic transition validator, HazardReport DTOs)
  - `services/go-server/internal/reports/spatial.go` (Haversine formula, 300m corroboration radius, 50m aggregation clustering)
  - `services/go-server/internal/reports/verifier.go` (Bayesian corroboration scoring engine, multi-signal weighting)
  - `services/go-server/internal/reports/service.go` (In-memory report lifecycle, sliding 90-minute TTL, feedback bridge)
  - `services/go-server/internal/reports/handler.go` (`POST /api/reports`, `GET /api/reports`, `GET /api/reports/:id`)
  - `services/go-server/cmd/server/adapters.go` (`reportsCameraLocatorAdapter`, `reportsVerdictReaderAdapter`, `reportsTideReaderAdapter`)
  - `apps/web/src/utils/floodUtils.ts` (`UserFloodReport` interface with corroboration fields)
  - `apps/web/src/hooks/useFlood.ts` (Parallel fetching and merging of `/api/reports`)
  - `apps/web/src/components/MapComponent.tsx` (Emerald verified chip in tooltip and marker styling)
  - `apps/web/src/components/CameraModal.tsx` (Bilingual camera-verified badge)
- **Core Invariants**:
  - **Monotonic Lifecycle Progression**: A crowd-sourced report begins in `SUBMITTED`. It can transition to `CORROBORATED` (nearby camera or multi-reporter + tide surge confirms water) or `UNVERIFIED` (nearby cameras verified dry). Once `CORROBORATED`, it cannot revert to `SUBMITTED` or `UNVERIFIED`. The terminal state is `EXPIRED` (sliding 90-minute TTL).
  - **Leaf Modulith Boundary**: Module `internal/reports` imports zero sibling internal packages (`camera`, `hotset`, `tide`, `feedback`). External dependencies are defined as interfaces in `deps.go` (`CameraLocator`, `VerdictReader`, `TideReader`, `Clock`) and adapted exclusively in `cmd/server/adapters.go`.
  - **Minimalist Commuter UI**: Commuters need instant clarity without screen clutter. Corroborated status is rendered as a clean, monochromatic emerald badge (`[Xác thực qua camera · {pct}%]`) in the map tooltip and camera modal header, strictly adhering to zero emoji and bilingual i18n (`vi`/`en`).

---

## 1. Context & Problem Statement

Crowd-sourced hazard reporting (such as citizen flood reports via `POST /api/v1/cameras/:id/report-flood`) provides rapid initial warning when street flooding begins. However, crowdsourced inputs suffer from two major operational challenges:
1. **False Positives & Stale Reports**: Pavement wetness, transient car splashes, or malicious/mistaken reports can mark a road segment flooded when water is already draining.
2. **Lack of Automated Corroboration**: Prior to Issue #527, citizen reports remained simple candidate flags without cross-referencing against physical sensors (computer vision verdicts from adjacent CCTV cameras or astronomical river tide surge telemetry).

To elevate crowd reports from unverified alerts into high-confidence situational awareness, the system required an automated spatial corroboration engine that searches for CCTV cameras within a 300-meter proximity radius, checks recent CV submersion levels, fuses river tide data, and outputs a Bayesian verification confidence score.

---

## 2. Root Cause & Architectural Design

### A. Monotonic Lifecycle State Machine (`internal/reports/types.go`)
Reports follow a monotonic state machine:
```
           ┌──────────────┐
           │  SUBMITTED   │
           └──────┬───────┘
            ┌─────┴─────┐
            ▼           ▼
     ┌──────────────┐ ┌──────────────┐
     │ CORROBORATED │ │  UNVERIFIED  │
     └──────┬───────┘ └──────┬───────┘
            │   ▲            │
            │   └────────────┘ (re-evaluates on new reports)
            ▼                ▼
     ┌─────────────────────────────┐
     │           EXPIRED           │ (terminal)
     └─────────────────────────────┘
```
`ValidateTransition(current, next)` strictly enforces:
- `SUBMITTED` $\rightarrow$ `CORROBORATED` | `UNVERIFIED` | `EXPIRED`
- `CORROBORATED` $\rightarrow$ `EXPIRED` (cannot downgrade to `UNVERIFIED`)
- `UNVERIFIED` $\rightarrow$ `CORROBORATED` | `EXPIRED` (can upgrade if additional reporters or camera updates arrive)
- `EXPIRED` is terminal (no transitions permitted)

### B. Spatial Proximity & Haversine Distance Engine (`internal/reports/spatial.go`)
Citizen reports can be submitted with either a known `camera_id` or GPS coordinates (`latitude`, `longitude`).
The spatial engine computes great-circle distance via Haversine formula:
$$d = 2 R \arcsin \left( \sqrt{\sin^2\left(\frac{\Delta \phi}{2}\right) + \cos \phi_1 \cos \phi_2 \sin^2\left(\frac{\Delta \lambda}{2}\right)} \right)$$
- Proximity radius threshold: $R_{\text{prox}} = 300\text{ meters}$.
- Incident deduplication/clustering: Reports within $50\text{ meters}$ of an active incident within 15 minutes are clustered to prevent artificial report inflation while incrementing distinct reporter counts (`report_count`).

### C. Bayesian Multi-Signal Corroboration Scoring (`internal/reports/verifier.go`)
Confidence score $C \in [0.0, 1.0]$ is computed as:
$$C = w_1 \cdot S_{\text{CV}} + w_2 \cdot S_{\text{Reports}} + w_3 \cdot S_{\text{Tide}}$$
Where:
- $w_1 = 0.45$: Weight of physical CCTV camera vision verdict.
- $w_2 = 0.35$: Weight of independent reporter consensus ($S_{\text{Reports}} = \min(1.0, 0.4 \cdot N_{\text{reporters}})$).
- $w_3 = 0.20$: Weight of hydrological river tide surge ($S_{\text{Tide}} = 1.0$ if tide $\ge 1.60\text{m}$, $0.7$ if $\ge 1.50\text{m}$, $0.3$ if $\ge 1.30\text{m}$).

**State Determination Rules**:
- `CORROBORATED`:
  - $C \ge 0.50$ AND nearby camera detects water ($S_{\text{CV}} \ge 0.50$), OR
  - Multi-reporter consensus ($S_{\text{Reports}} \ge 0.70$) AND high river tide surge ($S_{\text{Tide}} \ge 0.70$) with $C \ge 0.50$.
- `UNVERIFIED`: Nearby cameras within 300m are verified dry ($S_{\text{CV}} = 0.0$) and $C < 0.40$.
- `SUBMITTED`: Awaiting further observations or distant from active cameras.

### D. Modulith Dependency Inversion & Leaf Compliance
In strict compliance with `AGENTS.md` Rule 7 and `docs/GO_BACKEND_ARCHITECTURE.md`, `internal/reports` has zero sibling package dependencies.
Interfaces defined in `deps.go`:
```go
type CameraLocator interface {
    FindCamerasWithin(lat, lng, radiusMeters float64) []NearbyCamera
    GetCamera(id string) (NearbyCamera, bool)
}

type VerdictReader interface {
    GetLatestVerdict(cameraID string) (CameraVerdictInfo, bool)
}

type TideReader interface {
    PredictTide(station string, t time.Time) float64
}
```
Implemented in `cmd/server/adapters.go`:
- `reportsCameraLocatorAdapter` queries `camera.Catalog`.
- `reportsVerdictReaderAdapter` queries `hotset.Store`.
- `reportsTideReaderAdapter` queries `tide.PredictTide`.
- `initFeedbackHandler` bridges legacy citizen flood reports to `reportsService.OnCameraReport(...)`.

### E. Frontend Verified Badging & Commuter UX
On the client (`apps/web`):
- `UserFloodReport` DTO augmented with `status`, `confidence`, `corroborating_camera_id`.
- `useFlood.ts` fetches `/api/reports` in parallel with `/api/flood/user-reports` and merges corroboration status.
- `MapComponent.tsx` displays an emerald chip in the marker tooltip: `[Xác thực qua camera · 85%]`.
- `CameraModal.tsx` renders the camera verification indicator in the modal header and action bar.
- Monochromatic dark styling, bilingual `vi`/`en`, and zero emoji (Rule 1).

---

## 3. Empirical Verification & Evidence

### A. Unit Test Suite Execution
- **Backend Tests (`internal/reports`)**:
  - `TestHaversineDistance`: Precision verification against known coordinates.
  - `TestMonotonicStateTransitions`: Validates strict progression and rejects backwards transitions (`CORROBORATED` $\rightarrow$ `SUBMITTED`) and terminal escapes (`EXPIRED` $\rightarrow$ `CORROBORATED`).
  - `TestBayesianCorroborationScenarios`: Four scenarios validating camera water confirmation, multi-reporter tide surge, dry camera rejection, and distant camera submission.
  - `TestReportsService_LifecycleAndTTL`: Tests in-memory aggregation, IP hashing, deduplication, and sliding 90m TTL pruning.
  - `TestReportsHandler`: Validates HTTP JSON endpoints (`POST /reports`, `GET /reports`, `GET /reports/:id`).
  - Result: `ok github.com/529-studio/projectKV/services/go-server/internal/reports 2.369s` under `-race`.
- **Frontend Tests (`apps/web`)**:
  - `apps/web/src/components/HazardReports.test.ts`: 4 scenarios covering zero-emoji bilingual formatting, TTL calculations, emerald tooltip rendering, and unverified/expired state handling.
  - Result: 512 tests passed across 104 suites in 1.07s.

### B. Live End-to-End Self-Verification Telemetry
Executing live server binary `/tmp/server_test_bin` against real dataset `cameras_enriched.json`:
1. `GET /healthz` $\rightarrow$ 200 OK healthy.
2. `POST /api/reports` with `camera_id: "662b86c41afb9c00172dd31c"`:
   ```json
   {"id":"25544a999d24904e","camera_id":"662b86c41afb9c00172dd31c","latitude":10.79189,"longitude":106.69105,"hazard_type":"flood","description":"Ngập sâu giao lộ","status":"SUBMITTED","confidence":0.16,"report_count":1,"submitted_at":"2026-11-24T12:30:54.383772Z","expires_at":"2026-11-24T14:00:54.383772Z"}
   ```
3. `GET /api/reports` $\rightarrow$ returns active list with status and confidence.
4. `GET /api/reports/25544a999d24904e` $\rightarrow$ returns verified report object.
5. `POST /api/v1/cameras/662b86c41afb9c00172dd31c/report-flood` $\rightarrow$ legacy endpoint successfully bridges into reports engine, clustering and incrementing `report_count: 2`, recalculating confidence to `0.26`, and extending sliding TTL.

---

## 4. Key Takeaways & Architecture Principles

1. **Physical Grounding Prevents Crowd Hallucinations**: Crowd reports are invaluable for early warning, but fusing physical ground truth (CCTV CV detection + river tide levels) separates genuine roadway inundation from noise.
2. **Dependency Inversion Keeps Leaf Modulith Clean**: By defining consumer interfaces in `internal/reports/deps.go` and wiring in `cmd/server/adapters.go`, the reports engine remains isolated, decoupled, and testable with mock providers.
3. **Monotonicity Protects Citizen Trust**: In emergency interfaces, flip-flopping alert states confuse drivers. Enforcing monotonic progression ensures that corroborated hazards remain affirmed until their sliding TTL cleanly expires.
4. **Minimalism in UI Design**: A single subtle badge with percentage confidence gives commuters actionable information in under 2 seconds without visual noise or cognitive overload.
