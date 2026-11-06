#!/usr/bin/env python3
"""
Unit tests for tools/harvester/verify_flywheel_soak.py (S10-T4 / ADR-012 D2).
"""

import json
import tempfile
import unittest
from pathlib import Path

from tools.harvester.verify_flywheel_soak import generate_simulated_soak, verify_soak_log


class TestFlywheelSoak(unittest.TestCase):
    def setUp(self):
        self.temp_dir = tempfile.TemporaryDirectory()
        self.output_path = Path(self.temp_dir.name) / "test_soak.json"

    def tearDown(self):
        self.temp_dir.cleanup()

    def test_generate_and_verify_soak(self):
        data = generate_simulated_soak(self.output_path)
        self.assertTrue(self.output_path.exists())

        # Assert data structure
        self.assertEqual(data["summary"]["scheduled_runs"], 9)
        self.assertEqual(data["summary"]["rain_runs"], 1)
        self.assertEqual(data["summary"]["total_runs"], 10)
        self.assertEqual(data["summary"]["total_cameras_failed"], 0)
        self.assertEqual(data["summary"]["missed_slots_count"], 0)
        self.assertGreater(data["summary"]["total_cameras_harvested"], 500)

        # Verification pass
        self.assertTrue(verify_soak_log(self.output_path))

    def test_verify_fails_on_missing_runs(self):
        data = generate_simulated_soak(self.output_path)
        # Corrupt data: remove rain run
        data["runs"].pop()
        data["summary"]["total_runs"] = len(data["runs"])
        with open(self.output_path, "w", encoding="utf-8") as f:
            json.dump(data, f)

        self.assertFalse(verify_soak_log(self.output_path))

    def test_verify_fails_on_camera_failures(self):
        data = generate_simulated_soak(self.output_path)
        # Corrupt data: introduce camera failures
        data["summary"]["total_cameras_failed"] = 5
        with open(self.output_path, "w", encoding="utf-8") as f:
            json.dump(data, f)

        self.assertFalse(verify_soak_log(self.output_path))


if __name__ == "__main__":
    unittest.main()
