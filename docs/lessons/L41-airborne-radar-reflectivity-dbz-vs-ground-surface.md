## Lesson 41: Airborne Radar Reflectivity (dBZ) vs. Ground Surface Inundation & The Alpha-Channel Blindness Fallacy

- **Incident & Tracking:** Issue #269 (`[rfc + web] Doppler Rain Radar Pipeline Overhaul`), Sprint 8 Priority 1 (S8-T1, PR targeting `dev`).
- **Symptom:**
  Live staging deployment and commuter feedback revealed the web map explorer rendered vast dark-blue flood/rain hexagons covering Ho Chi Minh City, with urgent tooltips claiming `Ngập sâu lòng đường, nguy cơ chết máy cao`, even when the city was completely sunny and monitored CCTV cameras (e.g., *Võ Văn Kiệt - An Bình*) showed bone-dry asphalt.
- **Root Cause (The 7 Compounding Failures):**
  1. **Alpha-Channel Blindness Fallacy**: In `rainHexUtils.ts`, `mapRadarPixelToSeverity` completely ignored RGB channels (`_r, _g, _b`) and checked only `a >= 200 -> FLOODED_DEEP`. Non-empty RainViewer Universal Blue pixels (Scheme 2) are fully opaque ($A=255$). Thus, 100% of echoes (whether 5 dBZ cirrus or 50 dBZ storm core) were marked as catastrophic deep roadway floods.
  2. **Coarse Tile Bilinear Smoothing Blur**: Backend requested `/2/1_1.png` at Zoom 7 ($1\text{ px} \approx 1.2\text{ km}$). Bilinear interpolation blurred faint high-altitude cloud edges across entire provinces, bleeding false precipitation into downtown HCMC.
  3. **Single-Centroid Spatial Aliasing**: Sampling only 1 pixel at the centroid of a $5.16\text{ km}^2$ H3 Resolution 7 hexagon allowed a single isolated noise pixel to illuminate an entire urban district.
  4. **Global Weather Model Fallback Flooding**: `generateOpenMeteoFallbackHexCells` mapped numerical weather thunderstorm codes (WMO 95) directly to `FLOODED_DEEP` for 100% of monitored city cells, even when measured ground rain was $0.0\text{ mm}$.
  5. **Semantic Conflation of Airborne Echoes vs. Ground Inundation**: Directly reusing camera-verified `FLOODED_*` roadway inundation enums for atmospheric radar echoes aloft ($1–3\text{ km}$ altitude) caused alarming and inaccurate road condition advisories.
  6. **Defaulting Initial Page Load to Radar Lens**: `apps/web/src/app/page.tsx` initialized with `activeLens = 'radar'`, forcing every visiting commuter to see atmospheric clutter instead of real-time arterial traffic flow.
  7. **Lack of Surface Ground-Truth Cross-Validation Gate**: Airborne virga (rain evaporating before reaching the ground) was rendered as active ground rain without cross-checking surface rain gauges or CCTV feeds.
- **Mandatory Takeaways & Engineering Resolutions:**
  1. **True RGB dBZ Decoding**:
     - RainViewer Universal Blue (Scheme 2) reflectivity decoded into calibrated dBZ tiers:
       - $a < 120$ or saturation $< 0.20$ (haze/clutter $< 18\text{ dBZ}$) $\rightarrow$ `null` (suppressed).
       - $18–29\text{ dBZ}$ (cyan, teal, sky blue) $\rightarrow$ `LIGHT_RAIN`.
       - $30–39\text{ dBZ}$ (saturated yellow/amber or deep navy) $\rightarrow$ `MODERATE_RAIN`.
       - $\ge 40\text{ dBZ}$ (orange, red, purple storm cores) $\rightarrow$ `HEAVY_RAIN`.
  2. **Non-Interpolated Raw Tiles**:
     - Upgraded tile template to `/2/0_0.png` (`smooth=0, snow=0`) at Zoom 7 ($1\text{ px} \approx 1.2\text{ km}$). Note: RainViewer API strictly caps radar tiles at Zoom 7; Zoom 8+ returns a placeholder "Zoom Level Not Supported" image. All H3 Resolution 7 cells across Ho Chi Minh City fit cleanly within exactly two Zoom 7 tiles: `(101, 60)` and `(102, 60)`, doubling spatial resolution with zero extra network overhead.
  3. **7-Point Contraction Sampling with Quorum**:
     - Implemented `getHexSamplingPoints` sampling the centroid + 6 outer vertices contracted to 70% radius.
     - Implemented `evaluateHexRadarIntensity` requiring a quorum of $\ge 3/7$ points to match a rain tier and calculating the median rank, completely eliminating tile boundary bleeding and single-pixel aliasing.
  4. **Surface Cross-Validation Gate**:
     - When Open-Meteo precipitation is $< 0.2\text{ mm}$ and sky code is clear/cloudy ($\le 3$), AND ground cameras in the cell observe dry pavement, the airborne echo is classified as `ADVISORY_CLOUD` (slate gray, fillOpacity 0.04) rather than asserting active ground rain or flooding.
  5. **Decoupled Semantic Architecture & Commuter-First Defaults**:
     - Default lens set to `'traffic'` (`apps/web/src/app/page.tsx` and `HexOverlay.tsx`), ensuring commuters immediately see real-time arterial saturation dots (Green/Yellow/Red) without map clutter on dry days.
     - Translucent sky/ocean/indigo styling for radar forecast layers (`#38bdf8`, `#3b82f6`, `#6366f1`) with dashed borders.
     - Zero graphic emojis (`⚠`, `✓`) across all modified tooltips per AGENTS.md Rule 1.
