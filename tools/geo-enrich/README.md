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
- `--h3-resolutions`: (Optional) Comma-separated H3 resolutions to compute (default: `7,9`; pass `'none'` or `''` to disable).
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
Verifies point-in-ring, polygon holes, bounding box reject, null fallback, sample fixture loading, unmatched CSV reporting, road name normalization, alias mapping, and deterministic idempotency.

---

## 6. Road Name Normalization & Corridor Keys (Issue #68 / ADR-004 §1)

`tools/geo-enrich/normalize_roads.py` computes stable, canonical `road_key` and `road_keys` for every camera in `cameras_enriched.json`, enabling robust corridor grouping and addressing data quality anomalies identified in ADR-004 §1.

### Normalization Rules

1. **Diacritics & Punctuation Stripping**:
   - Converts Vietnamese characters to ASCII (`đ`/`Đ` -> `d`/`D`, Unicode NFD diacritics stripped).
   - Replaces non-alphanumeric characters with underscores (`_`).
2. **Alias Unification (`ALIAS_TABLE`)**:
   - Highway abbreviations (`QL1`, `QL 1`, `Quốc Lộ 1`, `QL1A`, `Quốc Lộ 1A`, `IDICO-QL1`) all map to `quoc_lo_1`.
   - Provincial & rural routes (`TL10` -> `tinh_lo_10`, `HL2` -> `huong_lo_2`).
   - Urban arterials & acronyms:
     - `XLHN`, `Xa Lộ Hà Nội` -> `xa_lo_ha_noi`
     - `VVK`, `Võ Văn Kiệt`, `Đại Lộ Đông Tây` -> `vo_van_kiet`
     - `PVĐ`, `PVD`, `Phạm Văn Đồng` -> `pham_van_dong`
     - `ĐBP`, `Điện Biên Phủ` -> `dien_bien_phu`
     - `CMTT`, `Cách Mạng Tháng Tám`, `Cách Mạng Tháng 8` -> `cach_mang_thang_tam`
     - `MCT`, `Mai Chí Thọ` -> `mai_chi_tho`
     - `NVL`, `Nguyễn Văn Linh` -> `nguyen_van_linh`
     - `NTMK`, `Nguyễn Thị Minh Khai` -> `nguyen_thi_minh_khai`
     - `NKKN`, `Nam Kỳ Khởi Nghĩa` -> `nam_ky_khoi_nghia`
     - `THD`, `Trần Hưng Đạo` -> `tran_hung_dao`
     - `Cao tốc LTDG`, `CT LTDG` -> `cao_toc_long_thanh_dau_giay`
     - `TTH 48.4`, `Cao tốc Trung Lương` -> `cao_toc_trung_luong`
3. **Intersection Decomposition (`road_key` vs `road_keys`)**:
   - Camera names formatted as `A - B` (or with en-dash `–`, em-dash `—`, minus `−`) are split into individual road segments.
   - `road_key`: Primary corridor key (first valid road segment).
   - `road_keys`: Array of distinct road keys for the camera (e.g. `["nguyen_hue", "le_loi"]` for `"Nguyễn Huệ - Lê Lợi"`).
4. **Disambiguation: Designated Route Numbers vs. Camera Indexing**:
   - Numbered streets (`Đường số 10` -> `duong_so_10`), provincial highways (`Tỉnh lộ 10` -> `tinh_lo_10`), canals (`Kênh 4` -> `kenh_4`), and dates (`Tháng 8` -> `thang_tam`) preserve their numerical designations.
   - Suffix camera instances (e.g., `An Dương Vương 2` -> `an_duong_vuong`, `Đỗ Xuân Hợp (1)` -> `do_xuan_hop`) are cleanly stripped.
5. **Deterministic Handling of ADR-004 §1 Duplicate Titles**:
   - 6 camera title pairs appear multiple times in the catalog:
     - `"Nút giao Ngã sáu Cộng Hòa"`
     - `"Nút giao Công Trường Dân Chủ"`
     - `"Đường hầm (Đầu hầm Tp Thủ Đức)"`
     - `"Nút giao Ngã sáu Nguyễn Tri Phương"`
     - `"Cộng Hòa - Hoàng Hoa Thám"`
     - `"Nút giao Ngã bảy Lý Thái Tổ"`
   - Each physical camera retains its distinct camera `id` and coordinates while receiving identical, deterministic `road_key` and `road_keys` values.

### Alias Reference Table

| Raw Input Pattern | Canonical `road_key` | Description |
|---|---|---|
| `QL1`, `Quốc Lộ 1`, `QL1A`, `IDICO-QL1` | `quoc_lo_1` | National Highway 1 |
| `QL1K`, `Quốc Lộ 1K` | `quoc_lo_1k` | National Highway 1K |
| `QL13`, `Quốc Lộ 13` | `quoc_lo_13` | National Highway 13 |
| `QL22`, `Quốc Lộ 22` | `quoc_lo_22` | National Highway 22 |
| `QL50`, `Quốc Lộ 50` | `quoc_lo_50` | National Highway 50 |
| `QL51`, `Quốc Lộ 51` | `quoc_lo_51` | National Highway 51 |
| `TL10`, `Tỉnh Lộ 10` | `tinh_lo_10` | Provincial Route 10 |
| `TL8`, `Tỉnh Lộ 8` | `tinh_lo_8` | Provincial Route 8 |
| `HL2`, `Hương Lộ 2` | `huong_lo_2` | Rural Route 2 |
| `Cao tốc LTDG`, `CT LTDG` | `cao_toc_long_thanh_dau_giay` | Long Thành - Dầu Giây Expressway |
| `TTH 48.4`, `Cao tốc Trung Lương` | `cao_toc_trung_luong` | TP.HCM - Trung Lương Expressway |
| `XLHN`, `Xa Lộ Hà Nội` | `xa_lo_ha_noi` | Hanoi Highway |
| `VVK`, `Võ Văn Kiệt`, `Đại Lộ Đông Tây` | `vo_van_kiet` | Vo Van Kiet Boulevard |
| `PVĐ`, `PVD`, `Phạm Văn Đồng` | `pham_van_dong` | Pham Van Dong Boulevard |
| `ĐBP`, `DBP`, `Điện Biên Phủ` | `dien_bien_phu` | Dien Bien Phu |
| `CMTT`, `Cách Mạng Tháng Tám`, `Cách Mạng Tháng 8` | `cach_mang_thang_tam` | Cach Mang Thang Tam |
| `MCT`, `Mai Chí Thọ` | `mai_chi_tho` | Mai Chi Tho Boulevard |
| `NVL`, `Nguyễn Văn Linh` | `nguyen_van_linh` | Nguyen Van Linh Parkway |
| `NTMK`, `Nguyễn Thị Minh Khai` | `nguyen_thi_minh_khai` | Nguyen Thi Minh Khai |
| `NKKN`, `Nam Kỳ Khởi Nghĩa` | `nam_ky_khoi_nghia` | Nam Ky Khoi Nghia |
| `THD`, `Trần Hưng Đạo` | `tran_hung_dao` | Tran Hung Dao |

### Running Normalization

```bash
# Via Makefile:
make normalize-roads

# Via CLI:
python3 tools/geo-enrich/normalize_roads.py --input cameras_enriched.json --output cameras_enriched.json
```

---

## 7. H3 Hexagonal Cell ID Spatial Enrichment (Issue #66 / Sprint 2 C2 / ADR-004 §D11)

Offline hexagonal spatial cell ID generation for high-performance map hex overlays.

### Architectural Constraint (ADR-004 §D11)
- **Zero cgo in Go Server**: Banned `uber/h3-go` from `services/go-server/` (`GO_BACKEND_ARCHITECTURE.md` §1). All cell IDs are precomputed offline and loaded into memory as static camera attributes (`h3_res7`, `h3_res9`).
- **Zero `h3-js` in Web Client**: Frontend does not compute or index coordinates at runtime; it receives indexed cell IDs directly from backend catalog and query responses.

### Resolution Rationale
- **Resolution 7** (~5.16 km² cell area, edge length ~1.22 km): Macro zoom (11–13) overview for city-wide corridor clustering and regional traffic density heatmaps.
- **Resolution 9** (~0.105 km² cell area, edge length ~0.17 km): Mid-to-high zoom (14+) detail for street-level bottlenecks and intersection corridor evaluation.
- Both resolutions are computed and stored. The client dynamically toggles the rendered hex layer based on the current map zoom level.

### Coverage & Statistics
- **Total cameras**: 837
- **Cameras with valid coordinates**: 797 (100% assigned deterministic `h3_res7` and `h3_res9` cells).
- **Cameras lacking coordinates**: 40 (assigned `h3_res7: null` and `h3_res9: null`).

### Installation & Usage
Dependencies are listed in `tools/geo-enrich/requirements.txt`:
```bash
pip install -r tools/geo-enrich/requirements.txt
```

Run enrichment with default resolutions (7, 9):
```bash
# Via Makefile:
make geo-enrich

# Via CLI with explicit resolutions:
python3 tools/geo-enrich/enrich.py --h3-resolutions 7,9
```


