# Lesson 579: Multi-Segment Commute Builder (`origin -> waypoint -> destination`)

**Sprint:** S20
**Issue:** #579
**Author:** AI agent
**Date:** 2026-11-28

---

## Context

`CommuteConfig` previously modeled only two endpoints (`home`, `work`) and an
unordered list of `selectedCorridors`. Commuters traversing 2–3 connected
arterials via a junction or bridge could neither record an intermediate
waypoint nor order segments along their actual travel direction.

---

## Key Decisions

1. **Backward-compatible `waypoint` extension**: Added optional
   `waypoint?: CommuteLocation | null` to `CommuteConfig` and normalized legacy
   `warded_commute_route` payloads in `getStoredCommuteConfig()`.
2. **Ordered 3-segment cap (`MAX_COMMUTE_SEGMENTS = 3`)**: `MyRouteSetupModal`
   supports reordering chosen corridors with `>=44px` touch buttons, while
   `resolveOrderedCorridorsForDirection` reverses the segment order when
   toggling to `to_home`.
3. **Compact 3-stop `MyRouteCard` glance**: Renders `origin -> waypoint ->
   destination` inside the existing `max-h-[34vh]` budget without clipping on
   `390x844` mobile viewports.
