# Lesson 456: SVG Geometry Transform Origins, Hit Targets, and Hover Jitter Elimination

## Navigation Aid & Quick Reference
- **Domain**: SVG rendering, CSS Transforms in SVG, Hover jitter / flicker loops, Hit target ergonomics.
- **Relevant Issues**: Issue #456, Issue #377 (S10-T6), ADR-012.
- **Key Files**:
  - `apps/web/src/components/ForecastTimeline.tsx` (SVG data points, hit targets, origin centering)
  - `apps/web/src/utils/forecast.test.ts` (Point layout geometry and invariance verification)
- **Core Invariants**:
  - **SVG Transform Origin Defaults**: In SVG specifications and modern rendering engines (Blink/Chromium, WebKit), CSS transforms applied to SVG geometry elements (`<circle>`, `<path>`, `<rect>`) default to `transform-box: view-box; transform-origin: 0px 0px;`.
  - **Center Scaling Mandate**: Any CSS transform on an SVG geometry element with explicit spatial coordinates (`cx`, `cy`) MUST declare `transform-box: fill-box; transform-origin: center;` to prevent spatial translation during scaling.
  - **Decoupled Hit Targets**: An animated/scaling visual element should be decoupled from its hit target via `pointer-events-none` on the scaling visual element and a generous, static target (`pointer-events-auto`, e.g. `r="16"`) behind it. This eliminates edge boundary drop-offs and cursor flutter loops.

---

## 1. Context & Problem Statement

In `ForecastTimeline.tsx`, the short-term forecast chart renders 4 data points (Current, +10m, +20m, +30m) along a trend line. Each point was rendered as:
```tsx
<circle
  cx={p.x}
  cy={p.y}
  r="4.5"
  className="fill-zinc-950 stroke-zinc-100 stroke-[2] transition-transform duration-150 group-hover:scale-125"
/>
```
When a user hovered over any data point (e.g. `0.52`, `0.51`, `0.40`), the circle exhibited a severe visual bug: it jumped up to 95px down and to the right, and rapidly jittered in and out at monitor refresh rates (60–120 Hz).

---

## 2. Root Cause Analysis

### A. Coordinate Vector Multiplication from ViewBox Origin
Unlike HTML `<div>` elements whose CSS transform origin defaults to `50% 50%` (their own box center), SVG geometry elements default to `transform-box: view-box; transform-origin: 0px 0px;`.
When `scale(1.25)` is applied:
- The entire coordinate vector $(cx, cy)$ is multiplied by $1.25$:
  $$cx' = cx \times 1.25, \quad cy' = cy \times 1.25$$
- For a horizon point at $cx = 273.33, cy = 76.54$, the scaled circle moved to $cx' = 341.66, cy' = 95.67$.
- On screen, this displaced the circle by $+94.89\text{px}$ horizontally and $+25.43\text{px}$ vertically.

### B. Infinite Hover Toggle Loop (Jitter)
Because the circle was displaced far away from its original $(cx, cy)$ position:
1. Cursor enters circle at $(cx, cy) \to$ `:hover` activates on `.group`.
2. `scale(1.25)` triggers $\to$ circle jumps $95\text{px}$ away from cursor.
3. Cursor is now in empty space $\to$ `:hover` is immediately lost (`mouseleave`).
4. `scale(1.25)` is removed $\to$ circle snaps back to original $(cx, cy)$ under the cursor.
5. Cursor is on circle again $\to$ `:hover` reactivates $\to$ circle jumps away $\to$ loop repeats at frame rate.

---

## 3. Implementation Details

The solution applies two synergistic fixes in `ForecastTimeline.tsx`:

1. **Origin Centering**:
   Added `style={{ transformBox: 'fill-box', transformOrigin: 'center' }}` to the visible circle. When scaled by 1.25x, the center of scaling is exactly $(cx, cy)$, resulting in zero displacement ($\Delta X = 0, \Delta Y = 0$).
2. **Decoupled Hit Target with Pointer Events Isolation**:
   Added a stationary, transparent hit-test circle with radius `r="16"` directly behind the visible dot. Marked the visible animated circle with `pointer-events-none`:
   ```tsx
   {/* Generous invisible hit target to prevent jitter at boundary edges (Issue #456) */}
   <circle
     cx={p.x}
     cy={p.y}
     r="16"
     className="fill-transparent cursor-default pointer-events-auto"
   />

   {/* Point Center Dot with origin-centered scaling */}
   <circle
     cx={p.x}
     cy={p.y}
     r="4.5"
     style={{ transformBox: 'fill-box', transformOrigin: 'center' }}
     className="fill-zinc-950 stroke-zinc-100 stroke-[2] transition-transform duration-150 group-hover:scale-125 pointer-events-none"
   />
   ```
   Even if the user moves their cursor slightly across the boundary, the hit target remains stable and never moves or resizes.

---

## 4. Empirical Verification (Playwright MCP)

1. **Live DOM Measurement (`http://localhost:3001/?corridor=Trường+Chinh`)**:
   - `displacementX`: **0px**
   - `displacementY`: **0px**
   - `dotCenter`: $(803.92, 780.80)$
   - `hitCenter`: $(803.92, 780.80)$
   - `groupHovered`: `true`, `hitHovered`: `true` (stable continuous hover).
2. **Empirical Screenshot Proof**:
   `docs/proofs/s14_ui_ux/forecast_dot_hover_fixed.png` demonstrates the scaled-up point perfectly centered on the trend polyline without displacement.
3. **Automated Tests**:
   `npm test --prefix apps/web` passed 407/407 tests (81 suites).
