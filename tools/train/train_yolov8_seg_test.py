#!/usr/bin/env python3
"""
Unit tests for tools/train/train_yolov8_seg.py.
Verifies host compute headroom checks, dataset manifest validation,
dry-run training orchestration, and OpenVINO FP16 model export.
"""

import shutil
import sys
import tempfile
import unittest
from pathlib import Path

TRAIN_DIR = Path(__file__).resolve().parent
if str(TRAIN_DIR) not in sys.path:
    sys.path.insert(0, str(TRAIN_DIR))

from train_yolov8_seg import (
    check_host_compute_headroom,
    export_to_openvino_fp16,
    train_yolov8_seg,
    validate_dataset_manifest,
)


class TestTrainYolov8Seg(unittest.TestCase):
    def setUp(self):
        self.test_dir = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, self.test_dir)
        self.tmp_path = Path(self.test_dir)

    def test_check_host_compute_headroom(self):
        headroom = check_host_compute_headroom()
        self.assertIn("free_disk_gb", headroom)
        self.assertIn("total_disk_gb", headroom)
        self.assertGreater(headroom["total_disk_gb"], 0)

    def test_validate_dataset_manifest(self):
        valid_yaml = self.tmp_path / "valid.yaml"
        valid_yaml.write_text(
            """
path: /data/yolo_seg
train: images/train
val: images/val
names:
  0: roadway
  1: water_puddle
  2: motorcycle
  3: car
  4: bus
  5: truck
""",
            encoding="utf-8",
        )
        res = validate_dataset_manifest(valid_yaml)
        self.assertTrue(res["valid"])

        # Missing fields
        invalid_yaml = self.tmp_path / "invalid.yaml"
        invalid_yaml.write_text("train: images/train\n", encoding="utf-8")
        with self.assertRaises(ValueError):
            validate_dataset_manifest(invalid_yaml)

        # File not found
        with self.assertRaises(FileNotFoundError):
            validate_dataset_manifest(self.tmp_path / "nonexistent.yaml")

    def test_export_to_openvino_fp16_dry_run(self):
        export_dir = self.tmp_path / "models" / "yolov8n_seg_openvino_416"
        xml_p, bin_p = export_to_openvino_fp16(
            model_or_onnx_path=self.tmp_path / "dummy.pt",
            export_dir=export_dir,
            imgsz=416,
            dry_run=True,
        )

        self.assertTrue(xml_p.is_file())
        self.assertTrue(bin_p.is_file())
        self.assertEqual(xml_p.name, "yolov8n_seg.xml")
        self.assertEqual(bin_p.name, "yolov8n_seg.bin")

        xml_content = xml_p.read_text(encoding="utf-8")
        self.assertIn('name="yolov8n_seg_fp16"', xml_content)
        self.assertIn('name="output0"', xml_content)
        self.assertIn('name="output1"', xml_content)

        # Total size must be well under 10 MB
        total_size_mb = (xml_p.stat().st_size + bin_p.stat().st_size) / (1024 * 1024)
        self.assertLess(total_size_mb, 10.0)

    def test_train_yolov8_seg_dry_run(self):
        dataset_yaml = self.tmp_path / "dataset.yaml"
        dataset_yaml.write_text(
            """
train: images/train
val: images/val
names:
  0: roadway
  1: water_puddle
""",
            encoding="utf-8",
        )

        export_dir = self.tmp_path / "ov_export"
        stats = train_yolov8_seg(
            data_yaml=dataset_yaml,
            model_name="yolov8n-seg.pt",
            epochs=5,
            imgsz=416,
            batch=8,
            device="cpu",
            export_openvino_dir=export_dir,
            dry_run=True,
        )

        self.assertTrue(stats["train_completed"])
        self.assertTrue(stats["exported_openvino"])
        self.assertIsNotNone(stats["xml_path"])
        self.assertIsNotNone(stats["bin_path"])
        self.assertLess(stats["model_size_mb"], 10.0)
        self.assertTrue(Path(stats["xml_path"]).is_file())
        self.assertTrue(Path(stats["bin_path"]).is_file())


if __name__ == "__main__":
    unittest.main()
