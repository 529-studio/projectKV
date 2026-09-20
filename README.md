# CAMERA VIEW

<div align="center">

**Ho Chi Minh City Traffic Camera Monitor & Extensible Analytics**
*Ứng Dụng Xem Camera Giao Thông TP. Hồ Chí Minh & Tính Năng Mở Rộng*

[🇬🇧 English](#-english) &nbsp;•&nbsp; [🇻🇳 Tiếng Việt](#-tiếng-việt)

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
│   └── web/                           # [FRONTEND] Standalone Web Client (@camera-view/web)
│       ├── index.html                 # Clean, fast-loading web interface with i18n
│       ├── styles.css                 # Responsive CSS (Desktop, Tablet & Mobile)
│       ├── app.js                     # Leaflet map, Chained Deep Filter, Marker scaling, Dock
│       └── package.json
│
├── services/
│   ├── go-server/                     # [BACKEND — Go] Single static binary
│   │   ├── cmd/api/main.go            # Entry point, wiring
│   │   └── internal/
│   │       ├── proxycache/            # Snapshot reverse-proxy + in-memory TTL cache
│   │       ├── hotset/                # Hot Set Pool scheduler (see ADR-001 §2)
│   │       ├── notify/                # Notification Adapter: Telegram / Zalo / WebPush / SMS
│   │       └── store/                 # SQLite (pure Go, no cgo) — subscriptions, dedupe state
│   │
│   └── cv-engine/                     # [ML PLANE — Python] Model inference only
│       ├── models/                    # YOLO / ONNX weights (Phase 2, self-hosted)
│       └── api.py                     # Analysis endpoint called by the Go scheduler
│
├── tools/
│   └── crawler/                       # [DATA CRAWLER]
│       └── get_all_cameras.js         # Fetch camera dataset from official DOT portal
│
├── docs/
│   ├── SYSTEM_DESIGN.md               # Original caching & CV roadmap design
│   ├── ADR-001-flood-alert-architecture.md   # Flood-alert feasibility & architecture
│   └── (ADR-002 is appended to ADR-001, backend language decision)
│
├── cameras_all.json                   # Traffic camera catalog dataset
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
*   **Responsive Layout (100dvh)**: Desktop, tablet, and smartphone friendly.
*   **Go Backend**: Single static binary, no runtime dependency install, in-memory + SQLite state, goroutine-based Hot Set scheduler.

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

## Tài liệu kiến trúc chuyên sâu
- [`docs/SYSTEM_DESIGN.md`](docs/SYSTEM_DESIGN.md) — thiết kế caching & roadmap CV gốc.
- [`docs/ADR-001-flood-alert-architecture.md`](docs/ADR-001-flood-alert-architecture.md) — đánh giá khả thi cảnh báo ngập lụt, lựa chọn kênh thông báo (Telegram/Zalo/Web Push/SMS), và quyết định chuyển backend sang Go (ADR-002).

## Vì sao chuyển sang Go?
Không phải vì lưu lượng hiện tại đã vượt ngưỡng (chưa vượt), mà vì các thành phần mới cần xây (Hot Set Scheduler, Notification Adapter, state store) hợp với goroutine/channel hơn, và rủi ro viết mới bằng Go thấp hơn nhiều so với việc viết lại `server.js` đang chạy ổn. Phần suy luận mô hình CV (YOLO/ONNX, xử lý ảnh nặng) vẫn giữ ở Python vì hệ sinh thái thư viện tốt hơn hẳn.
