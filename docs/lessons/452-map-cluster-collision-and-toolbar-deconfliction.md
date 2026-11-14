# Lesson 452: Grid Binning Without Collision Deconfliction Overlaps Aggregates, and Uncoordinated Absolute Headers Collide

## Context

Issue `#452` investigated systemic UI defects observed on the staging deployment (`https://warded.529studio.site`):
1. Pitch-black cluster badges overlapping into massive unreadable clusters across the city at zoom 10–13.
2. The desktop header toolbar mangling text (`[H d e Quanh tôi]`) when viewport width or sidebar constraints tightened.
3. Programmer debug square brackets (`[...]`) leaking into user-facing copy.
4. A silent Go variable-shadowing defect in `cvclient/cvengine.go` causing nil pointer panics on directional cameras when cv-engine is unreachable.

## Root causes and what changed

1. **Spatial grid binning guarantees non-overlapping cells, but arithmetic centroids do not guarantee non-overlapping badges.**
   When cameras are grouped into discrete pixel cells (e.g. 64px or 80px), the cluster badge coordinate is placed at the arithmetic centroid of its constituent cameras. If two adjacent cells contain cameras clustered near their mutual boundary, their centroids can be just 5–15px apart on screen. When rendered as 48–68px badges, they overlap by 80%+.
   **Rule:** Grid binning is only Phase 1. A clustering helper must include a deterministic collision deconfliction pass (`deconflictClusters`): any adjacent rendered badges whose distance is $< (D_1 + D_2)/2 + \text{padding}$ are merged hierarchically into a single parent cluster, bubbling up count, unioning bounds, and maintaining the worst hazard level.

2. **Opaque dark pucks destroy cartographic legibility.**
   Solid `bg-zinc-950` disks rendered with thick dark borders looked like black bullet holes covering the urban road network.
   **Rule:** Map aggregates must use modern translucent dark glassmorphism (`bg-zinc-900/85 backdrop-blur-md border border-zinc-700/80 shadow-lg text-zinc-100`) with refined danger/amber accent rings, keeping underlying street topography visible.

3. **Uncoordinated absolute containers on the same axis guarantee layout collisions.**
   The left desktop controls (`md:top-3.5 md:left-14`) and right desktop controls (`md:top-3.5 md:right-3`) were two disjoint absolute elements. On laptops or when the 384px sidebar is open, the total combined width exceeded the available viewport, causing the right-aligned buttons to render directly over the left-aligned offline toggle.
   **Rule:** Top controls must be housed in a single coordinated flex layout (`absolute top-3.5 left-14 right-3 justify-between`) with `max-w-[calc(100%-380px)]` and `flex-wrap` on the left cluster so secondary tools wrap gracefully to a second row rather than colliding.

4. **Short variable declarations (`:=`) in nested conditionals silently shadow outer error variables.**
   In `services/go-server/internal/cvclient/cvengine.go`, `mpBody, contentType, err := buildMultipartBody(...)` declared a new inner `err`. Subsequent `predResp, err = a.postPredict(...)` assigned to the shadowed error. When `postPredict` failed, the outer `err` remained `nil`, skipping error handling and executing `defer predResp.Body.Close()`, triggering a SIGSEGV crash.
   **Rule:** Pre-declare error variables and use `=` assignment in conditional branches, and always guard `predResp != nil && predResp.Body != nil`.

5. **Debug brackets belong in terminal logs, not user-facing UI.**
   Strings like `● [Xanh: Thông thoáng]` and `[Hiện camera mất tín hiệu]` violate minimalist design and AGENTS.md Rule 1. All user-facing labels were cleaned to natural bilingual text (`Thông thoáng`, `Hiện camera mất tín hiệu`, `HOẠT ĐỘNG`).
