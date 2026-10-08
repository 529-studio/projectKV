#!/usr/bin/env python3
"""
verify_smoke_set_test.py — Unit test suite for smoke-test verification script.

Part of Sprint 4 T7 (Issue #187).
Tests fixture parsing, verdict ingestion formats, discrepancy detection,
and CLI execution.
"""

import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

# Add tools/eval to sys.path
EVAL_DIR = Path(__file__).resolve().parent
sys.path.insert(0, str(EVAL_DIR))

import verify_smoke_set as vss  # noqa: E402


class TestSmokeTestFixtureLoading(unittest.TestCase):
    def setUp(self):
        self.fixtures_path = EVAL_DIR / "fixtures" / "smoke_test_cameras.json"

    def test_fixture_file_exists_and_loads(self):
        self.assertTrue(self.fixtures_path.exists(), "smoke_test_cameras.json must exist")
        cameras = vss.load_smoke_cameras(self.fixtures_path)
        self.assertEqual(len(cameras), 10, "Expected exactly 10 curated smoke-test cameras")

    def test_camera_attributes_and_baselines(self):
        cameras = vss.load_smoke_cameras(self.fixtures_path)
        clear_count = sum(1 for c in cameras if c.expected_baseline_traffic == "CLEAR")
        congested_count = sum(1 for c in cameras if c.expected_baseline_traffic == "CONGESTED")

        self.assertEqual(clear_count, 5, "Expected 5 CLEAR baseline cameras")
        self.assertEqual(congested_count, 5, "Expected 5 CONGESTED baseline cameras")

        corridors = {c.corridor for c in cameras}
        expected_corridors = {"vo_van_kiet", "mai_chi_tho", "dien_bien_phu", "cong_hoa"}
        self.assertEqual(corridors, expected_corridors)

        for cam in cameras:
            self.assertTrue(cam.camera_id, "Camera ID must not be empty")
            self.assertTrue(cam.camera_name, "Camera name must not be empty")
            self.assertTrue(cam.rationale, "Rationale must not be empty")
            self.assertIn("type", cam.validation_time_window)

    def test_load_nonexistent_fixture_raises(self):
        with self.assertRaises(FileNotFoundError):
            vss.load_smoke_cameras(Path("/nonexistent/path/cameras.json"))


class TestVerdictParsingAndIngestion(unittest.TestCase):
    def test_parse_verdict_entry_string(self):
        verdict = vss.parse_verdict_entry("cam_01", "CLEAR")
        self.assertEqual(verdict.camera_id, "cam_01")
        self.assertEqual(verdict.traffic, "CLEAR")
        self.assertIsNone(verdict.saturation_index)

    def test_parse_verdict_entry_dict_with_si(self):
        data = {
            "traffic": "CONGESTED",
            "saturation_index": 0.95,
            "timestamp": "2026-10-08T10:00:00Z",
        }
        verdict = vss.parse_verdict_entry("cam_02", data)
        self.assertEqual(verdict.camera_id, "cam_02")
        self.assertEqual(verdict.traffic, "CONGESTED")
        self.assertEqual(verdict.saturation_index, 0.95)
        self.assertEqual(verdict.timestamp, "2026-10-08T10:00:00Z")

    def test_load_verdicts_from_list_file(self):
        with tempfile.NamedTemporaryFile("w", suffix=".json") as tmp:
            payload = [
                {"camera_id": "cam_a", "traffic": "CLEAR", "saturation_index": 0.2},
                {"camera_id": "cam_b", "traffic": "CONGESTED", "saturation_index": 1.1},
            ]
            json.dump(payload, tmp)
            tmp.flush()

            verdicts = vss.load_verdicts_from_file(Path(tmp.name))
            self.assertEqual(len(verdicts), 2)
            self.assertEqual(verdicts["cam_a"].traffic, "CLEAR")
            self.assertEqual(verdicts["cam_b"].traffic, "CONGESTED")

    def test_load_verdicts_from_dict_file(self):
        with tempfile.NamedTemporaryFile("w", suffix=".json") as tmp:
            payload = {
                "cam_a": {"status": "CLEAR", "si": 0.15},
                "cam_b": "CONGESTED",
            }
            json.dump(payload, tmp)
            tmp.flush()

            verdicts = vss.load_verdicts_from_file(Path(tmp.name))
            self.assertEqual(len(verdicts), 2)
            self.assertEqual(verdicts["cam_a"].traffic, "CLEAR")
            self.assertEqual(verdicts["cam_a"].saturation_index, 0.15)
            self.assertEqual(verdicts["cam_b"].traffic, "CONGESTED")


class TestVerificationLogic(unittest.TestCase):
    def setUp(self):
        self.cameras = [
            vss.SmokeCamera(
                camera_id="cam_clear",
                camera_name="Clear Intersection",
                title="T1",
                corridor="vo_van_kiet",
                road_keys=["vo_van_kiet"],
                expected_baseline_traffic="CLEAR",
                expected_si_threshold="<0.35",
                validation_time_window={"type": "off_peak"},
                rationale="Testing clear baseline",
            ),
            vss.SmokeCamera(
                camera_id="cam_congested",
                camera_name="Congested Intersection",
                title="T2",
                corridor="cong_hoa",
                road_keys=["cong_hoa"],
                expected_baseline_traffic="CONGESTED",
                expected_si_threshold=">=0.90",
                validation_time_window={"type": "peak"},
                rationale="Testing congested baseline",
            ),
        ]

    def test_verify_all_passed(self):
        verdicts = {
            "cam_clear": vss.SmokeVerdict("cam_clear", "CLEAR", 0.22),
            "cam_congested": vss.SmokeVerdict("cam_congested", "CONGESTED", 0.98),
        }
        results, stats = vss.verify_smoke_set(self.cameras, verdicts)
        self.assertTrue(stats["is_all_passed"])
        self.assertEqual(stats["passed"], 2)
        self.assertEqual(stats["failed"], 0)
        self.assertEqual(stats["missing"], 0)
        self.assertEqual(results[0].status, "PASS")
        self.assertEqual(results[1].status, "PASS")

    def test_verify_false_positive_congestion_failure(self):
        # Road is CLEAR baseline, but model reports CONGESTED
        verdicts = {
            "cam_clear": vss.SmokeVerdict("cam_clear", "CONGESTED", 0.95),
            "cam_congested": vss.SmokeVerdict("cam_congested", "CONGESTED", 0.95),
        }
        results, stats = vss.verify_smoke_set(self.cameras, verdicts)
        self.assertFalse(stats["is_all_passed"])
        self.assertEqual(stats["passed"], 1)
        self.assertEqual(stats["failed"], 1)
        self.assertEqual(results[0].status, "FAIL")
        self.assertIn("Expected CLEAR but got CONGESTED", results[0].discrepancy_details)

    def test_verify_false_negative_bottleneck_failure(self):
        # Bottleneck is CONGESTED baseline, but model reports CLEAR
        verdicts = {
            "cam_clear": vss.SmokeVerdict("cam_clear", "CLEAR", 0.20),
            "cam_congested": vss.SmokeVerdict("cam_congested", "CLEAR", 0.18),
        }
        results, stats = vss.verify_smoke_set(self.cameras, verdicts)
        self.assertFalse(stats["is_all_passed"])
        self.assertEqual(stats["passed"], 1)
        self.assertEqual(stats["failed"], 1)
        self.assertEqual(results[1].status, "FAIL")
        self.assertIn("Expected CONGESTED but got CLEAR", results[1].discrepancy_details)

    def test_verify_missing_camera(self):
        verdicts = {
            "cam_clear": vss.SmokeVerdict("cam_clear", "CLEAR", 0.20),
        }
        results, stats = vss.verify_smoke_set(self.cameras, verdicts)
        self.assertFalse(stats["is_all_passed"])
        self.assertEqual(stats["passed"], 1)
        self.assertEqual(stats["missing"], 1)
        self.assertEqual(results[1].status, "MISSING")


class TestReportingAndCLI(unittest.TestCase):
    def test_terminal_report_formatting(self):
        results = [
            vss.VerificationResult(
                camera_id="cam_01",
                camera_name="Cam 1",
                corridor="vo_van_kiet",
                expected_traffic="CLEAR",
                actual_traffic="CLEAR",
                actual_si=0.20,
                status="PASS",
            )
        ]
        stats = {
            "total_cameras": 1,
            "evaluated_cameras": 1,
            "passed": 1,
            "failed": 0,
            "missing": 0,
            "pass_rate": 100.0,
            "is_all_passed": True,
        }
        report = vss.format_terminal_report(results, stats)
        self.assertIn("PROJECTKV SMOKE-TEST CAMERA VERIFICATION REPORT", report)
        self.assertIn("[PASS]", report)
        self.assertIn("OVERALL SMOKE VERIFICATION VERDICT: PASSED", report)

    def test_markdown_report_formatting(self):
        results = [
            vss.VerificationResult(
                camera_id="cam_01",
                camera_name="Cam 1",
                corridor="vo_van_kiet",
                expected_traffic="CLEAR",
                actual_traffic="CLEAR",
                actual_si=0.20,
                status="PASS",
            )
        ]
        stats = {
            "total_cameras": 1,
            "evaluated_cameras": 1,
            "passed": 1,
            "failed": 0,
            "missing": 0,
            "pass_rate": 100.0,
            "is_all_passed": True,
        }
        md = vss.format_markdown_report(results, stats)
        self.assertIn("# Smoke-Test Camera Verification Report", md)
        self.assertIn("| `cam_01` | Cam 1 | `vo_van_kiet` |", md)

    def test_cli_mock_mode_success(self):
        cmd = [
            sys.executable,
            str(EVAL_DIR / "verify_smoke_set.py"),
            "--mock",
            "--strict",
        ]
        proc = subprocess.run(cmd, capture_output=True, text=True)
        self.assertEqual(proc.returncode, 0, f"CLI stderr: {proc.stderr}")
        self.assertIn("OVERALL SMOKE VERIFICATION VERDICT: PASSED", proc.stdout)

    def test_cli_mock_mismatch_failure(self):
        cmd = [
            sys.executable,
            str(EVAL_DIR / "verify_smoke_set.py"),
            "--mock",
            "--mock-mismatch",
            "5822b5ededeb6c0012a2d66a",
            "--strict",
        ]
        proc = subprocess.run(cmd, capture_output=True, text=True)
        self.assertEqual(proc.returncode, 1)
        self.assertIn("OVERALL SMOKE VERIFICATION VERDICT: FAILED", proc.stdout)

    def test_cli_output_files_generated(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            out_md = Path(tmpdir) / "report.md"
            out_json = Path(tmpdir) / "report.json"

            cmd = [
                sys.executable,
                str(EVAL_DIR / "verify_smoke_set.py"),
                "--mock",
                "--output-markdown",
                str(out_md),
                "--output-json",
                str(out_json),
            ]
            proc = subprocess.run(cmd, capture_output=True, text=True)
            self.assertEqual(proc.returncode, 0)
            self.assertTrue(out_md.exists())
            self.assertTrue(out_json.exists())

            with open(out_json) as f:
                data = json.load(f)
                self.assertIn("stats", data)
                self.assertIn("results", data)
                self.assertEqual(data["stats"]["passed"], 10)


if __name__ == "__main__":
    unittest.main()
