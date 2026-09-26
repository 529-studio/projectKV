# Issue Template — Frontend (apps/web)

Copy this template for every new frontend issue. Frontend work is
feature-sliced (not module-DAG like the Go backend) but still needs the same
discipline: explicit technical details and an objective Definition of Done,
so an agent session has a clear stopping point.

---

## Title
`[web] <feature-name>: <one-line summary>`

## Context
Link to the relevant backend API contract row in
`docs/GO_BACKEND_ARCHITECTURE.md` §4 (if this feature consumes a new
endpoint), and to `AGENTS.md` for the design rules this must follow
(monochromatic palette, i18n vi/en, no heavy framework, marker scaling
rules, etc.).

## Depends on
Which backend issue(s)/endpoint(s) must already be merged and deployed
before this frontend work can be tested end-to-end. Frontend issues are
gated by backend endpoint availability, not by other frontend issues,
unless explicitly noted.

## Technical Details
- Exact files touched: `apps/web/index.html`, `apps/web/styles.css`,
  `apps/web/app.js` — name the specific function(s)/section(s) to add or
  modify, do not just say "update app.js".
- Exact backend endpoint(s) consumed, with expected request/response shape
  (copy from the API contract table).
- i18n: list every new user-facing string that needs both `vi` and `en`
  entries in the i18n catalog.
- Any new UI state (e.g. WebSocket connection status, subscription state)
  and where it lives (do not introduce a state management library — plain
  JS state per `AGENTS.md` zero-bloat rule).

## Out of Scope
Explicitly list what NOT to touch — no new npm dependency, no CSS framework,
no change to the chained filter or pin dock logic unless that IS the task,
no backend changes from a frontend issue.

## Definition of Done
- [ ] All listed files modified, no files touched outside Technical Details
- [ ] Every new user-facing string present in both `vi` and `en`, with
      correct Vietnamese diacritics (per `AGENTS.md`)
- [ ] Monochromatic palette respected — no new colors outside the approved
      set (black/dark gray/white + red/green/yellow status accents)
- [ ] No new third-party dependency added (`package.json` unchanged, or
      justified explicitly if it must change)
- [ ] Responsive: verified on desktop width and a mobile viewport (~375px)
- [ ] No console errors in browser dev tools on load and on the new
      interaction path
- [ ] Manually verified against the real backend endpoint (not just a
      mocked response), or against `cvclient`'s mock analyzer if the
      real backend isn't deployed yet
- [ ] Feature branch pushed, PR opened against `dev` per `AGENTS.md` workflow
