# Lesson 444: Forecast Weather Feature Parity, Strict Skill Gating & Off-Box Model Promotion

## Navigation Aid & Quick Reference
- **Domain**: Congestion forecasting, train/serve feature parity, meteorological feature engineering, strict backtest skill gating, calibrated computer vision head promotion.
- **Relevant Issues**: Issue #444 (S14-T4), Issue #398 (S11-T4), Issue #413 (S12-T1), Issue #411 (S12-T5), PR #409, PR #436.
- **Key Files**:
  - `services/go-server/internal/forecast/deps.go` (`WeatherObservation`, `WeatherReader` DIP interfaces)
  - `services/go-server/internal/forecast/model.go` (`Features`, dynamic `ModelParams`, `PredictRidgeDelta`, `ClassifyWeatherCode`, `ComputeWeatherFeatures`, `BuildFeaturesWithWeather`)
  - `services/go-server/internal/forecast/service.go` (`Service` weather integration, `WithWeatherReader`)
  - `services/go-server/cmd/server/adapters.go` (`forecastWeatherAdapter`, wiring `store.WeatherRepo`)
  - `tools/forecast/backtest.py` (strict skill gating: `>= 5%` MAE improvement on `>= 2` horizons, `BacktestConfig`)
  - `tools/forecast/train_live_model.py` (`--feature-set`, strict skill gate evaluation, 12-feature artifact generation)
  - `tools/train/colab_pipeline.py` (measured retraining gate and bundle packaging)
  - `tools/train/train_yolov8_seg.py` (`derive_head_status(0) == "trained"`, OpenVINO FP16 export)
- **Core Invariants**:
  - **Zero feature schema disconnect**: Features used during ridge regression training (`precip_mm_15m`, `precip_mm_60m`, `rain_flag`, `hours_since_rain`, `weather_code_class`, `radar_active`) must be identically available and extracted in online Go serving.
  - **Dynamic backward/forward vector compatibility**: `ModelParams` deserializes parameter slices dynamically (`[]float64`), executing cleanly on both 6-feature baseline and 12-feature weather models without schema breakages.
  - **No negligible improvement promotion**: Models exhibiting trivial improvements (e.g. +0.05% on 1 horizon) are strictly blocked; candidate models must beat the persistence baseline by at least 5.0% on at least 2 forecast horizons on held-out test data.
  - **Authentic head status invariant**: Only fine-tuned weights with 0 re-initialised tensors may carry `head_status: trained`. Re-initialised COCO weights (`head_status: reinitialised_untrained`) must never be benchmarked or promoted to production.

---

## 1. Context & Root Cause Analysis

Prior to Sprint 14 Task 4 (Issue #444), two systemic gaps disconnected forecasting and vision systems from production reality:
1. **Feature Schema Disconnect**: While the weather sampler collected meteorological observations into SQLite (`weather_observations`), online Go serving (`services/go-server/internal/forecast/model.go`) only accepted 6 traffic lag and calendar features (`lag_5m`, `lag_10m`, `lag_15m`, `rolling_mean_15m`, `hour_of_day`, `day_of_week`). The offline ablation tooling in `tools/forecast/backtest.py` had proven that Variant B (+weather) improved forecasting accuracy during rain events, yet online Go serving models could not ingest meteorological features.
2. **Negligible Skill Gating**: The backtest verification gate passed any candidate that beat persistence on a single horizon by as little as 0.05%. A model with 0.0478% improvement on 10m but degraded accuracy on 20m and 30m was eligible for promotion.
3. **Untrained Segmentation Head**: The deployed OpenVINO FP16 weights reported `head_status: reinitialised_untrained` (36 reinitialised tensors), which emitted zero detections on live camera feeds.

---

## 2. Core Architectural Solutions

### A. Train/Serve Meteorological Feature Parity
`services/go-server/internal/forecast/deps.go` defines `WeatherObservation` and `WeatherReader` (`GetAsOf`, `GetHistory`), wired from `store.WeatherRepo` via `forecastWeatherAdapter`.
In `model.go`:
- `Features` is expanded with 6 meteorological fields: `PrecipMm15m`, `PrecipMm60m`, `RainFlag`, `HoursSinceRain`, `WeatherCodeClass`, `RadarActive`.
- `ComputeWeatherFeatures` derives rolling precipitation (15m, 60m), WMO classification (`ClassifyWeatherCode`), rain flag, and elapsed hours since last rain with a strict 30-minute staleness cutoff (ADR-012 D3, Lesson 67).
- `BuildFeaturesWithWeather` extracts traffic and weather features relative to `baseTime`, while preserving `BuildFeatures` as a backward-compatible wrapper.

### B. Dynamic ModelParams & Forward/Backward Compatibility
`ModelParams` parameters (`Means`, `Stds`, `Coeffs`) are refactored from fixed `[6]float64` arrays to slices (`[]float64`).
`PredictRidgeDelta` dynamically loops up to $\min(\text{len}(Coeffs), \text{len}(Features.AsSlice()))$. This provides:
- Seamless backward compatibility: legacy 6-feature artifacts load and evaluate the first 6 features.
- Forward compatibility: Variant B artifacts with 12 features utilize the complete traffic and weather feature vector.

### C. Strict Skill Gating ($\ge 5\%$ MAE Improvement on $\ge 2$ Horizons)
In `tools/forecast/backtest.py` and `tools/forecast/train_live_model.py`:
- `build_verification` now evaluates both raw beats (`beats_persistence_horizons`) and strict skill horizons (`strict_skill_horizons` where `mae_improvement_percent >= min_improvement_pct`).
- When strict skill gating is enforced (`min_improvement_pct=5.0`, `min_horizons=2`), candidate promotion is halted with `EXIT_GATE_FAILED` unless the candidate beats persistence by at least 5.0% across at least 2 forecast horizons on held-out test data.

### D. Off-Box GPU Pipeline & Calibrated Head Promotion
In `tools/train/train_yolov8_seg.py` and `tools/train/colab_pipeline.py`:
- Head status is derived strictly: `derive_head_status(0) == "trained"`.
- When fine-tuning on Dataset v3, measured `val_metrics.json` must satisfy `water_puddle` IoU $\ge 0.612$, mAP50-95 $\ge 0.496$, and p95 latency $\le 25.0$ ms on Linux x86_64 AVX2 workers before model promotion.

---

## 3. Verification & Guardrails

- `cd services/go-server && go test -race ./...`: 100% pass across all packages, verifying `TestPredictRidgeDelta_12Features`, `TestComputeWeatherFeatures`, `TestClassifyWeatherCode`, and `TestBuildFeaturesWithWeather`.
- `python3 -m unittest discover -s tools/forecast -p "*test*.py"`: 148 tests pass, verifying strict skill gating, weather feature parity, and hermetic Go execution via `TestArtifact_ExternalFileLoad`.
- `python3 -m unittest discover -s tools/train -p "*test*.py"`: 137 tests pass, verifying head status derivation, measured retraining gates, and AVX2 benchmark evidence.
- `python3 tools/docs/docs_lint.py`: 0 new violations, 0 stale exemptions.
- `bash scripts/check-public-safe.sh`: 0 credential leaks, 0 private infrastructure references.
