import json
import os
import tempfile
import unittest

from generate_hex_polygons import generate_polygons


class TestGenerateHexPolygons(unittest.TestCase):
    def test_generate_polygons(self):
        sample_cameras = [
            {
                "id": "cam-1",
                "h3_res7": "8765b5662ffffff",
                "h3_res9": "8965b5662abffff",
            },
            {
                "id": "cam-2",
                "h3_res7": "8765b5662ffffff",  # duplicate cell id should be deduped
                "h3_res9": "8965b564483ffff",
            },
            {
                "id": "cam-no-h3",
                "h3_res7": None,
                "h3_res9": "",
            },
        ]

        with tempfile.TemporaryDirectory() as tmpdir:
            input_file = os.path.join(tmpdir, "cameras.json")
            output_file = os.path.join(tmpdir, "polygons.json")

            with open(input_file, "w", encoding="utf-8") as f:
                json.dump(sample_cameras, f)

            count = generate_polygons(input_file, output_file)
            self.assertEqual(count, 3)  # 1 unique res7 + 2 unique res9

            with open(output_file, "r", encoding="utf-8") as f:
                polygons = json.load(f)

            self.assertIn("8765b5662ffffff", polygons)
            self.assertIn("8965b5662abffff", polygons)
            self.assertIn("8965b564483ffff", polygons)

            # Check 6 boundary vertices each with [lat, lng]
            poly = polygons["8765b5662ffffff"]
            self.assertEqual(len(poly), 6)
            for pt in poly:
                self.assertEqual(len(pt), 2)
                lat, lng = pt
                self.assertTrue(10.0 <= lat <= 12.0)
                self.assertTrue(106.0 <= lng <= 108.0)


if __name__ == "__main__":
    unittest.main()
