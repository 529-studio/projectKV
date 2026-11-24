# Specification: Standard Prometheus / OpenMetrics Endpoint (Issue #529)

- **Issue**: [#529](https://github.com/529-studio/projectKV/issues/529) (Sprint 18 Task 6)
- **Author**: Antigravity (529 Studio)
- **Status**: Draft -> Implementation
- **Target Delivery**: Sprint 18, Day 6 (2026-11-28)
- **Active Effort Estimate**: 4–6 engineering hours (AGENTS.md Rule 3)

---

## 1. Problem Statement & Operational Rationale

As Warded orchestrates live CCTV camera snapshot ingestion, flood hazard assessments, tidal surge forecasts, and commuter corridor evaluations across Ho Chi Minh City, operators require continuous observability into:
1. **Corridor Saturation**: Number of actively evaluated and monitored traffic corridors.
2. **Ingest Queue Depth**: Snapshot retrieval and frame processing backlog depth.
3. **Computer Vision Inference Latency**: Latency distribution from `cv-engine` inference requests.
4. **Replication & Restore Drill SLAs**: Elapsed time since the last successful Litestream SQLite backup snapshot or restore drill.
5. **Camera Health / Offline Count**: Number of dead, unreachable, or stale cameras in the catalog.

Currently, operational health is inspected through ad-hoc logs and offline CLI evaluation scripts. Introducing a standardized `GET /metrics` endpoint in Prometheus / OpenMetrics exposition format enables automated scraping by Prometheus, alerting via Alertmanager, and visualization on Grafana dashboards.

---

## 2. Technical Architecture & Modulith Design

### A. Pure Go Leaf Modulith Package (`services/go-server/internal/metrics`)
- Enforces leaf modulith package rules (no imports of sibling packages under `internal/`).
- Standard library only (`sync`, `sync/atomic`, `fmt`, `io`, `net`, `net/http`, `sort`, `time`) and `github.com/gin-gonic/gin` for router integration. Zero third-party telemetry frameworks (GORM, client_golang, etc.).
- **Data Structures**:
  - `MetricType`: `Counter`, `Gauge`, `Histogram`.
  - `Gauge`: Thread-safe float64 gauge with `Set(float64)`, `Add(float64)`, `Get() float64`.
  - `Counter`: Monotonically increasing float64 counter with `Inc()`, `Add(float64)`, `Get() float64`.
  - `Histogram`: Exponential or linear bucketed histogram with `Observe(float64)`.
  - `GaugeFunc`: Dynamically evaluated telemetry function (`func() float64`) executed during scrape.
  - `Registry`: Thread-safe metric collection registry producing compliant OpenMetrics / Prometheus text output (`text/plain; version=0.0.4; charset=utf-8`).

### B. Core Telemetry Metric Catalog

| Metric Name | Type | Description |
| :--- | :--- | :--- |
| `warded_corridor_active_count` | Gauge | Count of active monitored corridors |
| `warded_ingest_queue_depth` | Gauge | Depth of snapshot ingestion / processing queue |
| `warded_cv_inference_latency_ms` | Gauge / Histogram | Inference latency observed from cv-engine in ms |
| `warded_litestream_lag_seconds` | Gauge | Seconds elapsed since last successful replication / backup drill |
| `warded_camera_offline_count` | Gauge | Count of cameras marked offline or stale |
| `warded_http_requests_total` | Counter | Total HTTP requests handled labeled by method, path, status |
| `warded_http_request_duration_seconds` | Histogram | Latency histogram for HTTP requests |
| `warded_uptime_seconds` | Gauge | Process uptime in seconds |

### C. Access Security & Network Protection
- In production (`AppEnv == "production"`):
  - Access is restricted to trusted internal networks (Loopback CIDRs `127.0.0.1/32`, `::1/128` and Private RFC 1918 CIDRs `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`).
  - External scrape requests must provide a valid Bearer token (`Authorization: Bearer <METRICS_AUTH_TOKEN>`).
  - Unauthorized requests receive `401 Unauthorized` or `403 Forbidden`.
- In non-production environments (`AppEnv != "production"`):
  - Open for local developer inspection and CI verification.

---

## 3. Acceptance Criteria
- [x] `internal/metrics` implemented as a zero-dependency leaf package under `services/go-server`.
- [x] Standard Prometheus exposition format with valid `# HELP` and `# TYPE` headers.
- [x] Thread-safe concurrent reads and writes with zero data races (`go test -race`).
- [x] Memory leak audit proves metrics collection incurs zero unbounded memory growth.
- [x] Network security protection restricts unauthorized production scrapes.
- [x] Integrated into `cmd/server` composition root and mounted at `GET /metrics`.
- [x] 100% passing tests across unit, integration, and CI gates.
