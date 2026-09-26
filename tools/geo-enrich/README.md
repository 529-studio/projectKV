# `tools/geo-enrich`: Batch Phường Point-in-Polygon Enrichment

Offline batch data enrichment utility for `projectKV`. Joins each traffic camera's geographical coordinates (`latitude`, `longitude` in `cameras_all.json`) against post-reform municipal boundary polygons (GeoJSON) to enrich the catalog with `phuong_id` and `phuong_name`.

---

## 1. Context & Administrative Reform Background

- **Legal Framework**: Under **Nghị quyết số 202/2025/QH15** (effective July 1, 2025) and **Nghị quyết số 1685/NQ-UBTVQH15** on administrative unit restructuring, Vietnam shifted to a 2-tier local governance structure:
  $$\text{Tỉnh / Thành phố} \longrightarrow \text{Xã / Phường / Đặc khu}$$
  The intermediate *Quận / Huyện* tier was formally abolished.
- **Current Data Debt**: The legacy `cameras_all.json` catalog crawled from the municipal portal contains outdated administrative labels (e.g., `"Q.12 - Hóc Môn - Củ Chi"`).
- **Architecture Mandate (ADR-003 Decision 3)**:
  - The hierarchical navigation tree is corrected to: **Phường $\rightarrow$ Đường (Corridor) $\rightarrow$ Camera**.
  - Phường assignment is computed **once, offline via batch point-in-polygon**, never calculated dynamically at runtime or request-time.
  - Cameras outside municipal boundaries (e.g. inter-provincial feeds in Bà Rịa - Vũng Tàu, Bình Dương) or unmapped locations receive `phuong_id: null` and `phuong_name: null`. **No mappings are ever fabricated.**

---

## 2. Boundary Data Source Gap Analysis (OSM / GSO)

Per the verification requirements of Sprint 1 Backlog Task A1:

| Source | Status | Finding & Data Gap Details |
|---|---|---|
| **General Statistics Office (GSO)** / Cổng Dữ liệu Quốc gia | **Incomplete / Not Readily Downloadable** | GSO provides national administrative codes (Mã đơn vị hành chính), but polygon vector spatial layers (GeoJSON/Shapefile) for post-reform 2025/2026 wards are not hosted as an open, direct single-file download. Municipal GIS departments maintain internal Shapefiles that are not yet synchronized on public open portals. |
| **OpenStreetMap (OSM)** | **Partial / Community In-Progress** | Community contributors in Vietnam are actively updating boundaries (e.g., Overpass queries for `admin_level=8`). However, full topological closure for all 168 wards/communes across HCMC with unified post-reform identifiers remains in active editing. |

### Operational Recommendation & Fallback Policy:
1. **Never fabricate boundaries**: If a camera point does not fall strictly within a verified boundary polygon, it is output with `null`.
2. **Pluggable GeoJSON Input**: The tool accepts any standard GeoJSON `FeatureCollection` via `--boundaries <path>`. As soon as official open GIS polygons are exported, placing the file at `tools/geo-enrich/data/phuong_boundaries.geojson` and re-running `make geo-enrich` immediately updates the dataset with zero code changes.
3. **Sample Fixture Included**: A verified reference fixture covering sample central wards is provided at `tools/geo-enrich/data/sample_phuong_boundaries.geojson` for testing and pipeline validation.

---

## 3. Features & Implementation

- **Zero-Dependency Core**: Includes a pure-Python Ray Casting point-in-polygon engine with bounding-box pre-filtering. Can run out of the box on standard Python 3.9+ without compiling C-libraries.
- **Vectorized Acceleration (Optional)**: Automatically uses `shapely` / `geopandas` if installed in the local environment.
- **Idempotent**: Formats JSON with 2-space indentation and UTF-8 encoding. Running the script multiple times on the same input produces byte-identical results.
- **Preserves Catalog Integrity**: Preserves all existing fields (`id`, `title`, `displayName`, `status`, `isOnline`, `latitude`, `longitude`, `snapshotUrl`).

---

## 4. Usage

### Via Makefile:
```bash
make geo-enrich
```

### Via CLI:
```bash
# Enrich using custom boundary file
python3 tools/geo-enrich/enrich.py \
  --cameras cameras_all.json \
  --boundaries tools/geo-enrich/data/phuong_boundaries.geojson \
  --output cameras_enriched.json

# Test run using sample fixture
python3 tools/geo-enrich/enrich.py \
  --cameras cameras_all.json \
  --boundaries tools/geo-enrich/data/sample_phuong_boundaries.geojson \
  --output cameras_enriched.json
```

### CLI Arguments:
- `--cameras`: Path to input camera JSON (default: `cameras_all.json`).
- `--boundaries`: Path to Phường boundary GeoJSON (default: `tools/geo-enrich/data/phuong_boundaries.geojson`).
- `--output`: Path for enriched output JSON (default: `cameras_enriched.json`).
- `--id-prop`: (Optional) Explicit GeoJSON property name for ward ID.
- `--name-prop`: (Optional) Explicit GeoJSON property name for ward name.
- `--in-place`: (Optional) Overwrite `--cameras` in place instead of creating a separate file.

---

## 5. Testing

Run the test suite:
```bash
python3 -m unittest discover -s tools/geo-enrich -p "test_*.py"
```
Verifies point-in-ring, polygon holes, bounding box reject, null fallback, sample fixture loading, and deterministic idempotency.
