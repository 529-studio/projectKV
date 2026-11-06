# Autonomous Harvester Data Flywheel Staging & Operational Runbook

## Overview

This runbook documents the architecture, operational verification, observability probes, and emergency procedures for the autonomous harvester data flywheel on staging and production environments.

Issue Context: S9-T3 (#299), S10-T4 (#375) / [ADR-012](../ADR-012-sprint-10-architecture.md) D2.

The data flywheel autonomously accumulates high-resolution traffic snapshots and municipal flood event frames to continuously train, validate, and refit downstream time-series forecasting models (S10-T1, S10-T5) and segmentation vision networks (S10-T7).

---

## Architecture & Triggering Mechanics

The autonomous harvester subsystem runs in `services/go-server/internal/harvester/` as a background coordinator with two independent triggers:

1. **Daily Scheduled Peak Trigger (`ScopeScheduled`)**:
   - Fires at three fixed commuter peak intervals in Indochina Time (ICT, UTC+7):
     - `07:30` (Morning commute onset)
     - `12:00` (Midday peak)
     - `17:30` (Evening commute onset)
   - Targets the 57 priority dual-carriageway arterial intersection cameras defined in `FilterHotsetCameras` (corridors: `vo_van_kiet`, `mai_chi_tho`, `dien_bien_phu`, `cong_hoa`).
   - Implements a 2-hour catch-up window: if the server restarts shortly after a slot, the missed slot fires immediately rather than losing the day's collection.

2. **Meteorological Rain Watcher Trigger (`ScopeRain`)**:
   - Polls weather telemetry (Open-Meteo precipitation rate $\ge 5.0\text{ mm/h}$ or convective rain weather codes $61, 63, 65, 80, 81, 82$).
   - Enforces a 60-minute sliding cooldown to avoid flooding storage during protracted monsoons.
   - Targets the 24 historical flood-prone corridors defined in `FilterFloodCorridorCameras`.

3. **Missed-Slot Watchdog (ADR-012 D2)**:
   - Continuously evaluates whether any scheduled slot from today was not executed within 15 minutes of its scheduled onset.
   - Automatically flags overdue slots in `missed_slots` in status telemetry.

4. **30-Day Data Retention Job (ADR-012 D2)**:
   - Executes daily chunked purges (`LIMIT 5000`) on `hotset_verdict_history` to delete observations older than 30 days without holding SQLite write locks.

---

## Storage & Cloudflare R2 Layout

All harvested frames and JSON manifests are uploaded to Cloudflare R2 object storage:

```
harvester/
├── scheduled_peak/
│   └── YYYY-MM-DD/
│       └── HHMM/
│           ├── <camera_id>.jpg             # Authenticated snapshot (min 20 KB)
│           └── <camera_id>.manifest.json    # Draft-07 compliant ground truth manifest
└── rain_watcher/
    └── YYYY-MM-DD/
        └── HHMM/
            ├── <camera_id>.jpg
            └── <camera_id>.manifest.json
```

Each manifest contains camera metadata, observation timestamp, vision model predictions, vehicle counts, weather conditions, and directional roadway polygons when registered.

---

## Environment Configuration

The following variables in `.env` govern harvester behavior:

| Variable | Type | Default | Description |
|---|---|---|---|
| `HARVESTER_ENABLED` | bool | `true` | Master switch for harvester coordinator |
| `HARVESTER_SCHEDULE_HOURS` | string | `"07:30,12:00,17:30"` | Comma-separated ICT trigger slots |
| `HARVESTER_RAIN_COOLDOWN_MINUTES` | int | `60` | Minimum minutes between rain watcher triggers |
| `HARVESTER_CONCURRENCY` | int | `15` | Worker pool size for concurrent camera processing |
| `STORAGE_DRIVER` | string | `"s3"` | Storage backend (`s3` for Cloudflare R2, `local` for disk) |
| `R2_BUCKET` | string | `""` | Cloudflare R2 target bucket |
| `R2_ACCESS_KEY_ID` | string | `""` | R2 HMAC access key ID (Rule 8: no fallbacks) |
| `R2_SECRET_ACCESS_KEY` | string | `""` | R2 HMAC secret access key |
| `R2_ENDPOINT` | string | `""` | Cloudflare R2 endpoint URL |

---

## Observability & Health Probes

### 1. Harvester Subsystem Status
```bash
curl -s http://localhost:8924/api/harvester/status \
  -H "X-Dev-Token: <your-dev-access-token>" | jq .
```
Response fields:
- `enabled`: boolean master state.
- `schedule_slots`: `["07:30", "12:00", "17:30"]`.
- `next_slot`: next upcoming slot label.
- `next_slot_at`: RFC3339 UTC timestamp.
- `missed_slots`: array of overdue slots requiring operator review.
- `last_success_at`: timestamp of last successful harvest.
- `history_coverage_hours`: continuous operational hours spanned.

### 2. Time-Series History Coverage API
```bash
# Single camera trailing 72 hours
curl -s "http://localhost:8924/api/v1/hotset/history/coverage?camera_id=56de42f611f398ec0c48128b&window=72h" | jq .

# Aggregate across all Hot Set cameras
curl -s "http://localhost:8924/api/v1/hotset/history/coverage?window=72h" | jq .
```
Response fields:
- `coverage_ratio`: ratio of 10m bins containing valid observations.
- `longest_gap_seconds`: maximum gap between contiguous observations.
- `horizon_eligibility`: serving (`>= 80%` over trailing 60m) and refit (`>= 70%` over 72h) flags.

### 3. Server Schema Health
```bash
curl -s http://localhost:8924/healthz | jq .
```
Verifies `schema.history_ready: true`. If migrations 0007 or 0008 are missing, the server halts at boot with exit code 1.

---

## 72-Hour Flywheel Soak Protocol

To verify contiguous data accumulation over a 72-hour window:

1. **Verify Database Readiness**:
   ```bash
   sqlite3 data/traffic.db "SELECT COUNT(*) FROM hotset_verdict_history;"
   ```
2. **Execute Soak Verification Suite**:
   ```bash
   python3 tools/harvester/verify_flywheel_soak.py --verify
   ```
   Asserts that 9 scheduled runs and 1 rain run completed with zero camera failures and all R2 synchronization proofs verified.
3. **Commitment Proof**:
   Results are recorded in `docs/proofs/sprint10/harvester_observation.json`.

---

## Emergency Rollback & Troubleshooting

### Emergency Disable
If downstream vision inference or camera networks degrade, disable harvesting immediately without restarting `go-server`:
```bash
# In .env:
HARVESTER_ENABLED=false
```
Or restart with `HARVESTER_ENABLED=false`. All other server components (hotset verdicts, subscriptions, REST APIs) continue operating normally.

### Missed Slot Remediation
If `missed_slots` contains overdue slots:
1. Verify network reachability to camera proxy cache (`/api/cameras/snapshot/:id`).
2. Verify CV inference engine status at `http://localhost:8000/healthz`.
3. Check `recent_errors` in `/api/harvester/status` for DNS timeouts or storage rate limits.
4. Catch-up will automatically fire slots that occurred within the trailing 2 hours on next reboot.
