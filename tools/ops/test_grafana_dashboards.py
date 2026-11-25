#!/usr/bin/env python3
"""
Unit tests validating Grafana dashboard JSON schemas and metric name parity
against Go backend Prometheus instrumentation (Issue #554 / Sprint 19 Task 6).
"""

import json
import unittest
from pathlib import Path

DASHBOARD_PATH = Path(__file__).resolve().parents[2] / "docs/ops/grafana/projectkv-operations-dashboard.json"

REGISTERED_METRICS = {
    "warded_corridor_active_count",
    "warded_camera_offline_count",
    "warded_uptime_seconds",
    "warded_ingest_queue_depth",
    "warded_cv_inference_latency_ms",
    "warded_litestream_lag_seconds",
    "warded_http_requests_total",
    "warded_http_request_duration_seconds_bucket",
}


class TestGrafanaDashboard(unittest.TestCase):
    def test_valid_schema_and_panels(self):
        self.assertTrue(DASHBOARD_PATH.is_file(), f"Missing dashboard JSON at {DASHBOARD_PATH}")
        data = json.loads(DASHBOARD_PATH.read_text(encoding="utf-8"))

        self.assertGreaterEqual(data.get("schemaVersion", 0), 38)
        self.assertEqual(data.get("uid"), "projectkv-ops")
        self.assertEqual(data.get("timezone"), "Asia/Ho_Chi_Minh")

        panels = data.get("panels", [])
        self.assertEqual(len(panels), 8, "Expected 8 core telemetry panels")

        referenced_exprs = []
        for panel in panels:
            self.assertIn("title", panel)
            self.assertIn("targets", panel)
            for target in panel["targets"]:
                expr = target.get("expr", "")
                self.assertTrue(expr, f"Empty PromQL expr in panel {panel['title']}")
                referenced_exprs.append(expr)

        joined = "\n".join(referenced_exprs)
        for metric in REGISTERED_METRICS:
            self.assertIn(
                metric,
                joined,
                f"Registered metric {metric} is not visualized in Grafana dashboard",
            )


if __name__ == "__main__":
    unittest.main()
