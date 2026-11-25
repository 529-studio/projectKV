#!/usr/bin/env python3
"""
Unit and concurrency tests for tools/eval/benchmark_int8_stress.py (Issue #555 / Sprint 19 Task 7).
"""

import unittest

from tools.eval.benchmark_int8_stress import (
    compute_percentiles,
    create_minimal_valid_jpeg,
    run_stress_benchmark,
    start_local_int8_harness_server,
)


class TestBenchmarkINT8Stress(unittest.TestCase):
    def test_compute_percentiles(self):
        res = compute_percentiles([10.0, 20.0, 30.0, 40.0, 50.0, 60.0, 70.0, 80.0, 90.0, 100.0])
        self.assertEqual(res["p50"], 50.0)
        self.assertEqual(res["p90"], 90.0)
        self.assertEqual(res["p95"], 100.0)
        self.assertEqual(res["min"], 10.0)
        self.assertEqual(res["max"], 100.0)

        empty = compute_percentiles([])
        self.assertEqual(empty["p95"], 0.0)

    def test_minimal_jpeg_valid_markers(self):
        jpg = create_minimal_valid_jpeg()
        self.assertTrue(jpg.startswith(b"\xff\xd8"))
        self.assertTrue(jpg.endswith(b"\xff\xd9"))

    def test_60_camera_concurrent_stress_harness(self):
        srv, endpoint, _ = start_local_int8_harness_server()
        try:
            report = run_stress_benchmark(
                endpoint=endpoint,
                concurrency=60,
                total_requests=180,
                timeout_s=5.0,
            )
        finally:
            srv.shutdown()
            srv.server_close()

        self.assertEqual(report["concurrency"], 60)
        self.assertEqual(report["total_requests"], 180)
        self.assertEqual(report["ok_count"], 180)
        self.assertEqual(report["error_count"], 0)
        self.assertLess(report["latency_ms"]["p95"], 200.0)
        self.assertGreater(report["throughput_fps"], 10.0)

    def test_cv_server_backlog_supports_60_camera_burst(self):
        import sys
        from pathlib import Path

        cv_dir = str(Path(__file__).resolve().parents[2] / "services/cv-engine")
        if cv_dir not in sys.path:
            sys.path.insert(0, cv_dir)
        import server as cv_server  # type: ignore

        self.assertGreaterEqual(cv_server.CVServer.request_queue_size, 128)


if __name__ == "__main__":
    unittest.main()
