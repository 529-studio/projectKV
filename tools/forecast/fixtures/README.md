# Forecast backtest fixture

`synthetic_congestion_5m.csv` is a deterministic, synthetic feature table used to
validate the backtest harness and the Markdown report renderer. It is not
production data and must not be cited as evidence of forecast quality.

## Shape

- Two cameras: `cam-hotset-01`, `cam-hotset-02`.
- Two days of 5-minute bins starting at `2026-10-28T00:00:00Z`.
- 1,152 rows total; every row carries complete `lag_5m`, `lag_10m`, `lag_15m`,
  `rolling_mean_15m`, `hour_of_day`, and `day_of_week` values after the first
  three bins of each camera.

## Signal

For camera index `c` (0 or 1) and 5-minute step `i`, with `minute_of_day`
computed in `Asia/Ho_Chi_Minh` to match the feature store contract:

```text
minute_of_day = local_hour * 60 + local_minute
saturation_index =
    0.30
  + 0.10 * c
  + 0.20 * sin(2 * pi * (minute_of_day - 420) / 1440)
  + 0.05 * sin(2 * pi * minute_of_day / 240)
  + 0.02 * i / 288
```

The daily cycle, four-hour cycle, and slow drift are smooth, so a lag-based
model can legitimately outperform persistence on this fixture. The fixture
therefore tests the mechanics of the harness, not whether the real Hot Set
signal is predictable.

## Why it exists

`hotset_verdict_history` in the live database contained zero rows when the
harness was built. Without a committed fixture, the report generator could only
be demonstrated against an uncommitted temporary file. The fixture keeps the
report reproducible while the provenance note in the report makes the synthetic
origin explicit.

Regenerate the report:

```bash
python3 tools/forecast/backtest.py --input tools/forecast/fixtures/synthetic_congestion_5m.csv --json-out docs/reports/forecast_prototype_v1.json --markdown-out docs/reports/forecast_prototype_v1.md --data-note 'Synthetic harness fixture; live hotset_verdict_history had 0 rows on 2026-11-05, so these numbers are not production evidence. Regenerate from data/forecast/features_5m.csv once history accumulates.' --log-level INFO
```

Replace `--input` with `data/forecast/features_5m.csv` once live history has
accumulated.
