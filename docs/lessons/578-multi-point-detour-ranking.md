# Lesson 578: Ranked Multi-Point Detour Suggestions Across Backend & Client Evaluation

**Sprint:** S20
**Issue:** #578
**Author:** AI agent
**Date:** 2026-11-28

---

## Context

`EvaluateCommute` in `services/go-server/internal/corridor/corridor.go` and
`evaluateCommuteRoute` in `apps/web/src/utils/commuteRouteUtils.ts` previously
broke out of their detour loop after finding a single 1-hop `PASSABLE` pair.
Multi-corridor commutes with several hazardous segments only received one
recommendation, `quoc_lo_1` had no outgoing detour edges, and 2-hop parallel
arterial chains were never explored when direct neighbors were congested or
shallow-flooded.

---

## Key Decisions & Pitfalls

1. **Preserve `detour` while adding `detours[]`**: Keep the top-ranked
   suggestion in `detour` (`Detour` in Go) and expose `detours[]` (`Detours` in
   Go) with `detour_path`, `detour_path_names`, `estimated_delta_seconds`, and
   `is_dry_route`. Existing tests and API consumers reading `.detour` keep
   working unchanged.
2. **Preserve canonical 1-hop edge expectations**: Existing unit assertions pin
   `CORRIDOR_DETOUR_MAP.nguyen_huu_canh` and `.vo_van_kiet`; adding 2-hop BFS
   over the arterial graph discovers multi-point arcs (such as `dien_bien_phu -> pham_van_dong`)
   without mutating those pinned direct edges, while completing coverage for
   `quoc_lo_1`.
3. **Sync client fallback and Go response parity**: Updating both `corridor.go`
   and `commuteRouteUtils.ts` ensures instant local evaluation matches live
   `/api/corridors/commute-eval` responses when offline or before hydration.
4. **Kiosk touch targets inside `max-h-[34vh]`**: Every rendered detour button
   retains `min-h-[48px]` inside the scrollable body of `MyRouteCard.tsx`,
   verified via `MobileErgonomics.test.ts` and `hud-cards.mobile.spec.ts` at
   `390x844`.
