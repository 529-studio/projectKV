# Report: Real-Time Rain Camera Collection & Ground-Truth Labeling (2026-10-13)

- **Date**: 2026-10-13 (Afternoon capture window: 15:05–15:08 GMT+7)
- **Meteorological Context**: Active afternoon rain event across Ho Chi Minh City, Vietnam.
  - Open-Meteo Current Weather: `rain: 0.10 mm`, `weather_code: 51` (Light drizzle / continuous rain), `relative_humidity: 79%`, `temperature: 28.5°C`.
  - Visual Confirmation: Wet road surfaces, tire spray, puddle accumulation along curbs and crosswalks, commuters wearing raincoats/ponchos.
- **Collection Scope**: Flood-prone corridors and 10 authentic municipal flood chokepoints (`tools/eval/fixtures/flood_chokepoints.json`).
- **Storage Target**: Cloudflare R2 bucket `warded-proofs` (`R2:warded-proofs/2026-10-13/` and `R2:warded-proofs/labels/2026-10-13/`).

---

## 1. Executive Summary & Metrics

| Metric | Measurement | Notes |
|---|---|---|
| **Total Frames Collected** | **201 real frames** | Validated JFIF magic bytes (`\xff\xd8`), size 25 KB – 69 KB |
| **Collection Success Rate** | **97.1%** (201/207 attempts) | 6 upstream timeouts or placeholders excluded |
| **Chokepoints Captured** | **10 / 10 (100%)** | All 10 authentic municipal flood chokepoints present |
| **Ground-Truth Labels Generated** | **201 labels** | Dual-format (Team Web Labeler format + Benchmark dataset format) |
| **R2 Blob Storage Synced** | **201 JPEGs + 201 JSONs** | Partition: `2026-10-13/` and `labels/2026-10-13/` |
| **Inference Processing Rate** | **31.2 frames/sec** | OpenVINO YOLOv8n FP16 416x416 (`warded-model` container) |

---

## 2. Dataset Label Distributions

### Traffic Severity Distribution (PCU Saturation Index calibrated per S6-T2)
- **CLEAR**: **62 frames** (30.8%) — Free-flowing traffic on wide corridors (Võ Văn Kiệt, Mai Chí Thọ, QL1).
- **MODERATE**: **66 frames** (32.8%) — Steady traffic platoons through signal cycles (Điện Biên Phủ, Kinh Dương Vương).
- **HEAVY**: **31 frames** (15.4%) — Slow-moving dense queues (Xô Viết Nghệ Tĩnh, QL13 Cầu Ông Dầu).
- **CONGESTED**: **42 frames** (20.9%) — Severe tailbacks and gridlocks at major highway choke points (QL1 Ngã tư Linh Xuân, QL1 Tân Kỳ Tân Quý).

### Flood Severity Distribution
- **NONE**: **133 frames** (66.2%) — Wet asphalt surfaces without standing water accumulation.
- **SHALLOW**: **68 frames** (33.8%) — Visible puddles and standing water along curbs, intersections, and roadside gutters.
- **DEEP**: **0 frames** (0.0%) — No wheel-height submersion observed during the initial 0.10 mm drizzle window.

### Environmental Attributes
- **Weather**: **rain** = 201 frames (100.0%).
- **Time of Day**: **day** = 201 frames (100.0%, 15:05 GMT+7).

---

## 3. Authentic Municipal Flood Chokepoints Audit (10/10 Captured)

| Camera ID | Intersection & Corridor | District | Observed PCU | Saturation Index | Traffic | Flood |
|---|---|---|:---:|:---:|:---:|:---:|
| `586e2995f9fab7001111b0b5` | Ung Văn Khiêm - Nguyễn Gia Trí (D2) | Bình Thạnh | 2.0 | 0.13 | `CLEAR` | `SHALLOW` |
| `66b1c3fa779f740018674146` | Nguyễn Hữu Cảnh - Ung Văn Khiêm | Bình Thạnh | 4.8 | 0.32 | `MODERATE` | `SHALLOW` |
| `5a8254b05058170011f6eac3` | Xô Viết Nghệ Tĩnh - Nguyễn Xí 1 (Cầu Đỏ) | Bình Thạnh | 10.3 | 0.69 | `HEAVY` | `SHALLOW` |
| `63bd1f21bfd3d90017ec3d04` | Huỳnh Tấn Phát - Phạm Hữu Lầu | District 7 | 5.2 | 0.21 | `CLEAR` | `SHALLOW` |
| `662a88521afb9c00172d2592` | Kinh Dương Vương - Hồ Học Lãm | Bình Tân | 7.9 | 0.32 | `MODERATE` | `SHALLOW` |
| `6623f4df6f998a001b2528eb` | Quốc Lộ 13 - Cầu Ông Dầu | TP Thủ Đức | 12.8 | 0.51 | `HEAVY` | `SHALLOW` |
| `66b1c426779f74001867415e` | Quốc Lộ 1 - Cầu Bình Điền | Bình Chánh | 15.6 | 0.39 | `MODERATE` | `SHALLOW` |
| `5deb576d1dc17d7c5515ad08` | Nguyễn Văn Linh - Huỳnh Tấn Phát | District 7 | 11.2 | 0.25 | `CLEAR` | `SHALLOW` |
| `586e1f18f9fab7001111b0a5` | Đinh Bộ Lĩnh - Bạch Đằng | Bình Thạnh | 4.0 | 0.16 | `CLEAR` | `SHALLOW` |
| `58175c81edeb6c0012a2d5c2` | Võ Văn Kiệt - Hải Thượng Lãn Ông | District 5 | 8.5 | 0.19 | `CLEAR` | `SHALLOW` |

---

## 4. Storage & Persistence Layout

- **Cloudflare R2 Bucket**: `warded-proofs`
  - Frames: `R2:warded-proofs/2026-10-13/<camera_id>/<timestamp>_sample.jpg` (201 items)
  - Labels: `R2:warded-proofs/labels/2026-10-13/<camera_id>_agent_curator_<timestamp>.json` (201 items, 84.2 KiB total)
- **Local Repository Cache**:
  - Raw Proof Frames: `data/proofs/2026-10-13/`
  - R2 Format Labels: `tools/eval/labels_today_r2/`
  - Benchmark Dataset Format Labels: `tools/eval/labels_today_benchmark/`
  - Automation Tool: `tools/eval/label_today_rain.py`
