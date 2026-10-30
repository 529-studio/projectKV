## Lesson 50: Waterway & Bridge Footing Sensor Hygiene (Exclusion of Non-Road Hydrological Cameras)

- **Incident & Tracking:** Issue #283, Kanban **[KV-15]** (`fix/hide-waterway-bridge-cameras`, PR targeting `dev`).
- **Symptom:**
  1. Staging user identified camera `Cầu Sài Gòn 2 (dạ cầu)` (`589d0bceb3bf7600110283d6`) mounted under the Saigon Bridge 2 pier directly facing the Saigon River surface. The frame showed open river water, bridge columns, and navigational hazard barriers with zero road surface or commuter vehicles.
  2. Because the camera surveyed river water, computer vision flood verification and segmentation pipelines continuously detected high water coverage, triggering false flood alerts.
  3. Commuters navigating street traffic were confused by non-road river cameras rendered on the urban traffic map.
- **Root Cause:**
  1. **Dual-Role Camera Upstream Ingestion**: The Department of Transportation (DOT) feeds encompass both municipal roadway traffic cameras and inland waterway / river navigation clearance monitoring cameras under bridge piers.
  2. **Indiscriminate Liveness Ingestion**: The camera had `isOnline: true` and `status: "UP"` in `cameras_enriched.json` and `cameras_all.json` because upstream HTTP snapshot endpoints were streaming live frames of river water.
  3. **Re-synchronization Drift**: Automated crawler probe tooling (`sync_catalog_from_audit.js`) derives `isOnline: true, status: "UP"` from `SUCCESS` HTTP responses, which would repeatedly unhide waterway cameras during daily audits unless explicitly prevented.
- **Mandatory Takeaways & Engineering Resolutions:**
  1. **Tri-Tier Camera Exclusion Architecture**:
     - **Static Catalogs (`cameras_enriched.json` & `cameras_all.json`)**: Tagged waterway footing cameras (`589d0bceb3bf7600110283d6`, `5b6c155aca0577001163fe36`, `5b6c1618ca0577001163fe3c`) with `status: "WATERWAY_MONITOR"`, `isOnline: false`, and `isWaterway: true`.
     - **Backend Ingestion Gating (`services/go-server/internal/camera/catalog.go`)**: Introduced `KnownWaterwayCameras` registry setting `cam.IsWaterway = true`, `cam.IsOnline = false`, and `cam.Status = "WATERWAY_MONITOR"`, preventing non-road cameras from participating in traffic status calculations or automated flood chokepoint checks.
     - **Crawler Governance (`tools/crawler/sync_catalog_from_audit.js`)**: Injected `WATERWAY_CAMERAS` set into `desiredState()`, preventing automated audit jobs from reverting waterway cameras back to `UP` / `isOnline: true`.
  2. **Resilient Frontend Map Hygiene (`apps/web`)**:
     - In `apps/web/src/utils/cameraFilterUtils.ts` and `apps/web/src/app/page.tsx`, defined `isWaterwayCamera` and filtered out waterway cameras from `displayCameras`. Even when commuters click `[Hiện camera mất tín hiệu]` / `[Hiện offline]` to inspect offline traffic feeds, non-road waterway cameras remain strictly hidden.
  3. **Strict Roadway Verification**:
     - Audited all 94 bridge-related cameras and all 10 cameras containing footing/waterway keywords (`dạ cầu`, `chân cầu`). Confirmed that active roadway underpasses (`Cầu Sài Gòn 1`, `Cầu Sài Gòn 3`, `Cầu Sài Gòn 5`, `Nguyễn Hữu Cảnh - Ngô Tất Tố 2`, `Võ Văn Kiệt - Cầu Chà Và 1`, `Nút giao Cây Gõ 3`, `Tỉnh lộ 43 - Chân cầu Gò Dưa`) remain fully visible and operational.
