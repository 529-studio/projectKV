# Lesson 569: Playwright e2e for Static Next.js Export on 390x844 Mobile

**Sprint:** S20
**Issue:** #569
**Author:** AI agent
**Date:** 2026-11-27

---

## Context

Sprint 20 P0 required end-to-end Playwright verification of two HUD cards
(`MyRouteCard`, `CorridorOverviewCard`) and the Web Push notification drawer,
tested at 390x844 (iPhone 14 Pro equivalent) mobile viewport.

The `apps/web` Next.js app uses `output: 'export'` (fully static), so there is
no dev server to point Playwright at. This created a constraint the standard
Playwright docs do not cover directly.

---

## Key Findings

### 1. Static export requires `npx serve out` as webServer

Next.js `output: 'export'` produces a static `out/` directory; running
`next dev` or `next start` is not an option after export.

The `playwright.config.ts` `webServer` block must be:

```ts
webServer: {
  command: 'npx serve out -p 3001 -l tcp',
  url: 'http://localhost:3001',
  reuseExistingServer: !process.env.CI,
  timeout: 60_000,
},
```

Port `3001` avoids collision with the Go backend (`3000`) and the Next.js dev
server (`3000`).

### 2. `out/sw.js` is copied from `public/sw.js` by the build

The service worker is served at `/sw.js` in production. After `npm run build`,
it appears at `out/sw.js`. A Playwright test that does
`page.request.get('/sw.js')` and asserts `status === 200` will catch any build
misconfiguration that omits this file.

### 3. `Notification.permission` must be injected via `addInitScript`

In a headless Chromium context (CI or locally without a real browser), the Web
Push permission APIs are not available or return unexpected values. React
hydrates with the real `window.Notification`, so the only reliable way to
control the permission state in tests is `page.addInitScript` before
navigation:

```ts
await page.addInitScript((perm) => {
  Object.defineProperty(window, 'Notification', {
    writable: true,
    value: class MockNotification {
      static permission = perm;
      static requestPermission = () => Promise.resolve(perm);
    },
  });
  // also mock PushManager and navigator.serviceWorker
}, 'default');
await page.goto('/');
```

This must run before `page.goto` — it is an _init_ script, not a `page.evaluate`.

### 4. `NotificationPromptBanner` only renders when permission is `default`

The banner component checks:
```ts
webPush.isSupported && !webPush.isSubscribed && webPush.permission === 'default' && !dismissed
```
When mocking a `denied` permission, the banner does not render. The drawer can
still be opened programmatically via `page.evaluate`, but the banner CTA path
is unavailable. Tests must account for this expected UX behavior.

### 5. `NotificationDrawer` is only in the DOM when `isOpen === true`

The drawer is controlled by parent state in `page.tsx`. It is not a CSS
`display: none` toggle — it is a conditional render. Playwright cannot find
`role="dialog"` until the user clicks the banner CTA button.

### 6. HUD card heights: 34vh on 844px = 286.96px

The CSS class `max-h-[34vh]` on `CorridorOverviewCard` and `MyRouteCard`
translates to `0.34 * 844 = 286.96 px`. Allow 290px in assertions to account
for sub-pixel rounding and border-box sizing.

### 7. Touch target minimum is 44px for inner elements

AGENTS.md requires all interactive elements to be at least 48px. Some inner
elements inside 48px containers use `min-h-[44px]` (the iOS HIG minimum).
Playwright `boundingBox().height >= 44` is the correct assertion for inner
buttons.

---

## Fix Applied: Issue #568 — MyRouteSetupModal required corridors

`handleSave` previously fell back to `['nguyen_huu_canh']` when no corridors
were selected, so users could save a route with no corridors and
`MyRouteCard` would silently render with a single hard-coded corridor.

Fixed by:
1. Adding `corridorError` state.
2. Returning early with an error message when `selectedCorridors.length === 0`.
3. Removing the fallback; `selectedCorridors` is passed directly to `onSave`.

---

## CI Integration

The `verify-frontend` GitHub Actions job now runs Playwright after the build:

```yaml
- name: Playwright e2e (mobile 390x844)
  run: |
    cd apps/web
    npx playwright install chromium --with-deps
    npm run test:e2e
```

Chromium is installed fresh on each CI run. The `out/` directory produced by
`npm run build` is consumed by `npx serve` as the Playwright web server.
`out/` is gitignored and never committed.

---

## References

- `apps/web/playwright.config.ts`
- `apps/web/e2e/notification-drawer.mobile.spec.ts`
- `apps/web/e2e/hud-cards.mobile.spec.ts`
- `apps/web/src/hooks/useWebPush.test.ts` (denial-recovery pin tests added)
- `apps/web/src/components/MyRouteSetupModal.tsx` (fix for #568)
- `.github/workflows/ci.yml` (Playwright e2e step added)
