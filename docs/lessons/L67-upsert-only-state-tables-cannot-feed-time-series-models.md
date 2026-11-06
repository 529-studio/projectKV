# L67: Upsert-Only State Tables Cannot Feed Time-Series Models

## Context & Problem

Issue #302 assumed that "hotset verdicts persisted since Sprint 7" already provided
a per-camera PCU/saturation-index history. They did not. `camera_verdicts` is an
upsert table with one row per camera, so every 30-second analysis overwrote the
previous one. The forecast could not be trained or backtested from the live
database even though the PCU calculator computed a fresh saturation index on every
cycle.

Two secondary traps sat behind the same assumption:

1. `modernc.org/sqlite` persisted `time.Time` through its Go `String()`
   representation, producing values such as
   `2026-10-11 17:35:13.525256 +0700 +07 m=+0.944197834`. The monotonic suffix is
   not parseable by the Python extractor and does not sort reliably in SQL.
2. A naive `pandas.resample().shift(1)` would treat each emitted bin as one step.
   After a collection gap, `lag_5m` would silently refer to a sample from hours
   earlier, turning stale data into a fabricated feature.

## Root Cause

A current-state table and an event log answer different questions. `camera_verdicts`
answers "what is the state now"; forecasting needs "what was observed, in order,
and how old is each observation". Mutating the latest row destroys the evidence a
time-series model requires. Timestamp encoding and gap semantics are part of the
storage contract, not details for the consumer to guess.

## Solution

- Added `hotset_verdict_history` as an append-only table with
  `UNIQUE(camera_id, observed_at)` and `ON CONFLICT DO NOTHING`, so restarts and
  repeated fan-outs are idempotent.
- Excluded stale or frozen verdicts from the history write. Replaying an unchanged
  frame would create fake variation in lag and rolling features.
- Bound `observed_at` as an explicit RFC3339Nano UTC string in
  `store.VerdictHistoryRepo` instead of binding `time.Time` directly.
- Opened the feature store read-only and anchored each lag to the raw observation
  timeline at `bin_start - offset`, with an explicit `lag_*_age_seconds` column and
  a tolerance that returns null rather than carrying a stale value forward.
- Emitted a CSV plus a metadata sidecar containing the exact feature columns,
  source counts, and time range so a run can be reproduced from the same inputs.

## Verification

- Go integration tests apply the real migration to a temporary SQLite database and
  assert RFC3339Nano storage, duplicate suppression, and validation failures.
- Python tests cover Go-style timestamps, label-note parsing, 5/10-minute bins,
  lag/rolling values, deduplication, and the end-to-end CSV/metadata contract.
- A manual run applied the migration to a temporary database, inserted seven
  synthetic 5-minute observations, and confirmed at `08:30Z`:
  `lag_5m=0.71`, `lag_10m=0.63`, `lag_15m=0.55`, and
  `rolling_mean_15m=0.63`.

## Rule References

- Rule 2: the table was added and wired, not mocked with a synthetic history.
- Rule 4: the gap and stale-timestamp failures were reproduced before the fix.
- Rule 6: schema and feature definitions live in
  `docs/reports/forecast_feature_store_v1.md`.
- Rule 8: the extractor opens SQLite read-only and uses no credentials.
