# Routing Engine Spike — Valhalla vs OSRM vs Hosted (HCMC)

**Issue:** [#72](https://github.com/529-studio/projectKV/issues/72)  
**Branch:** `spike/b5-routing-engine`  
**Date:** 2026-10-01  
**Decision:** Adopt **Valhalla** self-hosted.

## Executive Summary

| Criterion | Valhalla | OSRM | Hosted API |
|---|---|---|---|
| Motorcycle / scooter profile | `motorcycle`, `motor_scooter` built-in | No — must author Lua profile | Varies |
| Arbitrary avoid-area (flooded segment) | `exclude_polygons` / `exclude_locations` supported | Only `exclude` by road class (toll/motorway/ferry) | Varies |
| RAM inside 256 MB container | **~152 MiB peak** — fits | **OOM killed** — does not fit | N/A |
| Peak RAM (unlimited) | ~223 MiB | ~344 MiB | N/A |
| Build time (HCMC clip) | ~21 s | ~42 s | N/A |
| Graph disk size | ~150 MB | ~506 MB | N/A |
| Latency P50 (local) | 7–35 ms | <1–1.4 ms | Network dependent |
| Latency P99 (local) | 13–122 ms | 2.3–9 ms | Network dependent |
| License | MIT | BSD-2-Clause | Provider-specific |
| Verdict | **Adopt** | Weak for 2c | Fallback only |

**Adopt Valhalla.** It is the only option that satisfies the two hard product requirements:

1. First-class scooter/motorcycle routing (`motor_scooter` costing).
2. Arbitrary polygon avoidance (`exclude_polygons`) for flooded corridor rerouting (2c).

OSRM is much faster but fails both hard requirements. A hosted API is a fallback if self-hosting becomes operationally impractical, but it reintroduces quota, ToS, and scooter-support uncertainty.

## Methodology

All measurements were run locally on a MacBook Pro (Apple M3, 8 cores, 24 GB RAM) using Docker Desktop configured with **4 CPUs / 7.7 GB**.

1. **OSM extract:** `vietnam-latest.osm.pbf` from [Geofabrik](https://download.geofabrik.de/asia/vietnam-latest.osm.pbf/) (314 MB).
2. **Clip:** Extracted HCMC bounding box with `osmium-tool`:
   ```text
   106.35,10.15 → 107.45,11.35
   ```
   Resulting `hcmc.osm.pbf` is **36 MB**.
3. **Valhalla:** `ghcr.io/valhalla/valhalla:3.6.0` — built config + tiles with `valhalla_build_config` / `valhalla_build_tiles`.
4. **OSRM:** `ghcr.io/project-osrm/osrm-backend:latest` — MLD pipeline (`osrm-extract` → `osrm-partition` → `osrm-customize`).
5. **Latency:** Python loop of 50 sequential HTTP requests per route, warm caches, measured with `time.perf_counter()` (includes HTTP overhead).

> **Note on port 5000:** macOS AirPlay Receiver binds port 5000. OSRM had to be exposed on port **5001** to avoid 403 responses from the OS service.

## Detailed Results

### Build Metrics

| Metric | Valhalla | OSRM |
|---|---|---|
| Input PBF | `hcmc.osm.pbf` (36 MB) | `hcmc.osm.pbf` (36 MB) |
| Build command | `valhalla_build_config` + `valhalla_build_tiles` | `osrm-extract` + `osrm-partition` + `osrm-customize` |
| Build time | ~21 s | ~42 s |
| Graph disk size | 150 MB | 506 MB |
| Threads used | 4 | 4 |

### RAM Measurements

**Container limit: `--memory=256m`**

| Engine | Result | Peak observed |
|---|---|---|
| Valhalla | ✅ Starts and serves routes | 151.6 MiB |
| OSRM | ❌ Killed by OOM during startup | N/A (process killed before serving) |

**Unlimited container during benchmark load**

| Engine | Peak RSS |
|---|---|
| Valhalla | 223.4 MiB |
| OSRM | 343.8 MiB |

For the clipped HCMC extract, Valhalla fits inside the 256 MB staging limit with a small safety margin. OSRM does not, primarily because it memory-maps/loads the full MLD graph (~506 MB on disk) into the process address space.

### Latency Benchmark

10 representative HCMC routes, 50 sequential requests each.

| Route | Valhalla P50 | Valhalla P99 | OSRM P50 | OSRM P99 |
|---|---:|---:|---:|---:|
| short_d1_d3 | 8.2 ms | 42.4 ms | 0.8 ms | 5.1 ms |
| short_d1_turtle | 7.0 ms | 13.8 ms | 0.8 ms | 3.0 ms |
| medium_d1_landmark81 | 11.9 ms | 13.2 ms | 0.9 ms | 2.8 ms |
| medium_d1_phunhuan | 12.3 ms | 16.5 ms | 0.9 ms | 2.5 ms |
| medium_q7_d1 | 11.2 ms | 13.4 ms | 1.0 ms | 2.3 ms |
| long_thuduc_d1 | 19.6 ms | 28.5 ms | 1.1 ms | 5.6 ms |
| long_hocmon_catlai | 18.1 ms | 44.1 ms | 1.3 ms | 9.0 ms |
| long_binhtan_thuduc | 27.2 ms | 79.9 ms | 1.4 ms | 7.3 ms |
| long_q12_d1 | 27.2 ms | 122.2 ms | 1.2 ms | 2.8 ms |
| avoid_d1_d3* | 34.9 ms | 35.8 ms | N/A | N/A |

\* `avoid_d1_d3` was measured on Valhalla with a working `exclude_polygons` request that forced a detour (distance increased from 1.90 km to 2.83 km). OSRM cannot express arbitrary polygon avoidance, so no measurement exists.

### Motorcycle Profile Comparison (Valhalla)

| Costing | Distance | Time | Notes |
|---|---:|---:|---|
| `motorcycle` | 5.977 km | 470 s | Defaults to higher-class roads |
| `motor_scooter` | 5.994 km | 563 s | Avoids higher-class roads unless country override — preferred for HCMC scooter traffic |

`motor_scooter` is the appropriate default for the product.

### Avoid-Area Support

| Engine | Capability | Observed |
|---|---|---|
| Valhalla | `exclude_polygons` with arbitrary lat/lon polygon arrays | ✅ Works — route detours around blocked polygon |
| Valhalla | `exclude_locations` (radius around points) | Available in API (not explicitly tested) |
| OSRM | `exclude=toll\|motorway\|ferry` (one class at a time, depending on profile `excludable` config) | ✅ Single-class exclude works; combinations rejected |
| OSRM | Arbitrary polygon / flooded segment | ❌ Not supported |

## Analysis

### Why not OSRM?

1. **No scooter profile:** The bundled profiles are `car`, `bicycle`, `foot`. A realistic motorbike profile for HCMC would require authoring and maintaining a custom Lua profile and continuously validating it against local road rules.
2. **No arbitrary avoid-area:** Route-check 2c needs to detour around flooded corridor segments. OSRM only supports excluding predefined road classes, not user-supplied polygons. This is a hard blocker.
3. **Does not fit 256 MB:** Even for the small HCMC clip, OSRM was OOM-killed under a 256 MB limit.

OSRM's latency advantage is real, but it only matters if the engine can do the job. It cannot.

### Why not hosted?

A hosted routing API removes RAM pressure, but:

- Scooter/motorcycle support and avoid-area behavior vary by provider.
- Public OSRM demo server is non-commercial, rate-limited, and unreliable for a product.
- Commercial APIs introduce quota cost, network latency, and external dependency for a safety-critical path.

Hosted should remain a fallback, not the primary design.

### Valhalla caveats

- **Close to the RAM ceiling:** Valhalla peaked at ~152 MiB in a 256 MB container for this HCMC clip. There is little headroom for concurrent requests or a larger extract. If the operational area grows beyond HCMC, the 256 MB assumption must be revisited.
- **`exclude_polygons` reliability:** There are open GitHub reports of `exclude_polygons` misbehaving in some Valhalla versions. In this spike it worked correctly, but it must be re-verified whenever the image is upgraded.
- **P99 latency spikes:** Some long routes showed P99 >100 ms (e.g., `long_q12_d1`). These are still acceptable for a non-real-time route-check UI but should be monitored under load.

## Decision

**Adopt Valhalla self-hosted** for the route-check engine.

Run the engine as a separate container/service on the home server (`local-edge host`), not inside the 256 MB `warded-stg` container. Tiles can be pre-built on a faster machine (e.g., the M3 Mac used in this spike) and copied to the server, because Valhalla tiles are architecture-independent serialized data.

## Reproducible Commands

```bash
# 1. Download Vietnam extract
curl -L --path-as-is -o vietnam-latest.osm.pbf \
  "https://download.geofabrik.de/asia/vietnam-latest.osm.pbf/"

# 2. Clip HCMC
osmium extract -b 106.35,10.15,107.45,11.35 \
  vietnam-latest.osm.pbf -o hcmc.osm.pbf

# 3. Build Valhalla tiles
mkdir -p valhalla/tiles
docker run --rm -v "$PWD:/data" ghcr.io/valhalla/valhalla:3.6.0 bash -c '
  valhalla_build_config \
    --mjolnir-tile-dir /data/valhalla/tiles \
    --mjolnir-timezone /data/valhalla/timezones.sqlite \
    --mjolnir-admin /data/valhalla/admins.sqlite \
    > /data/valhalla/valhalla.json
  valhalla_build_tiles -c /data/valhalla/valhalla.json /data/hcmc.osm.pbf
'

# 4. Run Valhalla
docker run -d --rm --name valhalla -v "$PWD:/data" -p 8002:8002 \
  ghcr.io/valhalla/valhalla:3.6.0 valhalla_service /data/valhalla/valhalla.json 1

# 5. Build OSRM graph
docker run --rm -v "$PWD:/data" ghcr.io/project-osrm/osrm-backend:latest bash -c '
  cd /data
  osrm-extract -p /opt/car.lua hcmc.osm.pbf
  osrm-partition hcmc.osrm
  osrm-customize hcmc.osrm
'

# 6. Run OSRM (use 5001 on macOS to avoid AirPlay on 5000)
docker run -d --rm --name osrm -v "$PWD:/data" -p 5001:5000 \
  ghcr.io/project-osrm/osrm-backend:latest osrm-routed --algorithm mld /data/hcmc.osrm

# 7. Sample Valhalla motor_scooter route with polygon avoidance
curl -s http://localhost:8002/route -X POST \
  -H "Content-Type: application/json" \
  -d '{"locations":[{"lat":10.772,"lon":106.698},{"lat":10.780,"lon":106.699}],"costing":"motor_scooter","exclude_polygons":[[[106.697,10.775],[106.700,10.775],[106.700,10.778],[106.697,10.778]]]}'

# 8. Sample OSRM route
curl -s "http://localhost:5001/route/v1/driving/106.698,10.772;106.699,10.780"
```

## Next Steps

1. Define the `route.Engine` Go interface in `services/go-server/internal/route/` (swappable adapter for Valhalla, OSRM, hosted).
2. Integrate Valhalla HTTP calls into the route-check handler (2b): A→B polyline → camera matching (D3) → worst-of verdict coloring.
3. Build a CI step or script that pre-builds Valhalla tiles from `cameras_enriched.json` bbox and ships them to `local-edge host`.
4. Re-verify `exclude_polygons` after every Valhalla image upgrade.
5. Monitor Valhalla memory under concurrent route requests; if it grows close to 256 MB, move the engine to a dedicated container with ≥512 MB.

---

*Measured by `gdq2k2` / OpenCode agent on branch `spike/b5-routing-engine`.*
