#!/usr/bin/env python3
"""
Unit test suite for tools/eval/cull_dead_cameras.py
"""

import datetime
import json
import tempfile
import unittest
from pathlib import Path

from tools.eval.cull_dead_cameras import (
    audit_and_cull_cameras,
    build_corridor_index,
    load_catalog,
    save_catalog,
)


class TestCullDeadCameras(unittest.TestCase):

    def setUp(self):
        self.now = datetime.datetime(2026, 11, 24, 12, 0, 0, tzinfo=datetime.timezone.utc)
        self.sample_cameras = [
            {
                "id": "cam_online_1",
                "title": "Online Cam 1",
                "displayName": "Street A - Camera 1",
                "status": "UP",
                "isOnline": True,
                "road_key": "street_a",
                "road_keys": ["street_a"],
            },
            {
                "id": "cam_dead_1",
                "title": "Dead Cam 1",
                "displayName": "Street A - Camera 2",
                "status": "NOT_IMAGE",
                "isOnline": False,
                "road_key": "street_a",
                "road_keys": ["street_a"],
            },
            {
                "id": "cam_corridor_solo",
                "title": "Solo Cam",
                "displayName": "Bridge B - Solo",
                "status": "UP",
                "isOnline": True,
                "road_key": "bridge_b",
                "road_keys": ["bridge_b"],
            },
            {
                "id": "cam_fragile_1",
                "title": "Fragile Cam 1",
                "displayName": "Arterial C - Cam 1",
                "status": "NOT_IMAGE",
                "isOnline": False,
                "road_key": "arterial_c",
                "road_keys": ["arterial_c"],
            },
            {
                "id": "cam_fragile_2",
                "title": "Fragile Cam 2",
                "displayName": "Arterial C - Cam 2",
                "status": "NOT_IMAGE",
                "isOnline": False,
                "road_key": "arterial_c",
                "road_keys": ["arterial_c"],
            },
        ]

    def test_build_corridor_index(self):
        index = build_corridor_index(self.sample_cameras)
        self.assertIn("street_a", index)
        self.assertEqual(len(index["street_a"]), 2)
        self.assertIn("bridge_b", index)
        self.assertEqual(len(index["bridge_b"]), 1)
        self.assertIn("arterial_c", index)
        self.assertEqual(len(index["arterial_c"]), 2)

    def test_dry_run_leaves_cameras_unchanged(self):
        updated, summary = audit_and_cull_cameras(
            cameras=self.sample_cameras,
            threshold_days=14,
            min_corridor_coverage=0.50,
            dry_run=True,
            now=self.now,
        )
        self.assertTrue(summary["dry_run"])
        # In dry run, status and isOnline must not be changed in updated list
        self.assertEqual(updated[1]["status"], "NOT_IMAGE")
        self.assertEqual(summary["total_cameras"], 5)
        self.assertGreater(summary["culled_count"], 0)

    def test_culling_updates_status_in_active_mode(self):
        updated, summary = audit_and_cull_cameras(
            cameras=self.sample_cameras,
            threshold_days=14,
            min_corridor_coverage=0.50,
            dry_run=False,
            now=self.now,
        )
        self.assertFalse(summary["dry_run"])
        # cam_dead_1 in street_a: street_a has 2 cams, 1 online (50% coverage).
        # Since 1/2 >= 0.50, cam_dead_1 is culled.
        culled = next(c for c in updated if c["id"] == "cam_dead_1")
        self.assertEqual(culled["status"], "stale_culled")
        self.assertFalse(culled["isOnline"])

    def test_arterial_corridor_floor_protection(self):
        # Create a corridor where culling both cameras would drop coverage to 0%
        cameras = [
            {
                "id": "c1",
                "status": "UP",
                "isOnline": True,
                "road_keys": ["corridor_x"],
            },
            {
                "id": "c2",
                "status": "UP",
                "isOnline": True,
                "road_keys": ["corridor_x"],
            },
        ]
        # Simulate last seen 30 days ago for c1 (inactive > 14 days)
        # If c1 is culled, corridor_x has 1/2 = 50% >= 50% -> allowed
        # But if c1 and c2 are both dead, second cam must be protected if floor is 0.60
        updated, summary = audit_and_cull_cameras(
            cameras=cameras,
            min_corridor_coverage=0.60,
            dry_run=False,
            now=self.now,
        )
        # Both cameras were UP and online, so none are candidates
        self.assertEqual(summary["culled_count"], 0)

    def test_save_and_load_catalog_integrity(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            tmp_path = Path(tmpdir) / "catalog.json"
            save_catalog(tmp_path, self.sample_cameras)
            loaded = load_catalog(tmp_path)
            self.assertEqual(len(loaded), len(self.sample_cameras))
            self.assertEqual(loaded[0]["id"], self.sample_cameras[0]["id"])


if __name__ == "__main__":
    unittest.main()
