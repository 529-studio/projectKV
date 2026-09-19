# CAMERA VIEW

<div align="center">

**Ứng Dụng Xem Camera Giao Thông TP. Hồ Chí Minh & Tính Năng Mở Rộng**  
*Ho Chi Minh City Traffic Camera Monitor & Extensible Analytics*

[🇻🇳 Tiếng Việt](#-tiếng-việt) &nbsp;•&nbsp; [🇬🇧 English](#-english)

</div>

---

# 🇻🇳 Tiếng Việt

> **Ứng dụng tra cứu và theo dõi camera giao thông Thành phố Hồ Chí Minh theo thời gian thực với độ trễ thấp, giao diện tối giản trực quan, hỗ trợ bộ lọc phân cấp liên hoàn, dock ghim theo dõi nhiều camera đồng thời cùng các tính năng phân tích tích hợp thêm.**

Hệ thống được tổ chức theo cấu trúc **Monorepo tinh gọn**, phân tách độc lập giữa ứng dụng Web Client (`apps/web`) và dịch vụ Backend Node.js (`services/node-server`), sẵn sàng mở rộng các dịch vụ phân tích hình ảnh (Computer Vision) trong tương lai.

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
├── Makefile                           # Tập lệnh điều phối dự án (make dev-all, dev-node...)
├── LICENSE                            # Bản quyền đóng, chỉ dành cho nghiên cứu tham khảo
└── README.md                          # Tài liệu hướng dẫn song ngữ
```

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

## 4. Định Hướng Phát Triển Kế Tiếp

Chi tiết thiết kế kiến trúc và giải pháp kỹ thuật đã được ghi lại đầy đủ trong:
📄 **[`docs/SYSTEM_DESIGN.md`](docs/SYSTEM_DESIGN.md)**

*   **Mở rộng nhiều người dùng (Multi-User Scalability)**: Áp dụng cơ chế **Hot Set Pool** — dù có 10 hay 10.000 người cùng xem một nút giao, hệ thống chỉ gửi 1 request lấy ảnh mỗi 10 giây.
*   **Tích hợp Computer Vision (CV Engine)**: Phát hiện tắc đường, đếm xe container, đo chiều dài hàng xe và cảnh báo sự cố theo mô hình **Pluggable Adapter** (Mock ➔ Cloud Vision ➔ Self-hosted YOLOv8/v11 ONNX).

---

# 🇬🇧 English

> **Real-time Ho Chi Minh City traffic camera viewer and explorer with ultra-low latency, clean monochromatic high-contrast UI, chained hierarchical filtering, multi-camera pin dock, and extensible modular architecture.**

The system is organized as a **clean monorepo**, strictly decoupling the Web Client (`apps/web`) from the Node.js Backend service (`services/node-server`), ready for future computer vision and analytics extensions.

## 1. Project Directory Structure (Monorepo)

```
hcm-traffic-cameras/
│
├── apps/
│   └── web/                           # [FRONTEND] Standalone Web Client (@camera-view/web)
│       ├── index.html                 # Clean, fast-loading web interface
│       ├── styles.css                 # Optimized responsive CSS (Desktop, Tablet & Mobile)
│       ├── app.js                     # Leaflet map, Chained Deep Filter, Marker scaling, Dock
│       └── package.json
│
├── services/
│   └── node-server/                   # [NODE.JS BACKEND] Express Server & Proxy Cache
│       ├── server.js                  # API & Snapshot proxy with in-memory cache (Port 3000)
│       └── package.json               # Isolated backend dependencies (express, dotenv)
│
├── tools/
│   └── crawler/                       # [DATA CRAWLER]
│       └── get_all_cameras.js         # Fetch 837 cameras dataset from official DOT portal
│
├── docs/                              # [DOCUMENTATION]
│   └── SYSTEM_DESIGN.md               # System architecture, Hot Set caching & CV roadmap
│
├── cameras_all.json                   # 837 traffic cameras dataset
├── Makefile                           # Unified orchestration commands (make dev-all, dev-node...)
├── LICENSE                            # Strict proprietary, reference-only license
└── README.md                          # Bilingual documentation (VI / EN)
```

## 2. Quick Start

The project is optimized for fast launch via **Makefile**:

```bash
# Install backend dependencies (first time only)
make install

# [RECOMMENDED] Launch entire system concurrently (Backend :3000 & Frontend Dev :3001)
make dev-all

# Or run Node.js backend only (Port 3000) - Serves both API and Web Frontend
make dev-node

# (Optional) Run standalone static server for Frontend (Port 3001)
make dev-web

# Crawl & update camera dataset from official DOT portal
make crawl
```

*   **Browser Access**:
    *   Full Application (Backend + Web): `http://localhost:3000`
    *   Standalone Frontend Dev: `http://localhost:3001`

## 3. Key Highlights

*   **Monochromatic & Intuitive Interface**: Minimalist high-contrast design using black, dark gray, and white tones to focus on real-world camera imagery without eye strain.
*   **Clean Standard OSM Basemap**: Uses clean OpenStreetMap tiles with no third-party watermark, geographically constrained within Southeastern Vietnam coordinates (`maxBounds`).
*   **Chained Deep Filter**:
    *   Multi-tier faceted filtering: *Zone* ➔ *Road / Corridor* ➔ *Intersection / Node*.
    *   Dynamic facet count computation, showing only available options sorted by camera count.
*   **Multi-Camera Corridor Tracking Dock**:
    *   Click any camera to instantly preview live snapshots.
    *   Click `[+ GHIM THEO DÕI]` to pin cameras into the bottom tracking dock.
    *   Automatic synchronized snapshot refresh every 10 seconds via in-memory backend cache.
*   **Responsive Layout (100dvh)**: Seamless experience across wide desktop displays, tablets, and smartphones (sidebar auto-collapses on mobile when selecting a camera).

## 4. Next Evolution Steps

Detailed architectural decisions and technical specifications are documented in:
📄 **[`docs/SYSTEM_DESIGN.md`](docs/SYSTEM_DESIGN.md)**

*   **Multi-User Scalability**: **Hot Set Pool** architecture — whether 10 or 10,000 concurrent users view the same camera, only 1 request is sent to the traffic portal every 10 seconds.
*   **Computer Vision Integration (CV Engine)**: Traffic congestion detection, container truck counting, and queue length measurement via a **Pluggable Adapter** pattern (Mock ➔ Cloud Vision ➔ Self-hosted YOLOv8/v11 ONNX).
