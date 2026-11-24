# Implementation Plan: Standard Prometheus Metrics Endpoint (Issue #529)

- **Issue**: [#529](https://github.com/529-studio/projectKV/issues/529) (Sprint 18 Task 6)
- **Active Effort Estimate**: 4–6 engineering hours (AGENTS.md Rule 3).
- **Delivery Window**: 2026-11-28 (Sprint 18, Day 6).

---

## Proposed Changes

### 1. Leaf Package `services/go-server/internal/metrics`
- `registry.go`:
  - Thread-safe metric registry (`Registry`) supporting `Gauge`, `Counter`, `Histogram`, and `GaugeFunc`.
  - Prometheus text format serializer (`WriteOpenMetrics(w io.Writer) error`).
- `handler.go`:
  - Gin handler mounting `GET /metrics`.
  - Network CIDR filter + Bearer token security check for production environments.
  - `RegisterRoutes(r gin.IRoutes)`.
- `middleware.go`:
  - HTTP request counter and latency observer middleware for automated route telemetry.

### 2. Configuration & Wiring
- In `services/go-server/internal/config/config.go`:
  - Add `MetricsAuthToken` (env: `METRICS_AUTH_TOKEN`, fallback `DEV_ACCESS_TOKEN`).
  - Add `MetricsAllowedCIDRs` (env: `METRICS_ALLOWED_CIDRS`, default: loopback + private RFC 1918 blocks).
- In `services/go-server/cmd/server/main.go` & `routes.go`:
  - Instantiate `metrics.Registry` and register system gauges:
    - `warded_corridor_active_count`
    - `warded_ingest_queue_depth`
    - `warded_cv_inference_latency_ms`
    - `warded_litestream_lag_seconds`
    - `warded_camera_offline_count`
    - `warded_uptime_seconds`
  - Attach `metrics.Handler` to router at `GET /metrics`.

### 3. Verification & Testing
- `internal/metrics/registry_test.go`:
  - Test Gauge, Counter, Histogram, GaugeFunc concurrency and output formatting.
  - Test concurrent reads and writes (`-race`).
  - Memory leak test (100,000 iterations without unbounded growth).
- `internal/metrics/handler_test.go`:
  - Test HTTP responses, content types, CIDR filtering, and token authentication.
- `cmd/server` integration tests verifying live `/metrics` endpoint.
- Architecture review via `architecture-guardian` skill.

### 4. Living Documentation & Lesson
- Create `docs/lessons/529-prometheus-metrics-endpoint.md` (Rule 6).
