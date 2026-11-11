# Lesson 84: Forecast Refit Daemon, Single-Flight Locking, and Cadence Scheduling

## Navigation Aid & Quick Reference
- **Domain**: Machine learning ops, scheduled refit pipelines, process concurrency control, durable telemetry state.
- **Relevant Issues**: Issue #413 (S12-T1), Issue #398 (S11-T4), PR #409, Lesson L80, Lesson L83.
- **Key Files**:
  - `tools/forecast/train_live_model.py` (daemon loop, `ForecastRefitLock`, `SchedulerState`, `compute_next_delay`)
  - `tools/forecast/train_live_model_test.py` (lock contention, state serialization, schedule calculation, mock runs)
  - `docker-compose.stg.yml` (`warded-forecast-cron` sidecar container)
  - `deploy/systemd/projectkv-forecast-refit.{service,timer}` (systemd timer definitions)
  - `deploy/k8s/forecast-refit-cronjob.yaml` (Kubernetes CronJob manifest)
- **Core Invariants**:
  - **The lock file is never unlinked**: Unlinking an advisory lock file creates an inode race condition where competing processes lock disjoint files.
  - **Single source of execution truth**: All deployment targets (Compose sidecar, systemd, k8s CronJob) execute the same CLI flags against `train_live_model.py`.
  - **Fail-closed read-only DB access**: Row count checks and training reads enforce SQLite read-only mode (`mode=ro`) and `PRAGMA query_only = ON`.
  - **Atomic crash-safe telemetry**: State file updates use temporary sibling files followed by atomic `os.replace` to prevent corrupted telemetry.

---

## 1. Context & Problem Statement

In Sprint 11 Task 4 (Issue #398, PR #409, L80), `tools/forecast/train_live_model.py` was introduced as a single-run CLI refit pipeline. It extracts 5-minute traffic observations from `hotset_verdict_history`, trains Ridge regression models, evaluates against a chronological persistence baseline, and atomically promotes model weights to `prototype_v1.json`.

While functional as a manual CLI, it was an inert tool:
1. Continuous background data ingestion (S11-T2) appends live frames 24/7, but model parameters remained static unless manually refitted by an operator.
2. Uncoordinated cron jobs or concurrent operator runs risked overlapping refit operations, contending for CPU/memory and thrashing the promotion files.
3. Diverse deployment environments (staging Docker Compose, bare-metal Linux servers, and Kubernetes clusters) lacked unified, hardened scheduling definitions.

---

## 2. Core Architectural Decisions

### A. Non-Unlinked Kernel Advisory Lock (`ForecastRefitLock`)
A common locking antipattern is deleting the lockfile upon process exit. If Process A unlinks the lockfile while Process B is in the middle of opening it, Process B opens and locks an unlinked inode, while a subsequent Process C creates a new file and inode, allowing two processes to hold the lock simultaneously.
`ForecastRefitLock` uses `fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)` on a permanent lock file (`var/run/forecast_refit.lock`), truncating and writing diagnostic PID data only after the lock is acquired, and never unlinking the file on release.

### B. Durable State & Forensic Quarantine (`SchedulerState`)
Scheduler state (`var/run/forecast_refit_state.json`) tracks cycle counts (`cycles_total`, `cycles_promoted`, `cycles_skipped_locked`, `cycles_skipped_no_new_data`), consecutive failures, and timestamps.
If a state file contains corrupt JSON or an unrecognized schema, it is never silently deleted. Instead, it is atomically renamed to `<filename>.corrupt-<timestamp>` for forensic analysis, and a clean baseline is initialized.

### C. Wall-Clock Schedule & Timezone Arithmetic (`compute_next_delay`)
Fixed intervals (e.g. 6 hours) drift over time. Wall-clock scheduling (`--schedule-hours 02:00,08:00,14:00,20:00`) anchors execution to exact local time slots. Candidates are evaluated as absolute UTC instants over an 8-day lookahead window, guaranteeing positive delays and immunity to clock drift.

### D. Row-Delta Gating (`--min-new-rows`)
Refitting a machine learning model on identical or trivial data deltas wastes compute and pollutes audit logs. By querying `SELECT count(*) FROM hotset_verdict_history` through a read-only connection before refitting, cycles with fewer than `min_new_rows` (e.g. 50 rows) are recorded as `skipped_no_new_data` and skipped without invoking matrix solvers.

---

## 3. Verification & Guardrails

- `python3 -m unittest discover -s tools/forecast -p "*_test.py"`: 128 tests covering Ridge pipeline, persistence gate, lock contention, PID tracking, corrupt state quarantine, slot arithmetic, and daemon loop bounds.
- Staging sidecar verified via `docker compose --profile forecast-refit config`.
- Standardized error codes: `EXIT_FAIL_ON_LOCKED = 4` for strict orchestrators, `EXIT_OK = 0` for non-blocking daemons.
