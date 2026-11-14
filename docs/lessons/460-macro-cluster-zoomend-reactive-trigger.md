# Lesson 460: Leaflet Zoom Lifecycle, Non-Binary Clustering Regimes, and Instant Drill-Down Responsiveness

## Navigation Aid & Quick Reference
- **Domain**: Leaflet lifecycle, Map clustering, React state synchronization, UX responsiveness, Touch targets.
- **Relevant Issues**: Issue #460, Issue #454 (S14 adaptive clustering), Issue #443 (S14-T3 commuter clustering).
- **Key Files**:
  - `apps/web/src/components/MapComponent.tsx` (Zoomend lifecycle, cluster badge interactions, marker rebuilding)
  - `apps/web/src/utils/mapClustering.ts` (Adaptive grid resolution, Mercator projections, deconfliction)
  - `apps/web/src/utils/mapClustering.test.ts` (Adaptive clustering unit test assertions)
- **Core Invariants**:
  - **Clustering is Multiscale, Not Binary**: Map clustering is not a boolean toggle (`zoom < 14` vs `zoom >= 14`). Between overview zoom (10) and threshold zoom (14), every integer zoom step produces distinct spatial partitions and cluster counts.
  - **Zoomend State Synchronization**: Any zoom transition within the clustered regime MUST trigger marker rebuilding on `zoomend` to guarantee sub-clusters appear immediately ($0\text{ms}$) upon animation completion without awaiting external asynchronous events (e.g. SSE ticks).
  - **Smart Zoom Leaps on Drill-Down**: Clicking a macro aggregate cluster ($\ge 20$ cameras) must calculate an expansion step of at least $+2$ zoom levels to guarantee the cluster fractures cleanly rather than remaining grouped.

---

## 1. Context & Problem Statement

In Sprint 14, macro clustering and collision deconfliction were introduced (Issue #443, #452, #454). At city zoom (e.g. zoom 10), downtown cameras aggregated into a macro badge (e.g. count `201`).

When a commuter tapped this `201` badge:
1. The map smoothly zoomed from zoom 10 to zoom 12 via `flyToBounds`.
2. Upon landing at zoom 12, the single `201` badge remained centered on screen with an active blue focus ring.
3. No sub-clusters appeared. The interface appeared frozen, leaving users confused about next steps.
4. Approximately 3–5 seconds later, the single badge suddenly vanished and was replaced by 60+ smaller sub-clusters.

---

## 2. Root Cause Analysis

### A. The Non-Binary State Illusion in `zoomend`
In `MapComponent.tsx`:
```typescript
map.on('zoomend', () => {
  const currentZoom = map.getZoom();

  const nextClustered =
    shouldClusterMarkers(currentZoom, activeLensRef.current === 'flood') &&
    !isFilteredRef.current;
  if (clusteredRef.current !== nextClustered) {
    clusteredRef.current = nextClustered;
    setMarkerEpoch((epoch) => epoch + 1);
  }
...
```
The developer assumed clustering was binary:
- `clustered` ($Z < 14$)
- `unclustered` ($Z \ge 14$)

When moving from zoom 10 to zoom 12:
- At $Z=10$: `clusteredRef.current = true`.
- At $Z=12$: `nextClustered = true` ($12 < 14$).
- `clusteredRef.current !== nextClustered` evaluated to `false`.
- `setMarkerEpoch` was **never called**.
- React did not re-render, and `buildCameraClusters` was **never invoked**.
- Leaflet simply repositioned the DOM element of badge `201` at its centroid coordinates.

### B. Delayed Fracture from Unrelated SSE Ticks
The marker `useEffect` declared dependencies on `[verdicts, floodVerdicts, cameras, markerEpoch]`.
The 3–5 second delay experienced by users was the latency of the backend SSE / Hot Set refresh cycle. When a random verdict packet arrived from the server, React re-rendered the component. Only then did `MapComponent` read the new zoom level (12), calculate the sub-clusters, and replace the marker layer.

### C. Static Grid Parameter Override
`MapComponent.tsx` was passing `gridPx: MAP_CONFIG.CLUSTER_GRID_PX` (64) directly into `buildCameraClusters`, which overrode the dynamic zoom-based resolution in `getAdaptiveGridPx(zoom)` ($180\text{px}$ at $z=10$, $150\text{px}$ at $z=11$, $105\text{px}$ at $z=12$, $70\text{px}$ at $z=13$).

---

## 3. Implementation Details

1. **Multiscale Zoom Tracking with `lastRenderedZoomRef`**:
   Added `lastRenderedZoomRef` to track the rendered integer zoom step. In `zoomend`:
   ```typescript
   const roundedZoom = Math.round(currentZoom);
   const nextClustered =
     shouldClusterMarkers(currentZoom, activeLensRef.current === 'flood') &&
     !isFilteredRef.current;
   const regimeChanged = clusteredRef.current !== nextClustered;
   const zoomStepChanged = nextClustered && lastRenderedZoomRef.current !== roundedZoom;

   if (regimeChanged || zoomStepChanged) {
     clusteredRef.current = nextClustered;
     lastRenderedZoomRef.current = roundedZoom;
     setMarkerEpoch((epoch) => epoch + 1);
   }
   ```
   When unclustered ($Z \ge 14$), Leaflet continues to resize existing circle markers in-place via Canvas / CSS without rebuilding the DOM. When clustered ($Z < 14$), every zoom change triggers an immediate cluster layer rebuild.

2. **Instant Tactile Click Feedback & Smart Zoom Leap**:
   - Cluster badges now feature `cursor-pointer transition-all duration-150 hover:scale-105 active:scale-90`.
   - On click, `clusterMarker.getElement()` immediately receives `scale-90 opacity-75` so the user receives instant tactile feedback.
   - For macro clusters ($\ge 20$ cameras), `targetZoom` enforces a minimum $+2$ zoom leap (`Math.max(currentZoom + 2, boundsZoom)`), preventing shallow zooms that fail to fracture dense clusters.

3. **Adaptive Resolution Restoration**:
   Removed static `gridPx: MAP_CONFIG.CLUSTER_GRID_PX` from `MapComponent.tsx` and removed the hardcoded default in `buildCameraClusters`, allowing `getAdaptiveGridPx(zoom)` to scale grid resolution inversely with zoom.

---

## 4. Verification & Empirical Proof

- **Empirical Playwright Benchmark**:
  - Time to fracture on cluster click: reduced from **~4,000 ms** to **603 ms** (immediately upon 500 ms `flyTo` completion).
  - Cluster count transition: `13` macro clusters $\to$ `66` neighborhood clusters $\to$ `0` clusters (full Canvas individual cameras at $Z \ge 14$).
  - Zoom-out transition: 4 zoom-out steps instantly restored the 13 macro clusters with zero lag.
- **Unit & System Tests**:
  - All 40 `mapClustering.test.ts` assertions pass.
  - All 408 web test suites pass.
  - All 27 Go packages pass with `-race`.
- **Visual Evidence**:
  - Sub-cluster split evidence: `docs/proofs/s14_ui_ux/macro_cluster_zoom_instant_split.png`
  - Street level evidence: `docs/proofs/s14_ui_ux/macro_cluster_zoom_street_level.png`
