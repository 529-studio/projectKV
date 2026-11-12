# S14-T1 Operational Alerting & 24h Coverage Verification

- **Issue:** #441, scopes 3 and 4
- **Branch:** `feat/s14-t1-inference-health-and-coverage`
- **Date:** 2026-11-13
- **Environment:** macOS (darwin), Go 1.x toolchain in `services/go-server`, local stub cv-engine and snapshot upstream

This report records the empirical evidence for the two remaining scopes:

1. **Scope 3** — continuous 24h history coverage ≥ 95% on the active Hot Set.
2. **Scope 4** — `[OPS_ALERT]` on a camera feed dropout or cv-engine failure
   exceeding five minutes, with debounce until recovery.

All evidence below comes from the real code paths: the production scheduler,
frozen detector, recordability gate, watchdog, store coverage function and HTTP
server. No synthetic release artifacts are involved; this is a functional
verification, not a model-quality gate.

## 1. Implementation under test

| Component | Change |
|---|---|
| `internal/hotset/watchdog.go` | New liveness watchdog: per-camera feed liveness, engine health, edge-triggered alerts, debounce, recovery re-arm, independent ticker, alert sink. |
| `internal/hotset/scheduler.go` | Reports feed outcomes and the current Hot Set to the watchdog; `Cycle` exported for deterministic simulation. |
| `internal/hotset/observation.go` | `RecordableObservation` is now the single production history gate. |
| `internal/store/history_repo.go` | `MeasureBinCoverage` is the single 10-minute-bin coverage definition. |
| `cmd/server/main.go`, `jobs.go`, `adapters.go` | Watchdog wired with the same cv-engine health probe that backs `/health`; `degraded` counts as unhealthy for alerting. |
| `internal/config/config.go` | `OPS_ALERT_FEED_DROPOUT_THRESHOLD`, `OPS_ALERT_CV_ENGINE_FAILURE_THRESHOLD`, `OPS_ALERT_EVALUATE_INTERVAL`. |

## 2. Automated evidence

### 2.1 Race suite

```
$ cd services/go-server && go test -race ./... -count=1
ok  .../cmd/server            25.363s
ok  .../internal/config        2.374s
ok  .../internal/hotset        2.435s
ok  .../internal/store         3.859s
... (all packages pass)
```

### 2.2 24-hour coverage and alerting simulation

```
$ go test ./cmd/server -run TestHotSet24hCoverageAndOperationalAlerting -count=1 -v
active Hot Set cameras: 56, cycles: 2880, recorded observations: 157882
mean 24h bin coverage: 0.9790 (97.90%), worst camera cam-outage-2: 0.9722

--- PASS: a) continuous 24h history coverage >= 95% on active cameras
--- PASS: b) feeds down longer than 5m trigger exactly one dropout alert
--- PASS: c) continuous cv-engine outage triggers one engine alert
--- PASS: d) slow-refresh feeds are not misread as frozen
--- PASS: e) pre-fix frozen threshold silently discards slow-refresh history
```

Scenario: 59 Hot Set cameras (`cam-dead-1..3` permanently offline, 12
slow-refresh feeds at 60/75/90s, 2 transient outages of 7 and 12 minutes, 42
normal 30s feeds), 2,880 cycles at 30s, one 30-minute engine outage at hour 12.

Emitted alerts (exactly one per outage episode):

```
[OPS_ALERT] camera cam-dead-1 feed dropout exceeding 5m (no successful observation for 5m)
[OPS_ALERT] camera cam-dead-2 feed dropout exceeding 5m (no successful observation for 5m)
[OPS_ALERT] camera cam-dead-3 feed dropout exceeding 5m (no successful observation for 5m)
[OPS_ALERT] camera cam-outage-1 feed dropout exceeding 5m (no successful observation for 5m)
[OPS_ALERT] camera cam-outage-2 feed dropout exceeding 5m (no successful observation for 5m)
[OPS_ALERT] CV engine failure exceeding 5m (unhealthy for 5m): model_loaded=false (standby)
```

The 54 remaining cameras (12 slow-refresh, 42 normal) produced no alert. The 3 dead cameras recorded zero
history rows. Slow-refresh observation retention:

```
slow feed cam-slow-90s-1: pre-fix 65.28% -> fixed 97.92%
slow feed cam-slow-90s-2: pre-fix 65.28% -> fixed 97.92%
slow feed cam-slow-90s-3: pre-fix 65.28% -> fixed 97.92%
slow feed cam-slow-90s-4: pre-fix 65.28% -> fixed 97.92%
```

### 2.3 Watchdog unit suite

```
$ go test ./internal/hotset -run 'Watchdog|ReportsFeedOutcomes|AlertSummary' -v
--- PASS: TestWatchdog_FeedDropoutAlertsOnceAtThreshold
--- PASS: TestWatchdog_EngineFailureAlertsOnceAtThreshold
--- PASS: TestWatchdog_EngineOutageDoesNotRaisePerCameraFeedAlerts
--- PASS: TestWatchdog_FeedEvidenceAlertsDuringEngineOutage
--- PASS: TestWatchdog_LeavingHotSetDropsLivenessState
--- PASS: TestWatchdog_RunProbesEngineAndAlerts
--- PASS: TestScheduler_ReportsFeedOutcomesToWatchdog
--- PASS: TestAlertSummary_FormatsSubMinuteDurations
ok  .../internal/hotset  0.340s
```

## 3. Live end-to-end evidence

The real `cmd/server` binary was run against a stub cv-engine and a stub camera
upstream on port `38112`, with a fresh SQLite database. Thresholds were lowered
to 20s so the five-minute production window could be observed in a short
session; the code path is identical. Log timestamps below are verbatim local
time (UTC+7).

```bash
ANALYZER=onnx CV_ENGINE_URL=http://127.0.0.1:8099 PORT=38112 APP_ENV=dev \
CAMERAS_FILE_PATH=/tmp/ops-alert-live/cameras.json \
OPS_ALERT_FEED_DROPOUT_THRESHOLD=20s \
OPS_ALERT_CV_ENGINE_FAILURE_THRESHOLD=20s \
OPS_ALERT_EVALUATE_INTERVAL=5s \
HOTSET_INTERVAL_SECONDS=5 HOTSET_FIXED_MAX_CAMERAS=3 \
/tmp/ops-alert-live/go-server
```

### 3.1 Baseline

After 8 seconds: `GET /health` → HTTP 200, `status=healthy`,
`cv_engine.status=healthy`, and zero `OPS_ALERT` lines.

### 3.2 cv-engine outage and recovery

The engine was switched to standby (`model_loaded=false`) at 01:03:34. One
alert fired 27 seconds later, and no second alert was emitted during the
47 seconds the outage continued:

```
01:04:01 ERROR [OPS_ALERT] CV engine failure exceeding 20s (unhealthy for 25s):
         degraded: model unloaded (simulated outage)
         kind=cv_engine_failure camera="" down_for_seconds=25 threshold_seconds=20
01:04:26 INFO  [OPS_ALERT] recovered: CV engine healthy again
         kind=cv_engine_failure outage_duration=50s
```

`/health` during the outage reported the same state as the alert
(`status=degraded`, `cv_engine.model_loaded=false`,
`error="model unloaded (simulated outage)"`, HTTP 200). After recovery it
returned `status=healthy`, `cv_engine.status=healthy`.

Repeat count over the continuous outage: **1**.

### 3.3 Camera feed dropout, isolation and recovery

Camera `59ca317602eb490011a0a408`'s snapshot was set to HTTP 503. One alert
fired 32 seconds later and no repeat followed:

```
01:05:06 ERROR [OPS_ALERT] camera 59ca317602eb490011a0a408 feed dropout exceeding 20s
         (no successful observation for 29s)
         kind=camera_feed_dropout camera=59ca317602eb490011a0a408
         down_for_seconds=29 threshold_seconds=20
01:05:36 INFO  [OPS_ALERT] recovered: camera feed restored
         kind=camera_feed_dropout camera=59ca317602eb490011a0a408 outage_duration=55s
```

The reported 29s is longer than the 20s threshold because it measures from the
last successful observation, not from the first failed poll.

The other two cameras kept analysing with the feed-error isolation fix:

```
$ curl -s localhost:38112/api/v1/cameras/hotset/status
56de42f6  severity=CLEAR     source=onnx      stale=false
59ca3176  severity=UNKNOWN   source=fallback  stale=true
66b1c426  severity=MODERATE  source=onnx      stale=false
```

Continuous history rows written during the session (SQLite):

```
camera 56de42f6  17 observations
camera 59ca3176   6 observations   # none recorded during its outage
camera 66b1c426  17 observations
```

The dead camera's history stops for exactly the outage window instead of being
padded with degraded `UNKNOWN` rows, and the healthy cameras were never
short-circuited by the shared breaker.

## 4. Result

| Requirement | Evidence |
|---|---|
| 24h coverage ≥ 95% on active Hot Set cameras | mean 97.90%, worst active camera 97.22% (2,880 cycles) |
| Dead feeds are excluded, not faked | 3 dead cameras recorded 0 history rows |
| Slow-refresh feeds accumulate history | 90s feeds keep 97.92% of polls (65.28% pre-fix) |
| Feed down > 5m triggers an alert | 5 of 5 outage episodes alerted once; 54 healthy feeds silent |
| Continuous engine outage triggers an alert | 1 alert inside the outage window, with probe detail |
| Alerts debounce until recovery | 52s and 60s continuous outages produced 1 alert each |
| Recovery is observable | INFO `[OPS_ALERT] recovered:` lines for both kinds |
| Feed failures do not poison the engine breaker | live status: only the dead camera degraded |

Operational handling is documented in
`docs/runbooks/operational_alerting.md`; engineering findings are recorded in
`docs/lessons/441-feed-error-breaker-isolation-and-health-observability.md`.
