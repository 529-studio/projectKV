# Contributor Onboarding Guide

**Project:** ProjectKV (Ho Chi Minh City Real-Time Urban Traffic & Flood Monitoring)  
**System Name:** Warded (`warded-backend`, `warded-frontend`, `warded-model`)  

---

## 1. Executive Overview & System Purpose

ProjectKV provides a real-time, zero-click urban traffic and flood condition explorer for Ho Chi Minh City, Vietnam. Commuters explore live streaming camera snapshots, corridor traffic saturation levels, and rain/flood layers before traveling to avoid bottlenecks and submerged streets.

---

## 2. Microservice Architecture & Port Allocations

| Service | Technology | Port | Host & Node Role | Primary Responsibilities |
|---|---|:---:|---|---|
| **Web Client** (`apps/web`) | Next.js 15 (App Router), React 19, Tailwind CSS, Leaflet | `3001` | `edge host` (Portainer Endpoint 3) | Map visualization, discrete camera marker dots with dynamic arterial saturation coloring (Green/Yellow/Red dots), H3 hex overlays, offline camera toggle, horizontal drag-to-scroll pinned camera dock, bilingual UI (`vi` default with full diacritics, `en` secondary). |
| **Backend API** (`services/go-server`) | Go 1.24, Gin, SQLite (WAL mode) | `3000` | `edge host` (Portainer Endpoint 3) | Snapshot proxy cache with 15s in-memory TTL, camera catalog, hotset scheduler (30s cycle, `internal/hotset`), autonomous harvester & Cloudflare R2 archiver (`internal/harvester`), PCU Saturation Index calculator, Telegram flood alert dispatcher (`internal/notify`), SSE (`/api/live/stream`), WebSockets (`/ws/live`), Weather forecast (`/api/weather`), Swagger docs. |
| **CV Engine** (`services/cv-engine`) | Python 3.11, ThreadingHTTPServer, OpenVINO IR runtime, YOLOv8n FP16 | `8000` | `inference worker` worker (Portainer Endpoint 4) | Automated vehicle detection (`POST /predict`), class counting (motorcycle, car, bus, truck), `GET /healthz` healthchecks, multi-threaded request processing, 2 vCPU / 2 GB RAM capped. |

---

## 3. Local Development Quickstart

For detailed prerequisites, environment variables, and setup instructions, see [`README.md`](../README.md) — English, canonical. The Vietnamese translation is [`README.vi.md`](../README.vi.md).

```bash
# 1. Start core web and backend services:
make dev-all

# 2. Start OpenVINO vehicle detection container:
colima start --cpu 2 --memory 4
make dev-model

# 3. Run automated test suites:
make test-go
make test-model
make test-eval
make test-train
make test-pcu
make test-enrich
cd apps/web && npm test
```

---

## 4. Engineering Guardrails & AI Agent Rules

All developers and AI agents must strictly adhere to the non-negotiable project rules in [`AGENTS.md`](../AGENTS.md).
Key mandates include zero-emoji UX/logs, root-cause verification, issue estimation, git flow, and holistic doc maintenance.
Refer directly to [`AGENTS.md`](../AGENTS.md) for the complete and authoritative specification of all engineering standards.

---

## 5. Documentation & Historical Reports

- **Engineering Lessons Learned Index:** [`docs/lessons/INDEX.md`](lessons/INDEX.md)
- **Historical Metrics Scorecard (as of 2026-10-30):** [`docs/reports/project-status-scorecard-2026-10-30.md`](reports/project-status-scorecard-2026-10-30.md)
