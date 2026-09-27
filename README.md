# CAMERA VIEW

<div align="center">

**Ho Chi Minh City Traffic Camera Monitor & Extensible Analytics**  
*Ứng Dụng Xem Camera Giao Thông TP. Hồ Chí Minh & Tính Năng Mở Rộng*

<p align="center">
  <a href="https://github.com/529-studio/projectKV/actions"><img src="https://img.shields.io/badge/CI-passing-2ea44f?logo=github&logoColor=white" alt="CI"></a>
  <a href="https://nextjs.org/"><img src="https://img.shields.io/badge/Next.js-14.2+-000000?logo=nextdotjs&logoColor=white" alt="Next.js"></a>
  <a href="https://www.typescriptlang.org/"><img src="https://img.shields.io/badge/TypeScript-5.0+-3178C6?logo=typescript&logoColor=white" alt="TypeScript"></a>
  <a href="https://tailwindcss.com/"><img src="https://img.shields.io/badge/Tailwind_CSS-3.4+-06B6D4?logo=tailwindcss&logoColor=white" alt="Tailwind CSS"></a>
  <a href="https://www.framer.com/motion/"><img src="https://img.shields.io/badge/Framer_Motion-11+-0055FF?logo=framer&logoColor=white" alt="Framer Motion"></a>
  <a href="https://leafletjs.com/"><img src="https://img.shields.io/badge/Leaflet-1.9+-199900?logo=leaflet&logoColor=white" alt="Leaflet"></a>
  <a href="https://go.dev/"><img src="https://img.shields.io/badge/Go-1.23+-00ADD8?logo=go&logoColor=white" alt="Go"></a>
  <a href="https://sqlite.org/"><img src="https://img.shields.io/badge/SQLite-3-003B57?logo=sqlite&logoColor=white" alt="SQLite"></a>
  <a href="docs/ADR-001-flood-alert-architecture.md#adr-003-sprint-1-scope--live-updates-sse-hot-set-scoped-congestion-colors-phường-based-filter"><img src="https://img.shields.io/badge/SSE-Live_Stream-FFA500?logo=html5&logoColor=white" alt="SSE"></a>
  <a href="https://python.org/"><img src="https://img.shields.io/badge/Python-3.11+-3776AB?logo=python&logoColor=white" alt="Python"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-Proprietary-yellow" alt="License"></a>
</p>

[🇬🇧 English](#-english) &nbsp;•&nbsp; [🇻🇳 Tiếng Việt](#-tiếng-việt)

<br />
<br />

<p align="center">
  <img src="apps/web/public/warded-dockUI.jpg" alt="Warded Traffic Camera Monitor - Multi-Camera Corridor Dock UI" width="100%">
</p>
<p align="center">
  <em>Multi-Camera Corridor Tracking Dock & Map Explorer</em>
</p>

<br />

<p align="center">
  <img src="apps/web/public/warded-camera.jpg" alt="Warded Traffic Camera Monitor - Live Camera Inspection View" width="100%">
</p>
<p align="center">
  <em>Live Camera Snapshot Modal with Traffic Verdict & Pin Control</em>
</p>

</div>

---

# 🇬🇧 English

> **Ho Chi Minh City traffic camera viewer and explorer optimized for low latency, clean monochromatic high-contrast UI, chained hierarchical filtering, multi-camera pin dock, and extensible modular architecture — extending toward real-time computer vision and flood alerting.**

The system is organized as a **clean monorepo**, decoupling the Web Client (`apps/web`) from a Go backend (`services/go-server`), with a separate Python service (`services/cv-engine`) for model inference. See [`docs/ADR-001-flood-alert-architecture.md`](docs/ADR-001-flood-alert-architecture.md) for the full architectural rationale.

## 1. Project Directory Structure (Monorepo)

```
hcm-traffic-cameras/
│
├── apps/
│   └── web/                           # [FRONTEND] Next.js App Router Client (@camera-view/web)
│       ├── src/
│       │   ├── app/                   # App Router pages & layout
│       │   ├── components/            # Leaflet map, docked tracking UI, modals, legends
│       │   ├── hooks/                 # useCameras, useTraffic (SSE EventSource)
│       │   └── config/                # API endpoints, map constants
│       ├── tailwind.config.ts         # Tailwind CSS styling
│       └── package.json
│
├── services/
│   ├── go-server/                     # [BACKEND — Go] Single static binary (Gin + SQLite)
│   │   ├── cmd/server/main.go         # Composition root, routing, graceful shutdown
│   │   └── internal/
│   │       ├── live/                  # Server-Sent Events (SSE) hub & live stream endpoint
│   │       ├── camera/                # Camera catalog, Phường indexing, query endpoints
│   │       ├── proxycache/            # Snapshot reverse-proxy + in-memory TTL cache
│   │       ├── hotset/                # Hot Set Pool scheduler (see ADR-001 §2)
│   │       ├── notify/                # Notification Adapter: Telegram / Zalo / WebPush / SMS
│   │       ├── store/                 # SQLite (pure Go, no cgo) — subscriptions, dedupe, verdicts
│   │       └── tilecache/             # Offline/local map tile proxy and caching
│   │
│   └── cv-engine/                     # [ML PLANE — Python] Model inference only
│       ├── models/                    # YOLO / ONNX weights (Phase 2, self-hosted)
│       └── api.py                     # Analysis endpoint called by the Go scheduler
│
├── tools/
│   ├── geo-enrich/                    # [GEO ENRICHMENT] Batch Phường point-in-polygon tool
│   │   ├── enrich.py                  # CLI enrichment script (Ray casting / Shapely)
│   │   └── data/                      # GeoJSON boundary polygons
│   └── crawler/                       # [DATA CRAWLER]
│       └── get_all_cameras.js         # Fetch camera dataset from official DOT portal
│
├── docs/
│   ├── SYSTEM_DESIGN.md               # Original caching & CV roadmap design
│   ├── ADR-001-flood-alert-architecture.md   # Flood-alert feasibility, Go migration & Sprint 1 scope
│   └── sprint1_execution_guide.md     # Sprint 1 task sequencing and agy CLI commands
│
├── cameras_all.json                   # Base traffic camera catalog dataset
├── cameras_enriched.json              # Enriched camera catalog with phuong_id / phuong_name
├── Makefile                           # Unified orchestration commands
├── LICENSE                            # Strict proprietary, reference-only license
└── README.md                          # Project documentation
```

> **Migration note**: `services/node-server` (the original Express.js proxy) is being retired in favor of `services/go-server`. The migration is incremental — new components (Hot Set Scheduler, Notification Adapter, SQLite state store) are Go-native from the start; the existing snapshot proxy/cache is ported once the new components are verified stable. See ADR-002 for the full rationale.

## 2. Quick Start

```bash
# Install dependencies (Go modules + frontend, first time only)
make install

# [RECOMMENDED] Launch entire system concurrently (Go Backend :3000 & Frontend Dev :3001)
make dev-all

# Or run the Go backend only (Port 3000) - Serves both API and Web Frontend
make dev-go

# (Optional) Run standalone static server for Frontend (Port 3001)
make dev-web

# Crawl & update camera dataset from official DOT portal
make crawl
```

*   **Browser Access**:
    *   Full Application (Backend + Web): `http://localhost:3000`
    *   Standalone Frontend Dev: `http://localhost:3001`

## 3. Key Highlights

*   **Monochromatic & Intuitive Interface**: Minimalist high-contrast design using black, dark gray, and white tones.
*   **Bilingual i18n Support**: Instant switching between Vietnamese (`vi`, default) and English (`en`), persisted across sessions.
*   **Clean Standard OSM Basemap**: OpenStreetMap tiles, no watermark, geographically constrained to Southeastern Vietnam (`maxBounds`).
*   **Chained Deep Filter**: Multi-tier faceted filtering — *Zone* ➔ *Road / Corridor* ➔ *Intersection / Node*.
*   **Multi-Camera Corridor Tracking Dock**: Pin cameras into the bottom tracking dock with synchronized snapshot refresh.
*   **Responsive Layout (100dvh)**: Desktop, tablet, and smartphone friendly with collapsible legend and mobile bottom-sheet drawer.
*   **Go Backend**: Single static binary, no runtime dependency install, in-memory + SQLite state, goroutine-based Hot Set scheduler.

### Tech Stack & Core Libraries

| Layer | Technologies & Badges | Purpose |
|---|---|---|
| **Frontend** | [![Next.js](https://img.shields.io/badge/Next.js-14.2+-000000?logo=nextdotjs&logoColor=white)](https://nextjs.org/) [![TypeScript](https://img.shields.io/badge/TypeScript-5.0+-3178C6?logo=typescript&logoColor=white)](https://www.typescriptlang.org/) [![Tailwind CSS](https://img.shields.io/badge/Tailwind_CSS-3.4+-06B6D4?logo=tailwindcss&logoColor=white)](https://tailwindcss.com/) [![Framer Motion](https://img.shields.io/badge/Framer_Motion-11+-0055FF?logo=framer&logoColor=white)](https://www.framer.com/motion/) [![Leaflet](https://img.shields.io/badge/Leaflet-1.9+-199900?logo=leaflet&logoColor=white)](https://leafletjs.com/) | Next.js App Router, TypeScript, Tailwind CSS, Framer Motion animations, Leaflet interactive mapping |
| **Backend** | [![Go](https://img.shields.io/badge/Go-1.23+-00ADD8?logo=go&logoColor=white)](https://go.dev/) [![Gin](https://img.shields.io/badge/Gin-1.10+-008ECF?logo=go&logoColor=white)](https://gin-gonic.com/) [![SQLite](https://img.shields.io/badge/SQLite-3-003B57?logo=sqlite&logoColor=white)](https://sqlite.org/) [![SSE](https://img.shields.io/badge/SSE-Live_Stream-FFA500?logo=html5&logoColor=white)](docs/ADR-001-flood-alert-architecture.md#adr-003-sprint-1-scope--live-updates-sse-hot-set-scoped-congestion-colors-phường-based-filter) | Single static binary, Gin HTTP router, SQLite state persistence, in-memory TTL proxy caching, Server-Sent Events (SSE) live streaming |
| **Computer Vision** | [![Python](https://img.shields.io/badge/Python-3.11+-3776AB?logo=python&logoColor=white)](https://python.org/) [![PyTorch](https://img.shields.io/badge/PyTorch-EE4C2C?logo=pytorch&logoColor=white)](https://pytorch.org/) [![OpenCV](https://img.shields.io/badge/OpenCV-5C3EE8?logo=opencv&logoColor=white)](https://opencv.org/) | Python 3.11+, YOLO / ONNX model inference, pluggable traffic congestion and flood alert analyzers |
| **DevOps & Tooling** | [![CI](https://img.shields.io/badge/GitHub_Actions-CI-2ea44f?logo=githubactions&logoColor=white)](https://github.com/features/actions) [![License](https://img.shields.io/badge/License-Proprietary-yellow)](LICENSE) [![Geo-Enrich](https://img.shields.io/badge/Geo--Enrich-Point--in--Polygon-708090?logo=python&logoColor=white)](tools/geo-enrich) | GitHub Actions CI/CD workflows, golangci-lint, ESLint, unified Makefile, batch Phường point-in-polygon enrichment |

## 4. Next Evolution Steps

Detailed architectural decisions are documented in:
📄 **[`docs/SYSTEM_DESIGN.md`](docs/SYSTEM_DESIGN.md)** — original caching & CV roadmap
📄 **[`docs/ADR-001-flood-alert-architecture.md`](docs/ADR-001-flood-alert-architecture.md)** — flood-alert feasibility, notification channels, and the Go backend migration decision (ADR-002)

*   **Computer Vision Integration**: Traffic congestion + flood detection via a **Pluggable Adapter** pattern (Mock ➔ Cloud Vision API ➔ Self-hosted YOLO/ONNX), orchestrated from Go, model inference in Python.
*   **Real-Time Flood Alerting**: Zone-based subscriptions with a pluggable **Notification Adapter** — Telegram (Phase 1), Zalo OA (Phase 2), Web Push, SMS — because a rider on a motorbike cannot open a browser tab to check a map.

---

# 🇻🇳 Tiếng Việt

> **Ứng dụng tra cứu và theo dõi camera giao thông Thành phố Hồ Chí Minh với độ trễ thấp tối ưu, giao diện tối giản trực quan, hỗ trợ bộ lọc phân cấp liên hoàn, dock ghim theo dõi nhiều camera đồng thời, tích hợp đa ngôn ngữ i18n cùng các tính năng phân tích mở rộng — hướng tới xử lý thị giác máy tính thời gian thực và cảnh báo ngập lụt.**

Hệ thống được tổ chức theo mô hình monorepo, tách biệt Web Client (`apps/web`) khỏi backend Go (`services/go-server`), với một service Python riêng (`services/cv-engine`) chuyên trách suy luận mô hình (model inference).

## Khởi chạy nhanh
- Khởi chạy toàn bộ: `make dev-all`
- Truy cập trình duyệt: `http://localhost:3000` (Backend + Web) hoặc `http://localhost:3001` (Frontend Dev).

## Công nghệ cốt lõi (Tech Stack)

| Phân tầng | Công nghệ & Huy hiệu | Vai trò |
|---|---|---|
| **Frontend** | [![Next.js](https://img.shields.io/badge/Next.js-14.2+-000000?logo=nextdotjs&logoColor=white)](https://nextjs.org/) [![TypeScript](https://img.shields.io/badge/TypeScript-5.0+-3178C6?logo=typescript&logoColor=white)](https://www.typescriptlang.org/) [![Tailwind CSS](https://img.shields.io/badge/Tailwind_CSS-3.4+-06B6D4?logo=tailwindcss&logoColor=white)](https://tailwindcss.com/) [![Framer Motion](https://img.shields.io/badge/Framer_Motion-11+-0055FF?logo=framer&logoColor=white)](https://www.framer.com/motion/) [![Leaflet](https://img.shields.io/badge/Leaflet-1.9+-199900?logo=leaflet&logoColor=white)](https://leafletjs.com/) | Next.js App Router, TypeScript, Tailwind CSS, animation Framer Motion, bản đồ tương tác Leaflet |
| **Backend** | [![Go](https://img.shields.io/badge/Go-1.23+-00ADD8?logo=go&logoColor=white)](https://go.dev/) [![Gin](https://img.shields.io/badge/Gin-1.10+-008ECF?logo=go&logoColor=white)](https://gin-gonic.com/) [![SQLite](https://img.shields.io/badge/SQLite-3-003B57?logo=sqlite&logoColor=white)](https://sqlite.org/) [![SSE](https://img.shields.io/badge/SSE-Live_Stream-FFA500?logo=html5&logoColor=white)](docs/ADR-001-flood-alert-architecture.md#adr-003-sprint-1-scope--live-updates-sse-hot-set-scoped-congestion-colors-phường-based-filter) | Single static binary (Go), Gin router, lưu trữ SQLite, in-memory TTL cache, phát luồng Server-Sent Events (SSE) trực tiếp |
| **Thị giác máy tính** | [![Python](https://img.shields.io/badge/Python-3.11+-3776AB?logo=python&logoColor=white)](https://python.org/) [![PyTorch](https://img.shields.io/badge/PyTorch-EE4C2C?logo=pytorch&logoColor=white)](https://pytorch.org/) [![OpenCV](https://img.shields.io/badge/OpenCV-5C3EE8?logo=opencv&logoColor=white)](https://opencv.org/) | Python 3.11+, suy luận mô hình YOLO / ONNX, phân tích kẹt xe và ngập úng |
| **DevOps & Công cụ** | [![CI](https://img.shields.io/badge/GitHub_Actions-CI-2ea44f?logo=githubactions&logoColor=white)](https://github.com/features/actions) [![License](https://img.shields.io/badge/License-Proprietary-yellow)](LICENSE) [![Geo-Enrich](https://img.shields.io/badge/Geo--Enrich-Point--in--Polygon-708090?logo=python&logoColor=white)](tools/geo-enrich) | CI/CD GitHub Actions, golangci-lint, ESLint, Makefile điều phối tập trung, công cụ gán Phường point-in-polygon |

## Tài liệu kiến trúc chuyên sâu
- [`docs/SYSTEM_DESIGN.md`](docs/SYSTEM_DESIGN.md) — thiết kế caching & roadmap CV gốc.
- [`docs/ADR-001-flood-alert-architecture.md`](docs/ADR-001-flood-alert-architecture.md) — đánh giá khả thi cảnh báo ngập lụt, lựa chọn kênh thông báo (Telegram/Zalo/Web Push/SMS), và quyết định chuyển backend sang Go (ADR-002).

## Vì sao chuyển sang Go?
Không phải vì lưu lượng hiện tại đã vượt ngưỡng (chưa vượt), mà vì các thành phần mới cần xây (Hot Set Scheduler, Notification Adapter, state store) hợp với goroutine/channel hơn, và rủi ro viết mới bằng Go thấp hơn nhiều so với việc viết lại `server.js` đang chạy ổn. Phần suy luận mô hình CV (YOLO/ONNX, xử lý ảnh nặng) vẫn giữ ở Python vì hệ sinh thái thư viện tốt hơn hẳn.
