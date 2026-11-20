# Lesson 511: Hourly Tidal & Convective Rain Timeline Scrubber

## Navigation Aid & Quick Reference

- **Domain**: Hydrological forecasting, astronomical tidal modeling, leaf modulith dependency inversion, interactive timeline ergonomics.
- **Relevant Issues**: Issue #511 (S17-T2), Issue #510, ADR-009, ADR-012, Lesson L90, Lesson L510.
- **Key Files**:
  - `services/go-server/internal/tide/deps.go` (Dependency inversion interface: `WeatherReader`)
  - `services/go-server/internal/tide/tide.go` (Semi-diurnal harmonic tide model, gauge alert thresholds, low-lying road elevation mappings)
  - `services/go-server/internal/tide/handler.go` (`GET /api/forecast/hourly` endpoint)
  - `services/go-server/cmd/server/adapters.go` (`tideWeatherAdapter` adapting `*weather.Service` to `tide.WeatherReader`)
  - `apps/web/src/types/forecast.ts` (Hourly forecast DTO types)
  - `apps/web/src/hooks/useHourlyForecast.ts` (1-minute TTL polling hook)
  - `apps/web/src/components/HourlyTimelineScrubber.tsx` (Monochromatic, zero-emoji, >=48px kiosk touch target timeline slider)
- **Core Invariants**:
  - **Astronomical Tidal Realism**: Semi-diurnal tides in the East Sea / Saigon River basin fluctuate with periods ~12.42h ($M_2$) and ~12.0h ($S_2$), modulated by a ~29.53-day synodic lunar envelope. Water elevations must conform to realistic hydrological ranges ($-1.5\text{m}$ to $+2.2\text{m}$).
  - **Leaf Modulith Boundary**: Module `internal/tide` must not import sibling packages. Meteorological precipitation dependencies are inverted via consumer interface `WeatherReader` and adapted at the composition root (`cmd/server`).
  - **Zero Disruption to Existing Contracts**: Extending shared DTO files (`apps/web/src/types/forecast.ts`) must retain existing short-horizon camera congestion types (`ForecastMultiResponse`, `HorizonPrediction`) while appending new hourly tidal structures.

---

## 1. Context & Problem Statement

Commuters in Ho Chi Minh City face dual flooding vectors: convective rainfall and astronomical tidal surges (*triều cường*). Critical riverside corridors—including Trần Xuân Soạn (Quận 7), Mễ Cốc (Quận 8), Huỳnh Tấn Phát (Nhà Bè), Quốc Hương (Thảo Điền), and Bán đảo Thanh Đa (Bình Thạnh)—submerge under 30–50 cm of water during full moon and new moon tidal peaks even under cloudless skies.

Commuters departing work need a quick 3-second answer to a vital scheduling dilemma:
*"If I leave now at 17:00, will Trần Xuân Soạn be flooded, and will delaying departure to 18:30 avoid the peak?"*

Prior to Issue #511, ProjectKV only provided static weather reports and reactive camera snapshots. Commuters could not anticipate tidal peak windows or explore future flood scenarios across upcoming hours.

---

## 2. Root Cause & Architectural Design

### A. Harmonic Semi-Diurnal Astronomical Model
Rather than requiring brittle external scrapers that break on third-party portal updates, `services/go-server/internal/tide` computes deterministic harmonic predictions based on the 4 primary astronomical constituents:
- $M_2$ (Principal lunar semidiurnal, $T = 12.4206\text{h}$)
- $S_2$ (Principal solar semidiurnal, $T = 12.0000\text{h}$)
- $K_1$ (Luni-solar diurnal, $T = 23.9345\text{h}$)
- $O_1$ (Lunar diurnal, $T = 25.8193\text{h}$)
- Synodic lunar envelope ($T = 29.5306\text{ days}$) modulating spring/neap amplification factor between $0.70$ and $1.30$.

Predictions are calibrated against official alert levels at Phú An (Sài Gòn River) and Nhà Bè (Đồng Điền / Soài Rạp River):
- Alert Level 1: $1.40\text{ m}$
- Alert Level 2: $1.50\text{ m}$ (Overflow threshold for road curbs)
- Alert Level 3: $1.60\text{ m}$ (Critical deep flooding across riverside arterials)

### B. Road Elevation Inversion
Corridor vulnerability is modeled by comparing predicted river gauge height against physical road surface elevations:
- Trần Xuân Soạn: $1.42\text{ m}$ (overflows at $+3\text{ cm}$ at Level 1, impassable at $+26\text{ cm}$ at Level 3).
- Mễ Cốc: $1.40\text{ m}$.
- Bán đảo Thanh Đa: $1.45\text{ m}$.
- Quốc Hương: $1.48\text{ m}$.
- Huỳnh Tấn Phát: $1.50\text{ m}$.

### C. Modulith Dependency Inversion
`services/go-server/internal/tide` requires hourly rain probabilities from Open-Meteo. Following Rule 1 of Modulith Principles, `internal/tide/deps.go` defines:
```go
type WeatherReader interface {
    GetHourlyRainForecast(ctx context.Context) ([]RainForecast, error)
}
```
`services/go-server/cmd/server/adapters.go` implements `tideWeatherAdapter` adapting `*weather.Service`. The `tide` package remains an independent leaf module with zero unauthorized sibling imports.

---

## 3. UI/UX Ergonomics & Interaction

1. **Timeline Slider Dock**:
   Docked horizontally above the bottom dock:
   `[Hiện tại] — [+1h] — [+2h] — [+3h] — [+4h] — [+5h] — [+6h]`
   Each hour segment provides 3 compact lines of data:
   - Offset & local time (e.g., `+2h (20:54)`).
   - Tidal gauge prediction (`0.82m`, turning amber at Alert 1 and red at Alert 2/3).
   - Rain probability and volume (`38% (0mm)`).
2. **Dynamic Scrubbing**:
   Clicking any hour segment updates the active forecast context. A "Về hiện tại" (Reset) button immediately returns the viewport to real-time mode.
3. **Expandable Affected Roads Tray**:
   An expandable drawer surfaces low-lying road segments with predicted water depths in cm (e.g. `Đường Trần Xuân Soạn: +26cm - Nguy cơ chết máy`).
4. **Kiosk Touch Ergonomics**:
   All buttons maintain touch targets $\ge 48\text{px}$ adhering to mobile kiosk ergonomics.

---

## 4. Verification Evidence

1. **Backend Unit Tests**:
   - `services/go-server/internal/tide/tide_test.go`:
     - `TestPredictTide_HarmonicBoundsAndFidelity`: Validates continuous oscillation between $-1.5\text{m}$ and $+2.2\text{m}$ across 48 hours.
     - `TestGetAlertLevel`: Validates 8 threshold boundaries.
     - `TestGetHourlyForecast_Service`: Validates 7-item timeline array and bilingual copy.
     - `TestHandler_RegisterRoutes`: Tests HTTP 200 JSON contract with `httptest.NewRecorder`.
2. **Frontend Unit Tests**:
   - `apps/web/src/components/HourlyTimelineScrubber.test.ts`:
     - Scenario 1: Bilingual compliance and zero-emoji assertion.
     - Scenario 2: Critical tide detection (> 1.60m) mapping to `DANGER` and `IMPASSABLE`.
     - Scenario 3: Minor overflow distinction.
   - Total frontend tests: 458/458 passing.
3. **Playwright E2E Screenshots**:
   - `docs/proofs/511-timeline-scrubber/01_hourly_timeline_docked.png`: Timeline scrubber docked above bottom dock showing 7 hours.
   - `docs/proofs/511-timeline-scrubber/02_scrub_to_future_hour.png`: Scrubbed to `+3h` with selected tab styling.
   - `docs/proofs/511-timeline-scrubber/03_expanded_affected_roads.png`: Expanded affected roads list showing tidal flood depths.
