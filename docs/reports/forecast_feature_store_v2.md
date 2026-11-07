# Forecast Feature Store v2

Feature Store specification and contract for Issue #376 (S10-T5: Weather & Precipitation Feature Store v2 with Ablation).

This document defines the schema, sampling pipeline, as-of join semantics, and feature catalog for the short-horizon congestion forecast feature store v2. It supersedes `docs/reports/forecast_feature_store_v1.md` by incorporating meteorological signals (Open-Meteo), city-scale radar state (RainViewer), and directional roadway polygon signals (ADR-012 §D1 & §D3).

---

## 1. Meteorological & Radar Observation Persistence

In Feature Store v1, weather data existed solely as an in-memory cache in `services/go-server/internal/weather/service.go`. As a result, historical traffic slowdowns caused by tropical rainstorms could not be distinguished from ordinary peak congestion.

### 1.1 Database Migration

Migration `services/go-server/internal/store/migrations/0009_weather_observations.sql`:

```sql
CREATE TABLE IF NOT EXISTS weather_observations (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    precipitation_mm   REAL NOT NULL DEFAULT 0.0,
    weather_code       INTEGER NOT NULL DEFAULT 0,
    temperature_c      REAL NOT NULL DEFAULT 0.0,
    humidity_pct       REAL NOT NULL DEFAULT 0.0,
    radar_active       INTEGER NOT NULL DEFAULT 0,
    radar_frame_age_s  INTEGER NOT NULL DEFAULT 0,
    source             TEXT NOT NULL,
    payload_hash       TEXT NOT NULL DEFAULT '',
    observed_at        TEXT NOT NULL,
    recorded_at        TEXT NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_weather_obs_source_time
    ON weather_observations(source, observed_at);

CREATE INDEX IF NOT EXISTS idx_weather_obs_observed_at
    ON weather_observations(observed_at);
```

### 1.2 Ingestion Cadence & Decoupling

A background ticker in `services/go-server/cmd/server/main.go` executes every 10 minutes (`10 * time.Minute`), invoking `weather.Sampler`:

1. Queries Open-Meteo for real-time precipitation (`rain_mm`), WMO weather code, temperature, and relative humidity.
2. Queries RainViewer for city-scale radar frame availability and timestamp.
3. Computes a deterministic SHA-256 payload hash over the normalized readings.
4. Persists the observation into `weather_observations` using `ON CONFLICT(source, observed_at) DO NOTHING`.
5. Operates completely independent of the client-facing HTTP cache TTL (`DefaultWeatherTTL = 10m`, `DefaultRadarTTL = 5m`).

---

## 2. As-Of Join Discipline & Boundary Gating

Joining time-series across mismatched sampling rates (traffic at 10–30s intervals vs weather at 10–15m intervals) introduces risks of lookahead bias or fabricated variations (Lesson 67).

Feature Store v2 enforces strict as-of join constraints in `tools/forecast/extract_features.py`:

```text
Target Traffic Bin Start: T
Closest Prior Weather Observation: T_weather <= T
Observation Age: dt = T - T_weather
```

- **Cutoff Window**: $\text{max\_age} = 30 \text{ minutes}$ (1,800 seconds).
- **Valid Observation ($0 \le dt \le 1800\text{s}$)**: Features (`precip_mm_15m`, `precip_mm_60m`, `rain_flag`, `hours_since_rain`, `weather_code_class`, `radar_active`) are populated.
- **Stale Observation ($dt > 1800\text{s}$)**: All 6 weather columns evaluate to `None` (empty CSV cells).
- **Forward-Fill Prohibition**: Values are never forwarded across missing periods or database restart gaps.

---

## 3. Feature Store v2 Column Catalog

The output CSV schema produced by `tools/forecast/extract_features.py` (`FEATURE_SCHEMA_VERSION = 2`):

| Column | Type | Category | Description |
| --- | --- | --- | --- |
| `camera_id` | TEXT | Identifier | Camera identifier matching catalog. |
| `direction` | TEXT | Identifier | Roadway direction (`a`, `b`, or empty string `""` for frame-wide). |
| `bin_start_utc` | TIMESTAMP | Temporal | Regular UTC bin start boundary (ISO 8601). |
| `bin_start_local` | TIMESTAMP | Temporal | Local bin start (`Asia/Ho_Chi_Minh`). |
| `bin_minutes` | INTEGER | Config | Bin width (5 or 10 minutes). |
| `observation_count` | INTEGER | Telemetry | Count of raw analysis records in bin. |
| `saturation_sample_count` | INTEGER | Telemetry | Count of valid non-null SI records in bin. |
| `pcu_total` | REAL | Traffic | Mean passenger car unit equivalent in bin. |
| `saturation_index` | REAL | Target | Mean saturation index $SI \in [0, \infty)$; training target. |
| `severity` | TEXT | Traffic | Modal traffic severity (`CLEAR`, `MODERATE`, `HEAVY`, `CONGESTED`). |
| `confidence` | REAL | Traffic | Mean analyzer confidence score. |
| `source` | TEXT | Provenance | Analyzer source (`onnx`, `label_backfill`, etc.). |
| `lag_5m` | REAL | Traffic Lag | Saturation index 5 minutes prior to bin start. |
| `lag_10m` | REAL | Traffic Lag | Saturation index 10 minutes prior to bin start. |
| `lag_15m` | REAL | Traffic Lag | Saturation index 15 minutes prior to bin start. |
| `lag_5m_age_seconds` | REAL | Quality | Age of observation used for 5m lag. |
| `lag_10m_age_seconds` | REAL | Quality | Age of observation used for 10m lag. |
| `lag_15m_age_seconds` | REAL | Quality | Age of observation used for 15m lag. |
| `rolling_mean_15m` | REAL | Rolling | Mean saturation index across trailing 15-minute window. |
| `rolling_std_15m` | REAL | Rolling | Standard deviation across trailing 15-minute window. |
| `rolling_count_15m` | INTEGER | Rolling | Number of samples in trailing 15-minute window. |
| `hour_of_day` | INTEGER | Calendar | Local hour of day ($0 \dots 23$). |
| `day_of_week` | INTEGER | Calendar | Local day of week ($0 = \text{Monday} \dots 6 = \text{Sunday}$). |
| `is_weekend` | INTEGER | Calendar | 1 if Saturday or Sunday, else 0. |
| `precip_mm_15m` | REAL | Weather | Precipitation accumulated in trailing 15 minutes. |
| `precip_mm_60m` | REAL | Weather | Precipitation accumulated in trailing 60 minutes. |
| `rain_flag` | INTEGER | Weather | Binary indicator: 1 if `precip_mm_15m > 0` or rain code active, else 0. |
| `hours_since_rain` | REAL | Weather | Elapsed hours since last observed rain event (capped at 24.0h baseline). |
| `weather_code_class` | INTEGER | Weather | Discrete WMO class (0: Clear, 1: Cloudy, 2: Drizzle, 3: Heavy Rain, 4: Storm). |
| `radar_active` | INTEGER | Radar | RainViewer city radar coverage indicator (1: active frames, 0: none). |
| `peer_si` | REAL | Directional | Concurrent saturation index of opposing roadway direction (`a` vs `b`). |
| `si_delta_ab` | REAL | Directional | Symmetrical saturation delta ($SI_a - SI_b$) between directions. |

---

## 4. Directional Roadway Pairing

When cameras have registered directional polygons (ADR-012 §D1 / Migration 0008), both directions `a` and `b` are extracted concurrently:

- For direction `a`:
  - `peer_si`: concurrent $SI_b$
  - `si_delta_ab`: $SI_a - SI_b$
- For direction `b`:
  - `peer_si`: concurrent $SI_a$
  - `si_delta_ab`: $SI_a - SI_b$
- For unregistered or frame-wide cameras (`direction = ""`):
  - `peer_si`: `None` (empty)
  - `si_delta_ab`: `None` (empty)

This formulation ensures that directional asymmetry is captured without duplicating camera entities or leaking future information.

---

## 5. Verification Commands

Extract features with weather and directional support:

```bash
python3 tools/forecast/extract_features.py \
    --db data/camera-view.db \
    --bin-minutes 5 \
    --include-directional \
    --out data/forecast/features_5m.csv
```

Run 3-way ablation backtest:

```bash
python3 tools/forecast/backtest.py \
    --input data/forecast/features_5m.csv \
    --ablation \
    --ablation-markdown-out docs/reports/forecast_ablation_v2.md \
    --ablation-json-out docs/reports/forecast_ablation_v2.json
```
