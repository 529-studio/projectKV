# Lesson 554: Provisioning Grafana Dashboards for Prometheus Telemetry

## Navigation Aid & Quick Reference
- **Domain**: Observability, Prometheus OpenMetrics consumption, Grafana dashboard provisioning, SLO alerting thresholds.
- **Relevant Issues**: Issue #554 (S19-T6), Issue #529 (S18-T6 Prometheus `/metrics` Endpoint).
- **Key Files**:
  - `docs/ops/grafana/projectkv-operations-dashboard.json` (Grafana v10+ JSON dashboard template covering 8 core operational panels)
  - `docs/ops/grafana/README.md` (Scrape configuration, import guide, and panel SLO thresholds)
  - `tools/ops/test_grafana_dashboards.py` (Schema and PromQL metric-parity verification test)
- **Core Invariants**:
  - **100% Metric Name Parity**: Every PromQL expression in the dashboard template is checked by automated unit tests against the exact metric names registered in `services/go-server/cmd/server/main.go` and `internal/metrics/middleware.go`.
  - **Asia/Ho_Chi_Minh Timezone Alignment**: Dashboards lock timezone to `Asia/Ho_Chi_Minh` so flood rush-hour spikes correlate directly with local commuter logs.

---

## 1. Context & Observability Gap

Sprint 18 introduced a zero-dependency OpenMetrics `/metrics` endpoint protected by `METRICS_ALLOWED_CIDR` (`internal/metrics`). However, operators lacked pre-built Grafana dashboards aligned to ProjectKV's exact metric identifiers (`warded_corridor_active_count`, `warded_cv_inference_latency_ms`, `warded_litestream_lag_seconds`, `warded_http_request_duration_seconds_bucket`).

---

## 2. Verification & Empirical Proof

1. **JSON Schema & PromQL Parity Verification**:
   - Executed `python3 -m unittest tools/ops/test_grafana_dashboards.py`: 100% pass verifying Grafana schemaVersion 38 and complete coverage across all 8 registered Prometheus metrics.
