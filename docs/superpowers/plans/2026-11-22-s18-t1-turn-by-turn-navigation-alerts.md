# S18-T1 Implementation Plan: Turn-by-Turn Navigation Hazard Alerts

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a turn-by-turn sequential navigation hazard alert drawer and timeline along arterial corridor polylines that warns commuters of upcoming flood and congestion checkpoints adjusted to their vehicle ground clearance.

**Architecture:** A mathematical geometry utility (`navigationAlertUtils.ts`) projects corridor camera checkpoints onto the corridor GeoJSON `LineString` to calculate ordered chainage (distances in meters). A responsive, kiosk-ergonomic React component (`NavigationAlerts.tsx`) displays an interactive sequence of upcoming intersection hazard cards with vehicle clearance passability warnings, interactive map pan-to-point actions, and bilingual support.

**Tech Stack:** Next.js 15, React 19, TypeScript, Tailwind CSS, Leaflet, Vitest.

**Spec & Context:**
- Handed off in `docs/HANDOFF-s17.md` (Sprint 18 Horizon Task 1).
- Builds on Sprint 17 deliverables: Vehicle Clearance Profiles (PR #516), Mobile Kiosk Ergonomics (PR #521), and MyRoute/Corridor Route Layer (PR #518).

---

## Global Constraints

- **AGENTS.md Rule 1 (Design & UX)**: Minimalist, monochromatic, zero-emoji in user-facing UI. Bilingual i18n (`vi` default with full diacritics, `en` secondary).
- **AGENTS.md Rule 2 (Sound Solutions)**: Real geometry projections along polyline segments, not naive bounding box approximations.
- **AGENTS.md Rule 3 (Estimation Mandate)**: Active Effort: 6–8 hours; Delivery Window: 2026-11-23 (Sprint 18, Day 1).
- **AGENTS.md Rule 5 (Git Flow)**: Branch off `dev` as `feat/<issue-number>-turn-by-turn-navigation-alerts`, targeting `dev`.
- **AGENTS.md Rule 7 (Architecture Standards)**: Leaflet layers in dedicated panes (`corridorRoutePane`), high-contrast sunlight readability, clean decoupling of math from UI.
- **AGENTS.md Rule 10 (Verification)**: Comprehensive unit tests with Vitest, empirical browser verification scenarios, and zero console warnings.
- **Ergonomics**: Minimum 48px touch targets for all interactive cards and action buttons, safe-area inset accommodation.

---

## Proposed File Structure

- `apps/web/src/types/navigation.ts`: Data models for navigation checkpoints, hazard alerts, polyline projections, and i18n strings.
- `apps/web/src/utils/navigationAlertUtils.ts`: Pure geometric functions to project coordinates onto polyline segments, calculate cumulative distances, sort checkpoints sequentially, and compute vehicle-specific hazard warnings.
- `apps/web/src/utils/navigationAlertUtils.test.ts`: Vitest unit tests verifying projection math, edge cases (empty coordinates, collinear points, reverse traversal), and hazard classification.
- `apps/web/src/components/NavigationAlerts.tsx`: Minimalist, touch-friendly UI component displaying turn-by-turn milestone alerts, distance counters, stall risk warnings, and camera preview triggers.
- `apps/web/src/components/NavigationAlerts.test.ts`: Component test suite verifying rendering states, collapsed/expanded toggles, bilingual copy, and event callbacks.
- `apps/web/src/app/page.tsx`: Seamlessly integrates `NavigationAlerts` above the dock when a corridor or commute route is selected.

---

## Implementation Tasks

### Task 1: Navigation Types and Geometric Projection Utilities
- [ ] Define `NavigationHazardAlert`, `PolylineCheckpoint`, and i18n dictionary in `apps/web/src/types/navigation.ts`.
- [ ] Write failing unit tests in `apps/web/src/utils/navigationAlertUtils.test.ts` for polyline projection, distance along polyline calculation, and vehicle hazard mapping.
- [ ] Implement `projectPointOntoPolyline`, `calculateCheckpointChainage`, and `generateNavigationAlerts` in `apps/web/src/utils/navigationAlertUtils.ts`.
- [ ] Run `pnpm --filter web test navigationAlertUtils.test.ts` and verify all tests pass.

### Task 2: NavigationAlerts UI Component
- [ ] Create `apps/web/src/components/NavigationAlerts.tsx` with:
  - Header with corridor name, total checkpoints, and collapse/expand toggle.
  - Horizontally or vertically scrollable list of upcoming intersection cards.
  - Distance badge (e.g. `Cách 350m`, `Cách 1.2km`), passability badge (`THÔNG THOÁNG`, `CẢNH BÁO XE GẦM THẤP`, `NGUY CƠ CHẾT MÁY`).
  - Actionable advice text adapted to vehicle clearance (e.g. `Ngập 22cm vượt gầm xe 15cm`).
  - Minimum 48px touch targets and full keyboard accessibility.
  - Click-to-pan handler to highlight corresponding marker on Leaflet map.
- [ ] Write component tests in `apps/web/src/components/NavigationAlerts.test.ts`.
- [ ] Run `pnpm --filter web test NavigationAlerts.test.ts` and verify 100% green.

### Task 3: Map Integration and Ergonomics Polish
- [ ] Wire `NavigationAlerts` into `apps/web/src/app/page.tsx` within the overlay stack when `selectedCorridor` and `corridorRoute` are active.
- [ ] Connect `onSelectCheckpoint` callback to pan the map (`map.flyTo([lat, lng], 16)`) and trigger `handleSelectCameraById`.
- [ ] Verify safe-area insets (`env(safe-area-inset-bottom)`) so the alert cards stack neatly without obscuring the pinned dock or map controls.

### Task 4: Automated Verification & Empirical Evidence
- [ ] Run full web test suite: `pnpm --filter web test`.
- [ ] Run typechecks and linting: `pnpm --filter web typecheck && pnpm --filter web lint`.
- [ ] Execute manual verification scenario with simulated camera data and capture visual evidence.
- [ ] Audit for zero emoji, clean English code/comments, and bilingual UI parity.
