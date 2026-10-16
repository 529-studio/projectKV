# S7-T6: Real-Time Arterial Marker Saturation Coloring Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement real-time arterial marker saturation coloring (Green / Yellow / Red dots) and pulsating animation for the 50 Hot Set cameras on the web explorer map with 30-second polling and a monochromatic map footer legend.

**Architecture:** 
1. Create `hotsetUtils.ts` containing strongly typed definitions for Hot Set camera status, saturation thresholds ($SI < 0.30 \rightarrow$ `#10b981`, $0.30 \le SI < 0.60 \rightarrow$ `#f59e0b`, $SI \ge 0.60 \rightarrow$ `#ef4444`), color resolvers, and tooltip generators.
2. Build `useHotsetStatus.ts` React hook polling `GET /api/v1/cameras/hotset/status` every 30s with `AbortController`, memory leak prevention, and state caching.
3. Integrate with `MapComponent.tsx` and `Map.tsx` using in-place Leaflet `marker.setStyle`, `marker.setTooltipContent`, and SVG class toggling for pulsating animation on CONGESTED markers without layer recreation (preserving 60 FPS).
4. Add `@keyframes marker-congested-pulse` in `globals.css`.
5. Add minimalist monochromatic footer legend `[Xanh: Thông thoáng] [Vàng: Đông xe] [Đỏ: Ùn ứ]` in `MapComponent.tsx` and synchronize `MapLegend.tsx`.

**Tech Stack:** Next.js 15, React 18, Leaflet 1.9, Tailwind CSS, TypeScript 5, Node.js Test Runner.

**Spec:** Issue [#251](https://github.com/529-studio/projectKV/issues/251) and ADR-008.

## Global Constraints
- Minimalist, monochromatic, zero-emoji in user-facing UI and backend logs (Rule 1).
- Bilingual i18n (`vi` default with full diacritics, `en` secondary).
- Colors: Emerald green `#10b981` ($SI < 0.30$), Amber yellow `#f59e0b` ($0.30 \le SI < 0.60$), Rose red `#ef4444` ($SI \ge 0.60$).
- 60 FPS maintained during map panning/zooming without layer teardown.
- Zero direct push, PR targeting `dev` strictly starting with `Close #251 closes [KV-5]` (Rule 5 & Rule 9).

## Review Focus
1. **Clock Drift / Stale Verdicts**: When a camera's hot set status is marked `stale: true`, markers must not pulse and should indicate stale state.
2. **Missing or Non-Hotset Cameras**: Non-hotset cameras must retain standard online white `#ffffff` or offline `#52525b` marker styling.
3. **Hover & Filter Dimming Interactivity**: Hot set markers must respect ward/corridor filter dimming and hover enlargement without losing saturation color.
4. **Click Modal Preservation**: Clicking on any hot set marker must reliably trigger `onCameraClick(cam)` and open the camera preview modal.
5. **Memory Leak Prevention on Rapid Unmount**: Poller interval and abort controller must be cleaned up cleanly on component unmount.

---

### Task 1: Hotset Constants, Types, and Pure Utility Functions (TDD)

**Files:**
- Create: `apps/web/src/utils/hotsetUtils.ts`
- Modify: `apps/web/src/config/constants.ts`
- Test: `apps/web/src/utils/hotsetUtils.test.ts`

**Interfaces:**
- Produces:
  - `HotsetCameraStatus`, `HotsetStatusResponse` interfaces
  - `SATURATION_COLORS = { CLEAR: '#10b981', MODERATE: '#f59e0b', CONGESTED: '#ef4444' }`
  - `getSaturationColor(si: number): string`
  - `isHotsetCongested(si: number): boolean`
  - `getHotsetMarkerStyle(status?: HotsetCameraStatus | null, isOnline?: boolean): { fillColor: string; isCongested: boolean; isStale: boolean }`
  - `buildHotsetTooltipSection(status: HotsetCameraStatus, lang: 'vi' | 'en'): string`

- [ ] **Step 1: Write failing test in `apps/web/src/utils/hotsetUtils.test.ts`**
- [ ] **Step 2: Run test to verify it fails (`npm test`)**
- [ ] **Step 3: Implement `SATURATION_COLORS`, `API_ENDPOINTS.HOTSET_STATUS`, `HOTSET_LEGEND_I18N` in `constants.ts` and utility functions in `apps/web/src/utils/hotsetUtils.ts`**
- [ ] **Step 4: Run test to verify it passes (`npm test`)**
- [ ] **Step 5: Commit `git commit -m "feat(web): add hotset saturation coloring utilities and constants"`**

---

### Task 2: Polling Hook `useHotsetStatus` with 30s Interval and Resilience (TDD)

**Files:**
- Create: `apps/web/src/hooks/useHotsetStatus.ts`
- Test: `apps/web/src/hooks/useHotsetStatus.test.ts`

**Interfaces:**
- Produces:
  - `useHotsetStatus(options?: { pollIntervalMs?: number; enabled?: boolean }): { hotsetStatus: Map<string, HotsetCameraStatus>; total: number; updatedAt: string | null; loading: boolean; error: string | null; refresh: () => Promise<void> }`

- [ ] **Step 1: Write failing test in `apps/web/src/hooks/useHotsetStatus.test.ts` testing map parsing, abort handling, and fallback behavior**
- [ ] **Step 2: Run test to verify it fails (`npm test`)**
- [ ] **Step 3: Implement `useHotsetStatus` in `apps/web/src/hooks/useHotsetStatus.ts`**
- [ ] **Step 4: Run test to verify it passes (`npm test`)**
- [ ] **Step 5: Commit `git commit -m "feat(web): add useHotsetStatus hook with 30s polling"`**

---

### Task 3: CSS Pulsating Animation on Congested Markers

**Files:**
- Modify: `apps/web/src/app/globals.css`

**Interfaces:**
- Produces:
  - `@keyframes marker-congested-pulse`
  - `.marker-congested-pulse`

- [ ] **Step 1: Add `@keyframes marker-congested-pulse` and `.marker-congested-pulse` targeting SVG path elements in `apps/web/src/app/globals.css`**
- [ ] **Step 2: Verify CSS formatting and build compatibility**

---

### Task 4: Integrate Marker Saturation Coloring and Pulsating in `MapComponent.tsx`

**Files:**
- Modify: `apps/web/src/components/MapComponent.tsx`
- Modify: `apps/web/src/components/Map.tsx`
- Modify: `apps/web/src/app/page.tsx`

**Interfaces:**
- Consumes:
  - `useHotsetStatus`, `getHotsetMarkerStyle`, `isHotsetCongested`, `buildHotsetTooltipSection`
- Produces:
  - In-place marker update logic supporting Hot Set saturation styling without layer rebuilding (60 FPS)
  - Marker tooltip enrichment with $SI$ and PCU count
  - Monochromatic map footer legend: `[Xanh: Thông thoáng] [Vàng: Đông xe] [Đỏ: Ùn ứ]`
  - Modal click handler retained

- [ ] **Step 1: Add `hotsetStatus` prop to `MapComponentProps` with internal fallback to `useHotsetStatus`**
- [ ] **Step 2: Update marker creation and in-place style updates to apply saturation color and toggle `.marker-congested-pulse`**
- [ ] **Step 3: Update `buildTooltipHtml` to display saturation index ($SI$) and PCU for Hot Set cameras**
- [ ] **Step 4: Add monochromatic map footer legend in `MapComponent.tsx`**
- [ ] **Step 5: Wire `hotsetStatus` in `page.tsx` and synchronize `MapLegend.tsx`**
- [ ] **Step 6: Run tests and Next.js build (`npm test && npm run build`)**
- [ ] **Step 7: Commit `git commit -m "feat(web): integrate arterial marker saturation coloring and map footer legend"`**

---

### Task 5: Holistic Verification, Documentation, and PR Creation

**Files:**
- Update: `docs/project_status.md`
- Update: `README.md` (English & Vietnamese sections)
- Test: Full web test suite and build

- [ ] **Step 1: Run comprehensive tests: `npm test` and `npm run build` in `apps/web`**
- [ ] **Step 2: Update `docs/project_status.md` and `README.md` per Rule 6**
- [ ] **Step 3: Push branch `feat/s7-t6-map-marker-saturation` to origin**
- [ ] **Step 4: Create Pull Request targeting `dev` starting strictly with `Close #251 closes [KV-5]`**
