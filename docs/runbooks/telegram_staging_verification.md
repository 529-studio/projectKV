# Runbook — Live Staging Telegram Flood Alert Verification (Stack 14 homesrv)

Operational runbook for Issue #246 / S8-T9: activating the Telegram flood alert
channel on the staging server (Portainer Stack 14, `homesrv`) out of simulation
mode and verifying live end-to-end delivery before public subscriptions open.

Audience: on-call operators and developers with Portainer access to `homesrv`.
Estimated execution time: 30-45 minutes (excluding the optional 31-minute
debounce-expiry observation).

---

## 1. Scope and Acceptance Criteria

This runbook satisfies the Definition of Done of Issue #246:

| # | Acceptance criterion | Verified in |
|---|---|---|
| 1 | Live notification received in staging Telegram group with zero emojis | Section 5 (script) + Section 6 (subscription + live verdict) |
| 2 | 30-minute quiet period suppresses repeat alert (5-minute re-trigger) | Section 7 (deterministic test + staging observation) |
| 3 | Zero bot token leakage in container logs or error responses | Section 8 |
| 4 | Staging verification runbook committed | This document |

Out of scope: Zalo OA webhook adapter (ADR-001 Phase 2), public commuter
subscription UI.

## 2. Prerequisites

1. **Staging bot token.** Create a dedicated staging bot via [@BotFather](https://t.me/BotFather)
   (`/newbot`, suggested name `projectkv_staging_alerts_bot`). Copy the token of
   the form `<bot_id>:<base64url-secret>` (e.g. `123456789:AAH...`).
   Never reuse the production bot for staging.
2. **Staging verification group.** Create a Telegram group for the staging team,
   add the bot, and resolve the numeric chat ID:
   - Send any message in the group, then
     `curl "https://api.telegram.org/bot<TOKEN>/getUpdates"` and read
     `result[-1].message.chat.id` (negative for groups, e.g. `-1001234567890`).
   - If the API answers `400: chat not found`, the bot has not seen the chat
     yet; send one more message in the group and retry.
3. **Portainer access** to `homesrv` Stack 14.
4. **Credentials locally.** Place the token and chat ID in the gitignored
   repo-root `.env` (see `.env.example`, keys `TELEGRAM_BOT_TOKEN` and
   `TELEGRAM_CHAT_ID`) or export them in your shell. Per AGENTS.md Rule 8 these
   values must never be hardcoded, committed, or pasted into tickets.

## 3. Configure Portainer Stack 14

1. Open Portainer (`homesrv`) > Stacks > Stack 14 > Editor.
2. Add / update the following environment entries for the go-server container:
   ```yaml
   TELEGRAM_BOT_TOKEN: <your-telegram-bot-token>
   TELEGRAM_DEBOUNCE_MINUTES: "30"
   ```
   `TELEGRAM_CHAT_ID` is NOT a server variable: the go-server resolves
   recipients dynamically from the subscription store
   (`internal/subscription`), so it is only needed on the operator machine for
   the pre-flight script.
3. Deploy / update the stack, then confirm the channel mode from the container
   startup log:
   ```bash
   docker logs <go-server-container> 2>&1 | grep "\[INIT\] Telegram"
   ```
   - Live mode (expected): `[INIT] Telegram notification channel registered and active.`
   - Simulation mode (token missing): `[INIT] TELEGRAM_BOT_TOKEN not configured: Telegram channel registered in simulation mode (delivery skipped).`
4. Confirm the debouncer line in the startup banner, e.g.
   `[NOTIF] Telegram:   debounce=30m, active=true`.

## 4. Deployed Controls (What You Are Verifying)

| Control | Implementation | Guarantee |
|---|---|---|
| Bilingual zero-emoji HTML template | `internal/notify/telegram.go` `FormatTelegramMessage` | Monochromatic VI/EN message per Rule 1 |
| 30-minute sliding debouncer | `internal/notify/debounce.go` + `Manager.Dispatch` | At most one alert per chokepoint per window; suppressed repeats do not advance the anchor |
| Token redaction in channel errors | `internal/notify/telegram.go` `SendTo` | `telegram http post: ... [REDACTED_BOT_TOKEN] ...` |
| Token redaction in HTTP responses | `internal/middleware/sanitizer.go` `telegramBotTokenRegex` | Any token echoed in a response body is scrubbed to `[REDACTED_BOT_TOKEN]` |

## 5. Pre-Flight: Live Bot API Round-Trip

From a machine with the repo checked out and credentials exported:

```bash
cd ~/repos/projectKV
./scripts/verify_staging_telegram.sh
```

The script validates, in order: credential presence (fail-fast), `getMe` token
authenticity (HTTP 200, `ok=true`), the bilingual zero-emoji message contract
for camera `586e2995f9fab7001111b0b5` (Ung Văn Khiêm - Nguyễn Gia Trí, D2), and
a live `sendMessage` delivery to the staging group (HTTP 200, `ok=true`).

Expected output: `Result: PASSED.` — then visually confirm in the Telegram
group that the message renders as bold HTML with both Vietnamese and English
lines and contains no emojis. The composed message mirrors the production
`dispatchFloodAlert` → `notify.FormatTelegramMessage` output for this camera;
parity is pinned by `TestFormatTelegramMessage_StagingChokepointFixture`.

Note on the Area line: production renders `district = cam.Zone`, a **traffic
zone** — for this camera `Khu vực Nội thành TP.HCM` (default `ZoneNoiThanh`
classification), not the administrative district (`Bình Thạnh`) listed in the
chokepoints fixture. Expect `Khu vực / Area: Thạnh Mỹ Tây, Khu vực Nội thành
TP.HCM`; that value is correct, not a bug.

Optional staging liveness probe (chokepoints endpoint serving the camera):

```bash
STAGING_BASE_URL=https://warded.529studio.site ./scripts/verify_staging_telegram.sh
```

Dry-run against a local mock (no network):
`TELEGRAM_API_BASE=http://127.0.0.1:8081 ./scripts/verify_staging_telegram.sh`

## 6. Register the Staging Subscription

So that real flood verdicts reach the group, register the chat as a subscriber
for the staging chokepoint:

```bash
curl -s -X POST "https://warded.529studio.site/api/subscriptions" \
  -H "Content-Type: application/json" \
  -d '{
    "channel_name": "telegram",
    "chat_id": "<your-telegram-chat-id>",
    "target_type": "chokepoint",
    "target_id": "586e2995f9fab7001111b0b5"
  }'
```

Expected: HTTP `201`. Verify with `curl -s "https://warded.529studio.site/api/subscriptions"`.

On-demand verification of the camera (runs the real flood analysis pipeline;
dispatches an alert only when the verdict is `FLOODED_SHALLOW`/`FLOODED_DEEP`):

```bash
curl -s -X POST "https://warded.529studio.site/api/flood/verify?camera_id=586e2995f9fab7001111b0b5"
```

## 7. Verify the 30-Minute Sliding Debouncer

**Deterministic (offline, always run first).** The suppression semantics —
including the exact Issue #246 scenario "second alert within 5 minutes is
suppressed, alert after window expiry re-delivers" — are covered by:

```bash
cd services/go-server
go test -race ./cmd/server -run 'TestNotifyIntegration' -v
go test -race ./internal/notify/ -v
```

`TestNotifyIntegration_DebounceFiveMinuteWindow` asserts: dispatch at T0
delivers; dispatch at T0+5min is suppressed AND does not advance the window
anchor; dispatch at T0+31min delivers again.

**Live observation on staging.** During the next genuine multi-verdict flood
event (or a controlled analysis burst on the same chokepoint):

1. Watch the container log:
   ```bash
   docker logs -f <go-server-container> 2>&1 | grep "notify:"
   ```
2. Confirm the first verdict logs `notify: dispatching flood alert to channels`
   followed by `telegram: dispatching flood alert`, and the group receives
   exactly one message.
3. Confirm a subsequent verdict on the same camera within the window logs
   `notify: flood alert suppressed by 30-minute debounce window` and no new
   group message appears.
4. (Optional, 31+ minutes later) confirm the next alert after window expiry is
   delivered again.

## 8. Verify Zero Bot Token Leakage

1. **Container logs.** Scan the full log stream for the raw token:
   ```bash
   docker logs <go-server-container> 2>&1 | grep -F "<raw-token>" | wc -l   # must be 0
   docker logs <go-server-container> 2>&1 | grep -c "REDACTED_BOT_TOKEN"    # >= 0, present on failures
   ```
2. **Error responses.** The response sanitizer
   (`internal/middleware/sanitizer.go`, `telegramBotTokenRegex`) scrubs any
   echoed token to `[REDACTED_BOT_TOKEN]`. Coverage is unit-tested by
   `TestSecretSanitizer_RedactsTelegramBotToken`
   (`go test ./internal/middleware/ -run Telegram -v`).
3. **Startup log.** The token is never printed at boot; only the mode line
   (`active=true/false`) appears in the `[NOTIF]` banner.

## 9. Rollback / Cleanup

- Remove the staging subscription:
  `curl -X DELETE "https://warded.529studio.site/api/subscriptions" -H "Content-Type: application/json" -d '{"channel_name":"telegram","chat_id":"<chat-id>","target_type":"chokepoint","target_id":"586e2995f9fab7001111b0b5"}'`
- To return to simulation mode, clear `TELEGRAM_BOT_TOKEN` in Stack 14 and
  redeploy; the startup log must show the simulation-mode line.
- Revoke the staging bot token via @BotFather `/revoke` if it was exposed.

## 10. Evidence to Attach When Closing Issue #246

1. Terminal capture of `./scripts/verify_staging_telegram.sh` ending in
   `Result: PASSED`.
2. Screenshot of the delivered Telegram message (bold HTML, bilingual, no
   emojis) with the bot name visible.
3. Log excerpt showing the debounce suppression line during a repeat trigger.
4. Log grep output proving zero raw-token occurrences (Section 8, count 0).
