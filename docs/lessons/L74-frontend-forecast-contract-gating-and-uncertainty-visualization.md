# L74: Frontend Forecast Contract Gating and Uncertainty Visualization

## Context
In Sprint 10 Task 6 (Issue #377 / ADR-012 §D5), we introduced the first client-side consumer of short-horizon traffic congestion forecasts. Prior prototype designs directly rendered single-point saturation numbers without expressing prediction variance, and lacked mechanisms to distinguish genuine live-fitted predictions from unvalidated synthetic models or sparse history gaps.

## Root Cause & Mistrust Risks
Exposing bare point forecasts (`predicted_saturation_index = 0.58`) creates false precision. If history is incomplete (e.g., camera recently rebooted or network dropped), interpolation creates hallucinatory confidence. Furthermore, if a model has not passed live backtest gates (`validated = false`), displaying predictions directly in the commuter UI misleads users and undermines trust during peak commute hours.

## Architectural Decision & Implementation
1. **Fail-Closed Availability Gating**: The frontend enforces strict availability status checks (`ok`, `insufficient_history`, `model_unvalidated`, `stale`). Numeric point predictions and trend lines are suppressed entirely when availability is non-OK; the UI instead renders informative requirement panels (e.g., stating `4` observations required over `45` minutes).
2. **Confidence Band Visualization**: Rather than plotting a lonely sparkline, the component renders a semi-transparent shaded SVG polygon mapping the 95% confidence interval (`lower_95` to `upper_95`) across +10m, +20m, and +30m horizons.
3. **Staleness Dimming**: When observations exceed the 15-minute threshold, the series opacity drops to 50% and displays the observation age in minutes, visually communicating degraded confidence.
4. **4-File Architecture Pattern**: Strictly decoupled into `types/forecast.ts` (DTOs), `utils/forecastApi.ts` (pure fetch), `hooks/useForecast.ts` (abortable 60s polling), and `components/ForecastTimeline.tsx` (monochromatic, accessible, zero-emoji UI).

## Verification & Impact
All 181 frontend tests pass. Viewport responsiveness down to 390px mobile screens and 48px touch targets ensure seamless accessibility across kiosk and smartphone devices. Visual proofs committed under `docs/proofs/sprint10/`.
