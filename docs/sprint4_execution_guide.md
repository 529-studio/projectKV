# Sprint 4 — Hướng dẫn Vận hành & Thực thi (Execution Guide)

> **Tài liệu tham chiếu chuẩn**: [ADR-005](ADR-005-sprint3-audit-and-sprint4-plan.md) (Sprint 3 Audit & Sprint 4 Plan), [GO_BACKEND_ARCHITECTURE.md](GO_BACKEND_ARCHITECTURE.md), [AGENTS.md](../AGENTS.md).  
> **Nguyên tắc cốt lõi**: Không tính năng mới, đóng toàn bộ khoảng cách giữa báo cáo và thực tế (Closing report vs. reality gap). Bằng chứng thực nghiệm là điều kiện tiên quyết trước mọi tuyên bố hoàn thành.

---

## 1. Bản đồ Sprint 4 Backlog & Trạng thái Khởi tạo

| Wave | ID | GitHub Issue | Tiêu đề | Phụ thuộc | Trạng thái hiện tại |
|:---:|:---:|:---:|---|---|:---:|
| **Wave 0** | **T8** | [#188](https://github.com/529-studio/projectKV/issues/188) | `[docs] add mandatory live-production-check to Definition of Done templates` | None | **Sẵn sàng chạy ngay** |
| **Wave 0** | **T6** | [#186](https://github.com/529-studio/projectKV/issues/186) | `[go-server + cv-engine] resolve container-class detection gap` | T1 (Done) | **Sẵn sàng chạy ngay** |
| **Wave 0** | **T7** | [#187](https://github.com/529-studio/projectKV/issues/187) | `[spike] build known-state camera smoke-test set for model verification` | T2 (Done) | **Sẵn sàng chạy ngay** |
| **Gate T2b** | **T2b** | [#182](https://github.com/529-studio/projectKV/issues/182) / [PR #185](https://github.com/529-studio/projectKV/pull/185) | `[go-server] T2b — archiver saves the actual analyzed frame, not a coincidental proxy-cache hit` | T2 (Done) | **Đã merge `dev`, ĐANG CHỜ BẰNG CHỨNG STAGING (30 phút)** |
| **Wave 1** | **T3** | *(Chờ mở khóa)* | `[tooling] label real flood + traffic dataset via S6 labeler` | T2b (Cần bằng chứng tích lũy) | **BỊ KHÓA bởi Gate T2b** |
| **Wave 2** | **T4** | *(Chờ mở khóa)* | `[tooling] run real D6 accuracy gate, report precision/recall/FNR` | T3, T7 | **BỊ KHÓA bởi T3** |
| **Wave 3** | **T5** | *(Chờ mở khóa)* | `[go-server] calibrate PCU weights against labeled traffic data` | T3, T4 | **BỊ KHÓA bởi T4** |
| **Wave 3** | **T9** | *(Chờ mở khóa)* | `[decision] keep self-hosted OpenVINO or revert to VLM for traffic` | T4 | **BỊ KHÓA bởi T4** |

---

## 2. Gate T2b: Thủ tục Xác nhận Bằng chứng Staging Thực tế

Trước khi tạo và kích hoạt **T3**, đội ngũ phải thực hiện xác nhận thực tế trên máy chủ Staging (`local-homesrv` / Portainer Stack 14) theo đúng quy định tại ADR-005 §5 và Issue #182.

### Các bước kiểm tra trên Staging

1. **Kiểm tra Container đã nhận Image mới chứa PR #185**:
   ```bash
   # Kiểm tra commit hash hoặc thời gian container start
   docker inspect warded-backend --format '{{.State.StartedAt}}'
   ```

2. **Đo đạc tốc độ tích lũy frame trong `/app/data/proofs` trong 30 phút**:
   - Với 57 camera trong Hot Set, chu kỳ lấy mẫu 30s (`ARCHIVE_SAMPLE_INTERVAL_SECONDS=300` tương đương mỗi camera được lưu 1 frame mỗi 5 phút = 12 frame/giờ/camera):
   - Dự kiến tối thiểu trong 30 phút: $57 \times 6 = 342$ frames mới (cộng thêm các frame do `state_change`).
   ```bash
   # Lần đo T0 (bắt đầu):
   BEFORE_COUNT=$(docker exec warded-backend find /app/data/proofs -type f -name "*.jpg" | wc -l)
   echo "Count at T0: $BEFORE_COUNT"

   # Đợi 30 phút quan sát thụ động (không cần mở web client)...
   sleep 1800

   # Lần đo T30:
   AFTER_COUNT=$(docker exec warded-backend find /app/data/proofs -type f -name "*.jpg" | wc -l)
   DIFF=$((AFTER_COUNT - BEFORE_COUNT))
   echo "Count at T30: $AFTER_COUNT (Growth: +$DIFF frames)"
   ```

3. **Kiểm tra kích thước file ảnh thật**:
   ```bash
   docker exec warded-backend ls -lh /app/data/proofs/$(date +%Y-%m-%d)/*/*.jpg | head -n 10
   # Đảm bảo dung lượng > 30KB - 200KB (không phải 107 bytes mock)
   ```

4. **Đính kèm bằng chứng vào Issue #182 / PR #185**:
   - Chụp lại output hoặc copy log growth đính kèm vào comment để đóng hoàn toàn DoD của T2b.

---

## 3. Hướng dẫn Thực thi Wave 0 (Chạy ngay, không phụ thuộc T2b)

### 3.1. Issue T8 (#188) — Cập nhật DoD Templates
- **Nhiệm vụ**: Thêm dòng bắt buộc đính kèm bằng chứng production/staging thật vào `docs/ISSUE_TEMPLATE_go_backend.md` và `docs/ISSUE_TEMPLATE_frontend.md`.
- **Thực thi**:
  ```bash
  git checkout -b docs/188-dod-live-production-check
  # Chỉnh sửa 2 file template
  git add docs/ISSUE_TEMPLATE_*.md
  git commit -m "docs: add mandatory live-production-check to Definition of Done templates (#188)"
  git push -u origin docs/188-dod-live-production-check
  gh pr create --base dev --head docs/188-dod-live-production-check ...
  ```

### 3.2. Issue T6 (#186) — Xử lý Container-Class Gap
- **Nhiệm vụ**: Giải quyết dead code `Container = 3.0` ở `pcu.go`.
- **Trình tự bắt buộc**:
  1. Thảo luận và chọn 1 trong 3 phương án:
     - (a) Bỏ `Container`, gộp vào `Truck` (PCU = 2.0).
     - (b) Thêm bước detect heuristic/aspect-ratio riêng cho xe container ở các camera cảng Cát Lái.
     - (c) Dùng model chuyên dụng cho nhóm camera đó.
  2. Ghi văn bản quyết định vào `docs/` trước khi code.
  3. Sửa `pcu.go`, `pcu_calculator.go`, hoặc `server.py` theo đúng quyết định.
  4. Chạy `make test-pcu` và `make test-go`.

### 3.3. Issue T7 (#187) — Bộ Camera Smoke-Test Trạng thái Đã biết
- **Nhiệm vụ**: Xây dựng danh sách 6–10 camera có baseline chắc chắn (`CLEAR` giờ thấp điểm, `CONGESTED` giờ cao điểm) vào `tools/eval/fixtures/smoke_test_cameras.json` và script `tools/eval/verify_smoke_set.py`.
- **Mục tiêu**: Chuẩn bị sẵn harness kiểm tra khách quan cho T4.

---

## 4. Kế hoạch Mở khóa Wave 1, 2, 3 (Sau khi Gate T2b Đạt)

Ngay sau khi Gate T2b có bằng chứng thật về tốc độ lưu ảnh:
1. **Khởi tạo Issue T3, T4, T5, T9** bằng `gh issue create`.
2. **Thực thi T3**:
   - Chạy `tools/eval/labeler.html`.
   - Gán nhãn các frame thực tế tích lũy trong `data/proofs/` sau ngày 2026-10-08.
   - **Tuyệt đối loại bỏ** toàn bộ kho ảnh trước ngày 2026-10-06 (đã chứng minh là mock frame).
   - Mục tiêu: $\ge 300$ frames giao thông, $\ge 60$ frames ngập (yêu cầu chờ tích lũy qua các đợt mưa thực tế).
3. **Thực thi T4**:
   - Chạy `benchmark.py` trên tập nhãn T3 kết hợp smoke set T7.
   - Lập báo cáo D6 đầu tiên trong lịch sử dự án có số liệu FNR/Precision/Recall thật.
4. **Thực thi T5 & T9**:
   - Hiệu chỉnh PCU từ số liệu T4 (T5).
   - Ra quyết định chính thức: Giữ OpenVINO hay chuyển về VLM (T9).
