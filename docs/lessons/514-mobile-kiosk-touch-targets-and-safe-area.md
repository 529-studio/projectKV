# Lesson 514: Mobile Kiosk Touch Targets & Safe-Area Ergonomics Polish

**Sprint**: Sprint 17 (Commuter Transformation)  
**Issue**: [#514](https://github.com/529-studio/projectKV/issues/514)  
**Category**: Frontend Ergonomics / Mobile UX & Kiosk Accessibility  
**Author**: Antigravity Assistant  
**Date**: 2026-11-21  

---

## 1. Problem Statement & Real-World Commuter Context

Warded is built for real-world Ho Chi Minh City commuters who frequently interact with the application on edge-to-edge smartphones (such as iPhone X through 16 with Home Indicators/Dynamic Islands, and modern Android devices with gesture navigation bars) in demanding outdoor environments:
- Waiting at rain-swept traffic lights with wet hands or gloves.
- Operating the phone with one thumb while balanced on a motorbike.
- Glancing at rapid route forecasts and vehicle hazard clearances before entering arterial corridors.

Following the initial rollout of Sprint 17 commuter transformations (Issues #510, #511, and #512), an ergonomic audit revealed several interaction barriers on mobile screens ($< 768\text{px}$):
1. **Sub-44px Touch Targets**: Several interactive controls (e.g., commute direction switch button, modal close glyphs, detour selection CTA, timeline scrubber reset/expand toggles, and corridor selection rows) measured only $24\text{px}$ to $36\text{px}$ in height, severely violating the $\ge 48\text{px} \times 48\text{px}$ kiosk ergonomics standard and causing mis-taps.
2. **System Gesture & Notch Clipping**: The mobile top search bar and bottom card stack lacked strict `env(safe-area-inset-top)` and `env(safe-area-inset-bottom)` offsets. When the camera dock was unpinned, the hourly timeline scrubber touched the physical bottom screen boundary, overlapping iOS home swipe indicators and Android navigation bars.
3. **Narrow Viewport (360px) Overflow Risk**: Floating vehicle clearance dropdowns and header control groups risked overflowing horizontally on narrow viewports without responsive boundary constraints.

---

## 2. Technical Architecture & Ergonomic Standard

```
+---------------------------------------------------------------------------------+
|                   MOBILE KIOSK TOUCH TARGETS & SAFE-AREA ARCHITECTURE           |
+---------------------------------------------------------------------------------+
                                       |
         +-----------------------------+-----------------------------+
         |                                                           |
         v                                                           v
[Kiosk Touch Standard >= 48px]                              [Safe-Area Inset Enforcement]
  • Direction Switch: min-h-[48px] min-w-[48px]               • Top Bar: top-[calc(0.75rem+env(safe-area-inset-top))]
  • Setup Modal Close: min-h-[48px] min-w-[48px]              • Bottom Stack: pb-[calc(0.5rem+env(safe-area-inset-bottom))]
  • Setup Inputs: min-h-[48px] px-3 py-2.5                    • Setup Modal: pt/pb-[max(1rem,env(safe-area-inset))]
  • Detour CTA: min-h-[48px] px-3.5                           • FAB Buttons: left/right-[calc(0.75rem+env(safe-area))]
  • Scrubber Reset: min-h-[48px] min-w-[48px]
  • Scrubber Expand: min-h-[48px] min-w-[48px]
  • Corridor Checkboxes: min-h-[44px]
         |                                                           |
         +-----------------------------+-----------------------------+
                                       |
                                       v
                      [Narrow Viewport (360px) Resilience]
                        • Dropdown max-w-[calc(100vw-1.5rem)]
                        • Timeline overscroll-x-contain touch-pan-x
                        • -webkit-overflow-scrolling: touch
```

### Key Implementation Adjustments:

1. **`apps/web/src/components/MyRouteCard.tsx`**:
   - Direction switch button elevated from `px-2 py-1 text-[11px]` to `min-h-[48px] min-w-[48px] px-3 py-1.5 text-xs rounded-lg`.
   - Setup gear button, collapse toggle, and close buttons expanded from sub-28px padding boxes to explicit `min-w-[48px] min-h-[48px] flex items-center justify-center`.
   - Detour action button upgraded to `min-h-[48px] px-3.5 py-2 font-semibold`.
   - Telegram alert subscription CTA expanded to a `min-h-[48px]` touch area.

2. **`apps/web/src/components/MyRouteSetupModal.tsx`**:
   - Modal backdrop and container equipped with `pt-[max(1rem,env(safe-area-inset-top))] pb-[max(1rem,env(safe-area-inset-bottom))]`.
   - Modal header close icon enlarged to `min-w-[48px] min-h-[48px]`.
   - Home and Work destination text inputs upgraded to `min-h-[48px] px-3 py-2.5`.
   - Preset route buttons and corridor selection labels formatted with `min-h-[48px]` and `min-h-[44px]` hitboxes.
   - Clear, Cancel, and Save action buttons standardized to `min-h-[48px] min-w-[48px]`.

3. **`apps/web/src/components/HourlyTimelineScrubber.tsx`**:
   - Scrubber reset-to-now button upgraded from `min-h-[36px]` to `min-h-[48px] min-w-[48px]`.
   - Expand/collapse chevron upgraded from `min-w-[36px] min-h-[36px]` to `min-w-[48px] min-h-[48px]`.
   - Timeline hourly buttons enforce `min-w-[84px] min-h-[52px]` hitboxes.
   - Added `overscroll-x-contain touch-pan-x` and `-webkit-overflow-scrolling: touch` to eliminate rubberbanding during mobile scrub gestures.

4. **`apps/web/src/components/VehicleProfileSelector.tsx`**:
   - Constrained dropdown menu with `max-w-[calc(100vw-1.5rem)]` to prevent horizontal viewport overflow on 360px screens.

5. **`apps/web/src/app/page.tsx`**:
   - Mobile floating header shifted to `top-[calc(0.75rem+env(safe-area-inset-top,0px))]`, `left-[calc(0.75rem+env(safe-area-inset-left,0px))]`, `right-[calc(0.75rem+env(safe-area-inset-right,0px))]`.
   - Mobile overlay stack dynamically applies `pb-[calc(0.5rem+env(safe-area-inset-bottom,0px))]` when pinned camera dock is empty.
   - Floating Action Buttons (FABs) for Near-Me and Mobile Search explicitly incorporate horizontal safe-area insets.

---

## 3. Empirical Verification Proofs (AGENTS.md Rule 10)

Automated tests and live browser simulations verified zero regression and strict adherence:
- **Unit Test Coverage**: `apps/web/src/components/MobileErgonomics.test.ts` expanded with Scenarios 6–9 covering all commuter controls and safe-area invariants (472/472 tests green).
- **Live Playwright Mobile Emulation**:
  - `docs/proofs/514-mobile-ergonomics/01_mobile_dock_and_vehicle_touch_targets.png` (271 KB): Displays 48px vehicle selector dropdown and mobile dock layout on iPhone viewport.
  - `docs/proofs/514-mobile-ergonomics/02_mobile_hourly_scrubber_safe_area.png` (298 KB): Displays expanded 48px hourly tidal & rain scrubber buttons clearing the bottom safe area.
  - `docs/proofs/514-mobile-ergonomics/03_mobile_my_route_card_ergonomics.png` (138 KB): Displays My-Route card with 48px direction toggle, 48px setup gear, and 48px dismiss action.
- **Budget Compliance**: All proof assets are strictly $\le 1\text{ MiB}$ per Rule 11.

---

## 4. Key Takeaways & Best Practices

1. **Thumb-First Design is Not Optional for Commuter Tech**: Interactive elements operated during commute transitions cannot rely on compact desktop button sizing. Every control must meet or exceed $48\text{px} \times 48\text{px}$.
2. **Safe Areas Differ by Context**: Safe-area padding must never be hardcoded as static margins (e.g. `pb-4`). Combining `env(safe-area-inset-*)` with fallback minimums (e.g. `max(1rem, env(safe-area-inset-bottom))`) guarantees zero clipping on both modern notched phones and older rectangular viewports.
3. **Horizontal Scroll Containment**: Dense horizontal timelines must declare `overscroll-x-contain` and `touch-pan-x` to isolate horizontal swipe friction from page-level gestures.
