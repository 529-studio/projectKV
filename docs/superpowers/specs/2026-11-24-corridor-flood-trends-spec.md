# Engineering Specification: Corridor Flood Depth Trends & Tidal Correlation (Issue #526)

**Issue**: [#526](https://github.com/529-studio/projectKV/issues/526) — `feat(analytics): historical flood depth trend charts & tidal inundation correlation dashboard`  
**Target Delivery**: Sprint 18, Day 3 (2026-11-25)  
**Author**: Antigravity  
**Status**: In Review  

---

## 1. Problem Statement & Commuter Rationale

Commuters navigating arterial corridors in Ho Chi Minh City (e.g. Võ Văn Kiệt, Nguyễn Hữu Cảnh, Trần Xuân Soạn, Mễ Cốc) frequently deal with diurnal astronomical high tides (*triều cường*) and sudden monsoon cloudbursts.

Currently, Warded displays real-time instantaneous flood depth measurements and passability verdicts. However, commuters planning their trips ask two core questions:
1. **"What time is peak flood and peak tide today along this corridor?"**
2. **"What time will the water recede so that it becomes safe to pass?"**

Providing an overwhelming, multi-axis weather dashboard (like Windy or AccuWeather) imposes heavy cognitive load on commuters checking their phones under bright sunlight. This feature implements a **glanceable 24-hour Spark-Ribbon** that answers these two questions within 2–3 seconds.

---

## 2. System Architecture & Backend Contract

```
[Camera Video Frames] 
         │
         ▼
  [cvclient / CV Engine]
         │
         ▼
 [flood.Service (onFloodVerdict)]
         │
         ├───► [camera_verdicts] (Latest instantaneous verdict)
         │
         └───► [flood_verdict_history] (Append-only SQLite time series)
                         │
                         ▼
             [corridor.Service.GetHistory]
                         │
        ┌────────────────┴────────────────┐
        ▼                                 ▼
[Downsample 1-hour buckets]       [tide.PredictTide (Phú An / Nhà Bè)]
        │                                 │
        └────────────────┬────────────────┘
                         │
                         ▼
             [GET /api/roads/:road_key/history?hours=24]
                         │
                         ▼
        [apps/web: CorridorOverviewCard -> CorridorTrends]
```

### 2.1 Storage Migration: `0015_flood_verdict_history.sql`

```sql
-- Migration: 0015_flood_verdict_history.sql
-- Time-series flood observations for corridor historical trends (Issue #526).

CREATE TABLE IF NOT EXISTS flood_verdict_history (
    id                     INTEGER PRIMARY KEY AUTOINCREMENT,
    camera_id              TEXT NOT NULL,
    flood_depth_cm         REAL,
    flood_hazard_level     INTEGER,
    flood_severity         TEXT NOT NULL DEFAULT '',
    flood_depth_confidence TEXT NOT NULL DEFAULT '',
    flood_calibration_ref  TEXT NOT NULL DEFAULT '',
    observed_at            TIMESTAMP NOT NULL,
    recorded_at            TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_flood_verdict_history_camera_observed
    ON flood_verdict_history(camera_id, observed_at);

CREATE INDEX IF NOT EXISTS idx_flood_verdict_history_observed
    ON flood_verdict_history(observed_at);

CREATE INDEX IF NOT EXISTS idx_flood_verdict_history_camera_observed
    ON flood_verdict_history(camera_id, observed_at);
```

### 2.2 Store Repository: `FloodHistoryRepo`

Leaf package `services/go-server/internal/store`:
- `Record(ctx context.Context, h FloodVerdictRecord) error`
- `GetRecentByCameraIDs(ctx context.Context, cameraIDs []string, startTime time.Time, endTime time.Time) ([]FloodVerdictRecord, error)`

### 2.3 Historical Aggregator: `corridor.Service.GetHistory`

1. **Camera Mapping**:
   Locates all cameras associated with `road_key` via `RoadKey` or `RoadKeys`. If none found, returns 404.
2. **Station Mapping**:
   Maps `road_key` to associated hydrological tide station (`StationPhuAn` or `StationNhaBe` via `LowLyingRoads`). Defaults to `StationPhuAn`.
3. **Downsampling Math**:
   Divides the requested window (e.g. past 24 hours up to `now`) into equal 1-hour intervals:
   - `avg_depth_cm`: Arithmetic mean of all measured flood readings in the hour bucket across corridor cameras. If dry or unmeasured, returns `0.0`.
   - `max_depth_cm`: Maximum flood reading observed across any camera along the corridor during that hour.
   - `tide_level_m`: Astronomical tide elevation computed via `tide.PredictTide(station, bucketTime)`.
   - `tide_alert_level`: Evaluated against official HCMC thresholds: Normal (< 1.40m), Level 1 (1.40m - 1.49m), Level 2 (1.50m - 1.59m), Level 3 ($\ge 1.60\text{m}$).
   - `hazard_level`: `CLEAR` (depth < 10cm), `CAUTION` (10cm - 20cm), `IMPASSABLE` (> 20cm).
4. **Summary & Advisory Computation**:
   Computes peak tide time & level and peak flood depth time & level over the 24-hour span to drive the single-line glanceable summary.

### 2.4 REST API Schema (`GET /api/roads/:road_key/history?hours=24`)

```json
{
  "road_key": "vo_van_kiet",
  "hours": 24,
  "station": "phu_an",
  "station_name": {
    "vi": "Trạm Phú An (Sông Sài Gòn)",
    "en": "Phu An Station (Saigon River)"
  },
  "summary": {
    "peak_tide_time": "2026-11-24T17:30:00Z",
    "peak_tide_level_m": 1.62,
    "peak_flood_time": "2026-11-24T17:00:00Z",
    "max_depth_cm": 22.0,
    "headline": {
      "vi": "Đỉnh triều 1.62m lúc 17:30 • Dự kiến ngập 22cm (rút sau 19:00)",
      "en": "Peak tide 1.62m at 17:30 • Expected flood 22cm (receding after 19:00)"
    }
  },
  "timeline": [
    {
      "time": "2026-11-24T00:00:00Z",
      "hour_label": "00:00",
      "avg_depth_cm": 0.0,
      "max_depth_cm": 0.0,
      "tide_level_m": 1.15,
      "tide_alert_level": "NORMAL",
      "hazard_level": "CLEAR"
    }
  ]
}
```

---

## 3. Frontend UI/UX Design: Commuter Spark-Ribbon

### 3.1 Ergonomic Guidelines
- **Height**: Compact ~72px strip.
- **Zero Heavy Dependencies**: Pure inline SVG and CSS classes, zero chart.js, d3, or recharts.
- **Kiosk Ergonomics**: Minimum 48px touch targets for scrub interaction.
- **Monochromatic & Accessible**: Neutral zinc base with high-contrast amber/rose hazard indicators.

### 3.2 Visual Components
1. **Summary Banner (Collapsed)**:
   A clean button inside `CorridorOverviewCard`:
   > `[ Triều & ngập 24h: Đỉnh 1.62m lúc 17:30 (Ngập 22cm) ▾ ]`
2. **Spark-Ribbon (Expanded)**:
   - 24 hourly columns representing the past 24 hours up to the current hour.
   - Bar height scales with `max_depth_cm` (0–50cm scale):
     - `max_depth_cm == 0`: 2px baseline bar (`bg-zinc-800`).
     - `1cm - 20cm`: Amber caution fill (`bg-amber-500/80`).
     - `> 20cm`: Rose impassable fill (`bg-rose-500/90`).
   - Smoothed dashed curve across the top representing river tide height ($1.0\text{m} - 1.8\text{m}$).
   - Dashed horizontal line at $1.50\text{m}$ (Alert Level 2 / overflow baseline).
   - "Hiện tại / Now" indicator on the current hour column.
3. **Touch Scrubbing Tooltip**:
   Hovering or touching any hour column highlights the bar and displays a micro-tooltip:
   `17:00 • Triều 1.58m • Ngập 22cm`.

---

## 4. Verification & Testing Plan

1. **Backend Tests**:
   - `services/go-server/internal/store/flood_history_repo_test.go`:
     - Idempotent writes on duplicate `(camera_id, observed_at)`.
     - Time range queries with ASC ordering.
   - `services/go-server/internal/corridor/corridor_test.go`:
     - Downsampling 5-minute readings into 1-hour buckets.
     - Accurate `avg_depth_cm` and `max_depth_cm` calculations.
     - Correct station association for known vs unknown road keys.
     - 404 for non-existent corridor.
2. **Frontend Tests**:
   - `apps/web/src/components/CorridorTrends.test.ts`:
     - Renders collapsed summary banner.
     - Expands on toggle click.
     - Handles empty/zero-depth history gracefully (all clear state).
     - Renders bilingual labels in `vi` and `en` with zero emoji.
     - Verifies touch target dimensions ($\ge 48\text{px}$).
3. **CI & Compliance**:
   - `go test -v -race ./...` (100% green).
   - `cd apps/web && npm test` (500+ tests pass).
   - `make docs-lint` (0 new violations).
