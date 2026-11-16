# Lesson 478: Telegram Corridor Hazard Subscriptions & Dispatcher — Real-Time Alerting for Commuter Corridors

## Navigation Aid & Quick Reference

- **Domain**: Go leaf-modulith backend, Telegram Bot API, arterial corridor subscriptions, 15-minute sliding-window debouncing, monochromatic alert formatting.
- **Relevant Issues**: Issue #478 (S15-T4), Issue #475 (S15-T1 arterial corridors), Issue #476 (S15-T2 commuter route UI), AGENTS.md Rule 1 (Zero-emoji, monochromatic formatting), Rule 2 (Sound root-cause solutions), Rule 7 (Leaf modulith boundaries), Rule 10 (Empirical verification).
- **Key Files**:
  - `services/go-server/internal/subscription/types.go` (`TargetCorridor TargetType = "corridor"`, `TaxonomyProvider`)
  - `services/go-server/internal/subscription/sqlite.go` (`GetSubscribersForCorridor`, normalized target matching)
  - `services/go-server/internal/telegrambot/deeplink.go` (`sub_corridor_<road_key>` codec)
  - `services/go-server/internal/telegrambot/commands.go` (`/sub corridor <road_key>`, `/unsub corridor <road_key>`, `/my_subs`)
  - `services/go-server/internal/telegrambot/handler.go` (Webhook handler, command dispatch, corridor onboarding report)
  - `services/go-server/internal/notify/alert.go` (`CorridorAlert`, `CorridorSubscriberFinder`)
  - `services/go-server/internal/notify/alert_builder.go` (`BuildCorridorAlert`)
  - `services/go-server/internal/notify/telegram.go` (`FormatTelegramCorridorMessage`, `SendCorridor`)
  - `services/go-server/internal/notify/manager.go` (`DispatchCorridor`, 15-minute corridor debouncer)
  - `services/go-server/internal/store/migrations/0013_add_corridor_subscription.sql` (Expanded CHECK constraint on `target_type`)
  - `services/go-server/cmd/server/corridor_alert_integration_test.go` (Rule 10 verification suite)
- **Core Invariants**:
  - **Zero-emoji monochromatic alerting (Rule 1)**: All outbound Telegram alert templates and bot command replies are strictly text-based, monochromatic, and bilingual (`vi` primary, `en` secondary). Emojis are completely prohibited to prevent telemetry stream corruption and maintain professional credibility.
  - **Leaf modulith decoupling (Rule 7)**: `subscription`, `telegrambot`, and `notify` are strict leaf packages under `services/go-server/internal/`. They never import each other or sibling packages (`corridor`, `camera`). Inter-package communication is strictly mediated via consumer-defined interfaces adapted in `cmd/server/adapters.go`.
  - **15-minute anti-spam sliding debounce window**: Commuters must not be spammed with duplicate notifications every inference cycle. Subsequent hazard updates on the same arterial corridor within 15 minutes are cleanly suppressed.
  - **Empirical verification on live SQLite store (Rule 10 & 4)**: Synthetic unit tests with mocked database tables hide schema constraint mismatches. Real integration testing against migrated SQLite tables uncovered the `target_type IN (...)` CHECK constraint, necessitating migration 0013.

---

## 1. Context & Problem Statement

Commuters travelling along major Ho Chi Minh City arterial corridors (e.g. Võ Văn Kiệt, Mai Chí Thọ, Cộng Hòa) need proactive alerting before departing so they do not unknowingly ride into submerged roads during severe downpours. Following Sprint 15 Wave 1, arterial corridor geometries and passability status were successfully modelled on the backend (Issue #475) and rendered on the web map (Issue #476).

However, web maps require the commuter to actively open the browser and look up their route before riding. Telegram bot push alerts provide universal, proactive, and reliable notification delivery across all smartphones (iOS and Android) without requiring PWA installation or background service worker wake-up permissions.

Expanding the notification system to support arterial corridors required addressing several architectural challenges:
1. **Subscription Taxonomy Expansion**: The existing SQLite subscription schema strictly constrained `target_type` to `all`, `chokepoint`, `zone`, and `ward`. Introducing corridor-level subscriptions required updating the domain model and schema without breaking legacy records or unmigrated instances.
2. **Web-to-Bot Deep-Link Onboarding**: The frontend `CorridorOverviewCard` links commuters directly to `https://t.me/<bot>?start=sub_corridor_<road_key>`. The bot needed to decode the start payload within Telegram's 64-character limit, enroll the user, and immediately report the current passability and water depth of the chosen corridor.
3. **Conversational Command Ergonomics**: Users needed commands to subscribe, check status, and unsubscribe directly in chat (`/sub corridor <road_key>`, `/unsub corridor <road_key>`, `/my_subs`).
4. **Anti-Spam Debounce & Monochromatic Broadcast**: Hazard conditions (water depth $\ge 15\text{ cm}$ or status `IMPASSABLE`/`CAUTION`) must trigger broadcast alerts to subscribed commuters within seconds, but must be throttled with a 15-minute sliding debounce window to avoid spamming drivers. All messages must strictly comply with Rule 1 zero-emoji policy.

---

## 2. Root Cause Analysis & Architectural Implementation

### 2.1. Leaf Modulith Independence (Rule 7)
To prevent circular dependency coupling, the three internal packages remained strictly decoupled:
- `internal/subscription`: Defines `TargetCorridor TargetType = "corridor"`. Implements `GetSubscribersForCorridor` with SQL queries matching both exact corridor subscribers and city-wide (`all`) subscribers. Handles case and hyphen/underscore aliases (`vo-van-kiet` vs `vo_van_kiet`).
- `internal/telegrambot`: Defines `targetTypeCorridor = "corridor"` and parses deep links `sub_corridor_<road_key>`. Communicates with taxonomy and storage only via consumer-defined interfaces (`Store`, `Taxonomy`, and optional `CorridorConditionReporter`).
- `internal/notify`: Defines `CorridorAlert` and `CorridorSubscriberFinder`. Implements `FormatTelegramCorridorMessage` with zero emojis, and `DispatchCorridor` with a 15-minute `corridorDebouncer`.

The composition root in `services/go-server/cmd/server/adapters.go` adapts `corridor.Service` and `subscription.SQLiteStore` to satisfy these interfaces.

### 2.2. Database CHECK Constraint Migration (0013)
During initial testing against the real SQLite store, `store.AssertSchemaReady` and `subStore.Subscribe` failed with:
`CHECK constraint failed: target_type IN ('all', 'chokepoint', 'zone', 'ward')`

In SQLite, `CHECK` constraints on table columns cannot be altered with a simple `ALTER TABLE` statement. Migration `0013_add_corridor_subscription.sql` was implemented to safely transition the table:
```sql
CREATE TABLE IF NOT EXISTS flood_subscriptions (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    channel_name  TEXT NOT NULL,
    chat_id       TEXT NOT NULL,
    target_type   TEXT NOT NULL CHECK (target_type IN ('all', 'chokepoint', 'zone', 'ward', 'corridor')),
    target_id     TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(channel_name, chat_id, target_type, target_id)
);

CREATE TABLE flood_subscriptions_new (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    channel_name  TEXT NOT NULL,
    chat_id       TEXT NOT NULL,
    target_type   TEXT NOT NULL CHECK (target_type IN ('all', 'chokepoint', 'zone', 'ward', 'corridor')),
    target_id     TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(channel_name, chat_id, target_type, target_id)
);

INSERT INTO flood_subscriptions_new (id, channel_name, chat_id, target_type, target_id, created_at)
SELECT id, channel_name, chat_id, target_type, target_id, created_at FROM flood_subscriptions;

DROP TABLE flood_subscriptions;
ALTER TABLE flood_subscriptions_new RENAME TO flood_subscriptions;
```
By adding `CREATE TABLE IF NOT EXISTS flood_subscriptions` at the beginning of the migration, legacy test scenarios that only instantiate isolated tables execute without missing-table errors.

### 2.3. Zero-Emoji Bilingual Alert Formatter (Rule 1)
All corridor alert messages are formatted as monochromatic, clean HTML:
```html
<b>[CẢNH BÁO TUYẾN ĐƯỜNG / CORRIDOR HAZARD ALERT]</b>

<b>Tuyến đường / Corridor:</b> Đại lộ Võ Văn Kiệt
<b>Trạng thái / Passability:</b> [NGẬP SÂU - ÙN TẮC / IMPASSABLE]
<b>Mức nước cao nhất / Peak Depth:</b> 35 cm tại Cầu Rạch Chiếc
<b>Ước tính chậm trễ / Est. Delay:</b> +25 phút / +25 mins
<b>Thời gian / Time:</b> 15:30 16/11/2026 (ICT)
<b>Xem bản đồ lộ trình / Route Map:</b> <a href="https://warded.529studio.site/roads/vo_van_kiet">https://warded.529studio.site/roads/vo_van_kiet</a>
```
All dynamic strings (corridor name, peak location, URL) pass through `escapeHTML` to prevent HTML injection.

---

## 3. Verification & Empirical Evidence (Rule 10)

The comprehensive integration test suite in `corridor_alert_integration_test.go` verified all 5 scenarios from the task specification end-to-end:
1. **Scenario 1**: Bot receives `/sub corridor vo_van_kiet`, saves record in SQLite database, and replies with zero-emoji confirmation and road display name.
2. **Scenario 2**: Web deep link `/start sub_corridor_mai_chi_tho` auto-enrolls the user and delivers immediate initial condition report.
3. **Scenario 3**: High flood depth event (35 cm) on `vo_van_kiet` triggers broadcast alert dispatched to enrolled users within seconds.
4. **Scenario 4**: Second hazard event on `vo_van_kiet` 2 minutes later is cleanly suppressed by the 15-minute sliding debounce window.
5. **Scenario 5**: Bot receives `/unsub corridor vo_van_kiet`, confirms cancellation, and removes record from SQLite database.

All 25 Go backend packages under `services/go-server` pass all unit and integration tests.
