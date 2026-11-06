# Forecast Feature Store v1

Part 1 of Issue #302 (S9-T6: Short-Horizon Congestion Forecast Prototype).

This document defines the persistence schema and the feature extraction contract
used by the 10/20/30-minute congestion forecast prototype. Part 2 (backtest) and
Part 3 (internal forecast endpoint) consume the CSV produced here; no user-facing
UI is part of this document.

## 1. Why a separate history table

`camera_verdicts` is an upsert table with one row per camera. It answers "what is
the state now" for `/api/v1/cameras/hotset/status`, but it discards every prior
observation, including the PCU total and saturation index that the forecast
needs. `hotset_verdict_history` is therefore append-only:

- every Hot Set analysis appends one row;
- duplicate `(camera_id, observed_at)` writes are ignored, making restarts and
  repeated fan-outs idempotent;
- stale or frozen verdicts are not appended, because replaying an unchanged
  frame would fabricate variation in lag and rolling features;
- the feature extractor opens the database read-only and never writes back.

## 2. Table schema

Migration: `services/go-server/internal/store/migrations/0007_hotset_verdict_history.sql`.

```sql
CREATE TABLE IF NOT EXISTS hotset_verdict_history (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    camera_id         TEXT NOT NULL,
    pcu_total         REAL,
    saturation_index  REAL,
    severity          TEXT NOT NULL,
    confidence        REAL NOT NULL DEFAULT 0.0,
    source            TEXT NOT NULL DEFAULT '',
    observed_at       TIMESTAMP NOT NULL,
    recorded_at       TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (pcu_total IS NULL OR pcu_total >= 0.0),
    CHECK (saturation_index IS NULL OR saturation_index >= 0.0)
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_hotset_verdict_history_camera_observed
    ON hotset_verdict_history(camera_id, observed_at);
CREATE INDEX IF NOT EXISTS idx_hotset_verdict_history_observed
    ON hotset_verdict_history(observed_at);
CREATE INDEX IF NOT EXISTS idx_hotset_verdict_history_camera_recorded
    ON hotset_verdict_history(camera_id, recorded_at);
```

| Column | Type | Meaning |
| --- | --- | --- |
| `id` | INTEGER | Surrogate key for append order. |
| `camera_id` | TEXT | Upstream camera identifier. Joins the camera catalog. |
| `pcu_total` | REAL, nullable | Passenger Car Equivalent total from `PCUCalculator`. |
| `saturation_index` | REAL, nullable | `pcu_total / effective_capacity`; the forecast target. |
| `severity` | TEXT | `CLEAR`, `MODERATE`, `HEAVY`, `CONGESTED`, or `UNKNOWN`. |
| `confidence` | REAL | Analyzer confidence, normally `0.0` to `1.0`. |
| `source` | TEXT | `onnx`, `vlm`, `mock`, `label_backfill`, and so on. |
| `observed_at` | TIMESTAMP | Upstream frame observation time, written as RFC3339Nano UTC. |
| `recorded_at` | TIMESTAMP | Persistence time, UTC; independent of `observed_at`. |

`observed_at` is deliberately stored as an RFC3339Nano UTC string rather than
letting the SQLite driver persist `time.Time.String()`. The latter includes a
monotonic suffix such as ` m=+0.944197834`, which is not portable to Python or
SQLite date functions. The Go writer converts explicitly in
`services/go-server/internal/store/history_repo.go`.

`pcu_total` and `saturation_index` are nullable so older sources can append a
label-only observation. The extractor drops rows without a saturation index,
because that value is the training target.

## 3. Write path

`cmd/server/main.go` wires `store.VerdictHistoryRepo` next to the existing
`VerdictRepo`. On Fan-out 2b of every Hot Set verdict:

1. `v.Stale == false` is required.
2. The verdict must carry a PCU/SI series (`source == "onnx"`, or a non-zero
   PCU/SI) and must not be an `UNKNOWN` error verdict. A VLM-only severity label
   is not persisted as a zero-saturation observation.
3. `observed_at` falls back to `timestamp` if the analyzer left it zero.
4. `INSERT ... ON CONFLICT(camera_id, observed_at) DO NOTHING` appends the row.
5. A write failure is logged as `[WARN]` and does not interrupt the live fan-out.

The existing `camera_verdicts` upsert is unchanged, so status endpoints and
hydration behave exactly as before.

## 4. Feature extraction contract

Script: `tools/forecast/extract_features.py` (Python standard library only).

```bash
python3 tools/forecast/extract_features.py \
  --db data/camera-view.db \
  --bin-minutes 5 \
  --labels-root data/harvested/labels \
  --labels-root data/proofs \
  --out data/forecast/features_5m.csv
```

Default behaviour:

- reads `hotset_verdict_history` in read-only URI mode;
- backfills `data/harvested/labels` and `data/proofs` recursively for `*.json`;
- accepts `--bin-minutes 5` or `--bin-minutes 10`;
- interprets naive timestamps in `Asia/Ho_Chi_Minh`;
- drops label rows that duplicate a SQLite `(camera_id, observed_at)` pair;
- writes a CSV plus a `<out>.meta.json` sidecar with row counts, source stats,
  time range, and the exact feature column list.

Label manifests are accepted when they contain `camera_id`, a timestamp
(`observed_at`, `annotated_at`, `captured_at`, or `timestamp`), and a saturation
index. The index may be a direct key (`saturation_index`, `si`,
`saturationIndex`) or encoded in free-text notes as `pcu=12.3; SI=0.2733`.
Labels without a saturation index are skipped rather than imputed.

### 4.1 Time bins

Every observation is floored to a regular UTC boundary:

```text
bin_start = floor(epoch_seconds / (bin_minutes * 60)) * bin_minutes * 60
```

Only bins that contain at least one saturation-index sample are emitted. Empty
bins carry no target and would otherwise create enormous artificial gaps after
collection outages. Bin boundaries themselves are always regular, and lag and
rolling windows are computed from the raw observation timeline, so a missing bin
cannot shift a value into the wrong bucket.

Multiple observations in one bin are aggregated as follows:

| Field | Aggregation |
| --- | --- |
| `saturation_index` | Arithmetic mean of samples in the bin. |
| `pcu_total` | Arithmetic mean of non-null samples. |
| `severity` | Mode; ties resolve to the worst observed label. |
| `confidence` | Arithmetic mean of non-null samples. |
| `source` | Sorted unique sources joined with `|`. |

### 4.2 Lag features

`lag_5m`, `lag_10m`, and `lag_15m` are the latest saturation-index sample at or
before `bin_start - offset`. They are anchored to the bin start, not to the bin
width, so a 10-minute bin still asks for a sample 5 and 10 minutes before its
start. Observations without a saturation index are skipped so an older valid
sample can serve the lag.

`lag_<N>m_age_seconds` records how old the selected sample is. If the newest
usable sample is older than `--lag-tolerance-minutes` (default `5`), the lag is
left empty rather than silently carried forward. This is the guard against
stale history becoming a fabricated feature.

### 4.3 Rolling and calendar features

| Column | Definition |
| --- | --- |
| `rolling_mean_15m` | Mean saturation index in `[bin_start - 15m, bin_start)`. |
| `rolling_std_15m` | Population standard deviation over the same window; empty with fewer than two samples. |
| `rolling_count_15m` | Number of saturation-index samples in the window. |
| `hour_of_day` | Local hour, `0` to `23`. |
| `day_of_week` | Local weekday, `0` = Monday, `6` = Sunday. |
| `is_weekend` | `1` when `day_of_week >= 5`, otherwise `0`. |

### 4.4 CSV columns

```text
camera_id, bin_start_utc, bin_start_local, bin_minutes,
observation_count, saturation_sample_count, pcu_total, saturation_index,
severity, confidence, source,
lag_5m, lag_10m, lag_15m,
lag_5m_age_seconds, lag_10m_age_seconds, lag_15m_age_seconds,
rolling_mean_15m, rolling_std_15m, rolling_count_15m,
hour_of_day, day_of_week, is_weekend
```

`bin_start_utc` uses a trailing `Z`. `bin_start_local` carries the configured
offset, for example `2026-11-05T17:15:00+07:00`.

## 5. Reproducibility and validation

The extractor is deterministic for a fixed database, label tree, clock window,
and argument set. Reproduce a run by keeping the metadata sidecar with the CSV:
it records the bin width, timezone, lag offsets, tolerance, rolling window,
feature column list, camera list, row count, and source statistics.

Unit and integration tests:

```bash
python3 -m unittest tools.forecast.extract_features_test -v
cd services/go-server && go test ./internal/store/...
```

The integration test applies the real `0007_hotset_verdict_history.sql`
migration to a temporary database, inserts both RFC3339 and Go
`time.Time.String()` timestamps, and asserts the end-to-end CSV and metadata
contract.

## 6. Known limits

- Historical label manifests often contain one sample per camera, so lag and
  rolling columns are empty until the live Hot Set accumulates several cycles.
- `annotated_at` is used when a label has no explicit observation time. For
  batch-generated labels this can be minutes after capture; the live SQLite
  path always carries the analyzer's real `observed_at`.
- The prototype has no retention policy. Before running the history table in a
  long-lived environment, decide whether to downsample old rows or prune by
  `recorded_at`; the forecast only needs a bounded lookback window.
- Part 2 is implemented by `tools/forecast/backtest.py` and documented in
  `docs/reports/forecast_prototype_v1.md`. It performs the chronological split,
  exact future-target alignment, baseline comparison, and verification gate.
  The `/forecast` endpoint remains Part 3 of Issue #302 and is not covered here.
