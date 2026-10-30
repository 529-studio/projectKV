## Lesson 44: In-Memory Spatial Aggregator TTL Synchronization, Multi-Lens Marker Prominence, and Semantic Navigation Honesty

- **Incident & Tracking:** Issue #269 followup / Real-time Rain Verification on 2026-10-17 (`feat/s8-t1-radar-pipeline-overhaul`, PR #272).
- **Symptom:**
  1. **Cross-Component Semantic Desynchronization**: The left sidebar hero card displayed `"TOÀN THÀNH PHỐ: Các tuyến đường khô ráo — Không ghi nhận ngập"`, while the top lens toolbar displayed `"Camera ngập [8]"`.
  2. **Invisible Incident Locations**: Commuters could not identify which 8 cameras or road segments were affected because camera markers retained generic traffic green/orange/red styling, and `WET` polygons blended into the daylight OpenStreetMap basemap (`fillOpacity: 0.15`).
- **Root Cause:**
  1. **In-Memory Aggregator Staleness Leak**: In `services/go-server`, `/api/flood-status` dynamically checked `time.Since(obs) > 180s` and marked aged records as `stale: true`, which the frontend filtered out (`floodedCameraIds.size === 0`, `wetCameraCount === 0`). However, `FloodHexAggregator` (`/api/flood-hex`) stored verdicts in RAM without TTL checks or reaper routines, indefinitely serving 8 stale H3 cells.
  2. **Badge Semantic Conflation**: All 8 cameras had `flood_severity: "WET"` (light rain on asphalt, zero standing water). Frontend `HexOverlay` lumped `WET` together with `FLOODED_SHALLOW` and `FLOODED_DEEP` under a single red badge `[8]`, misleading users into believing 8 road segments were submerged.
  3. **Multi-Lens Marker Collision**: Markers rendered traffic colors regardless of active lens. When switching to `flood`, non-affected cameras were not dimmed and flood/wet cameras were not elevated, making them invisible in high-density camera clusters.
- **Mandatory Takeaways & Engineering Resolutions:**
  1. **Dual TTL Eviction in Stateful In-Memory Aggregators**:
     - Any companion in-memory aggregator (`FloodHexAggregator`) serving spatial polygons MUST mirror the TTL rules of endpoint handlers (`staleTTL = 180s`).
     - Added periodic background reaper (`ReapStaleCells()`) running every 30 seconds to prune aged camera observations, update cell aggregates, and broadcast SSE clearance events (`CameraCount: 0`, `FloodSeverity: DRY`).
  2. **Semantic Honesty in Navigation Badges**:
     - Decoupled badge rendering: Rose badge `[N]` reserved strictly for standing floods (`FLOODED_SHALLOW`, `FLOODED_DEEP`). Sky-blue badge `[N ướt]` used on desktop (`[N]` on mobile) when only wet pavement is detected. Badge is completely omitted when count is 0.
     - Sidebar hero card displays `GHI NHẬN MƯA: N điểm đường ướt (chưa ghi nhận ngập)` with direct action `Xem danh sách N camera đường ướt →`.
  3. **Contextual Marker Prominence & Dimming**:
     - When `activeLens === 'flood'`, unaffected cameras are dimmed (`opacity: 0.20`, `fillOpacity: 0.15`).
     - Cameras with active flood/wet verdicts are highlighted in high-contrast cyan (`#38bdf8` for WET) or crimson (`#dc2626` for DEEP) with thick white borders, enlarged radius, and elevated `zIndexOffset(1000)` so they immediately command visual attention.
