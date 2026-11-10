#!/usr/bin/env python3
"""
Unit tests for tools/train/train_yolov8_seg.py.
Verifies host compute headroom checks, dataset manifest validation,
dry-run training orchestration, real OpenVINO FP16 export integrity,
and the committed model artifact (Issue #399 / ADR-013 F4).
"""

import shutil
import sys
import tempfile
import unittest
from pathlib import Path

TRAIN_DIR = Path(__file__).resolve().parent
if str(TRAIN_DIR) not in sys.path:
    sys.path.insert(0, str(TRAIN_DIR))

REPO_ROOT = TRAIN_DIR.parents[1]
COMMITTED_IR_DIR = REPO_ROOT / "models" / "yolov8n_seg_openvino_416"

from train_yolov8_seg import (
    MIN_EXPORT_BIN_MB,
    MIN_WEIGHT_ENTROPY,
    V1_BASELINE_METRICS,
    V2_GATE_THRESHOLDS,
    check_host_compute_headroom,
    evaluate_retraining_gate,
    export_to_openvino_fp16,
    shannon_entropy,
    train_yolov8_seg,
    validate_dataset_manifest,
    verify_openvino_ir,
    verify_operational_rollback,
)

try:  # optional: only needed for compile-and-infer assertions
    import openvino as ov  # noqa: F401
    HAS_OPENVINO = True
except ImportError:
    HAS_OPENVINO = False


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

    def test_export_to_openvino_fp16_dry_run_writes_no_stub(self):
        """A dry run must never fabricate IR weights (root cause of ADR-013 F4)."""
        export_dir = self.tmp_path / "models" / "yolov8n_seg_openvino_416"
        xml_p, bin_p = export_to_openvino_fp16(
            model_or_onnx_path=self.tmp_path / "dummy.pt",
            export_dir=export_dir,
            imgsz=416,
            dry_run=True,
        )

        self.assertIsNone(xml_p)
        self.assertIsNone(bin_p)
        self.assertFalse(export_dir.exists(), "dry run must not create IR artifacts")

    def test_verify_openvino_ir_rejects_repeating_stub(self):
        """The 64 KB repeating-byte stub must fail closed with a clear reason."""
        export_dir = self.tmp_path / "stub_ir"
        export_dir.mkdir()
        xml_p = export_dir / "yolov8n_seg.xml"
        bin_p = export_dir / "yolov8n_seg.bin"
        xml_p.write_text(
            "<net><layers><layer id='0' type='Parameter'/></layers></net>", encoding="utf-8"
        )
        bin_p.write_bytes(b"\x00\x00\x01\x00" * 16384)

        with self.assertRaises(RuntimeError) as ctx:
            verify_openvino_ir(xml_p, bin_p, imgsz=416)
        message = str(ctx.exception)
        self.assertIn("below the", message)
        self.assertIn("entropy", message)
        self.assertIn("periodic repetition", message)
        self.assertIn("layers", message)

    def test_shannon_entropy_bounds(self):
        self.assertEqual(shannon_entropy(b""), 0.0)
        self.assertAlmostEqual(shannon_entropy(b"\x00" * 4096), 0.0, places=6)
        self.assertGreater(shannon_entropy(bytes(range(256)) * 4), MIN_WEIGHT_ENTROPY)

    @unittest.skipUnless(HAS_OPENVINO, "openvino runtime not installed")
    def test_committed_ir_is_authentic_and_loadable(self):
        """Regression guard for Issue #399: the shipped IR must be real FP16 weights."""
        xml_p = COMMITTED_IR_DIR / "yolov8n_seg.xml"
        bin_p = COMMITTED_IR_DIR / "yolov8n_seg.bin"
        self.assertTrue(xml_p.is_file(), f"missing {xml_p}")
        self.assertTrue(bin_p.is_file(), f"missing {bin_p}")

        evidence = verify_openvino_ir(xml_p, bin_p, imgsz=416, warmup_runs=1, timed_runs=2)
        self.assertGreater(evidence["bin_size_mb"], MIN_EXPORT_BIN_MB)
        self.assertGreaterEqual(evidence["shannon_entropy_bits_per_byte"], MIN_WEIGHT_ENTROPY)
        self.assertGreaterEqual(evidence["unique_byte_values"], 32)
        self.assertEqual(evidence["input_shape"][1], 3)
        self.assertEqual(evidence["inference_output_shapes"][0][1], 42)
        self.assertEqual(evidence["inference_output_shapes"][1], [1, 32, 104, 104])
        self.assertTrue(evidence["compile_ok"])
        # The cv-engine load-shedding path (320 px) must execute, not merely reshape.
        verified = {tuple(item["resolution"]) for item in evidence["resolutions_verified"]}
        self.assertIn((416, 416), verified)
        self.assertIn((320, 320), verified)

    @unittest.skipUnless(HAS_OPENVINO, "openvino runtime not installed")
    def test_committed_metadata_records_training_provenance(self):
        """metadata.yaml must state whether the class head was actually trained."""
        metadata_path = COMMITTED_IR_DIR / "metadata.yaml"
        self.assertTrue(metadata_path.is_file(), f"missing {metadata_path}")
        import yaml

        metadata = yaml.safe_load(metadata_path.read_text(encoding="utf-8"))
        self.assertIn("projectkv", metadata)
        provenance = metadata["projectkv"]
        self.assertEqual(provenance["class_count"], 6)
        self.assertEqual(provenance["output_channels"], 42)
        self.assertIn(
            provenance["head_status"],
            {"trained", "reinitialised_untrained", "converted_external_graph"},
        )

    @unittest.skipUnless(HAS_OPENVINO, "openvino runtime not installed")
    def test_export_refuses_untrained_head_by_default(self):
        """An 80-class checkpoint re-headed to 6 classes must not export silently."""
        from train_yolov8_seg import build_segmentation_model  # noqa: PLC0415

        checkpoint = REPO_ROOT / "yolov8n-seg.pt"
        if not checkpoint.is_file():
            self.skipTest(f"{checkpoint.name} not present in the workspace")

        model, stats = build_segmentation_model(checkpoint, nc=6)
        self.addCleanup(shutil.rmtree, stats["staging_dir"], ignore_errors=True)
        self.assertEqual(stats["target_nc"], 6)
        self.assertEqual(stats["source_nc"], 80)
        self.assertGreater(stats["reinitialised_tensors"], 0)

        with self.assertRaises(RuntimeError) as ctx:
            export_to_openvino_fp16(
                checkpoint,
                export_dir=self.tmp_path / "guarded_export",
                imgsz=416,
                nc=6,
                dry_run=False,
                allow_untrained_head=False,
            )
        self.assertIn("NOT trained", str(ctx.exception))

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
        self.assertFalse(stats["exported_openvino"], "dry run must not export fabricated IR")
        self.assertIsNone(stats["xml_path"])
        self.assertIsNone(stats["bin_path"])
        self.assertEqual(stats["model_size_mb"], 0.0)
        self.assertFalse((export_dir / "yolov8n_seg.bin").exists())

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
