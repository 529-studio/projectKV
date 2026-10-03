#!/usr/bin/env python3
import os
import tempfile
import unittest
from storm_io_benchmark import WorkerMetrics, execute_workload


class TestStormIOBenchmark(unittest.TestCase):
    def test_worker_metrics_summary(self):
        m = WorkerMetrics("test")
        m.record(10.0, 1024 * 1024)
        m.record(20.0, 1024 * 1024)
        m.record(30.0, 1024 * 1024)
        summary = m.summarize(duration_sec=1.0)
        self.assertEqual(summary["operations"], 3)
        self.assertEqual(summary["errors"], 0)
        self.assertEqual(summary["throughput_mb_s"], 3.0)
        self.assertEqual(summary["iops"], 3.0)
        self.assertEqual(summary["latency_p50_ms"], 20.0)
        self.assertEqual(summary["latency_max_ms"], 30.0)

    def test_execute_workload_baseline(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            res = execute_workload("baseline", duration_sec=1.0, base_dir=tmpdir)
            self.assertEqual(res["mode"], "baseline")
            self.assertIn("archiver", res["workers"])
            self.assertIn("sqlite", res["workers"])
            self.assertIn("valhalla", res["workers"])
            self.assertIn("cv_worker", res["workers"])
            self.assertGreater(res["aggregate"]["total_operations"], 0)

    def test_execute_workload_storm_burst(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            res = execute_workload("storm-burst", duration_sec=1.0, base_dir=tmpdir)
            self.assertEqual(res["mode"], "storm-burst")
            self.assertGreater(res["aggregate"]["total_operations"], 0)
            self.assertGreater(res["aggregate"]["total_iops"], 0)


if __name__ == "__main__":
    unittest.main()
