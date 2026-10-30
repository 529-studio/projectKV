## Lesson 43: Upstream Tile Server Zoom Ceiling & Silent 200 OK Placeholder Degradation in Web Canvas Decoders

- **Incident & Tracking:** Issue #269 followup / Real-time Rain Verification on 2026-10-17.
- **Symptom:**
  During an active afternoon rainstorm in Ho Chi Minh City (Open-Meteo showing `Mưa rào nhẹ`, 88%–100% rain probability, and CCTV at *Điện Biên Phủ - Nguyễn Hữu Cảnh* showing visibly wet roadway pavement), the map's `[Dự báo radar]` lens rendered 0 blue rain hexagons citywide.
- **Root Cause:**
  1. **RainViewer Radar Tile Zoom Ceiling**: The RainViewer radar tile endpoint (`/v2/radar/{id}/256/{z}/{x}/{y}/2/0_0.png`) is meteorologically capped at **Zoom 7**. When a client requests Zoom 8 (e.g. `z=8, x=203, y=120`), RainViewer returns HTTP 200 containing a 1,370-byte static PNG image with the rendered text: `"Zoom Level Not Supported"`.
  2. **Silent Failure in HTML5 Canvas Pipeline**: Because the server responded with HTTP 200, `loadImage` succeeded without error. The offscreen canvas drew the error graphic and extracted gray text pixels ($R=G=B=110$). `mapRadarPixelToRainIntensity` rejected these gray pixels as low-saturation noise, returning `null` for all 129 H3 macro cells across the city. Concurrently, the fallback mechanism was bypassed because the image objects were not `null`.
  3. **Palette Coverage Gaps**: Scheme 2 (Universal Blue) utilizes deep crimson (`193, 0, 0`), vivid orange (`255, 170, 0`), deep navy (`0, 85, 136`), and tan clutter boundary masks (`206, 192, 135, a=150`). Missing these specific thresholds caused intermittent dropouts in heavy storm cores.
- **Mandatory Takeaways & Engineering Resolutions:**
  1. **Enforce Zoom 7 Tile Space**:
     - Ho Chi Minh City ($10.7^\circ–10.9^\circ\text{N}$, $106.5^\circ–106.9^\circ\text{E}$) maps exactly into two Zoom 7 tiles at latitude row $Y=60$: Tile `(101, 60)` (Western & Central HCMC) and Tile `(102, 60)` (Eastern HCMC / Thu Duc City).
     - Standardized `latLonToTilePixel(lat, lon, zoom = 7)` across client and unit tests.
  2. **Calibrated Universal Blue Scheme 2 Palette**:
     - Explicitly filter tan boundary masks (`r > 180, g > 180, b > 120, a < 200`).
     - Heavy rain: Violet/pink convective storm cores (`255, 170, 255`), crimson (`193, 0, 0`), vivid red (`255, 68, 0`), and bright orange (`255, 170, 0`).
     - Moderate rain: Pure yellow/amber (`255, 238, 0`, `255, 197, 0`) and deep navy blue (`0, 85, 136`).
     - Light rain: Medium sky blue (`0, 119, 170`, `0, 163, 224`) and light cyan/teal (`136, 221, 238`, `54, 186, 229`).
  3. **End-to-End Verification**:
     - Verified with live Doppler radar tiles: **85 active rain H3 cells** immediately illuminated across Ho Chi Minh City.
     - Confirmed CCTV location *Điện Biên Phủ - Nguyễn Hữu Cảnh* (tile 101, pixel 241, 35) maps to active `HEAVY_RAIN` (RGB `193, 0, 0, 255`), matching the wet roadway seen on the camera.
