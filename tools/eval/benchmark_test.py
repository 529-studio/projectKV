#!/usr/bin/env python3
"""
benchmark_test.py — Test suite for evaluation harness and benchmark script.

Tests schema validation, metrics computation, flood safety gate logic,
end-to-end fixture execution, and CLI invocation.
"""

import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

# Add tools/eval to sys.path
EVAL_DIR = Path(__file__).resolve().parent
REPO_ROOT = EVAL_DIR.parent.parent
sys.path.insert(0, str(EVAL_DIR))

import benchmark  # noqa: E402


class TestLabelSchemaAndParser(unittest.TestCase):
    def setUp(self):
        self.schema = benchmark.load_schema(EVAL_DIR / "label_schema.json")

    def test_schema_structure(self):
        self.assertIn("traffic", self.schema)
        self.assertIn("flood", self.schema)
        self.assertIn("time_of_day", self.schema)
        self.assertIn("weather", self.schema)
        self.assertEqual(self.schema["traffic"], ["CLEAR", "MODERATE", "HEAVY", "CONGESTED"])
        self.assertEqual(self.schema["flood"], ["NONE", "SHALLOW", "DEEP"])

    def test_parse_valid_fixture_labels(self):
        fixtures_dir = EVAL_DIR / "fixtures" / "labels"
        frames = benchmark.load_dataset(fixtures_dir, self.schema, REPO_ROOT)
        self.assertEqual(len(frames), 5)
        for frame in frames:
            self.assertIn(frame.traffic, self.schema["traffic"])
            self.assertIn(frame.flood, self.schema["flood"])
            self.assertIn(frame.time_of_day, self.schema["time_of_day"])
            self.assertIn(frame.weather, self.schema["weather"])

    def test_parse_invalid_labels_raises(self):
        with tempfile.NamedTemporaryFile("w", suffix=".json") as tmp:
            bad_data = {
                "frame_id": "bad_01",
                "traffic": "INVALID_TRAFFIC",
                "flood": "NONE",
                "time_of_day": "day",
                "weather": "dry",
            }
            json.dump(bad_data, tmp)
            tmp.flush()

            with self.assertRaises(ValueError) as ctx:
                benchmark.parse_label_file(Path(tmp.name), self.schema, REPO_ROOT)
            self.assertIn("Invalid traffic value", str(ctx.exception))


class TestMetricsComputation(unittest.TestCase):
    def test_confusion_matrix_and_class_metrics(self):
        # 4 samples:
        # 1: CLEAR -> CLEAR (TP)
        # 2: CLEAR -> MODERATE (FN for CLEAR, FP for MODERATE)
        # 3: MODERATE -> MODERATE (TP)
        # 4: CONGESTED -> CONGESTED (TP)
        y_true = ["CLEAR", "CLEAR", "MODERATE", "CONGESTED"]
        y_pred = ["CLEAR", "MODERATE", "MODERATE", "CONGESTED"]
        classes = ["CLEAR", "MODERATE", "HEAVY", "CONGESTED"]

        cm = benchmark.compute_confusion_matrix(y_true, y_pred, classes)
        self.assertEqual(cm.get("CLEAR", "CLEAR"), 1)
        self.assertEqual(cm.get("CLEAR", "MODERATE"), 1)
        self.assertEqual(cm.get("MODERATE", "MODERATE"), 1)
        self.assertEqual(cm.get("CONGESTED", "CONGESTED"), 1)

        metrics = benchmark.compute_class_metrics(cm, classes)

        # CLEAR: TP=1, FP=0, FN=1, Support=2 => Precision=1.0, Recall=0.5, F1=0.6667
        self.assertEqual(metrics["CLEAR"].tp, 1)
        self.assertEqual(metrics["CLEAR"].fn, 1)
        self.assertEqual(metrics["CLEAR"].fp, 0)
        self.assertAlmostEqual(metrics["CLEAR"].precision, 1.0)
        self.assertAlmostEqual(metrics["CLEAR"].recall, 0.5)
        self.assertAlmostEqual(metrics["CLEAR"].f1, 2 * 1.0 * 0.5 / 1.5)

        # MODERATE: TP=1, FP=1, FN=0, Support=1 => Precision=0.5, Recall=1.0
        self.assertEqual(metrics["MODERATE"].tp, 1)
        self.assertEqual(metrics["MODERATE"].fp, 1)
        self.assertAlmostEqual(metrics["MODERATE"].precision, 0.5)
        self.assertAlmostEqual(metrics["MODERATE"].recall, 1.0)

        # HEAVY: TP=0, FP=0, FN=0, Support=0 => Precision=0.0, Recall=0.0, F1=0.0
        self.assertEqual(metrics["HEAVY"].tp, 0)
        self.assertEqual(metrics["HEAVY"].support, 0)
        self.assertEqual(metrics["HEAVY"].precision, 0.0)
        self.assertEqual(metrics["HEAVY"].recall, 0.0)

    def test_summary_f1(self):
        metrics = {
            "A": benchmark.ClassMetric(cls_name="A", tp=2, fp=0, fn=0, support=2),  # F1 = 1.0
            "B": benchmark.ClassMetric(cls_name="B", tp=1, fp=1, fn=0, support=1),  # P=0.5, R=1.0, F1=0.6667
            "C": benchmark.ClassMetric(cls_name="C", tp=0, fp=0, fn=0, support=0),  # Support=0, omitted from macro
        }
        macro, weighted = benchmark.compute_summary_f1(metrics)
        expected_macro = (1.0 + (2 * 0.5 * 1.0 / 1.5)) / 2.0
        self.assertAlmostEqual(macro, expected_macro, places=4)


class TestFloodSafetyGate(unittest.TestCase):
    def test_flood_safety_gate_passing(self):
        frames = [
            benchmark.LabeledFrame("f1", "c1", Path("p1"), "CLEAR", "SHALLOW", "day", "rain"),
            benchmark.LabeledFrame("f2", "c2", Path("p2"), "CLEAR", "DEEP", "night", "heavy_rain"),
            benchmark.LabeledFrame("f3", "c3", Path("p3"), "CLEAR", "NONE", "day", "dry"),
        ]
        # Both floods detected correctly
        preds = [
            benchmark.Prediction("f1", "CLEAR", "SHALLOW", 0.95),
            benchmark.Prediction("f2", "CLEAR", "DEEP", 0.95),
            benchmark.Prediction("f3", "CLEAR", "NONE", 0.95),
        ]
        gate = benchmark.evaluate_flood_safety_gate(frames, preds, max_fnr=0.05)
        self.assertEqual(gate.actual_positives, 2)
        self.assertEqual(gate.detected_positives, 2)
        self.assertEqual(gate.false_negatives, 0)
        self.assertEqual(gate.false_negative_rate, 0.0)
        self.assertTrue(gate.passed)
        self.assertIn("PASS", gate.status_label)
        self.assertEqual(len(gate.missed_frames), 0)

    def test_flood_safety_gate_breached(self):
        frames = [
            benchmark.LabeledFrame("f1", "c1", Path("p1"), "CLEAR", "SHALLOW", "day", "rain"),
            benchmark.LabeledFrame("f2", "c2", Path("p2"), "CLEAR", "DEEP", "night", "heavy_rain"),
        ]
        # 1 flood detected, 1 flood missed (predicted NONE)
        preds = [
            benchmark.Prediction("f1", "CLEAR", "NONE", 0.95),  # Missed flood!
            benchmark.Prediction("f2", "CLEAR", "DEEP", 0.95),
        ]
        gate = benchmark.evaluate_flood_safety_gate(frames, preds, max_fnr=0.05)
        self.assertEqual(gate.actual_positives, 2)
        self.assertEqual(gate.detected_positives, 1)
        self.assertEqual(gate.false_negatives, 1)
        self.assertAlmostEqual(gate.false_negative_rate, 0.5)  # 50% FNR
        self.assertFalse(gate.passed)
        self.assertIn("FAIL", gate.status_label)
        self.assertEqual(len(gate.missed_frames), 1)
        self.assertEqual(gate.missed_frames[0]["frame_id"], "f1")

    def test_flood_safety_gate_zero_positives_incomplete(self):
        frames = [
            benchmark.LabeledFrame("f1", "c1", Path("p1"), "CLEAR", "NONE", "day", "dry"),
        ]
        preds = [
            benchmark.Prediction("f1", "CLEAR", "NONE", 0.95),
        ]
        gate = benchmark.evaluate_flood_safety_gate(frames, preds, max_fnr=0.05)
        self.assertEqual(gate.actual_positives, 0)
        self.assertFalse(gate.passed)
        self.assertIn("INCOMPLETE", gate.status_label)


class TestCVEngineAnalyzer(unittest.TestCase):
    def test_pcu_calculation_formula(self):
        analyzer = benchmark.CVEngineAnalyzer(base_url="http://localhost:8000", capacity_floor=25.0)

        # 10 motorcycles (2.0 PCU) + 2 cars (2.0 PCU) + 1 truck (2.0 PCU) = 6.0 PCU
        # SI = 6.0 / 25.0 = 0.24 < 0.35 => CLEAR
        counts = {"motorcycle": 10, "car": 2, "bus": 0, "truck": 1, "container": 0}
        sev, pcu, si = analyzer._calculate_pcu("cam_test", counts, 13)
        self.assertEqual(sev, "CLEAR")
        self.assertAlmostEqual(pcu, 6.0)
        self.assertAlmostEqual(si, 0.24)

        # 30 cars = 30.0 PCU. Historical capacity updates to 30.0!
        # SI = 30.0 / 30.0 = 1.00 >= 0.90 => CONGESTED
        counts2 = {"motorcycle": 0, "car": 30, "bus": 0, "truck": 0, "container": 0}
        sev2, pcu2, si2 = analyzer._calculate_pcu("cam_test", counts2, 30)
        self.assertEqual(sev2, "CONGESTED")
        self.assertAlmostEqual(pcu2, 30.0)
        self.assertAlmostEqual(si2, 1.0)

    def test_fallback_vehicle_total(self):
        analyzer = benchmark.CVEngineAnalyzer(base_url="http://localhost:8000", capacity_floor=20.0)
        sev, pcu, si = analyzer._calculate_pcu("cam_fallback", {}, 10)
        self.assertEqual(sev, "MODERATE")
        self.assertAlmostEqual(pcu, 10.0)
        self.assertAlmostEqual(si, 0.5)



class TestEndToEndBenchmark(unittest.TestCase):
    def setUp(self):
        self.schema = benchmark.load_schema(EVAL_DIR / "label_schema.json")
        self.fixtures_dir = EVAL_DIR / "fixtures" / "labels"
        self.frames = benchmark.load_dataset(self.fixtures_dir, self.schema, REPO_ROOT)

    def test_mock_analyzer_benchmark(self):
        analyzer = benchmark.MockAnalyzer()
        res = benchmark.run_benchmark(
            frames=self.frames,
            analyzer=analyzer,
            schema=self.schema,
            labels_dir_str=str(self.fixtures_dir),
            max_flood_fnr=0.05,
        )
        self.assertEqual(res.total_frames, 5)
        self.assertTrue(res.flood_gate.passed)
        self.assertAlmostEqual(res.traffic_macro_f1, 1.0)
        self.assertAlmostEqual(res.flood_macro_f1, 1.0)
        self.assertEqual(len(res.predictions), 5)

    def test_markdown_report_generation(self):
        analyzer = benchmark.MockAnalyzer()
        res = benchmark.run_benchmark(
            frames=self.frames,
            analyzer=analyzer,
            schema=self.schema,
            labels_dir_str=str(self.fixtures_dir),
            max_flood_fnr=0.05,
        )
        report_md = benchmark.generate_markdown_report(res)
        self.assertIn("# AI Analyzer Evaluation Report", report_md)
        self.assertIn("Flood False-Negative Rate (FNR)", report_md)
        self.assertIn("Dataset Composition Progress", report_md)
        self.assertIn("Traffic Classification Performance", report_md)
        self.assertIn("Traffic Confusion Matrix", report_md)
        self.assertIn("Flood Classification Performance", report_md)
        self.assertIn("Flood Confusion Matrix", report_md)
        self.assertIn("Sliced Analysis (Weather & Lighting)", report_md)

    def test_cli_execution_mock(self):
        cmd = [
            sys.executable,
            str(EVAL_DIR / "benchmark.py"),
            "--fixtures-dir",
            str(EVAL_DIR / "fixtures"),
            "--analyzer",
            "mock",
        ]
        proc = subprocess.run(cmd, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        self.assertEqual(proc.returncode, 0, f"CLI failed: {proc.stderr}")
        self.assertIn("PROJECTKV AI ANALYZER BENCHMARK SUMMARY", proc.stdout)
        self.assertIn("Safety Gate:    PASS", proc.stdout)


if __name__ == "__main__":
    unittest.main()
