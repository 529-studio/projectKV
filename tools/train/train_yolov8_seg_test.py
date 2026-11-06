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
    V1_BASELINE_METRICS,
    V2_GATE_THRESHOLDS,
    check_host_compute_headroom,
    evaluate_retraining_gate,
    export_to_openvino_fp16,
    train_yolov8_seg,
    validate_dataset_manifest,
    verify_operational_rollback,
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

    def test_evaluate_retraining_gate_passes_canonical(self):
        passed, summary, failures = evaluate_retraining_gate()
        self.assertTrue(passed)
        self.assertEqual(summary["verdict"], "PASSED")
        self.assertEqual(len(failures), 0)
        self.assertGreaterEqual(summary["puddle_iou"], V2_GATE_THRESHOLDS["water_puddle_iou_min"])
        self.assertGreaterEqual(summary["mask_map50_95"], V2_GATE_THRESHOLDS["mask_map50_95_min"])

    def test_evaluate_retraining_gate_rejects_degradations(self):
        # 1. Puddle IoU drop below threshold
        bad_metrics = {
            "mask_map50": 0.70,
            "mask_map50_95": 0.50,
            "class_iou": {
                "roadway": 0.75,
                "water_puddle": 0.59,  # Below 0.612
                "motorcycle": 0.62,
                "car": 0.68,
                "bus": 0.71,
                "truck": 0.65,
            },
        }
        passed, _, failures = evaluate_retraining_gate(bad_metrics)
        self.assertFalse(passed)
        self.assertTrue(any("water_puddle IoU" in f for f in failures))

        # 2. Individual class IoU drop beyond 0.02 vs v1 baseline (0.741 -> 0.710 = -0.031)
        degraded_roadway = {
            "mask_map50": 0.75,
            "mask_map50_95": 0.52,
            "class_iou": {
                "roadway": 0.710,  # Dropped by 0.031
                "water_puddle": 0.620,
                "motorcycle": 0.621,
                "car": 0.684,
                "bus": 0.710,
                "truck": 0.655,
            },
        }
        passed2, _, failures2 = evaluate_retraining_gate(degraded_roadway)
        self.assertFalse(passed2)
        self.assertTrue(any("Class 'roadway' IoU dropped" in f for f in failures2))

        # 3. Latency exceeding 25ms
        passed3, _, failures3 = evaluate_retraining_gate(p95_latency_ms=28.5)
        self.assertFalse(passed3)
        self.assertTrue(any("p95 latency" in f for f in failures3))

        # 4. Model size exceeding 10MB
        passed4, _, failures4 = evaluate_retraining_gate(model_size_mb=12.4)
        self.assertFalse(passed4)
        self.assertTrue(any("Model size" in f for f in failures4))

    def test_verify_operational_rollback(self):
        model_dir = self.tmp_path / "rollback_test_models"
        model_dir.mkdir()
        seg_xml = model_dir / "yolov8n_seg.xml"
        seg_xml.write_text("<net name='seg'/>", encoding="utf-8")

        # Rollback test simulates rename to .bak, verifies fallback, and restores
        passed, errs = verify_operational_rollback(model_dir)
        self.assertTrue(passed)
        self.assertEqual(len(errs), 0)
        self.assertTrue(seg_xml.is_file())


if __name__ == "__main__":
    unittest.main()
