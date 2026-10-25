#!/usr/bin/env python3
"""
Unit tests for tools/eval/export_yolo_seg.py.
Verifies polygon normalization, bounding box translation, class mapping,
dataset manifest generation, and deterministic train/val splitting.
"""

import json
import shutil
import sys
import tempfile
import unittest
from pathlib import Path

# Add tools/eval to sys.path so test can be run directly or discovered
TOOLS_EVAL_DIR = Path(__file__).resolve().parent
if str(TOOLS_EVAL_DIR) not in sys.path:
    sys.path.insert(0, str(TOOLS_EVAL_DIR))

from export_yolo_seg import (
    TARGET_CLASSES,
    bbox_to_polygon_norm,
    export_dataset,
    normalize_class_name,
    parse_label_file,
    synthesize_polygons_from_metadata,
    verify_dataset,
)


class TestExportYoloSeg(unittest.TestCase):
    def setUp(self):
        self.test_dir = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, self.test_dir)
        self.tmp_path = Path(self.test_dir)

    def test_normalize_class_name(self):
        # Target direct names
        self.assertEqual(normalize_class_name("roadway"), 0)
        self.assertEqual(normalize_class_name("water_puddle"), 1)
        self.assertEqual(normalize_class_name("motorcycle"), 2)
        self.assertEqual(normalize_class_name("car"), 3)
        self.assertEqual(normalize_class_name("bus"), 4)
        self.assertEqual(normalize_class_name("truck"), 5)

        # Aliases
        self.assertEqual(normalize_class_name("road"), 0)
        self.assertEqual(normalize_class_name("asphalt"), 0)
        self.assertEqual(normalize_class_name("puddle"), 1)
        self.assertEqual(normalize_class_name("flood"), 1)
        self.assertEqual(normalize_class_name("motorbike"), 2)
        self.assertEqual(normalize_class_name("bike"), 2)
        self.assertEqual(normalize_class_name("mc"), 2)
        self.assertEqual(normalize_class_name("automobile"), 3)
        self.assertEqual(normalize_class_name("van"), 3)
        self.assertEqual(normalize_class_name("coach"), 4)
        self.assertEqual(normalize_class_name("container"), 5)
        self.assertEqual(normalize_class_name("lorry"), 5)

        # Unknown
        self.assertIsNone(normalize_class_name("pedestrian"))
        self.assertIsNone(normalize_class_name("traffic_light"))

    def test_bbox_to_polygon_norm(self):
        # Normalized [xmin, ymin, xmax, ymax]
        poly = bbox_to_polygon_norm([0.1, 0.2, 0.5, 0.8], is_norm=True, fmt="xyxy")
        self.assertEqual(len(poly), 8)
        self.assertEqual(poly, [0.1, 0.2, 0.5, 0.2, 0.5, 0.8, 0.1, 0.8])

        # Normalized [cx, cy, w, h]
        poly_cx = bbox_to_polygon_norm([0.3, 0.5, 0.4, 0.6], is_norm=True, fmt="cxcywh")
        # cx=0.3, cy=0.5, w=0.4, h=0.6 -> xmin=0.1, ymin=0.2, xmax=0.5, ymax=0.8
        self.assertAlmostEqual(poly_cx[0], 0.1)
        self.assertAlmostEqual(poly_cx[1], 0.2)
        self.assertAlmostEqual(poly_cx[2], 0.5)
        self.assertAlmostEqual(poly_cx[5], 0.8)

        # Normalized [xywh]
        poly_xywh = bbox_to_polygon_norm([0.1, 0.2, 0.4, 0.6], is_norm=True, fmt="xywh")
        self.assertAlmostEqual(poly_xywh[0], 0.1)
        self.assertAlmostEqual(poly_xywh[1], 0.2)
        self.assertAlmostEqual(poly_xywh[2], 0.5)
        self.assertAlmostEqual(poly_xywh[5], 0.8)

        # Pixel coords conversion
        poly_px = bbox_to_polygon_norm([192, 108, 960, 540], img_w=1920.0, img_h=1080.0, is_norm=False, fmt="xyxy")
        self.assertEqual(poly_px, [0.1, 0.1, 0.5, 0.1, 0.5, 0.5, 0.1, 0.5])

        # Invalid length
        with self.assertRaises(ValueError):
            bbox_to_polygon_norm([0.1, 0.2, 0.3])

    def test_synthesize_polygons_from_metadata(self):
        # Dry scene: should synthesize roadway, but NO water puddle
        dry_meta = {
            "traffic": "CLEAR",
            "flood": "NONE",
            "waterline_depth": "DRY",
            "flood_submersion_anchor": "dry",
        }
        dry_polys = synthesize_polygons_from_metadata(dry_meta)
        self.assertEqual(len(dry_polys), 1)
        self.assertEqual(dry_polys[0][0], 0)  # Class 0: roadway

        # Wet asphalt without standing puddle (ADR-009 §4.3 Rule 1)
        wet_meta = {
            "traffic": "MODERATE",
            "flood": "NONE",
            "waterline_depth": "WET_SURFACE",
            "flood_submersion_anchor": "wet_asphalt_no_standing",
        }
        wet_polys = synthesize_polygons_from_metadata(wet_meta)
        self.assertEqual(len(wet_polys), 1)
        self.assertEqual(wet_polys[0][0], 0)  # Only roadway

        # Shallow flood: roadway + water_puddle
        shallow_meta = {
            "traffic": "MODERATE",
            "flood": "SHALLOW",
            "waterline_depth": "TIRE_QUARTER_SUBMERGED",
            "flood_submersion_anchor": "tire_base_under_10cm",
        }
        shallow_polys = synthesize_polygons_from_metadata(shallow_meta)
        self.assertEqual(len(shallow_polys), 2)
        classes = [p[0] for p in shallow_polys]
        self.assertIn(0, classes)
        self.assertIn(1, classes)

        # Deep flood: roadway + water_puddle
        deep_meta = {
            "traffic": "HEAVY",
            "flood": "DEEP",
            "waterline_depth": "TIRE_HALF_SUBMERGED",
            "flood_submersion_anchor": "exhaust_hub_over_15cm",
        }
        deep_polys = synthesize_polygons_from_metadata(deep_meta)
        self.assertEqual(len(deep_polys), 2)
        classes = [p[0] for p in deep_polys]
        self.assertIn(0, classes)
        self.assertIn(1, classes)

    def test_parse_label_file_explicit_annotations(self):
        label_file = self.tmp_path / "explicit_label.json"
        data = {
            "frame_id": "frame_001",
            "image_path": "data/proofs/2026-10-24/frame_001.jpg",
            "annotations": [
                {
                    "class": "roadway",
                    "polygon": [[0.1, 0.9], [0.9, 0.9], [0.6, 0.4], [0.4, 0.4]],
                },
                {
                    "class": "water_puddle",
                    "polygon": [[0.2, 0.8], [0.8, 0.8], [0.5, 0.5]],
                },
                {
                    "class": "motorcycle",
                    "bbox": [0.4, 0.6, 0.5, 0.8],
                },
            ],
            "labels": {"flood": "SHALLOW"},
        }
        with open(label_file, "w", encoding="utf-8") as f:
            json.dump(data, f)

        _, segments, used_synthetic = parse_label_file(label_file, repo_root=self.tmp_path)
        self.assertEqual(len(segments), 3)
        self.assertFalse(used_synthetic)
        self.assertEqual(segments[0][0], 0)  # roadway
        self.assertEqual(len(segments[0][1]), 8)
        self.assertEqual(segments[1][0], 1)  # water_puddle
        self.assertEqual(len(segments[1][1]), 6)
        self.assertEqual(segments[2][0], 2)  # motorcycle
        self.assertEqual(len(segments[2][1]), 8)

    def test_parse_label_file_synthetic_is_opt_in(self):
        label_file = self.tmp_path / "meta_only_label.json"
        data = {
            "frame_id": "frame_meta",
            "labels": {
                "traffic": "MODERATE",
                "flood": "SHALLOW",
                "waterline_depth": "TIRE_QUARTER_SUBMERGED",
            },
        }
        with open(label_file, "w", encoding="utf-8") as f:
            json.dump(data, f)

        # Default: NO synthetic geometry (ADR-011 D2) — empty segments.
        _, segments, used_synthetic = parse_label_file(label_file, repo_root=self.tmp_path)
        self.assertEqual(segments, [])
        self.assertFalse(used_synthetic)

        # Opt-in: metadata-derived geometry is returned and flagged.
        _, segments_opt, used_synthetic_opt = parse_label_file(
            label_file, repo_root=self.tmp_path, allow_synthetic=True
        )
        self.assertTrue(len(segments_opt) >= 2)  # roadway + shallow puddle
        self.assertTrue(used_synthetic_opt)

    def test_export_dataset_skips_labels_without_annotations_by_default(self):
        labels_dir = self.tmp_path / "labels_default"
        labels_dir.mkdir()
        output_dir = self.tmp_path / "output_default"

        # Classification-only labels (the entire production catalog today).
        for i in range(10):
            lf = labels_dir / f"cam_{i:03d}.json"
            data = {
                "frame_id": f"cam_{i:03d}",
                "labels": {
                    "traffic": "MODERATE",
                    "flood": "SHALLOW" if i % 2 == 0 else "NONE",
                    "waterline_depth": "TIRE_QUARTER_SUBMERGED" if i % 2 == 0 else "DRY",
                },
            }
            with open(lf, "w", encoding="utf-8") as f:
                json.dump(data, f)

        stats = export_dataset(
            labels_dir=labels_dir,
            output_dir=output_dir,
            val_split=0.2,
            seed=42,
            copy_images=False,
            verify=False,
        )

        self.assertEqual(stats["total_labels"], 10)
        self.assertEqual(stats["skipped_no_annotations"], 10)
        self.assertEqual(stats["synthesized_labels"], 0)
        self.assertEqual(stats["train_samples"], 0)
        self.assertEqual(stats["val_samples"], 0)
        # No YOLO label files may be emitted from fabricated geometry.
        self.assertEqual(list((output_dir / "labels" / "train").glob("*.txt")), [])
        self.assertEqual(list((output_dir / "labels" / "val").glob("*.txt")), [])

    def test_export_dataset_allow_synthetic_opt_in(self):
        labels_dir = self.tmp_path / "labels_synth"
        labels_dir.mkdir()
        output_dir = self.tmp_path / "output_synth"

        for i in range(10):
            lf = labels_dir / f"cam_{i:03d}.json"
            is_flood = (i % 2 == 0)
            data = {
                "frame_id": f"cam_{i:03d}",
                "labels": {
                    "traffic": "MODERATE",
                    "flood": "SHALLOW" if is_flood else "NONE",
                    "waterline_depth": "TIRE_QUARTER_SUBMERGED" if is_flood else "DRY",
                },
            }
            with open(lf, "w", encoding="utf-8") as f:
                json.dump(data, f)

        # Export with val_split 0.2 (2 val, 8 train)
        stats = export_dataset(
            labels_dir=labels_dir,
            output_dir=output_dir,
            val_split=0.2,
            seed=42,
            copy_images=False,
            verify=True,
            allow_synthetic=True,
        )

        self.assertEqual(stats["total_labels"], 10)
        self.assertEqual(stats["train_samples"], 8)
        self.assertEqual(stats["val_samples"], 2)
        self.assertEqual(stats["skipped_no_annotations"], 0)
        self.assertEqual(stats["synthesized_labels"], 10)
        self.assertTrue(stats.get("verification_passed", False))

        # Check dataset.yaml
        yaml_file = output_dir / "dataset.yaml"
        self.assertTrue(yaml_file.is_file())
        with open(yaml_file, "r", encoding="utf-8") as f:
            yaml_content = f.read()
        self.assertIn("train: images/train", yaml_content)
        self.assertIn("val: images/val", yaml_content)
        for idx, name in TARGET_CLASSES.items():
            self.assertIn(f"{idx}: {name}", yaml_content)

        # Check train labels
        train_labels = list((output_dir / "labels" / "train").glob("*.txt"))
        val_labels = list((output_dir / "labels" / "val").glob("*.txt"))
        self.assertEqual(len(train_labels), 8)
        self.assertEqual(len(val_labels), 2)

        # Re-run verification directly
        valid, errs = verify_dataset(output_dir)
        self.assertTrue(valid)
        self.assertEqual(len(errs), 0)

    def test_verify_dataset_detects_malformed_entries(self):
        output_dir = self.tmp_path / "malformed_yolo"
        (output_dir / "labels" / "train").mkdir(parents=True)
        (output_dir / "labels" / "val").mkdir(parents=True)

        # Write invalid class_id
        with open(output_dir / "labels" / "train" / "bad1.txt", "w") as f:
            f.write("9 0.1 0.1 0.2 0.2 0.3 0.3\n")

        # Write out-of-bounds coord
        with open(output_dir / "labels" / "train" / "bad2.txt", "w") as f:
            f.write("0 1.5 0.1 0.2 0.2 0.3 0.3\n")

        # Missing yaml
        valid, errs = verify_dataset(output_dir)
        self.assertFalse(valid)
        self.assertTrue(any("Missing dataset.yaml" in e for e in errs))
        self.assertTrue(any("invalid class_id" in e for e in errs))
        self.assertTrue(any("out of bounds" in e for e in errs))


if __name__ == "__main__":
    unittest.main()
