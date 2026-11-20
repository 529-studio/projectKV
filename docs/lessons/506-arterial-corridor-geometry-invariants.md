# Lesson 506: Arterial Corridor Waypoint Order & Geographic Continuity Invariants

## Navigation Aid & Quick Reference

- **Domain**: Geographic Information Systems (GIS), corridor polyline generation, map rendering, Go backend routes.
- **Relevant Issues**: Issue #506, Issue #475, Issue #492 (S16-T3 QL1 geometry alignment).
- **Key Files**:
  - `services/go-server/internal/corridor/routes.go` (`ArterialCorridors` polyline definitions)
  - `services/go-server/internal/corridor/corridor_test.go` (`TestArterialCorridors_GeometryInvariants`)
  - `apps/web/src/components/CorridorRouteLayer.tsx` (Leaflet polyline rendering pane)
- **Core Invariants**:
  - **Coordinates must follow vehicle trajectory, not raw longitude sorting.** Sorting waypoints naively by longitude causes acute reversals whenever a roadway curves slightly against its primary orientation.
  - **Inter-provincial camera name collisions must never pollute city arterials.** Matching camera labels without geographic bounding causes cameras in other provinces (e.g. Phú Mỹ, Bình Dương) sharing street names to inject 40km spikes into urban polylines.
  - **Automated geometry sanity tests must enforce bounding boxes, max segment spacing, and non-reversal angles.**

---

## 1. Context & Problem Statement

In Sprint 15, canonical arterial corridor polylines were introduced for HCMC commuter arterials. In Sprint 16, a severe zig-zag on Quốc lộ 1 was fixed (S16-T3 / PR #493). However, a comprehensive audit across all 10 canonical corridors in Sprint 17 revealed four additional corridors suffering from severe geometry corruption:
1. **`nguyen_huu_canh`**: Underpass camera (lat 10.791126) and bridge-foot camera (lat 10.795078) were inverted due to a 10m longitude difference, causing a 168° zig-zag reversal forward and backward across the underpass.
2. **`dien_bien_phu`**: Contained two rogue cameras from Mỹ Phước Tân Vạn in Bình Dương at latitude 10.997, causing 25km spikes north into another province and blowing total corridor length from 6.8 km to 102.8 km.
3. **`cach_mang_thang_tam`**: Contained a camera in Phú Mỹ (Bà Rịa - Vũng Tàu) at `(107.054729, 10.605945)`, causing a 44km tail spike east.
4. **`nguyen_van_linh`**: Contained a camera in Phú Mỹ at `(107.054386, 10.599364)`, causing a 39km spike east, plus duplicate camera jitter at multi-camera intersections.

## 2. Root Cause Analysis

The coordinates were initially extracted by matching camera catalog strings containing road names and sorting results naively by `longitude`. This failed in two fundamental ways:
1. **Name Ambiguity Across Administrative Boundaries**: Streets like "Điện Biên Phủ", "Cách Mạng Tháng 8", and "Nguyễn Văn Linh" exist across multiple provinces in southern Vietnam. String matching without geographic boundary filters linked remote cameras into urban corridors.
2. **Non-Monotonicity of Physical Curves**: Real-world roads do not follow strictly monotonic longitude or latitude. When a roadway meanders or heads north-northeast (as Nguyễn Hữu Cảnh does around Hầm chui / Cầu Thủ Thiêm), two adjacent checkpoints may have near-identical longitudes where the southern point has a marginally higher longitude than the northern point. Sorting by longitude swapped their sequential order, producing a loop or zig-zag.

## 3. Solution

1. **Purged Stray Remote Coordinates**:
   - Removed Bình Dương waypoints from `dien_bien_phu`.
   - Removed Phú Mỹ / Bà Rịa - Vũng Tàu waypoints from `cach_mang_thang_tam` and `nguyen_van_linh`.
2. **Trajectory Realignment**:
   - Reordered Nguyễn Hữu Cảnh checkpoints sequentially from Tôn Đức Thắng through Hầm chui, Dạ cầu, to Cầu Sài Gòn.
   - De-duplicated clustered camera points at single intersections on Nguyễn Văn Linh and Phạm Văn Đồng to eliminate microscopic jitter.
3. **Automated Geometry Invariant Test**:
   Added `TestArterialCorridors_GeometryInvariants` to `services/go-server/internal/corridor/corridor_test.go` enforcing:
   - All points within HCMC urban bounds `[106.30..107.00, 10.30..11.20]`.
   - Maximum segment distance $\le 4.5\text{ km}$.
   - Maximum direction reversal angle between adjacent segments $\le 110^\circ$.
   - Total polyline length within $50\%\dots150\%$ of declared corridor length.

## 4. Verification & Empirical Evidence

- **Automated Tests**: `go test -v -run TestArterialCorridors_GeometryInvariants ./internal/corridor/...` passed 10/10 subtests in 0.4s.
- **Empirical Visual Capture**: Captured Playwright screenshots before and after fix at `http://localhost:3001/?corridor=Nguy%E1%BB%85n+H%E1%BB%AFu+C%E1%BA%A3nh`. Saved under `docs/proofs/506-corridor-geometry/01_before_nhc_zigzag.png` and `02_after_nhc_smooth.png`.
- **Docs Lint & Evidence Integrity**: `tools/ci/check_evidence_integrity.py` and `tools/docs/docs_lint.py` passed with 0 new violations.
