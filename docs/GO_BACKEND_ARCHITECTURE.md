# GO BACKEND ARCHITECTURE — Implementation Brief for Coding Agents

**Purpose of this document**: paste this whole file to a coding agent (Claude Code, or any agent with file/bash access) inside the repo root. It contains everything needed to scaffold the directory structure, write all remaining Go source files, configure git hooks, CI, and a project skill — without further architectural decisions needed from a human. Two files referenced below (`hotset/scheduler.go`, `notify/channel.go`, `notify/telegram.go`) already exist and are attached separately — **do not regenerate them, only reference their interfaces.**

Read this entire document before writing any code. Follow the module boundary rules in §2 strictly — they are not suggestions.

---

## 0. Context (do not skip)

This is an evolution of an existing HCMC traffic-camera viewer (`apps/web`, vanilla JS/Leaflet frontend, unchanged by this work). The Go backend replaces a previous Express.js service and adds real-time flood alerting. Related documents already in the repo, read them first if present:
- `docs/SYSTEM_DESIGN.md` — original caching & CV roadmap
- `docs/ADR-001-flood-alert-architecture.md` — flood-alert feasibility, notification channel decision, ADR-002 (why Go)
- `AGENTS.md` — existing project-wide rules (monochromatic UI, i18n, branch protection, English-only CI/docs). **These rules still apply and are extended, not replaced, by this document.**

---

## 1. Framework & Library Decisions (final — do not re-litigate)

| Concern | Choice | Notes |
|---|---|---|
| HTTP framework | **Gin** (`github.com/gin-gonic/gin`, latest v1.1x) | Pure Go, no cgo. Route grouping per module, binding/validation tags for webhook payloads. |
| DB driver | `modernc.org/sqlite` | Pure Go, no cgo — preserves single static binary / scratch Docker image goal. |
| WebSocket | `nhooyr.io/websocket` | Context-based, modern, minimal API. |
| Config | Plain `os.Getenv` + a typed `Config` struct with defaults | No Viper/koanf — not needed at this scale. |
| Migrations | Hand-rolled: embedded `.sql` files (`embed.FS`) + a `schema_migrations` table, applied in order at startup | No `golang-migrate` — overkill for a handful of tables. |
| Logging | `log/slog` (stdlib) | No zerolog/zap — stdlib is sufficient. |
| Linting | `golangci-lint` with `govet`, `staticcheck`, `errcheck`, `gofmt` enabled | Config file spec in §7. |
| Go version | 1.23+ | Use generics/stdlib routing improvements freely. |

**Explicit non-goals** (do not add these unless a human asks): ORM (GORM/ent), Viper, Cobra CLI framework, microservices split beyond `go-server` + `cv-engine`, Kubernetes manifests, message broker (Kafka/NATS/RabbitMQ) — the scale does not justify any of these yet.

---

## 2. Modulith Principles (mandatory — read carefully)

This is a **modular monolith**: one deployable binary, internally split into packages with strict, enforced boundaries — so it can later be split into real services without a rewrite, but isn't prematurely distributed today.

### Rule 1 — Dependency Inversion at every boundary
When module A needs a capability from module B, **define the interface in A** (the consumer) and **implement it in B** (the producer), then wire the concrete type together only in `cmd/api/main.go`. This is already the pattern in the two files provided:
- `hotset.Analyzer` is defined in `hotset`, implemented in `cvclient`.
- `notify.SubscriptionStore` is defined in `notify`, implemented in `store`.

Follow this pattern for every new cross-module dependency. **No module should import a "God package."**

### Rule 2 — Allowed import graph
```
cmd/api        → may import anything (composition root only — wiring, no business logic)
httpserver     → camera, proxycache, hotset, cvclient, subscription, notify, ws (for route registration only)
camera         → (leaf — no internal imports)
proxycache     → camera
hotset         → camera            (defines Analyzer interface itself; does NOT import store or cvclient)
cvclient       → hotset, camera    (implements hotset.Analyzer)
notify         → i18n              (defines SubscriptionStore interface itself; does NOT import store)
subscription   → store, camera, notify
store          → camera            (implements notify.SubscriptionStore + verdict repo interfaces defined by hotset/httpserver as needed)
ws             → hotset            (for verdict types only)
i18n           → (leaf)
config         → (leaf)
platform       → (leaf — logger setup)
```
If you find yourself needing an import not in this table, stop and reconsider the design — most likely you need an interface, not a direct import.

### Rule 3 — Package = module boundary
Everything lives under `internal/` (enforced by the Go compiler — nothing outside this repo can import these packages, which is intentional at this stage). Each top-level folder under `internal/` is one module. A module owns its own types; do not reach into another module's internal structs.

---

## 3. Final Directory Structure

```
services/go-server/
├── cmd/
│   └── api/
│       └── main.go                    # composition root: load config, build all modules, wire, start
├── internal/
│   ├── config/
│   │   └── config.go                  # Config struct + LoadFromEnv()
│   ├── platform/
│   │   └── logger.go                  # slog.Logger setup (JSON in prod, text in dev)
│   ├── httpserver/
│   │   ├── server.go                  # gin.Engine setup, global middleware, graceful shutdown
│   │   └── router.go                  # top-level route grouping, calls each module's RegisterRoutes
│   ├── camera/
│   │   ├── catalog.go                 # loads cameras_all.json at startup, in-memory slice/map, domain type Camera
│   │   └── handler.go                 # GET /api/cameras, GET /api/cameras/:id, GET /api/zones
│   ├── proxycache/
│   │   ├── cache.go                   # TTL cache: map[string]cacheEntry + sync.RWMutex, singleflight to collapse concurrent misses
│   │   └── handler.go                 # GET /api/snapshot/:id — proxies to upstream DOT portal, serves from cache
│   ├── hotset/
│   │   ├── scheduler.go               # [ALREADY PROVIDED — paste as-is, do not regenerate]
│   │   └── handler.go                 # GET /api/traffic-status, GET /api/flood-status (reads latest verdicts from a small in-memory or store-backed cache)
│   ├── cvclient/
│   │   ├── analyzer.go                # shared HTTP client setup for the analyzers below
│   │   ├── mock.go                    # MockAnalyzer: deterministic fake verdicts for local dev/testing
│   │   ├── cloudvision.go             # CloudVisionAnalyzer: calls Gemini/OpenAI multimodal API, implements hotset.Analyzer
│   │   └── cvengine.go                # CVEngineAnalyzer: calls Python cv-engine HTTP API (Phase 2 self-hosted model), implements hotset.Analyzer
│   ├── notify/
│   │   ├── channel.go                 # [ALREADY PROVIDED — paste as-is]
│   │   ├── telegram.go                # [ALREADY PROVIDED — paste as-is]
│   │   ├── telegram_webhook.go        # inbound: POST /telegram/webhook, parses /subscribe <zone> command, calls subscription.Service
│   │   ├── zalo.go                    # Phase 2 stub: Channel implementation returning "not implemented" error, ready to fill in once OA is approved
│   │   └── webpush.go                 # stub Channel implementation for later
│   ├── subscription/
│   │   ├── service.go                 # Subscribe(ctx, channel, recipientID, zone) — validates zone against camera.Catalog, upserts via store
│   │   └── handler.go                 # POST /api/subscriptions (for web-push based subscribe flow), DELETE /api/subscriptions/:id
│   ├── store/
│   │   ├── sqlite.go                  # db *sql.DB setup, runs embedded migrations on startup
│   │   ├── migrations/
│   │   │   └── 0001_init.sql          # see §5 DDL
│   │   ├── subscription_repo.go       # implements notify.SubscriptionStore
│   │   └── verdict_repo.go            # persists latest verdict per camera; implements whatever interface hotset/handler.go needs for GET /api/traffic-status
│   ├── ws/
│   │   └── hub.go                     # WebSocket hub: register/unregister clients, broadcast verdict updates to map clients in real time
│   └── i18n/
│       └── messages.go                # vi/en message templates for alerts (shared by notify channels) — do not let this drift from apps/web's i18n strings; keep both in sync manually until a shared source is set up
├── go.mod
├── go.sum
├── Dockerfile                          # multi-stage build → scratch/distroless final image (no cgo dependency, so this works cleanly)
└── .golangci.yml
```

Also update at repo root:
```
Makefile                # add: dev-go, build-go, test-go, lint-go targets; remove/deprecate dev-node once migration verified
.githooks/pre-commit     # NEW — gofmt + go vet check before commit (see §7)
.github/workflows/go-ci.yml   # NEW — build/vet/lint/test on PR (see §7)
.claude/skills/go-modulith/SKILL.md   # NEW — see §8
```

---

## 4. HTTP API Contract

| Method | Path | Module | Purpose |
|---|---|---|---|
| GET | `/healthz` | httpserver | liveness/readiness probe |
| GET | `/api/cameras` | camera | full camera catalog (supports query params for the chained filter: zone, corridor) |
| GET | `/api/cameras/:id` | camera | single camera metadata |
| GET | `/api/zones` | camera | list of valid zone identifiers, used to validate subscription requests |
| GET | `/api/snapshot/:id` | proxycache | cached proxied snapshot image, also calls `hotset.Registry.Touch` for that camera |
| GET | `/api/traffic-status` | hotset | latest congestion verdicts, all Hot Set cameras |
| GET | `/api/flood-status` | hotset | latest flood verdicts, filtered to flood-relevant cameras |
| POST | `/api/subscriptions` | subscription | body: `{channel, recipientId, zone}` — used by web-push flow |
| DELETE | `/api/subscriptions/:id` | subscription | unsubscribe |
| POST | `/telegram/webhook` | notify | Telegram Bot API webhook receiver, parses commands |
| GET | `/ws/live` | ws | WebSocket upgrade, pushes verdict updates to connected map clients |

All JSON responses use `snake_case` keys to match the existing `cameras_all.json` dataset convention — check that file's existing key casing before finalizing struct tags, and match it exactly rather than introducing a second casing convention.

---

## 5. Database Schema (SQLite) — `internal/store/migrations/0001_init.sql`

```sql
CREATE TABLE IF NOT EXISTS schema_migrations (
    version     INTEGER PRIMARY KEY,
    applied_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS subscriptions (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    channel_name  TEXT NOT NULL,       -- "telegram" | "zalo" | "webpush" | "sms"
    recipient_id  TEXT NOT NULL,       -- platform-specific identifier
    zone          TEXT NOT NULL,
    created_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(channel_name, recipient_id, zone)
);
CREATE INDEX IF NOT EXISTS idx_subscriptions_zone ON subscriptions(zone);

CREATE TABLE IF NOT EXISTS alert_history (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    channel_name  TEXT NOT NULL,
    recipient_id  TEXT NOT NULL,
    zone          TEXT NOT NULL,
    severity      TEXT NOT NULL,
    sent_at       TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_alert_history_lookup ON alert_history(channel_name, recipient_id, zone, sent_at);

CREATE TABLE IF NOT EXISTS camera_verdicts (
    camera_id   TEXT PRIMARY KEY,
    severity    TEXT NOT NULL,
    confidence  REAL NOT NULL,
    updated_at  TIMESTAMP NOT NULL
);
```

`HasRecentAlert` (required by `notify.SubscriptionStore`) queries `alert_history` for a matching row within a configurable cooldown window (`DEDUPE_COOLDOWN_MINUTES`, see §6). `RecordAlert` inserts a row.

---

## 6. Configuration — `internal/config/config.go`

| Env var | Default | Used by |
|---|---|---|
| `PORT` | `3000` | httpserver |
| `SNAPSHOT_UPSTREAM_BASE_URL` | (required, no default) | proxycache |
| `SNAPSHOT_CACHE_TTL_SECONDS` | `10` | proxycache |
| `HOTSET_INTERVAL_SECONDS` | `30` | hotset scheduler |
| `HOTSET_MAX_PARALLEL` | `10` | hotset scheduler |
| `HOTSET_ACTIVE_TTL_SECONDS` | `120` | hotset registry |
| `TELEGRAM_BOT_TOKEN` | (required if Telegram enabled) | notify |
| `CLOUD_VISION_API_KEY` | (required if CloudVisionAnalyzer used) | cvclient |
| `CV_ENGINE_URL` | `http://localhost:8001` | cvclient (Phase 2) |
| `SQLITE_PATH` | `./data/camera-view.db` | store |
| `DEDUPE_COOLDOWN_MINUTES` | `15` | store (alert_history lookup) |
| `LOG_LEVEL` | `info` | platform/logger |

Fail fast at startup (log + `os.Exit(1)`) if a required var for an enabled feature is missing — do not silently disable a channel.

---

## 7. Git Hooks & CI (extends `AGENTS.md`, does not replace it)

The existing two-tier branch protection (`.githooks/pre-push` + `branch-guard.yml`) stays as-is. Add:

**`.githooks/pre-commit`** (new):
```bash
#!/usr/bin/env bash
set -euo pipefail
cd services/go-server
UNFORMATTED=$(gofmt -l .)
if [ -n "$UNFORMATTED" ]; then
  echo "Go files not formatted (run gofmt -w):"
  echo "$UNFORMATTED"
  exit 1
fi
go vet ./...
```
Wire this into `make install`/`make setup-hooks` alongside the existing pre-push hook, same as the current pattern for `.githooks/pre-push`.

**`.github/workflows/go-ci.yml`** (new, English-only per `AGENTS.md` §4):
```yaml
name: go-ci
on:
  pull_request:
    branches: [dev, main]
jobs:
  build-and-test:
    runs-on: ubuntu-latest
    defaults:
      run:
        working-directory: services/go-server
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.23"
      - run: go build ./...
      - run: go vet ./...
      - uses: golangci/golangci-lint-action@v6
        with:
          working-directory: services/go-server
      - run: go test ./... -race -cover
```

**`.golangci.yml`**:
```yaml
linters:
  enable:
    - govet
    - staticcheck
    - errcheck
    - gofmt
run:
  timeout: 3m
```

---

## 8. Claude Code Skill — `.claude/skills/go-modulith/SKILL.md`

Create this so any future agent session in this repo automatically loads the module-boundary discipline instead of re-deriving it:

```markdown
---
name: go-modulith-conventions
description: Use whenever adding, modifying, or reviewing Go code under services/go-server. Enforces the modulith import rules and package responsibilities defined in docs/GO_BACKEND_ARCHITECTURE.md.
---

# Go Modulith Conventions for services/go-server

Before writing or modifying any file under `services/go-server/internal/`:
1. Re-check the allowed import graph in `docs/GO_BACKEND_ARCHITECTURE.md` §2. If your change requires an import not listed there, define an interface in the consuming package instead of importing the producing package directly.
2. Every package under `internal/` is a module boundary — do not reach into another package's unexported types.
3. Run `gofmt -w` and `go vet ./...` before considering a change complete.
4. Do not add new third-party dependencies without checking §1 of the architecture doc first (framework/library decisions are final unless a human explicitly revisits them).
```

---

## 9. Execution Checklist (do in this order)

1. Scaffold the directory tree in §3 (empty files where noted).
2. `go mod init` inside `services/go-server`, add dependencies from §1.
3. Copy the three already-provided files (`hotset/scheduler.go`, `notify/channel.go`, `notify/telegram.go`) into their listed paths unchanged.
4. Implement `config`, `platform/logger` (leaf packages, no dependencies — do these first).
5. Implement `camera` (leaf).
6. Implement `store` (migrations + both repos), then `i18n`.
7. Implement `cvclient` (mock analyzer first, so the scheduler can run end-to-end before any external API key is configured).
8. Implement `proxycache`, `hotset/handler.go`, `notify/telegram_webhook.go`, `subscription`, `ws`.
9. Implement `httpserver` (wires all `RegisterRoutes` calls) and `cmd/api/main.go` (composition root — construct every concrete type, inject interfaces, start the scheduler goroutine and the Gin server, handle graceful shutdown on SIGINT/SIGTERM).
10. Write the git hooks, CI workflow, `.golangci.yml`, and the skill file from §7–8.
11. Update root `Makefile` with `dev-go`, `build-go`, `test-go`, `lint-go`.
12. Write the `Dockerfile` (multi-stage, `CGO_ENABLED=0`, final stage `scratch` or `gcr.io/distroless/static`).
13. Run `make lint-go && make test-go && make build-go` and fix everything before considering this done.

Do not skip ahead to step 9 before the leaf packages compile — the whole point of the modulith boundaries is that each package should be independently compilable and testable in isolation.
