# Grafana Operations Dashboards (`docs/ops/grafana/`)

Production Grafana templates consuming OpenMetrics/Prometheus telemetry exposed by `services/go-server` at `GET /metrics`.

## 1. Prometheus Scrape Configuration

Because `/metrics` is gated by `METRICS_ALLOWED_CIDR` (defaulting to loopback `127.0.0.1/32` and private Docker/Tailnet subnets), configure Prometheus inside the monitoring subnet to scrape `warded-backend:8080`:

```yaml
scrape_configs:
  - job_name: 'warded-backend'
    scrape_interval: 15s
    metrics_path: '/metrics'
    static_configs:
      - targets: ['warded-backend:8080']
```

## 2. Importing Dashboards into Grafana

1. Navigate to **Dashboards -> New -> Import** in Grafana 10+.
2. Upload `docs/ops/grafana/projectkv-operations-dashboard.json`.
3. Select your configured **Prometheus** datasource for the `DS_PROMETHEUS` template variable.

## 3. Monitored Metrics & SLO Thresholds

| Panel | Metric Expression | SLO / Alert Threshold |
| :---- | :---------------- | :-------------------- |
| Active Monitored Corridors | `warded_corridor_active_count` | Static baseline >= 6 |
| Offline Cameras Count | `warded_camera_offline_count` | Warn >= 50, Critical >= 150 |
| Server Process Uptime | `warded_uptime_seconds` | Unexpected drops indicate OOM/crash |
| Litestream Replication Lag | `warded_litestream_lag_seconds` | Warn >= 30s, Critical >= 60s |
| CV Engine Inference Latency | `warded_cv_inference_latency_ms` | Critical >= 200ms |
| Snapshot Ingestion Queue Depth | `warded_ingest_queue_depth` | Sustained growth signals worker stall |
| HTTP Request Throughput | `rate(warded_http_requests_total[5m])` | Monitor 5xx spike ratio |
| HTTP Request Latency | `histogram_quantile(0.95, ...)` | Target P95 < 0.25s |
