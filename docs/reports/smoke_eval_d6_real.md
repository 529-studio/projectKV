# Smoke-Test Camera Verification Report

- **Generated At**: 2026-10-09T19:46:32.057771+00:00
- **Total Cameras**: 10
- **Passed**: 5
- **Failed**: 5
- **Missing**: 0
- **Pass Rate**: 50.0%
- **Overall Verdict**: **FAIL**

## Detailed Results

| Camera ID | Name | Corridor | Expected | Actual | SI | Status | Details |
|---|---|---|:---:|:---:|:---:|:---:|---|
| `5822b5ededeb6c0012a2d66a` | Mai Chí Thọ - Nguyễn Cơ Thạch 1 | `mai_chi_tho` | CLEAR | CLEAR | 0.00 | **PASS** | - |
| `5a10c79f02eb490011a0b0dd` | Mai Chí Thọ - Tố Hữu (C6) | `mai_chi_tho` | CLEAR | CLEAR | 0.00 | **PASS** | - |
| `56de42f611f398ec0c48129c` | Võ Văn Kiệt - Quốc Lộ 1 | `vo_van_kiet` | CLEAR | CLEAR | 0.00 | **PASS** | - |
| `56de42f611f398ec0c481287` | Võ Văn Kiệt - Ký Con | `vo_van_kiet` | CLEAR | CLEAR | 0.00 | **PASS** | - |
| `5deb576d1dc17d7c5515ad02` | Điện Biên Phủ - Pasteur | `dien_bien_phu` | CLEAR | CLEAR | 0.00 | **PASS** | - |
| `5deb576d1dc17d7c5515ad08` | Cộng Hòa - Tân Kỳ Tân Quý | `cong_hoa` | CONGESTED | CLEAR | 0.28 | **FAIL** | Expected CONGESTED but got CLEAR (false-negative bottleneck detection); SI 0.28 < 0.70 congestion floor |
| `586e1f18f9fab7001111b0a5` | Cộng Hòa - Trường Chinh | `cong_hoa` | CONGESTED | CLEAR | 0.00 | **FAIL** | Expected CONGESTED but got CLEAR (false-negative bottleneck detection); SI 0.00 < 0.70 congestion floor |
| `66b1c426779f74001867415e` | Điện Biên Phủ - Nguyễn Gia Trí | `dien_bien_phu` | CONGESTED | CLEAR | 0.00 | **FAIL** | Expected CONGESTED but got CLEAR (false-negative bottleneck detection); SI 0.00 < 0.70 congestion floor |
| `5deb576d1dc17d7c5515acf2` | Điện Biên Phủ - Cách Mạng Tháng Tám | `dien_bien_phu` | CONGESTED | CLEAR | 0.00 | **FAIL** | Expected CONGESTED but got CLEAR (false-negative bottleneck detection); SI 0.00 < 0.70 congestion floor |
| `56de42f611f398ec0c481282` | Mai Chí Thọ - Đồng Văn Cống 2 | `mai_chi_tho` | CONGESTED | CLEAR | 0.00 | **FAIL** | Expected CONGESTED but got CLEAR (false-negative bottleneck detection); SI 0.00 < 0.70 congestion floor |

## 2. Comparison with Pre-T5 Baseline (`smoke_eval_d6_pre_t5.md`)

| Smoke Camera | Corridor | Expected | Pre-T5 (T7 Snapshot) | Post-T5 (D6 Dataset Run) | Root Cause & Temporal Window Analysis |
|---|---|:---:|:---:|:---:|---|
| Mai Chí Thọ - Nguyễn Cơ Thạch 1 | `mai_chi_tho` | CLEAR | HEAVY (SI 0.70) | CLEAR (SI 0.00) [PASS] | Pre-T5 snapshot was sampled during off-peak with moving platoons; latest dataset frame was late night empty street. |
| Mai Chí Thọ - Tố Hữu (C6) | `mai_chi_tho` | CLEAR | MODERATE (SI 0.51) | CLEAR (SI 0.00) [PASS] | Pre-T5 had false detections; late night frame has 0 vehicles detected. |
| Võ Văn Kiệt - Quốc Lộ 1 | `vo_van_kiet` | CLEAR | CLEAR (SI 0.00) | CLEAR (SI 0.00) [PASS] | Free-flowing section consistently detected as CLEAR. |
| Võ Văn Kiệt - Ký Con | `vo_van_kiet` | CLEAR | HEAVY (SI 0.84) | CLEAR (SI 0.00) [PASS] | Pre-T5 had severe vehicle hallucination at dusk; late night frame is empty. |
| Điện Biên Phủ - Pasteur | `dien_bien_phu` | CLEAR | CONGESTED (SI 1.00) | CLEAR (SI 0.00) [PASS] | Pre-T5 suffered background lighting hallucination; late night frame has 0 detections. |
| Cộng Hòa - Tân Kỳ Tân Quý | `cong_hoa` | CONGESTED | MODERATE (SI 0.40) | CLEAR (SI 0.28) [FAIL] | Chronic bottleneck queue missed due to occlusion and rain distortion. |
| Cộng Hòa - Trường Chinh | `cong_hoa` | CONGESTED | CLEAR (SI 0.00) | CLEAR (SI 0.00) [FAIL] | **Zero-detection blind spot**: Bumper-to-bumper queue under flyover completely missed by 320px model. |
| Điện Biên Phủ - Nguyễn Gia Trí | `dien_bien_phu` | CONGESTED | CONGESTED (SI 1.00) | CLEAR (SI 0.00) [FAIL] | Pre-T5 peak snapshot detected queue; late night frame was empty. |
| Điện Biên Phủ - Cách Mạng Tháng Tám | `dien_bien_phu` | CONGESTED | CLEAR (SI 0.32) | CLEAR (SI 0.00) [FAIL] | Nighttime frame missed stopped queue entirely. |
| Mai Chí Thọ - Đồng Văn Cống 2 | `mai_chi_tho` | CONGESTED | CLEAR (SI 0.00) | CLEAR (SI 0.00) [FAIL] | **Zero-detection blind spot**: Heavy container trucks and dense queue missed at intersection entrance. |

### Diagnostic Summary:
- **Pass Rate**: 5/10 (50.0%) vs 2/10 (20.0%) pre-T5.
- However, all 5 passes occurred because the test frames evaluated for CLEAR cameras happened to have 0 vehicle detections (empty late-night conditions).
- For all 5 CONGESTED cameras, **0 out of 5 passed** (100% failure rate). Bumper-to-bumper congestion queues were completely missed (4 cameras reporting $SI = 0.00$ and 1 reporting $SI = 0.28$).
- This confirms the S5-T2 hypothesis: The model suffers from severe object detection blindness under congestion queues, which parameter tuning cannot resolve.

