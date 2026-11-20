# Lesson 494: Overlay Stacks Need Shared Flow, Not Magic Offsets

## Navigation Aid & Quick Reference

- **Domain**: Web layout, absolutely positioned overlays, collision avoidance, mobile responsive widths.
- **Relevant Issues**: Issue #494, PR #496 (S16 UI drawer layout regression), AGENTS.md Rule 2 (root-cause verification), Rule 10 (manual E2E verification).
- **Key Files**:
  - `apps/web/src/app/page.tsx` (overlay stack anchoring the corridor card and the dock)
  - `apps/web/src/components/CorridorOverviewCard.tsx` (card root sizing and positioning)
  - `apps/web/src/components/PinnedDock.tsx` (desktop strip, mobile pill, mobile drawer)
- **Core Invariants**:
  - **An overlay only reserves space if it is in normal flow.** `absolute` and `fixed` children are invisible to a parent flex container, so wrapping floating panels in a flex column guarantees nothing.
  - **No bottom offset may encode another component's size.** `bottom: calc(var(--safe-bottom) + 64px)` is a guess that rots the moment the measured element changes height, breakpoint, or content.
  - **A fixed pixel width is not a responsive width.** `w-[380px]` overflows any viewport narrower than 380px + the container insets.

---

## 1. Context & Problem Statement

Selecting an arterial corridor renders `CorridorOverviewCard` over the map, next to `PinnedDock`. The card was anchored with:

```tsx
className="fixed left-3 right-3 sm:right-auto sm:left-4 z-40 max-w-sm sm:w-[380px]"
style={{ bottom: 'calc(var(--safe-bottom, 0px) + 64px)' }}
```

The `64px` matched neither dock variant: the desktop strip measured 203 px at three pinned cameras (219 px in grid mode), the mobile pill is 48 px plus a `0.875rem` inset and the safe-area, and the expanded drawer reaches `60vh`. Measured at 1280x800 with three pins, the card bottom sat at 784 while the dock top sat at 597, so **187 px of the card was painted underneath the dock** — including the Telegram subscription CTA.

## 2. Root Cause Analysis

The PR's first attempt lifted the card into an overlay flex column:

```tsx
<div className="absolute inset-x-0 bottom-0 z-[800] flex flex-col items-start justify-end p-4">
  <div className="pointer-events-auto flex flex-col-reverse gap-4 w-[380px] max-w-full">
    <PinnedDock ... />
    <CorridorOverviewCard ... />
  </div>
</div>
```

That does not work, because every `PinnedDock` root is out of flow:

| Breakpoint | Root class | Flow |
|---|---|---|
| Desktop strip | `absolute bottom-0 left-0 right-0` | out of flow |
| Mobile pill | `fixed bottom-[...] left-1/2` | out of flow |
| Mobile drawer | `fixed bottom-0 left-0 right-0` | out of flow |

The card was therefore the only in-flow child, so the flex column placed it 16 px (`p-4`) above the viewport bottom and the dock painted over it at `z-[800]`. Two further regressions came from the same edit: the card dropped its `left-3 right-3` mobile sizing for a hard `w-[380px]` (6 px overflow at 390 px, 36 px at 360 px), and the mobile pill still overlapped the card by 30 px.

## 3. Solution

Both panels are siblings in one bottom-anchored flow column, and each contributes its real height:

```tsx
<div className="absolute inset-x-0 bottom-0 pointer-events-none z-[800] flex flex-col">
  {selectedCorridor && corridorRoute?.summary && (
    <div className="w-full px-3 pb-3 sm:px-4 sm:pb-4">
      <CorridorOverviewCard ... />
    </div>
  )}
  <PinnedDock ... />
</div>
```

`PinnedDock` roots became flow participants: the desktop strip is `w-full` instead of `absolute inset-x-0 bottom-0`, and the mobile pill is `w-fit mx-auto mb-[calc(0.875rem+env(safe-area-inset-bottom,0px))]` instead of `fixed left-1/2` (the `x: '-50%'` framer-motion props were dropped with it). The mobile drawer stays `fixed` — it is a modal sheet and must cover the stack — but inherits `pointer-events-auto` because the stack is `pointer-events-none`.

The card root became `w-full sm:w-[380px]`, restoring full-width mobile sizing inside the 12 px / 16 px insets.

## 4. Verification & Empirical Evidence

Measured with headless Chromium against the live stack (`go run ./cmd/server` + `next dev`), card rectangles from `getBoundingClientRect()`:

| Viewport | Card bottom | Dock/pill top | Gap | Horizontal overflow |
|---|---|---|---|---|
| 1280x800 strip | 581 | 597 | 16 px | none |
| 1280x800 grid (dock 219 px) | 565 | 581 | 16 px | none |
| 768x1024 | 757 | 773 | 16 px | none |
| 390x844 mobile | 770 | 782 | 12 px | none (width 366) |
| 360x640 mobile | 566 | 578 | 12 px | none (width 336) |

Before the fix the same measurements read 187 px of vertical overlap on desktop, 30 px on mobile, and 36 px of horizontal overflow at 360 px. Dock growth (203 px to 219 px when switching strip to grid) moved the card up automatically, proving no offset constant remained.

Interactions re-checked in the same session: card collapse/expand toggle, camera thumbnail opening `CameraModal`, mobile pill opening and the backdrop closing the drawer, and map clicks through the empty stack area still reaching the Leaflet pane. `next lint` reported no new warnings, `npm test` passed 448 tests, and `next build` exported cleanly.
