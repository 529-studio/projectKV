# `tools/geo-enrich`: Batch Phường Point-in-Polygon Enrichment

Offline batch data enrichment utility for `projectKV`. Joins each traffic camera's geographical coordinates (`latitude`, `longitude` in `cameras_all.json`) against post-reform municipal boundary polygons (GeoJSON) to enrich the catalog with `phuong_id` and `phuong_name`.

---

## 1. Context & Administrative Reform Background

- **Legal Framework**: Under **Nghị quyết số 202/2025/QH15** (effective July 1, 2025) and **Nghị quyết số 1685/NQ-UBTVQH15** on administrative unit restructuring, Vietnam shifted to a 2-tier local governance structure:
  $$\text{Tỉnh / Thành phố} \longrightarrow \text{Xã / Phường / Đặc khu}$$
  The intermediate *Quận / Huyện* tier was formally abolished.
- **Current Data Debt**: The legacy `cameras_all.json` catalog crawled from the municipal traffic portal contains outdated administrative labels (e.g., `"Q.12 - Hóc Môn - Củ Chi"`).
- **Architecture Mandate (ADR-003 Decision 3 & ADR-004 §1)**:
  - The hierarchical navigation tree is structured as: **Phường $\rightarrow$ Đường (Corridor) $\rightarrow$ Camera**.
  - Phường assignment is computed **once, offline via batch point-in-polygon**, never calculated dynamically at runtime or request-time.
  - Cameras outside municipal boundaries (e.g. inter-provincial feeds on expressways into Long An) or cameras lacking coordinates receive `phuong_id: null` and `phuong_name: null`. **No mappings are ever fabricated.**

---

## 2. Boundary Dataset Source & Coverage (Issue #63 / Sprint 2 A1)

### Dataset Source
- **Origin**: [thanglequoc/vietnamese-provinces-database](https://github.com/thanglequoc/vietnamese-provinces-database) (GIS dataset, `json/geojson/79_ho_chi_minh/wards/`).
- **Data Source Layer**: `diaphanhanhchinhcapxa_2025` (National Administrative Boundaries Layer post-reform 2025-2026).
- **Download & Consolidation Date**: 2026-09-29.
- **License**: MIT License.
- **Coordinate Reference System (CRS)**: WGS84 (`EPSG:4326`) with 6 decimal place precision (~0.1m).
- **Feature Count**: 168 administrative wards/communes covering the entire Ho Chi Minh City municipal and metropolitan corridor.

### Coverage & Matching Statistics
Running `make geo-enrich` against the 837-camera catalog yields:
- **Total cameras in catalog**: 837
- **Cameras with coordinates**: 797
- **Matched Phường (`phuong_name` populated)**: **794** (99.6% of cameras with coordinates)
- **Unmatched / Null**: **43**
  - **40 cameras**: Missing coordinates in upstream traffic portal (`latitude` / `longitude` is `null`).
  - **3 cameras**: Outside administrative extent on the TP.HCM - Trung Lương expressway corridor in Long An province (`TTH 0001` at QL1 - Ba Làng, `TTH 0003` at QL50 - Long An border arch, `TTH 0009` at Trần Văn Giàu - Cầu Đôi).

### Unmatched Cameras Report
All 43 unmatched cameras are systematically recorded in `tools/geo-enrich/unmatched_cameras.csv` with their exact ID, title, coordinates, and classification reason.

---

## 3. Features & Implementation

- **Zero-Dependency Core**: Includes a pure-Python Ray Casting point-in-polygon engine with bounding-box pre-filtering. Runs out of the box on standard Python 3.9+ without compiling C-libraries.
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
# Standard run (writes cameras_enriched.json and tools/geo-enrich/unmatched_cameras.csv)
python3 tools/geo-enrich/enrich.py \
  --cameras cameras_all.json \
  --boundaries tools/geo-enrich/data/phuong_boundaries.geojson \
  --output cameras_enriched.json \
  --unmatched-report tools/geo-enrich/unmatched_cameras.csv

# Test run using sample fixture
python3 tools/geo-enrich/enrich.py \
  --cameras cameras_all.json \
  --boundaries tools/geo-enrich/data/sample_phuong_boundaries.geojson \
  --output /tmp/test_enriched.json
```

### CLI Arguments:
- `--cameras`: Path to input camera JSON (default: `cameras_all.json`).
- `--boundaries`: Path to Phường boundary GeoJSON (default: `tools/geo-enrich/data/phuong_boundaries.geojson`).
- `--output`: Path for enriched output JSON (default: `cameras_enriched.json`).
- `--unmatched-report`: Path for unmatched cameras CSV report (default: `tools/geo-enrich/unmatched_cameras.csv`).
- `--id-prop`: (Optional) Explicit GeoJSON property name for ward ID.
- `--name-prop`: (Optional) Explicit GeoJSON property name for ward name.
- `--in-place`: (Optional) Overwrite `--cameras` in place instead of creating a separate file.

---

## 5. Testing

Run the test suite:
```bash
make test-enrich
```
or
```bash
python3 -m unittest discover -s tools/geo-enrich -p "test_*.py"
```
Verifies point-in-ring, polygon holes, bounding box reject, null fallback, sample fixture loading, unmatched CSV reporting, and deterministic idempotency.
