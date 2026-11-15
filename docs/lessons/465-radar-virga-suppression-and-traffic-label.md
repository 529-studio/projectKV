# Lesson 465: Doppler Radar Virga Suppression Pitfall, NWP Model Latency, and Commuter Semantic Disambiguation

## Navigation Aid & Quick Reference
- **Domain**: Weather radar pipelines, Doppler reflectivity (dBZ), Numerical Weather Prediction (NWP) latency, Leaflet Canvas rendering, Modal semantics, Bilingual UI.
- **Relevant Issues**: Issue #465, Issue #269 (Doppler radar overhaul), Lesson 41.
- **Key Files**:
  - `apps/web/src/utils/rainHexUtils.ts` (`resolveRadarHexSeverity`, Doppler reflectivity tiering, NWP latency protection)
  - `apps/web/src/utils/rainHexUtils.test.ts` (Unit test assertions for convective core protection)
  - `apps/web/src/hooks/useRainHexRadar.ts` (Doppler radar ingestion, H3 Resolution 7 7-point sampling)
  - `apps/web/src/utils/floodUtils.ts` (`getRadarHexColorAndOpacity`, visual contrast on dark basemaps)
  - `apps/web/src/components/CameraModal.tsx` (`severityLabel` disambiguation from `BÌNH THƯỜNG` to `THÔNG THOÁNG`)
  - `apps/web/src/utils/cameraModalUtils.test.ts` (Traffic severity label consistency assertions)
- **Core Invariants**:
  - **Convective Core Inviolability**: Doppler reflectivity $\ge 30\text{ dBZ}$ (`MODERATE_RAIN`) and $\ge 40\text{ dBZ}$ (`HEAVY_RAIN`) represents physically dense liquid precipitation cores. It must NEVER be suppressed by NWP model forecast latency.
  - **NWP Model Latency vs. Real-time Sweeps**: Numerical Weather Prediction models (e.g. Open-Meteo) update on multi-hour schedules and fail to resolve fast-developing tropical convective showers ($15–30\text{ min}$). Doppler radar is an empirical sweep ($10\text{ min}$) and must take precedence during overcast/cloudy conditions.
  - **Virga Restriction to Verified Clear Skies**: Downgrading airborne radar echoes to `ADVISORY_CLOUD` (virga) is valid strictly under verified clear/sunny conditions (WMO code $\le 1$: Clear sky / Mainly clear) with $0\text{ mm}$ measured rain and confirmed dry ground pavement.
  - **Minimum Contrast for Translucent Lens Overlays**: Layer overlays on dark basemaps (`#18181b`) must never drop below $8\%$ fill opacity with visible bounding strokes, preventing invisible rendering.
  - **Domain Disambiguation for Status Badges**: Camera status badges must use distinct traffic domain vocabulary (`THÔNG THOÁNG` matching the Map Legend) instead of generic terms (`BÌNH THƯỜNG`) that commuters mistake for weather or road dryness.

---

## 1. Context & Problem Statement

During real rain events in Ho Chi Minh City, commuters observed that:
1. Switching to the `Dự báo radar [Mưa]` lens displayed a pitch-black map with zero rain warning hexagons, despite rain visibly falling across districts.
2. The citywide banner on the sidebar reported: `TOÀN THÀNH PHỐ: Các tuyến đường khô ráo — Không ghi nhận ngập`.
3. Opening the camera modal for *Quốc lộ 1 - Lê Trọng Tấn* revealed reflective, soaking wet asphalt with vehicles wearing rain ponchos, yet the camera badge stated `HOẠT ĐỘNG / BÌNH THƯỜNG`.

---

## 2. Root Cause Analysis

### A. The False Virga Suppression Gate in `useRainHexRadar`
In Sprint 8 (Lesson 41 / Issue #269), a "Surface Cross-Validation Gate" was introduced to eliminate false-positive thunderstorm overlays on bone-dry, sunny days:
```typescript
const isSurfaceDry =
  (!weather || (weather.rain_mm < 0.2 && weather.weather_code <= 3)) &&
  activeGroundFloodCount === 0;

const finalSeverity = isSurfaceDry ? 'ADVISORY_CLOUD' : rawIntensity;
```

This gate failed under real tropical rain conditions due to three compounding assumptions:
1. **Open-Meteo NWP Latency**: Open-Meteo is a global numerical forecast model. When convective showers broke out over HCMC at 09:00, Open-Meteo still reported `weather_code: 3` (Overcast), `rain_mm: 0`, and precipitation probability $3\%$ (forecasting rain only in the afternoon). Thus `weather.rain_mm < 0.2 && weather.weather_code <= 3` evaluated to `true`.
2. **Asymmetric Detection Tasks**: The background CCTV ONNX model classifies traffic congestion (PCU saturation). It does not classify shallow water films, so `activeGroundFloodCount` was `0`.
3. **The Invisibility Cascade**: `isSurfaceDry` evaluated to `true` across all 104 detected Doppler rain cells. Every cell was downgraded from `HEAVY_RAIN` / `MODERATE_RAIN` / `LIGHT_RAIN` to `ADVISORY_CLOUD`.
4. **Sub-threshold Opacity**: In `floodUtils.ts`, `ADVISORY_CLOUD` was styled with `fillOpacity: 0.04` (4% opacity) and `fillColor: '#94a3b8'` (slate gray). On a dark Leaflet basemap (`#18181b`), 4% opacity is completely invisible to human eyes, rendering the entire radar lens pitch black.

### B. Modal Badge Semantic Conflation
In `CameraModal.tsx`:
`severity === 'CLEAR'` was mapped to `severityLabel = 'BÌNH THƯỜNG'`.
While intended to represent unimpeded vehicular traffic, the generic term `BÌNH THƯỜNG` misled commuters into thinking the system claimed the weather or road surface was dry. On the Map Legend, the exact same status was properly labeled `Thông thoáng`.

---

## 3. Solution Architecture & Implementation

### A. Dedicated Radar Severity Resolver (`resolveRadarHexSeverity`)
A pure, deterministic function was extracted and added to `rainHexUtils.ts`:
1. `HEAVY_RAIN` ($\ge 40\text{ dBZ}$) and `MODERATE_RAIN` ($30–39\text{ dBZ}$) are convective cores and are **never** suppressed by NWP model latency or camera counts.
2. `LIGHT_RAIN` ($18–29\text{ dBZ}$) is retained whenever ground cameras detect wet/flooded pavement or Open-Meteo reports rain ($\ge 0.2\text{ mm}$ or WMO rain code).
3. Under overcast (code 3) or cloudy skies (code 2), `LIGHT_RAIN` is preserved because tropical showers develop ahead of NWP sync intervals.
4. `LIGHT_RAIN` is strictly downgraded to `ADVISORY_CLOUD` only when the sky is verified clear/sunny (WMO code $\le 1$: Clear sky / Mainly clear) with $0\text{ mm}$ rain and zero wet cameras.

### B. Contrast Calibration for Translucent Radar Overlays
In `floodUtils.ts`:
- `LIGHT_RAIN`: `fillOpacity: 0.10`, `strokeColor: '#7dd3fc'`, `strokeWeight: 0.75`.
- `ADVISORY_CLOUD`: `fillOpacity: 0.08`, `strokeColor: '#cbd5e1'`, `strokeWeight: 0.75`.
Both provide clear visibility on dark Leaflet canvas renders without obscuring underlying streets.

### C. Traffic Semantic Disambiguation
In `CameraModal.tsx`:
Changed `severityLabel` from `BÌNH THƯỜNG` to `THÔNG THOÁNG`, perfectly aligning with `VERDICT_I18N.SEVERITY.CLEAR` and the bottom Map Legend (`● Thông thoáng ● Đông xe ● Ùn ứ`). Added explicit tooltip `title="Lưu thông giao thông: THÔNG THOÁNG"`.

---

## 4. Empirical Verification Evidence (AGENTS.md Rule 10)

1. **Automated Unit Tests**:
   - `rainHexUtils.test.ts`: Added comprehensive test suite verifying convective core protection, NWP latency resilience, and strict virga gating (8 new assertions).
   - `cameraModalUtils.test.ts`: Added test asserting `VERDICT_I18N.SEVERITY.CLEAR.vi.toUpperCase() === 'THÔNG THOÁNG'`.
   - Full test suite passed: 417 tests, 82 suites, 0 failures.
   - Next.js production build (`npm run build`) succeeded without warnings or TypeScript errors.
2. **Live Browser Verification (Playwright MCP)**:
   - Evaluated `leaflet-radarHex-pane canvas`: verified active canvas rendering with **4,608,422 non-zero pixels** displaying sky blue (`#38bdf8`) and ocean blue (`#3b82f6`) rain polygons over HCMC.
   - Captured empirical screenshot: `radar_hexagons_restored_live.png`.
   - Opened Camera Modal: verified `[HOẠT ĐỘNG]` and `[THÔNG THOÁNG]` badges rendered with `#22c55e` styling and proper tooltip.
   - Captured empirical screenshot: `camera_modal_thong_thoang_badge.png`.
