# Smoke-Test Camera Verification Report

- **Generated At**: 2026-10-08T13:19:59.592300+00:00
- **Total Cameras**: 10
- **Passed**: 2
- **Failed**: 8
- **Missing**: 0
- **Pass Rate**: 20.0%
- **Overall Verdict**: **FAIL**

## Detailed Results

| Camera ID | Name | Corridor | Expected | Actual | SI | Status | Details |
|---|---|---|:---:|:---:|:---:|:---:|---|
| `5822b5ededeb6c0012a2d66a` | Mai Chí Thọ - Nguyễn Cơ Thạch 1 | `mai_chi_tho` | CLEAR | HEAVY | 0.70 | **FAIL** | Expected CLEAR but got HEAVY (potential false-positive congestion); SI 0.70 >= 0.35 clear ceiling |
| `5a10c79f02eb490011a0b0dd` | Mai Chí Thọ - Tố Hữu (C6) | `mai_chi_tho` | CLEAR | MODERATE | 0.51 | **FAIL** | Expected CLEAR but got MODERATE (potential false-positive congestion); SI 0.51 >= 0.35 clear ceiling |
| `56de42f611f398ec0c48129c` | Võ Văn Kiệt - Quốc Lộ 1 | `vo_van_kiet` | CLEAR | CLEAR | 0.00 | **PASS** | - |
| `56de42f611f398ec0c481287` | Võ Văn Kiệt - Ký Con | `vo_van_kiet` | CLEAR | HEAVY | 0.84 | **FAIL** | Expected CLEAR but got HEAVY (potential false-positive congestion); SI 0.84 >= 0.35 clear ceiling |
| `5deb576d1dc17d7c5515ad02` | Điện Biên Phủ - Pasteur | `dien_bien_phu` | CLEAR | CONGESTED | 1.00 | **FAIL** | Expected CLEAR but got CONGESTED (potential false-positive congestion); SI 1.00 >= 0.35 clear ceiling |
| `5deb576d1dc17d7c5515ad08` | Cộng Hòa - Tân Kỳ Tân Quý | `cong_hoa` | CONGESTED | MODERATE | 0.40 | **FAIL** | Expected CONGESTED but got MODERATE (false-negative bottleneck detection); SI 0.40 < 0.70 congestion floor |
| `586e1f18f9fab7001111b0a5` | Cộng Hòa - Trường Chinh | `cong_hoa` | CONGESTED | CLEAR | 0.00 | **FAIL** | Expected CONGESTED but got CLEAR (false-negative bottleneck detection); SI 0.00 < 0.70 congestion floor |
| `66b1c426779f74001867415e` | Điện Biên Phủ - Nguyễn Gia Trí | `dien_bien_phu` | CONGESTED | CONGESTED | 1.00 | **PASS** | - |
| `5deb576d1dc17d7c5515acf2` | Điện Biên Phủ - Cách Mạng Tháng Tám | `dien_bien_phu` | CONGESTED | CLEAR | 0.32 | **FAIL** | Expected CONGESTED but got CLEAR (false-negative bottleneck detection); SI 0.32 < 0.70 congestion floor |
| `56de42f611f398ec0c481282` | Mai Chí Thọ - Đồng Văn Cống 2 | `mai_chi_tho` | CONGESTED | CLEAR | 0.00 | **FAIL** | Expected CONGESTED but got CLEAR (false-negative bottleneck detection); SI 0.00 < 0.70 congestion floor |
