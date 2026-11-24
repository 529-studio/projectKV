# Lesson 526: Historical Flood Depth Trend Charts & Tidal Inundation Correlation Dashboard

## Navigation Aid & Quick Reference

- **Domain**: Time-series flood depth aggregation, harmonic astronomical river tide correlation, leaf modulith dependency inversion, commuter spark-ribbon ergonomics.
- **Relevant Issues**: Issue #526 (S18-T3), Issue #476 (S15-T2), Issue #510 (S17-T3), Issue #511 (S17-T2), ADR-004, ADR-012, Lesson L90, Lesson L511.
- **Key Files**:
  - `services/go-server/internal/store/migrations/0015_flood_verdict_history.sql` (Append-only time-series migration)
  - `services/go-server/internal/store/flood_history_repo.go` (Append-only storage engine with chronological querying)
  - `services/go-server/internal/corridor/corridor.go` (Historical downsampling aggregator, station mapper, headline summary builder)
  - `services/go-server/internal/corridor/handler.go` (`GET /api/roads/:road_key/history?hours=24` endpoint)
  - `services/go-server/cmd/server/adapters.go` (`corridorFloodHistoryAdapter`, `corridorTidePredictorAdapter`, pipeline persistence)
  - `apps/web/src/types/corridorHistory.ts` (Corridor history DTO contracts)
  - `apps/web/src/hooks/useCorridorHistory.ts` (1-minute TTL client cache hook)
  - `apps/web/src/components/CorridorTrends.tsx` (Glanceable 1-line collapsed summary & ~72px SVG spark-ribbon)
  - `apps/web/src/components/CorridorOverviewCard.tsx` (Integrated corridor overview card)
- **Core Invariants**:
  - **Append-Only Time Series Invariant**: Unlike ephemeral hotset state (`camera_verdicts` upsert), flood depth observations must be retained chronologically in `flood_verdict_history` with unique index `(camera_id, observed_at)` and `ON CONFLICT DO NOTHING` idempotence.
  - **Leaf Modulith Boundary**: Module `internal/corridor` must never import sibling modules `internal/store` or `internal/tide`. Persistent storage and tidal constituent calculation are abstracted via consumer interfaces `FloodHistoryProvider` and `TidePredictor`, wired exclusively in `cmd/server/adapters.go`.
  - **Glanceable Commuter Ergonomics**: Commuters checking road conditions at a red light or before leaving work require a glanceable 1-line headline (`Đỉnh triều 1.62m lúc 17:30 · Ngập đỉnh 23cm ▾`), expandable to a compact ~72px SVG ribbon with color-coded hazard bars and a dashed tide curve, without bloat from heavy external charting libraries.

---

## 1. Context & Problem Statement

In coastal and estuarine cities like Ho Chi Minh City, arterial corridors experience recurrent tidal inundation compounded by urban runoff. Commuters on major corridors (such as Đại lộ Võ Văn Kiệt, Huỳnh Tấn Phát, and Trần Xuân Soạn) frequently face flooded bottlenecks where water levels rise rapidly during high tide peaks.

While real-time camera verdicts provide snapshot status ("passable" or "caution"), commuters need temporal context to answer key questions:
1. *Is the flooding currently subsiding or actively rising?*
2. *Is the standing water caused by an astronomical river tide surge or lingering localized runoff?*
3. *When did the peak occur, and what was the maximum depth recorded along the corridor?*

Prior to Issue #526, the backend only stored the single latest verdict per camera in SQLite table `camera_verdicts`. Past flood depth observations were discarded once overwritten by newer cycles, preventing any historical trend visualization or correlation with astronomical tidal cycles.

---

## 2. Root Cause & Architectural Design

### A. Append-Only Time-Series Migration (`0015_flood_verdict_history.sql`)
To preserve historical flood depth readings without bloating the primary camera verdict table:
```sql
CREATE TABLE IF NOT EXISTS flood_verdict_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    camera_id TEXT NOT NULL,
    flood_depth_cm REAL,
    flood_hazard_level INTEGER,
    flood_severity TEXT NOT NULL,
    flood_depth_confidence TEXT,
    flood_calibration_ref TEXT,
    observed_at TEXT NOT NULL,
    recorded_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_flood_verdict_history_cam_time 
ON flood_verdict_history(camera_id, observed_at);

CREATE INDEX IF NOT EXISTS idx_flood_verdict_history_lookup 
ON flood_verdict_history(camera_id, observed_at ASC);
```
Both the hotset scheduler (`buildHotsetOnVerdict`) and the decoupled multimodal flood pipeline (`initFloodPipeline`) fan out incoming observations to `floodHistoryRepo.Record(...)`.

### B. Hourly Downsampling & Tidal Fusion Aggregator
Querying raw camera observations over a 24- to 72-hour window across 20+ cameras per corridor would yield thousands of points, exceeding mobile bandwidth constraints.
`corridor.Service.GetHistory(roadKey, hours)` implements:
1. **Time-Bucket Downsampling**: Buckets observations into 1-hour windows `[t, t + 1h)`:
   - `avg_depth_cm`: Arithmetic mean of observed depths across corridor cameras.
   - `peak_depth_cm`: Absolute maximum depth across all cameras within that hour.
   - `observation_count`: Total valid readings in the bucket.
2. **Harmonic Astronomical Tidal Correlation**: Computes river tide height $H_{\text{tide}}(t)$ at the start of each hour using the harmonic constituent model (`tide.PredictTide`):
   - Corridors in southern districts (`huynh_tan_phat`) map to Nhà Bè station on the Soài Rạp River.
   - Central/western corridors (`vo_van_kiet`) map to Phú An station on the Sài Gòn River.
3. **Glanceable Summary Headline**: Derives peak tide height, peak tide timestamp, maximum corridor flood depth, and whether impassable water levels ($\ge 20\text{ cm}$) were encountered.

### C. Modulith Dependency Inversion
Following ADR-004 and `GO_BACKEND_ARCHITECTURE.md`, `internal/corridor` declares:
```go
type FloodHistoryProvider interface {
    GetRecentByCameraIDs(ctx context.Context, cameraIDs []string, start, end time.Time) ([]FloodHistoryObservation, error)
}

type TidePredictor interface {
    PredictTide(station string, t time.Time) float64
}
```
`cmd/server/adapters.go` implements:
- `corridorFloodHistoryAdapter`: queries `store.FloodHistoryRepo` within `[start, end]`.
- `corridorTidePredictorAdapter`: calls `tide.PredictTide(station, t)`.

### D. Minimalist Commuter Spark-Ribbon UI
Rather than integrating heavy visualization bundles (e.g. Chart.js, D3, Recharts) that inflate client bundle size:
- Uses pure SVG (`apps/web/src/components/CorridorTrends.tsx`) with zero extra runtime dependencies.
- **Collapsed State**: Single-line glanceable summary strip with status indicator dot and accessible toggle button ($\ge 48\text{px}$ touch target).
- **Expanded State**: Compact ~72px strip featuring 24 hourly color-coded bars:
  - Zinc (`#3f3f46`): Dry ($0\text{ cm}$).
  - Amber (`#fbbf24` / `#f59e0b`): Shallow / Caution ($1\text{--}20\text{ cm}$).
  - Rose (`#f43f5e`): Impassable ($> 20\text{ cm}$).
- Dashed cyan line for astronomical tide elevation with an explicit $1.50\text{ m}$ Alert Level 3 horizontal threshold marker.
- Interactive mouse and touch scrubbing showing instant depth and tide tooltips.

---

## 3. Empirical Verification & Invariant Enforcement

1. **Unit & Concurrency Tests**:
   - `services/go-server/internal/store/flood_history_repo_test.go`: 3/3 tests verifying append, idempotency on duplicate `(camera_id, observed_at)`, and range querying under `-race`.
   - `services/go-server/internal/corridor/corridor_test.go`: 16/16 tests verifying 1-hour downsampling math, hour clamping ($[1, 72]$), station mapping, and zero observation fallback.
   - `apps/web/src/components/CorridorTrends.test.ts`: 5/5 tests verifying bilingual summary formatting, hazard color coding, SVG alert line calculation, and 60-second client cache invalidation.
2. **Modulith & Dependency Invariant**:
   - Verified zero sibling cross-imports from `internal/corridor` to `internal/store` or `internal/tide`.
   - `go vet ./...` and `gofmt -l .` clean with zero errors.
3. **Frontend Production Build**:
   - `npx tsc --noEmit` and `npm test` (508 passing suites).
   - `npm run build` completes with static page generation and zero bundle errors.

---

## 4. Key Takeaways

- **Preserve Immutable Observations Separately from Hotset State**: Fast status endpoints require single-row overwrites, but trend analysis requires append-only tables. Splitting `camera_verdicts` and `flood_verdict_history` avoids lock contention while unlocking longitudinal insights.
- **Tailor Visual Information Density to Real-World User Needs**: Commuters navigating traffic require instant glanceability. A 1-line headline with an on-demand compact SVG ribbon delivers actionable situational awareness far more effectively than an over-engineered weather dashboard.
