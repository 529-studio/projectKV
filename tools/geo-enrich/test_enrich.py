#!/usr/bin/env python3
"""
Unit tests for tools/geo-enrich/enrich.py
"""

import json
import os
import tempfile
import unittest

from enrich import (
    BoundaryFeature,
    compute_bbox,
    enrich_cameras,
    load_boundaries,
    point_in_polygon,
    point_in_ring,
    write_unmatched_report,
)


class TestGeoEnrich(unittest.TestCase):

    def setUp(self):
        # A simple 1x1 square polygon from (0,0) to (1,1)
        self.square_ring = [
            [0.0, 0.0],
            [1.0, 0.0],
            [1.0, 1.0],
            [0.0, 1.0],
            [0.0, 0.0],
        ]
        self.sample_feature = {
            "type": "Feature",
            "properties": {
                "phuong_id": "P01",
                "phuong_name": "Phường Bến Nghé"
            },
            "geometry": {
                "type": "Polygon",
                "coordinates": [self.square_ring]
            }
        }

    def test_point_in_ring(self):
        # Center of square
        self.assertTrue(point_in_ring(0.5, 0.5, self.square_ring))
        # Outside square
        self.assertFalse(point_in_ring(1.5, 0.5, self.square_ring))
        self.assertFalse(point_in_ring(-0.5, 0.5, self.square_ring))
        self.assertFalse(point_in_ring(0.5, 1.5, self.square_ring))
        self.assertFalse(point_in_ring(0.5, -0.5, self.square_ring))

    def test_point_in_polygon_with_hole(self):
        # Exterior [0,0] to [10,10], interior hole [4,4] to [6,6]
        exterior = [[0.0, 0.0], [10.0, 0.0], [10.0, 10.0], [0.0, 10.0], [0.0, 0.0]]
        hole = [[4.0, 4.0], [6.0, 4.0], [6.0, 6.0], [4.0, 6.0], [4.0, 4.0]]
        coords = [exterior, hole]

        # Inside exterior and outside hole -> True
        self.assertTrue(point_in_polygon(2.0, 2.0, coords))
        # Inside hole -> False
        self.assertFalse(point_in_polygon(5.0, 5.0, coords))
        # Outside exterior -> False
        self.assertFalse(point_in_polygon(12.0, 5.0, coords))

    def test_compute_bbox(self):
        coords = [[[10.0, 20.0], [30.0, 40.0], [5.0, 25.0]]]
        min_x, min_y, max_x, max_y = compute_bbox(coords)
        self.assertEqual(min_x, 5.0)
        self.assertEqual(min_y, 20.0)
        self.assertEqual(max_x, 30.0)
        self.assertEqual(max_y, 40.0)

    def test_boundary_feature_containment(self):
        feat = BoundaryFeature(self.sample_feature)
        self.assertEqual(feat.phuong_id, "P01")
        self.assertEqual(feat.phuong_name, "Phường Bến Nghé")

        # Point inside
        self.assertTrue(feat.contains_point(0.5, 0.5))
        # Point outside
        self.assertFalse(feat.contains_point(2.0, 2.0))

    def test_enrich_cameras_matching_and_null_fallback(self):
        feat = BoundaryFeature(self.sample_feature)
        boundaries = [feat]

        cameras = [
            {
                "id": "cam-inside",
                "title": "Camera 1",
                "latitude": 0.5,
                "longitude": 0.5,
            },
            {
                "id": "cam-outside",
                "title": "Camera 2",
                "latitude": 5.0,
                "longitude": 5.0,
            },
            {
                "id": "cam-no-coords",
                "title": "Camera 3",
                "latitude": None,
                "longitude": None,
            }
        ]

        enriched, matched, unmatched = enrich_cameras(cameras, boundaries)

        self.assertEqual(len(enriched), 3)
        self.assertEqual(matched, 1)
        self.assertEqual(unmatched, 2)

        # Matched camera gets Phường attributes
        self.assertEqual(enriched[0]["phuong_id"], "P01")
        self.assertEqual(enriched[0]["phuong_name"], "Phường Bến Nghé")

        # Outside camera gets null (never fabricated values)
        self.assertIsNone(enriched[1]["phuong_id"])
        self.assertIsNone(enriched[1]["phuong_name"])

        # Missing coords camera gets null
        self.assertIsNone(enriched[2]["phuong_id"])
        self.assertIsNone(enriched[2]["phuong_name"])

    def test_enrichment_idempotency(self):
        feat = BoundaryFeature(self.sample_feature)
        boundaries = [feat]

        cameras = [
            {"id": "c1", "latitude": 0.5, "longitude": 0.5},
            {"id": "c2", "latitude": 9.9, "longitude": 9.9},
        ]

        run1, _, _ = enrich_cameras(cameras, boundaries)
        run2, _, _ = enrich_cameras(cameras, boundaries)

        str1 = json.dumps(run1, indent=2, ensure_ascii=False)
        str2 = json.dumps(run2, indent=2, ensure_ascii=False)

        self.assertEqual(str1, str2, "Enrichment must be byte-identical when run multiple times (idempotent)")

    def test_load_sample_geojson_file(self):
        sample_path = os.path.join(os.path.dirname(__file__), "data", "sample_phuong_boundaries.geojson")
        features = load_boundaries(sample_path)
        self.assertGreaterEqual(len(features), 2)
        names = [f.phuong_name for f in features]
        self.assertIn("Phường Bến Nghé", names)
        self.assertIn("Phường Tân Định", names)

    def test_write_unmatched_report(self):
        cameras = [
            {
                "id": "c_matched",
                "title": "Camera Matched",
                "displayName": "Matched Cam",
                "latitude": 10.7,
                "longitude": 106.6,
                "phuong_name": "Phường Tân Định",
            },
            {
                "id": "c_outside",
                "title": "Camera Outside",
                "displayName": "Outside Cam",
                "latitude": 10.5,
                "longitude": 106.2,
                "phuong_name": None,
            },
            {
                "id": "c_no_coords",
                "title": "Camera No Coords",
                "displayName": "No Coords Cam",
                "latitude": None,
                "longitude": None,
                "phuong_name": None,
            },
        ]
        with tempfile.NamedTemporaryFile("w+", suffix=".csv", delete=False) as tf:
            report_path = tf.name

        try:
            write_unmatched_report(cameras, report_path)
            with open(report_path, "r", encoding="utf-8") as f:
                content = f.read().splitlines()

            self.assertEqual(len(content), 3)  # Header + 2 unmatched
            self.assertIn("id,title,displayName,latitude,longitude,zone,reason", content[0])
            self.assertIn("Outside administrative boundary polygon extent", content[1])
            self.assertIn("Missing coordinates in upstream catalog", content[2])
        finally:
            if os.path.exists(report_path):
                os.remove(report_path)


if __name__ == "__main__":
    unittest.main()
