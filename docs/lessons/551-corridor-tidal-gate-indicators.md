# Lesson 551: Localized Tidal Sluice Gate Status Indicators and Pump Telemetry

## Navigation Aid & Quick Reference
- **Domain**: Arterial corridor flood analytics, hydraulic infrastructure telemetry, tidal sluice gate state modeling, frontend operational badges.
- **Relevant Issues**: Issue #551 (S19-T3), Issue #526 (Corridor Historical Flood Depth & Tidal Harmonics), Issue #476 (Corridor Route Overview).
- **Key Files**:
  - `services/go-server/internal/corridor/routes.go` (`TidalGate`, `TidalGateStatus`, `CorridorTidalGateMapping`, extended `CorridorRouteSummary`)
  - `services/go-server/internal/corridor/corridor.go` (`evaluateTidalGates` resolving real-time gate states from downstream astronomical tide predictions)
  - `services/go-server/internal/corridor/corridor_test.go` (`TestTidalGatesEvaluation` verifying `CLOSED_PUMPING` and `OPEN_GRAVITY` transitions)
  - `apps/web/src/types/corridor.ts` (`TidalGate`, `TidalGateStatus`)
  - `apps/web/src/components/CorridorOverviewCard.tsx` (Monochromatic sluice gate & pumping station indicators)
  - `apps/web/src/components/CorridorRouteAndCard.test.ts` (Scenario 8 validating zero-emoji compliance and bilingual rendering)
- **Core Invariants**:
  - **Hydraulic Threshold Mapping**: When downstream river tide predictions at Phu An or Nha Be reach or exceed $1.40\text{ m}$, mapped tidal sluice gates transition to `CLOSED_PUMPING` with active forced drainage (`pump_active = true`, upstream canal level maintained at $0.75\text{ m}$). Below $1.40\text{ m}$, gates revert to `OPEN_GRAVITY`.
  - **Zero-Emoji Monochromatic Contract**: Follows AGENTS.md Rule 1 with strictly zero emoji and clean zinc/cyan/amber borders on corridor route cards.

---

## 1. Context & User Experience Gap

In Ho Chi Minh City, street inundation along arterial corridors depends not only on river tide peaks and rainfall intensity, but critically on whether regional tidal control sluice gates (such as Nhiêu Lộc - Thị Nghè, Bến Nghé, Tân Thuận, Mương Chuối, Cây Khô, and Phú Xuân) are closed and pumping or open for gravity discharge. Previously, commuters viewing high tide forecasts lacked immediate visibility into whether floodgates protecting their route were actively holding back water.

---

## 2. Implementation Summary

### A. Backend Sluice Gate Modeling (`internal/corridor`)
- Mapped canonical HCMC arterial corridors (`nguyen_huu_canh`, `dien_bien_phu`, `vo_van_kiet`, `huynh_tan_phat`, `quoc_lo_50`, `mai_chi_tho`) to their governing flood control gates in `CorridorTidalGateMapping`.
- `evaluateTidalGates` queries `s.tidePredictor.PredictTide(station, now)` and populates `TidalGates []TidalGate` on `CorridorRouteSummary` and GeoJSON properties.
- Regenerated Swagger API documentation (`make swagger`) to expose `corridor.TidalGate` schemas.

### B. Corridor Card Visualization (`CorridorOverviewCard.tsx`)
- Added a clean telemetry block showing each protecting sluice gate's name, operational state (`ĐÓNG NGĂN TRIỀU & BƠM` / `CLOSED & PUMPING`, `MỞ XẢ TỰ NHIÊN` / `OPEN (GRAVITY)`), river vs. canal water levels in meters, and active pump status.

---

## 3. Verification & Empirical Proof

1. **Go Backend Verification**:
   - `go test -race -v ./internal/corridor/... -run TestTidalGatesEvaluation`: Passed.
   - `make swagger`: Regenerated `docs.go`, `swagger.json`, and `swagger.yaml` cleanly.
2. **Web Frontend Verification**:
   - `npm test` (`apps/web`): 516/516 tests passing (including Scenario 8).
   - `npm run build` (`apps/web`): Production build succeeded with zero type or lint errors.
