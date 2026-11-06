#!/usr/bin/env python3
"""
Unit tests for build_roadway_registry.py (S10-T3 / Issue #374).
"""

from __future__ import annotations

import json
import shutil
import sys
import tempfile
import unittest
from pathlib import Path

# Add project root to sys.path
PROJECT_ROOT = Path(__file__).resolve().parent.parent.parent
if str(PROJECT_ROOT) not in sys.path:
    sys.path.insert(0, str(PROJECT_ROOT))

from tools.eval.build_roadway_registry import (
    build_and_export_registry,
    parse_labels_into_registry,
    validate_registry_dict,
    verify_registry_dir,
    PILOT_CORRIDORS,
)


class TestBuildRoadwayRegistry(unittest.TestCase):
    def setUp(self):
        self.test_dir = Path(tempfile.mkdtemp(prefix="roadway_test_"))

    def tearDown(self):
        shutil.rmtree(self.test_dir, ignore_errors=True)

    def test_validate_registry_valid_pilot(self):
        for cam_id, data in PILOT_CORRIDORS.items():
            valid, errors = validate_registry_dict(data)
            self.assertTrue(valid, f"Pilot corridor {cam_id} failed: {errors}")
            self.assertEqual(len(errors), 0)

    def test_validate_registry_inconsistent_capacity_share(self):
        # Case 1: sum < 0.99
        bad_data_low = {
            "camera_id": "test_cam_01",
            "regions": [
                {
                    "region_id": "test_cam_01_dir_a",
                    "direction": "a",
                    "polygon": [[0.1, 0.9], [0.4, 0.9], [0.3, 0.5]],
                    "capacity_share": 0.40,
                },
                {
                    "region_id": "test_cam_01_dir_b",
                    "direction": "b",
                    "polygon": [[0.6, 0.9], [0.9, 0.9], [0.7, 0.5]],
                    "capacity_share": 0.40,
                },
            ],
        }
        valid, errors = validate_registry_dict(bad_data_low)
        self.assertFalse(valid)
        self.assertTrue(any("capacity_share sum" in e for e in errors))

        # Case 2: sum > 1.01
        bad_data_high = {
            "camera_id": "test_cam_02",
            "regions": [
                {
                    "region_id": "test_cam_02_dir_a",
                    "direction": "a",
                    "polygon": [[0.1, 0.9], [0.4, 0.9], [0.3, 0.5]],
                    "capacity_share": 0.60,
                },
                {
                    "region_id": "test_cam_02_dir_b",
                    "direction": "b",
                    "polygon": [[0.6, 0.9], [0.9, 0.9], [0.7, 0.5]],
                    "capacity_share": 0.50,
                },
            ],
        }
        valid, errors = validate_registry_dict(bad_data_high)
        self.assertFalse(valid)
        self.assertTrue(any("capacity_share sum" in e for e in errors))

    def test_validate_registry_invalid_direction(self):
        bad_dir = {
            "camera_id": "test_cam_03",
            "regions": [
                {
                    "region_id": "test_cam_03_dir_x",
                    "direction": "x",  # invalid: only 'a' or 'b'
                    "polygon": [[0.1, 0.9], [0.4, 0.9], [0.3, 0.5]],
                    "capacity_share": 1.0,
                }
            ],
        }
        valid, errors = validate_registry_dict(bad_dir)
        self.assertFalse(valid)
        self.assertTrue(any("direction must be 'a' or 'b'" in e for e in errors))

    def test_validate_registry_filename_mismatch(self):
        data = {
            "camera_id": "cam_real",
            "regions": [
                {
                    "region_id": "cam_real_dir_a",
                    "direction": "a",
                    "polygon": [[0.1, 0.9], [0.4, 0.9], [0.3, 0.5]],
                    "capacity_share": 1.0,
                }
            ],
        }
        file_path = self.test_dir / "cam_wrong.json"
        valid, errors = validate_registry_dict(data, file_path=file_path)
        self.assertFalse(valid)
        self.assertTrue(any("filename stem" in e for e in errors))

    def test_parse_labels_and_export(self):
        labels_dir = self.test_dir / "labels"
        labels_dir.mkdir()
        out_dir = self.test_dir / "output"

        # Create a mock ground-truth label with directional roadway polygons
        label_file = labels_dir / "cam123_20261106.json"
        label_content = {
            "camera_id": "cam123",
            "annotations": [
                {
                    "class": "roadway",
                    "direction": "a",
                    "capacity_share": 0.5,
                    "polygon": [[0.1, 0.9], [0.4, 0.9], [0.3, 0.5]],
                },
                {
                    "class": "roadway",
                    "direction": "b",
                    "capacity_share": 0.5,
                    "polygon": [[0.6, 0.9], [0.9, 0.9], [0.7, 0.5]],
                },
            ],
        }
        with open(label_file, "w", encoding="utf-8") as f:
            json.dump(label_content, f)

        # Build and export
        count = build_and_export_registry(
            output_dir=out_dir,
            labels_dir=labels_dir,
            include_pilot=False,
        )
        self.assertEqual(count, 1)

        dest_file = out_dir / "cam123.json"
        self.assertTrue(dest_file.exists())
        with open(dest_file, "r", encoding="utf-8") as f:
            exported = json.load(f)
        self.assertEqual(exported["camera_id"], "cam123")
        self.assertEqual(exported["direction_coverage"], "dual")
        self.assertEqual(len(exported["regions"]), 2)

        # Verify output directory
        valid, errors = verify_registry_dir(out_dir)
        self.assertTrue(valid)
        self.assertEqual(len(errors), 0)


if __name__ == "__main__":
    unittest.main()
