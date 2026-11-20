# Lesson 510: Vehicle Clearance Profiles & Dynamic Hazard Thresholds

## Navigation Aid & Quick Reference

- **Domain**: Commuter risk classification, adaptive clearance thresholds, CSS stacking context deconfliction, spatial polyline ordering.
- **Relevant Issues**: Issue #510 (S17-T3), Issue #507, ADR-009 §5.2, Lesson L90 (Physical Depth Hazard Classification).
- **Key Files**:
  - `apps/web/src/types/vehicle.ts` (canonical vehicle profiles: scooter, motorbike, sedan, suv)
  - `apps/web/src/utils/vehicleHazardUtils.ts` (physical depth-first hazard evaluation, local persistence)
  - `apps/web/src/components/VehicleProfileSelector.tsx` (monochromatic, zero-emoji, >=48px kiosk touch target selector)
  - `services/go-server/internal/corridor/routes.go` (`VehicleClearanceThresholds`, arterial corridor coordinates)
  - `services/go-server/internal/corridor/corridor.go` (`calculateRouteSummary` depth-first precedence)
  - `services/go-server/internal/corridor/handler.go` (`NormalizeVehicleType`, query binding)
- **Core Invariants**:
  - **Physical Depth Trumps Categorical Flood Severity**: When physical depth in cm is measured (`maxDepthCm != nil`), vehicle threshold comparisons (`ImpassableDepthCm`, `CautionDepthCm`) must strictly determine the passability verdict. Fallback to generic `worstFloodSev == "FLOODED_SHALLOW"` or `"FLOODED_DEEP"` must only apply when depth is unmeasured (`maxDepthCm == nil`).
  - **CSS Stacking Context Deconfliction**: Fixed bottom stacks (`z-[800]`) must not occlude top header toolbars and their dropdowns (`z-[850]`, `z-[1200]`). Avoid wrapping map wrappers in arbitrary low stacking contexts (`z-0`) that trap interactive dropdown overlays beneath bottom cards.
  - **Corridor Coordinates Are Road Polylines, Not Random Camera Pins**: GeoJSON LineStrings representing an arterial road must follow the geographic curvature of the road sequentially, never dumping unordered camera coordinates or jumping across parallel corridors.

---

## 1. Context & Problem Statement

Commuters in Ho Chi Minh City face vastly different physical hazard thresholds during convective downpours and high river tides:
- A commuter riding a low-slung scooter (e.g., Honda Lead, Vision, Vespa) risks engine flooding and hydrolock at water depths as low as 20 cm (where the air intake and exhaust are submerged).
- A commuter on a high-wheel motorbike (e.g., Honda Wave, Winner, Exciter) can navigate water up to 25 cm safely.
- A commuter driving a high-clearance SUV or pickup (e.g., Ford Ranger, Everest) can traverse water up to 35-40 cm without mechanical stall.

Prior to Issue #510, ProjectKV evaluated corridor passability using a static, one-size-fits-all rule: any shallow flood triggered `CAUTION` and any deep flood triggered `IMPASSABLE`. This produced severe false positives for SUV drivers (warning them of impassability in 22 cm of water) and dangerous false negatives for scooter commuters (failing to warn that 22 cm of water would instantly stall their scooter).

Furthermore, early corridor geometries dumped raw camera points into coordinates, causing zig-zag polylines that jumped between parallel avenues (e.g., Nguyễn Hữu Cảnh and Điện Biên Phủ) rather than tracing the physical road.

---

## 2. Root Cause Analysis

### A. Categorical Severity Overriding Physical Depth
In the initial implementation of route summary calculations:
```go
if worstFloodSev == "FLOODED_SHALLOW" {
    verdict = Caution
}
```
Because the CV model categorizes any pooling over 5 cm as `FLOODED_SHALLOW`, an SUV facing 22 cm of water (which has a 25 cm caution threshold and 40 cm impassable threshold) was forced into `CAUTION` even though 22 cm is completely safe for an SUV. The categorical label took precedence over measured physical depth.

### B. CSS Stacking Context Trap
The desktop map header controls were placed inside `MapComponent`, whose parent container was styled with `relative z-0`. The bottom overlay stack (holding the Corridor Overview Card) had `absolute inset-x-0 bottom-0 z-[800]`.
Under CSS stacking context rules, any element inside a `z-0` container is trapped within that stacking context. As a result, even dropdown menus with `z-[1200]` inside `MapComponent` were rendered underneath the `z-[800]` bottom card, causing pointer events to be intercepted by elements in the bottom stack.

---

## 3. Implementation Details

### A. Sound Physical Depth Precedence
Both the Go backend (`corridor.go`) and TypeScript frontend (`vehicleHazardUtils.ts`) now enforce sound physical depth precedence:

```typescript
if (maxDepthCm !== null && maxDepthCm !== undefined && maxDepthCm > 0) {
  if (maxDepthCm >= profile.impassableDepthCm) {
    return { verdict: 'IMPASSABLE', reason: 'depth_exceeded' };
  }
  if (maxDepthCm >= profile.cautionDepthCm) {
    return { verdict: 'CAUTION', reason: 'depth_caution' };
  }
  // Depth is measured and within safe clearance: proceed to check traffic congestion
} else {
  // Only fallback to categorical flood severity when depth is unmeasured
  if (worstFloodSeverity === 'FLOODED_DEEP') {
    return { verdict: 'IMPASSABLE', reason: 'deep_flood_unmeasured' };
  }
  if (worstFloodSeverity === 'FLOODED_SHALLOW') {
    return { verdict: 'CAUTION', reason: 'shallow_flood_unmeasured' };
  }
}
```

### B. Stacking Hierarchy Deconfliction
1. Removed `z-0` from the map container wrapper in `apps/web/src/app/page.tsx`, allowing header toolbar elements to participate in the root stacking context.
2. Promoted desktop header toolbar to `z-[850]` and dropdown listboxes to `z-[1200]`, strictly above bottom card containers (`z-[800]`).

### C. Road Geometry Tracing
Re-aligned arterial corridor coordinates in `services/go-server/internal/corridor/routes.go` to strictly trace the road centerlines sequentially, eliminating zig-zag artifacts and false detours across parallel streets.

---

## 4. Verification Evidence

1. **Automated Unit Tests**:
   - `apps/web/src/utils/vehicleHazardUtils.test.ts`: 5 test scenarios validating dynamic threshold divergence at 22 cm and 32 cm.
   - `services/go-server/internal/corridor/corridor_test.go`: `TestVehicleClearanceProfiles` testing Go normalization and dynamic threshold divergence at 22 cm flood (scooter: IMPASSABLE, motorbike: CAUTION, sedan: CAUTION, SUV: PASSABLE).
2. **Empirical Browser Verification**:
   - `docs/proofs/510-vehicle-clearance/01_corridor_scooter_view.png`: Default scooter profile shows `NGUY CƠ CHẾT MÁY / THỦY KÍCH` and threshold `≥20cm`.
   - `docs/proofs/510-vehicle-clearance/02_vehicle_selector_dropdown.png`: Dropdown displaying 4 distinct vehicle profiles and clearance metrics.
   - `docs/proofs/510-vehicle-clearance/03_suv_passable_badge.png`: Selecting SUV dynamically transitions the verdict to `THÔNG THOÁNG` / `PASSABLE` with threshold `≥40cm`.
   - `docs/proofs/510-vehicle-clearance/04_polyline_toggled_off.png`: Corridor route polyline cleanly toggled off while retaining focus.
   - `docs/proofs/510-vehicle-clearance/05_card_dismissed_map_retained.png`: Card dismissed via close button without resetting map center or zoom.
