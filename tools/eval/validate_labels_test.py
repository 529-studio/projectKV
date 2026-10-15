#!/usr/bin/env python3
"""
validate_labels_test.py — Unit test suite for validate_labels.py.

Tests Draft-07 schema loading, strict validation checks, error reporting,
and CLI behavior.
"""

import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

EVAL_DIR = Path(__file__).resolve().parent
REPO_ROOT = EVAL_DIR.parent.parent
sys.path.insert(0, str(EVAL_DIR))

import validate_labels as vl  # noqa: E402


class TestValidateLabels(unittest.TestCase):
    def setUp(self):
        self.schema = vl.load_schema(EVAL_DIR / "label_schema.json")

    def test_valid_label_dict(self):
        valid_data = {
            "frame_id": "test_cam_01_123456",
            "image_path": "data/proofs/2026-10-15/test_cam_01/123456_sample.jpg",
            "labels": {
                "traffic": "MODERATE",
                "carriageway_direction": "bidirectional",
                "traffic_dir_a": "MODERATE",
                "traffic_dir_b": "CLEAR",
                "is_waiting_red_light": False,
                "queue_reason": "free_flow",
                "flood": "SHALLOW",
                "waterline_depth": "TIRE_QUARTER_SUBMERGED",
                "time_of_day": "day",
                "weather": "rain",
                "quality_tags": ["rain_on_lens"],
            },
            "annotator": "test_suite",
            "annotated_at": "2026-10-15T12:00:00Z",
        }
        errors = vl.validate_label_dict(valid_data, self.schema)
        self.assertEqual(len(errors), 0, f"Expected 0 errors, got: {errors}")

    def test_missing_required_root_field(self):
        invalid_data = {
            # frame_id is missing
            "image_path": "data/proofs/sample.jpg",
            "labels": {
                "traffic": "CLEAR",
                "flood": "NONE",
            },
            "annotator": "test_suite",
            "annotated_at": "2026-10-15T12:00:00Z",
        }
        errors = vl.validate_label_dict(invalid_data, self.schema)
        self.assertTrue(any("frame_id" in err for err in errors), f"Should flag missing frame_id: {errors}")

    def test_missing_required_nested_label(self):
        invalid_data = {
            "frame_id": "cam_01",
            "image_path": "data/proofs/sample.jpg",
            "labels": {
                "traffic": "CLEAR",
                # flood is missing
            },
            "annotator": "test_suite",
            "annotated_at": "2026-10-15T12:00:00Z",
        }
        errors = vl.validate_label_dict(invalid_data, self.schema)
        self.assertTrue(any("flood" in err for err in errors), f"Should flag missing flood: {errors}")

    def test_invalid_traffic_enum(self):
        invalid_data = {
            "frame_id": "cam_01",
            "image_path": "data/proofs/sample.jpg",
            "labels": {
                "traffic": "JAMMED",  # Not in CLEAR, MODERATE, HEAVY, CONGESTED
                "flood": "NONE",
            },
            "annotator": "test_suite",
            "annotated_at": "2026-10-15T12:00:00Z",
        }
        errors = vl.validate_label_dict(invalid_data, self.schema)
        self.assertTrue(any("JAMMED" in err or "traffic" in err for err in errors), f"Should flag invalid enum: {errors}")

    def test_invalid_waterline_depth_enum(self):
        invalid_data = {
            "frame_id": "cam_01",
            "image_path": "data/proofs/sample.jpg",
            "labels": {
                "traffic": "CLEAR",
                "flood": "SHALLOW",
                "waterline_depth": "UNDERWATER_5M",  # Invalid enum
            },
            "annotator": "test_suite",
            "annotated_at": "2026-10-15T12:00:00Z",
        }
        errors = vl.validate_label_dict(invalid_data, self.schema)
        self.assertTrue(any("waterline_depth" in err for err in errors), f"Should flag invalid waterline_depth: {errors}")

    def test_invalid_type_boolean_field(self):
        invalid_data = {
            "frame_id": "cam_01",
            "image_path": "data/proofs/sample.jpg",
            "labels": {
                "traffic": "CLEAR",
                "flood": "NONE",
                "is_waiting_red_light": "yes",  # String instead of boolean
            },
            "annotator": "test_suite",
            "annotated_at": "2026-10-15T12:00:00Z",
        }
        errors = vl.validate_label_dict(invalid_data, self.schema)
        self.assertTrue(any("is_waiting_red_light" in err for err in errors), f"Should flag boolean mismatch: {errors}")

    def test_cli_execution_success(self):
        with tempfile.NamedTemporaryFile("w", suffix=".json") as tmp:
            valid_data = {
                "frame_id": "cli_test_cam",
                "image_path": "data/proofs/cli_test.jpg",
                "labels": {
                    "traffic": "CLEAR",
                    "flood": "NONE",
                },
                "annotator": "cli_tester",
                "annotated_at": "2026-10-15T12:00:00Z",
            }
            json.dump(valid_data, tmp)
            tmp.flush()

            cmd = [sys.executable, str(EVAL_DIR / "validate_labels.py"), tmp.name, "--quiet"]
            res = subprocess.run(cmd, capture_output=True, text=True)
            self.assertEqual(res.returncode, 0, f"CLI exited with non-zero code: {res.stderr}")

    def test_cli_execution_failure(self):
        with tempfile.NamedTemporaryFile("w", suffix=".json") as tmp:
            invalid_data = {
                "frame_id": "cli_test_cam",
                # missing image_path, annotator, etc.
                "labels": {},
            }
            json.dump(invalid_data, tmp)
            tmp.flush()

            cmd = [sys.executable, str(EVAL_DIR / "validate_labels.py"), tmp.name, "--quiet"]
            res = subprocess.run(cmd, capture_output=True, text=True)
            self.assertEqual(res.returncode, 1, "CLI should return exit code 1 on invalid file")


if __name__ == "__main__":
    unittest.main()
