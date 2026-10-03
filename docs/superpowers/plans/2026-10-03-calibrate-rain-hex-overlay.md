# Calibrate Rain Hex Radar Overlay & Non-Tech UI/UX Simplification Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Calibrate H3 rain hexagon mapping thresholds, fix false fallback on dry days, and redesign UI terminology, visual hierarchy, and legends so that non-tech commuters instantly understand rain conditions in 1 glance.

**Architecture:** 
1. **Radar Noise Suppression (`rainHexUtils.ts`)**: Elevate reflectivity threshold (`a >= 75` for light rain, `a >= 140` for moderate, `a >= 200` for heavy) to eliminate atmospheric haze and non-precipitating clouds.
2. **Safe Fallback Guard (`useRainHexRadar.ts`)**: A zero-echo result from successfully loaded radar tiles indicates clear weather; never blanket all 677 H3 cells with fallback. Fallback only runs when radar images fail to load AND weather indicates confirmed rain (`weather_code >= 61` or `rain_mm >= 1.0`).
3. **Sleek Minimalist Opacity (`floodUtils.ts`, `HexOverlay.tsx`)**: Translucent fills (`WET`: 0.07, `SHALLOW`: 0.12, `DEEP`: 0.18) with hairline borders (`0.5px` - `1.0px`) and stroke opacity 0.45 to ensure roads, routes, and camera dots remain razor-sharp.
4. **Non-Tech Human-Centric UI/UX (`floodUtils.ts`, `HexOverlay.tsx`, `MapLegend.tsx`)**:
   - Eliminate developer/GIS jargon: Remove `(H3)` from all labels and tooltips.
   - Replace terrifying false labels ("Ngập sâu (nguy hiểm)" for rain echoes) with honest, intuitive rain terminology: "Mưa to (nguy cơ ngập)", "Mưa vừa", "Mưa nhẹ".
   - Replace confusing polygon count badge (`545`) with clear status badge (`Có mưa` or hide counter if dry).
   - Unify natural color gradient: Light Sky Blue (`#38bdf8`) $\rightarrow$ Medium Blue (`#0284c7`) $\rightarrow$ Deep Royal Blue (`#1d4ed8`).

**Tech Stack:** Next.js (App Router), Leaflet, H3 Geo Polygons, Canvas 2D API, Tailwind CSS, TypeScript, Node.js Test Runner.

**Spec:** PR #129 / Issue #106 & #120 radar-to-hex reconciliation / UI/UX Non-Tech Commuter Usability.

## Global Constraints

- **Minimalist • Monochromatic • Pragmatic • UX-First** per AGENTS.md: Never obscure map corridors, routes, or traffic camera markers.
- **Honesty Rule (ADR-004 §D4)**: Never claim flood depth from airborne radar echoes; distinguish radar forecast vs camera-confirmed ground observations.
- 100% English for code, comments, commit messages, and test descriptions.
- Vietnamese UI diacritics strictly preserved in all user-facing labels.
- Zero Direct Push: All commits on branch `feat/h3-rain-hex-weather-mapping`.

## Review Focus

1. **Clear Day Echoes**: When radar tiles load but have zero rain echoes, `rainRadarHexCells` must remain empty (0 cells), never falling back to mark 677 cells as wet.
2. **Atmospheric Moisture Suppression**: Radar alpha values below 75 (representing thin cirrus/stratus clouds and ground moisture echoes) must evaluate to `null` (no cell rendered).
3. **Non-Tech Copy Intuitiveness**: A commuter with no tech or weather background must instantly know what each color means without consulting technical manuals.
4. **Visual Translucency**: Overlapping hexagonal cells must not create heavy opaque boundaries; strokes must remain delicate (0.5 - 1.0px) and fills translucent (0.07 - 0.18).
5. **No Counter Conflict**: Users must not see contradictory numbers (e.g. 545 raw H3 cells vs 35 camera alerts).

---

### Task 1: Update Rain Radar Thresholds and Fallback Rules in `rainHexUtils.ts`

**Files:**
- Modify: `apps/web/src/utils/rainHexUtils.ts:60-80`
- Modify: `apps/web/src/utils/rainHexUtils.ts:115-152`
- Test: `apps/web/src/utils/rainHexUtils.test.ts`

**Interfaces:**
- Consumes: `mapRadarPixelToSeverity(r, g, b, a): 'FLOODED_DEEP' | 'FLOODED_SHALLOW' | 'WET' | null`
- Consumes: `generateOpenMeteoFallbackHexCells(weather, polygons, monitoredCellIds): Map<string, T>`
- Produces: Calibrated thresholds filtering out cloud echoes (`a < 75`), and conditional Open-Meteo fallback requiring active rain (`weather_code >= 61` or `rain_mm >= 1.0`).

- [ ] **Step 1: Write failing tests in `apps/web/src/utils/rainHexUtils.test.ts`**

Update tests for `mapRadarPixelToSeverity` and `generateOpenMeteoFallbackHexCells`:
- Alpha values 20, 45, 70 return `null` (noise/cloud clutter filtered out).
- Alpha values 75, 100, 139 return `'WET'` (light rain).
- Alpha values 140, 180, 199 return `'FLOODED_SHALLOW'` (moderate rain).
- Alpha values 200, 240, 255 return `'FLOODED_DEEP'` (heavy rain / storm).
- Fallback returns empty if `rain_mm < 1.0` and weather code is clear/cloudy (`weather_code < 61`).

- [ ] **Step 2: Run test to verify failure**

Run: `npm --prefix apps/web test -- rainHexUtils.test.ts`
Expected: FAIL on threshold assertions.

- [ ] **Step 3: Implement calibrated thresholds in `apps/web/src/utils/rainHexUtils.ts`**

Adjust `mapRadarPixelToSeverity`:
```ts
export function mapRadarPixelToSeverity(
  _r: number,
  _g: number,
  _b: number,
  a: number
): 'FLOODED_DEEP' | 'FLOODED_SHALLOW' | 'WET' | null {
  if (a < 75) return null;
  if (a >= 200) return 'FLOODED_DEEP';
  if (a >= 140) return 'FLOODED_SHALLOW';
  return 'WET';
}
```
And adjust `generateOpenMeteoFallbackHexCells` to require meaningful precipitation:
```ts
const isRaining = weather.rain_mm >= 1.0 || (weather.weather_code >= 61 && isWmoRain(weather.weather_code));
```

- [ ] **Step 4: Run test to verify it passes**

Run: `npm --prefix apps/web test -- rainHexUtils.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/web/src/utils/rainHexUtils.ts apps/web/src/utils/rainHexUtils.test.ts
git commit -m "fix(weather): calibrate radar pixel reflectivity thresholds and fallback rules"
```

---

### Task 2: Refine Visual Opacities, Color Hierarchy, and Non-Tech Labels in `floodUtils.ts`

**Files:**
- Modify: `apps/web/src/utils/floodUtils.ts:1-75`
- Modify: `apps/web/src/utils/floodUtils.ts:140-264`
- Test: `apps/web/src/utils/floodUtils.test.ts`

**Interfaces:**
- Consumes: `FLOOD_CONFIG` constants (`FILL_OPACITY_*`, `STROKE_WEIGHT_*`, `COLOR_*`)
- Consumes: `FLOOD_I18N` bilingual copy dictionary
- Produces: `getFloodHexColorAndOpacity(severity)` returning intuitive blue palette and sleek opacities; `formatFloodHexTooltip` rendering clean, actionable, non-tech copy.

- [ ] **Step 1: Write failing tests in `apps/web/src/utils/floodUtils.test.ts`**

Update test assertions in `floodUtils.test.ts`:
- `FLOODED_DEEP`: `fillOpacity: 0.18`, `strokeWeight: 1.0`, label: `"Mưa to (nguy cơ ngập)"` / `"Heavy rain (flood risk)"`
- `FLOODED_SHALLOW`: `fillOpacity: 0.12`, `strokeWeight: 0.75`, label: `"Mưa vừa"` / `"Moderate rain"`
- `WET`: `fillColor: '#38bdf8'`, `fillOpacity: 0.07`, `strokeWeight: 0.5`, label: `"Mưa nhẹ"` / `"Light rain"`
- `LAYER_FLOOD`: `"Vùng mưa"` / `"Rain zones"` (no technical "Khả năng có mưa (H3)").

- [ ] **Step 2: Run test to verify failure**

Run: `npm --prefix apps/web test -- floodUtils.test.ts`
Expected: FAIL on `fillOpacity` / labels / colors mismatches.

- [ ] **Step 3: Update `FLOOD_CONFIG` and `FLOOD_I18N` in `apps/web/src/utils/floodUtils.ts`**

Update:
```ts
export const FLOOD_CONFIG = Object.freeze({
  MIN_ZOOM: 11,
  MAX_ZOOM: 13,
  COLOR_DEEP: '#1d4ed8',      // Deep royal blue for heavy rain
  COLOR_SHALLOW: '#0284c7',   // Medium vibrant sky blue for moderate rain
  COLOR_WET: '#38bdf8',       // Light translucent sky blue for light rain
  COLOR_DRY: '#22c55e',
  COLOR_UNKNOWN: '#71717a',
  STROKE_DEEP: '#1e40af',
  STROKE_SHALLOW: '#0369a1',
  STROKE_WET: '#38bdf8',
  STROKE_DRY: '#16a34a',
  STROKE_UNKNOWN: '#52525b',
  FILL_OPACITY_DEEP: 0.18,
  FILL_OPACITY_SHALLOW: 0.12,
  FILL_OPACITY_WET: 0.07,
  FILL_OPACITY_DRY: 0.06,
  FILL_OPACITY_UNKNOWN: 0.08,
  STROKE_WEIGHT_DEEP: 1.0,
  STROKE_WEIGHT_SHALLOW: 0.75,
  STROKE_WEIGHT_WET: 0.5,
  STROKE_WEIGHT_DRY: 0.5,
});

export const FLOOD_I18N = Object.freeze({
  LAYER_TRAFFIC: { vi: 'Giao thông', en: 'Traffic' },
  LAYER_FLOOD: { vi: 'Vùng mưa', en: 'Rain zones' },
  TOOLTIP_TITLE: { vi: 'Thời tiết khu vực', en: 'Area Weather' },
  DEEP_WATER: { vi: 'Mưa to (nguy cơ ngập)', en: 'Heavy rain (flood risk)' },
  SHALLOW_WATER: { vi: 'Mưa vừa', en: 'Moderate rain' },
  WET_ROAD: { vi: 'Mưa nhẹ', en: 'Light rain' },
  DRY_ROAD: { vi: 'Khô ráo', en: 'Dry' },
  UNKNOWN_STATUS: { vi: 'Chưa xác minh', en: 'Unverified' },
  ...
});
```
Refactor `formatFloodHexTooltip` to provide clear, actionable travel guidance:
- Show clear headline with color indicator: `Mưa vừa` / `Mưa to`.
- Show practical road advice: `Đường trơn ướt, chú ý giảm tốc độ` or `Nguy cơ ngập cục bộ tại vùng trũng`.
- Show clean source badge: `Radar thời tiết (RainViewer)` or `Camera xác nhận ngập`.

- [ ] **Step 4: Run test to verify it passes**

Run: `npm --prefix apps/web test -- floodUtils.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add apps/web/src/utils/floodUtils.ts apps/web/src/utils/floodUtils.test.ts
git commit -m "style(flood): simplify rain terminology and refine translucent color hierarchy"
```

---

### Task 3: Fix Radar Sampling & Eliminate False-Fallback in `useRainHexRadar.ts`

**Files:**
- Modify: `apps/web/src/hooks/useRainHexRadar.ts:153-215`
- Test: `npm --prefix apps/web test`

**Interfaces:**
- Consumes: `mapRadarPixelToSeverity`, `buildRadarTileUrl`
- Produces: Clean radar sampling where clear days produce 0 polygons without triggering spurious fallback.

- [ ] **Step 1: Remove erroneous fallback trigger in `useRainHexRadar.ts`**

Remove the flawed condition:
```ts
// DELETE:
// if (mappedCells.size === 0 && weather && weather.rain_mm > 0) { ... }
```
When radar images load successfully and detect zero echoes, Ho Chi Minh City is genuinely dry. Keep `mappedCells` empty (0 cells).

- [ ] **Step 2: Restrict tile-failure fallback to active rainfall**

Only apply Open-Meteo fallback if:
- Both tile images failed (`!img101 && !img102`) OR radar metadata is null.
- AND `weather` reports active confirmed rain (`weather.rain_mm >= 1.0` or `weather.weather_code >= 61`).

- [ ] **Step 3: Run unit tests**

Run: `npm --prefix apps/web test`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add apps/web/src/hooks/useRainHexRadar.ts
git commit -m "fix(radar): eliminate spurious fallback on clear days"
```

---

### Task 4: Simplify UI Controls, Remove GIS Acronyms, and Harmonize Counters in `HexOverlay.tsx` & `MapLegend.tsx`

**Files:**
- Modify: `apps/web/src/components/HexOverlay.tsx:350-495`
- Modify: `apps/web/src/components/MapLegend.tsx:110-185`
- Test: `npm --prefix apps/web test`

**Interfaces:**
- Consumes: `FLOOD_I18N`, `HEX_I18N`
- Produces:
  - Intuitive 2-tab switch: `[ 🌧️ Vùng mưa ]` and `[ 🚗 Giao thông ]` with clear active states.
  - Eliminates the misleading raw polygon count (`545`) when dry. When rain is active, displays a clean `Có mưa` badge or count.
  - Map Legend removes technical acronym `(H3)` $\rightarrow$ `"Vùng mưa (Radar thời tiết)"`.
  - Non-tech labels in Legend: `"Mưa to (nguy cơ ngập)"`, `"Mưa vừa"`, `"Mưa nhẹ"`.

- [ ] **Step 1: Simplify segmented control and counter badges in `HexOverlay.tsx`**

- Button 1 label: `Lớp bản đồ` (or `Vùng`).
- Flood mode button: Displays `Vùng mưa`.
  - When `floodedHexCount === 0`: No noisy counter bubble; shows clean neutral text or hides badge.
  - When `floodedHexCount > 0`: Shows compact indicator `<span className="w-1.5 h-1.5 rounded-full bg-sky-400" />` with `Có mưa` (or count).
- Polygon stroke opacity set to `0.45` to prevent black mesh seams.

- [ ] **Step 2: Clean up `MapLegend.tsx`**

- Change header from `Khả năng có mưa (H3)` to `Vùng mưa (Radar thời tiết)` / `Rain zones (Weather radar)`.
- Use updated labels: `Mưa to (nguy cơ ngập)`, `Mưa vừa`, `Mưa nhẹ`.
- Ensure legend sample boxes match new color and opacity styling (`#38bdf8`, `#0284c7`, `#1d4ed8`).

- [ ] **Step 3: Verify build and test suite**

Run:
```bash
npm --prefix apps/web test
npm --prefix apps/web run build
```
Expected: All tests pass, static build succeeds with 0 lint/TS errors.

- [ ] **Step 4: Commit**

```bash
git add apps/web/src/components/HexOverlay.tsx apps/web/src/components/MapLegend.tsx
git commit -m "feat(ui): eliminate H3 tech jargon, simplify rain controls, and clarify legend for non-tech commuters"
```
