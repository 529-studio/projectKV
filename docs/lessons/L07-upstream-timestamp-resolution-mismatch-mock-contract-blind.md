## Lesson 7: Upstream Timestamp Resolution Mismatch & Mock Contract Blind Spots

- **Incident & Tracking:** Issue #172, Pull Request (`fix/172-weather-hourly-forecast-current-hour`).
- **Symptom:**
  At local daytime (e.g. 14:20 GMT+7 in Vietnam), the Weather Widget popup displayed the next 6-hour rain forecast as `00:00` to `05:00` (midnight to early morning) instead of `14:00` to `19:00`, and the current precipitation probability displayed the 00:00 slot value (64%) instead of the current hour's probability.
- **Root Cause:**
  1. **Resolution Divergence**: Upstream Open-Meteo returns `current.time` at 15-minute intervals (e.g. `"2026-10-06T14:15"`), whereas `hourly.time` slots are strictly on-the-hour (`"2026-10-06T14:00"`, `"2026-10-06T15:00"`).
  2. **Naive String Equality**: In `services/go-server/internal/weather/service.go`, exact string comparison `if h.Time == currentTimeStr` always evaluated to `false`. Consequently, `currentIdx` defaulted to `0` (midnight), and `prob` fell back to `hourlyList[0].PrecipitationProbability`.
  3. **Single-Day Lookahead Truncation**: Querying `forecast_days=1` only yielded 24 hours of data. If queried late in the evening (e.g. 20:00), the 12-hour slice capped at 24:00, providing only 4 hours instead of a full lookahead into tomorrow morning.
  4. **Mock Blind Spot (Reaffirming Lesson 1)**: Unit test fixtures hardcoded `current.time` and `hourly.time` both to `"2026-10-02T15:00"`, hiding the upstream API contract constraint during automated testing.
- **Mandatory Takeaway:**
  1. **Timestamp Normalization**: When matching time series with varying sample intervals (15-min current vs. 60-min hourly), normalize timestamps to the target resolution bucket (`truncateToHour`) before index lookup.
  2. **Cross-Midnight Horizon Buffering**: Multi-hour rolling lookaheads across midnight require querying at least 2 forecast days (`forecast_days=2`) to guarantee an unconstrained rolling window.
  3. **Realistic Mock Contracts**: Test fixtures must simulate real-world API responses with non-zero minute offsets and multi-day arrays to prevent false positives.
