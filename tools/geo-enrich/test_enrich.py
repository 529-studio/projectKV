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
    extract_road_fields,
    load_boundaries,
    parse_h3_resolutions,
    parse_phuong_name,
    point_in_polygon,
    point_in_ring,
    write_unmatched_report,
)

try:
    import h3
    HAS_H3 = True
except ImportError:
    HAS_H3 = False


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

        # Matched camera gets Phường attributes (split into phuong_type and prefix-less phuong_name)
        self.assertEqual(enriched[0]["phuong_id"], "P01")
        self.assertEqual(enriched[0]["phuong_type"], "Phường")
        self.assertEqual(enriched[0]["phuong_name"], "Bến Nghé")

        # Outside camera gets null (never fabricated values)
        self.assertIsNone(enriched[1]["phuong_id"])
        self.assertIsNone(enriched[1]["phuong_type"])
        self.assertIsNone(enriched[1]["phuong_name"])

        # Missing coords camera gets null
        self.assertIsNone(enriched[2]["phuong_id"])
        self.assertIsNone(enriched[2]["phuong_type"])
        self.assertIsNone(enriched[2]["phuong_name"])

    def test_parse_phuong_name(self):
        self.assertEqual(parse_phuong_name("Phường Bến Nghé"), ("Phường", "Bến Nghé"))
        self.assertEqual(parse_phuong_name("phường Tân Định"), ("Phường", "Tân Định"))
        self.assertEqual(parse_phuong_name("Phường 14"), ("Phường", "14"))
        self.assertEqual(parse_phuong_name("Xã Tân An Hội"), ("Xã", "Tân An Hội"))
        self.assertEqual(parse_phuong_name("Thị trấn Củ Chi"), ("Thị trấn", "Củ Chi"))
        self.assertEqual(parse_phuong_name("Bến Nghé"), (None, "Bến Nghé"))
        self.assertEqual(parse_phuong_name(None), (None, None))
        self.assertEqual(parse_phuong_name(""), (None, None))

    def test_extract_road_fields(self):
        self.assertEqual(extract_road_fields("Trần Quang Khải - Trần Khắc Chân"), ("Đường", "Trần Quang Khải"))
        self.assertEqual(extract_road_fields("Quốc Lộ 13 - cầu Ông Dầu"), ("Quốc lộ", "13"))
        self.assertEqual(extract_road_fields("QL1 - Tỉnh lộ 10 B (2)"), ("Quốc lộ", "1"))
        self.assertEqual(extract_road_fields("Xa lộ Hà Nội - Cát Lái"), ("Xa lộ", "Hà Nội"))
        self.assertEqual(extract_road_fields("Cao tốc LTDG - Võ Chí Công"), ("Cao tốc", "LTDG"))
        self.assertEqual(extract_road_fields("Cầu Sài Gòn - Dạ cầu"), ("Cầu", "Sài Gòn"))
        self.assertEqual(extract_road_fields("Cầu vượt Tân Thới Hiệp 1"), ("Cầu vượt", "Tân Thới Hiệp 1"))
        self.assertEqual(extract_road_fields("Nút giao Hàng Xanh 1"), ("Nút giao", "Hàng Xanh 1"))
        self.assertEqual(extract_road_fields("Đường số 7 - An Dương Vương"), ("Đường", "số 7"))
        self.assertEqual(extract_road_fields(None, None), (None, None))

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

    def test_parse_h3_resolutions(self):
        self.assertEqual(parse_h3_resolutions("7,9"), [7, 9])
        self.assertEqual(parse_h3_resolutions("9, 7"), [7, 9])
        self.assertEqual(parse_h3_resolutions("7, 7, 9"), [7, 9])
        self.assertEqual(parse_h3_resolutions("none"), [])
        self.assertEqual(parse_h3_resolutions(""), [])
        self.assertEqual(parse_h3_resolutions(None), [])
        self.assertEqual(parse_h3_resolutions("0"), [])
        self.assertEqual(parse_h3_resolutions("false"), [])

        with self.assertRaises(ValueError):
            parse_h3_resolutions("invalid")
        with self.assertRaises(ValueError):
            parse_h3_resolutions("7,16")
        with self.assertRaises(ValueError):
            parse_h3_resolutions("-1,7")

    @unittest.skipUnless(HAS_H3, "h3 package required")
    def test_h3_enrichment_known_coordinates(self):
        cameras = [
            {
                "id": "cam_district_1",
                "title": "District 1 Center",
                "latitude": 10.776889,
                "longitude": 106.700806,
            },
            {
                "id": "cam_tan_dinh",
                "title": "Tan Dinh Intersection",
                "latitude": 10.7918902432446,
                "longitude": 106.691054105759,
            },
        ]
        enriched, _, _ = enrich_cameras(cameras, h3_resolutions=[7, 9])

        self.assertEqual(len(enriched), 2)
        # Known pre-calculated H3 cell IDs
        self.assertEqual(enriched[0]["h3_res7"], "8765b5662ffffff")
        self.assertEqual(enriched[0]["h3_res9"], "8965b5662abffff")

        self.assertEqual(enriched[1]["h3_res7"], "8765b5644ffffff")
        self.assertEqual(enriched[1]["h3_res9"], "8965b564483ffff")

    @unittest.skipUnless(HAS_H3, "h3 package required")
    def test_h3_enrichment_missing_and_invalid_coordinates(self):
        cameras = [
            {
                "id": "cam_null",
                "title": "Camera without coordinates",
                "latitude": None,
                "longitude": None,
            },
            {
                "id": "cam_invalid",
                "title": "Camera with invalid coordinates",
                "latitude": "not-a-float",
                "longitude": 106.69,
            },
        ]
        enriched, _, _ = enrich_cameras(cameras, h3_resolutions=[7, 9])

        self.assertEqual(len(enriched), 2)
        self.assertIsNone(enriched[0]["h3_res7"])
        self.assertIsNone(enriched[0]["h3_res9"])
        self.assertIsNone(enriched[1]["h3_res7"])
        self.assertIsNone(enriched[1]["h3_res9"])

    @unittest.skipUnless(HAS_H3, "h3 package required")
    def test_h3_resolution_hierarchy(self):
        import h3
        cameras = [
            {
                "id": "cam_1",
                "latitude": 10.82,
                "longitude": 106.65,
            }
        ]
        enriched, _, _ = enrich_cameras(cameras, h3_resolutions=[7, 9])
        res7 = enriched[0]["h3_res7"]
        res9 = enriched[0]["h3_res9"]
        self.assertIsNotNone(res7)
        self.assertIsNotNone(res9)
        # Res 7 must be the ancestor/parent of Res 9
        self.assertEqual(h3.cell_to_parent(res9, 7), res7)

    @unittest.skipUnless(HAS_H3, "h3 package required")
    def test_h3_enrichment_idempotent(self):
        cameras = [
            {"id": "c1", "latitude": 10.776889, "longitude": 106.700806},
            {"id": "c2", "latitude": None, "longitude": None},
        ]
        run1, _, _ = enrich_cameras(cameras, h3_resolutions=[7, 9])
        run2, _, _ = enrich_cameras(cameras, h3_resolutions=[7, 9])

        self.assertEqual(run1, run2)
        str1 = json.dumps(run1, indent=2, ensure_ascii=False)
        str2 = json.dumps(run2, indent=2, ensure_ascii=False)
        self.assertEqual(str1, str2)

    def test_enrich_cameras_disabled_h3(self):
        cameras = [
            {"id": "c1", "latitude": 10.776889, "longitude": 106.700806}
        ]
        enriched, _, _ = enrich_cameras(cameras, h3_resolutions=[])
        self.assertNotIn("h3_res7", enriched[0])
        self.assertNotIn("h3_res9", enriched[0])


if __name__ == "__main__":
    unittest.main()
