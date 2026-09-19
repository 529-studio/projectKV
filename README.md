# CAMERA VIEW — Xem Camera Giao Thông TP. Hồ Chí Minh & Tính Năng Mở Rộng

> **Ứng dụng tra cứu và theo dõi camera giao thông Thành phố Hồ Chí Minh theo thời gian thực với độ trễ thấp, giao diện tối giản trực quan, hỗ trợ bộ lọc phân cấp liên hoàn, dock ghim theo dõi nhiều camera đồng thời cùng các tính năng phân tích tích hợp thêm.**

Hệ thống được tổ chức theo cấu trúc **Monorepo tinh gọn**, phân tách độc lập giữa ứng dụng Web Client (`apps/web`) và dịch vụ Backend Node.js (`services/node-server`), sẵn sàng mở rộng các dịch vụ phân tích hình ảnh (Computer Vision) trong tương lai.

---

## 1. Cấu Trúc Thư Mục Dự Án (Monorepo)

```
hcm-traffic-cameras/
│
├── apps/
│   └── web/                           # [FRONTEND] Web App độc lập (@camera-view/web)
│       ├── index.html                 # Giao diện web trực quan, tải trang tức thì
│       ├── styles.css                 # CSS tối ưu, tương thích Desktop, Tablet & Mobile
│       ├── app.js                     # Leaflet map, Chained Deep Filter, Marker scaling, Dock
│       └── package.json
│
├── services/
│   └── node-server/                   # [NODE.JS BACKEND] Server Express & Proxy Cache
│       ├── server.js                  # API & Snapshot proxy (Port 3000)
│       └── package.json               # Dependencies riêng biệt (express, dotenv)
│
├── tools/
│   └── crawler/                       # [CÔNG CỤ CÀO DỮ LIỆU]
│       └── get_all_cameras.js         # Thu thập danh mục 837 camera từ cổng GTVT
│
├── docs/                              # [TÀI LIỆU DỰ ÁN]
│   └── SYSTEM_DESIGN.md               # Thiết kế hệ thống, kiến trúc Hot Set & lộ trình mở rộng
│
├── cameras_all.json                   # Cơ sở dữ liệu 837 camera giao thông
├── Makefile                           # Tập lệnh điều phối dự án (make dev-node, dev-web...)
├── LICENSE                            # Bản quyền đóng, chỉ dành cho nghiên cứu tham khảo
└── README.md                          # Hướng dẫn chi tiết
```

---

## 2. Hướng Dẫn Khởi Chạy Nhanh (Quick Start)

Dự án được tối ưu để khởi chạy nhanh nhất thông qua **Makefile**:

```bash
# Cài đặt dependencies cho backend (chỉ cần chạy lần đầu)
make install

# [KHUYÊN DÙNG] Khởi chạy toàn bộ hệ thống cùng lúc (Backend :3000 & Frontend Dev :3001)
make dev-all

# Hoặc chỉ chạy Backend Node.js (Port 3000) - Phục vụ cả API và Web Frontend
make dev-node

# (Tùy chọn) Chạy riêng server tĩnh cho Frontend (Port 3001)
make dev-web

# Cập nhật danh sách camera mới nhất từ cổng GTVT TP.HCM
make crawl
```

*   **Truy cập trên trình duyệt**:
    *   Ứng dụng hoàn chỉnh (Backend + Web): `http://localhost:3000`
    *   Frontend Dev độc lập: `http://localhost:3001`

---

## 3. Tính Năng Nổi Bật

*   **Giao Diện Đơn Sắc & Trực Quan**: Thiết kế tối giản với độ tương phản cao, tông màu đen / xám đậm / trắng, giúp tập trung theo dõi hình ảnh hiện trường một cách rõ ràng, không gây mỏi mắt.
*   **Bản Đồ Mặc Định OSM Chuẩn**: Sử dụng OpenStreetMap tự nhiên, đường sá sắc nét, tuyệt đối không dính watermark và khóa chặt phạm vi lãnh thổ Đông Nam Bộ (`maxBounds`).
*   **Chained Deep Filter (Bộ Lọc Phân Cấp Liên Hoàn)**:
    *   Lọc đa tầng: *Khu vực* ➔ *Tuyến đường* ➔ *Giao lộ / Điểm cắt*.
    *   Thuật toán Facet tự động tính toán số lượng và chỉ hiển thị các giá trị khả dụng thực tế, tự động sắp xếp theo nơi có nhiều camera nhất.
*   **Dock Theo Dõi Đa Camera Đồng Thời (Corridor Tracking Dock)**:
    *   Bấm chọn camera để xem ảnh snapshot trực tiếp tức thì.
    *   Bấm `[+ GHIM THEO DÕI]` để đưa camera vào thanh dock dưới đáy màn hình.
    *   Tự động đồng bộ làm mới ảnh mỗi 10 giây qua bộ đệm proxy mà không làm nghẽn nguồn GTVT.
*   **Đáp Ứng Đa Thiết Bị (Responsive 100dvh)**: Trải nghiệm mượt mà từ màn hình máy tính lớn đến điện thoại di động (sidebar tự thu gọn khi chọn camera trên mobile).

---

## 4. Định Hướng Phát Triển Kế Tiếp

Chi tiết thiết kế kiến trúc và giải pháp kỹ thuật đã được ghi lại đầy đủ trong:
📄 **[`docs/SYSTEM_DESIGN.md`](docs/SYSTEM_DESIGN.md)**

*   **Mở rộng nhiều người dùng (Multi-User Scalability)**: Áp dụng cơ chế **Hot Set Pool** — dù có 10 hay 10.000 người cùng xem một nút giao, hệ thống chỉ gửi 1 request lấy ảnh và 1 lần chạy AI mỗi 30–45 giây.
*   **Tích hợp Computer Vision (CV Engine)**: Phát hiện tắc đường, đếm xe container, đo chiều dài hàng xe và cảnh báo sự cố theo mô hình **Pluggable Adapter** (Mock ➔ Cloud Vision ➔ Self-hosted YOLOv8/v11 ONNX).
