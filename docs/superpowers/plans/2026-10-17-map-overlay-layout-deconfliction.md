# Map Overlay Layout Deconfliction Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Resolve component overlapping and collision bugs on both Desktop and Mobile viewports between the active filter chip, weather widget, camera filter, and bottom dock pill.

**Architecture:** Deconflict map overlays by establishing clear visual layering hierarchies: separate active contextual filter chips into their own dedicated sub-row beneath the main toolbar on Desktop; relocate the mobile WeatherWidget trigger into the top quick-search header row alongside the search bar; make CameraFilter text responsive to save horizontal space; and dynamically elevate the bottom arterial legend when pinned dock cameras are active on Mobile.

**Tech Stack:** Next.js 15 (React 19), Tailwind CSS, Framer Motion, TypeScript, Node test runner (`node:test`).

**Spec:** User bug report on Desktop view (active filter chip hidden behind weather widget and legend) and Mobile view (CameraFilter crashing into WeatherWidget at `top-28`, and collapsed dock pill overlapping arterial status legend).

## Global Constraints

- Monochromatic, zero-emoji UI design across all components and localized strings (Rule 1).
- Full bilingual i18n support (`vi` default with complete diacritics, `en` secondary) across all strings.
- Mobile touch targets must meet accessibility minimums ($\ge 36\text{px}$ height).
- Maintain 60 FPS leaflet map interactions without layout thrashing.
- Zero hardcoded fallback credentials or mock strings (Rule 8).
- Estimated active effort: 2 hours. Delivery window: Sprint 8 / immediate.

## Review Focus

1. **Very narrow mobile screens (320px - 360px)**: Ensure the search input and weather widget in the top header row truncate cleanly without horizontal scroll or breaking out of view.
2. **Long corridor/ward names in active filter chip**: Verify that strings like "Phường Tây Thạnh, Quận Tân Phú" or "Đoạn Nguyễn Văn Trỗi - Nam Kỳ Khởi Nghĩa" wrap or truncate cleanly on Desktop without reaching the top-right controls.
3. **Empty vs populated pinned dock state**: Confirm smooth vertical transition of the bottom arterial legend between `bottom-2.5` (0 pins) and `bottom-14` (1+ pins) on mobile.
4. **Offline toggle button state preservation**: Ensure clicking the compact `[Hiện offline]` / `[Ẩn offline]` button triggers state toggles accurately without bubbling to the map.
5. **Weather widget popover alignment**: Verify the dropdown popover from the top header on mobile opens downward within viewport boundaries without overflowing the right edge.

---

### Task 1: Add Compact Localization & Unit Tests for CameraFilter

**Files:**
- Modify: `apps/web/src/utils/cameraFilterUtils.ts:13-36`
- Modify: `apps/web/src/utils/cameraFilterUtils.test.ts:1-40`

**Interfaces:**
- Produces: `CAMERA_FILTER_I18N.COUNTER_COMPACT: { vi: (v, t) => string, en: (v, t) => string }`
- Produces: `CAMERA_FILTER_I18N.HIDE_OFFLINE_BTN_COMPACT: { vi: string, en: string }`
- Produces: `CAMERA_FILTER_I18N.SHOW_OFFLINE_BTN_COMPACT: { vi: string, en: string }`

- [ ] **Step 1: Write the failing test for compact filter strings**

In `apps/web/src/utils/cameraFilterUtils.test.ts`, add test cases validating:
- `CAMERA_FILTER_I18N.COUNTER_COMPACT.vi(9, 837)` returns `'9 / 837 cam'`
- `CAMERA_FILTER_I18N.COUNTER_COMPACT.en(9, 837)` returns `'9 / 837 cams'`
- `CAMERA_FILTER_I18N.HIDE_OFFLINE_BTN_COMPACT.vi` equals `'[Ẩn offline]'`
- `CAMERA_FILTER_I18N.SHOW_OFFLINE_BTN_COMPACT.vi` equals `'[Hiện offline]'`
- Zero emojis across all new strings.

- [ ] **Step 2: Run test to verify it fails**

Run: `node --test apps/web/src/utils/cameraFilterUtils.test.ts`
Expected: FAIL with `COUNTER_COMPACT is not a function / undefined`.

- [ ] **Step 3: Implement compact strings in `cameraFilterUtils.ts`**

Export `COUNTER_COMPACT`, `HIDE_OFFLINE_BTN_COMPACT`, and `SHOW_OFFLINE_BTN_COMPACT` in `CAMERA_FILTER_I18N` with proper Vietnamese diacritics and English equivalents.

- [ ] **Step 4: Run test to verify it passes**

Run: `node --test apps/web/src/utils/cameraFilterUtils.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/web/src/utils/cameraFilterUtils.ts apps/web/src/utils/cameraFilterUtils.test.ts
git commit -m "feat(web): add compact camera filter localization strings for mobile"
```

---

### Task 2: Make CameraFilter Component Responsive

**Files:**
- Modify: `apps/web/src/components/CameraFilter.tsx:20-60`

**Interfaces:**
- Consumes: `CAMERA_FILTER_I18N` with compact keys from Task 1.
- Renders: Full labels on `sm:` viewports and compact labels on `<sm` viewports.

- [ ] **Step 1: Update `CameraFilter.tsx` to render responsive labels**

In `apps/web/src/components/CameraFilter.tsx`:
- Compute `counterCompactText` and `buttonCompactText` using `CAMERA_FILTER_I18N`.
- In the badge, render `<span className="hidden sm:inline">{counterText}</span><span className="sm:hidden">{counterCompactText}</span>`.
- In the toggle button, render `<span className="hidden sm:inline">{buttonText}</span><span className="sm:hidden">{buttonCompactText}</span>`.

- [ ] **Step 2: Verify `npm test` and component build**

Run: `cd apps/web && npm test`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add apps/web/src/components/CameraFilter.tsx
git commit -m "feat(web): render responsive compact text in CameraFilter on mobile"
```

---

### Task 3: Deconflict Desktop Map Controls & Filter Chip Hierarchy

**Files:**
- Modify: `apps/web/src/components/MapComponent.tsx:516-550`

**Interfaces:**
- Separates Left controls into Row 1 (HexOverlay + CameraFilter) and Row 2 (Active Filter Chip).
- Preserves mobile vertical stacking without breaking desktop flex layout.

- [ ] **Step 1: Restructure left controls in `MapComponent.tsx`**

In `apps/web/src/components/MapComponent.tsx`:
- Replace the single `flex flex-col md:flex-row items-start md:items-center` container with a vertical column container `flex flex-col items-start gap-2`.
- Wrap `{children}` and `{cameraFilterControl}` into an inner row:
  `div className="flex flex-col md:flex-row items-start md:items-center gap-2"`
- Place `{isFiltered && activeFilterLabel && (...)}` directly below the inner row as a dedicated sub-row (Row 2).
- Update the chip container to use `max-w-[calc(100vw-24px)] md:max-w-xl` with clear text wrapping and z-index.

- [ ] **Step 2: Run web tests to verify zero regressions**

Run: `cd apps/web && npm test`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add apps/web/src/components/MapComponent.tsx
git commit -m "fix(web): place active filter chip in dedicated sub-row to prevent collision with right controls"
```

---

### Task 4: Relocate Mobile WeatherWidget & Deconflict Top Controls

**Files:**
- Modify: `apps/web/src/app/page.tsx:412-448`
- Modify: `apps/web/src/components/MapComponent.tsx:551-578`

**Interfaces:**
- Moves `WeatherWidget` on mobile into the top quick-search bar in `page.tsx`.
- Removes the conflicting `top-28 right-3` absolute positioning in `MapComponent.tsx`.

- [ ] **Step 1: Integrate WeatherWidget into the mobile search row in `page.tsx`**

In `apps/web/src/app/page.tsx`:
- Change the mobile search container from a single button to a flex container:
  `div className="md:hidden absolute top-3 left-3 right-3 z-[810] pointer-events-auto flex items-center gap-2"`
- Add `flex-1 min-w-0` to the search button.
- Add `<div className="shrink-0"><WeatherWidget weather={weather} loading={weatherLoading} lang={lang} /></div>`.

- [ ] **Step 2: Remove redundant mobile weather widget from `MapComponent.tsx`**

In `apps/web/src/components/MapComponent.tsx`:
- Remove `{weatherControl && (<div className="md:hidden absolute top-28 right-3 ...">...</div>)}`.
- Keep `weatherControl` on desktop view (`hidden md:flex absolute top-3.5 right-3 z-[800] items-start gap-2`).

- [ ] **Step 3: Run web tests to verify zero regressions**

Run: `cd apps/web && npm test`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add apps/web/src/app/page.tsx apps/web/src/components/MapComponent.tsx
git commit -m "fix(web): integrate mobile weather widget into top header and remove top-28 collision"
```

---

### Task 5: Deconflict Mobile Bottom Dock Pill & Arterial Legend

**Files:**
- Modify: `apps/web/src/components/MapComponent.tsx:31-65, 579-601`
- Modify: `apps/web/src/app/page.tsx:333-355`

**Interfaces:**
- Consumes: `hasPinnedCameras?: boolean` prop in `MapComponent`.
- Produces: Dynamically positioned bottom legend (`bottom-14 md:bottom-2.5` when pins exist, `bottom-2.5` when 0 pins).

- [ ] **Step 1: Add `hasPinnedCameras` prop to `MapComponentProps`**

In `apps/web/src/components/MapComponent.tsx`:
- Add `hasPinnedCameras?: boolean` to `MapComponentProps`.
- Destructure `hasPinnedCameras = false` in `MapComponent`.
- Update the bottom legend container className to dynamically apply:
  `hasPinnedCameras ? 'bottom-14 md:bottom-2.5' : 'bottom-2.5'`

- [ ] **Step 2: Pass `hasPinnedCameras` from `page.tsx`**

In `apps/web/src/app/page.tsx`:
- Add `hasPinnedCameras={pinnedCameras.length > 0}` to `<MapComponent ... />`.

- [ ] **Step 3: Run web tests and production build**

Run: `cd apps/web && npm test && npm run build`
Expected: PASS and successful Next.js production build.

- [ ] **Step 4: Commit**

```bash
git add apps/web/src/components/MapComponent.tsx apps/web/src/app/page.tsx
git commit -m "fix(web): elevate mobile arterial legend when pinned dock is active to eliminate overlap"
```

---

### Task 6: Comprehensive Verification & Visual Regression Check

**Files:**
- Verify: Full browser rendering on desktop ($1920\times 1080$, $1366\times 768$, $1024\times 768$) and mobile ($375\times 667$, $390\times 844$, $414\times 896$).

- [ ] **Step 1: Run complete test suite**

Run: `cd apps/web && npm test`
Expected: All 148+ tests PASS.

- [ ] **Step 2: Run production Next.js build**

Run: `cd apps/web && npm run build`
Expected: Zero build errors or lint warnings.

- [ ] **Step 3: Document layout audit and lessons learned**

Update `docs/lesson_learn.md` with Lesson 41 documenting overlay coordinate zoning best practices:
- Never assign independent absolute coordinates across sibling components without a shared collision coordinator.
- Contextual search/filter badges must occupy sub-rows rather than competing for horizontal space with permanent system toolbars.
- Collapsed mobile floating action bars must claim a reserved bottom clearance zone.
