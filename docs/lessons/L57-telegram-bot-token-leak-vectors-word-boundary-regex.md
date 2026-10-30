## Lesson 57: Telegram Bot Token Leak Vectors, Word-Boundary Regex Pitfalls, and Ops Script-to-Go Format Parity

- **Incident & Tracking:** Sprint 8 Wave 3 / Issue #246 / PR targeting `dev` (`ops/s8-t9-staging-telegram-verification`, [KV-21]).
- **Symptom:**
  1. The response secret sanitizer (`internal/middleware/sanitizer.go`) covered Google keys, AWS access keys, and URL query credentials, but had NO pattern for Telegram Bot API tokens — a token echoed back through any HTTP error response would have leaked verbatim.
  2. The staging Telegram channel had only ever run in simulation mode (`TELEGRAM_BOT_TOKEN=""`); no operator procedure existed to prove live delivery, the 30-minute debouncer, or log hygiene on Portainer Stack 14.
- **Root Cause:**
  1. **Secret coverage gap by credential family:** sanitizers tend to be written reactively per incident (Gemini keys after a VLM leak, AWS keys after an R2 incident). Telegram tokens (`<bot_id>:<base64url>`) are a structurally distinct credential family that no prior incident had exercised.
  2. **Regex word-boundary fallacy:** an initial `\b\d{8,10}:...\b` anchor never matched real leak strings because tokens appear glued to `/bot<token>/sendMessage` URL prefixes — `t` and `7` are both word characters, so no `\b` boundary exists at the junction. Go RE2 has no lookbehind, so the leading anchor must simply be dropped.
  3. **Token length variance:** the documented BotFather example token has a 34-character tail, not the commonly assumed 35; strict `{35}` misses real tokens. Observed variance is 33–36 characters.
- **Mandatory Takeaways & Engineering Resolutions:**
  1. **`telegramBotTokenRegex = \d{8,10}:[A-Za-z0-9_-]{33,36}\b`** in `SanitizeBytes` scrubs any echoed token to `[REDACTED_BOT_TOKEN]` across every HTTP response (defense in depth), while the long strict tail preserves clock times (`12:34:56`), ratios (`10:1`), and short tails verbatim — locked by `TestSecretSanitizer_RedactsTelegramBotToken` and `TestSecretSanitizer_PreservesClockTimes`.
  2. **Channel-level redaction retained:** `TelegramChannel.SendTo` already rewrites its own transport errors to `[REDACTED_BOT_TOKEN]`; middleware now covers every other echo path. Redundancy is intentional (two independent layers).
  3. **Ops-script-to-Go format parity via pinned contract test:** `scripts/verify_staging_telegram.sh` composes the alert message in bash; drift is prevented by `TestFormatTelegramMessage_StagingChokepointFixture` pinning the exact canonical HTML for the staging camera. When the Go formatter changes, the failing contract test forces a lockstep script update.
  4. **Deterministic debounce acceptance:** the Issue #246 acceptance step ("second alert within 5 minutes is suppressed") is enforced by `TestNotifyIntegration_DebounceFiveMinuteWindow` (dispatch at T0 delivers; T0+5m suppressed without advancing the sliding-window anchor; T0+31m re-delivers) — live staging observation then only confirms deployment, not semantics.
  5. **Bash ops scripts honor the same engineering bar as production code:** strict mode (`set -euo pipefail`), fail-fast credential checks before any network round-trip, token redaction of curl stderr, JSON assertions via `python3` (never `grep` on JSON booleans — `json.dumps(true)` is lowercase while Python prints `True`), and abort-on-contract-violation before dispatching a non-conforming message.


