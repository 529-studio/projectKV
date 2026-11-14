# Lesson 454: Adaptive Macro Clustering, Context-Aware Smart Lens, and UI Clutter Elimination

## Navigation Aid & Quick Reference
- **Domain**: Map clustering ergonomics, zoom-tiered macro aggregation, smart context-aware default lens, UI clutter elimination.
- **Relevant Issues**: Issue #454, Issue #452, Issue #144, ADR-004.
- **Key Files**:
  - `apps/web/src/utils/mapClustering.ts` (`getAdaptiveGridPx`, `CLUSTER_COLLISION_PADDING_PX`, `getClusterHazardVisual`)
  - `apps/web/src/app/page.tsx` (Context-aware lens auto-selection, user override persistence)
  - `apps/web/src/components/PinnedDock.tsx` (Elimination of hardcoded brackets in counter badge)
  - `apps/web/src/config/constants.ts` (`PINNED_DOCK_I18N.CLEAR_ALL` copy update)
- **Core Invariants**:
  - **Zoom-Tiered Adaptive Grids**: Rather than a fixed pixel grid (e.g. 64px or 80px), grid cell size scales dynamically with zoom level (`<= 10`: 180px, `<= 11`: 150px, `<= 12`: 105px, `<= 13`: 70px, `>= 14`: unclustered).
  - **Macro Regional Aggregation**: At city-wide zoom 11, cameras aggregate into 5–10 regional metropolitan sectors rather than 35+ micro-clusters, preserving cartographic readability and preventing overlay competition with H3 hexagons.
  - **Context-Aware Default Lens**: On initial app load without explicit user preference, the app defaults to `traffic` (the dominant daily commuter use case); it automatically elevates `radar` if active rainfall (`rain_mm > 0.2` or `precipitation_probability_percent >= 60`) or significant flood hazard (level 2/3) is detected.
  - **Clean Production Ergonomics**: Hardcoded developer debug brackets (`[Đang ghim ...]`, `[XÓA HẾT]`) are replaced with clean, localized typographic labels conforming to Rule 1.

---

## 1. Context & Problem Statement

Following the collision deconfliction improvements in Issue #452, user testing on staging (`http://localhost:3001` and live environments) revealed two remaining visual clutter and usability concerns:

1. **Micro-Cluster Clutter at City Overview (Zoom 11)**:
   A uniform 80px clustering grid caused peripheral districts (Củ Chi, Bình Chánh, Cần Giờ, Hóc Môn) to generate dozens of 2-to-4 camera micro-clusters scattered across the map. These badges competed visually with the colored traffic hexagon layer and road cartography, creating visual noise without providing actionable high-level density insights.
2. **Ambiguity in Default Lens Selection**:
   When opening the application for the first time, users were uncertain whether the map should display rain radar or traffic saturation. On clear days, showing a dormant radar layer provided little commuter value, whereas on stormy days, defaulting to traffic risked missing critical flood flashpoints.
3. **Developer Debug Syntax in UI**:
   The Pinned Camera Dock displayed bracket-wrapped strings such as `[Đang ghim 5 / Tối đa 12 camera]` and `[XÓA HẾT]`, resembling unfinished debug placeholders rather than polished production copy.

---

## 2. Root Cause Analysis

### A. Fixed Pixel Spacing Across Spatial Scales
In Leaflet clustering, a static grid distance (e.g. 80px) represents dramatically different geographic areas depending on zoom level:
- At zoom 14, 80px corresponds to ~300 meters (street level).
- At zoom 11, 80px corresponds to ~2.5 kilometers.
Because camera density in peripheral regions is sparse (cameras spaced 2–5 km apart along outer ring roads), an 80px grid left almost every peripheral camera as an isolated 2-to-3 badge cluster. To form meaningful macro hubs, the aggregation window at city zoom must expand to 150px (~5–6 km).

### B. Rigid Static Default vs Dynamic Commuter Intent
Previous implementations either statically defaulted to `radar` or `traffic`. Commuter intent varies fundamentally with weather conditions:
- Normal/Dry: Commuters want arterial traffic speed and congestion levels.
- Rain/Storm: Commuters urgently need flood risk and radar precipitation cells.

---

## 3. Implementation Details

### A. Dynamic Adaptive Grid Sizing (`mapClustering.ts`)
```typescript
export function getAdaptiveGridPx(zoom: number, customGridPx?: number): number {
  if (customGridPx != null && customGridPx > 0) return customGridPx;
  if (zoom <= 10) return 180;
  if (zoom <= 11) return 150;
  if (zoom <= 12) return 105;
  if (zoom <= 13) return 70;
  return 60;
}
```
In `clusterCameras`:
```typescript
const effectiveGridPx = getAdaptiveGridPx(zoom, gridPx);
const cellX = Math.floor(pixel.x / effectiveGridPx);
const cellY = Math.floor(pixel.y / effectiveGridPx);
```
Furthermore, `CLUSTER_COLLISION_PADDING_PX` was expanded to 12px, and cluster halo styling was toned down for normal (clear) status (`bg-zinc-900/80 backdrop-blur-md text-zinc-200 border-zinc-700/70`), reserving eye-catching warning halos for hazard levels 2 and 3.

### B. Context-Aware Lens Initialization (`page.tsx`)
```typescript
useEffect(() => {
  if (hasUserSelectedLensRef.current) return;
  const isRaining = Boolean(
    weather && (weather.rain_mm > 0.2 || weather.precipitation_probability_percent >= 60),
  );
  const hasActiveFlooding = Array.from(floodVerdicts.values()).some(
    (v) => v && (v.flood_hazard_level === 2 || v.flood_hazard_level === 3),
  );

  if (isRaining || hasActiveFlooding) {
    setActiveLens('radar');
  } else {
    setActiveLens('traffic');
  }
}, [weather, floodVerdicts]);
```
User overrides via manual lens toggle are permanently recorded in `localStorage` (`warded_active_lens`) and guarded by `hasUserSelectedLensRef`, ensuring that the automatic switch never overrides explicit citizen intent.

### C. Clean UI Copy
- `PINNED_DOCK_I18N.CLEAR_ALL`: `vi: 'Xóa tất cả'`, `en: 'Clear all'`.
- `PinnedDock.tsx`: Removed `[` and `]` wrapping around `pinnedCameraIds.length` counter.

---

## 4. Verification & Empirical Proof

1. **Unit Test Suite**:
   `npm test --prefix apps/web` executed 406 test cases across 81 suites with 100% pass rate. `mapClustering.test.ts` verified adaptive grid sizing across zoom tiers 10, 11, 12, 13 and custom overrides.
2. **Playwright E2E Screenshot Verification**:
   - `docs/proofs/s14_ui_ux/macro_clustering_city_zoom.png`: Captured city overview at 1440x900 viewport. Total cluster markers dropped from 35+ scattered circles to 17 clean macro regional hubs (centering around a 365-camera metropolitan core).
   - `docs/proofs/s14_ui_ux/macro_clustering_with_dock.png`: Verified Pinned Dock counter rendered cleanly as `Đang ghim 2 / Tối đa 12 camera` and action button as `Xóa tất cả`.
3. **Docs & Lint Ratchet**:
   `python3 tools/docs/docs_lint.py` confirmed 0 new violations and strict compliance with the 1 MiB asset size budget.
