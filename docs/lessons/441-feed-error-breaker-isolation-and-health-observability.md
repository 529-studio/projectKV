# Lesson 441: A Dead Camera Must Not Trip the Engine Breaker

## Context

S14-T1 (`#441`) fixed live inference darkness in three places that shared one
root cause: a per-camera failure was being treated as a system-wide failure.
A single camera with a dead upstream feed advanced the shared circuit breaker
until it opened, which then short-circuited every other camera in the Hot Set
to `UNKNOWN`. The harvester compounded it by queueing offline and ghost cameras
and letting a hung snapshot read hold a concurrency slot for the whole run,
while `/health` reported `ok` even when the cv-engine could not serve inference.

## Root causes and fixes

1. **Breaker poisoning by feed errors.** `cvclient.CVEngineAnalyzer.Analyze`
   already returns `*FeedError` for snapshot-phase failures, but the scheduler
   recorded every error on the shared breaker.
   `internal/hotset/scheduler.go` now detects `interface{ IsFeedError() bool }`
   through `errors.As` (works through wrap and `errors.Join`) and skips
   `RecordFailure` for feed errors. The camera still degrades to `UNKNOWN` for
   that cycle; only engine-class errors (timeouts, 5xx, standby) advance the
   breaker.
2. **A frozen-feed threshold tuned for 30s feeds only.** `DefaultFrozenThreshold`
   was 3, so a 60-90s refresh source that legitimately repeats a frame looked
   frozen and was dropped from history. It is now 10 (5 minutes at the 30s
   Hot Set interval). `FROZEN_FRAME_THRESHOLD` defaults to 10 as well.
3. **Harvester wasted slots on cameras that cannot succeed.**
   `HarvestCameras` filters `!IsOnline || IsGhost` before acquiring the
   semaphore, and `ProcessCamera` bounds snapshot fetching with
   `DefaultSnapshotFetchTimeout` (5s) so a hanging feed fails fast.
4. **Health told half the truth.** `httpserver` now accepts a `HealthChecker`
   (or a `CVEngineHealthProbe` adapter) and reports
   `status: healthy | degraded | unhealthy` plus a `cv_engine` block on
   `/health`, `/healthz` and `/api/health`, returning 503 on `unhealthy`.
5. **Degradation was silent.** The pipeline logged warnings per camera, but
   nothing aggregated "this camera has produced nothing for five minutes" or
   "the engine has not answered for five minutes". `hotset.Watchdog` now tracks
   per-camera feed liveness and engine health and emits one edge-triggered
   `[OPS_ALERT]` per outage episode, followed by a recovery line. It runs its
   own ticker, so a stuck analysis cycle cannot silence alerting, and the
   engine probe reuses the same health check as `/health`, so the alert and the
   endpoint cannot disagree.
6. **The coverage claim had no reproducible harness.** The audit measured 24h
   coverage once, by hand. `cmd/server/coverage_alerting_simulation_test.go`
   now drives the real scheduler, frozen detector, recordability gate, watchdog
   and coverage math over 2,880 simulated 30s cycles with 59 cameras, and the
   10-minute bin definition moved into `store.MeasureBinCoverage` so the
   harness and `/api/v1/hotset/history/coverage` cannot diverge.

## Invariants worth keeping

- **Blast radius must match the failure.** A camera feed is per-camera state;
  a breaker guards the inference engine. Never feed the former into the latter.
- **Thresholds are expressed in time, not in ticks.** Convert a frame-count
  threshold to wall-clock time at the deployed polling interval before
  deciding it is "large enough".
- **Readiness means the dependency can serve.** `/health` that returns `ok`
  while `POST /predict` cannot serve is a liveness probe pretending to be a
  readiness probe.
- **Every scheduled unit must have a bounded failure time.** Concurrency slots
  are a finite shared resource; an unbounded fetch converts one dead camera
  into a starved pool.
- **Alert on evidence, not on the absence of success.** "No success for five
  minutes" can mean a dead feed, an engine outage, an exhausted analyzer budget
  or a breaker short-circuit. Starting the dropout clock only on an actual
  `FeedError` is what prevents an engine incident from paging once per camera.
- **Alerts are edges, not levels.** One page per outage episode, re-armed by
  recovery. A level-triggered alert on a five-minute timer becomes 2,880 log
  lines a day per broken camera and trains operators to ignore the tag.
- **A shared metric needs a shared implementation.** If the verification
  harness reimplements the bin math, it can report 95% while the endpoint says
  40%. Extract the pure function and call it from both.
- **The watchdog must not depend on the pipeline it watches.** A ticker inside
  `Scheduler.Run` stops when the scheduler deadlocks, which is exactly the
  incident you need to hear about.

## Verification

- `go test ./internal/hotset/... ./internal/config/... ./internal/harvester/... ./internal/httpserver/...`
  passes, including new tests for feed-vs-engine breaker behaviour, the K=10
  frozen threshold, offline/ghost filtering, the 5s fetch bound, and all three
  cv-engine health states.
- `go test -race ./...` passes, including the watchdog unit suite (threshold
  crossing, debounce, recovery re-arm, Hot Set pruning, probe path, and the
  engine-outage anti-storm rule).
- `go test ./cmd/server -run TestHotSet24hCoverageAndOperationalAlerting -v`
  simulates 24h over 59 cameras (3 permanently dead, 12 slow-refresh at
  60/75/90s, 2 transient dropouts of 7 and 12 minutes, one 30-minute engine
  outage) and reports:
  - mean 10m-bin coverage **97.90%**, worst active camera **97.22%**;
  - exactly **5** dropout alerts, one per outage episode, none for the other
    54 cameras;
  - exactly **1** engine alert, inside the simulated outage window;
  - 90s-refresh feeds keep **97.92%** of polls, versus **65.28%** under the
    pre-fix K=3 threshold.
- Live server with a stub cv-engine (20s thresholds for the test):
  `[OPS_ALERT] CV engine failure exceeding 20s (unhealthy for 20s)` once,
  `[OPS_ALERT] recovered: CV engine healthy again`, then
  `[OPS_ALERT] camera <id> feed dropout exceeding 20s` once while the other
  cameras kept reporting `source=onnx`; the dead camera's history rows stop for
  exactly the outage window. Full transcript:
  `docs/reports/s14_t1_ops_alerting_verification.md`.

