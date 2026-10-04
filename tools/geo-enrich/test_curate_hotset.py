#!/usr/bin/env python3
"""
Unit tests for Hot Set Camera Curation Tool (Sprint 3 Issue #136, S2).
"""

import json
import os
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.dirname(__file__))

from curate_hotset_cameras import (
    DEFAULT_PRIORITY_CORRIDORS,
    build_manifest,
    filter_curated_cameras,
    format_summary_table,
    is_eligible_camera,
)


class TestCurateHotSet(unittest.TestCase):
    def setUp(self):
        self.sample_cameras = [
            # 1. Valid intersection on vo_van_kiet (eligible)
            {
                "id": "cam-vvk-1",
                "displayName": "Võ Văn Kiệt - An Bình",
                "title": "TTH 12.1",
                "isOnline": True,
                "status": "UP",
                "isGhost": False,
                "road_keys": ["vo_van_kiet", "an_binh"],
            },
            # 2. Mid-block on vo_van_kiet (len(road_keys) == 1 -> rejected per A6 rule)
            {
                "id": "cam-vvk-mid",
                "displayName": "Võ Văn Kiệt giữa cầu",
                "title": "TTH 12.2",
                "isOnline": True,
                "status": "UP",
                "isGhost": False,
                "road_keys": ["vo_van_kiet"],
            },
            # 3. 3-way roundabout (len(road_keys) == 3 -> rejected)
            {
                "id": "cam-vvk-3way",
                "displayName": "Võ Văn Kiệt - Ngã 3",
                "title": "TTH 12.3",
                "isOnline": True,
                "status": "UP",
                "isGhost": False,
                "road_keys": ["vo_van_kiet", "an_binh", "tran_hung_dao"],
            },
            # 4. Offline camera (isOnline: False -> rejected)
            {
                "id": "cam-vvk-offline",
                "displayName": "Võ Văn Kiệt - Hải Thượng Lãn Ông",
                "title": "TTH 12.4",
                "isOnline": False,
                "status": "UP",
                "isGhost": False,
                "road_keys": ["vo_van_kiet", "hai_thuong_lan_ong"],
            },
            # 5. Status DOWN camera (status != 'UP' -> rejected)
            {
                "id": "cam-vvk-down",
                "displayName": "Võ Văn Kiệt - Ký Con",
                "title": "TTH 12.5",
                "isOnline": True,
                "status": "DOWN",
                "isGhost": False,
                "road_keys": ["vo_van_kiet", "ky_con"],
            },
            # 6. Ghost camera (isGhost: True -> rejected)
            {
                "id": "cam-vvk-ghost",
                "displayName": "Võ Văn Kiệt - Ghost",
                "title": "TTH 12.6",
                "isOnline": True,
                "status": "UP",
                "isGhost": True,
                "road_keys": ["vo_van_kiet", "ghost_st"],
            },
            # 7. Duplicate stream (duplicateOf -> rejected)
            {
                "id": "cam-vvk-dup",
                "displayName": "Võ Văn Kiệt - Duplicate",
                "title": "TTH 12.7",
                "isOnline": True,
                "status": "UP",
                "isGhost": False,
                "duplicateOf": "cam-vvk-1",
                "road_keys": ["vo_van_kiet", "an_binh"],
            },
            # 8. Valid intersection on cong_hoa (eligible)
            {
                "id": "cam-ch-1",
                "displayName": "Cộng Hòa - Út Tịch",
                "title": "TTH 20.1",
                "isOnline": True,
                "status": "UP",
                "isGhost": False,
                "road_keys": ["cong_hoa", "ut_tich"],
            },
            # 9. Valid intersection on non-designated corridor (rejected by corridor filter)
            {
                "id": "cam-other-1",
                "displayName": "Lê Lợi - Pasteur",
                "title": "Q1 01",
                "isOnline": True,
                "status": "UP",
                "isGhost": False,
                "road_keys": ["le_loi", "pasteur"],
            },
        ]

    def test_is_eligible_camera(self):
        self.assertTrue(is_eligible_camera(self.sample_cameras[0]))
        self.assertFalse(is_eligible_camera(self.sample_cameras[3]))  # isOnline: False
        self.assertFalse(is_eligible_camera(self.sample_cameras[4]))  # status: DOWN
        self.assertFalse(is_eligible_camera(self.sample_cameras[5]))  # isGhost: True
        self.assertFalse(is_eligible_camera(self.sample_cameras[6]))  # duplicateOf

    def test_filter_curated_cameras(self):
        corridors = ["vo_van_kiet", "cong_hoa"]
        curated, counts = filter_curated_cameras(self.sample_cameras, corridors)

        # Only cam-vvk-1 and cam-ch-1 should qualify
        self.assertEqual(len(curated), 2)
        curated_ids = [c["id"] for c in curated]
        self.assertIn("cam-vvk-1", curated_ids)
        self.assertIn("cam-ch-1", curated_ids)

        self.assertEqual(counts["vo_van_kiet"], 1)
        self.assertEqual(counts["cong_hoa"], 1)

    def test_build_manifest(self):
        corridors = ["vo_van_kiet", "cong_hoa"]
        curated, counts = filter_curated_cameras(self.sample_cameras, corridors)
        manifest = build_manifest(curated, counts, corridors)

        self.assertIn("metadata", manifest)
        self.assertIn("cameras", manifest)
        self.assertEqual(manifest["metadata"]["total_cameras"], 2)
        self.assertEqual(manifest["metadata"]["corridors"], corridors)
        self.assertEqual(len(manifest["cameras"]), 2)

    def test_format_summary_table(self):
        counts = {"vo_van_kiet": 23, "mai_chi_tho": 13, "dien_bien_phu": 11, "cong_hoa": 10}
        table = format_summary_table(counts, 57)
        self.assertIn("Võ Văn Kiệt", table)
        self.assertIn("Mai Chí Thọ", table)
        self.assertIn("Điện Biên Phủ", table)
        self.assertIn("Cộng Hòa", table)
        self.assertIn("57", table)

    def test_real_catalog_curation_s1_option_1b(self):
        """Cross-checks curation against actual cameras_enriched.json to verify Option 1b (57 cameras)."""
        repo_root = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
        catalog_path = os.path.join(repo_root, "cameras_enriched.json")
        if not os.path.exists(catalog_path):
            self.skipTest(f"cameras_enriched.json not found at {catalog_path}")

        with open(catalog_path, "r", encoding="utf-8") as f:
            cameras = json.load(f)

        curated, counts = filter_curated_cameras(cameras, DEFAULT_PRIORITY_CORRIDORS)

        # Reconcile against S1 capacity numbers (docs/spike-vehicle-capacity.md §2 & §5)
        self.assertEqual(
            len(curated),
            57,
            f"Expected exactly 57 cameras for Option 1b, got {len(curated)}",
        )
        self.assertEqual(counts["vo_van_kiet"], 23)
        self.assertEqual(counts["mai_chi_tho"], 13)
        self.assertEqual(counts["dien_bien_phu"], 11)
        self.assertEqual(counts["cong_hoa"], 10)

        # Verify all IDs are unique
        ids = [c["id"] for c in curated]
        self.assertEqual(len(ids), len(set(ids)))

        # Verify every camera strictly complies with DoD rules
        for c in curated:
            self.assertEqual(len(c["road_keys"]), 2)
            self.assertTrue(
                any(k in DEFAULT_PRIORITY_CORRIDORS for k in c["road_keys"])
            )


if __name__ == "__main__":
    unittest.main()
