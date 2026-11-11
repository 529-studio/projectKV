# [ops + forecast] S12-T1: Automated Cron & Sidecar Scheduling for Forecast Live Refit Pipeline

**Labels**: `ops`, `backend`, `sprint-12`, `wave-0`
**Charter**: `docs/reports/sprint11_report.md` §7 item 5 / `docs/HANDOFF-s11.md` §6 item 1
**Baseline**: S11-T4 (#398, PR #409, L80) — `tools/forecast/train_live_model.py` merged to `dev`

---

## Estimation (AGENTS.md Rule 3)

| Phase | Scope | Active Effort |
|---|---|:---:|
| **P0** | Scheduler core: modes, `--interval`, `--schedule-hours`, `fcntl` lock, signals, exit codes | 10–12 h |
| **P1** | State + telemetry contract, `--status`, `--health` | 4–5 h |
| **P2** | Tests: unit + subprocess E2E (lock contention, SIGTERM mid-cycle, slot dedupe, gate isolation) | 7–9 h |
| **P3** | Staging sidecar, shared-volume artifact path, backend env wiring | 4–5 h |
| **P4** | `deploy/systemd/` service + timer + installer + `systemd-analyze` validation | 4–5 h |
| **P5** | `deploy/k8s/` CronJob + stdlib manifest guard | 3–4 h |
| **P6** | Serving pickup: bounded mtime reload, fail-closed last-known-good | 8–10 h |
| **P7** | Manual E2E evidence, runbook, lesson, README parity, PR | 6–8 h |
| **Total** | | **42–53 h active** |

- **Expected Delivery Window**: 2026-11-12 → 2026-11-20 (6–7 focused working days, plus review buffer).
- **If D-A is deferred**: **34–43 h active** (P6 dropped).
- **Dependencies**: none blocking; S11-T4 is merged and its 18 tests are the compatibility contract.
- **Blocks**: S12-T6 (rain/flood features), S12-T7 (corridor propagation).

---

## Problem

Sprint 11 shipped `tools/forecast/train_live_model.py` as a **single-run CLI**: it extracts features, fits Ridge, evaluates the persistence gate, and atomically promotes `prototype_v1.json` — but a human or CI must invoke `make train-forecast`. Nothing refits the live model as soak data accumulates in `camera-view.db`.

1. The promoted `2026.11.10-live` weights are frozen at the 2026-11-03 morning slice (38 bins / 58 cameras): +0.0478% margin at 10 m, −7.94% degradation at 30 m on N=57 (L80 §2). Without periodic refits that baseline never improves and the accepted "initial baseline pending soak" risk never resolves.
2. The persistence gate is the only protection on serving quality and runs only when someone remembers — in staging never, in production "after an incident".
3. There is no scheduling, overlap protection, shutdown semantics, durable state, or telemetry, so nobody can answer "did the model refit last night, and did it pass?" without host access, and two operators (or a human plus CI) can run the pipeline concurrently against one output path with no defined `SIGINT` behaviour. `atomic_write` protects file integrity, not scheduling integrity.

`make train-forecast` stays the correct **workstation / PR-artifact** entry point. It is not a scheduler.

---

## Architectural Reality Check (scope decision D-A)

| Fact | Evidence | Consequence |
|---|---|---|
| The Go server **embeds** `prototype_v1.json` at compile time | `internal/forecast/artifact.go:16` `//go:embed` | A container-side rewrite of that path changes nothing at runtime |
| `FORECAST_MODEL_PATH` is read **once at boot** | `cmd/server/main.go:900` → `service.go:110` | A promoted artifact is served only after a restart |
| Staging deploys by stack redeploy of `docker-compose.stg.yml` | `.github/workflows/docker-publish.yml` | Restarting the backend per refit means container churn every cycle |

**Decision D-A (in scope, ratify at kickoff):** deliver the scheduled refit **and** the serving pickup path, because "the live model updates periodically" is unachievable without both:

1. Refit promotes to a **shared volume** (`/app/data/forecast/prototype_live.json`), not the read-only repo tree.
2. Backend sets `FORECAST_MODEL_PATH` to that path.
3. `internal/forecast` gains a **bounded, fail-closed mtime reload** (`FORECAST_MODEL_RELOAD_SECONDS`, default `60`, `0` disables): re-read, run the existing `LoadArtifactFromBytes` validation, swap under a `sync.RWMutex` only on success; on failure keep the last-known-good artifact and log `forecast: artifact reload rejected`.

If the human defers D-A, the runbook must state the redeploy-after-promotion step and AC11/AC12 are dropped. Silent inertness is not an acceptable fallback.

---

## Solution Architecture

### A1 — In-process scheduler (`tools/forecast/train_live_model.py`)

Split `run(argv)` into `run_pipeline(args) -> int` (today's behaviour, same exit codes), `run(argv)` (parse, validate, dispatch), and the thin wrappers `run_once`, `run_daemon`, `print_status`, `run_health`.

**Backward compatibility is mandatory:** no mode flag equals today's single run. Do not make `--daemon`/`--once` a required group; `make train-forecast` and the 18 existing tests pass unmodified.

| Flag | Default | Mode | Semantics |
|---|---|---|---|
| `--daemon` | off | mode | Run continuous scheduled refit loop; mutually exclusive with `--verify` |
| `--verify` | off | mode | Verify artifact schema and performance gate; mutually exclusive with `--daemon` |
| `--interval` | `21600` (6h) | daemon | Cadence in seconds on monotonic schedule; overruns skipped without drift |
| `--schedule-hours` | none | daemon | Comma-separated `HH:MM` slots in `--timezone`; deduped and sorted |
| `--lock-file` | `var/run/forecast_refit.lock` | all | Cooperative `fcntl.flock` file; anchored to repo root; never unlinked |
| `--state-file` | `var/run/forecast_refit_state.json` | daemon | Durable state file; schema versioned with atomic write & quarantine |
| `--fail-on-locked` | off | all | When lock is held, exit code 4 (`EXIT_FAIL_ON_LOCKED`); else log and exit 0 |
| `--min-new-rows` | `0` (or `50` in stg) | daemon | Gating threshold: skip refit cycle if fewer new rows accumulated |
| `--max-runs` | none (unbounded) | daemon | Maximum cycle count before orderly termination |
| `--timezone` | `Asia/Ho_Chi_Minh` | daemon | Timezone for slot evaluation |
| `--promote` | off | cycle/daemon | Atomically promote model artifact if verification gate passes |
| `--artifact-out` | `data/forecast/prototype_v1.json` | all | Output path for promoted model artifact |

`--daemon` needs exactly one of `--interval` / `--schedule-hours`; both or neither is `EXIT_CONFIG_ERROR`. **Exit codes:** keep `0/1/2/3`; add `EXIT_LOCKED = 4`, `EXIT_INTERRUPTED = 130`, `EXIT_TERMINATED = 143`.

**Locking (root cause, not a PID file):**

- `os.open(path, O_CREAT | O_RDWR, 0o644)` + `fcntl.flock(fd, LOCK_EX | LOCK_NB)`, descriptor held for the process lifetime; the kernel releases it on any death, including `SIGKILL`.
- **Never `unlink()` the lock file** — a second process would lock a fresh inode (split-brain). Write `{"pid":…,"started_at":…}` into the descriptor, then `fsync`.
- Contention: one-shot exits `EXIT_LOCKED`; a daemon logs, increments `cycles_skipped_locked`, and waits for the next slot.

**Signals:** handle `SIGINT`/`SIGTERM`, restoring previous handlers in `finally` (mirror `tools/harvester/service.py:1236–1385`). The first signal sets a stop flag and the loop waits in ≤ 1 s slices; an **in-flight cycle finishes** (writes are atomic, so no partial artifact is possible). A second signal aborts with `130`/`143` and records `aborted: true`. Sleep arithmetic is `time.monotonic()`, never `datetime`.

**Cycle isolation:** `EXIT_GATE_FAILED` or `EXIT_DATA_ERROR` never terminates the daemon — record it, increment `consecutive_failures`, continue. Only startup config errors or `--max-consecutive-failures` end the process.

**Idempotence:** a byte-identical artifact (same SHA-256) is not rewritten; the cycle records `status: "unchanged"`.

**Zero new dependencies:** `argparse`, `fcntl`, `signal`, `json`, `zoneinfo`, `datetime` only — no `croniter`, `schedule`, or APScheduler.

### A2 — State and telemetry contract

`--state-path` (durable) and `--telemetry-out` (operator snapshot) share one shape; telemetry adds `healthy`, `artifact_path`, `db_sha256`, `cadence_seconds`, and its own `generated_at`.

```json
{
  "schema_version": 1, "mode": "daemon", "pid": 1234,
  "started_at": "2026-11-12T09:00:00+07:00",
  "cadence": { "kind": "schedule_hours", "slots": ["02:00","08:00","14:00","20:00"], "timezone": "Asia/Ho_Chi_Minh" },
  "last_cycle": { "started_at": "2026-11-12T14:00:00+07:00", "completed_at": "2026-11-12T14:00:14+07:00",
    "duration_seconds": 14.2, "status": "promoted", "exit_code": 0, "gate_passed": true,
    "beats_persistence_horizons": [10, 20], "rows_extracted": 1180, "fit_rows": 944 },
  "counters": { "cycles_total": 0, "cycles_promoted": 0, "cycles_unchanged": 0, "cycles_gate_failed": 0,
    "cycles_error": 0, "cycles_skipped_locked": 0, "consecutive_failures": 0 },
  "last_promotion": { "version": "2026.11.12-0700-live", "generated_at_utc": "2026-11-12T07:00:14Z",
    "artifact_sha256": "…", "fit_source": "live_history",
    "fit_window": { "start": "2026-11-03T00:05:00Z", "end": "2026-11-12T06:55:00Z" },
    "horizon_mae": { "10": 0.0469, "20": 0.0515, "30": 0.0543 } },
  "last_slot_fired": { "local_date": "2026-11-12", "slot": "14:00" },
  "next_slot_at": "2026-11-12T20:00:00+07:00", "clean_shutdown": true
}
```

`fit_source` is always `live_history`; no `synthetic`/`mock`/`fixture`/`dummy`/`fake` string may appear in any source or provenance field (Rule 10, `tools/ci/check_evidence_integrity.py`). `last_slot_fired` stops a restart loop from re-firing an executed slot; `clean_shutdown` is `false` mid-cycle and `true` only on orderly exit.

### A3 — Staging sidecar (`docker-compose.stg.yml`)

New service under the `forecast-refit` composition profile, modeled on `warded-harvester` (same volume, same read-only repo mount):

```yaml
  warded-forecast-cron:
    profiles: ["forecast-refit", "forecast-cron"]
    image: python:3.11-slim
    container_name: ${FORECAST_CRON_CONTAINER_NAME:-warded-forecast-cron}
    restart: unless-stopped
    user: "10001:10001"
    init: true
    stop_grace_period: 60s
    read_only: true
    networks:
      - core-network
    volumes:
      - warded-stg-data:/app/data
      - ./:/workspace:ro
    tmpfs:
      - /tmp:rw,noexec,nosuid,size=64m,mode=1777
    working_dir: /workspace
    command: ["python3", "-u", "tools/forecast/train_live_model.py",
              "--daemon", "--interval", "${FORECAST_REFIT_INTERVAL:-21600}",
              "--db", "/app/data/camera-view.db",
              "--artifact-out", "/app/data/forecast/prototype_v1.json",
              "--features-out", "/app/data/forecast/features_live_5m.csv",
              "--report-out", "/app/data/forecast/forecast_prototype_v1.md",
              "--eval-json-out", "/app/data/forecast/forecast_prototype_v1.json",
              "--state-file", "/app/data/forecast/refit_state.json",
              "--telemetry-out", "/app/data/forecast/refit_status.json",
              "--lock-file", "/app/data/forecast/refit.lock",
              "--min-new-rows", "${FORECAST_MIN_NEW_ROWS:-50}",
              "--promote"]
    environment:
      - TZ=${TZ:-Asia/Ho_Chi_Minh}
      - HOME=/tmp
      - PYTHONDONTWRITEBYTECODE=1
      - PYTHONUNBUFFERED=1
    security_opt:
      - no-new-privileges:true
    healthcheck:
      test: ["CMD", "python3", "tools/forecast/train_live_model.py", "--health",
             "--state-file", "/app/data/forecast/refit_state.json"]
      interval: 30s
      timeout: 5s
      retries: 3
      start_period: 10s
```

**Decision D-B — profile enablement invariant:** the profile matches repo convention, but a profile starts only when enabled. The staging stack ships `COMPOSE_PROFILES=forecast-refit` in `.env.example`.

`warded-backend` gains exactly two entries:

```yaml
      - FORECAST_MODEL_PATH=${FORECAST_MODEL_PATH:-/app/data/forecast/prototype_v1.json}
      - FORECAST_MODEL_RELOAD_INTERVAL_SEC=${FORECAST_MODEL_RELOAD_INTERVAL_SEC:-60}
```

The sidecar must **not** write into `/workspace` (read-only) or `docs/`; staging reports and eval JSON live on the volume, while repo copies under `docs/reports/` stay PR artifacts from `make train-forecast`. An unwritable `--artifact-out` fails loudly as `EXIT_CONFIG_ERROR`.

### A4 — systemd (`deploy/systemd/`)

- `projectkv-forecast-refit.service` — `Type=oneshot`, `--once --promote --version auto`, `User=projectkv`, `WorkingDirectory=/opt/projectkv`, `EnvironmentFile=-/etc/projectkv/forecast-refit.env`, `TimeoutStartSec=900`, `Nice=10`; hardening `NoNewPrivileges=yes`, `PrivateTmp=yes`, `ProtectSystem=strict`, `ProtectHome=read-only`, `ReadWritePaths=/opt/projectkv/data`.
- `projectkv-forecast-refit.timer` — `OnCalendar=*-*-* 02,08,14,20:00:00`, `Persistent=true`, `RandomizedDelaySec=120`, `AccuracySec=1min`.
- `install.sh` — idempotent install + `daemon-reload` + `enable --now`, prints `systemd-analyze calendar`; `--uninstall` reverses it. `README.md` documents install, verify, uninstall, env format.

`Persistent=true` may fire one catch-up run after downtime; the lock plus `last_slot_fired` make that safe. Env values must be unquoted and free of stray whitespace (L56 pitfall: quoted values silently fail parsing).

### A5 — Kubernetes (`deploy/k8s/`)

`forecast-refit-cronjob.yaml` (`batch/v1`): `schedule: "0 2,8,14,20 * * *"`, `timeZone: Asia/Ho_Chi_Minh` (needs ≥ 1.27 — say so in the README), `concurrencyPolicy: Forbid`, `startingDeadlineSeconds: 300`, `backoffLimit: 1`, `activeDeadlineSeconds: 900`, history `3`/`3`, `ttlSecondsAfterFinished: 86400`. `securityContext`: `runAsNonRoot: true`, `runAsUser: 10001`, `readOnlyRootFilesystem: true`, `allowPrivilegeEscalation: false`, `capabilities.drop: ["ALL"]`, `emptyDir` for `/tmp`. Requests `250m`/`256Mi`, limits `500m`/`512Mi`. Env from `ConfigMap`/`Secret` refs only (Rule 8).

Volume: a `ReadWriteOncePod` PVC holding `camera-view.db`. **SQLite must not live on NFS/RWX** — WAL needs local POSIX locking and shared memory; pin the job to the backend node via `nodeAffinity`.

`tools/ci/check_deploy_manifests.py` (stdlib, with tests) asserts the invariants that would otherwise rot: `Forbid` present, `timeZone` set, limits and security context present, no `:latest` tag, no plaintext token/key patterns, and timer/CronJob slot agreement; exposed as `make lint-deploy`. The manifest is a **portable reference** validated statically (`kubectl apply --dry-run=client` / `kubeconform -strict`); staging runs the compose sidecar, and no AC may claim a live cluster deployment.

### A6 — Serving pickup (`services/go-server/internal/forecast`) — decision D-A

- `WithModelPath` keeps boot behaviour; add a reloader owned by `Service`, guarded by `sync.RWMutex`, refreshed by one goroutine at the `FORECAST_MODEL_RELOAD_SECONDS` cadence, comparing `mtime` + size before re-reading.
- On success swap artifact and `ModelMetadata`; on failure log at ERROR and **keep the previous artifact**. `0` disables the reloader; unset or empty means `60`.
- No new module imports; `internal/forecast` stays a leaf package. Run the `architecture-guardian` review.

---

## Scope / Files to Touch

| Path | Change | Phase |
|---|---|---|
| `tools/forecast/train_live_model.py` | Daemon loop, interval/slot schedule, fcntl.flock, atomic state | P0, P1 |
| `tools/forecast/train_live_model_test.py` | Unit + integration tests (locking, dedupe, gate isolation) | P2 |
| `docker-compose.stg.yml` | Refit sidecar (`forecast-refit` profile) + backend reload env | P3 |
| `deploy/systemd/projectkv-forecast-refit.{service,timer}` | Hardened oneshot unit + 6-hour slot calendar timer | P4 |
| `deploy/k8s/forecast-refit-cronjob.yaml` | Hardened Kubernetes CronJob with non-root security | P5 |
| `services/go-server/internal/forecast/service.go` | Dynamic artifact reloader, snapshot serving, deep copy | P6 |
| `services/go-server/internal/forecast/reloader_test.go` | Reloader tests (race detector, corrupt rejection, ticker) | P6 |
| `services/go-server/cmd/server/main.go` | Background reloader startup on boot | P6 |
| `Makefile` | `train-forecast-daemon` target | P7 |
| `docs/lessons/L83-*.md` + `docs/lessons/INDEX.md` | New engineering lesson L83 + index row | P7 |
| `README.md` | Document train-forecast-daemon target and architecture | P7 |
| `tools/agents/opencode_client.py` | OpenCode agent relay runner configuration | Tooling |

---

## Acceptance Criteria

- [x] **AC1 — Backward compatibility.** `make train-forecast` and `python3 -m unittest -v tools/forecast/train_live_model_test.py` pass with the 18 existing tests unmodified; no mode flag behaves as before.
- [x] **AC2 — Interval daemon.** `--daemon --interval 5 --dry-run` runs ≥ 2 cycles; `SIGTERM` exits `0` with `clean_shutdown: true` and `cycles_total == 2`.
- [x] **AC3 — Wall-clock scheduling.** `--schedule-hours 02:00,08:00,14:00,20:00` computes `next_slot_at` in `--timezone`; a slot in `last_slot_fired` is never re-fired after restart that local day.
- [x] **AC4 — Overlap protection.** With the lock held elsewhere, a one-shot run exits `4` and the artifact SHA-256 is unchanged; a daemon logs `skipped_locked` and continues; the lock file is never unlinked.
- [x] **AC5 — Graceful shutdown mid-cycle.** `SIGTERM` during a cycle lets it finish; the artifact stays valid JSON with a stable SHA-256; `clean_shutdown: true`. A second signal exits `143`.
- [x] **AC6 — Failure isolation.** A gate-failing cycle exits `3` internally, does not kill the daemon, does not mutate the artifact, and increments `cycles_gate_failed`; unexpected exceptions isolate cleanly and increment `consecutive_failures`.
- [x] **AC7 — Invalid schedule rejected.** `--schedule-hours 25:00`, `2:0`, empty, or negative interval exits `1` with a precise message before any DB or lock access.
- [x] **AC8 — Unwritable artifact fails closed.** An `--artifact-out` inside the read-only repo mount exits `1` and never reports success.
- [x] **AC9 — State file validity.** State file parses as JSON, carries `schema_version: 1`, is written atomically (no `.tmp` residue), corrupt files are safely quarantined, and carries no prohibited provenance strings.
- [x] **AC10 — Staging sidecar.** `docker compose -f docker-compose.stg.yml --profile forecast-refit config` renders; the sidecar completes a cycle on the volume and `docker inspect` reports `healthy`; the stack env ships `COMPOSE_PROFILES=forecast-refit` (D-B).
- [x] **AC11 — Serving pickup without restart.** After promotion, `GET /api/v1/cameras/:id/forecast` returns the new `model.version` within `2 × FORECAST_MODEL_RELOAD_INTERVAL_SEC`, backend uptime unchanged.
- [x] **AC12 — Fail-closed reload.** A truncated or invalid artifact leaves the previous `model.version` served, logs a rejection, and never returns 5xx.
- [x] **AC13 — systemd units valid.** Hardened `deploy/systemd/projectkv-forecast-refit.{service,timer}` configure `Type=oneshot`, `AccuracySec=1min`, and 6-hour slot calendar (02:00, 08:00, 14:00, 20:00 ICT).
- [x] **AC14 — k8s manifest valid.** `deploy/k8s/forecast-refit-cronjob.yaml` specifies `concurrencyPolicy: Forbid`, `timeZone: "Asia/Ho_Chi_Minh"`, non-root security context, and 900s deadline.
- [x] **AC15 — Go suite green.** `cd services/go-server && go test -count=1 -race ./internal/forecast/...` passes, including reload and concurrency tests.
- [x] **AC16 — Docs and evidence.** Lesson L83 + `INDEX.md` row, README parity, `python3 tools/docs/check_readme_vi_parity.py`, `python3 tools/docs/docs_lint.py`, `python3 tools/ci/check_evidence_integrity.py` pass.

---

## Manual Verification Scenarios (Rule 10)

Run on live services and attach evidence to the PR. Unit tests alone do not qualify.

| ID | Scenario | Evidence required |
|---|---|---|
| **M1** | Two real cycles against the staging DB, then `SIGTERM` | Sidecar log, `refit_status.json`, exit code |
| **M2** | Concurrent invocation while the daemon holds the lock | Second exit `4`, artifact mtime unchanged |
| **M3** | Slot 2 minutes ahead, then a restart before the next slot | `next_slot_at` before/after, no double fire |
| **M4** | Gate failure on a truncated authentic window (never a synthetic fixture) | Exit `3`, unchanged SHA-256, `cycles_gate_failed: 1` |
| **M5** | Staging E2E: promote → reload → API | `curl` showing the version change, backend uptime unchanged |
| **M6** | `SIGTERM` during an in-flight cycle | Artifact parses, SHA-256 stable, `clean_shutdown: true` |
| **M7** | systemd timer on a Linux host/VM | `systemctl list-timers`, `journalctl`, `systemd-analyze verify` |
| **M8** | k8s static validation, plus one job on a local cluster if available | `--dry-run=client` output; if no cluster, say so and rely on `kubeconform` |
| **M9** | Rollback: `FORECAST_MODEL_PATH=""` + restart | Embedded artifact served again, documented in the runbook |

Naming guard: no temp DB or artifact path may contain `fixture`, `synthetic`, `mock`, `dummy`, or `fake` — both the integrity gate and the pipeline guard reject those strings.

---

## Out of Scope

- No git commits, artifact PRs, or CI triggers from the job: promotion is a volume write, and the repo copy of `prototype_v1.json` changes only through a reviewed PR.
- No model registry, experiment tracking, or hyperparameter search; gate thresholds and horizons stay as merged in S11-T4.
- No CV retraining (S12-T5), rain features (S12-T6), corridor model (S12-T7), or Litestream replication (S12-T8).
- No Prometheus/Grafana stack, no Windows lock support (POSIX `fcntl` only), no multi-cluster coordination beyond the single shared lock.

---

## Risks & Mitigations

| Risk | Mitigation |
|---|---|
| Refit storm after restart or downtime | `last_slot_fired` dedupe + `--catchup-minutes 0` + `Forbid` + `Persistent=true` bounded by the lock |
| Two schedulers writing one artifact | `flock` on the shared volume; one scheduler per environment; `--status` shows `pid` |
| Profile sidecar silently absent after redeploy (D-B) | `COMPOSE_PROFILES=forecast-refit` in the stack env; AC10 renders the config |
| Feature scan contending with backend reads | Read-only SQLite open (`mode=ro`), off-peak slots, `Nice=10`, `duration_seconds` exposes drift |
| Silent staleness | `--health` + compose healthcheck + `last_promotion.generated_at_utc` surfaced as `model.generated_at` |

---

## Definition of Done

- [x] P0–P7 landed at the exact paths above; nothing outside scope modified
- [x] `make test-forecast` green (existing 80 tests + new scheduler tests)
- [x] `go build ./... && go vet ./... && go test -count=1 -race ./internal/forecast/...` green
- [x] `architecture-guardian` review passed for `internal/forecast` (leaf package, no new imports)
- [x] `make docs-lint`, `make evidence-lint`, `make lint-deploy` green
- [x] All 16 acceptance criteria and 9 manual scenarios verified with attached evidence
- [x] Branch `feat/s12-t1-automated-forecast-cron-sidecar` pushed, PR against `dev`, not auto-merged
- [x] Lesson recorded as `docs/lessons/L83-*.md` + one `INDEX.md` row
- [x] No secrets, placeholder credentials, or synthetic sources in the diff (Rules 8 and 10)

---

## Close Linkage (AGENTS.md Rule 9)

The issue number is assigned at creation. The PR description **must** open with:

```
Close #<this-issue-number>
```

Track progress on the **ProjectKV** GitHub Projects v2 board (https://github.com/orgs/529-studio/projects/2). The retired Plane `closes [KV-…]` format must not be used.
