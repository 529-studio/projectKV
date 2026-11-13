# Operational Alerting Runbook

On-call guide for the `[OPS_ALERT]` lines emitted by the Hot Set scheduler's
watchdog (Issue #441 scope 4). Everything here is implemented in
`services/go-server/internal/hotset/watchdog.go` and wired in
`cmd/server/main.go`.

## 1. What pages you

| Alert kind | Log line | Meaning |
|---|---|---|
| `camera_feed_dropout` | `[OPS_ALERT] camera <id> feed dropout exceeding 5m` | One Hot Set camera has not produced a successful observation for the threshold window. |
| `cv_engine_failure` | `[OPS_ALERT] CV engine failure exceeding 5m` | The cv-engine has been unhealthy for the threshold window. `degraded` (`model_loaded=false`, standby) counts as unhealthy because it cannot serve inference. |

Recovery is logged with the same tag and kind at INFO level:

```
[OPS_ALERT] recovered: camera feed restored camera=<id> outage_duration=12m0s
[OPS_ALERT] recovered: CV engine healthy again outage_duration=7m30s
```

Alerts are **edge-triggered**: one line per outage episode, re-armed by
recovery. A camera that stays down for a day produces one dropout alert, not
2,880. If you see repeats, the feed is flapping, not spamming.

## 2. Configuration

| Variable | Default | Purpose |
|---|---|---|
| `OPS_ALERT_FEED_DROPOUT_THRESHOLD` | `5m` | No-successful-observation window before a camera alert. |
| `OPS_ALERT_CV_ENGINE_FAILURE_THRESHOLD` | `5m` | Continuous-unhealthy window before an engine alert. |
| `OPS_ALERT_EVALUATE_INTERVAL` | `30s` | Watchdog probe/evaluation tick. Independent of the analysis cycle. |

Both thresholds are Go duration strings (`90s`, `5m`, `1h`). Values `<= 0` fall
back to the defaults. A delivery hook (`hotset.WithAlertSink`) is available for
a pager or ops chat channel; the log line is always emitted whether or not a
sink is configured.

## 3. Triage: camera feed dropout

1. Check whether it is one camera or many:
   `grep '\[OPS_ALERT\]' <app log> | tail -50`.
   - One camera: almost always upstream. Continue with step 2.
   - Many cameras: check the engine alert block first. A system-wide alert storm
     is an engine/network incident, not 50 broken feeds.
2. Confirm the camera's current verdict:
   `curl -s localhost:3000/api/v1/cameras/hotset/status | jq '.cameras[] | select(.camera_id=="<id>")'`.
   An `UNKNOWN`/`fallback` verdict with a recent timestamp means the scheduler
   is still cycling and only this feed is failing.
3. Separate feed from engine:
   - `source=fallback` plus `[WARN] feed unavailable` → snapshot fetch failure.
   - `source=fallback` plus `[WARN] analysis failed` → engine-class error; see
     section 4.
4. Confirm healthy cameras are unaffected. Their verdicts must still report
   `source=onnx` and a fresh `observed_at`. If every camera degraded at once,
   the cause is engine-class, not a feed.
5. Verify the upstream portal manually (`SNAPSHOT_UPSTREAM_BASE_URL`), then
   leave the camera in the Hot Set: it recovers automatically and the recovery
   line confirms it. History is not fabricated while it is down.

## 4. Triage: cv-engine failure

1. `curl -s localhost:3000/health | jq .cv_engine` — the alert and the health
   endpoint share one probe, so they cannot disagree.
   - `status=degraded` → the process answers but `model_loaded=false`; restart
     the inference worker so it loads the OpenVINO model.
   - `status=unhealthy` → the probe could not reach the engine; check container
     state, port `8000`, and `CV_ENGINE_URL`.
2. The Hot Set circuit breaker opens after repeated engine-class failures, so
   cameras degrade to `UNKNOWN` instead of queueing unbounded calls. That is
   expected; verdicts resume on the next successful probe/analysis.
3. Do not restart the Go server to "clear" the alert. The watchdog re-derives
   state from the probe and will re-alert if the engine is still down.

## 5. Why the two alerts do not cross-trigger

- A camera feed failure is a `*cvclient.FeedError`; the scheduler never records
  it on the shared breaker (see `docs/lessons/441-feed-error-breaker-isolation-and-health-observability.md`).
- Feed liveness is driven by actual feed failures, not by "time since last
  success". An engine outage, an exhausted analyzer budget, or a breaker
  short-circuit produces **no** feed evidence, so it never raises 59 camera
  alerts. The engine alert is the single page for that incident.
- Conversely, a camera with a genuinely dead feed still alerts while the engine
  is down: the feed is independently observable state.

## 6. Verifying the alerting path

Deterministic, no Docker required:

```bash
cd services/go-server
go test ./internal/hotset/ -run 'Watchdog|ReportsFeedOutcomes' -v
go test ./cmd/server/ -run TestHotSet24hCoverageAndOperationalAlerting -v
```

The second test runs 24 simulated hours over 59 Hot Set cameras (3 dead, 12
slow-refresh, 2 transient dropouts, a 30-minute engine outage) and asserts
coverage, dropout alerts and the engine alert. For a live smoke test, lower the
thresholds (`OPS_ALERT_*_THRESHOLD=20s`, `OPS_ALERT_EVALUATE_INTERVAL=5s`) and
point `CV_ENGINE_URL` at a stub; the verification transcript is in
`docs/reports/s14_t1_ops_alerting_verification.md`.
