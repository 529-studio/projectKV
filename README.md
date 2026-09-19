# CAMERA VIEW — Ho Chi Minh City Traffic Cameras & Integrated Features

> **A real-time Ho Chi Minh City traffic camera exploration and monitoring web application featuring low latency, a clean minimalist UI, chained hierarchical filtering, a multi-camera tracking dock, and support for integrated analytical features.**

Organized as a **clean monorepo**, the project decouples the Web Client application (`apps/web`) from the Node.js backend service (`services/node-server`), prepared for extending into image analysis and Computer Vision services in the future.

---

## 1. Project Directory Structure (Monorepo)

```
hcm-traffic-cameras/
│
├── apps/
│   └── web/                           # [FRONTEND] Standalone Web Client (@camera-view/web)
│       ├── index.html                 # Fast-loading, clean & responsive interface
│       ├── styles.css                 # Optimized CSS for Desktop, Tablet & Mobile
│       ├── app.js                     # Leaflet map, Chained Deep Filter, Marker scaling, Tracking dock
│       └── package.json
│
├── services/
│   └── node-server/                   # [BACKEND] Node.js Express server & proxy cache
│       ├── server.js                  # API & Snapshot proxy (Port 3000)
│       └── package.json               # Isolated dependencies (express, dotenv)
│
├── tools/
│   └── crawler/                       # [DATA CRAWLER]
│       └── get_all_cameras.js         # Camera database scraper (837 cameras from HCMC DOT)
│
├── docs/                              # [DOCUMENTATION]
│   └── SYSTEM_DESIGN.md               # System architecture, Hot Set pool & roadmap
│
├── cameras_all.json                   # Database of 837 traffic cameras
├── Makefile                           # Unified project commands (dev-all, dev-node, dev-web...)
├── LICENSE                            # Strict proprietary, reference-only license
└── README.md                          # Project documentation
```

---

## 2. Quick Start

The project is optimized for rapid local setup using **Makefile**:

```bash
# Install backend dependencies (run once initially)
make install

# [RECOMMENDED] Start all services concurrently (Backend :3000 & Frontend Dev :3001)
make dev-all

# Or start only the Node.js backend (Port 3000 - serves both API and Web UI)
make dev-node

# (Optional) Start standalone static server for frontend (Port 3001)
make dev-web

# Fetch & update latest camera list from the traffic portal
make crawl
```

*   **Browser Access**:
    *   Full Application (Backend + Web): `http://localhost:3000`
    *   Standalone Frontend Dev: `http://localhost:3001`

---

## 3. Key Features

*   **Clean & Minimalist Interface**: High-contrast, monochromatic design (black / dark gray / white) that eliminates visual noise and focuses 100% on live camera feeds.
*   **Standard OSM Tile Layer**: Clean OpenStreetMap tiles without commercial watermarks, with viewports restricted to mainland Southeast Vietnam (`maxBounds`).
*   **Chained Hierarchical Filter**:
    *   Multi-tier filtering: *Zone* ➔ *Corridor / Street* ➔ *Intersection / Node*.
    *   Dynamic facet algorithm calculates camera counts on-the-fly and automatically sorts by the densest coverage.
*   **Corridor Tracking Dock**:
    *   Click any camera to view live snapshot updates immediately.
    *   Click `[+ GHIM THEO DÕI]` to pin cameras to the bottom tracking dock.
    *   Automatic 10-second background refresh synchronized through backend proxy cache to prevent rate-limiting.
*   **Responsive Across All Devices (100dvh)**: Seamless experience across ultra-wide monitors, laptops, tablets, and smartphones (collapsible sidebar for mobile screens).

---

## 4. Architecture & Future Roadmap

Detailed system architecture and scalability specifications are documented in:
📄 **[`docs/SYSTEM_DESIGN.md`](docs/SYSTEM_DESIGN.md)**

*   **Multi-User Scalability**: Hot Set Pool architecture — whether 10 or 10,000 concurrent users view the same camera, the system only sends 1 snapshot request and 1 inference call every 10–30 seconds.
*   **Computer Vision Integration**: Automatic queue detection, vehicle density estimation, and traffic anomaly alerts via pluggable adapter architecture (Heuristics ➔ Cloud Vision ➔ Self-hosted YOLO ONNX).

---

## 5. License

Proprietary and Source-Available (For Reference Only). See [LICENSE](LICENSE) for full terms.
