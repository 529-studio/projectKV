## Lesson 36: Leaf-Modulith Notification Infrastructure, 30-Minute Sliding Debouncing, and Zero-Emoji Telegram Dispatch

- **Incident & Tracking:** Issue #225, Sprint 6 Task S6-T5 (`feat/s6-t5-telegram-flood-alert`).
- **Symptom:**
  Although ADR-001 and earlier architectural blueprints referenced flood alert notifications, an audit revealed that `services/go-server/internal/notify/` had never actually been built in `services/go-server/`. Only dead config fields (`cfg.TelegramBotToken`) remained. Furthermore, if real-time flood alerts were emitted naively on every environmental polling cycle or user verification request, rapid repeat notifications would bombard commuters, destroying user trust and exhausting Telegram API quotas.
- **Root Cause:**
  1. **Missing Leaf Subsystem**: No dedicated notification package existed to adapt flood events into multi-channel alerts.
  2. **Alert Spam Potential**: Gemini VLM verifications run every 60s during rainstorms; identical flood conditions would trigger repeated alerts without a time-window sliding suppression mechanism.
  3. **Modulith Coupling Temptation**: Risk of coupling notification logic to subscription storage or camera catalog directly, violating the Leaf-Modulith architecture pattern (`GO_BACKEND_ARCHITECTURE.md`).
- **Mandatory Takeaway & Engineering Resolution:**
  1. **Strict Leaf-Modulith Boundary & Dependency Inversion**:
     - `internal/notify` is a pure leaf package (zero imports of sibling `internal/` packages). It defines the `Channel` interface (`Send(ctx, alert) error`) and `SubscriberFinder` interface.
     - `internal/subscription` is also a pure leaf package (zero internal imports) backed by raw SQLite SQL (`0006_flood_subscriptions.sql`) for tracking chat IDs mapped to `"all"`, `"chokepoint"`, `"zone"`, or `"ward"`.
     - `cmd/server/main.go` acts as the composition root, adapting `subscription.Store.GetSubscribersForChokepoint` to `notify.SubscriberFinderFunc`.
  2. **Thread-Safe 30-Minute Sliding Debounce**:
     - Built `notify.Debouncer` enforcing a strict 30-minute quiet period per camera/chokepoint. If the same camera reports `FLOODED_SHALLOW` or `FLOODED_DEEP` within 30 minutes, subsequent dispatches are cleanly suppressed while allowing fresh notifications for other locations or after the quiet period expires.
  3. **Rule 1 Monochromatic & Bilingual Zero-Emoji Delivery**:
     - Telegram HTML template completely eschews emojis (`⚠️`, `🚨`, `🌊`, `🌧️` strictly banned), formatting alerts cleanly with high-contrast tags: `<b>[CẢNH BÁO NGẬP NƯỚC / FLOOD ALERT]</b>`.
     - Bilingual presentation (`vi` primary with diacritics, `en` secondary in brackets).
  4. **Rule 8 Token Safety & Sanitization**:
     - `TELEGRAM_BOT_TOKEN` defaults to empty string. If unset in dev, `TelegramChannel` operates in graceful simulation mode, preventing crashes. Any network errors sanitize token substrings (`[REDACTED_BOT_TOKEN]`), preventing credential leaks in logs.
