# CAMERA VIEW

<div align="center">

**Ho Chi Minh City Traffic Camera Monitor & Extensible Analytics**  
*Ứng Dụng Xem Camera Giao Thông TP. Hồ Chí Minh & Tính Năng Mở Rộng*

[🇬🇧 English](#-english) &nbsp;•&nbsp; [🇻🇳 Tiếng Việt](#-tiếng-việt)

</div>

---

# 🇬🇧 English

> **Ho Chi Minh City traffic camera viewer and explorer optimized for low latency, clean monochromatic high-contrast UI, chained hierarchical filtering, multi-camera pin dock, and extensible modular architecture.**

The system is organized as a **clean monorepo**, strictly decoupling the Web Client (`apps/web`) from the high-throughput Go Backend service (`services/go-server`), ready for future computer vision and analytics extensions.

## 1. Project Directory Structure (Monorepo)

```
hcm-traffic-cameras/
│
├── apps/
│   └── web/                           # [FRONTEND] Standalone Web Client (@camera-view/web)
│       ├── index.html                 # Clean, fast-loading web interface with i18n
│       ├── styles.css                 # Optimized responsive CSS (Desktop, Tablet & Mobile)
│       ├── app.js                     # Leaflet map, Chained Deep Filter, Marker scaling, Dock
│       └── package.json
│
├── services/
│   └── go-server/                     # [BACKEND] High-throughput Go Gin Server & Proxy Cache
│       ├── cmd/server/main.go         # Entry point, router, graceful shutdown
│       ├── internal/
│       │   ├── cache/                 # RWMutex snapshot cache with singleflight dedup
│       │   ├── config/                # Environment configuration & path resolvers
│       │   ├── handlers/              # /api/cameras & /api/cameras/snapshot/:id
│       │   ├── middleware/            # CORS middleware for dev client
│       │   └── models/                # Camera data models & catalog loader
│       └── go.mod
│
├── tools/
│   └── crawler/                       # [DATA CRAWLER]
│       └── get_all_cameras.js         # Fetch 837 cameras dataset from official DOT portal
│
├── docs/                              # [DOCUMENTATION]
│   └── SYSTEM_DESIGN.md               # System architecture, Hot Set caching & CV roadmap
│
├── cameras_all.json                   # 837 traffic cameras dataset
├── Makefile                           # Unified orchestration commands (make dev-all, dev-go...)
├── LICENSE                            # Strict proprietary, reference-only license
└── README.md                          # Project documentation
```

## 2. Quick Start

The project is optimized for fast launch via **Makefile**:

```bash
# Install dependencies & activate git hooks (first time only)
make install

# [RECOMMENDED] Launch entire system concurrently (Go Backend :3000 & Frontend Dev :3001)
make dev-all

# Or run Go Gin backend server only (Port 3000) - Serves both API and Web Frontend
make dev-go

# Run Go test suite
make test-go

# Compile standalone Go production binary
make build-go

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
*   **Bilingual i18n Support**: Seamless instant switching between Vietnamese (`vi`, default) and English (`en`) via a one-click header toggle, persisted across sessions.
*   **Clean Standard OSM Basemap**: Uses clean OpenStreetMap tiles with no third-party watermark, geographically constrained within Southeastern Vietnam coordinates (`maxBounds`).
*   **Chained Deep Filter**:
    *   Multi-tier faceted filtering: *Zone* ➔ *Road / Corridor* ➔ *Intersection / Node*.
    *   Dynamic facet count computation, showing only available options sorted by camera count.
*   **Multi-Camera Corridor Tracking Dock**:
    *   Click any camera to instantly preview live snapshots.
    *   Click `[+ PIN FOR MONITORING]` to pin cameras into the bottom tracking dock.
    *   Automatic synchronized snapshot refresh every 10 seconds via in-memory backend cache.
*   **Responsive Layout (100dvh)**: Seamless experience across wide desktop displays, tablets, and smartphones (sidebar auto-collapses on mobile when selecting a camera).

## 4. Next Evolution Steps

Detailed architectural decisions and technical specifications are documented in:
📄 **[`docs/SYSTEM_DESIGN.md`](docs/SYSTEM_DESIGN.md)**

*   **Multi-User Scalability**: **Hot Set Pool** architecture — whether 10 or 10,000 concurrent users view the same camera, only 1 request is sent to the traffic portal every 10 seconds.
*   **Computer Vision Integration (CV Engine)**: Traffic congestion detection, container truck counting, and queue length measurement via a **Pluggable Adapter** pattern (Mock ➔ Cloud Vision ➔ Self-hosted YOLOv8/v11 ONNX).

---

# 🇻🇳 Tiếng Việt

> **Ứng dụng tra cứu và theo dõi camera giao thông Thành phố Hồ Chí Minh với độ trễ thấp tối ưu, giao diện tối giản trực quan, hỗ trợ bộ lọc phân cấp liên hoàn, dock ghim theo dõi nhiều camera đồng thời, tích hợp đa ngôn ngữ i18n cùng các tính năng phân tích mở rộng.**

Vui lòng tham khảo các lệnh khởi chạy nhanh:
- Khởi chạy toàn bộ (Go Backend + Web Dev): `make dev-all`
- Khởi chạy riêng Go Gin Backend: `make dev-go`
- Chạy kiểm thử Go: `make test-go`
- Biên dịch Go binary: `make build-go`
- Truy cập trình duyệt: `http://localhost:3000` (Go Backend + Web) hoặc `http://localhost:3001` (Frontend Dev).
- Tài liệu kiến trúc chuyên sâu: [`docs/SYSTEM_DESIGN.md`](docs/SYSTEM_DESIGN.md).
