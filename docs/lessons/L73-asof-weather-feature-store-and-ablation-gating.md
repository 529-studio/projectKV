# Lesson 73: As-Of Weather Feature Store & Ablation Gating

## Context
In Sprint 9 (F3), the short-horizon congestion forecast prototype omitted weather and radar signals because Open-Meteo and RainViewer data resided exclusively in an ephemeral in-memory cache. Consequently, models could not distinguish rain-driven congestion spikes from normal recurring rush-hour peaks. Furthermore, no empirical protocol existed to prove whether meteorological features improved forecast accuracy or degraded clear-weather performance.

## Root Cause
1. **Volatile HTTP-Tied Meteorological State**: Calling external weather APIs only on incoming HTTP requests with short TTLs (5–10 min) meant past observations were discarded at restart, starving historical model training.
2. **Unbounded Forward Fill Risk**: Forward-filling weather across multi-hour gaps fabricates non-existent storm events or assumes perpetual rain, contaminating training datasets (Lesson 67).
3. **Speculative Feature Inclusion**: Adding multi-modal signals without rigorous ablation testing risks degrading simple horizon predictions while increasing feature collection complexity.

## Solution
1. **Append-Only Observation Feature Store (ADR-012 D3)**: Migration `0009_weather_observations.sql` records snapshots (`precipitation_mm`, `weather_code`, `temperature_c`, `humidity_pct`, `radar_active`, `radar_frame_age_s`, `payload_hash`, `observed_at`, `recorded_at`). A 10-minute ticker in `cmd/server/main.go` continuously ingests observations independent of HTTP client traffic.
2. **Strict As-Of Join Discipline**: In `tools/forecast/extract_features.py`, each 5-minute traffic bin joins the closest weather observation within a maximum 30-minute age window ($T - T_{\text{weather}} \le 30\text{m}$). Observations older than 30 minutes evaluate strictly to `None`, eliminating forward-fill hallucinations.
3. **3-Way Empirical Ablation Gating**: In `tools/forecast/backtest.py`, three variants are evaluated on an identical chronological split (Variant A: traffic lags; Variant B: A + weather; Variant C: B + directional). Weather features are retained only if Variant B improves MAE by $\ge 2\%$ on at least one horizon without regressing any horizon by $> 5\%$.
