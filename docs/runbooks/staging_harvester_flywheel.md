# Runbook — Staging Autonomous Data Flywheel (Stack 14)

Operational runbook for Issue #299 / S9-T3: enabling the autonomous harvester
on staging (`warded-stg`, Portainer Stack 14, `home-srv`) so fixed-hour
snapshot collection + auto-labeling run unattended 24/7 — no developer
terminal, no `make dev-all` — and verifying flywheel health from a browser via
`GET /api/harvester/status`.

Audience: operators with Portainer access. Estimated effort: 30 minutes plus
one observation day.

---

## 1. Background & Verified Behavior

The harvester (07:30/12:00/17:30-style slots + rain watcher) and its quality
gates were fully validated during the 2026-10-25 overnight rehearsal on a
developer laptop:

```text
07:30:03 INFO harvester: daily scheduled harvest triggered slot=07:30
07:30:03 INFO harvester: executing scheduled peak harvest cameras=57
07:30:03 WARN harvester: camera harvest failed ... err="invalid JPEG frame: size 2638 bytes (min 20480 bytes) ..."
07:30:07 INFO harvester: scheduled harvest complete harvested=56 total=57
```

- 56/57 cameras auto-labeled (the single failure was a frozen upstream
  placeholder correctly rejected by the JPEG quality gate; the sample archiver
  gained the same gate in PR #306).
- Slot timing is exact to the second; there is **no built-in catch-up before
  this task**: a server that boots after a slot lost that slot for the day.
  This task adds boot-time catch-up (`HARVESTER_CATCHUP_MINUTES`, default 120:
  slots missed within the window fire once at startup) plus the status
  endpoint below.

## 2. Enable on Portainer Stack 14

1. Open Portainer (`home-srv`) > Stacks > **Stack 14** (`warded-stg`) > Editor.
2. Set / confirm the `warded-backend` environment:
   ```yaml
   HARVESTER_ENABLED: "true"
   HARVESTER_SCHEDULE_HOURS: "07:30,12:00,17:30"   # or the dense schedule
   HARVESTER_CONCURRENCY: "15"
   HARVESTER_RAIN_COOLDOWN_MINUTES: "60"
   HARVESTER_CATCHUP_MINUTES: "120"                # 0 disables boot-time catch-up
   STORAGE_DRIVER: "r2"                             # frames + labels to warded-proofs
   ```
   `CV_ENGINE_URL` already routes to the dev-box engine with the internal
   fallback; auto-labeling requires the engine to be healthy.
3. **Update the stack** (re-pull image if a new backend build is needed).
4. Confirm the startup banner in container logs:
   ```text
   [INIT] Autonomous harvester enabled (scheduleSlots=07:30,12:00,17:30, rainCooldown=60m, concurrency=15, hotsetCams=57, rainCams=206)
   ```

## 3. Verify Flywheel Health Without a Terminal

The new endpoint (dev-auth protected: `X-Dev-Token` header, Bearer, or
`?token=`):

```bash
curl -s -H "X-Dev-Token: <DEV_ACCESS_TOKEN>" \
  https://warded-stg.529studio.site/api/harvester/status | jq
```

Reference payload (captured from the live local rehearsal of this feature,
2026-10-25 16:41 ICT — catch-up fired slot 16:40 one minute late at boot):

```json
{
  "enabled": true,
  "schedule_slots": ["16:40"],
  "next_slot": "16:40",
  "next_slot_at": "2026-10-26T09:40:00Z",
  "slot_runs": {
    "16:40": {
      "scope": "scheduled_peak",
      "started_at": "2026-10-25T09:41:09.166842Z",
      "duration_ms": 29295,
      "total_cameras": 57,
      "harvested": 42,
      "failed": 15
    }
  },
  "totals": { "scheduled_runs": 1, "rain_runs": 0, "cameras_harvested": 42, "cameras_failed": 15 },
  "rain_watcher": { "enabled": true, "cooldown_minutes": 60 },
  "recent_errors": []
}
```

Reading it: `slot_runs` shows the latest run per slot (a missing slot after
its time + catch-up window means the server was down); `rain_watcher.last_trigger_at`
confirms rain triggers; `totals` accumulate since backend start; `recent_errors`
collects the last 10 run-level errors.

## 4. One-Day Acceptance Observation

1. **Three scheduled slots**: for each of 07:30 / 12:00 / 17:30 the day after
   enablement, confirm a `slot_runs` entry with `harvested > 0` and the
   corresponding `proofs/<date>/` + `labels/<date>/` objects in R2
   (`rclone lsd R2:warded-proofs/labels/<date>/` or the labeler at
   `https://warded-stg.529studio.site/labeler`).
2. **Rain path**: during rain (or simulated by temporarily lowering the
   watcher threshold in a dev env), verify `rain_watcher.last_trigger_at` set
   once per cooldown window and a `last_rain` run record appears.
3. **Catch-up**: restart the stack within 2 hours after any slot and confirm
   the log line `harvester: catch-up firing missed slot` plus the updated
   `slot_runs` entry.
4. **No regression on home-srv**: capture `docker stats warded-backend` before
   and during a slot; harvester load stays within the existing container
   headroom (fetch concurrency 15, ~30 s burst).

## 5. Rollback

Set `HARVESTER_ENABLED: "false"` (or remove the variable) and update the
stack. The startup log must show `Autonomous harvester disabled`; the status
endpoint keeps answering with `"enabled": false`. No data is removed by
disabling.

## 6. Evidence to Attach When Closing Issue #299

1. Startup banner screenshot/log excerpt from Stack 14.
2. `GET /api/harvester/status` captures across the three daily slots.
3. R2 object counts per slot date (or labeler screenshot showing the day's
   auto-harvested frames).
4. One rain-trigger record (or documented absence of rain during observation).
5. `docker stats` before/after comparison.
