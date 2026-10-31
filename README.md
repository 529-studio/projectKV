# CAMERA VIEW

<div align="center">

**Ho Chi Minh City Traffic Camera Monitor & Extensible Analytics**  
*Ứng Dụng Xem Camera Giao Thông TP. Hồ Chí Minh & Tính Năng Mở Rộng*

<p align="center">
  <a href="https://github.com/529-studio/projectKV/actions"><img src="https://img.shields.io/badge/CI-passing-2ea44f?logo=github&logoColor=white" alt="CI"></a>
  <a href="https://nextjs.org/"><img src="https://img.shields.io/badge/Next.js-15.5+-000000?logo=nextdotjs&logoColor=white" alt="Next.js"></a>
  <a href="https://www.typescriptlang.org/"><img src="https://img.shields.io/badge/TypeScript-5.0+-3178C6?logo=typescript&logoColor=white" alt="TypeScript"></a>
  <a href="https://tailwindcss.com/"><img src="https://img.shields.io/badge/Tailwind_CSS-3.4+-06B6D4?logo=tailwindcss&logoColor=white" alt="Tailwind CSS"></a>
  <a href="https://www.framer.com/motion/"><img src="https://img.shields.io/badge/Framer_Motion-11+-0055FF?logo=framer&logoColor=white" alt="Framer Motion"></a>
  <a href="https://leafletjs.com/"><img src="https://img.shields.io/badge/Leaflet-1.9+-199900?logo=leaflet&logoColor=white" alt="Leaflet"></a>
  <a href="https://go.dev/"><img src="https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&logoColor=white" alt="Go"></a>
  <a href="https://sqlite.org/"><img src="https://img.shields.io/badge/SQLite-3-003B57?logo=sqlite&logoColor=white" alt="SQLite"></a>
  <a href="docs/ADR-001-flood-alert-architecture.md#adr-003-sprint-1-scope--live-updates-sse-hot-set-scoped-congestion-colors-phường-based-filter"><img src="https://img.shields.io/badge/SSE-Live_Stream-FFA500?logo=html5&logoColor=white" alt="SSE"></a>
  <a href="https://open-meteo.com/"><img src="https://img.shields.io/badge/Open--Meteo-Weather_API-FF8C00?logo=weatherdotcom&logoColor=white" alt="Open-Meteo"></a>
  <a href="https://www.rainviewer.com/api.html"><img src="https://img.shields.io/badge/RainViewer-Radar_API-0284C7?logo=radar&logoColor=white" alt="RainViewer"></a>
  <a href="https://www.cloudflare.com/developer-platform/r2/"><img src="https://img.shields.io/badge/Cloudflare_R2-Object_Storage-F38020?logo=cloudflare&logoColor=white" alt="Cloudflare R2"></a>
  <a href="https://python.org/"><img src="https://img.shields.io/badge/Python-3.11+-3776AB?logo=python&logoColor=white" alt="Python"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-AGPL--3.0-blue" alt="License"></a>
</p>

[English](README.md#english) &nbsp;•&nbsp; [Tiếng Việt](README.vi.md)

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

# English

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
│   ├── go-server/                     # [BACKEND — Go] Single static binary (Gin + SQLite + R2 Storage)
│   │   ├── cmd/
│   │   │   ├── server/main.go         # Composition root, routing, graceful shutdown
│   │   │   ├── notify-preview/main.go # CLI for single-source Telegram flood alert message preview
│   │   │   ├── sync-proofs/main.go    # High-concurrency Cloudflare R2 proofs sync CLI
│   │   │   └── verify-r2/main.go      # Cloudflare R2 connectivity verification tool
│   │   └── internal/
│   │       ├── live/                  # Server-Sent Events (SSE) hub & live stream endpoint
│   │       ├── camera/                # Camera catalog, Phường indexing, query endpoints
│   │       ├── proxycache/            # Snapshot reverse-proxy + in-memory TTL cache
│   │       ├── hotset/                # Hot Set Pool scheduler (see ADR-001 §2)
│   │       ├── storage/               # Pluggable object storage (LocalStorage & Cloudflare R2)
│   │       ├── archiver/              # Snapshot proof archiver with ICT (UTC+7) timezone standard
│   │       ├── harvester/             # Autonomous dual-trigger poller, label generator & R2 streamer
│   │       ├── notify/                # Notification Adapter: Telegram / Zalo / WebPush / SMS
│   │       ├── store/                 # SQLite (pure Go, no cgo) — subscriptions, dedupe, verdicts
│   │       └── tilecache/             # Offline/local map tile proxy and caching
│   │
│   └── cv-engine/                     # [ML PLANE — Python] Model inference on inference worker
│       ├── models/                    # YOLO / ONNX / OpenVINO IR FP16 weights
│       └── api.py                     # Multi-threaded OpenVINO detection endpoint (:8000)
│
├── tools/
│   ├── eval/                          # [EVALUATION] Ground-truth benchmarks & label curation
│   │   ├── auto_harvest.py            # Standalone automated frame harvester & Draft-07 labeler CLI
│   │   ├── export_yolo_seg.py         # Standalone YOLO segmentation dataset exporter CLI (ADR-009)
│   │   ├── label_curator.py           # Taxonomy curation and legacy label cleansing
│   │   └── benchmark.py               # PCU & flood accuracy evaluation harness
│   ├── train/                         # [OFF-BOX TRAINING] Road surface water segmentation pipeline
│   │   ├── train_yolov8_seg.py        # Off-box YOLOv8-seg training & OpenVINO FP16 export
│   │   └── train_yolov8_seg_test.py   # Training pipeline & export unit tests
│   ├── storage/                       # [OBJECT STORAGE] Cloudflare R2 verification tooling
│   │   └── verify_r2.py               # Boto3 / R2 connectivity verification script
│   ├── geo-enrich/                    # [GEO ENRICHMENT] Batch Phường point-in-polygon tool
│   │   ├── enrich.py                  # CLI enrichment script (Ray casting / Shapely)
│   │   └── data/                      # GeoJSON boundary polygons
│   └── crawler/                       # [DATA CRAWLER]
│       └── get_all_cameras.js         # Fetch camera dataset from official DOT portal
│
├── scripts/
│   └── verify_staging_telegram.sh     # Ops pre-flight: live staging Telegram flood alert verification (Issue #246)
│
├── docs/
│   ├── SYSTEM_DESIGN.md               # Original caching & CV roadmap design
│   ├── GO_BACKEND_ARCHITECTURE.md     # Go backend service layout and runtime model
│   ├── ADR-001-flood-alert-architecture.md   # Flood-alert feasibility, Go migration & Sprint 1 scope
│   └── labeling_guidelines.md         # Label schema and annotation rules
│
├── cameras_all.json                   # Base traffic camera catalog dataset
├── cameras_enriched.json              # Enriched camera catalog with phuong_id / phuong_name
├── Makefile                           # Unified orchestration commands
├── LICENSE                            # GNU Affero General Public License v3.0
└── README.md                          # Project documentation
```

> **Migration note**: `services/node-server` (the original Express.js proxy) is being retired in favor of `services/go-server`. The migration is incremental — new components (Hot Set Scheduler, Notification Adapter, SQLite state store) are Go-native from the start; the existing snapshot proxy/cache is ported once the new components are verified stable. See ADR-002 for the full rationale.

## 2. Quick Start

### 2.1. Basic Mode (UI & Backend without Local Model Inference)
Suitable for rapid UI development, offline testing, or lightweight exploration:

```bash
# Install dependencies (Go modules + frontend, first time only)
make install

# Launch Go backend (:3000) and Next.js frontend dev server (:3001) concurrently
make dev-all

# Or run the Go backend only (Port 3000) - Serves both API and Web Frontend
make dev-go

# (Optional) Run standalone static server for Frontend (Port 3001)
make dev-web

# Crawl & update camera dataset from official DOT portal
make crawl
```

### 2.2. Full-Stack with Local OpenVINO Model Inference (Recommended)
To run real AI vehicle detection and PCU saturation scoring locally using self-hosted OpenVINO (`yolov8n_openvino_320`):

```bash
# Step 1: Start the CV Engine container in the background (port 8000)
make dev-model
# (Equivalent to: docker compose -f docker-compose.local.yml up -d)

# Step 2: Ensure .env is configured for local OpenVINO inference:
# ANALYZER=onnx
# CV_ENGINE_URL=http://localhost:8000

# Step 3: Launch Go Backend (:3000) and Next.js Frontend (:3001)
make dev-all
```

#### Service Endpoints & Verification Matrix

| Service | Port | Endpoint URL | Verification Check |
|---|:---:|---|---|
| **Web Map Explorer** | `3001` | http://localhost:3001 | Interactive Leaflet map, corridor dock |
| **Collaborative Labeler** | `3001` | http://localhost:3001/labeler | Snapshot labeling tool with auto-harvest review filters, in-memory partition cache, Shift+Enter fast approval, and a segmentation mode for drawing 6-class polygon ground truth (ADR-011 D1) |
| **Go Backend API** | `3000` | http://localhost:3000 | Core Gin HTTP REST server |
| **Subscription API** | `3000` | http://localhost:3000/api/subscriptions | `POST`, `GET`, `DELETE` flood alert subscriptions |
| **Swagger UI** | `3000` | http://localhost:3000/swagger/index.html | Interactive OpenAPI documentation |
| **Backend Health** | `3000` | http://localhost:3000/health | Returns `{"status":"ok"}` |
| **CV Inference Engine** | `8000` | http://localhost:8000/healthz | Returns `{"status":"healthy","model":"yolov8n_openvino_320"}` |

> [!NOTE]
> `make dev-all` launches the Go backend and Next.js dev server. It expects the `warded-model` Docker container to be already running if `ANALYZER=onnx` is configured.

*   **Browser Access**:
    *   Full Application (Backend + Web): `http://localhost:3000`
    *   Standalone Frontend Dev: `http://localhost:3001`

## 3. Key Highlights

*   **Monochromatic & Intuitive Interface**: Minimalist high-contrast design using black, dark gray, and white tones.
*   **Bilingual i18n Support**: Instant switching between Vietnamese (`vi`, default) and English (`en`), persisted across sessions.
*   **Clean Standard OSM Basemap**: OpenStreetMap tiles, no watermark, geographically constrained to Southeastern Vietnam (`maxBounds`).
*   **Map Clutter Hygiene & Offline Camera Filtering**: Automatically hides 135 offline or no-signal cameras (`status === 'NOT_IMAGE'` / `'NO_SIGNAL'`) by default, with dynamic header counter badge (`Đang hiển thị 702 / 837 camera`) and a monochromatic toggle button.
*   **Chained Deep Filter**: Multi-tier faceted filtering — *Zone* ➔ *Road / Corridor* ➔ *Intersection / Node*.
*   **Real-Time Arterial Marker Saturation Coloring**: Dynamically styles the 50 arterial Hot Set camera markers based on continuous 30-second autonomous ONNX PCU saturation indexes: Emerald green (`#10b981`) for Clear ($SI < 0.30$), Amber yellow (`#f59e0b`) for Moderate ($0.30 \le SI < 0.60$), and Rose red (`#ef4444`) for Congested ($SI \ge 0.60$) with subtle pulsating CSS animation, rich tooltip metrics (SI% & PCU), and monochromatic map footer legend `[Xanh: Thông thoáng] [Vàng: Đông xe] [Đỏ: Ùn ứ]`.
*   **Calibrated Doppler Rain Radar Pipeline (S8-T1)**: RainViewer Doppler radar tiles projected at raw Zoom 8 (`0_0.png`) with true meteorological RGB reflectivity (dBZ) decoding ($18–29\text{ dBZ}$ Light, $30–39\text{ dBZ}$ Moderate, $\ge 40\text{ dBZ}$ Heavy rain), 7-point hexagon spatial contraction sampling with quorum, and surface cross-validation gate with ground cameras, preventing false inundation alarms on clear days.
*   **Multi-Camera Corridor Tracking Dock**: Pin cameras into the bottom tracking dock with synchronized snapshot refresh.
*   **Human-in-the-Loop Flood Reporting & Real-Time Map Bridge (S8-T7)**: Commuters can report real-time street flooding directly from the camera modal with sliding rate limits; reports bridge live to SSE map streams and spatial hex aggregators, highlighting cameras with high-contrast amber markers (`[Báo cáo từ người dân: Có ngập]`), 15-minute sliding TTL, and multi-reporter escalation ($\ge 2$ distinct IP hashes within 10 minutes).
*   **Directional Multi-Lane Taxonomy & Physical Flood Markers**: Classifies traffic density independently by direction (Direction A approaching vs Direction B receding) to resolve asymmetric divided boulevards, and anchors flood severity strictly to physical tire/wheel depth markers.
*   **Responsive Layout (100dvh)**: Desktop, tablet, and smartphone friendly with collapsible legend and mobile bottom-sheet drawer.
*   **Go Backend**: Single static binary, no runtime dependency install, in-memory + SQLite state, goroutine-based Hot Set scheduler.
*   **Cloudflare R2 Object Storage**: S3-compatible cloud storage (`warded-proofs`) for snapshot proofs, distributed via edge CDN (`https://file.529studio.site`) for centralized team labeling workflows.

### Microservice Architecture & Host Topology

| Service | Technology | Port | Host & Node Role | Primary Responsibilities |
|---|---|:---:|---|---|
| **Web Client** (`apps/web`) | Next.js 15, React 19, Tailwind CSS, Leaflet | `3001` | `edge host` | Map visualization, discrete camera marker dots, H3 hex overlays, horizontal corridor dock, bilingual UI (`vi` / `en`). |
| **Backend API** (`services/go-server`) | Go 1.26, Gin, SQLite (WAL mode) | `3000` | `edge host` | Snapshot proxy cache (15s TTL), camera catalog, hotset scheduler (30s cycle), crowdsourced flood report archiver (`internal/feedback`), PCU Saturation Index, SSE stream, Cloudflare R2 storage integration. |
| **CV Engine** (`services/cv-engine`) | Python 3.11, ThreadingHTTPServer, OpenVINO IR, YOLOv8n-seg FP16 | `8000` | `inference worker` | High-throughput vehicle detection & road water segmentation (`POST /predict`), Dual-Layer Physical Triangulation (Layer 1 surface water ratio, Layer 2 motorcycle wheel submersion), `GET /healthz` healthchecks (< 38.5ms latency). |
| **Storage & Edge CDN** | Cloudflare R2, AWS Signature V4 | `443` | Cloudflare Edge (`file.529studio.site`) | S3-compatible snapshot proof repository (`warded-proofs`), Vietnam ICT (`UTC+7`) key hierarchy (`YYYY-MM-DD/<camera_id>/<unix_ts>_<reason>.jpg`). |

### Tech Stack & Core Libraries

| Layer | Technologies & Badges | Purpose |
|---|---|---|
| **Frontend** | [![Next.js](https://img.shields.io/badge/Next.js-15.5+-000000?logo=nextdotjs&logoColor=white)](https://nextjs.org/) [![TypeScript](https://img.shields.io/badge/TypeScript-5.0+-3178C6?logo=typescript&logoColor=white)](https://www.typescriptlang.org/) [![Tailwind CSS](https://img.shields.io/badge/Tailwind_CSS-3.4+-06B6D4?logo=tailwindcss&logoColor=white)](https://tailwindcss.com/) [![Framer Motion](https://img.shields.io/badge/Framer_Motion-11+-0055FF?logo=framer&logoColor=white)](https://www.framer.com/motion/) [![Leaflet](https://img.shields.io/badge/Leaflet-1.9+-199900?logo=leaflet&logoColor=white)](https://leafletjs.com/) | Next.js App Router, TypeScript, Tailwind CSS, Framer Motion animations, Leaflet interactive mapping |
| **Backend** | [![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&logoColor=white)](https://go.dev/) [![Gin](https://img.shields.io/badge/Gin-1.10+-008ECF?logo=go&logoColor=white)](https://gin-gonic.com/) [![SQLite](https://img.shields.io/badge/SQLite-3-003B57?logo=sqlite&logoColor=white)](https://sqlite.org/) [![SSE](https://img.shields.io/badge/SSE-Live_Stream-FFA500?logo=html5&logoColor=white)](docs/ADR-001-flood-alert-architecture.md#adr-003-sprint-1-scope--live-updates-sse-hot-set-scoped-congestion-colors-phường-based-filter) | Single static binary, Gin HTTP router, SQLite state persistence, in-memory TTL proxy caching, Server-Sent Events (SSE) live streaming |
| **Cloud Storage & CDN** | [![Cloudflare R2](https://img.shields.io/badge/Cloudflare_R2-Object_Storage-F38020?logo=cloudflare&logoColor=white)](https://www.cloudflare.com/developer-platform/r2/) [![AWS SDK v2](https://img.shields.io/badge/AWS_SDK_v2-Go-FF9900?logo=amazonwebservices&logoColor=white)](https://github.com/aws/aws-sdk-go-v2) | Cloudflare R2 S3-compatible object storage (`warded-proofs`), public edge delivery (`file.529studio.site`), centralized proof snapshots repository |
| **Computer Vision** | [![Python](https://img.shields.io/badge/Python-3.11+-3776AB?logo=python&logoColor=white)](https://python.org/) [![PyTorch](https://img.shields.io/badge/PyTorch-EE4C2C?logo=pytorch&logoColor=white)](https://pytorch.org/) [![OpenCV](https://img.shields.io/badge/OpenCV-5C3EE8?logo=opencv&logoColor=white)](https://opencv.org/) [![OpenVINO](https://img.shields.io/badge/Intel_OpenVINO-Runtime-0071C5?logo=intel&logoColor=white)](https://openvino.ai/) | Python 3.11+, OpenVINO IR YOLOv8n FP16 inference, pluggable traffic congestion and flood alert analyzers |
| **Weather & Radar APIs** | [![Open-Meteo](https://img.shields.io/badge/Open--Meteo-Weather_API-FF8C00?logo=weatherdotcom&logoColor=white)](https://open-meteo.com/) [![RainViewer](https://img.shields.io/badge/RainViewer-Radar_API-0284C7?logo=radar&logoColor=white)](https://www.rainviewer.com/api.html) | Real-time weather, temperature, humidity, rainfall metrics (Open-Meteo) and live composite weather radar precipitation tiles (RainViewer) |
| **DevOps & Tooling** | [![CI](https://img.shields.io/badge/GitHub_Actions-CI-2ea44f?logo=githubactions&logoColor=white)](https://github.com/features/actions) [![License](https://img.shields.io/badge/License-AGPL--3.0-blue)](LICENSE) [![Geo-Enrich](https://img.shields.io/badge/Geo--Enrich-Point--in--Polygon-708090?logo=python&logoColor=white)](tools/geo-enrich) | GitHub Actions CI/CD workflows, golangci-lint, ESLint, unified Makefile, batch Phường point-in-polygon enrichment |

### Configuration Reference

#### Backend Service (`services/go-server`)

| Variable | Default Value | Description |
|---|---|---|
| `PORT` | `3000` | HTTP service port |
| `STORAGE_DRIVER` | `local` | Storage driver (`local` for disk or `r2` / `s3` for Cloudflare R2) |
| `STORAGE_LOCAL_ROOT` | `./data/proofs` | Root directory for local snapshot archiving |
| `R2_ACCOUNT_ID` | `<your-account-id>` | Cloudflare Account Identifier |
| `R2_ENDPOINT` | `https://<account_id>.r2.cloudflarestorage.com` | Base S3 API endpoint for Cloudflare R2 |
| `R2_BUCKET_NAME` | `warded-proofs` | Object storage bucket name |
| `R2_REGION` | `auto` | R2 signing region |
| `R2_PUBLIC_URL` | `https://file.529studio.site` | Cloudflare Edge CDN custom domain |
| `CV_ENGINE_URL` | `http://localhost:8000` | URL of self-hosted CV inference worker |
| `ANALYZER` | `none` | Traffic analyzer (`none`, `mock`, `vlm`, `onnx`) |
| `SNAPSHOT_UPSTREAM_BASE_URL` | *(required)* | DOT upstream image snapshot endpoint |
| `TELEGRAM_BOT_TOKEN` | `""` | Telegram Bot API token for flood warning dispatch (empty for simulation mode) |
| `TELEGRAM_DEBOUNCE_MINUTES` | `30` | Anti-spam quiet period debounce window per chokepoint (in minutes) |
| `HARVESTER_ENABLED` | `false` | Enable background dual-trigger scheduled & rain harvester |
| `HARVESTER_SCHEDULE_HOURS` | `7,12,17` | Daily peak harvest schedule in ICT (UTC+7, e.g. `07:30,12:00,17:30`) |
| `HARVESTER_RAIN_COOLDOWN_MINUTES` | `60` | Sliding window cooldown for rain watcher triggers (in minutes) |
| `HARVESTER_CONCURRENCY` | `15` | Worker pool concurrency for frame ingestion & R2 streaming |
| `HARVESTER_CATCHUP_MINUTES` | `120` | Boot-time catch-up window: slots missed by a late start or restart fire once at startup (0 disables) |

#### CV Model Service (`services/cv-engine`)

| Parameter | Configuration | Description |
|---|---|---|
| **Host Worker** | `inference worker` | Dedicated multi-core host with private mesh VPN |
| **Model Runtime** | Intel OpenVINO 2024.4 IR | Optimized FP16 vehicle detection engine (~6.3 MB) |
| **Input Resolution** | 416x416 (with letterbox preprocessing & NMS) | Low-latency inference geometry with aspect ratio preservation and box deduplication |
| **Resource Caps** | 2 vCPU / 2 GB RAM | Resource limits configured via Docker Compose stack |
| **API Endpoints** | `POST /predict`, `GET /healthz` | Prediction endpoint accepting JPEG bytes and returning vehicle counts |

#### Operational Verification: Staging Telegram Flood Alerts

Before enabling public subscriptions, verify live alert delivery on the staging
stack with the pre-flight script:

```bash
# Credentials are read from environment variables or the gitignored .env (never hardcoded)
TELEGRAM_BOT_TOKEN=<your-telegram-bot-token> TELEGRAM_CHAT_ID=<your-telegram-chat-id> \
  ./scripts/verify_staging_telegram.sh
```

The script validates the bot token (`getMe`), the bilingual zero-emoji message
contract, and live `sendMessage` delivery (HTTP 200). The 30-minute sliding
debouncer and token redaction are enforced deterministically by
`go test -race ./cmd/server -run TestNotifyIntegration`.

## 4. Next Evolution Steps

Detailed architectural decisions are documented in:
**[`docs/SYSTEM_DESIGN.md`](docs/SYSTEM_DESIGN.md)** — original caching & CV roadmap
**[`docs/ADR-001-flood-alert-architecture.md`](docs/ADR-001-flood-alert-architecture.md)** — flood-alert feasibility, notification channels, and the Go backend migration decision (ADR-002)
**[`docs/ADR-009-zero-vlm-flood-segmentation.md`](docs/ADR-009-zero-vlm-flood-segmentation.md)** — self-hosted flood segmentation, OpenVINO model selection benchmarks, and dual-layer physical triangulation (Zero-VLM)

*   **Computer Vision Integration**: Traffic congestion + flood detection via a **Pluggable Adapter** pattern (Mock ➔ Cloud Vision API ➔ Self-hosted YOLO/ONNX), orchestrated from Go, model inference in Python.
*   **Real-Time Flood Alerting**: Zone-based subscriptions with a pluggable **Notification Adapter** — Telegram (Phase 1), Zalo OA (Phase 2), Web Push, SMS — because a rider on a motorbike cannot open a browser tab to check a map.
## License

This project is licensed under the **GNU Affero General Public License v3.0**
(AGPL-3.0). See [`LICENSE`](LICENSE) for the full text.

AGPL-3.0 is a copyleft license with a network clause: if you run a modified
version of this software as a network service, you must make the corresponding
source code available to the users of that service. Self-hosting an unmodified
copy requires no additional obligation.

- Contributions are accepted under the same license, see [`CONTRIBUTING.md`](CONTRIBUTING.md).
- Security reports follow [`SECURITY.md`](SECURITY.md).

