#!/usr/bin/env python3
"""
dataset_progress_test.py — Unit test suite for dataset progress auditor.

Part of Sprint 4 T3 (Issue #194).
Tests schema validation, mock frame exclusion, category calculation,
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

import dataset_progress as dp  # noqa: E402


class TestDatasetProgressAudit(unittest.TestCase):
    def setUp(self):
        self.schema = dp.load_schema(EVAL_DIR / "label_schema.json")

    def test_mock_frame_exclusion(self):
        with tempfile.NamedTemporaryFile("w", suffix=".json") as tmp:
            mock_data = {
                "frame_id": "cam_5a825b7d_mock",
                "image_path": "data/proofs/2026-10-01/cam_5a825b7d/sample.jpg",
                "labels": {
                    "traffic": "CLEAR",
                    "flood": "NONE",
                    "time_of_day": "day",
                    "weather": "dry",
                },
            }
            json.dump(mock_data, tmp)
            tmp.flush()

            label = dp.parse_label_file(Path(tmp.name), self.schema)
            self.assertTrue(label.is_mock, "Frames from 2026-10-01 must be flagged as mock")

    def test_real_frame_parsing_and_validity(self):
        with tempfile.NamedTemporaryFile("w", suffix=".json") as tmp:
            real_data = {
                "frame_id": "cam_real_01",
                "image_path": "data/proofs/2026-10-08/5822b5ededeb6c0012a2d66a/1791448307_sample.jpg",
                "labels": {
                    "traffic": "CLEAR",
                    "flood": "NONE",
                    "time_of_day": "day",
                    "weather": "dry",
                },
                "annotator": "tester",
                "annotated_at": "2026-10-08T15:00:00Z",
            }
            json.dump(real_data, tmp)
            tmp.flush()

            label = dp.parse_label_file(Path(tmp.name), self.schema)
            self.assertFalse(label.is_mock)
            self.assertTrue(label.is_valid)
            self.assertEqual(label.traffic, "CLEAR")
            self.assertEqual(label.flood, "NONE")
            self.assertEqual(label.time_of_day, "day")
            self.assertEqual(label.weather, "dry")

    def test_invalid_label_value(self):
        with tempfile.NamedTemporaryFile("w", suffix=".json") as tmp:
            bad_data = {
                "frame_id": "cam_bad_01",
                "image_path": "data/proofs/2026-10-08/sample.jpg",
                "labels": {
                    "traffic": "INVALID_STATE",
                    "flood": "NONE",
                },
            }
            json.dump(bad_data, tmp)
            tmp.flush()

            label = dp.parse_label_file(Path(tmp.name), self.schema)
            self.assertFalse(label.is_valid)
            self.assertIn("Invalid traffic value", label.validation_error)

    def test_category_counting_and_stats(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            tmp_path = Path(tmpdir)

            # Create 1 clear/dry/day frame
            f1 = {
                "frame_id": "real_01",
                "image_path": "data/proofs/2026-10-08/sample1.jpg",
                "labels": {"traffic": "CLEAR", "flood": "NONE", "weather": "dry", "time_of_day": "day"},
            }
            with open(tmp_path / "real_01.json", "w") as f:
                json.dump(f1, f)

            # Create 1 flood shallow / rain frame
            f2 = {
                "frame_id": "real_02",
                "image_path": "data/proofs/2026-10-08/sample2.jpg",
                "labels": {"traffic": "HEAVY", "flood": "SHALLOW", "weather": "rain", "time_of_day": "night"},
            }
            with open(tmp_path / "real_02.json", "w") as f:
                json.dump(f2, f)

            # Create 1 mock frame (pre-10-06)
            f3 = {
                "frame_id": "fixture_01",
                "image_path": "tools/eval/fixtures/frames/fixture_01.jpg",
                "labels": {"traffic": "CLEAR", "flood": "NONE"},
            }
            with open(tmp_path / "fixture_01.json", "w") as f:
                json.dump(f3, f)

            labels, stats = dp.audit_labels(tmp_path, self.schema)
            self.assertEqual(stats["real_valid_labels"], 2)
            self.assertEqual(stats["mock_labels_excluded"], 1)
            self.assertEqual(stats["flood_positive_count"], 1)
            self.assertFalse(stats["is_d6_gate_ready"])

            cat_map = {c["key"]: c["count"] for c in stats["categories"]}
            self.assertEqual(cat_map["clear_dry_day"], 1)
            self.assertEqual(cat_map["flood_shallow"], 1)
            self.assertEqual(cat_map["rain"], 1)
            self.assertEqual(cat_map["night"], 1)

    def test_cli_execution_and_reports(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            out_md = Path(tmpdir) / "progress.md"
            out_json = Path(tmpdir) / "progress.json"

            cmd = [
                sys.executable,
                str(EVAL_DIR / "dataset_progress.py"),
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
                self.assertIn("real_valid_labels", data)
                self.assertIn("categories", data)


if __name__ == "__main__":
    unittest.main()
