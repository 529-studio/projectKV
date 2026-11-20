# Lesson 507: Decouple Card Dismissal from Map Viewport and Filter State

## Navigation Aid & Quick Reference

- **Domain**: Web UX, Leaflet map viewport choreography, filter state management, corridor route visual layering.
- **Relevant Issues**: Issue #507, PR #508 (corridor geometry fix), AGENTS.md Rule 2 (sound solutions), Rule 10 (empirical manual verification).
- **Key Files**:
  - `apps/web/src/app/page.tsx` (state decoupling between card dismissal and active corridor filter)
  - `apps/web/src/components/MapComponent.tsx` (removal of rogue `isFiltered` transition effect; `filterExtraControl` slot)
  - `apps/web/src/components/CorridorOverviewCard.tsx` (independent `showPolyline` toggle with $\ge 48\text{px}$ touch target)
  - `apps/web/src/components/CorridorRouteLayer.tsx` (polyline rendering gated by `showPolyline` while keeping checkpoint camera markers interactive)
- **Core Invariants**:
  - **Dismissing a contextual card must never reset the user's viewport.** Hitting `✕` on a floating card dismisses that card overlay only; the user's spatial focus on the roadway must remain locked.
  - **Recenter to default city-wide network view must be strictly explicit.** `map.flyTo(DEFAULT_CENTER, DEFAULT_ZOOM)` triggers only when the user deliberately clears the filter (`✕ Xóa lọc`, Esc, or unselecting the road from the list).
  - **Corridor route polyline and checkpoint camera pins are independently valuable.** Users examining neighboring side streets need the route line out of the way without losing camera pins or corridor filter focus.

---

## 1. Context & Problem Statement

In Sprint 15 & 16, commuter arterial corridors introduced `CorridorOverviewCard` and `CorridorRouteLayer`. When a commuter clicked an arterial road (e.g., "Đường Nguyễn Hữu Cảnh"), the map flew to the corridor bounds and rendered a high-contrast glowing polyline with camera checkpoint pins.

However, users experienced two major UX pain points:
1. **Accidental Zoom Reset**: When the user wanted to see nearby streets around Nguyễn Hữu Cảnh without the floating overview card blocking the screen, clicking `✕` on the card unexpectedly reset the map viewport all the way back to the city-wide network view (`DEFAULT_CENTER`, `DEFAULT_ZOOM`). The user immediately lost spatial context and had to manually re-pan and re-zoom.
2. **Missing Route Line Toggle**: When zoomed in on Nguyễn Hữu Cảnh, the glowing polyline overlaid the road centerline, preventing the user from inspecting micro street details or side alleys. There was no way to hide the polyline while keeping camera checkpoint pins active.

---

## 2. Root Cause Analysis

### Accidental Recenter
In `MapComponent.tsx`, an automated `useEffect` listened to `isFiltered`:
```tsx
useEffect(() => {
  if (prevFilteredRef.current && !isFiltered && mapRef.current) {
    mapRef.current.flyTo(MAP_CONFIG.DEFAULT_CENTER as L.LatLngTuple, MAP_CONFIG.DEFAULT_ZOOM, { duration: 0.8 });
  }
  prevFilteredRef.current = isFiltered;
}, [isFiltered]);
```
In `page.tsx`, the card's `onClose` handler was bound to `setSelectedCorridor(null)`. Because `isFiltered = Boolean(selectedPhuong || selectedCorridor || isFloodFilterActive)`, dismissing the card transitioned `isFiltered` from `true` to `false`, immediately triggering `map.flyTo(DEFAULT_CENTER, DEFAULT_ZOOM)`. Card dismissal was incorrectly conflated with global filter clearing.

### Hardcoded Polyline Layering
`CorridorRouteLayer.tsx` always added both the glow line and core solid line to the Leaflet `layerGroup` whenever `routeData` was present, offering no prop or UI toggle to suppress line rendering while retaining camera markers.

---

## 3. Architecture & Implementation

### A. Decoupled State Management (`page.tsx`)
We introduced two orthogonal pieces of state:
- `isCorridorCardDismissed`: tracks whether the floating card is hidden by the user.
- `showCorridorRoute`: tracks whether the green glowing street polyline is rendered.

```tsx
// Corridor display state
const [showCorridorRoute, setShowCorridorRoute] = useState(true);
const [isCorridorCardDismissed, setIsCorridorCardDismissed] = useState(false);
```

When selecting a corridor, `setIsCorridorCardDismissed(false)` and `setShowCorridorRoute(true)` restore both. When clicking `✕` on the card, `setIsCorridorCardDismissed(true)` hides the card while leaving `selectedCorridor` active and the viewport intact.

Explicit filter clearance (`handleFilterClear` via `✕ Xóa lọc` or `Esc`) cleanly resets all filter state and initiates `map.flyTo(MAP_CONFIG.DEFAULT_CENTER, MAP_CONFIG.DEFAULT_ZOOM, { duration: 0.8 })`.

### B. Persistent Top Badge Controls (`MapComponent.tsx` & `filterExtraControl`)
When the card is dismissed, the top status badge (`● Đường Nguyễn Hữu Cảnh`) remains visible and exposes:
- `[Tắt line] / [Bật line]`: toggles route line visibility at any time.
- `[Xem thẻ]`: restores the floating card without having to re-select the corridor from the sidebar.
- `[✕ Xóa lọc]`: clears the corridor filter and smoothly returns to city center view.

### C. Gated Route Rendering (`CorridorRouteLayer.tsx`)
`CorridorRouteLayer` accepts `showPolyline?: boolean` (default `true`). When `false`, the LineString glow and core polylines are not added to `layerGroup`, but initial viewport bounds are still calculated and camera pins remain interactive with tooltips and click handlers.

---

## 4. Verification & Empirical Proof

1. **Automated Unit Tests** (`apps/web/src/components/CorridorRouteAndCard.test.ts`):
   - Scenario 6 asserts bilingual toggle labels (`Tắt đường kẻ lộ trình` / `Hide route line`, `Bật đường kẻ lộ trình` / `Show route line`).
   - Scenario 7 simulates user workflow: card dismissal leaves `selectedCorridor` active and asserts `mapResetCount == 0`, while explicit filter clear resets the corridor and triggers `mapResetCount == 1`.
2. **Playwright End-to-End Visual Verification** (`docs/proofs/507-corridor-close-toggle/`):
   - `01_corridor_card_and_line_visible.png`: initial corridor focus with card and polyline.
   - `02_polyline_toggled_off.png`: polyline hidden, camera pins intact.
   - `03_card_dismissed_focus_retained.png`: card dismissed via `✕`, camera position strictly retained on Nguyễn Hữu Cảnh.
   - `04_line_toggled_off_no_card.png`: line toggled off directly from map badge while card dismissed.
   - `05_card_restored.png`: card restored via `[Xem thẻ]` button.
   - `06_explicit_filter_clear_recenter.png`: filter cleared via `[✕ Xóa lọc]`, map smoothly recenters to city center view.
3. **Quality Gates**:
   - `npx tsc --noEmit` and `npm test`: 448 tests pass (100% green).
   - `tools/ci/check_evidence_integrity.py`: 105 artifacts PASS.
   - `tools/docs/docs_lint.py`: 0 new violations.
