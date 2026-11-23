# Lesson 523: Polyline Projection Along Arterial Corridors & Adaptive Navigation Hazards

## Context

Sprint 18 Task 1 (`#523`) introduced turn-by-turn sequential navigation hazard alerts along arterial corridor polylines (`NavigationAlerts.tsx` and `navigationAlertUtils.ts`). When commuters travel along major corridors (e.g. Võ Văn Kiệt, Nguyễn Hữu Cảnh), overview cards provide general passability verdicts, but active riders need milestone awareness: which upcoming intersection is flooded, how many meters ahead it is, and whether the depth exceeds their specific vehicle ground clearance.

## Root causes and what changed

1. **Orthogonal projection onto polyline segments beats naive distance-to-origin.** Earlier corridor checkpoints relied on geographic sorting or raw Euclidean distances to the corridor start point. For winding or curving arterial routes (e.g. Mai Chí Thọ or Nguyễn Hữu Cảnh bending along canal banks), naive bounding distances inverted the traversal order of camera points. `projectPointOntoPolyline` projects camera coordinates orthogonally onto each polyline segment with equirectangular metric scaling, clamping $t \in [0, 1]$ and accumulating cumulative chainage.
2. **Vehicle ground clearance defines passability, not generic severity strings.** A 22 cm flood is navigable for an SUV (`impassableDepthCm = 40cm`), but causes engine stalls on a low-clearance scooter (`impassableDepthCm = 20cm`). Checkpoint alerts now compute vehicle-specific excess depths ($+\text{depth} - \text{impassableDepth}$) and adapt advice dynamically (`Nguy cơ chết máy với Xe tay ga gầm thấp: Ngập 25cm (vượt gầm 20cm)`).
3. **Grace margins prevent premature checkpoint dismissal.** In outdoor GPS navigation, GPS jitter ($10–30\text{m}$) causes near-intersection checkpoints to flicker between "ahead" and "passed". Implementing a 50m grace margin (`distanceAhead >= -50`) ensures the driver continues seeing the active turn warning until cleanly past the intersection.
4. **Kiosk touch target invariants ($\ge 48\text{px}$) protect high-stress commuter interaction.** Commuters operating devices in rainy conditions or with gloves require large hit targets. All interactive controls (collapse toggles, map inspect triggers, and close buttons) enforce `min-h-[48px] min-w-[48px]`.

## Reusable guardrails

- Always project points onto ordered polyline segments to calculate true road chainage rather than straight-line distance.
- Account for floating point representation when rounding distances (e.g. `Math.round(m / 100) / 10` instead of bare `(m / 1000).toFixed(1)`).
- Provide a grace threshold before toggling milestone items from upcoming to passed.
- Maintain zero emoji and bilingual English/Vietnamese parity with complete diacritics.
