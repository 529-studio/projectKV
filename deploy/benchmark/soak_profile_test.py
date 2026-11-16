#!/usr/bin/env python3
"""
Unit tests for deploy/benchmark/soak_profile.py (S15-T5 / Issue #479).
"""

from __future__ import annotations

import json
import os
import sys
import tempfile
import threading
import time
import unittest
from datetime import datetime, timedelta, timezone
from pathlib import Path

# Add project root to sys.path
REPO_ROOT = str(Path(__file__).resolve().parents[2])
if REPO_ROOT not in sys.path:
    sys.path.insert(0, REPO_ROOT)

SERVICE_DIR = str(Path(REPO_ROOT) / "services" / "cv-engine")
if SERVICE_DIR not in sys.path:
    sys.path.insert(0, SERVICE_DIR)

import server
from server import CVServer, CVRequestHandler
from deploy.benchmark.soak_profile import (
    SoakProfileHarness,
    calculate_rss_slope,
    get_process_rss_mb,
    check_avx2_supported,
)


class TestSoakProfileCalculations(unittest.TestCase):
    def test_get_process_rss_mb(self):
        rss = get_process_rss_mb()
        self.assertIsInstance(rss, float)
        self.assertGreater(rss, 0.0)
        self.assertLess(rss, 2048.0)  # Standard test runner < 2GB

    def test_calculate_rss_slope_flat(self):
        samples = [(0.0, 100.0), (10.0, 100.0), (20.0, 100.0), (30.0, 100.0)]
        slope = calculate_rss_slope(samples)
        self.assertAlmostEqual(slope, 0.0, places=4)

    def test_calculate_rss_slope_rising(self):
        # 1 MB growth over 3600 seconds = 1.0 MB/hour
        samples = [(0.0, 100.0), (1800.0, 100.5), (3600.0, 101.0)]
        slope = calculate_rss_slope(samples)
        self.assertAlmostEqual(slope, 1.0, places=2)

    def test_calculate_rss_slope_insufficient_samples(self):
        self.assertEqual(calculate_rss_slope([(0.0, 100.0)]), 0.0)


class TestSoakProfileHarnessExecution(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        server.queue_manager.reset()
        cls.httpd = CVServer(("127.0.0.1", 0), CVRequestHandler)
        cls.port = cls.httpd.server_address[1]
        cls.httpd_thread = threading.Thread(target=cls.httpd.serve_forever, daemon=True)
        cls.httpd_thread.start()
        cls.base_url = f"http://127.0.0.1:{cls.port}/predict"

    @classmethod
    def tearDownClass(cls):
        cls.httpd.shutdown()
        cls.httpd.server_close()

    def setUp(self):
        self.tmpdir = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmpdir.cleanup)
        self.out_path = Path(self.tmpdir.name) / "test_soak_evidence.json"

    def test_harness_short_run_generates_valid_report(self):
        harness = SoakProfileHarness(
            endpoint=self.base_url,
            duration_seconds=1.0,
            fps=20.0,
            out_path=str(self.out_path),
        )
        report = harness.execute_soak()
        print("GATE CRITERIA:", report.get("gate_criteria"))
        print("MEMORY PROFILE:", report.get("memory_profile"))
        self.assertTrue(self.out_path.exists())
        self.assertEqual(report["schema_version"], 1)
        self.assertEqual(report["fit_source"], "live_history")
        self.assertEqual(report["gate_verdict"], "PASS")
        self.assertIn("window", report)
        self.assertIn("memory_profile", report)
        self.assertIn("latency_percentiles_ms", report)
        self.assertGreater(report["soak_summary"]["total_requests"], 0)


if __name__ == "__main__":
    unittest.main()
