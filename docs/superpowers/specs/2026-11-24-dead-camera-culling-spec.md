# Specification: Automated Camera Synchronization and Dead Feed Health Culling

## 1. Problem Statement & Operational Rationale
The municipal traffic camera catalog in Ho Chi Minh City comprises 837 physical camera endpoints. Over time:
1. Cameras experience infrastructure renumbering, power dropouts, or permanent hardware decommissioning.
2. Persistently dead cameras (blackouts $> 14$ days or HTTP 404/500 failures) clutter corridor status calculations and waste harvesting network bandwidth and storage.
3. However, naive culling risks creating coverage voids across key arterial commuter corridors (e.g. Mai Chí Thọ, Võ Văn Kiệt, Nguyễn Hữu Cảnh).
Issue #531 establishes an automated, safe feed health audit tool (`tools/eval/cull_dead_cameras.py`) with arterial coverage floors.

## 2. Technical Scope & Architecture

### A. Dead Camera Culling Tool (`tools/eval/cull_dead_cameras.py`)
1. **Inputs & Invariants**:
   - Camera catalog JSON (`cameras_enriched.json`).
   - Snapshot history or audit records (harvest proofs, status records).
   - Configurable blackout threshold (`--threshold-days`, default: 14).
   - Configurable corridor coverage floor (`--min-corridor-coverage`, default: 0.50 / 50%).
2. **Arterial Corridor Protection**:
   - Corridors are identified via `road_keys` and `road_key`.
   - If culling an offline camera would cause active coverage along any corridor with $\ge 2$ cameras to fall below the minimum threshold (e.g. $< 50\%$), the camera is preserved and flagged with `cull_blocked: "corridor_floor"`.
3. **Execution Modes**:
   - `--dry-run`: Emits detailed audit report and statistics without mutating `cameras_enriched.json`.
   - Mutating mode: Updates camera records to `status: "stale_culled"` and `isOnline: false`.
   - Emits structured audit summary JSON (`reports/camera_cull_audit.json`).

### B. Automation & Workflow
- Makefile target: `make cull-dead-cameras` invoking `python3 tools/eval/cull_dead_cameras.py --dry-run`.
- Operational runbook: `docs/runbooks/camera_catalog_maintenance.md`.
- Architectural lesson: `docs/lessons/531-daily-camera-sync-and-culling.md`.

## 3. Acceptance Criteria
- Dry-run evaluation on `cameras_enriched.json` verifies offline camera cataloging and corridor protections with zero false positive drops.
- Unit test suite (`tools/eval/test_cull_dead_cameras.py`) covering 14-day thresholding, corridor floors, dry-run safety, and JSON integrity.
- All repository CI gates pass cleanly (100% green).
