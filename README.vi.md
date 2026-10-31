# CAMERA VIEW

**Ứng dụng xem camera giao thông TP. Hồ Chí Minh & tính năng phân tích mở rộng**

[English](README.md#english) &nbsp;•&nbsp; [Tiếng Việt](README.vi.md)

Đây là **hướng dẫn nhanh** bằng tiếng Việt, đủ để chạy ứng dụng trên máy cục bộ.
Bản đầy đủ — kiến trúc, cấu hình, API, roadmap, giấy phép — nằm ở
[`README.md`](README.md), và đó là bản chuẩn.

File này cố tình **không** phải bản dịch toàn bộ. Một bản dịch đầy đủ lệch khỏi bản gốc
sau vài ngày và không ai đọc lại phần đã lệch; phần dưới đây chỉ giữ những gì thay đổi
chậm — lệnh chạy và cổng dịch vụ.

---

## 1. Cần có trước

- Go 1.26+
- Node.js 20+ và npm (CI dùng Node 22)
- Docker — chỉ cần khi chạy mô hình OpenVINO cục bộ

## 2. Chạy ứng dụng

```bash
# Cài dependencies, chỉ lần đầu
make install

# Go backend (:3000) + Next.js dev server (:3001)
make dev-all
```

Chạy kèm mô hình nhận diện xe cục bộ:

```bash
make dev-model    # container CV Engine (:8000)
# trong .env cần có: ANALYZER=onnx và CV_ENGINE_URL=http://localhost:8000
make dev-all
```

Cấu hình: sao chép `.env.example` thành `.env` rồi điền giá trị. Không bao giờ commit `.env`.
Danh sách đầy đủ các biến nằm ở mục **Configuration Reference** trong [`README.md`](README.md).

## 3. Cổng dịch vụ

| Dịch vụ | Cổng | Địa chỉ |
|---|:---:|---|
| Bản đồ camera | `3001` | http://localhost:3001 |
| Công cụ gán nhãn | `3001` | http://localhost:3001/labeler |
| Go Backend API | `3000` | http://localhost:3000 |
| Swagger | `3000` | http://localhost:3000/swagger/index.html |
| Kiểm tra sức khỏe | `3000` | http://localhost:3000/health |
| CV Engine | `8000` | http://localhost:8000/healthz |

## 4. Đọc tiếp

- Kiến trúc backend Go: [`docs/GO_BACKEND_ARCHITECTURE.md`](docs/GO_BACKEND_ARCHITECTURE.md)
- Điểm bắt đầu cho developer: [`docs/ONBOARDING.md`](docs/ONBOARDING.md)
- Quy tắc bắt buộc của dự án: [`AGENTS.md`](AGENTS.md)
