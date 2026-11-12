# S12-T4 Specification — Telegram Alert Bot Live Provisioning & Citizen Subscription UI

**Issue**: [#402](https://github.com/529-studio/projectKV/issues/402) · **Sprint**: 12 · **Branch**: `feat/402-telegram-bot-and-web-subscription`
**Owner**: Product Owner · **Consumers**: Lead Developer, Tester, Ops
**Depends on**: #400 (PR [#418](https://github.com/529-studio/projectKV/pull/418) merged), #401 (PR [#430](https://github.com/529-studio/projectKV/pull/430) merged)
**Charter**: `docs/ADR-013-sprint-11-architecture.md` §F5/§D6, `docs/HANDOFF-s12.md` §6, Issue #225 (S6-T5)

---

## 0. Estimation (Rule 3)

| Phase | Scope | Active effort |
|:---|:---|:---:|
| P0 | `telegrambot` payload codec + command parser/renderers (pure, table-tested) | 5–6 h |
| P1 | Webhook: secret, dedupe, queue, workers, per-chat limiter, filtering | 8–10 h |
| P2 | `UnsubscribeAll`, confirmation DM, taxonomy `bot_username`, list hardening, config, swagger | 8–10 h |
| P3 | `subscriptionUtils.ts`, `useSubscriptions.ts`, `SubscriptionModal.tsx`, `page.tsx` | 9–11 h |
| P4 | VI/EN catalog, emoji guard, a11y pass, ops runbook, docs sync | 5–6 h |
| P5 | Live bot provisioning, M1–M12 evidence, PR | 5–7 h |
| **Total** | | **40–50 h active** |

**Delivery window**: Day 26–35 (2026-11-13 → 2026-11-22, 6–8 focused days plus review).

---

## 1. Problem (verified)

1. **Simulation only.** `cmd/server/main.go` logs `TELEGRAM_BOT_TOKEN not configured: Telegram channel registered in simulation mode`; `Send`/`SendTo` return `ErrTelegramTokenMissing`. Formatting already complies (monochromatic, bilingual, zero emoji) — missing are a live token, an inbound endpoint and a citizen surface.
2. **No citizen UI.** No file under `apps/web` references `subscriptions`; the only clients are `curl` and `cmd/subscriptions-audit`.
3. **No inbound surface.** Nothing consumes Telegram updates, so `/start`, `/status`, `/stop` do not exist.
4. **Ready but unused backend.** `GET /api/subscriptions/taxonomy` already serves 4 target types, 8 canonical zones, 101 wards and the camera catalog; the store is idempotent (`ON CONFLICT … DO NOTHING`) and taxonomy-validated.

---

## 2. Decisions (normative)

### 2.1 Deep-link payload grammar

```
payload := "sub_" target
target  := "all" | "zone_" slug | "ward_" slug | "cam_" id
slug    := base64url(UTF-8(id)) without '='      # RFC 4648 §5, unpadded
id (cam) := [A-Za-z0-9_-]{1,40}
```

- Total ≤ **64** chars (Telegram `start` limit), enforced in `EncodePayload` and by a whole-taxonomy table test.
- Prefix match is case-insensitive; the slug is strict (reject `=` padding, trailing-bit variance, invalid UTF-8).
- Targets are validated against the **live taxonomy**: zones via `NormalizeZone` (legacy aliases accepted, stored canonical), wards via the catalog (`Phường `/`Xã `/`Thị trấn ` tolerated, stored bare), chokepoints via `catalog.ByID`. Unknown → guidance reply, **no DB write**.

Worst cases, asserted in `deeplink_test.go`: zone `Khu vực Nội thành TP.HCM` → `sub_zone_S2h1IHbhu7FjIE7hu5lpIHRow6BuaCBUUC5IQ00` (44), zone `Quận 7 - Nhà Bè` → `sub_zone_UXXhuq1uIDcgLSBOaMOgIELDqA` (31), ward `Đông Hưng Thuận` → `sub_ward_xJDDtG5nIEjGsG5nIFRodeG6rW4` (32), cam `591c6d3ba6bfb500119400ac` → `sub_cam_591c6d3ba6bfb500119400ac` (28).

Shared Go/TS vectors: `Bến Nghé → QuG6v24gTmdow6k`, `Bà Rịa - Vũng Tàu → QsOgIFLhu4thIC0gVsWpbmcgVMOgdQ`.

### 2.2 Endpoint and fail-closed activation

`POST /api/notify/telegram/webhook` — under `/api`, so the existing Traefik `PathPrefix('/api')` router serves staging with **no compose label change**.

Registered **only when** `TELEGRAM_BOT_TOKEN`, `TELEGRAM_BOT_USERNAME` and `TELEGRAM_WEBHOOK_SECRET` are all valid. Otherwise the route is absent (404) and startup logs one warning — never an open endpoint, never a failed boot (Rule 8). When registered:

| # | Check | Failure |
|:--:|:--|:--|
| 1 | `X-Telegram-Bot-Api-Secret-Token` matches, `crypto/subtle.ConstantTimeCompare`, length first | `401`, empty body |
| 2 | Body ≤ 64 KiB (`http.MaxBytesReader`) | `413` |
| 3 | Decodable `Update` | `400` |
| 4 | `update_id` unseen | `200 {"ok":true}`, no-op |
| 5 | Queue has room | `503` + `Retry-After: 5` |

No response echoes the secret, the token or the update body.

### 2.3 Pipeline

```
Webhook ─401/413/400─▶ reject
        └─ dedupe(update_id) ─▶ enqueue(cap 256) ─▶ 200 (never waits on Bot API)
                                    │ 4 workers
                                    ▼
                    private text message only
                    ├─ ignore edited_message, callback_query, channel_post, from.is_bot
                    ├─ non-private chat → one guidance reply, no persistence
                    ├─ parse command, strip "@BotUsername"
                    ├─ per-chat bucket 6/min → one "slow down" reply/min, then silent
                    └─ execute → Sender.SendMessage → structured log
```

- **Dedupe** `map[int64]time.Time`, cap 2048, TTL 15 min, oldest evicted; a replay creates neither row nor reply. **Limiter**: the global limiter keys on Telegram's shared egress IPs, so one burst would 429 the bot — exempt the path, limit per chat in the handler.
- **Logging** `chat=<last4>`, `cmd`, `target_type`, `outcome`, `duration_ms` only. **Shutdown** closes the queue, drains ≤5 s, cancels in-flight calls.

### 2.4 Commands (every reply: VI block, blank line, EN block; HTML; zero emoji)

| Input | Behaviour |
|:--|:--|
| `/start` (no payload) | Welcome + active count + 3 example deep links + `/help` hint |
| `/start sub_<target>` | Validate → insert → confirmation naming the target; duplicate → "already active"; unknown → guidance, no row |
| `/help` | `/status`, `/stop`, `/unsubscribe`, web link |
| `/status` | One line per target (`- Toàn thành phố / City-wide`, `- Khu vực / Zone: …`) or "none" |
| `/stop`, `/unsubscribe` | `UnsubscribeAll` → reply with the count (`0` is not an error) |
| `/cmd@BotUsername` | Suffix stripped, case-insensitive |
| Unknown command, plain text | `/help` body, max one reply per chat per 60 s |
| Non-private chat | One reply: private chats only, open a DM |

Cap **20 subscriptions per chat**; the 21st is refused with copy pointing at `/stop`.

### 2.5 Web direct path (`POST /api/subscriptions`)

Contract kept (`channel_name`, `chat_id`, `target_type`, `target_id`, `201`). Additions:

- `chat_id` must match `^[0-9]{5,20}$` here (web users are private chats; `-100…` → `400`).
- Dedicated per-IP limiter on this route only: 5/min, burst 3 → `429` + `Retry-After`.
- On success with `channel_name=telegram`, a best-effort confirmation DM (3 s timeout, errors logged, response unaffected) names the target and `/stop`, so an owner whose id was registered by someone else learns and can revoke it.
- `GET /api/subscriptions` moves behind `RequireDevAuth`: it enumerated any chat id anonymously, unacceptable for a citizen alerting system.

**Accepted risk R1**: the direct path cannot prove chat ownership. Mitigations: strict id format, limiter, owner-visible DM, `/stop`, and the deep link (ownership-proof by construction) is the UI's primary CTA.

### 2.6 Configuration

| Variable | Default | Rules | If empty/invalid |
|:--|:--|:--|:--|
| `TELEGRAM_BOT_TOKEN` | `""` | secret, never logged | dispatch + webhook disabled (today's behaviour) |
| `TELEGRAM_BOT_USERNAME` | `WardedAlertBot` | public handle, **not** a credential; `^[A-Za-z][A-Za-z0-9_]{3,30}$`; `@` stripped | webhook disabled + warning |
| `TELEGRAM_WEBHOOK_SECRET` | `""` | secret; `^[A-Za-z0-9_-]{32,256}$` | webhook disabled + warning |
| `TELEGRAM_DEBOUNCE_MINUTES` | `30` | existing | unchanged |

The username keeps a default because a public handle is not a key (Rule 8 covers keys, tokens, passwords). Exactly one startup line:
`[INIT] Telegram webhook active: bot=@<username> path=/api/notify/telegram/webhook` or
`[INIT] Telegram webhook disabled: <token missing|username invalid|webhook secret missing>`.

### 2.7 Copy rules

`parse_mode: "HTML"`; always escape `& < > "` on interpolated values; severity vocabulary mirrors `FormatTelegramMessage`; timestamps in ICT. Forbidden: emoji, pictographs, box-drawing, markdown emphasis — enforced by a regex guard test over every renderer.

---

## 3. User stories & acceptance criteria

### US-402.1 — Subscribe in three taps
- **AC1** Entry point on desktop and mobile, ≥48 × 48 px, keyboard reachable, `aria-haspopup="dialog"`.
- **AC2** Exactly four scopes — Thành phố (`all`), Khu vực (`zone`, 8 canonical zones), Phường/Xã (`ward`, full taxonomy), Nút giao (`chokepoint`) — never a target absent from the live taxonomy.
- **AC3** The CTA opens `https://t.me/<bot_username>?start=sub_<payload>` in a new tab (`rel="noopener noreferrer"`), payload per §2.1.
- **AC4** If `bot_username` cannot be resolved the CTA is disabled with a stated reason and the manual form still works.

### US-402.2 — The bot is self-service
- **AC5** A valid `/start sub_<target>` persists exactly one row (`channel_name="telegram"`, real chat id, canonical type/id) and confirms by name.
- **AC6** Repeating the same deep link stays at one row, replies "already active", HTTP `200`.
- **AC7** `/status` lists scopes; `/stop` and `/unsubscribe` delete all rows for the chat and report the count.
- **AC8** An invalid/unknown target yields guidance, no row, no 5xx.

### US-402.3 — Ops go live in under 15 minutes, and fail closed
- **AC9** With all three variables set, `setWebhook` succeeds and `getWebhookInfo` reports the staging URL with `pending_update_count: 0`.
- **AC10** Missing/wrong secret → `401` and no row; any variable unset → route 404 and dispatch stays in simulation.
- **AC11** No secret appears in logs, responses, swagger, `.env.example` values or the built bundle.

### US-402.4 — Accessible and bilingual
- **AC12** `role="dialog"` + `aria-modal="true"` + `aria-labelledby`; Escape closes, Tab/Shift+Tab trapped, focus restored, background scroll locked.
- **AC13** Every string exists in VI (full diacritics) and EN; switching language while open re-renders instantly, no refetch, no state loss.
- **AC14** Every target ≥48 × 48 px at 390 px; validation via `role="status"`/`aria-live="polite"` and `aria-describedby`.

### US-402.5 — Honest state
- **AC15** Six modal states: taxonomy loading, taxonomy error (retry), submitting, success, field-scoped validation error, network error. No silent success.
- **AC16** A live verdict reaches each matching subscriber once; a repeat for the same chokepoint inside the debounce window is suppressed and logged `suppressed=true`.

---

## 4. Backend (`services/go-server`)

### 4.1 New leaf package `internal/telegrambot` (imports no `internal/` package)

Files: `deeplink.go` (codec), `commands.go` (pure renderers, clock injected), `handler.go` (HTTP + workers).

- **`deeplink.go`** — `MaxStartPayloadLen = 64`, `PayloadPrefix = "sub_"`, `type Target struct{ Type, ID string }`, `ErrPayloadTooLong|ErrPayloadMalformed|ErrPayloadUnknown`, `EncodePayload(Target) (string, error)`, `DecodePayload(string) (Target, error)`, `SlugEncode(string) string`, `SlugDecode(string) (string, error)`.
- **`commands.go`** — `type Command struct{ Name, Arg string }`, `ParseCommand(text, botUsername)`, renderers `RenderWelcome`, `RenderHelp(webURL)`, `RenderSubscribed/AlreadySubscribed(Target, label)`, `RenderStatus([]Record, Labeler)`, `RenderStopped(n)`, `RenderSlowDown`, `RenderUnknownTarget(raw)`, `RenderLimitReached(max)`, `RenderPrivateChatOnly(bot)`, `RenderDeliveryFailed` (all `string`), `type Labeler interface{ Label(Target) string }`.
- **`handler.go`** — `type Record struct{ TargetType, TargetID string; CreatedAt time.Time }`; `Store` = `Subscribe(ctx, channel, chatID, Record) error` + `UnsubscribeAll(ctx, channel, chatID) (int64, error)` + `List(...) ([]Record, error)`; `Sender` = `SendMessage(ctx, chatID, html string) error`; `Taxonomy` = `CanonicalZone/Ward/Chokepoint(string) (string, bool)` + `Label(Target)`; `Config{BotUsername, WebhookSecret string; Workers, QueueSize, MaxSubsPerChat, PerChatPerMinute int}`; `NewHandler(cfg, Store, Taxonomy, Sender, *slog.Logger)`, `Enabled()`, `RegisterRoutes(*gin.Engine)` (no-op unless enabled), `Shutdown(ctx) error`.

Defaults: 4 workers, queue 256, 20 subscriptions per chat, 6 commands per chat per minute. `Store`/`Sender`/`Taxonomy` are satisfied structurally by adapters in `cmd/server` (DIP), beside `subscriptionTaxonomyAdapter`.

### 4.2 `internal/notify/telegram.go`

```go
func (c *TelegramChannel) SendMessage(ctx context.Context, chatID, html string) error
```

`SendTo` becomes `c.SendMessage(ctx, chatID, FormatTelegramMessage(alert))`, reusing token redaction, the 10 s timeout and `telegramAPIResponse` handling. A `429` retries **once** after `retry_after` (≤2 s); a `403` (blocked bot) is logged and never deletes the subscription.

### 4.3 `internal/subscription`

- `Store` gains `UnsubscribeAll(ctx, channelName, chatID string) (int64, error)` (hand-written `DELETE … WHERE channel_name=? AND chat_id=?`, returns `RowsAffected`).
- `NewHandler(store, taxonomy, opts ...Option)`; options `WithConfirmationRenderer(func(Record) string)`, `WithBotInfo(username string, enabled bool)`, `WithSubscribeLimiter(gin.HandlerFunc)`.
- `RegisterRoutes(rg *gin.RouterGroup, opts ...RouteOption)`; `WithAuditAuth(gin.HandlerFunc)` puts `GET` behind dev auth. `POST`/`DELETE` stay public (citizen-facing).
- `TaxonomyResponse` gains `BotUsername` and `DeepLinkEnabled` (additive). `subscription` must not import `telegrambot`, so confirmation copy arrives as a renderer function.
- Chat-id pattern and limiter live in the handler, not `IsValid`, so historical rows stay readable.

### 4.4 Wiring

`internal/config/config.go`: `ServerConfig` gains `TelegramBotUsername`, `TelegramWebhookSecret`, both `parseEnvString`, validated at load, never defaulted to a credential.

`cmd/server/main.go`: (1) build `devAuth` before the `api` group — it is created later today — and pass it to `subHandler.RegisterRoutes`; (2) after `telegramChan`, build adapters and `telegrambot.NewHandler`, call `RegisterRoutes(router)`, `defer Shutdown`; (3) emit the §2.6 line.

`middleware/ratelimit.go`: add the webhook path to `isExempt`, with a table test. `make swagger` after the route change.

---

## 5. Frontend (`apps/web`)

### 5.1 `src/utils/subscriptionUtils.ts` (pure, no `Buffer`/`btoa`)

```ts
export type TargetType = 'all' | 'zone' | 'ward' | 'chokepoint';
export interface TargetSelection { type: TargetType; id: string }
export interface WardItem { type: string; name: string }
export const MAX_START_PAYLOAD = 64;
export const CHAT_ID_PATTERN = /^[0-9]{5,20}$/;
export const BOT_USERNAME_PATTERN = /^[A-Za-z][A-Za-z0-9_]{3,30}$/;
export function slugEncode(v: string): string;            // UTF-8 → base64url, unpadded
export function slugDecode(slug: string): string | null;
export function buildStartPayload(sel: TargetSelection): string;  // throws if invalid/oversize
export function parseStartPayload(payload: string): TargetSelection | null;
export function normalizeBotUsername(raw?: string | null): string | null;
export function buildTelegramDeepLink(username: string, sel: TargetSelection): string;
export function isValidChatId(v: string): boolean;
export function normalizeSearch(v: string): string;       // lower + strip diacritics
export function filterWards(w: WardItem[], q: string, limit?: number): WardItem[];
export function groupWardsByType(w: WardItem[]): { type: string; wards: WardItem[] }[];
export function describeTarget(sel: TargetSelection, lang: 'vi' | 'en'): string;
export function mapSubmitError(status: number, body: unknown, lang: 'vi' | 'en'): string;
```

`buildStartPayload` emits `sub_all` / `sub_zone_<slug>` / `sub_ward_<slug>` / `sub_cam_<id>`, throwing above 64 chars. `filterWards` is diacritic-insensitive (`phuong ben nghe` → `Bến Nghé`), sorted by code point, capped at 20.

### 5.2 `src/hooks/useSubscriptions.ts`

```ts
export interface SubscriptionTaxonomy {
  target_types: string[]; zones: string[]; wards: WardItem[];
  chokepoints: { id: string; name?: string }[];
  bot_username?: string; deep_link_enabled?: boolean;
}
export interface SubmitResult { ok: boolean; status: number; message?: string }
export function useSubscriptions(): {
  taxonomy: SubscriptionTaxonomy | null; loading: boolean; error: Error | null;
  refetch: () => void;
  submit: (sel: TargetSelection, chatId: string) => Promise<SubmitResult>;
}
```

Lazy fetch on first open (10 s `AbortController`, one manual retry). `submit` posts `{channel_name:'telegram', chat_id, target_type, target_id}` (`''` for `all`) with an 8 s timeout and never throws — failures become `{ok:false, status, message}`. `NEXT_PUBLIC_TELEGRAM_BOT_USERNAME` is a last-resort fallback; the API value wins.

### 5.3 `src/components/SubscriptionModal.tsx`

```ts
interface SubscriptionModalProps {
  isOpen: boolean; onClose: () => void; lang: 'vi' | 'en';
  initialTarget?: TargetSelection;   // default { type: 'all', id: '' }
  botUsernameFallback?: string;
}
```

Header (title + 48 px close) · scope selector (native radio group, arrow-key accessible) · target picker (zone `<select>`; ward search + list; chokepoint search) · Telegram CTA as primary `<a>`, disabled state as `<span role="note">` · disclosure "Nhập Chat ID thủ công" (collapsed) · numeric input + 48 px submit · `role="status"` live region · privacy note. Reuse `NearMeModal.tsx`'s `FOCUSABLE_SELECTOR` trap, Escape, scroll lock, focus restore, `data-*` initial focus, zinc palette and inline SVGs only.

States: `taxonomy-loading` (skeleton), `taxonomy-error` (retry), `ready`, `submitting` (`aria-busy`, inputs disabled), `success`, `validation-error`, `network-error`.

### 5.4 Catalog — `SUBSCRIPTION_I18N` in `src/config/constants.ts`

30 keys, each `{ vi, en }`: `TITLE`, `SUBTITLE`, `SCOPE_ALL|ZONE|WARD|CHOKEPOINT`, `ZONE_LABEL`, `WARD_LABEL`, `CHOKEPOINT_LABEL`, `ZONE_PLACEHOLDER`, `SEARCH_WARD`, `SEARCH_CHOKEPOINT`, `TELEGRAM_CTA`, `TELEGRAM_HINT`, `BOT_UNAVAILABLE`, `MANUAL_TOGGLE`, `CHAT_ID_LABEL`, `CHAT_ID_HINT`, `CHAT_ID_INVALID`, `SUBMIT`, `SUBMITTING`, `SUCCESS_TITLE`, `SUCCESS_BODY`, `ALREADY_HINT`, `ERROR_NETWORK`, `ERROR_RATE_LIMIT`, `ERROR_UNKNOWN_TARGET`, `RETRY`, `CLOSE`, `OPEN_TELEGRAM`, `PRIVACY_NOTE` (31 with the grouped `SCOPE_*` expanded).

Normative sample: `TELEGRAM_CTA` = `Mở Telegram để đăng ký` / `Open Telegram to subscribe`; `CHAT_ID_INVALID` = `Chat ID chỉ gồm 5–20 chữ số.` / `Chat ID must be 5–20 digits.`; `PRIVACY_NOTE` = `Chúng tôi chỉ lưu Chat ID và phạm vi bạn chọn. Gửi /stop để huỷ bất cứ lúc nào.` / `We store only your chat ID and chosen scope. Send /stop to unsubscribe at any time.`

`API_ENDPOINTS` gains `SUBSCRIPTIONS: '/api/subscriptions'` and `SUBSCRIPTIONS_TAXONOMY: '/api/subscriptions/taxonomy'`.

### 5.5 `src/app/page.tsx`

State `subscriptionOpen`, `subscriptionTarget` (default `all`). Desktop: 48 px bell in the `weatherControl` stack, before the near-me button. Mobile: 48 px bell in the top row between search trigger and `WeatherWidget` — not in the bottom gutters already owned by `near-me` and `MobileCameraDrawer`. Render the modal once beside `NearMeModal`; opening either closes the other.

---

## 6. Edge cases (normative)

| # | Case | Required behaviour |
|:--|:--|:--|
| E1 | Payload >64 chars or unslugged target (`sub_zone_quận 7`) | `EncodePayload` errors; UI cannot produce it (max 44); webhook answers guidance naming `/help`, never a 500 |
| E2 | Legacy alias inside a valid slug | `NormalizeZone` → `Quận 7 - Nhà Bè`, stored canonical |
| E3 | Ward with/without `Phường ` prefix | Both resolve, stored bare, `/status` prints the bare name |
| E4 | Unknown camera id | Guidance, no row, `200` |
| E5 | Duplicate `/start`, replayed `update_id` | One row, one "already active" reply, both `200` |
| E6 | 21st subscription | Refused with limit copy + `/stop` hint |
| E7 | Group chat / channel | One guidance reply, nothing persisted |
| E8 | Bot blocked (`403`), Telegram `429` | Logged, row kept; one `429` retry after `retry_after` ≤2 s |
| E9 | Queue full | `503` + `Retry-After: 5` so Telegram retries |
| E10 | Missing/wrong secret, names containing `& < > "` | `401` with empty body and no DB access; names escaped in replies and `/status` |
| E11 | Taxonomy fetch fails / no `bot_username` | `taxonomy-error` + retry; CTA disabled, manual form usable |
| E12 | Double submit, language switch mid-submit, reduced motion | Second click suppressed; request survives the switch; no sheet animation |

---

## 7. Ops runbook

```bash
openssl rand -hex 32                     # → TELEGRAM_WEBHOOK_SECRET (never commit)
source .env
curl -sS -X POST "https://api.telegram.org/bot${TELEGRAM_BOT_TOKEN}/setWebhook" \
  -H 'Content-Type: application/json' \
  -d "{\"url\":\"https://warded-stg.529studio.site/api/notify/telegram/webhook\",
       \"secret_token\":\"${TELEGRAM_WEBHOOK_SECRET}\",
       \"allowed_updates\":[\"message\"],\"drop_pending_updates\":true,\"max_connections\":40}"
curl -sS "https://api.telegram.org/bot${TELEGRAM_BOT_TOKEN}/getWebhookInfo"   # url set, pending 0
# local dev without a public URL: deleteWebhook?drop_pending_updates=true
```

- **Rotation**: `setWebhook` accepts one secret, so it is `setWebhook(new)` → restart, with an announced ≤5-minute `401` window; never leave the route open while rotating. `scripts/verify_staging_telegram.sh` must run **after** a chat is subscribed, else it reports zero recipients.
- Compose/env placeholders (none may hold a real value): `docker-compose.stg.yml` gains `TELEGRAM_BOT_TOKEN`, `TELEGRAM_BOT_USERNAME=${TELEGRAM_BOT_USERNAME:-WardedAlertBot}`, `TELEGRAM_WEBHOOK_SECRET`; `.env.example` gains `TELEGRAM_BOT_USERNAME=WardedAlertBot` and `TELEGRAM_WEBHOOK_SECRET=<your-telegram-webhook-secret>`.

---

## 8. Tester plan

### 8.1 Automated gates

- **Go unit** — `go test ./internal/telegrambot/... ./internal/subscription/... ./internal/notify/... ./internal/middleware/...`: codec vectors, 64-char bound over the taxonomy, command parsing, emoji-free renderers, `401`, dedupe, `503`, `UnsubscribeAll`, confirmation DM, `isExempt`.
- **Go build** — `go vet ./... && go build ./...`: `telegrambot` imports no `internal/` package.
- **FE unit** — `npm test` (pure `.test.ts` only): TS vectors equal Go byte-for-byte, ward filter, chat-id validation, error mapping.
- **FE static** — `npx tsc --noEmit && npm run lint && npm run build`: no new dependency, no type escape.
- **Swagger / secrets** — `make swagger`; `git grep -nE '[0-9]{8,10}:[A-Za-z0-9_-]{33,36}'` and a grep of `.next/static` must both be empty.

### 8.2 Manual — Rule 10 (live `make dev-all` + real bot)

| ID | Scenario / evidence |
|:--|:--|
| M1 | Webhook missing/wrong/correct secret → `401`/`401`/`200`; unset secret + restart → `404`; no row written |
| M2 | `/start sub_all` from a real account → VI+EN confirmation DM, one row `('telegram', <chat>, 'all', '')`, log without message text |
| M3 | Zone deep link (`Khu vực Nội thành TP.HCM`, 44 chars) → canonical row; alias payload normalizes |
| M4 | Ward deep links (`Bến Nghé`, `Đông Hưng Thuận`) → stored bare, correct diacritics |
| M5 | `sub_cam_<valid>` → row; `sub_cam_does_not_exist` → guidance, no row, `200` |
| M6 | `/help`, `/status` with 2 rows, plain text, `/foo`, `/status@WardedBot` → five correct replies |
| M7 | `/stop` then `/unsubscribe` → `2` then `0`; `/status` empty; second `/stop` not an error |
| M8 | Replayed `update_id` → one row, one reply; malformed JSON → `400`; 70 KiB → `413` |
| M9 | Desktop bell opens the modal; CTA href equals `https://t.me/<bot>?start=sub_…`; repeated at 390 × 844 |
| M10 | Tab trapped, Shift+Tab wraps, Escape closes and restores focus, `aria-modal`; targets ≥48 px measured |
| M11 | VI ↔ EN across all six states → no refetch, no state loss, diacritics intact; emoji sweep empty |
| M12a | Direct form: 9-digit id → `201`, DM arrives, `/status` shows it; `abc` / `-100…` → field error; 6 submits → `429` |
| M12b | Live dispatch: one verdict → one alert per subscriber; repeat inside the window (`TELEGRAM_DEBOUNCE_MINUTES=1`) suppressed; after it, delivered |

Evidence in `docs/reports/s12_t4_manual_evidence.md` + `docs/proofs/s12_t4/`: redacted webhook transcripts, Telegram and modal screenshots in both languages, 48 px overlay, SQLite `SELECT`. Real bot, real chat, real verdicts — no fixture, no mock sender (Rule 10).

### 8.3 Definition of Done

- [ ] Every §4/§5 contract exists; `telegrambot` imports no `internal/` package; no new dependency.
- [ ] Fail-closed webhook: 404 unprovisioned, 401 bad secret, 200 on accepted/ignored/duplicate; payloads match the shared Go/TS vectors and the 64-char bound.
- [ ] Modal: six states, focus trap, Escape, restore, `aria-modal`, 48 px, VI default with diacritics, zero emoji.
- [ ] `POST /api/subscriptions` parity verified, `GET` behind dev auth, R1 documented; `.env.example` + `docker-compose.stg.yml` placeholders only.
- [ ] Runbook executed on staging, `make swagger` run, docs synced (`README.md`, lessons if one emerges).
- [ ] PR starts with `Close #402`, targets `dev`, follows the PR template; no auto-merge.
