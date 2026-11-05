# L68: Chronological Splits Still Leak at Horizon Boundaries

## Context & Problem

A forecast backtest naturally splits a time series at a cutoff timestamp: rows
before the cutoff train the model, rows after it score the model. That looks
leakage-free, and it is not enough for a multi-horizon target.

At a 30-minute horizon, a row at `cutoff - 5 minutes` is a training row, but its
target is `cutoff + 25 minutes`, which lies inside the test window. Fitting on
that pair teaches the model from a future observation it is later scored on. The
leak is small in row count and easy to miss because the current timestamp still
looks safely historical.

A second, quieter failure sits next to it: if persistence is scored on every row
with a current value while the ridge model is scored only on rows with complete
lag features, their MAE numbers describe different populations. The comparison
then measures dataset composition, not model skill.

## Root Cause

The split boundary was defined over observation timestamps only. A supervised
forecast sample has two timestamps, `t` and `t + horizon`, and both must respect
the partition. Likewise, a model comparison needs one common eligible set, not
per-model convenience sets.

## Solution

`tools/forecast/backtest.py` treats the split and the evaluation set as explicit
contracts:

- The split is global and chronological over distinct `bin_start_utc` values.
  Every camera sharing a timestamp lands on the same side of the cutoff.
- For every horizon, a training sample is kept only when its target timestamp is
  strictly before the cutoff. Rows whose target crosses the boundary are counted
  as `train_purged_boundary`.
- A target must exist at the exact future bin for the same camera. Missing
  targets and incomplete lag features are excluded and counted, never
  interpolated or carried forward.
- The historical median baseline buckets training targets by the target's local
  hour, not the current observation's hour, so a horizon that crosses an hour
  boundary is scored against the right climatology.
- Calendar columns are validated against `bin_start_utc` in the configured
  timezone. A CSV that wrote UTC hours into a local-hour column fails instead of
  silently training on the wrong seasonality.
- Persistence, historical median, and ridge regression are all scored on the
  same eligible test samples. The JSON records the common sample count and the
  exclusion counts per horizon.
- The verification gate is explicit: the candidate must beat persistence MAE on
  at least one horizon. A tie does not pass, and a failed gate still writes the
  JSON and Markdown diagnostics before returning exit code 3.

## Verification

- Unit tests cover exact target alignment, same-camera lookup, boundary purging,
  missing-target and missing-feature accounting, and the tie-fails gate.
- The end-to-end test generates a deterministic 5-minute series, asserts the
  70/30 split boundary and row counts, and verifies that the candidate beats
  persistence on all three horizons.
- The committed synthetic fixture is exercised end-to-end, so the fixture, the
  harness, and the committed report cannot drift apart unnoticed.
- A constant-series test confirms the failure path: equal MAE is a tie, the gate
  reports failure, and both output files are still written.
- `make test-forecast` runs the extraction and backtest suites together, and CI
  runs the same target for `tools/**` changes.

## Rule References

- Rule 2: boundary leakage and mismatched evaluation sets were fixed in the
  harness, not hidden in the report prose.
- Rule 3: the horizon purge and common evaluation set were designed before
  reporting any metric.
- Rule 6: the method is documented in
  `docs/reports/forecast_prototype_v1.md`, and the fixture provenance is labelled
  in `tools/forecast/fixtures/README.md`.
- Rule 10: the gate was exercised on both a passing series and an honest failing
  series, not only on the happy path.
