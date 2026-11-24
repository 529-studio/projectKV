# Lesson 529: Standard OpenMetrics Exposition & Leaf Collector Architecture

## Navigation Aid & Quick Reference
- **Domain**: Production operations, Prometheus / OpenMetrics exposition, observability, modulith leaf package architecture, CIDR subnet access control, zero-allocation telemetry caching.
- **Relevant Issues**: Issue #529 (S18-T6), Issue #401 (S12-T3 Watchdog), Issue #379 (S15-T5 Ingestion SLA).
- **Key Files**:
  - `services/go-server/internal/metrics/registry.go` (Pure Go OpenMetrics / Prometheus exposition collector: Gauge, Counter, Histogram, and deterministic label map cache)
  - `services/go-server/internal/metrics/handler.go` (HTTP route handler for `GET /metrics` with RFC 1918 CIDR subnet filtering and Bearer token fallback)
  - `services/go-server/internal/metrics/middleware.go` (Gin request middleware tracking `warded_http_requests_total` and `warded_http_request_duration_seconds`)
  - `services/go-server/internal/metrics/registry_test.go` (Unit tests verifying thread safety, atomic updates, and OpenMetrics formatting)
  - `services/go-server/internal/metrics/handler_test.go` (Unit tests verifying CIDR parsing and authorization in production vs non-production)
  - `services/go-server/internal/metrics/middleware_test.go` (Unit tests for request counting and latency histograms)
  - `services/go-server/cmd/server/metrics_integration_test.go` (End-to-end integration test with Gin router, health checks, and live scrape verification)
  - `services/go-server/cmd/server/main.go` (Wiring of dynamic metric closures over `corridorService` and `camCatalog`)
  - `services/go-server/cmd/server/routes.go` (Mounting of request middleware and `GET /metrics` endpoint)
  - `services/go-server/internal/config/config.go` (`MetricsAuthToken` and `MetricsAllowedCIDRs` configuration)
- **Core Invariants**:
  - **Leaf Package Modulith Invariant**: `internal/metrics` must remain a 100% pure leaf package with zero imports from sibling packages under `internal/`. Wiring occurs strictly at the composition root (`cmd/server/main.go`) via closures and callback functions (`RegisterGaugeFunc`).
  - **Zero Unbounded Allocation Growth**: All metric instances in `Registry` are cached by metric name and formatted label key (`byLabel map[string]map[string]MetricCollector`). Subsequent hits across HTTP endpoints reuse existing atomic counters/histograms rather than appending duplicate collectors to an ever-expanding slice.
  - **Fail-Closed Production Access Control**: In `AppEnv == "production"`, `GET /metrics` denies external unauthenticated traffic (`403 Forbidden`). Scrapes are permitted only from allowed network subnets (loopback `127.0.0.0/8`, `::1/128`, private RFC 1918 `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`) or when authenticated with `Authorization: Bearer <METRICS_AUTH_TOKEN>`. In development and test environments, access is open.
  - **Standard OpenMetrics Format**: Metric output conforms to standard Prometheus exposition syntax (`# HELP <name> <help>`, `# TYPE <name> <type>`, sorted label keys, and `+Inf` histogram buckets).

---

## 1. Context & Problem Statement

Prior to Sprint 18 Task 6, service operational telemetry (e.g. corridor saturation, camera offline drops, inference latencies, Litestream replication lag, and HTTP request throughput) was fragmented across stdout logs, operational watchdog alerts, and ad-hoc status queries. External observability platforms (such as Prometheus, Grafana, VictoriaMetrics, and Datadog) lacked a unified, standardized scraping endpoint to evaluate service-level objectives (SLOs) and trigger automated cluster scaling or failover alerts.

Issue #529 mandated a high-performance, standard OpenMetrics / Prometheus exposition endpoint at `GET /metrics` that:
1. Surfaces real-time core metrics across corridor traffic, cameras, queues, CV inference, Litestream replication, and HTTP requests.
2. Preserves the Go server modulith leaf architectural constraints (no cyclic or lateral dependencies across `internal/`).
3. Protects sensitive system metrics from public discovery via dual CIDR whitelist and Bearer token security gates in production.

---

## 2. Root Cause & Architectural Traps

### A. The Cyclic Modulith Dependency Trap
In many Go backend codebases, metrics packages eagerly import downstream business services (e.g., `corridor`, `camera`, `hotset`) to read internal state. In the ProjectKV architecture (`docs/GO_BACKEND_ARCHITECTURE.md`), doing so violates the leaf modulith rules, creating cyclic import loops or lateral coupling between domain packages.
- **Solution**: `internal/metrics` provides dynamic callback evaluation (`RegisterGaugeFunc(name, help, func() float64, labels)`). The metrics registry owns only the storage and formatting contracts. Domain binding occurs exclusively in `cmd/server/main.go` where `corridorService` and `camCatalog` are composed.

### B. The Unbounded Slice Allocation Trap
Naive metrics middlewares register counters or histograms dynamically inside the request handler:
```go
// ANTIPATTERN:
func (r *Registry) RegisterCounter(name string, labels map[string]string) *Counter {
    c := NewCounter()
    r.collectors = append(r.collectors, c) // Memory leak: grows with every HTTP request!
    return c
}
```
If every request registers a new counter with labels `{method="GET", path="/metrics", status="200"}`, `r.collectors` grows infinitely over time, leading to memory leaks and latency degradation during scrape serialization.
- **Solution**: `Registry` maintains a two-level thread-safe index `byLabel: map[string]map[string]MetricCollector`. The label key is deterministic and sorted (`{k1="v1",k2="v2"}`). Lookups return the existing atomic counter or histogram if already registered, ensuring static memory allocation after warm-up.

---

## 3. Implementation Details

### A. Registry & Prometheus Exposition (`services/go-server/internal/metrics/registry.go`)
- Thread-safe `Gauge`, `Counter`, and `Histogram` implementations using `sync/atomic` (via `math.Float64bits` / `math.Float64frombits`) and `sync.RWMutex`.
- Standard OpenMetrics formatting supporting escaping of special characters (`\`, `"`, `\n`) in label values.
- Uptime calculation helper `Uptime()` measuring uptime seconds since registry initialization.

### B. Security & Access Filtering (`services/go-server/internal/metrics/handler.go`)
- `ParseCIDRBlocks`: Validates IPv4 and IPv6 CIDR notations, auto-expanding bare IPs to `/32` or `/128`.
- In `production`: Checks `c.ClientIP()` against parsed `*net.IPNet` subnets (defaults to RFC 1918 and loopbacks) and inspects `Authorization: Bearer <token>` against `METRICS_AUTH_TOKEN`.
- In `development` / `test`: Permits open access for convenient local diagnostics and testing.

### C. HTTP Request Middleware (`services/go-server/internal/metrics/middleware.go`)
- Attaches to Gin engine pipeline.
- Records cumulative request counts (`warded_http_requests_total{method, path, status}`) and execution latency distributions (`warded_http_request_duration_seconds{method, path, status, le}`).
- Uses high-resolution wall clock duration (`time.Since(start).Seconds()`).

---

## 4. Verification & Empirical Evidence

1. **Unit & Race Verification**:
   - `services/go-server/internal/metrics/`: 12 unit tests covering gauge concurrency, atomic counters, histogram bucket accumulations, and CIDR subnet evaluation passing cleanly under `-race`.
2. **Integration Verification**:
   - `services/go-server/cmd/server/metrics_integration_test.go`: End-to-end integration test validating Gin router wiring, OpenMetrics payload formatting, and production CIDR / Bearer token gate enforcement.
3. **Live System Telemetry Verification**:
   - Live daemon execution verified via curl scraping `GET /metrics`:
     - `warded_corridor_active_count 514`
     - `warded_camera_offline_count 118`
     - `warded_uptime_seconds` actively advancing
     - `warded_http_requests_total` tracking incoming calls across `/healthz` and `/metrics`
4. **Repository Gates**:
   - `make test-go && make build-go`: 100% green with zero race conditions.
   - `make docs-lint`: 0 new violations.
   - `make evidence-lint`: 135/135 artifacts verified.
   - `make test-model`: 146 tests passed.
