# Municipal Camera Catalog Maintenance & Health Audit Runbook

## Navigation Aid & Quick Reference
- **Domain**: Production operations, municipal CCTV catalog lifecycle, feed health monitoring, dead camera culling, arterial corridor coverage protection.
- **Relevant Issues**: Issue #531 (S18-T8).
- **Key Files**:
  - `tools/eval/cull_dead_cameras.py` (Automated feed health audit tool & culler)
  - `tools/eval/test_cull_dead_cameras.py` (Hermetic unit test suite)
  - `cameras_enriched.json` (Municipal camera catalog of 837 traffic cameras)
  - `reports/camera_cull_audit.json` (Audit output report with culling telemetry and corridor protections)

---

## 1. Overview & Context

Ho Chi Minh City operates 837 public traffic cameras across 16 districts. Over time:
- Cameras undergo road widening, hardware failures, or IP renumbering by the Department of Transport.
- Permanently dead feeds (> 14 days consecutive blackout or persistent 404/500 errors) clutter corridor status calculations and waste upstream network bandwidth during scheduled harvesting.
- However, naive wholesale culling risks creating coverage voids across critical arterial commuter corridors (e.g. Mai Chí Thọ, Võ Văn Kiệt, Nguyễn Hữu Cảnh).

This runbook documents the automated feed health audit toolchain and corridor safety floors.

---

## 2. Culling Safeguards & Invariants

1. **Persistent Blackout Threshold**:
   - A camera is only flagged as a candidate for culling if it is marked as `NOT_IMAGE` / `isOnline: false` or has had zero successful harvest snapshots for at least **14 consecutive days** (`--threshold-days 14`).
   - Transient network glitches or brief power cycles (< 14 days) are never culled.
2. **Arterial Corridor Coverage Floor**:
   - Every camera belongs to one or more corridors via `road_keys` or `road_key`.
   - Culling is strictly prohibited if removing an active camera causes the active camera ratio along any multi-camera corridor ($\ge 2$ cameras) to fall below **50%** (`--min-corridor-coverage 0.50`).
   - Protected cameras are recorded in `reports/camera_cull_audit.json` under `protected_cameras` with `projected_coverage` metrics.
3. **Audit Trail**:
   - Culled cameras are assigned `status: "stale_culled"` and `isOnline: false`.
   - Raw records remain preserved in the catalog JSON structure to enable subsequent restoration if DOT revives the feed.

---

## 3. Operational Workflow

### A. Dry-Run Audit (Recommended Before Any Culling)
Run the audit in dry-run mode to inspect proposed changes without modifying the catalog:
```bash
make cull-dead-cameras
```
Or with custom thresholds:
```bash
python3 tools/eval/cull_dead_cameras.py --catalog cameras_enriched.json --threshold-days 14 --min-corridor-coverage 0.50 --dry-run
```

Expected terminal output:
```text
[INFO] Audit Report Summary:
[INFO]   Total Cameras:            837
[INFO]   Previously Offline:       118
[INFO]   Culled Dead Cameras:      89
[INFO]   Active Online Remaining:  719
[INFO]   Protected By Corridor:    29
[INFO]   Audit Report Written:     reports/camera_cull_audit.json
[INFO] Dry-run mode active: cameras_enriched.json left untouched
```

### B. Applying Culling Updates
When ready to synchronize and cull confirmed dead cameras:
```bash
python3 tools/eval/cull_dead_cameras.py --catalog cameras_enriched.json --output-report reports/camera_cull_audit.json
```

### C. Restoring a Revived Camera
If DOT restores connectivity to a culled camera:
1. Locate the camera ID in `cameras_enriched.json`.
2. Update `status: "UP"` and `isOnline: true`.
3. Verify live snapshot retrieval via `curl -I "https://giaothong.hochiminhcity.gov.vn/render/ImageHandler.ashx?id=<CAMERA_ID>"`.
