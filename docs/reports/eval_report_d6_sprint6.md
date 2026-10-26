# AI Analyzer Evaluation Report (ADR-004 §D6 Accuracy Gate)

- **Generated At**: `2026-10-13T06:09:13.977499+00:00`
- **Analyzer**: `cv-engine` (`yolov8n_openvino_416`)
- **Evaluation Dataset**: `764` labeled frames from `../../tools/eval/dataset/labels_d6_real.json`

## 1. Executive Summary & Accuracy Gate

| Gate Metric | Target | Actual Value | Verdict |
|---|---|---|---|
| **Flood False-Negative Rate (FNR)** | ≤ 5.0% | **100.0%** (120/120 missed) | **FAIL (SAFETY GATE BREACH)** |
| Traffic Macro F1 | ≥ 0.70 | **0.245** | **[FAIL]** (Below 0.70 Target) |
| Flood Macro F1 | ≥ 0.80 | **0.305** | **[FAIL]** (Below 0.80 Target) |

> **Safety Gate Rule (ADR-004 §D6)**: A flood false-negative (failing to detect standing water or deep floods) directs drivers into hazardous corridors. Until the flood FNR is rigorously ≤ 5.0% across ≥60 rain/flood frames, route verdicts cannot show an unrestricted 'clear' state.

## 2. Dataset Composition Progress (Target: ≥300 Frames)

| Category | Target | Current Count | Progress | Status |
|---|---|---|---|---|
| Clear/dry/day | 100 | 145 | `[==========]` 145% | [COMPLETE] |
| Moderate traffic | 50 | 199 | `[==========]` 398% | [COMPLETE] |
| Heavy/congested | 50 | 181 | `[==========]` 362% | [COMPLETE] |
| Rain (any traffic) | 40 | 468 | `[==========]` 1170% | [COMPLETE] |
| Flood shallow | 20 | 98 | `[==========]` 490% | [COMPLETE] |
| Flood deep | 10 | 22 | `[==========]` 220% | [COMPLETE] |
| Night | 30 | 468 | `[==========]` 1560% | [COMPLETE] |

## 3. Traffic Classification Performance

| Class | Precision | Recall | F1-Score | Support |
|---|---|---|---|---|
| **CLEAR** | 0.537 | 0.719 | 0.615 | 384 |
| **MODERATE** | 0.389 | 0.317 | 0.349 | 199 |
| **HEAVY** | 0.000 | 0.000 | 0.000 | 108 |
| **CONGESTED** | 0.023 | 0.014 | 0.017 | 73 |
| *Macro Avg* | - | - | **0.245** | 764 |
| *Weighted Avg* | - | - | **0.402** | 764 |

### Traffic Confusion Matrix

| Actual \ Predicted | CLEAR | MODERATE | HEAVY | CONGESTED | Total |
|---|---|---|---|---|---|
| **CLEAR** | 276 | 66 | 19 | 23 | **384** |
| **MODERATE** | 94 | 63 | 24 | 18 | **199** |
| **HEAVY** | 94 | 12 | 0 | 2 | **108** |
| **CONGESTED** | 50 | 21 | 1 | 1 | **73** |

## 4. Flood Classification Performance

| Class | Precision | Recall | F1-Score | Support |
|---|---|---|---|---|
| **NONE** | 0.843 | 1.000 | 0.915 | 644 |
| **SHALLOW** | 0.000 | 0.000 | 0.000 | 98 |
| **DEEP** | 0.000 | 0.000 | 0.000 | 22 |
| *Macro Avg* | - | - | **0.305** | 764 |
| *Weighted Avg* | - | - | **0.771** | 764 |

### Flood Confusion Matrix

| Actual \ Predicted | NONE | SHALLOW | DEEP | Total |
|---|---|---|---|---|
| **NONE** | 644 | 0 | 0 | **644** |
| **SHALLOW** | 98 | 0 | 0 | **98** |
| **DEEP** | 22 | 0 | 0 | **22** |

## 5. Flood False-Negative Diagnostic (Missed Floods)

[WARN] **120 flood instances were missed by the analyzer.**

| Frame ID | Camera ID | Actual Flood | Predicted | Confidence | Weather | Time of Day | Notes |
|---|---|---|---|---|---|---|---|
| `cam_56de42f611f398ec0c481298_1791467067` | `56de42f611f398ec0c481298` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Võ Văn Kiệt - An Dương Vương 1 |
| `cam_56de42f611f398ec0c481298_1791467234` | `56de42f611f398ec0c481298` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Võ Văn Kiệt - An Dương Vương 1 |
| `cam_56de42f611f398ec0c481298_1791467404` | `56de42f611f398ec0c481298` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Võ Văn Kiệt - An Dương Vương 1 |
| `cam_56de42f611f398ec0c481299_1791467067` | `56de42f611f398ec0c481299` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Võ Văn Kiệt - An Dương Vương 2 |
| `cam_56de42f611f398ec0c481299_1791467230` | `56de42f611f398ec0c481299` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Võ Văn Kiệt - An Dương Vương 2 |
| `cam_56de42f611f398ec0c481299_1791467403` | `56de42f611f398ec0c481299` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Võ Văn Kiệt - An Dương Vương 2 |
| `cam_56de42f611f398ec0c48129a_1791467068` | `56de42f611f398ec0c48129a` | **DEEP** | `NONE` | 0.88 | heavy_rain | night | Deep flood inundation bottleneck causing stalled traffic and tailbacks on Võ Văn Kiệt - Hồ Học Lãm 1 |
| `cam_56de42f611f398ec0c48129a_1791467238` | `56de42f611f398ec0c48129a` | **DEEP** | `NONE` | 0.88 | heavy_rain | night | Deep flood inundation bottleneck causing stalled traffic and tailbacks on Võ Văn Kiệt - Hồ Học Lãm 1 |
| `cam_56de42f611f398ec0c48129a_1791467405` | `56de42f611f398ec0c48129a` | **DEEP** | `NONE` | 0.88 | heavy_rain | night | Deep flood inundation bottleneck causing stalled traffic and tailbacks on Võ Văn Kiệt - Hồ Học Lãm 1 |
| `cam_56de42f611f398ec0c48129b_1791467068` | `56de42f611f398ec0c48129b` | **DEEP** | `NONE` | 0.88 | heavy_rain | night | Deep flood inundation bottleneck causing stalled traffic and tailbacks on Võ Văn Kiệt - Hồ Học Lãm 2 |
| `cam_56de42f611f398ec0c48129b_1791467237` | `56de42f611f398ec0c48129b` | **DEEP** | `NONE` | 0.88 | heavy_rain | night | Deep flood inundation bottleneck causing stalled traffic and tailbacks on Võ Văn Kiệt - Hồ Học Lãm 2 |
| `cam_56de42f611f398ec0c48129b_1791467405` | `56de42f611f398ec0c48129b` | **DEEP** | `NONE` | 0.88 | heavy_rain | night | Deep flood inundation bottleneck causing stalled traffic and tailbacks on Võ Văn Kiệt - Hồ Học Lãm 2 |
| `cam_585b284ac3f96200127dc500_1791467252` | `585b284ac3f96200127dc500` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Huỳnh Tấn Phát - Lưu Trọng Lư 1 |
| `cam_585b284ac3f96200127dc500_1791467410` | `585b284ac3f96200127dc500` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Huỳnh Tấn Phát - Lưu Trọng Lư 1 |
| `cam_585b2980c3f96200127dc502_1791467235` | `585b2980c3f96200127dc502` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Huỳnh Tấn Phát - Lưu Trọng Lư 2 |
| `cam_585b2980c3f96200127dc502_1791467405` | `585b2980c3f96200127dc502` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Huỳnh Tấn Phát - Lưu Trọng Lư 2 |
| `cam_585b2b1ec3f96200127dc505_1791467234` | `585b2b1ec3f96200127dc505` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Nguyễn Văn Linh - Huỳnh Tấn Phát 1 |
| `cam_585b2b1ec3f96200127dc505_1791467404` | `585b2b1ec3f96200127dc505` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Nguyễn Văn Linh - Huỳnh Tấn Phát 1 |
| `cam_585b2baec3f96200127dc508_1791467232` | `585b2baec3f96200127dc508` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Nguyễn Văn Linh - Huỳnh Tấn Phát 2 |
| `cam_585b2baec3f96200127dc508_1791467404` | `585b2baec3f96200127dc508` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Nguyễn Văn Linh - Huỳnh Tấn Phát 2 |
| `cam_586e2995f9fab7001111b0b5_1791467235` | `586e2995f9fab7001111b0b5` | **DEEP** | `NONE` | 0.88 | heavy_rain | night | Deep flood inundation bottleneck causing stalled traffic and tailbacks on Ung Văn Khiêm - Nguyễn Gia Trí (D2) |
| `cam_586e2995f9fab7001111b0b5_1791467404` | `586e2995f9fab7001111b0b5` | **DEEP** | `NONE` | 0.88 | heavy_rain | night | Deep flood inundation bottleneck causing stalled traffic and tailbacks on Ung Văn Khiêm - Nguyễn Gia Trí (D2) |
| `cam_58df4e22dc195800111e04bb_1791467233` | `58df4e22dc195800111e04bb` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Yên Thế - Bạch Đằng |
| `cam_58df4e22dc195800111e04bb_1791467404` | `58df4e22dc195800111e04bb` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Yên Thế - Bạch Đằng |
| `cam_595dd3a63dcfc400106f28a7_1791467235` | `595dd3a63dcfc400106f28a7` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Quốc lộ 1 - Đường Tô Ngọc Vân 1 |
| `cam_595dd3a63dcfc400106f28a7_1791467405` | `595dd3a63dcfc400106f28a7` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Quốc lộ 1 - Đường Tô Ngọc Vân 1 |
| `cam_595dd4133dcfc400106f28a9_1791467241` | `595dd4133dcfc400106f28a9` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Quốc lộ 1 - Đường Tô Ngọc Vân 2 |
| `cam_595dd4133dcfc400106f28a9_1791467407` | `595dd4133dcfc400106f28a9` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Quốc lộ 1 - Đường Tô Ngọc Vân 2 |
| `cam_59ca31d602eb490011a0a40b_1791467257` | `59ca31d602eb490011a0a40b` | **DEEP** | `NONE` | 0.88 | heavy_rain | night | Deep flood inundation bottleneck causing stalled traffic and tailbacks on QL1 - Hồ Học Lãm |
| `cam_59ca31d602eb490011a0a40b_1791467411` | `59ca31d602eb490011a0a40b` | **DEEP** | `NONE` | 0.88 | heavy_rain | night | Deep flood inundation bottleneck causing stalled traffic and tailbacks on QL1 - Hồ Học Lãm |
| `cam_5a6065c58576340017d06615_1791467230` | `5a6065c58576340017d06615` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Tô Ngọc Vân - TX25 |
| `cam_5a6065c58576340017d06615_1791467403` | `5a6065c58576340017d06615` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Tô Ngọc Vân - TX25 |
| `cam_5a6069238576340017d0661c_1791467234` | `5a6069238576340017d0661c` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Bạch Đằng - Lê Quang Định |
| `cam_5a6069238576340017d0661c_1791467404` | `5a6069238576340017d0661c` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Bạch Đằng - Lê Quang Định |
| `cam_5a606a0f8576340017d0661e_1791467238` | `5a606a0f8576340017d0661e` | **DEEP** | `NONE` | 0.88 | heavy_rain | night | Deep flood inundation bottleneck causing stalled traffic and tailbacks on Nơ Trang Long - Nguyễn Xí |
| `cam_5a606a0f8576340017d0661e_1791467406` | `5a606a0f8576340017d0661e` | **DEEP** | `NONE` | 0.88 | heavy_rain | night | Deep flood inundation bottleneck causing stalled traffic and tailbacks on Nơ Trang Long - Nguyễn Xí |
| `cam_5a606a958576340017d06621_1791467250` | `5a606a958576340017d06621` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water combined with chronic corridor choke on Đinh Bộ Lĩnh - Bạch Đằng 3 |
| `cam_5a606a958576340017d06621_1791467410` | `5a606a958576340017d06621` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water combined with chronic corridor choke on Đinh Bộ Lĩnh - Bạch Đằng 3 |
| `cam_5a606cd08576340017d06628_1791467232` | `5a606cd08576340017d06628` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water combined with chronic corridor choke on BX Miền Đông - Đinh Bộ Lĩnh 2 |
| `cam_5a606cd08576340017d06628_1791467404` | `5a606cd08576340017d06628` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water combined with chronic corridor choke on BX Miền Đông - Đinh Bộ Lĩnh 2 |
| `cam_5a8253615058170011f6eabf_1791467257` | `5a8253615058170011f6eabf` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water combined with chronic corridor choke on Đinh Bộ Lĩnh - Bạch Đằng 1 |
| `cam_5a8253615058170011f6eabf_1791467411` | `5a8253615058170011f6eabf` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water combined with chronic corridor choke on Đinh Bộ Lĩnh - Bạch Đằng 1 |
| `cam_5a8253bc5058170011f6eac1_1791467257` | `5a8253bc5058170011f6eac1` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water combined with chronic corridor choke on Đinh Bộ Lĩnh - Bạch Đằng 2 |
| `cam_5a8253bc5058170011f6eac1_1791467411` | `5a8253bc5058170011f6eac1` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water combined with chronic corridor choke on Đinh Bộ Lĩnh - Bạch Đằng 2 |
| `cam_5a8254b05058170011f6eac3_1791467241` | `5a8254b05058170011f6eac3` | **DEEP** | `NONE` | 0.88 | heavy_rain | night | Deep flood inundation bottleneck causing stalled traffic and tailbacks on Xô Viết Nghệ Tĩnh - Nguyễn Xí 1 |
| `cam_5a8254b05058170011f6eac3_1791467407` | `5a8254b05058170011f6eac3` | **DEEP** | `NONE` | 0.88 | heavy_rain | night | Deep flood inundation bottleneck causing stalled traffic and tailbacks on Xô Viết Nghệ Tĩnh - Nguyễn Xí 1 |
| `cam_5a8254f25058170011f6eac5_1791467247` | `5a8254f25058170011f6eac5` | **DEEP** | `NONE` | 0.88 | heavy_rain | night | Deep flood inundation bottleneck causing stalled traffic and tailbacks on Xô Viết Nghệ Tĩnh - Nguyễn Xí 2 |
| `cam_5a8254f25058170011f6eac5_1791467409` | `5a8254f25058170011f6eac5` | **DEEP** | `NONE` | 0.88 | heavy_rain | night | Deep flood inundation bottleneck causing stalled traffic and tailbacks on Xô Viết Nghệ Tĩnh - Nguyễn Xí 2 |
| `cam_5a8255a55058170011f6eac7_1791467253` | `5a8255a55058170011f6eac7` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water combined with chronic corridor choke on BX Miền Đông - Đinh Bộ Lĩnh 1 |
| `cam_5a8255a55058170011f6eac7_1791467410` | `5a8255a55058170011f6eac7` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water combined with chronic corridor choke on BX Miền Đông - Đinh Bộ Lĩnh 1 |
| `cam_5a8256315058170011f6eac9_1791467235` | `5a8256315058170011f6eac9` | **DEEP** | `NONE` | 0.88 | heavy_rain | night | Deep flood inundation bottleneck causing stalled traffic and tailbacks on Đinh Bộ Lĩnh - Nguyễn Xí |
| `cam_5a8256315058170011f6eac9_1791467405` | `5a8256315058170011f6eac9` | **DEEP** | `NONE` | 0.88 | heavy_rain | night | Deep flood inundation bottleneck causing stalled traffic and tailbacks on Đinh Bộ Lĩnh - Nguyễn Xí |
| `cam_5d8cd653766c88001718894c_1791467256` | `5d8cd653766c88001718894c` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Kha Vạn Cân - Võ Văn Ngân |
| `cam_5d8cd653766c88001718894c_1791467410` | `5d8cd653766c88001718894c` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Kha Vạn Cân - Võ Văn Ngân |
| `cam_5d8cd98d766c88001718895a_1791467235` | `5d8cd98d766c88001718895a` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Phạm Văn Đồng - Tô Ngọc Vân (hướng đi Bình Dương) |
| `cam_5d8cd98d766c88001718895a_1791467404` | `5d8cd98d766c88001718895a` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Phạm Văn Đồng - Tô Ngọc Vân (hướng đi Bình Dương) |
| `cam_5d8cd9cb766c88001718895c_1791467235` | `5d8cd9cb766c88001718895c` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Phạm Văn Đồng - Tô Ngọc Vân (hướng đi Sân bay) |
| `cam_5d8cd9cb766c88001718895c_1791467405` | `5d8cd9cb766c88001718895c` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Phạm Văn Đồng - Tô Ngọc Vân (hướng đi Sân bay) |
| `cam_5d8cda2e766c88001718895e_1791467252` | `5d8cda2e766c88001718895e` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Phạm Văn Đồng - Kha Vạn Cân (hướng đi Quốc lộ 1) |
| `cam_5d8cda2e766c88001718895e_1791467410` | `5d8cda2e766c88001718895e` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Phạm Văn Đồng - Kha Vạn Cân (hướng đi Quốc lộ 1) |
| `cam_5d8cda6a766c880017188960_1791467231` | `5d8cda6a766c880017188960` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Phạm Văn Đồng - Kha Vạn Cân (hướng đi Sân bay) |
| `cam_5d8cda6a766c880017188960_1791467403` | `5d8cda6a766c880017188960` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Phạm Văn Đồng - Kha Vạn Cân (hướng đi Sân bay) |
| `cam_5d9ddec9766c880017188c9c_1791467231` | `5d9ddec9766c880017188c9c` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Nút giao Hàng Xanh 5 (Hàng Xanh - Bạch Đằng) |
| `cam_5d9ddec9766c880017188c9c_1791467403` | `5d9ddec9766c880017188c9c` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Nút giao Hàng Xanh 5 (Hàng Xanh - Bạch Đằng) |
| `cam_63b3c9bfbfd3d90017e9b039_1791467238` | `63b3c9bfbfd3d90017e9b039` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Kha Vạn Cân - Đường Số 2 |
| `cam_63b3c9bfbfd3d90017e9b039_1791467405` | `63b3c9bfbfd3d90017e9b039` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Kha Vạn Cân - Đường Số 2 |
| `cam_63b65f64bfd3d90017eaa41f_1791467249` | `63b65f64bfd3d90017eaa41f` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water combined with chronic corridor choke on Xô Viết Nghệ Tĩnh - Đường D5 |
| `cam_63b65f64bfd3d90017eaa41f_1791467409` | `63b65f64bfd3d90017eaa41f` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water combined with chronic corridor choke on Xô Viết Nghệ Tĩnh - Đường D5 |
| `cam_63b65f8dbfd3d90017eaa434_1791467256` | `63b65f8dbfd3d90017eaa434` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water combined with chronic corridor choke on Xô Viết Nghệ Tĩnh - Phan Văn Hân (Cầu Thị Nghè) |
| `cam_63b65f8dbfd3d90017eaa434_1791467410` | `63b65f8dbfd3d90017eaa434` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water combined with chronic corridor choke on Xô Viết Nghệ Tĩnh - Phan Văn Hân (Cầu Thị Nghè) |
| `cam_63bd1f21bfd3d90017ec3d04_1791467230` | `63bd1f21bfd3d90017ec3d04` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Huỳnh Tấn Phát - Phạm Hữu Lầu |
| `cam_63bd1f21bfd3d90017ec3d04_1791467403` | `63bd1f21bfd3d90017ec3d04` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Huỳnh Tấn Phát - Phạm Hữu Lầu |
| `cam_63bd1f48bfd3d90017ec3d19_1791467254` | `63bd1f48bfd3d90017ec3d19` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Huỳnh Tấn Phát - Nguyễn Thị Thập |
| `cam_63bd1f48bfd3d90017ec3d19_1791467410` | `63bd1f48bfd3d90017ec3d19` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Huỳnh Tấn Phát - Nguyễn Thị Thập |
| `cam_6623e7b76f998a001b25242d_1791467254` | `6623e7b76f998a001b25242d` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water combined with chronic corridor choke on Đinh Bộ Lĩnh - Bùi Đình Túy |
| `cam_6623e7b76f998a001b25242d_1791467410` | `6623e7b76f998a001b25242d` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water combined with chronic corridor choke on Đinh Bộ Lĩnh - Bùi Đình Túy |
| `cam_6623e7f06f998a001b25244a_1791467257` | `6623e7f06f998a001b25244a` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water combined with chronic corridor choke on Đinh Bộ Lĩnh - Chu Văn An |
| `cam_6623e7f06f998a001b25244a_1791467410` | `6623e7f06f998a001b25244a` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water combined with chronic corridor choke on Đinh Bộ Lĩnh - Chu Văn An |
| `cam_6623ee666f998a001b252726_1791467235` | `6623ee666f998a001b252726` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Quang Trung - Phan Huy Ích |
| `cam_6623ee666f998a001b252726_1791467404` | `6623ee666f998a001b252726` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Quang Trung - Phan Huy Ích |
| `cam_6623f3436f998a001b252863_1791467237` | `6623f3436f998a001b252863` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Phạm Văn Đồng - Kha Vạn Cân 1 |
| `cam_6623f3436f998a001b252863_1791467405` | `6623f3436f998a001b252863` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Phạm Văn Đồng - Kha Vạn Cân 1 |
| `cam_6623f36e6f998a001b252878_1791467231` | `6623f36e6f998a001b252878` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Phạm Văn Đồng - Kha Vạn Cân 2 |
| `cam_6623f36e6f998a001b252878_1791467403` | `6623f36e6f998a001b252878` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Phạm Văn Đồng - Kha Vạn Cân 2 |
| `cam_662a881a1afb9c00172d2559_1791467230` | `662a881a1afb9c00172d2559` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Kinh Dương Vương - Tên Lửa |
| `cam_662a881a1afb9c00172d2559_1791467403` | `662a881a1afb9c00172d2559` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Kinh Dương Vương - Tên Lửa |
| `cam_662a88521afb9c00172d2592_1791467247` | `662a88521afb9c00172d2592` | **DEEP** | `NONE` | 0.88 | heavy_rain | night | Deep flood inundation bottleneck causing stalled traffic and tailbacks on Kinh Dương Vương - Hồ Học Lãm |
| `cam_662a88521afb9c00172d2592_1791467409` | `662a88521afb9c00172d2592` | **DEEP** | `NONE` | 0.88 | heavy_rain | night | Deep flood inundation bottleneck causing stalled traffic and tailbacks on Kinh Dương Vương - Hồ Học Lãm |
| `cam_662a8c931afb9c00172d2901_1791467257` | `662a8c931afb9c00172d2901` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Huỳnh Tấn Phát - Hoàng Quốc Việt |
| `cam_662a8c931afb9c00172d2901_1791467410` | `662a8c931afb9c00172d2901` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Huỳnh Tấn Phát - Hoàng Quốc Việt |
| `cam_662a8cc51afb9c00172d2938_1791467235` | `662a8cc51afb9c00172d2938` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Huỳnh Tấn Phát - Phú Thuận |
| `cam_662a8cc51afb9c00172d2938_1791467404` | `662a8cc51afb9c00172d2938` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Huỳnh Tấn Phát - Phú Thuận |
| `cam_662a8cfe1afb9c00172d296e_1791467237` | `662a8cfe1afb9c00172d296e` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Huỳnh Tấn Phát - Trần Trọng Cung |
| `cam_662a8cfe1afb9c00172d296e_1791467405` | `662a8cfe1afb9c00172d296e` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Huỳnh Tấn Phát - Trần Trọng Cung |
| `cam_662a8e121afb9c00172d2a3f_1791467236` | `662a8e121afb9c00172d2a3f` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Huỳnh Tấn Phát - cầu Tân Thuận 1 |
| `cam_662a8e121afb9c00172d2a3f_1791467405` | `662a8e121afb9c00172d2a3f` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Huỳnh Tấn Phát - cầu Tân Thuận 1 |
| `cam_662a8ef41afb9c00172d2af2_1791467233` | `662a8ef41afb9c00172d2af2` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Lê Văn Lương - Đường số 15 |
| `cam_662a8ef41afb9c00172d2af2_1791467404` | `662a8ef41afb9c00172d2af2` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Lê Văn Lương - Đường số 15 |
| `cam_662a8f701afb9c00172d2b5e_1791467250` | `662a8f701afb9c00172d2b5e` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Nguyễn Thị Thập - Lê Văn Lương |
| `cam_662a8f701afb9c00172d2b5e_1791467410` | `662a8f701afb9c00172d2b5e` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Nguyễn Thị Thập - Lê Văn Lương |
| `cam_662b4d781afb9c00172d8571_1791467250` | `662b4d781afb9c00172d8571` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on An Dương Vương - Lê Hồng Phong |
| `cam_662b4d781afb9c00172d8571_1791467410` | `662b4d781afb9c00172d8571` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on An Dương Vương - Lê Hồng Phong |
| `cam_662b50c11afb9c00172d8843_1791467242` | `662b50c11afb9c00172d8843` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on An Dương Vương - Đường số 7 |
| `cam_662b50c11afb9c00172d8843_1791467408` | `662b50c11afb9c00172d8843` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on An Dương Vương - Đường số 7 |
| `cam_662b50e51afb9c00172d886a_1791467239` | `662b50e51afb9c00172d886a` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Bà Hom - An Dương Vương |
| `cam_662b50e51afb9c00172d886a_1791467406` | `662b50e51afb9c00172d886a` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Bà Hom - An Dương Vương |
| `cam_662b56c51afb9c00172d9071_1791467235` | `662b56c51afb9c00172d9071` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Bạch Đằng - Đặng Văn Sâm |
| `cam_662b56c51afb9c00172d9071_1791467405` | `662b56c51afb9c00172d9071` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Bạch Đằng - Đặng Văn Sâm |
| `cam_662b5adc1afb9c00172d925b_1791467233` | `662b5adc1afb9c00172d925b` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water combined with chronic corridor choke on Trường Chinh - Phan Huy Ích 1 |
| `cam_662b5adc1afb9c00172d925b_1791467404` | `662b5adc1afb9c00172d925b` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water combined with chronic corridor choke on Trường Chinh - Phan Huy Ích 1 |
| `cam_665861d864f11e00173c73a9_1791467244` | `665861d864f11e00173c73a9` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Huỳnh Tấn Phát - Cầu Tân Thuận 1 |
| `cam_665861d864f11e00173c73a9_1791467408` | `665861d864f11e00173c73a9` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on Huỳnh Tấn Phát - Cầu Tân Thuận 1 |
| `cam_66b1c1bf779f740018673ef2_1791467255` | `66b1c1bf779f740018673ef2` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on An Dương Vương - Trần Phú |
| `cam_66b1c1bf779f740018673ef2_1791467410` | `66b1c1bf779f740018673ef2` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on An Dương Vương - Trần Phú |
| `cam_66b1c1f2779f740018673f0d_1791467235` | `66b1c1f2779f740018673f0d` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on An Dương Vương - Nguyễn Văn Cừ |
| `cam_66b1c1f2779f740018673f0d_1791467405` | `66b1c1f2779f740018673f0d` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water restricting lanes and slowing traffic on An Dương Vương - Nguyễn Văn Cừ |
| `cam_66b1c3fa779f740018674146_1791467231` | `66b1c3fa779f740018674146` | **DEEP** | `NONE` | 0.88 | heavy_rain | night | Deep flood inundation bottleneck causing stalled traffic and tailbacks on Nguyễn Hữu Cảnh - Ung Văn Khiêm |
| `cam_66b1c3fa779f740018674146_1791467404` | `66b1c3fa779f740018674146` | **DEEP** | `NONE` | 0.88 | heavy_rain | night | Deep flood inundation bottleneck causing stalled traffic and tailbacks on Nguyễn Hữu Cảnh - Ung Văn Khiêm |
| `cam_692293c644912200174bc83b_1791467239` | `692293c644912200174bc83b` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water combined with chronic corridor choke on Quốc Lộ 51 - Xô Viết Nghệ Tĩnh |
| `cam_692293c644912200174bc83b_1791467406` | `692293c644912200174bc83b` | **SHALLOW** | `NONE` | 0.88 | rain | night | Shallow standing water combined with chronic corridor choke on Quốc Lộ 51 - Xô Viết Nghệ Tĩnh |

## 6. Sliced Analysis (Weather & Lighting)

### Weather Slice

| Weather Condition | Frames | Traffic Accuracy | Flood Accuracy |
|---|---|---|---|
| **dry** | 296 | 36.1% | 100.0% |
| **heavy_rain** | 22 | 0.0% | 0.0% |
| **rain** | 446 | 52.2% | 78.0% |

### Time of Day Slice

| Lighting / Time | Frames | Traffic Accuracy | Flood Accuracy |
|---|---|---|---|
| **day** | 296 | 36.1% | 100.0% |
| **night** | 468 | 49.8% | 74.4% |
