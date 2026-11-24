# Implementation Plan: Automated Crowd-Sourced Hazard Report Verification Against Nearby Camera Feeds

- **Issue**: [#527](https://github.com/529-studio/projectKV/issues/527) (Sprint 18 Task 4)
- **Branch**: `feat/527-crowd-report-verification`
- **Specification**: `docs/superpowers/specs/2026-11-24-crowd-report-verification-spec.md`

---

## Tasks

### Task 1: Core Domain & State Machine (`services/go-server/internal/reports`)
- Create `services/go-server/internal/reports/types.go`:
  - `ReportStatus` enum: `SUBMITTED`, `CORROBORATED`, `UNVERIFIED`, `EXPIRED`.
  - State transition validator `ValidateTransition(current, next ReportStatus) error`.
  - DTOs: `HazardReport`, `NearbyCamera`, `VerificationResult`.
- Create `services/go-server/internal/reports/deps.go`:
  - Interfaces: `CameraLocator`, `VerdictReader`, `TideReader`.
- Create `services/go-server/internal/reports/spatial.go`:
  - Haversine distance calculator in meters between two lat/lng coordinates.
- Unit tests: `services/go-server/internal/reports/reports_test.go` testing state transitions and haversine calculations.

### Task 2: Bayesian Corroboration Engine (`services/go-server/internal/reports/verifier.go`)
- Implement `Verifier`:
  - `Corroborate(ctx context.Context, report HazardReport) VerificationResult`
  - Fetch cameras within 300m radius.
  - Query CV verdicts for nearby cameras.
  - Calculate Bayesian score: $C = 0.50 \cdot \text{CV} + 0.30 \cdot \text{Reports} + 0.20 \cdot \text{Tide}$.
  - Determine next state: `CORROBORATED` if $C \ge 0.55$, `UNVERIFIED` if camera dry & $C < 0.40$, else `SUBMITTED`.
- Unit tests covering edge cases: no cameras nearby, camera confirms deep flood, camera confirms dry, tidal surge correlation.

### Task 3: Service & HTTP Handlers (`services/go-server/internal/reports`)
- Implement `Service`:
  - Thread-safe in-memory cache with periodic expiration reaper (90-minute TTL).
  - `SubmitReport(req SubmitReportRequest) (*HazardReport, error)`
  - `GetActiveReports() []HazardReport`
  - Bridge callback to receive camera reports from `feedback.Handler`.
- Implement `Handler`:
  - `POST /api/reports`: Submits hazard report with lat/lng or camera ID.
  - `GET /api/reports`: Returns all active corroborated and submitted reports.
  - Integration with `/api/flood/user-reports` so existing consumers receive verified flags.

### Task 4: Server Wiring & Composition Root (`cmd/server`)
- In `cmd/server/adapters.go`:
  - Implement `reportsCameraLocatorAdapter` satisfying `reports.CameraLocator`.
  - Implement `reportsVerdictReaderAdapter` satisfying `reports.VerdictReader`.
  - Implement `reportsTideReaderAdapter` satisfying `reports.TideReader`.
  - Bridge `feedback.Handler.SetOnReportAccepted` to `reportsService.OnCameraReport(...)`.
- In `cmd/server/main.go` and `cmd/server/routes.go`:
  - Initialize `reports.Service` and `reports.Handler`.
  - Mount `/api/reports` routes.
  - Run `make swagger`.

### Task 5: Web UI Verified Badge & Tooltip (`apps/web`)
- Update `apps/web/src/types/reports.ts` or `useFlood.ts` to include `verified`, `status`, `confidence`.
- Update `MapComponent.tsx` tooltip & marker visual:
  - When report is `CORROBORATED`, display emerald/cyan chip `[Xác thực qua camera · {confidence}%]`.
- Update `CameraModal.tsx`:
  - Display camera corroboration status and remaining valid minutes.
- Add unit tests in `apps/web/src/components/HazardReports.test.ts`.

### Task 6: Documentation, Lesson & PR
- Create `docs/lessons/527-crowd-report-verification.md`.
- Verify `make docs-lint`, `make test-go`, `cd apps/web && npm test`.
- Open PR targeting `dev` with `Close #527`.
- Monitor CI until 100% green.
