# Lesson 550: Web Push Preferences, Commute Route Targeting, and Vehicle Clearance Drawer

## Navigation Aid & Quick Reference
- **Domain**: Frontend Web Push ergonomics, notification settings slide-over drawer, commuter route targeting, vehicle ground clearance filtering.
- **Relevant Issues**: Issue #550 (S19-T2), Issue #525 (VAPID Web Push Integration), Issue #510 (Vehicle Profiles), Issue #512 (My-Route Commuter Glance).
- **Key Files**:
  - `apps/web/src/components/NotificationDrawer.tsx` (Slide-over preferences drawer for push settings, route scopes, vehicle clearance, and commute hours)
  - `apps/web/src/components/NotificationPromptBanner.tsx` (Subtle opt-in banner inviting commuters to configure route flood alerts)
  - `apps/web/src/components/NotificationDrawer.test.ts` (Unit and contract tests validating zero emoji, 48px touch targets, and ARIA dialog semantics)
  - `apps/web/src/config/constants.ts` (`NOTIFICATION_DRAWER_I18N` catalog with full Vietnamese diacritics and English translations)
  - `apps/web/src/app/page.tsx` (Wired drawer trigger into `MyRouteCard` and banner into main map container)
- **Core Invariants**:
  - **Zero-Emoji Monochromatic Aesthetics**: In accordance with AGENTS.md Rule 1, all drawer surfaces use monochromatic zinc palettes (`bg-zinc-900`, `border-zinc-800`, `text-zinc-100`) and pure inline SVGs with zero emoji.
  - **Kiosk Ergonomics Target Floor**: Every interactive target (radio option cards, vehicle selectors, close/save buttons) satisfies the minimum $48 \times 48$ px touch bounding box.
  - **Zero-Backend Identity & Local Storage**: Commuter preferences (selected corridor, vehicle type, rush hours only vs 24/7) are stored locally in the browser under `kv_notification_preferences_v1`, requiring no user accounts or persistent tracking.

---

## 1. Context & User Experience Problem

Prior to Sprint 19, Web Push subscriptions were managed through a basic dialog tab (`SubscriptionModal.tsx`) that lacked route-specific controls:
1. **Lack of Route Context**: Commuters subscribing to notifications had no straightforward way to bind alerts to their regular commute (`My-Route`) or arterial corridor without manually searching through municipal taxonomies.
2. **Vehicle Clearance Ignorance**: Alerts did not differentiate between a motor scooter (which risks engine flooding at 15 cm) and an SUV (safe up to 35 cm), creating alert fatigue or under-alerting depending on the vehicle driven.
3. **Absence of Onboarding Prompt**: New visitors to the web app had no visible, non-intrusive entry point to opt in to route flood notifications.

---

## 2. Architectural Design & Implementation

### A. Slide-Over Notification Drawer (`NotificationDrawer.tsx`)
Implemented an accessible, focus-trapped drawer adhering to WAI-ARIA dialog specifications (`role="dialog"`, `aria-modal="true"`, `Escape` listener):
- **Web Push Activation**: Displays live browser permission state (`active`, `denied`, `unsupported`, `default`) with 1-click subscription toggle and test push verification.
- **Alert Scope Selector**: Allows commuters to select:
  1. *My Commute Route*: Alerts triggered by hazards along saved home-work corridor.
  2. *Arterial Corridors*: Direct dropdown selection from `ARTERIAL_CORRIDORS`.
  3. *All City Hotspots*: City-wide emergency notifications.
- **Vehicle Clearance Filter**: Selectable vehicle profiles (`scooter` [10 cm], `motorbike` [15 cm], `sedan` [20 cm], `suv` [35 cm]) ensuring alerts adapt to physical chassis heights.
- **Active Alert Hours**: Option to restrict notifications strictly to peak commute windows (06:30 - 09:00 & 16:30 - 19:30) vs 24/7.

### B. Onboarding Prompt Banner (`NotificationPromptBanner.tsx`)
A polite, dismissable top banner rendered when Web Push is supported and permission is still in default state, providing a direct 1-click CTA opening `NotificationDrawer`.

---

## 3. Verification & Empirical Proof

1. **Unit & Contract Tests (`apps/web/src/components/NotificationDrawer.test.ts`)**:
   - `NOTIFICATION_DRAWER_I18N`: 100% verified bilingual key completeness with 0 emoji.
   - `NotificationDrawer.tsx`: Verified $48 \times 48$ px touch targets, accessibility semantics, and local persistence.
   - `NotificationPromptBanner.tsx`: Verified touch targets and zero emoji.
2. **Full Web Suite Pass**:
   - `npm test`: 515/515 passing.
   - `npm run build`: Static export and type checks passed cleanly.
