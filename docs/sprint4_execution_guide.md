# Sprint 4 — Hướng dẫn Vận hành & Thực thi (Execution Guide)

> **Tài liệu tham chiếu chuẩn**: [ADR-005](ADR-005-sprint3-audit-and-sprint4-plan.md) (Sprint 3 Audit & Sprint 4 Plan), [GO_BACKEND_ARCHITECTURE.md](GO_BACKEND_ARCHITECTURE.md), [AGENTS.md](../AGENTS.md).  
> **Nguyên tắc cốt lõi**: Không tính năng mới, đóng toàn bộ khoảng cách giữa báo cáo và thực tế (Closing report vs. reality gap). Bằng chứng thực nghiệm là điều kiện tiên quyết trước mọi tuyên bố hoàn thành.

---

## 1. Bản đồ Sprint 4 Backlog & Trạng thái Thực tế

| Wave | ID | GitHub Issue | Tiêu đề | Phụ thuộc | Trạng thái hiện tại |
|:---:|:---:|:---:|---|---|:---:|
| **Wave 0** | **T8** | [#188](https://github.com/529-studio/projectKV/issues/188) / [PR #190](https://github.com/529-studio/projectKV/pull/190) | `[docs] add mandatory live-production-check to Definition of Done templates` | None | **MERGED (`dev`)** |
| **Wave 0** | **T6** | [#186](https://github.com/529-studio/projectKV/issues/186) / [PR #192](https://github.com/529-studio/projectKV/pull/192) | `[go-server + cv-engine] resolve container-class detection gap` | T1 (Done) | **MERGED (`dev`)** |
| **Wave 0** | **T7** | [#187](https://github.com/529-studio/projectKV/issues/187) / [PR #193](https://github.com/529-studio/projectKV/pull/193) | `[spike] build known-state camera smoke-test set for model verification` | T2 (Done) | **ĐÃ HOÀN TẤT (PR #193 Passing CI)** |
| **Gate T2b** | **T2b** | [#182](https://github.com/529-studio/projectKV/issues/182) / [PR #185](https://github.com/529-studio/projectKV/pull/185) | `[go-server] T2b — archiver saves the actual analyzed frame, not a coincidental proxy-cache hit` | T2 (Done) | **MERGED & XÁC NHẬN BẰNG CHỨNG STAGING (ĐÃ MỞ KHÓA)** |
| **Wave 1** | **T3** | [#194](https://github.com/529-studio/projectKV/issues/194) | `[tooling] label real flood + traffic dataset via S6 labeler` | T2b (Đã mở khóa) | **SẴN SÀNG THỰC THI (ĐANG TÍCH LŨY DỮ LIỆU)** |
| **Wave 2** | **T4** | [#195](https://github.com/529-studio/projectKV/issues/195) | `[tooling] run real D6 accuracy gate, report precision/recall/FNR` | T3, T7 | **Đã khởi tạo (Chờ tập nhãn T3)** |
| **Wave 3** | **T5** | [#196](https://github.com/529-studio/projectKV/issues/196) | `[go-server] calibrate PCU weights against labeled traffic data` | T3, T4 | **Đã khởi tạo (Chờ số liệu T4)** |
| **Wave 3** | **T9** | [#197](https://github.com/529-studio/projectKV/issues/197) | `[decision] keep self-hosted OpenVINO or revert to VLM for traffic` | T4 | **Đã khởi tạo (Chờ số liệu T4)** |

---

## 2. Gate T2b: Kết quả Nghiệm thu Thực tế trên Staging

Căn cứ comment nghiệm thu tại [PR #185](https://github.com/529-studio/projectKV/pull/185#issuecomment-6056548912) trên máy chủ Staging (`local-homesrv` / Portainer Stack 14), **Gate T2b đã chính thức ĐẠT và MỞ KHÓA hoàn toàn**:

1. **Tốc độ tích lũy frame thực tế**:
   - Tích lũy trong ngày 2026-10-08: **1.600+ frames** và tăng liên tục theo chu kỳ Hot Set ~30s.
   - Cửa sổ đo 30 phút (`-mmin -30`): **240+ frames**.
   - Cửa sổ đo 60 phút (`-mmin -60`): **733+ frames**.
   - Chu kỳ lấy mẫu định kỳ 5 phút (`_sample.jpg`) và lưu ảnh theo biến động trạng thái (`_state_change.jpg`) hoạt động hoàn toàn độc lập với việc có người mở web hay không.
2. **Kích thước và tính toàn vẹn của file ảnh**:
   - Dung lượng: **36 KB – 65 KB** (trung bình ~55 KB/frame). Toàn bộ mock placeholder 107 bytes đã bị triệt tiêu hoàn toàn.
   - Magic bytes: Xác nhận chuẩn **JFIF** hợp lệ (`FF D8 FF E0 00 10 4A 46 49 46`).
3. **Bằng chứng lưu trữ máy chủ**:
   - Ảnh chụp volume browser Termix tại thư mục `/var/lib/docker/volumes/warded-stg_warded-stg-data/_data/proofs/2026-10-08/` xác nhận ảnh thật đổ về liên tục từ 12:46 đến 15:59.

---

## 3. Hướng dẫn Thực thi Wave 1: Issue T3 (#194)

### 3.1. Mục tiêu & Nguyên tắc Bắt buộc
- **Mục tiêu**: Xây dựng tập nhãn ground-truth $\ge 300$ frames thật, trong đó $\ge 60$ frames ngập (shallow + deep) và $\ge 30$ frames ban đêm, theo đúng tiêu chuẩn ADR-004 §D6.
- **Kỷ luật dữ liệu**: **Tuyệt đối không dùng** bất kỳ file ảnh nào trước ngày 2026-10-08 (đã xác nhận là mock frame 107 bytes). Chỉ lấy ảnh thực tế từ `data/proofs/2026-10-08/` trở đi.
- **Lưu ý thời gian thụ động**: 
  - Gán nhãn các trạng thái giao thông (`CLEAR`, `MODERATE`, `HEAVY`, `CONGESTED`, `night`, `day`) có thể tiến hành ngay trên các frame đã tích lũy trong ngày (đã có >1.600 frame).
  - Riêng $\ge 60$ frame ngập nước (`SHALLOW`, `DEEP`) cần thời gian chờ thụ động qua các cơn mưa thực tế tại TP.HCM trong các ngày tiếp theo. Không tự ý bịa đặt nhãn ngập trên ảnh đường khô ráo.

### 3.2. Quy trình Gán nhãn bằng `labeler.html`
1. **Lấy ảnh từ Staging về máy local**:
   ```bash
   # Tải kho ảnh ngày 2026-10-08 từ staging về thư mục data/proofs/
   # Hoặc đồng bộ qua rsync / scp từ máy chủ local-homesrv
   mkdir -p data/proofs/2026-10-08
   ```
2. **Mở công cụ gán nhãn**:
   ```bash
   open tools/eval/labeler.html        # macOS
   xdg-open tools/eval/labeler.html   # Linux
   ```
3. **Thao tác gán nhãn bằng phím tắt**:
   - Nhấn **Open Folder** ➔ Chọn thư mục frame (`data/proofs/2026-10-08/` hoặc thư mục con theo camera).
   - Phím ngập nước (bắt buộc): **1** (NONE), **2** (SHALLOW), **3** (DEEP).
   - Phím giao thông: **Q** (CLEAR), **W** (MODERATE), **E** (HEAVY), **R** (CONGESTED).
   - Phím thời tiết: **A** (dry), **S** (rain), **D** (heavy_rain).
   - Phím thời gian: **Z** (day), **X** (night), **C** (dawn_dusk).
   - Phím **Enter**: Lưu nhãn và tự động chuyển sang frame tiếp theo.
4. **Đồng bộ nhãn vào repository**:
   - Sao chép các file JSON sinh ra vào `tools/eval/labels/<frame_id>.json`.
   - Kiểm tra tính hợp lệ của nhãn:
     ```bash
     python3 -m unittest discover -s tools/eval -p "*_test.py"
     ```

---

## 4. Hướng dẫn Thực thi Wave 2: Issue T4 (#195)

Ngay sau khi T3 hoàn thành đủ $\ge 300$ frames thật:
1. **Chạy benchmark D6 chính thức**:
   ```bash
   python3 tools/eval/benchmark.py --analyzer onnx --labels-dir tools/eval/labels --output-report docs/reports/eval_report_d6_real.md
   ```
2. **Đối chiếu với bộ Smoke-Test T7**:
   ```bash
   python3 tools/eval/verify_smoke_set.py --verdicts predictions_real.json --strict
   ```
3. **Công bố kết quả Gate D6**:
   - Ghi nhận con số thực tế: Precision, Recall, F1, và đặc biệt là **Flood False-Negative Rate (FNR)**.
   - Tiêu chuẩn an toàn: Nếu $FNR \le 5.0\%$, Gate D6 chính thức **PASS**.

---

## 5. Hướng dẫn Thực thi Wave 3: T5 (#196) & T9 (#197)

1. **Issue T5 — Hiệu chỉnh trọng số PCU**:
   - Phân tích phân phối thực tế từ T3/T4 để hiệu chỉnh các trọng số trong `services/go-server/internal/cvclient/pcu.go`.
   - Lập tài liệu so sánh trọng số cũ vs mới và công thức hiệu chuẩn toán học.
   - Chạy `make test-pcu` và `make test-go` xác nhận toàn vẹn.
2. **Issue T9 — Quyết định Giữ OpenVINO hay Revert VLM**:
   - Dựa trên con số thật từ T4, lập biên bản quyết định kiến trúc tại `docs/decision-openvino-vs-vlm.md`.
   - Cập nhật [ADR-005](ADR-005-sprint3-audit-and-sprint4-plan.md) và lộ trình Sprint 5.
