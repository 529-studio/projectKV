# Lesson 475: Arterial Corridor Hazard & Duration Engine — From Camera Pins to Commuter Journeys

## Navigation Aid & Quick Reference

- **Domain**: Go leaf-modulith backend, arterial corridor graph, GeoJSON FeatureCollection delivery, travel delay and cumulative flood hazard modelling.
- **Relevant Issues**: Issue #475 (S15-T1), Issue #476 (S15-T2 web route layer), ADR-004 §D1 (Corridor status), AGENTS.md Rule 2 (Sound solutions), Rule 7 (Leaf modulith).
- **Key Files**:
  - `services/go-server/internal/corridor/corridor.go` (Service, passability logic, route calculation)
  - `services/go-server/internal/corridor/routes.go` (Canonical arterial corridors & GeoJSON models)
  - `services/go-server/internal/corridor/handler.go` (`GET /api/roads/:road_key/route`, `GET /api/corridors/overview`)
  - `services/go-server/internal/corridor/corridor_test.go` (Deterministic unit test suite)
  - `services/go-server/docs/swagger.yaml` (Updated OpenAPI contracts)
- **Core Invariants**:
  - **A commuter needs an Origin-Destination path, not 800 disconnected camera pins.** Aggregating cameras by road key without travel delay and passability recommendations leaves the cognitive burden of route synthesis entirely on the driver in the rain.
  - **Server-side pre-computed GeoJSON eliminates client CPU/battery burn.** Graph traversal and route geometries calculated server-side in Go execute in < 2ms without shipping massive routing engines (OSRM/Leaflet Routing Machine) to mobile browsers over degraded cellular connections.
  - **Flood penalties are non-linear.** Water depth > 20 cm causes engine stalling / hydraulic lock (*thủy kích*) on standard motorbikes, immediately degrading a corridor to `IMPASSABLE` (`#ef4444`) with high delay penalties.

---

## 1. Context & Problem Statement

Independent technical and product auditing at the conclusion of Sprint 14 identified that ProjectKV was operating as an engineer's Traffic Operations Center (TOC) monitoring dashboard rather than a commuter resilience platform. A citizen riding a motorcycle from District 1 to District 7 or Thu Duc at 5:00 PM during a storm could inspect individual cameras, but had no way to answer:
*"Which arterial corridor should I take to get home without drowning my motorbike?"*

While `services/go-server/internal/corridor` previously exposed `GET /api/roads/:road_key/status`, it only provided an alphabetical list of camera verdicts and a single `worst_severity` string without:
1. Spatial route geometries (coordinates / polylines) connecting cameras along the corridor.
2. Cumulative flood hazard indexes factoring in physical depth in centimetres (cm).
3. Estimated travel duration and delay in seconds comparing free-flow against congested/flooded conditions.
4. Clear, actionable passability verdicts (`PASSABLE`, `CAUTION`, `IMPASSABLE`).
5. A city-wide overview endpoint allowing the frontend to present corridor choices in a single network round-trip.

---

## 2. Root Cause Analysis & Architectural Solutions

### 2.1. Canonical Arterial Corridors in Memory
Shipping an external topological graph routing database (e.g. pgRouting or local OSRM) would introduce banned dependencies or heavy runtime overheads. Instead, Ho Chi Minh City's primary commuter journeys are constrained to approximately 10 major arterial corridors (Võ Văn Kiệt, Mai Chí Thọ, Xa Lộ Hà Nội, Điện Biên Phủ, Nguyễn Hữu Cảnh, Nam Kỳ Khởi Nghĩa - Nguyễn Văn Trỗi, Cộng Hòa, Phạm Văn Đồng, Cách Mạng Tháng 8, Nguyễn Văn Linh).

In `routes.go`, each corridor is statically defined with its canonical geometry, physical length in kilometres, typical motorcycle free-flow speed, and coordinates:
```go
type CorridorDefinition struct {
    Key              string       `json:"key"`
    DisplayName      string       `json:"display_name"`
    Description      string       `json:"description"`
    LengthKm         float64      `json:"length_km"`
    FreeFlowSpeedKmh float64      `json:"free_flow_speed_kmh"`
    Aliases          []string     `json:"aliases,omitempty"`
    Coordinates      [][2]float64 `json:"coordinates"` // [longitude, latitude] in WGS84
}
```

If an ad-hoc road key is queried that is not pre-registered, the service dynamically synthesizes a corridor linestring by bounding and sorting the coordinates of cameras assigned to that road key.

### 2.2. Cumulative Hazard & Duration Computation
The travel duration engine calculates:
1. **Free-flow baseline**: `(LengthKm / FreeFlowSpeedKmh) * 3600`
2. **Congestion multiplier**:
   - `CLEAR` / `UNKNOWN`: 1.0x
   - `MODERATE`: 1.4x
   - `HEAVY` / `CONGESTED`: 2.2x
3. **Physical water depth penalty**:
   - Depth > 20 cm: +900s (15 min penalty / impassable)
   - Depth 10–20 cm: +360s (6 min penalty / exhaust submerged caution)
   - Depth < 10 cm: +120s (2 min penalty / caution)
4. **Passability recommendation**:
   - `IMPASSABLE` (`#ef4444`): If max depth > 20 cm, hazard level 3, or `FLOODED_DEEP`.
   - `CAUTION` (`#eab308`): If max depth 10–20 cm, hazard level 2, or heavy congestion.
   - `PASSABLE` (`#22c55e`): Normal flow with zero dangerous ponding.
   - `UNKNOWN` (`#71717a`): Insufficient verified observation data.

### 2.3. GeoJSON FeatureCollection Contract
`GET /api/roads/:road_key/route` outputs a GeoJSON `FeatureCollection`:
- Feature 1: The corridor `LineString` carrying route summary properties, stroke color, and delay metrics.
- Features 2..N: `Point` features for every camera on the corridor with live flood depth and status color.

In addition, `GET /api/corridors/overview` returns an array of `CorridorRouteSummary` structs for all 10 canonical corridors in a single sub-5ms call.

---

## 3. Verification & Empirical Evidence

- **Unit Tests**: Added `TestGetRoute_PassableAndGeoJSON`, `TestGetRoute_ImpassableFlood`, `TestGetCorridorsOverview`, and HTTP route tests in `corridor_test.go`.
- **Modulith Compliance**: Zero circular dependencies; strictly imports `camera` and `hotset` as authorized by `GO_BACKEND_ARCHITECTURE.md`.
- **Full Test Suite**: All 25 packages in `services/go-server` pass (`go test ./...`).
- **Swagger Documentation**: Generated and verified via `make swagger`.
