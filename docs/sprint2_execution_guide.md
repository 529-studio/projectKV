# Sprint 2 — Hướng dẫn chạy agy CLI theo từng Wave

Tài liệu hướng dẫn vận hành cho AI agents (`agy` CLI) và developers thực thi toàn bộ Sprint 2 Backlog theo quy chuẩn [ADR-004](docs/ADR-004-route-check-and-snapshot-archive.md), [ADR-001](docs/ADR-001-flood-alert-architecture.md), và [GO_BACKEND_ARCHITECTURE.md](docs/GO_BACKEND_ARCHITECTURE.md).

---

## 1. Issue Map & Epics Grouping

### Danh sách Epics (Nhóm công việc có liên hệ)

| Epic # | Tên Epic | Issues con liên quan | Mục tiêu chính |
|--------|----------|----------------------|----------------|
| **#77** | `[Epic] Snapshot Storage & Frame Archiving Pipeline` | #57, #62 | Nền tảng lưu trữ local disk phục vụ ảnh bằng chứng & eval frame |
| **#78** | `[Epic] Administrative & Road Catalog Enrichment` | #63, #68, #67 | Phủ toàn bộ boundary Phường, chuẩn hóa tên đường, hỗ trợ i18n |
| **#76** | `[Epic] Spatial Hexagonal Grid (H3) Overlay` | #66, #73, #74 | Lưới lục giác H3 hiển thị tổng hợp mật độ/ngập ở tầm zoom xa |
| **#79** | `[Epic] VLM Analysis & Accuracy Evaluation Gate` | #70, #71 | Tích hợp Vision LLM thật và đo benchmark độ chính xác ngập |

---

### Bảng phân bố Wave thực thi

| Wave | # | ID | Title | Epic / Nhóm | Labels | GitHub URL |
|:---:|:---:|:---:|---|---|---|---|
| **0** | #57 | — | `[go-server] feat(storage): implement internal/storage interface` | Epic #77 | backend | https://github.com/529-studio/projectKV/issues/57 |
| **0** | #63 | A1 | `[tooling] geo-enrich: full Phường polygon coverage for merged HCMC region` | Epic #78 | tooling | https://github.com/529-studio/projectKV/issues/63 |
| **0** | #65 | A3 | `[web] filter-focus: dim non-matching cameras instead of hiding them` | Độc lập | frontend | https://github.com/529-studio/projectKV/issues/65 |
| **1** | #62 | B1 | `[go-server] storage: event-driven snapshot archiver with retention policy` | Epic #77 | backend | https://github.com/529-studio/projectKV/issues/62 |
| **1** | #68 | A6 | `[tooling + data] road name normalization: stable road_key for corridor grouping` | Epic #78 | tooling | https://github.com/529-studio/projectKV/issues/68 |
| **1** | #66 | C2 | `[tooling] geo-enrich: add H3 cell ID enrichment for hex overlay` | Epic #76 | tooling | https://github.com/529-studio/projectKV/issues/66 |
| **1** | #64 | A5 | `[go-server + web] verdict provenance: observed_at, stale, DEMO badge` | Độc lập | backend, frontend | https://github.com/529-studio/projectKV/issues/64 |
| **2** | #67 | A2 | `[data + web] structured ward/road names with proper i18n rendering` | Epic #78 | tooling, frontend | https://github.com/529-studio/projectKV/issues/67 |
| **2** | #69 | B7 | `[go-server] middleware: rate limiting, camera cap, and budget breaker` | Độc lập | backend | https://github.com/529-studio/projectKV/issues/69 |
| **2** | #70 | B3 | `[go-server] cvclient: real VLM analyzer adapter (Gemini/OpenAI)` | Epic #79 | backend | https://github.com/529-studio/projectKV/issues/70 |
| **2** | #73 | C3 | `[go-server] live: hex cell aggregation and hex_update SSE event` | Epic #76 | backend | https://github.com/529-studio/projectKV/issues/73 |
| **3** | #74 | C4 | `[web] hex overlay: H3 cell visualization on Leaflet map` | Epic #76 | frontend | https://github.com/529-studio/projectKV/issues/74 |
| **3** | #75 | B6 | `[go-server + web] corridor status: GET /api/roads/{road_key}/status` | Độc lập | backend, frontend | https://github.com/529-studio/projectKV/issues/75 |
| **3** | #71 | B4 | `[spike] eval set: labeled frames + precision/recall benchmark script` | Epic #79 | tooling | https://github.com/529-studio/projectKV/issues/71 |
| **3** | #72 | B5 | `[spike] routing engine: Valhalla vs OSRM vs hosted — HCMC comparison` | Độc lập | documentation | https://github.com/529-studio/projectKV/issues/72 |

---

## 2. Chuẩn bị môi trường

Trước khi bắt đầu, đồng bộ nhánh `dev`:

```bash
cd ~/repos/projectKV
git checkout dev
git pull origin dev
```

---

## Wave 0 — Nền tảng Độc lập (#57, #63, #65)

Ba ticket này hoàn toàn độc lập, có thể mở 3 terminal chạy song song.

### Terminal 1 — #57 (`internal/storage` interface + Local driver)

```bash
cd ~/repos/projectKV

agy "Implement issue #57: [go-server] feat(storage): implement internal/storage interface with local filesystem driver.

Read the full issue at https://github.com/529-studio/projectKV/issues/57 for complete Technical Details, Out of Scope, and Definition of Done.

Key requirements:
- Create services/go-server/internal/storage/storage.go with Storage interface (Put, Get, Delete, Exists) and error types (ErrNotFound, ErrInvalidPath)
- Create services/go-server/internal/storage/local.go with LocalStorage driver
- Create services/go-server/internal/storage/storage_test.go verifying path traversal rejection, atomic write via temp file, subdirectories
- Read optional config STORAGE_DRIVER (default 'local') and STORAGE_LOCAL_ROOT (default './data/proofs')
- Allowed imports: stdlib ONLY. Zero external dependencies, no cgo.
- Storage is a LEAF package: no internal imports.
- Run architecture-guardian skill review before completing.

Branch: feat/storage-interface
Open PR against dev when done. Do NOT merge."
```

### Terminal 2 — #63 (A1: Full Phường polygon coverage)

```bash
cd ~/repos/projectKV

agy "Implement issue #63: [tooling] geo-enrich: full Phường polygon coverage for merged HCMC region.

Read the full issue at https://github.com/529-studio/projectKV/issues/63 for complete Technical Details, Out of Scope, and Definition of Done.

Key requirements:
- Update tools/geo-enrich/data/phuong_boundaries.geojson with complete post-reform Phường boundaries
- Run enrichment so that >= 700 cameras with coordinates get valid phuong_name
- Cameras outside extent (Bình Dương, Bà Rịa) correctly documented in tools/geo-enrich/unmatched_cameras.csv
- Document polygon data source and license in tools/geo-enrich/README.md
- Idempotent: 2 runs produce byte-identical output
- Commit updated cameras_enriched.json
- Do NOT touch Go backend or apps/web

Branch: feat/a1-full-ward-polygons
Open PR against dev when done. Do NOT merge."
```

### Terminal 3 — #65 (A3: Filter as focus, not hide)

```bash
cd ~/repos/projectKV

agy "Implement issue #65: [web] filter-focus: dim non-matching cameras instead of hiding them.

Read the full issue at https://github.com/529-studio/projectKV/issues/65 for complete Technical Details, Out of Scope, and Definition of Done.

Key requirements:
- In apps/web/src/app/page.tsx: pass all cameras to MapComponent, pass filteredCameraIds Set
- In apps/web/src/components/MapComponent.tsx: non-matching cameras dimmed (opacity 0.2, smaller radius) but still visible and clickable
- Clicking a dimmed camera clears the filter and opens its modal
- Add on-map filter chip with ✕ button to clear active filter
- Escape key clears the filter
- Monochromatic styling: bg-zinc-950, border-zinc-700, text-zinc-200
- i18n support for vi and en

Branch: feat/a3-filter-focus
Open PR against dev when done. Do NOT merge."
```

### ⏸️ Chờ Wave 0 xong
1. Review PRs: #57, #63, #65
2. Merge vào `dev`
3. Cập nhật local: `git checkout dev && git pull origin dev`

---

## Wave 1 — Lưu trữ Snapshot & Chuẩn hóa Dữ liệu (#62, #68, #66, #64)

### Terminal 1 — #62 (B1: Event-driven archiver & retention policy)

```bash
cd ~/repos/projectKV

agy "Implement issue #62: [go-server] storage: event-driven snapshot archiver with retention policy.

Read the full issue at https://github.com/529-studio/projectKV/issues/62 for complete Technical Details, Out of Scope, and Definition of Done.

Key requirements:
- Create internal/archiver/ package injecting internal/storage.Storage
- Policy engine: event-driven on state change to 'jam'/'deep' + sampled (1 frame per 5m per Hot Set camera)
- Retention job: background goroutine deleting files older than ARCHIVE_RETENTION_DAYS (default 14) and enforcing ARCHIVE_MAX_DISK_MB (default 20000)
- Wire OnVerdict into scheduler fan-out in cmd/server/main.go (gated by analysisEnabled)
- Storage path: {date}/{camera_id}/{timestamp}_{reason}.jpg
- Unit tests for policy and retention logic
- Run architecture-guardian skill review before completing

Branch: feat/b1-archiver
Open PR against dev when done. Do NOT merge."
```

### Terminal 2 — #68 (A6: Road name normalization)

```bash
cd ~/repos/projectKV

agy "Implement issue #68: [tooling + data] road name normalization: stable road_key for corridor grouping.

Read the full issue at https://github.com/529-studio/projectKV/issues/68 for complete Technical Details, Out of Scope, and Definition of Done.

Key requirements:
- Create tools/geo-enrich/normalize_roads.py
- Normalize aliases: QL1 and Quốc lộ 1 produce identical road_key
- Intersection format 'A - B' yields two road keys
- Add road_key / road_keys to cameras_enriched.json
- Unit tests in test_normalize_roads.py
- Document alias table and rules in README.md

Branch: feat/a6-road-normalization
Open PR against dev when done. Do NOT merge."
```

### Terminal 3 — #66 (C2: H3 cell ID offline enrichment)

```bash
cd ~/repos/projectKV

agy "Implement issue #66: [tooling] geo-enrich: add H3 cell ID enrichment for hex overlay.

Read the full issue at https://github.com/529-studio/projectKV/issues/66 for complete Technical Details, Out of Scope, and Definition of Done.

Key requirements:
- Extend tools/geo-enrich/enrich.py with --h3-resolutions (default 7,9)
- Use h3-py to compute h3_res7 and h3_res9 for every camera with coordinates
- Add h3 dependency to requirements.txt
- Update cameras_enriched.json with H3 fields
- CRITICAL: NO H3 dependency in services/go-server (zero cgo)
- Unit tests for coordinate-to-H3 conversion

Branch: feat/c2-h3-enrichment
Open PR against dev when done. Do NOT merge."
```

### Terminal 4 — #64 (A5: Verdict provenance & staleness)

```bash
cd ~/repos/projectKV

agy "Implement issue #64: [go-server + web] verdict provenance: observed_at, stale, DEMO badge.

Read the full issue at https://github.com/529-studio/projectKV/issues/64 for complete Technical Details, Out of Scope, and Definition of Done.

Key requirements:
- Backend: add ObservedAt and Stale to Verdict struct, DB migration 0003_add_observed_at.sql
- Frozen-frame detector: K=3 identical hashes marks verdict stale
- Frontend: stale verdicts rendered as gray dot (not colored), modal shows stale notice
- Frontend: mock verdicts in dev mode show DEMO badge
- Legend includes stale entry when analysisEnabled
- Full i18n in vi and en with correct Vietnamese diacritics

Branch: feat/a5-verdict-provenance
Open PR against dev when done. Do NOT merge."
```

### ⏸️ Chờ Wave 1 xong
1. Review PRs: #62, #68, #66, #64
2. Merge vào `dev`
3. Cập nhật local: `git checkout dev && git pull origin dev`

---

## Wave 2 — Chuẩn hóa Naming, Guards & VLM Analyzer (#67, #69, #70, #73)

### Terminal 1 — #67 (A2: Structured ward/road names & i18n)

```bash
cd ~/repos/projectKV

agy "Implement issue #67: [data + web] structured ward/road names with proper i18n rendering.

Read the full issue at https://github.com/529-studio/projectKV/issues/67 for complete Technical Details, Out of Scope, and Definition of Done.

Key requirements:
- Data: split phuong_name into phuong_type (Phường/Xã) + prefix-less phuong_name (Bến Nghé)
- Backend: catalog.go exposes structured fields
- Frontend: PhuongTreeFilter renders 'Phường X' (vi) / 'X Ward' (en), 'Đường Y' (vi) / 'Y Street' (en)
- No baked prefixes remain in raw data fields
- Diacritic-insensitive search works with prefix-less names

Branch: feat/a2-structured-names
Open PR against dev when done. Do NOT merge."
```

### Terminal 2 — #69 (B7: Abuse & cost guards)

```bash
cd ~/repos/projectKV

agy "Implement issue #69: [go-server] middleware: rate limiting, camera cap, and budget breaker.

Read the full issue at https://github.com/529-studio/projectKV/issues/69 for complete Technical Details, Out of Scope, and Definition of Done.

Key requirements:
- In internal/middleware/ratelimit.go: token-bucket rate limiter per IP (RATE_LIMIT_RPS=10, BURST=30)
- In internal/hotset/budget.go: daily analysis budget with circuit breaker (degrades to 'unknown', never stale green)
- Budget resets at midnight UTC
- Unit tests for rate limiting and budget circuit breaker
- Run architecture-guardian skill review before completing

Branch: feat/b7-abuse-guards
Open PR against dev when done. Do NOT merge."
```

### Terminal 3 — #70 (B3: Real VLM analyzer adapter)

```bash
cd ~/repos/projectKV

agy "Implement issue #70: [go-server] cvclient: real VLM analyzer adapter (Gemini/OpenAI multimodal).

Read the full issue at https://github.com/529-studio/projectKV/issues/70 for complete Technical Details, Out of Scope, and Definition of Done.

Key requirements:
- Implement CloudVisionAnalyzer in internal/cvclient/cloudvision.go calling Gemini API
- Prompt returns structured JSON: traffic_severity, flood_severity, confidence, description
- Error paths and low confidence (<0.5) return 'unknown' severity (never guess)
- Set Verdict.Source to 'vlm'
- Unit tests with mocked HTTP responses
- Gated by CLOUD_VISION_API_KEY, wire into ANALYZER=vlm

Branch: feat/b3-vlm-analyzer
Open PR against dev when done. Do NOT merge."
```

### Terminal 4 — #73 (C3: Hex aggregation + SSE event)

```bash
cd ~/repos/projectKV

agy "Implement issue #73: [go-server] live: hex cell aggregation and hex_update SSE event.

Read the full issue at https://github.com/529-studio/projectKV/issues/73 for complete Technical Details, Out of Scope, and Definition of Done.

Key requirements:
- Create internal/live/hex.go: group cameras by precomputed h3_id (resolution 7)
- Aggregation rules: >= 3 qualifying fresh non-mock cameras required; worst-of severity (never averaged)
- Emit 'hex_update' SSE event via existing /api/live/stream
- Precomputed polygon coordinates loaded at startup (NO H3 library in Go)
- Unit tests for floor threshold, worst-of rule, and stale exclusion

Branch: feat/c3-hex-aggregation
Open PR against dev when done. Do NOT merge."
```

### ⏸️ Chờ Wave 2 xong
1. Review PRs: #67, #69, #70, #73
2. Merge vào `dev`
3. Cập nhật local: `git checkout dev && git pull origin dev`

---

## Wave 3 — UI Hex Overlay, Corridor Status & Spikes (#74, #75, #71, #72)

### Terminal 1 — #74 (C4: Hex overlay rendering on Leaflet)

```bash
cd ~/repos/projectKV

agy "Implement issue #74: [web] hex overlay: H3 cell visualization on Leaflet map.

Read the full issue at https://github.com/529-studio/projectKV/issues/74 for complete Technical Details, Out of Scope, and Definition of Done.

Key requirements:
- Create apps/web/src/components/HexOverlay.tsx rendering Leaflet polygons from SSE hex_update
- Low-opacity functional tints matching approved palette (rgba green 0.18, yellow 0.18, red 0.22)
- Macro zoom (11-13) visible; micro zoom (14+) fades out
- Tooltip displays camera count and worst camera name (NEVER a ward name)
- Toggle chip: [ 🔷 Vùng tổng hợp ] / [ 🔷 Zone Overview ]
- NO h3-js dependency added to package.json
- Full i18n support in vi and en

Branch: feat/c4-hex-overlay
Open PR against dev when done. Do NOT merge."
```

### Terminal 2 — #75 (B6: 2a Corridor status endpoint & UI)

```bash
cd ~/repos/projectKV

agy "Implement issue #75: [go-server + web] corridor status: GET /api/roads/{road_key}/status.

Read the full issue at https://github.com/529-studio/projectKV/issues/75 for complete Technical Details, Out of Scope, and Definition of Done.

Key requirements:
- Backend: internal/corridor/ package providing GET /api/roads/{road_key}/status and GET /api/roads
- Return worst_severity, coverage_pct, oldest_observation_age_s, cameras list
- Honesty rules: never says 'OK to go', unverified length shown as 'unknown'
- Frontend: severity badge on street rows in PhuongTreeFilter
- Update Swagger documentation via make swagger

Branch: feat/b6-corridor-status
Open PR against dev when done. Do NOT merge."
```

### Terminal 3 — #71 (B4: Eval set & benchmark harness)

```bash
cd ~/repos/projectKV

agy "Implement issue #71: [spike] eval set: labeled frames + precision/recall benchmark script.

Read the full issue at https://github.com/529-studio/projectKV/issues/71 for complete Technical Details, Out of Scope, and Definition of Done.

Key requirements:
- Create tools/eval/ with label_schema.json (traffic, flood, weather, time_of_day)
- Create benchmark.py running analyzer on labeled frames, outputting precision/recall and confusion matrix
- Focus on flood false-negative rate (accuracy gate metric)
- Tested on sample fixtures; documentation in tools/eval/README.md

Branch: feat/b4-eval-harness
Open PR against dev when done. Do NOT merge."
```

### Terminal 4 — #72 (B5: Routing engine spike report)

```bash
cd ~/repos/projectKV

agy "Implement issue #72: [spike] routing engine: Valhalla vs OSRM vs hosted — HCMC comparison.

Read the full issue at https://github.com/529-studio/projectKV/issues/72 for complete Technical Details, Out of Scope, and Definition of Done.

Key requirements:
- Evaluate Valhalla vs OSRM vs hosted on HCMC extract
- Compare motorcycle profile, avoid-area support, RAM inside 256MB container, and P50/P99 latency
- Check in evaluation report to docs/spike-routing-engine.md with clear decision and rationale

Branch: spike/b5-routing-engine
Open PR against dev when done. Do NOT merge."
```

### ⏸️ Chờ Wave 3 xong
1. Review PRs: #74, #75, #71, #72
2. Merge vào `dev`
3. Cập nhật local: `git checkout dev && git pull origin dev`

---

## 3. Tổng kết quy trình Workflow

```
Wave 0:  Term 1 (#57)    ┃  Term 2 (#63 A1)  ┃  Term 3 (#65 A3)
         ───────────────────────────────────────────────────────
                      ⏸️ Review & Merge to dev
         ───────────────────────────────────────────────────────
Wave 1:  Term 1 (#62 B1) ┃  Term 2 (#68 A6)  ┃  Term 3 (#66 C2)  ┃ Term 4 (#64 A5)
         ───────────────────────────────────────────────────────────────────────────
                      ⏸️ Review & Merge to dev
         ───────────────────────────────────────────────────────────────────────────
Wave 2:  Term 1 (#67 A2) ┃  Term 2 (#69 B7)  ┃  Term 3 (#70 B3)  ┃ Term 4 (#73 C3)
         ───────────────────────────────────────────────────────────────────────────
                      ⏸️ Review & Merge to dev
         ───────────────────────────────────────────────────────────────────────────
Wave 3:  Term 1 (#74 C4) ┃  Term 2 (#75 B6)  ┃  Term 3 (#71 B4)  ┃ Term 4 (#72 B5)
         ───────────────────────────────────────────────────────────────────────────
                      ⛔ Sprint 2 Review & Gate to 2b
```

> **IMPORTANT**: Giữa mỗi wave, **bắt buộc** phải merge PRs vào `dev` và `git pull origin dev` trước khi bắt đầu wave tiếp theo để các agent có đầy đủ code phụ thuộc.
>
> **CAUTION**: Tuân thủ nghiêm ngặt `AGENTS.md`: AI agents tuyệt đối **không tự ý merge** Pull Request. Tất cả PR phải được Human review trước khi merge.
