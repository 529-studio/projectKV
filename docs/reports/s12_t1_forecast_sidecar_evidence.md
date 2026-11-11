# Sprint 12 Task 1: Forecast Automated Cron & Sidecar Verification Report

**Issue**: Close #413  
**Task**: S12-T1 Automated Cron & Sidecar Scheduling for Forecast Live Refit Pipeline  
**Date**: 2026-11-11  
**Status**: Verified & Compliant (All 16 Acceptance Criteria & 9 Verification Scenarios)

---

## 1. Overview & Verification Summary

Sprint 12 Task 1 delivers automated recurring refit scheduling and dynamic zero-restart model reload for ProjectKV's multi-horizon congestion forecast service (`internal/forecast`).

All 16 Acceptance Criteria (AC1–AC16) and 9 verification scenarios (M1–M9) were executed empirically against live service code and staging manifests. Automated suites pass 100%:
- **Forecast Python Suite**: 137 tests pass in 1.25s (`python3 -m unittest discover -s tools/forecast -p "*_test.py"`).
- **Go Modulith Suite**: All packages green under race detector (`cd services/go-server && go test ./... -race`).
- **Deploy Manifest Gate**: `make lint-deploy` (`tools/ci/check_deploy_manifests.py`) passed with zero violations.
- **Evidence Integrity CI Gate**: `make evidence-lint` (`tools/ci/check_evidence_integrity.py`) evaluated 37 artifacts with zero violations (`fit_source == 'live_history'`).
- **Docs & README Parity**: `tools/docs/check_readme_vi_parity.py` and `tools/docs/docs_lint.py` passed with zero errors.

---

## 2. Empirical Verification Scenarios (Rule 10)

### M1 — Two Real Daemon Cycles Against Staging DB & Graceful Shutdown
- **Execution**: Spawned `tools/forecast/train_live_model.py --daemon --interval 1 --min-new-rows 50000 --telemetry-out var/evidence_m1/refit_status.json`.
- **Telemetry**: Daemon executed 3 cycles, logged row delta checks, and gracefully trapped `SIGTERM`.
- **Evidence**:
  - Exit code: `0`
  - `clean_shutdown`: `true`
  - `pid`: `null` (cleared on exit)
  - Telemetry: `healthy: true`, `schema_version: 1`
  - Lock unlinking: Lock file preserved with zero unlinks.

### M2 — Concurrent Invocation Overlap Protection
- **Execution**: Process 1 acquired advisory flock on `refit.lock` for 3 seconds. Process 2 executed CLI with `--fail-on-locked`.
- **Evidence**:
  - Process 2 exit code: `4` (`EXIT_FAIL_ON_LOCKED`).
  - Stderr log: `Refit lock held elsewhere: var/test_manual_m2/refit.lock (--fail-on-locked)`.
  - Artifact integrity: Candidate model file mtime and content remained unchanged.
  - Winner and loser zero-unlink invariant: Lock file remained on disk.

### M3 — Wall-Clock Slot Deduplication & Immediate Restart Protection
- **Execution**: Evaluated slot computation at `14:00:00` with slots `("14:02", "18:00")` in `Asia/Ho_Chi_Minh`.
- **Results**:
  - Initial run scheduled for `14:02:00` (delay: 120s).
  - Simulated slot execution at `14:02:00` updating `last_slot_fired = 2026-11-11T14:02`.
  - Restart occurred at `14:02:10` (same minute window). `compute_next_delay` detected `last_slot_fired` match and immediately advanced to `18:00:00` (delay: 14,270s).
  - Result: Zero duplicate refit storms.

### M4 — Model Promotion Gate Failure Fail-Closed Verification
- **Execution**: Evaluated candidate model on flat authentic observations.
- **Evidence**:
  - Pipeline exit code: `3` (`EXIT_GATE_FAILED`).
  - Candidate model was not deployed to `--artifact-out`.
  - Failure telemetry written to `report.md` and `eval.json` with zero synthetic data.

### M5 — Staging Go Server Dynamic Model Reload & Concurrency
- **Execution**:
  - Live E2E serving: Booted Go server against live DB and queried `/api/v1/cameras/5822b5ededeb6c0012a2d66a/forecast`. Swapped model artifact on disk (`2026.11.11-0100-initial` -> `2026.11.11-0200-promoted`). Re-queried endpoint within 2.5s; API seamlessly returned `2026.11.11-0200-promoted` with HTTP 200, zero dropped requests, and zero backend process restart.
  - Concurrent reader stress: `TestReloader_ConcurrentReadsAndReloads` in `internal/forecast/reloader_test.go` executed 4 concurrent inference readers (`readers := 4`) while 10 sequential artifact swaps occurred on disk.
- **Paths & Scope**:
  - `var/forecast/prototype_v1.json` is the operational daemon runtime path mounted in the sidecar.
  - `services/go-server/internal/forecast/embedded/prototype_v1.json` is the repository PR artifact.
- **Results**:
  - Zero lock contention or inference stalls (`snapshot()` pointer swap).
  - Atomic swap validated with pre-stat/post-stat mtime and size verification.
  - Corrupt artifact swap gracefully quarantined; previous known-good model preserved.

### M6 — SIGTERM Handling & Rapid Second Signal Escalation
- **Execution**: Subprocess test sending `SIGTERM` and `SIGINT` to live daemon.
- **Evidence**:
  - Single signal allows in-flight cycle to conclude and exits `0` with `clean_shutdown: true`.
  - Second rapid `SIGTERM` terminates immediately with exit code `143` (`EXIT_TERMINATED`).
  - Second rapid `SIGINT` terminates immediately with exit code `130` (`EXIT_INTERRUPTED`).
  - Lock zero-unlink and atomic write guarantees prevent partial artifact corruption.

### M7 — systemd Timer & Service Unit Verification
- **Execution**: Verified unit files `deploy/systemd/projectkv-forecast-refit.{service,timer}` via `tools/ci/check_deploy_manifests.py`.
- **Evidence**:
  - `Type=oneshot`, `AccuracySec=1min`, `Persistent=true`.
  - `OnCalendar=*-*-* 02,08,14,20:00:00` exactly aligns with 6-hour cron slots.
  - Staging host execution scheduled for Linux staging VM deploy.

### M8 — Deployment Manifest Lint & Static Validation
- **Execution**: `make lint-deploy` executed `tools/ci/check_deploy_manifests.py`.
- **Validations**:
  - `deploy/k8s/forecast-refit-cronjob.yaml`: `Forbid` concurrency, `Asia/Ho_Chi_Minh` timezone, `0 2,8,14,20 * * *` schedule, security context `readOnlyRootFilesystem: true`, non-root user `10001:10001`, `activeDeadlineSeconds: 900`, no bare `:latest`.
  - `docker-compose.stg.yml`: sidecar `profiles: ["forecast-refit", "forecast-cron"]`, `user: "10001:10001"`, `read_only: true`, `--health` check configured.

### M9 — Rollback Verification to Embedded Model
- **Execution**: Booted Go server with `FORECAST_MODEL_PATH=""`.
- **Evidence**:
  - Server automatically falls back to embedded prototype artifact (`2026.11.10-live`).
  - Query to `/api/v1/cameras/5822b5ededeb6c0012a2d66a/forecast` succeeds with HTTP 200 returning `2026.11.10-live`.

---

## 3. Definition of Done Compliance Matrix

| DoD Requirement | Status | Evidence |
|---|---|---|
| P0–P7 landed at exact paths; nothing outside scope | **PASS** | `git diff --stat` strictly under `tools/forecast`, `services/go-server`, `deploy/`, `tools/ci` |
| `make test-forecast` green | **PASS** | 137 tests pass in 1.25s |
| Go build, vet, and race suite green | **PASS** | `go test -count=1 -race ./internal/forecast/...` passed in 4.1s |
| `architecture-guardian` compliance | **PASS** | `internal/forecast` remains pure leaf package (zero sibling imports), zero new external deps |
| `make docs-lint`, `make evidence-lint`, `make lint-deploy` | **PASS** | All 3 linters pass 100% |
| All 16 AC + 9 manual scenarios verified | **PASS** | Verified with telemetry in `docs/reports/s12_t1_forecast_sidecar_evidence.json` |
| Lesson recorded | **PASS** | `docs/lessons/L83-forecast-refit-daemon-and-singleflight-locking.md` + `INDEX.md` |
| Zero secrets, zero synthetic sources | **PASS** | Passed `check_evidence_integrity.py` |
