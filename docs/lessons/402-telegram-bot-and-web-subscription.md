# L402 — Issue-Keyed Lessons, Fail-Closed Webhooks, and Per-Chat Rate Limiting

**Issue**: [#402](https://github.com/529-studio/projectKV/issues/402) · **Sprint**: 12 · **Date**: 2026-11-12

## 1. The index was the merge conflict, not the lesson

Every lesson used to arrive as `docs/lessons/L<NN>-<slug>.md` plus a row in
`docs/lessons/INDEX.md`. Two branches open at the same time both pick the next
free `NN`, both edit the same one-row table, and the second merge has to resolve
a conflict in the single file that is loaded on every task. The index also has a
hard 20 KB ceiling (L66), so feature work was silently competing with a
sprint-level budget.

**Guardrail**: name the file after the issue (`docs/lessons/<issue>-<slug>.md`).
The issue number is unique by construction, so branches cannot collide. The
`INDEX.md` row is appended once in the sprint-close commit, by the person who
already owns the sprint boundary. A feature branch that edits the index is a
review finding, not a courtesy.

## 2. An unauthenticated webhook is worse than a missing one

The Telegram endpoint accepts writes from the public internet. Registering it
whenever the server boots would expose a route whose only authentication is a
token that may be unset. The safe default is absence, not rejection at request
time.

**Guardrail**: mount `POST /api/notify/telegram/webhook` only when
`TELEGRAM_BOT_TOKEN`, `TELEGRAM_BOT_USERNAME` and `TELEGRAM_WEBHOOK_SECRET` are
all present and syntactically valid. Otherwise leave the route unregistered so it
answers `404`, log exactly one reason, and keep alert dispatch in simulation
mode. Compare the secret with `crypto/subtle.ConstantTimeCompare` after a length
check, and never echo the supplied value, the token or the update body.

## 3. One shared egress IP must not exhaust the global limiter

Telegram calls webhooks from a small pool of shared IPs. The global per-IP token
bucket therefore sees every citizen's update as one client; a single busy minute
would produce `429`s that Telegram retries for minutes, long after the burst.
The fix is not a bigger global bucket — that weakens every other route.

**Guardrail**: exempt the webhook path from the global limiter and enforce the
limit that actually matches the threat model: a per-chat bucket (6 commands per
minute, one "slow down" reply per minute, then silence). Keep dedupe
(`update_id`, 15 min, 2048 entries), a bounded queue with `503 + Retry-After`
backpressure, and a worker pool so Telegram never waits on the Bot API.

## 4. Deep links are a two-language codec

The web UI and the bot independently encode the same target into a Telegram
`start` payload. A one-byte difference (padding, trailing bits, `+` versus `-`)
turns a valid CTA into "target not found" with no server error to grep.

**Guardrail**: share the normative vectors between Go and TypeScript
(`Bến Nghé → QuG6v24gTmdow6k`, `Bà Rịa - Vũng Tàu → QsOgIFLhu4thIC0gVsWpbmcgVMOgdQ`),
test the 64-character bound across the whole taxonomy, and validate the decoded
target against the live catalog before any database write. Unknown targets get
bilingual guidance and zero rows.

## Verification

- `go test ./...` — codec vectors, enqueue/dedupe/`503`, 401/413/400, `isExempt`,
  `UnsubscribeAll`, confirmation DM, limiter burst.
- `npm test` — byte-for-byte Go/TS payload parity, ward filter, chat-id pattern,
  error mapping, emoji-free catalog.
- Live gate: real bot, real chat, real verdicts under Rule 10; never a mock
  sender for release evidence.
