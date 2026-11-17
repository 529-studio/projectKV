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
| **Web Client** (`apps/web`) | Next.js 15, React 19, Tailwind CSS, Leaflet | `3001` | `edge host` | Map visualization, arterial saturation dots, H3 hex overlays, pinned camera dock, bilingual UI (`vi`/`en`). |
| **Backend API** (`services/go-server`) | Go 1.24, Gin, SQLite (WAL mode) | `3000` | `edge host` | Snapshot proxy cache (15s TTL), camera catalog, hotset scheduler, R2 archiver, PCU calculator, Telegram alerts, SSE, WebSockets. |
| **CV Engine** (`services/cv-engine`) | Python 3.11, OpenVINO IR, YOLOv8n FP16 | `8000` | `inference worker` | Vehicle detection (`POST /predict`), class counting, `GET /healthz`, 2 vCPU / 2 GB RAM capped. |

---

## 3. Local Development Quickstart

For detailed prerequisites, environment variables, and setup instructions, see [`README.md`](../README.md) — English, canonical. A Vietnamese quickstart covering prerequisites, launch commands and service ports is in [`README.vi.md`](../README.vi.md).

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
- **Inference worker capacity and load-shedding:** [`docs/runbooks/inference_worker_capacity.md`](runbooks/inference_worker_capacity.md) covers cgroup isolation, the nocturnal batch window, adaptive 320 px degradation, restart requirements, and rollback.
- **Reading large documents:** the context budget (always-loaded set ≤ 36 KB, on-demand files ≤ 25 KB) is defined in [`AGENTS.md`](../AGENTS.md) Rule 11. Enter a long document through its table of contents or a companion INDEX and read the range you need; never load it wholesale.
- **Documentation gate:** `make docs-lint` enforces Rule 11's budgets, unique ADR numbers, relative-link resolution, the 1 MB tracked-file ceiling and English prose in tracked markdown. `.docs-lint-baseline.txt` is a **ratchet, not a whitelist**: it records the violations that already existed when the gate landed. Fixing one means deleting its line in the same pull request; a stale line fails the gate, so the file can only shrink. Never add a line to silence a regression.
- **Sprint Guides (in flight only):** [`docs/sprints/README.md`](sprints/README.md)
- **Historical Metrics Scorecard (as of 2026-10-30):** [`docs/reports/project-status-scorecard-2026-10-30.md`](reports/project-status-scorecard-2026-10-30.md)
- **Documentation Audit & Remediation Status:** [`docs/reports/agent-context-bloat-audit-2026-10-30.md`](reports/agent-context-bloat-audit-2026-10-30.md)
