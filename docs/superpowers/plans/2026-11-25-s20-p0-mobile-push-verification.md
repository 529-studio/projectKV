# S20-P0 Mobile Push & 390x844 Verification Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Verify the existing Web Push opt-in flow end-to-end and add `390x844` Playwright responsive tests for `NotificationDrawer`, `MyRouteCard`, and `CorridorOverviewCard`.

**Architecture:** Install Playwright as a dev dependency; write a `playwright.config.ts` that runs against the static export (served by `npx serve out/`); add two test files — one for Web Push opt-in states (permission default → granted, denied recovery) and one for `390x844` layout assertions across the three HUD cards. No production code changes are expected; if a test reveals a real layout defect the fix goes in that task's commit.

**Tech Stack:** Playwright (latest), Node `--experimental-strip-types` for existing unit tests, Next.js 15 `output: 'export'`, Tailwind CSS.

**Spec:** `docs/HANDOFF-s19.md` §5 P0 — "End-to-end browser push opt-in, permission denial recovery state, and `390x844` responsive Playwright verification across `NotificationDrawer`, `MyRouteCard`, and `CorridorOverviewCard`."

## Global Constraints

- Zero emoji in all new source and test files (AGENTS.md Rule 1).
- Every interactive element >= 48px min-h / min-w (AGENTS.md Rule 1 / kiosk ergonomics).
- Tests run in `ci` mode: `npx playwright test --reporter=list`; must exit 0 on `dev` before PR.
- `NotificationDrawer` must remain at `z-[980]`; `CorridorOverviewCard` / `MyRouteCard` at `max-h-[34vh]`.
- Static export is in `apps/web/out/`; Playwright serves it locally on port 3001 to avoid colliding with the Go backend on 8080 and dev Next.js on 3000.
- Branch: `feat/s20-mobile-push-verification` targeting `dev`.
- PR description must start with `Close #<issue-number>` per AGENTS.md Rule 9.
- Lesson file: `docs/lessons/<issue>-s20-mobile-push-playwright.md` (no INDEX row on branch).

## Review Focus

1. **Permission denied then re-prompt**: After permission === 'denied', the Enable button is disabled and `STATUS_DENIED` text shows — a user who clears site permissions in browser settings and reloads should see the banner again. Test that `webPush.permission` re-reads from `Notification.permission` on `refreshStatus()`.
2. **SW registration on static export**: The app is `output: 'export'`; `sw.js` is in `public/` and served at `/sw.js` from the static root. Playwright must verify the SW registers without 404 on the mobile viewport.
3. **`max-h-[34vh]` overflow on 390px**: Both `MyRouteCard` and `CorridorOverviewCard` cap at `34vh`; on a 390x844 screen `34vh ≈ 287px`. Test that neither card overflows its container or clips its close button.
4. **`NotificationDrawer` full-height on mobile**: The drawer is `inset-y-0 right-0` (full viewport height); on 390px it must not overflow horizontally or clip the Save button in the footer. `max-w-md` = 448px > 390px, so the actual width constraint is `w-screen`.
5. **Banner visible on `permission === 'default'`**: `NotificationPromptBanner` only renders when `isSupported && !isSubscribed && permission === 'default'`; Playwright must intercept `Notification.permission` to simulate each state.

---

### Task 1: Install Playwright and add `playwright.config.ts`

**Files:**
- Create: `apps/web/playwright.config.ts`
- Modify: `apps/web/package.json` (add `@playwright/test` devDependency + `test:e2e` script)

**Interfaces:**
- Produces: `baseURL = 'http://localhost:3001'`, project `mobile-chrome` with viewport `{ width: 390, height: 844 }`, `webServer` block that runs `npx serve out -p 3001 -l tcp` and waits for port 3001.

- [ ] **Step 1: Install Playwright**

  ```bash
  cd apps/web && npm install --save-dev @playwright/test && npx playwright install chromium --with-deps
  ```

  Expected: `@playwright/test` appears in `package.json` `devDependencies`.

- [ ] **Step 2: Create `apps/web/playwright.config.ts`**

  Config must export a `PlaywrightTestConfig` with:
  - `testDir: './e2e'`
  - `projects: [{ name: 'mobile-chrome', use: { ...devices['Pixel 5'] } }]` — override `viewport: { width: 390, height: 844 }` to match iPhone 14 spec from handoff.
  - `webServer: { command: 'npx serve out -p 3001 -l tcp', url: 'http://localhost:3001', reuseExistingServer: !process.env.CI }`
  - `use: { baseURL: 'http://localhost:3001' }`

- [ ] **Step 3: Add `test:e2e` script to `apps/web/package.json`**

  Add: `"test:e2e": "playwright test"` alongside the existing `"test"` script.

- [ ] **Step 4: Verify config loads**

  Run: `cd apps/web && npx playwright test --list 2>&1 | head -20`
  Expected: Playwright lists test files (even if 0 yet) without config parse errors.

- [ ] **Step 5: Commit**

  ```bash
  git add apps/web/playwright.config.ts apps/web/package.json apps/web/package-lock.json
  git commit -m "chore(web): install Playwright and configure 390x844 mobile-chrome project"
  ```

---

### Task 2: Web Push permission-state unit tests (Node test runner)

**Files:**
- Modify: `apps/web/src/hooks/useWebPush.test.ts` (add denial-recovery and re-read tests)

**Interfaces:**
- Consumes: `checkPushSupport`, `mapPushPermission`, `urlBase64ToUint8Array` from `./useWebPush.ts` (unchanged signatures from S18).
- Produces: Tests that verify `mapPushPermission('denied', true) === 'denied'` and that `checkPushSupport` returns false when `Notification` is absent — pinning the denial recovery contract.

> These are Node unit tests (no browser), following the existing pattern in `useWebPush.test.ts`.

- [ ] **Step 1: Write failing test for denial recovery state**

  Append to `apps/web/src/hooks/useWebPush.test.ts`:

  ```ts
  test('denied permission maps to denied — Enable button must be disabled', () => {
    assert.equal(mapPushPermission('denied', true), 'denied');
  });

  test('permission denied with unsupported flag returns unsupported not denied', () => {
    assert.equal(mapPushPermission('denied', false), 'unsupported');
  });

  test('after clearing site permissions Notification absent maps to default', () => {
    // Simulates environment where Notification API is gone post-clear
    assert.equal(mapPushPermission(null, true), 'default');
  });
  ```

- [ ] **Step 2: Run existing + new tests to verify they pass**

  Run: `cd apps/web && node --experimental-strip-types --test 'src/hooks/useWebPush.test.ts'`
  Expected: All tests PASS (new ones are not novel logic, they pin existing behavior).

- [ ] **Step 3: Write failing test for `checkPushSupport` without Notification**

  Already covered by existing test `'returns false when Notification is missing'` — verify it passes. No new test needed; skip to commit.

- [ ] **Step 4: Commit**

  ```bash
  git add apps/web/src/hooks/useWebPush.test.ts
  git commit -m "test(web): pin Web Push permission denial-recovery contracts (S20-P0)"
  ```

---

### Task 3: `390x844` Playwright e2e — `NotificationDrawer` layout & push states

**Files:**
- Create: `apps/web/e2e/notification-drawer.mobile.spec.ts`

**Interfaces:**
- Consumes: `baseURL` from `playwright.config.ts`; static export at `apps/web/out/`.
- Produces: Playwright spec file exported as default; tests accessible via `npx playwright test e2e/notification-drawer.mobile.spec.ts`.

> The app is a static export; `Notification.permission` is simulated via `page.addInitScript`. The spec must build `out/` first (or assume it already exists); the `webServer` block in config handles serving.

- [ ] **Step 1: Build the static export**

  Run: `cd apps/web && npm run build`
  Expected: `out/` directory created, `sw.js` present in `out/sw.js`.

- [ ] **Step 2: Write the spec**

  Create `apps/web/e2e/notification-drawer.mobile.spec.ts`:

  ```ts
  import { test, expect } from '@playwright/test';

  // Helpers
  async function mockPushSupport(page, permission: 'default' | 'granted' | 'denied') {
    await page.addInitScript((perm) => {
      Object.defineProperty(window, 'Notification', {
        value: { permission: perm, requestPermission: async () => perm },
        writable: true,
      });
      Object.defineProperty(window, 'PushManager', { value: {}, writable: true });
      navigator.serviceWorker = {
        register: async () => ({ pushManager: { getSubscription: async () => null, subscribe: async () => { throw new Error('mock'); } }, ready: Promise.resolve({}) }),
        ready: Promise.resolve({} as any),
      } as any;
    }, permission);
  }
  ```

  Test assertions (write each as a separate `test()` block):

  1. `'NotificationPromptBanner renders on 390px when permission is default'`:
     - `mockPushSupport(page, 'default')` → `page.goto('/')` → assert `page.locator('[role="region"]')` is visible and within viewport width (`boundingBox().x + boundingBox().width <= 390`).
  
  2. `'NotificationDrawer opens and close button is reachable on 390px'`:
     - `mockPushSupport(page, 'default')` → open drawer (click banner CTA) → assert `[role="dialog"]` visible → assert close button `boundingBox().height >= 48` and `boundingBox().width >= 48`.
  
  3. `'NotificationDrawer Save button visible in footer without scrolling on 390px'`:
     - Open drawer → assert Save button is in viewport (`isInViewport()` / `isVisible()`).
  
  4. `'Enable Push button is disabled when permission is denied'`:
     - `mockPushSupport(page, 'denied')` → open drawer directly via `page.evaluate(() => { ... })` or by navigating to `/#notification-open` if that hook exists, otherwise interact via banner → assert Enable button `isDisabled()` is true.
  
  5. `'sw.js is served without 404'`:
     - `const res = await page.request.get('/sw.js')` → `expect(res.status()).toBe(200)`.

- [ ] **Step 3: Run Playwright spec**

  Run: `cd apps/web && npx playwright test e2e/notification-drawer.mobile.spec.ts --reporter=list`
  Expected: All 5 tests PASS. If a layout defect is found (e.g., Save button clipped), fix `NotificationDrawer.tsx` in this commit before marking done.

- [ ] **Step 4: Commit**

  ```bash
  git add apps/web/e2e/notification-drawer.mobile.spec.ts apps/web/out/
  git commit -m "test(web): 390x844 Playwright spec for NotificationDrawer push states (S20-P0)"
  ```

  > Do not commit `out/` to git — add `out/` to `.gitignore` if not already there; Playwright CI will rebuild.

---

### Task 4: `390x844` Playwright e2e — `MyRouteCard` and `CorridorOverviewCard` layout

**Files:**
- Create: `apps/web/e2e/hud-cards.mobile.spec.ts`

**Interfaces:**
- Consumes: `baseURL` from `playwright.config.ts`; `MyRouteCard` and `CorridorOverviewCard` are dynamically imported in `page.tsx` and require their state to be populated.
- Produces: Playwright spec that verifies both cards render within `34vh` and all header buttons are `>= 48px`.

> Both cards are conditionally rendered (require route/corridor selection). Use `page.addInitScript` to seed `localStorage` with mock `commute_config_v3` and trigger corridor selection via keyboard/click interactions.

- [ ] **Step 1: Write spec for `MyRouteCard` on 390px**

  Create `apps/web/e2e/hud-cards.mobile.spec.ts`:

  Test assertions:

  1. `'MyRouteCard stays within 34vh on 390x844 when commute config is set'`:
     - Seed localStorage with a mock `commute_config_v3` JSON before navigation.
     - Assert `section[aria-label*="Lộ trình"]` or `section[role="region"]` has `boundingBox().height <= 844 * 0.34 + 2` (allow 2px rounding).
  
  2. `'MyRouteCard close and collapse buttons are >= 48px on 390px'`:
     - Assert each header button `boundingBox().height >= 48` and `boundingBox().width >= 48`.
  
  3. `'CorridorOverviewCard stays within 34vh on 390px when corridor is selected'`:
     - Trigger corridor selection (click first corridor marker or use `page.evaluate` to dispatch a custom event that the page listens to, if available). Otherwise test against a fixture route that always shows the card.
     - Assert `section[aria-label*="Tổng quan"]` height constraint.
  
  4. `'CorridorOverviewCard close button is >= 48px on 390px'`:
     - Assert close button bounding box.

- [ ] **Step 2: Run Playwright spec**

  Run: `cd apps/web && npx playwright test e2e/hud-cards.mobile.spec.ts --reporter=list`
  Expected: All 4 tests PASS. Fix any layout defect in the same commit.

- [ ] **Step 3: Run full e2e suite**

  Run: `cd apps/web && npx playwright test --reporter=list`
  Expected: All 9 tests PASS (5 from Task 3 + 4 from Task 4).

- [ ] **Step 4: Commit**

  ```bash
  git add apps/web/e2e/hud-cards.mobile.spec.ts
  git commit -m "test(web): 390x844 Playwright spec for MyRouteCard and CorridorOverviewCard (S20-P0)"
  ```

---

### Task 5: CI gate, docs, and PR

**Files:**
- Modify: `.github/workflows/` — check if a web CI workflow exists and add `npx playwright test` step under a `test-e2e` job.
- Create: `docs/lessons/<issue>-s20-mobile-push-playwright.md`
- Modify: `README.md` if the `make test` or test instructions section changes.

**Interfaces:**
- Consumes: All prior tasks complete; `npx playwright test` exits 0.
- Produces: Open PR `feat/s20-mobile-push-verification → dev` with `Close #<issue>` prefix.

- [ ] **Step 1: Check existing CI workflows**

  Run: `ls .github/workflows/ | sort`
  If a `ci-web.yml` or `ci.yml` exists, add:
  ```yaml
  - name: Playwright e2e (mobile)
    run: cd apps/web && npx playwright install chromium --with-deps && npm run test:e2e
  ```
  If none exists, create `.github/workflows/ci-web-e2e.yml` with the minimal job.

- [ ] **Step 2: Run full unit + e2e tests once more**

  Run: `cd apps/web && node --experimental-strip-types --test 'src/**/*.test.ts' && npx playwright test --reporter=list`
  Expected: All pass.

- [ ] **Step 3: Write lesson file**

  Create `docs/lessons/<issue>-s20-mobile-push-playwright.md` with:
  - What: Static-export apps need Playwright's `webServer` to serve `out/` before tests run.
  - Why: `output: 'export'` has no dev server; using `npx serve` is the minimal adapter.
  - SW note: `sw.js` is copied to `out/sw.js` by Next.js build; a 404 test catches any `publicDir` misconfiguration.
  - Permission simulation: `page.addInitScript` intercepts `Notification` before React hydrates, allowing reliable permission-state testing without real OS prompts.

- [ ] **Step 4: Push branch and open PR**

  ```bash
  git push -u origin feat/s20-mobile-push-verification
  gh pr create --base dev --title "feat(web): 390x844 Playwright mobile verification + Web Push denial recovery (S20-P0)" \
    --body "$(cat .github/pull_request_template.md)"
  ```

  PR description first line: `Close #<issue-number>` (fill in GitHub issue number for this task).

- [ ] **Step 5: Verify CI passes**

  Wait for GitHub Actions to complete all checks green (unit tests, Playwright e2e, CodeQL, Docs Lint). Screenshot CI result for empirical evidence (AGENTS.md Rule 10).
