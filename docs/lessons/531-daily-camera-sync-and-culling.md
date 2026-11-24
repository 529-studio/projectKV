# Lesson 531: Automated Camera Synchronization and Corridor-Floor Dead Feed Culling

## Navigation Aid & Quick Reference
- **Domain**: Municipal camera catalog lifecycle, feed health audit, automated culling, arterial corridor coverage protection.
- **Relevant Issues**: Issue #531 (S18-T8), Issue #529 (Metrics Offline Tracking).
- **Key Files**:
  - `tools/eval/cull_dead_cameras.py` (Feed health audit tool with arterial corridor floor safeguards)
  - `tools/eval/test_cull_dead_cameras.py` (Unit tests verifying 14-day thresholding, corridor floors, dry-run safety)
  - `docs/runbooks/camera_catalog_maintenance.md` (Operational procedures for catalog audits and restoration)
  - `Makefile` (`cull-dead-cameras`, `test-cull-cameras`)
  - `cameras_enriched.json` (Municipal catalog of 837 traffic cameras)
  - `reports/camera_cull_audit.json` (Audit summary telemetry)
- **Core Invariants**:
  - **14-Day Inactivity Threshold**: Feeds experiencing transient glitches or brief maintenance are never culled. Feeds must experience $\ge 14$ consecutive days of blackout or persistent HTTP errors before becoming eligible for culling.
  - **Arterial Corridor Coverage Floor**: Culling must never create a coverage void along arterial commuter corridors. If culling an offline camera would cause active camera coverage on a multi-camera corridor ($\ge 2$ cameras) to fall below 50% (`--min-corridor-coverage 0.50`), the camera is protected from culling and flagged for manual field review.
  - **Zero Destructive Data Loss**: Culled cameras are updated to `status: "stale_culled"` and `isOnline: false`, preserving their coordinate and taxonomy metadata in the catalog JSON so they can be seamlessly reactivated if the Department of Transport restores the physical feed.

---

## 1. Context & Operational Problem

Ho Chi Minh City's traffic monitoring network encompasses 837 cameras across 16 urban districts. Over seasonal maintenance cycles:
1. Physical cameras are occasionally renumbered, damaged by weather, or decommissioned during road widening.
2. Inactive cameras that produce persistent HTTP 404, 500, or image placeholder errors clutter corridor traffic routing, dilute coverage percentages in `corridor.Service`, and waste upstream polling bandwidth in the background harvester.
3. However, naive automated culling (e.g. dropping all offline cameras) introduces severe operational hazards: key arterial roadways with only 2 or 3 cameras could lose all visibility, rendering congestion and flood forecasting blind along critical commuter paths.

Issue #531 mandated a safe, automated feed health audit toolchain with arterial corridor coverage floors.

---

## 2. Architectural Design & Safeguards

### A. Two-Tiered Audit Pipeline
1. **Candidate Identification**:
   - Evaluates camera status (`NOT_IMAGE`, `DECOMMISSIONED`) and `isOnline` flags.
   - Inspects harvest proof histories to evaluate trailing observation timestamps against the 14-day blackout window (`--threshold-days 14`).
2. **Corridor Protection Guard**:
   - Indexes all corridors (`road_keys`, `road_key`) across the catalog.
   - For every culling candidate, simulates the projected post-cull active camera ratio across all associated corridors.
   - If projected ratio $< 0.50$ on any corridor with $\ge 2$ cameras, culling is rejected and the camera is recorded as `protected_by_corridor_floor`.

### B. Dry-Run & Telemetry Emission
- Default operation via Makefile (`make cull-dead-cameras`) runs in `--dry-run` mode.
- Emits structured JSON audit reports (`reports/camera_cull_audit.json`) detailing culled cameras, protected cameras, remaining online counts, and corridor coverage ratios.

---

## 3. Verification & Empirical Results

1. **Unit Testing (`tools/eval/test_cull_dead_cameras.py`)**:
   - Verified corridor indexing, dry-run non-mutation, active status updates (`status: "stale_culled"`, `isOnline: false`), and corridor floor protection.
2. **Catalog Dry-Run Audit on `cameras_enriched.json`**:
   - **Total Cameras**: 837
   - **Previously Offline**: 118
   - **Culled Dead Cameras**: 89
   - **Active Online Remaining**: 719
   - **Protected By Corridor Floor**: 29
3. **Repository Verification**:
   - `python3 -m unittest tools/eval/test_cull_dead_cameras.py`: 5/5 passing.
   - `make cull-dead-cameras`: clean execution.
   - `make docs-lint`: 0 new violations.
   - `make evidence-lint`: 135/135 artifacts verified.
   - `make test-model`: 146 tests passed.
   - `make test-go && make build-go`: 100% green with race detector enabled.
