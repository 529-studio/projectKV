# L80: Automated Forecast Refit Pipeline & Live History Model Deployment

**Context**: Sprint 11 Wave 2 (Issue #398 / S11-T4). Building upon the 14-day soak archive ingestion (S11-T2 / L78) and directional roadway calibration (S11-T3 / L79), replacing the synthetic forecast prototype with an invocable refit pipeline via `make train-forecast` (externally schedulable via cron/timer) trained on authentic HCMC traffic history.

---

## 1. Problem & Root Cause

In Sprint 10 (ADR-012 D4, Finding F1), the forecast endpoint `GET /api/v1/cameras/:id/forecast` was implemented with a fail-closed gate that returned HTTP 503 `KindModelUnvalidated` because model parameters were embedded from a synthetic fixture (`synthetic_congestion_5m.csv`).

Because no pipeline existed to automatically extract chronological features, fit Ridge regression models on real traffic history, evaluate against a persistence baseline, and promote verified artifacts, the endpoint remained permanently unavailable in staging and production.

---

## 2. Core Architectural Decisions & Principles

1. **Deterministic Standard-Library Pipeline (`tools/forecast/train_live_model.py`)**:
   - The entire training and evaluation harness runs with zero external scientific dependencies (no numpy, no scikit-learn).
   - Solves regularized normal equations ($X^T X + \lambda I) w = X^T y$ using partial pivoting.
   - Operates on SQLite in strict read-only mode (`file:{db}?mode=ro`) to prevent write locks against running services.

2. **Chronological Splitting & Persistence Gate**:
   - Time-series rows are never shuffled; the split is strictly chronological (`train_ratio = 0.80`).
   - Candidate model (`ridge_delta`) must beat the persistence baseline ($y_{t+h} = y_t$) on held-out test splits for at least one horizon. Shipped evaluation against authentic HCMC history yields:
     - 10 min: Ridge MAE `0.04690819` vs Persistence MAE `0.04693062` (+0.0478% improvement, 285 samples).
     - 20 min: Ridge MAE `0.05152231` vs Persistence MAE `0.05173185` (+0.4051% improvement, 171 samples).
     - 30 min: Ridge MAE `0.05428774` vs Persistence MAE `0.05029311` (-7.9427% degradation, 57 samples).
   - Accepted baseline risk (M2): The 10m margin is narrow (+0.0478%) and 30m shows degradation (-7.94% with limited N=57 test samples); this is accepted as the initial live baseline pending continuous soak accumulation.
   - If candidate MAE exceeds persistence across all horizons, promotion is halted immediately with exit code `EXIT_GATE_FAILED` (3).

3. **Atomic File Promotion & Horizon Coverage (RB1)**:
   - Staging artifacts are written to temporary sibling files before swapping via `os.replace` and parent directory fsync.
   - Pre-promotion validation requires exact coverage of all supported horizons `(10, 20, 30)` to prevent silent runtime degradation to zero-weight persistence.

4. **Leaf Modulith Boot Invariant & Provenance**:
   - Embedded model artifact `services/go-server/internal/forecast/embedded/prototype_v1.json` is updated with `fit_source: "live_history"`, unlocking the endpoint natively without requiring dev override flags (`FORECAST_ALLOW_SYNTHETIC`).
   - Responses report `availability: "ok"`, `validated: true`, `experimental: false`, and calibrated 95% confidence intervals.

5. **Data Limitations & Reproducibility (A5 & A6)**:
   - Initial evaluation coverage derives from 38 distinct 5-minute bins across 58 cameras on 2026-11-03 morning (~3 hours), yielding 30 train bins and 7 test bins (N=57 at 30 min horizon). This limitation is explicitly disclosed and accepted as the initial baseline pending continuous soak accumulation.
   - The operational SQLite database (`services/go-server/data/camera-view.db`) and intermediate feature CSVs are gitignored. The artifact is reproducible in local development via `make train-forecast` against the operational database.

---

## 3. Verification & Guardrails

- `make train-forecast`: Invocable make target executing `tools/forecast/train_live_model.py` with atomic promotion to Go embedded artifact.
- `python3 -m unittest -v tools/forecast/train_live_model_test.py`: 18 comprehensive unit tests validating CLI arguments, feature extraction, gate failure halts, partial horizon rejection, atomic swaps, and Go artifact loading.
- `make test-forecast`: 80 tests covering feature extraction, backtesting, and refit pipeline.
- `cd services/go-server && go test -v -count=1 -race ./internal/forecast/...`: Validates HTTP 200 `availability: "ok"` boot, fail-closed handling on unvalidated models, complete horizon coverage, and monotonic speed estimation.
- `make evidence-lint`: Ratchets `.evidence-integrity-baseline.txt` down by eliminating grandfathered synthetic fixture references for `forecast_prototype_v1.json`.
