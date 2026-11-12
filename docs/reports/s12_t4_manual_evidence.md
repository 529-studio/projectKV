# S12-T4 / Issue #402 — Manual Evidence (Local Live Gate)

**Date**: 2026-11-12 · **Branch**: `feat/402-telegram-bot-and-web-subscription`
**Environment**: macOS host, live Go server on `:3100`, live Next.js dev server on
`:3002`, SQLite at `$TMPDIR/kv-s12t4.db`, Chromium via Playwright.
**Honesty note**: no real Telegram bot token was available in this environment.
Anything that requires a real Bot API credential is marked **PENDING** rather
than claimed as passed. Everything below was executed, not reasoned about.

## 1. Inbound webhook matrix (spec M1, M8)

| Check | Command | Result |
|:--|:--|:--|
| Unprovisioned webhook | `POST /api/notify/telegram/webhook` with no config | `404` |
| Valid username + secret, no token | restart, same POST | `404` (fail-closed) |
| Missing secret | POST without `X-Telegram-Bot-Api-Secret-Token` | `401`, empty body |
| Wrong secret | POST with a different 40-char secret | `401`, empty body |
| Correct secret | `{"update_id":11}` | `200 {"ok":true}` |
| Replay | same `update_id` again | `200 {"ok":true}`, no second job |
| Malformed JSON | `not-json` | `400` |
| 70 KiB body | 70,000-byte JSON | `413` |
| Global limiter exemption | 40 consecutive webhook POSTs | `40 x 200` (no `429`) |

Startup emitted exactly one line:
`[INIT] Telegram webhook active: bot=@WardedAlertBot path=/api/notify/telegram/webhook`,
and `[INIT] Telegram webhook disabled: token missing` when the token was unset.

## 2. Bot pipeline through production adapters (spec M2, M6, M7)

With a syntactically valid but fake token, a private `/start sub_all` update was
posted to the live webhook:

```json
{"update_id":9001,"message":{"message_id":1,"from":{"id":444555666,"is_bot":false},
 "chat":{"id":444555666,"type":"private"},"date":1700000000,"text":"/start sub_all"}}
```

- `200 {"ok":true}`; SQLite row: `telegram|444555666|all|` (exactly one).
- Structured log: `update=9001 chat=5666 cmd=start outcome=handled` (last-4 only).
- Reply delivery failed `401 Unauthorized` because the token was fake; the row
  was retained, which is the documented blocked/failed-delivery behaviour.
- `/stop` (`update_id=9002`) returned `200`, deleted the row, `COUNT(*) = 0`.

**PENDING**: a real account DM, `/help`, `/status` and `setWebhook/getWebhookInfo`
require the staging `TELEGRAM_BOT_TOKEN`; the renderers and parser are covered by
the Go table tests, but the live AC9/M2 gate is not claimed here.

## 3. Web direct path (spec M12a)

| Input | Result |
|:--|:--|
| `chat_id=abc` | `400` |
| `chat_id=-1001234567890` | `400` (group id rejected) |
| `chat_id=12345` | `201`; row `telegram|12345|all|` |
| Modal `-1001234567890` | field error, `aria-invalid=true`, `role=status` announces it |
| Modal `555000222` (chokepoint selected) | `201`; row `telegram|555000222|chokepoint|56de42f611f398ec0c481299` |
| 6 rapid POSTs from one IP | `201` for the burst of 3, then `429` with `Retry-After` |

The confirmation DM was attempted on both successful submits and failed
`403/401` against the fake token; the POST still returned `201` and the log
contained `chat=0222` only (no full chat id, no token). With a real token this is
the spec §2.5 owner-visible DM.

## 4. Citizen modal (spec M9, M10, M11)

Live API taxonomy returned 4 target types, 8 zones, 101 wards and 837
chokepoints; `bot_username=WardedAlertBot`, `deep_link_enabled=true`.

- Desktop bell (48 x 48, `aria-haspopup="dialog"`) opens `role="dialog"` +
  `aria-modal="true"` with four scopes.
- CTA hrefs observed:
  - all: `https://t.me/WardedAlertBot?start=sub_all`
  - zone `Khu vực Nội thành TP.HCM`:
    `https://t.me/WardedAlertBot?start=sub_zone_S2h1IHbhu7FjIE7hu5lpIHRow6BuaCBUUC5IQ00`
  - ward `Sài Gòn`: `https://t.me/WardedAlertBot?start=sub_ward_U8OgaSBHw7Ju`
  - chokepoint `56de42f611f398ec0c481299`:
    `https://t.me/WardedAlertBot?start=sub_cam_56de42f611f398ec0c481299`
- Ward search `sai gon` matched `Phường Sài Gòn` (diacritic-insensitive); the
  list is capped at 20 rows. The live catalog no longer contains `Bến Nghé`
  (2025 ward merger), so that historical vector is unit-tested, not offered.
- VI -> EN switch re-rendered instantly with the same selection and the same
  CTA; no refetch.
- Escape closed the dialog, restored focus to the bell and reset
  `aria-expanded="false"`. Reopening returned to VI with the manual section
  collapsed.
- Focus trap: Tab from the last control wrapped to the language toggle;
  Shift+Tab from the first wrapped to the submit button.
- At 390 x 844: four scope labels measured 174 x 48; input, select, submit and
  close were all 48 px high. The sheet occupied 390 x 501 and stayed in-view.
- Taxonomy error state was observed when CORS was misconfigured: heading
  "Không kết nối được máy chủ" plus a 48 px Retry button. It disappeared once
  the origin was whitelisted.

Screenshots: `docs/proofs/s12_t4/subscription-modal-{vi,en}-{desktop,mobile-390}.png`.

## 5. Automated gates run

```bash
cd services/go-server && go build ./... && go vet ./... && go test ./...
cd services/go-server && go test -race ./internal/telegrambot/... ./internal/subscription/... ./internal/notify/... ./internal/middleware/...
cd apps/web && npx tsc --noEmit && npm test && npm run lint && npm run build
python3 tools/docs/docs_lint.py
python3 tools/docs/check_readme_vi_parity.py
```

All passed. `npm run lint` reports only pre-existing `<img>` warnings and one
unused `_url` in an existing e2e test.

## 6. Remaining staging work (not claimed)

1. Provision the real bot token and secret, run the §7 runbook
   (`setWebhook`, `getWebhookInfo` with `pending_update_count: 0`).
2. Execute M2, M3, M4, M5, M6, M7 and M12b with a real account and live
   verdicts; capture the DM screenshots.
3. Run `scripts/verify_staging_telegram.sh` after at least one chat is
   subscribed.
