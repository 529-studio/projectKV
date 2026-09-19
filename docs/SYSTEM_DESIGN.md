# SYSTEM DESIGN & ARCHITECTURE ROADMAP (CAMERA VIEW)

This document summarizes the architectural design, engineering roadmap, and technical solutions for the **Camera View** project, covering the current streamlined monorepo phase through future high-throughput scale (Golang backend) and Computer Vision congestion intelligence.

---

## 1. Project Vision

*   **Application Name**: **CAMERA VIEW**
*   **Mission**: A low-latency Ho Chi Minh City traffic camera explorer and real-time monitoring suite with a high-contrast monochromatic interface, intelligent hierarchical chained filtering, multi-camera pin dock, and extensible modular services.
*   **Expansion Objectives**:
    1. Construct a resilient high-concurrency backend proxy & smart caching layer handling thousands of camera streams.
    2. Integrate Computer Vision (CV) to automate vehicle density estimation, queue length detection, and incident anomaly detection.

---

## 2. Monorepo Architecture

The system is organized as a lightweight, clean monorepo separating client applications, backend services, and offline tooling:

```
hcm-traffic-cameras/
│
├── apps/                                  # CLIENT APPLICATION LAYER
│   └── web/                               # Standalone Web Client (@camera-view/web)
│       ├── index.html                     # Minimalist, responsive web UI with i18n
│       ├── styles.css                     # Monochromatic CSS for Desktop, Tablet & Mobile
│       ├── app.js                         # Leaflet map, Chained Deep Filter, Pin Dock, i18n engine
│       └── package.json                   # Package configuration
│
├── services/                              # BACKEND SERVICES LAYER
│   ├── node-server/                       # [CURRENT] Lightweight Express.js server (Port 3000)
│   │   └── server.js                      # API /api/cameras & Snapshot Proxy Cache (10s TTL)
│   │
│   └── cv-engine/                         # [FUTURE ROADMAP] Computer Vision Service
│       ├── models/                        # YOLO / Detection model weights
│       ├── worker.py                      # Scheduled snapshot analysis worker
│       └── api.py                         # Congestion verdict API
│
├── tools/                                 # INTERNAL SCRIPTS & TOOLING
│   └── crawler/                           # Camera catalog updater & GPS data crawler
│       └── get_all_cameras.js
│
├── docs/                                  # DOCUMENTATION & SPECIFICATIONS
│   └── SYSTEM_DESIGN.md                   # This document
│
├── cameras_all.json                       # 837 Traffic camera catalog dataset
├── LICENSE                                # Proprietary & Source-Available license
├── Makefile                               # Centralized task orchestrator
└── README.md                              # Project documentation
```

---

## 3. Backend Evaluation & Caching Strategy

### 3.1. Architectural Assessment: Node.js (Current) vs. Golang (At Scale)
*   **Node.js Express (`services/node-server`) - Current (Recommended for Phase 1)**:
    *   *Strengths*: Zero compilation dependencies, unified JavaScript syntax shared with frontend, immediate developer onboarding.
    *   *Current Performance*: Easily handles hundreds of concurrent users thanks to in-memory Buffer caching (capped at 1 upstream fetch per 10s per camera).
    *   *Verdict*: Optimal for initial deployment, operational simplicity, and testing.
*   **Golang - Upgrade Choice for Massive Scale (High Traffic)**:
    *   *When to switch*: When scaling to thousands of cameras nationwide or tens of thousands of concurrent client connections. Goroutines and minimal memory footprint (< 30MB RAM) drastically outperform V8 under heavy parallel I/O.
    *   *Current Status*: Cleaned up unused prototype code to maintain a single source of truth in Node.js, ready to spin up a dedicated Go service when traffic justifies it.

### 3.2. Multi-Tier Caching Architecture
1. **Tier 1 - Browser Client**: 10-second client-side cache enforced via `Cache-Control: public, max-age=10`.
2. **Tier 2 - Backend Memory Cache (In-Memory Buffer)**:
   - Holds snapshot buffers in RAM for active cameras.
   - When 100 users view the same camera concurrently, only **1 single request** hits the upstream traffic portal every 10 seconds.
3. **Tier 3 - Edge Cache (Cloudflare CDN / Workers)**:
   - Edge caching with 15s TTL for global distribution.

### 3.3. Storage Strategy & Zero-Bloat Principle
*   **Current Production Decision**:
    - 100% **In-memory RAM Cache** (`Map` in Node.js / `sync.RWMutex` in Go) paired with static JSON dataset `cameras_all.json`.
    - **No external database required** (zero PostgreSQL, Redis, or MongoDB dependencies). Developers can clone and run immediately with zero environment configuration.
*   **Future CV Phase**:
    - When historical time-series data or hourly congestion reports are required, embedded SQLite (`data/traffic.db`) or Redis will be evaluated.

---

## 4. Computer Vision (CV Engine) Roadmap

### 4.1. Business Objectives
*   **Vehicle Density Estimation**: Classify roadway density into CLEAR, MODERATE, HEAVY, and CONGESTED.
*   **Queue Length Detection**: Identify truck and container queues awaiting entry into Cat Lai port and key interchanges.
*   **Incident & Anomaly Detection**: Detect waterlogged roads and stalled vehicles obstructing lanes.

### 4.2. Decoupled Compute via Hot Set Pool
To prevent system crashes when user traffic spikes, computation is completely decoupled from user requests:

1. **Hot Set Pool**:
   - The system tracks which cameras have active viewers or are pinned in the dock, plus ~30 critical arterial choke points (e.g., Cat Lai, My Thuy, An Suong).
   - Only cameras in the **Hot Set** are queued for inference.
2. **Fixed-Rate Background Worker**:
   - The worker analyzes the Hot Set on a fixed schedule (every 30–45s).
   - Whether **1 person** or **100,000 people** view a camera, inference runs **exactly once** per interval.
3. **Sub-2ms Cached API Delivery**:
   - Analysis results are stored in memory and served instantly via `GET /api/traffic-status`.
4. **Minimal Infrastructure Cost**:
   - A single cost-effective VPS running ONNX Runtime on CPU can process 50–100 Hot Set cameras continuously without costly GPU hardware.

### 4.3. Pluggable Adapter Pattern
```
┌─────────────────────────────────────────────────────────────┐
│                 STANDARD CV INTERFACE                        │
│        AnalyzeTraffic(ImageBuffer) -> TrafficVerdict        │
└─────────────────────────────────────────────────────────────┘
                               │
       ┌───────────────────────┼───────────────────────┐
       ▼                       ▼                       ▼
  [PHASE 1]               [PHASE 2]               [PHASE 3]
  Mock / Heuristic      Cloud Vision API        Self-Hosted YOLO/ONNX
  (UI Testing, $0 cost) (Gemini / OpenAI API)   (Python / Go ONNX Runtime)
```

*   **Benefit**: The Web Frontend (`apps/web`) and API contracts remain completely unchanged as the underlying analysis backend evolves.

---

## 5. Current Implementation Status

1. [x] Monorepo restructuring (`apps/web`, `services/node-server`, `tools/crawler`).
2. [x] Monochromatic UI & OpenStreetMap integration without watermarks.
3. [x] In-memory proxy snapshot cache (10s TTL).
4. [x] Dynamic Chained Deep Filter (Zone, Corridor, Intersection, Status).
5. [x] Multi-camera pin dock with 10s auto-refresh.
6. [x] Two-tier branch protection (`.githooks/pre-push` + GitHub Actions `branch-guard.yml`).
7. [x] Internationalization (`i18n`) for Web Client with bilingual support (VI/EN).
8. [ ] Implementation of mock congestion API (`/api/traffic-status`).
9. [ ] CV worker pipeline prototype in `services/cv-engine/`.
