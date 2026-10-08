# Known-State Smoke-Test Camera Set (Sprint 4 T7 / ADR-005 Finding B)

This document specifies the curated set of 10 traffic monitoring cameras in Ho Chi Minh City with deterministic baseline states, designed for objective verification of AI model inference and PCU saturation scoring without relying on anecdotal spot-checks.

---

## 1. Context & Purpose

Historically in ProjectKV, model verification suffered from subjective, anecdotal observations (e.g., inspecting random staging UI feeds without timestamp synchronization). This led to undetected regressions where empty streets were erroneously flagged as `CONGESTED` due to inaccurate capacity thresholds or misclassified reflections.

To establish an **objective, repeatable ground-truth harness** ahead of the full D6 precision/recall benchmark (Sprint 4 T4), this smoke set defines:
1. **5 High-Capacity Free-Flow Baselines (`CLEAR`)**: Wide boulevards and grade-separated intersections where off-peak daytime volume is consistently free-flowing ($SI < 0.35$). If a model flags these as `CONGESTED` or `HEAVY` during designated off-peak windows, a false-positive regression is immediately identified.
2. **5 Chronic Bottleneck Baselines (`CONGESTED`)**: Critical chokepoints and freight corridors with well-documented gridlock during morning and evening rush hours ($SI \ge 0.90$). If a model classifies these as `CLEAR` during peak rush hour windows, a false-negative regression is surfaced.

---

## 2. Selection Criteria

All 10 cameras satisfy the following strict criteria:
- **Hot Set Inclusion**: Part of the 57 priority corridor intersections (`len(road_keys) == 2`) defined in Sprint 3 S1/S2 (`vo_van_kiet`, `mai_chi_tho`, `dien_bien_phu`, `cong_hoa`).
- **Catalog Liveness**: Confirmed `status: UP` and `isOnline: true` in `cameras_enriched.json` with active DOT upstream snapshot delivery.
- **Physical Road Geometry**: Wide multi-lane profiles for `CLEAR` baselines, and constrained converging junctions with high signal cycle latency for `CONGESTED` baselines.
- **Corridor Balance**: Distributed across all 4 monitored arteries (2 on Võ Văn Kiệt, 3 on Mai Chí Thọ, 3 on Điện Biên Phủ, 2 on Cộng Hòa).

---

## 3. Curated Smoke-Test Cameras Summary

| # | Camera ID | Display Name | Corridor | Baseline State | Target Window (GMT+7) | Key Metric Target |
|---|---|---|---|:---:|---|:---:|
| 1 | `5822b5ededeb6c0012a2d66a` | Mai Chí Thọ - Nguyễn Cơ Thạch 1 | `mai_chi_tho` | `CLEAR` | 13:00 - 15:00 | $SI < 0.35$ |
| 2 | `5a10c79f02eb490011a0b0dd` | Mai Chí Thọ - Tố Hữu (C6) | `mai_chi_tho` | `CLEAR` | 13:00 - 15:00 | $SI < 0.35$ |
| 3 | `56de42f611f398ec0c48129c` | Võ Văn Kiệt - Quốc Lộ 1 | `vo_van_kiet` | `CLEAR` | 13:00 - 15:00 | $SI < 0.35$ |
| 4 | `56de42f611f398ec0c481287` | Võ Văn Kiệt - Ký Con | `vo_van_kiet` | `CLEAR` | 13:00 - 15:00 | $SI < 0.35$ |
| 5 | `5deb576d1dc17d7c5515ad02` | Điện Biên Phủ - Pasteur | `dien_bien_phu` | `CLEAR` | 13:00 - 15:00 | $SI < 0.35$ |
| 6 | `5deb576d1dc17d7c5515ad08` | Cộng Hòa - Tân Kỳ Tân Quý | `cong_hoa` | `CONGESTED` | 07:30-08:30 / 17:30-18:30 | $SI \ge 0.90$ |
| 7 | `586e1f18f9fab7001111b0a5` | Cộng Hòa - Trường Chinh | `cong_hoa` | `CONGESTED` | 07:30-08:30 / 17:30-18:30 | $SI \ge 0.90$ |
| 8 | `66b1c426779f74001867415e` | Điện Biên Phủ - Nguyễn Gia Trí | `dien_bien_phu` | `CONGESTED` | 17:30 - 18:30 | $SI \ge 0.90$ |
| 9 | `5deb576d1dc17d7c5515acf2` | Điện Biên Phủ - Cách Mạng Tháng Tám | `dien_bien_phu` | `CONGESTED` | 17:30 - 18:30 | $SI \ge 0.90$ |
| 10 | `56de42f611f398ec0c481282` | Mai Chí Thọ - Đồng Văn Cống 2 | `mai_chi_tho` | `CONGESTED` | 16:30 - 18:30 | $SI \ge 0.90$ |

---

## 4. Camera Profiles & Physical Rationale

### Group A: Free-Flow Baselines (`CLEAR`)

#### 1. Mai Chí Thọ - Nguyễn Cơ Thạch 1 (`5822b5ededeb6c0012a2d66a`)
- **Corridor**: `mai_chi_tho` (Thủ Thiêm new urban center)
- **Geometry**: 12-lane grade-separated divided boulevard with dedicated service roads.
- **Validation Window**: 13:00 - 15:00 GMT+7 (06:00 - 08:00 UTC).
- **Physical Rationale**: Exceptionally high geometric capacity. Midday off-peak volume flows at corridor speed limits (60–80 km/h) with virtually no queuing. False positives here typically indicate shadow misclassification or over-sensitive vehicle bounding boxes.

#### 2. Mai Chí Thọ - Tố Hữu (C6) (`5a10c79f02eb490011a0b0dd`)
- **Corridor**: `mai_chi_tho`
- **Geometry**: Wide multi-lane arterial crossing Tố Hữu.
- **Validation Window**: 13:00 - 15:00 GMT+7 (06:00 - 08:00 UTC).
- **Physical Rationale**: Unobstructed camera elevation and low roadside commercial activity. Continuous flow during off-peak hours with negligible vehicle queues.

#### 3. Võ Văn Kiệt - Quốc Lộ 1 (`56de42f611f398ec0c48129c`)
- **Corridor**: `vo_van_kiet` (Southwestern gateway)
- **Geometry**: High-speed highway interchange with separate multi-lane ramps.
- **Validation Window**: 13:00 - 15:00 GMT+7 (06:00 - 08:00 UTC).
- **Physical Rationale**: Free-flowing traffic disperses rapidly into National Route 1. Outside holiday migration waves, off-peak traffic density is consistently below 30% saturation.

#### 4. Võ Văn Kiệt - Ký Con (`56de42f611f398ec0c481287`)
- **Corridor**: `vo_van_kiet` (District 1 canal bank)
- **Geometry**: Wide straight boulevard immediately before the Saigon River Tunnel portal.
- **Validation Window**: 13:00 - 15:00 GMT+7 (06:00 - 08:00 UTC).
- **Physical Rationale**: Signal priority keeps traffic moving smoothly towards the tunnel. Off-peak density is low, making this a reliable baseline for clear conditions.

#### 5. Điện Biên Phủ - Pasteur (`5deb576d1dc17d7c5515ad02`)
- **Corridor**: `dien_bien_phu` (District 3 arterial)
- **Geometry**: Wide one-way 4-lane urban arterial.
- **Validation Window**: 13:00 - 15:00 GMT+7 (06:00 - 08:00 UTC).
- **Physical Rationale**: Green wave signal progression provides continuous platoon movement during midday hours without congestion stacking.

---

### Group B: Chronic Bottleneck Baselines (`CONGESTED`)

#### 6. Cộng Hòa - Tân Kỳ Tân Quý (`5deb576d1dc17d7c5515ad08`)
- **Corridor**: `cong_hoa` (Tân Bình airport corridor)
- **Geometry**: Narrowing intersection where Tân Kỳ Tân Quý feeds dense traffic from District 12 and Bình Tân into Cộng Hòa.
- **Validation Window**: Morning peak 07:30 - 08:30 GMT+7 / Evening peak 17:30 - 18:30 GMT+7.
- **Physical Rationale**: Notorious city bottleneck. Signal cycle capacity is severely exceeded during rush hours, causing bumper-to-bumper standstill traffic with $SI > 1.0$.

#### 7. Cộng Hòa - Trường Chinh (`586e1f18f9fab7001111b0a5`)
- **Corridor**: `cong_hoa` / `truong_chinh` (Northwest gateway)
- **Geometry**: Massive multi-way convergence point.
- **Validation Window**: Morning peak 07:30 - 08:30 GMT+7 / Evening peak 17:30 - 18:30 GMT+7.
- **Physical Rationale**: Merging commuter flows from An Sương flyover and Trường Chinh choke into Cộng Hòa. Consistently locked in heavy gridlock during peak windows.

#### 8. Điện Biên Phủ - Nguyễn Gia Trí (`66b1c426779f74001867415e`)
- **Corridor**: `dien_bien_phu` (Bình Thạnh district)
- **Geometry**: Intersection between Hàng Xanh roundabout and Saigon Bridge.
- **Validation Window**: Evening peak 17:30 - 18:30 GMT+7.
- **Physical Rationale**: University district traffic (HUTECH, Giao Thông Vận Tải) combined with homeward commuter streams to Thủ Đức creates multi-hundred-meter tailbacks.

#### 9. Điện Biên Phủ - Cách Mạng Tháng Tám (`5deb576d1dc17d7c5515acf2`)
- **Corridor**: `dien_bien_phu` (District 3 / 10 boundary)
- **Geometry**: High-volume crossroads with tight turning lanes.
- **Validation Window**: Evening peak 17:30 - 18:30 GMT+7.
- **Physical Rationale**: Saturated cross-traffic delays clearance across multiple light cycles, resulting in chronic evening stop-and-go congestion.

#### 10. Mai Chí Thọ - Đồng Văn Cống 2 (`56de42f611f398ec0c481282`)
- **Corridor**: `mai_chi_tho` (Port freight axis)
- **Geometry**: Direct turn-off into Đồng Văn Cống towards Tân Cảng Cát Lái.
- **Validation Window**: Afternoon/evening freight gate-in 16:30 - 18:30 GMT+7.
- **Physical Rationale**: Heavy container trucks and commercial tractor-trailers queue across 3 to 4 lanes waiting for port gate entry, producing sustained severe congestion ($SI \ge 0.90$).

---

## 5. Automated Verification Tooling

The verification script `tools/eval/verify_smoke_set.py` validates model outputs or stored verdicts against this specification:

```bash
# Verify precomputed verdict JSON against smoke baseline
python3 tools/eval/verify_smoke_set.py --verdicts verdicts.json --strict

# Run dry-run mock verification in CI
python3 tools/eval/verify_smoke_set.py --mock --strict

# Query live backend API on staging
python3 tools/eval/verify_smoke_set.py --backend-url http://localhost:3000
```

Discrepancies are reported per camera with expected vs. actual status, saturation index, and diagnostic hints.
