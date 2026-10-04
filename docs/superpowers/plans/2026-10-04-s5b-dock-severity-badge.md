# [Web] S5b: Wire Real Severity Badge into Pinned Dock Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the S5 placeholder "no analysis" severity badge in `PinnedDock.tsx` with a live traffic severity badge wired to real SSE/API verdict data, respecting provenance rules and monochromatic design guidelines.

**Architecture:** 
- A dedicated utility module `apps/web/src/utils/pinnedDock.ts` handles verdict evaluation, provenance gating (D10, A5), localization, and visual styling.
- `PinnedDock.tsx` receives verdicts and `appEnv`, evaluates the camera's verdict, and renders either the live severity indicator (CLEAR, MODERATE, HEAVY, CONGESTED) or falls back to the gray "no analysis" state.
- Strictly zero-emoji standard enforced across UI buttons, status badges, chips, and notifications.

**Tech Stack:** Next.js (App Router), React 19, TypeScript, Tailwind CSS, Framer Motion, Node test runner (`node:test`).

**Spec:** Issue #140: https://github.com/529-studio/projectKV/issues/140 & ADR-004 §3 A5, §9 D10, §10 S5b.

## Global Constraints

- Monochromatic & high-contrast palette; functional status accent colors strictly: Green (CLEAR), Yellow (MODERATE), Red (HEAVY / CONGESTED), Zinc/Gray (No analysis / stale).
- Strictly zero Unicode emojis in UI controls, status badges, chips, modal headers, navigation tabs, and notifications per `AGENTS.md` §1.
- Internationalization (i18n): 100% proper Vietnamese diacritics in `vi`, English in `en`.
- Provenance gate (D10, A5): Colored severity only when `source != 'mock'` (outside dev), `source != 'standby_dummy'`, verdict is not stale, and confidence is valid.
- Reuse S5's existing tile layout — no restructuring. No new third-party dependencies.

## Review Focus

1. **Stale verdict**: Stale verdicts (`stale === true` or observed > 180s ago) must never show a colored severity dot; they must display the neutral gray "no analysis" state.
2. **Provenance - standby_dummy**: Zero-confidence fallback verdicts from standby engine (`source === 'standby_dummy'`) must display gray "no analysis", even in dev mode.
3. **Provenance - mock outside dev**: Fabricated mock verdicts (`source === 'mock'`) must be suppressed to gray "no analysis" outside of dev mode (`appEnv !== 'dev'`).
4. **Distinguishing CONGESTED vs HEAVY**: Both use red accent, but `CONGESTED` must feature a distinct bold ring/border and pulsing dot indicator.
5. **No emoji corruption**: Zero Unicode emojis in user-facing dock badges, notice banners, and collapsed pills.

---

### Task 1: Add i18n Strings and Pinned Dock Severity Constants

**Files:**
- Modify: `apps/web/src/config/constants.ts`

**Interfaces:**
- Produces: `PINNED_DOCK_I18N.SEVERITY` with `{ CLEAR, MODERATE, HEAVY, CONGESTED }` bilingual strings (`vi` and `en`), and clean `COLLAPSED_PILL` without unicode emoji.

- [ ] **Step 1: Update `PINNED_DOCK_I18N` in `apps/web/src/config/constants.ts`**
  Add the four severity levels with exact Vietnamese diacritics and English translations:
  - `CLEAR`: "Thông thoáng" / "Clear"
  - `MODERATE`: "Vừa phải" / "Moderate"
  - `HEAVY`: "Đông đúc" / "Heavy"
  - `CONGESTED`: "Tắc nghẽn" / "Congested"
  Remove unicode emoji `📌` from `COLLAPSED_PILL`.

---

### Task 2: Create Pinned Dock Severity Utility Functions and Comprehensive Tests

**Files:**
- Create: `apps/web/src/utils/pinnedDock.ts`
- Modify: `apps/web/src/utils/pinnedDock.test.ts`

**Interfaces:**
- Produces:
  - `getDockSeverity(verdict?: TrafficVerdict | null, appEnv?: string): DockSeverity | null`
  - `getDockSeverityLabel(severity: DockSeverity | null | undefined, lang?: 'vi' | 'en'): string`
  - `getDockSeverityVisual(severity: DockSeverity | null | undefined): DockSeverityVisual`

- [ ] **Step 1: Write tests in `apps/web/src/utils/pinnedDock.test.ts`**
  Cover fresh verdicts, stale verdicts, standby_dummy, mock in dev vs non-dev, zero confidence, UNKNOWN, and styling classes.
- [ ] **Step 2: Run test to verify it fails**
  Run: `cd apps/web && npm test`
  Expected: FAIL due to missing `pinnedDock.ts` exports.
- [ ] **Step 3: Implement `apps/web/src/utils/pinnedDock.ts`**
  Implement `getDockSeverity`, `getDockSeverityLabel`, and `getDockSeverityVisual`.
- [ ] **Step 4: Run test to verify it passes**
  Run: `cd apps/web && npm test`
  Expected: PASS all tests.

---

### Task 3: Wire Severity Badge into PinnedDock Component and Eliminate Emojis

**Files:**
- Modify: `apps/web/src/components/PinnedDock.tsx`
- Modify: `apps/web/src/app/page.tsx`

**Interfaces:**
- Consumes: `getDockSeverity`, `getDockSeverityLabel`, `getDockSeverityVisual` from `../utils/pinnedDock`, `appEnv` from props.

- [ ] **Step 1: Update `PinnedDockProps` and `PinnedDock.tsx`**
  - Add `appEnv?: string` to `PinnedDockProps`.
  - In `renderTile`, replace the placeholder badge with the live severity indicator.
  - Adjust top bar right spacing to `right-7` to avoid colliding with the unpin `×` button.
  - Replace emojis (⚠️ in pin notice, 📌 in header and collapsed pill) with vector SVGs.
- [ ] **Step 2: Pass `appEnv={appEnv}` in `apps/web/src/app/page.tsx`**
- [ ] **Step 3: Run web tests, lint, and build**
  Run: `cd apps/web && npm test && npm run lint && npm run build`
  Expected: PASS all checks.

---

### Task 4: Final Verification, Git Commit, and Pull Request

**Files:**
- Git branch: `feat/s5b-dock-severity-badge`

- [ ] **Step 1: Verify git status and diff**
- [ ] **Step 2: Commit changes with descriptive commit message**
- [ ] **Step 3: Push branch to origin**
- [ ] **Step 4: Open PR against `dev` targeting Issue #140**
