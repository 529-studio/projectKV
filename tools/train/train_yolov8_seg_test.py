#!/usr/bin/env python3
"""
Unit tests for tools/train/train_yolov8_seg.py.
Verifies host compute headroom checks, dataset manifest validation,
dry-run training orchestration, real OpenVINO FP16 export integrity,
and the committed model artifact (Issue #399 / ADR-013 F4).

Sprint 12 Issue #411 (S12-T5) extends the suite with the measured-input gate
contract from docs/sprints/s12_t5_spec.md §2.7:
  * a PASS requires a measured val_metrics.json bound to a run manifest;
  * every quantitative boundary (0.611/0.612, 0.495/0.496, 0.020/0.021,
    25.0/25.01 ms, 10.0/10.01 MiB) is asserted on both sides;
  * --verify-gate without --metrics exits 2 unless --allow-untrained-head;
  * the anti-cherry-pick stability margin is <= 0.02 over the last-10 median;
  * head_status is trained only with 0 re-initialised tensors, and the
    metadata training block carries every provenance SHA.

Tests that depend on the not-yet-written S12-T5 API skip with a reason naming
the missing symbol; ``KV_TDD_STRICT=1`` turns those skips into failures.
"""

import functools
import json
import os
import shutil
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch
from pathlib import Path

TRAIN_DIR = Path(__file__).resolve().parent
if str(TRAIN_DIR) not in sys.path:
    sys.path.insert(0, str(TRAIN_DIR))

REPO_ROOT = TRAIN_DIR.parents[1]
COMMITTED_IR_DIR = REPO_ROOT / "models" / "yolov8n_seg_openvino_416"

from train_yolov8_seg import (
    MIN_EVIDENCE_EPOCHS,
    MIN_EXPORT_BIN_MB,
    MIN_WEIGHT_ENTROPY,
    TARGET_CLASSES,
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

import train_yolov8_seg as train_module

STRICT_RED_PHASE = os.environ.get("KV_TDD_STRICT") == "1"


def requires(*attrs):
    """Skip (or fail under KV_TDD_STRICT) when the S12-T5 API is absent."""

    def decorator(func):
        @functools.wraps(func)
        def wrapper(self, *args, **kwargs):
            missing = [name for name in attrs if not hasattr(train_module, name)]
            if missing:
                reason = (
                    "TDD red phase: tools/train/train_yolov8_seg.py does not expose "
                    + ", ".join(missing)
                )
                if STRICT_RED_PHASE:
                    raise AssertionError(reason)
                raise unittest.SkipTest(reason)
            return func(self, *args, **kwargs)

        return wrapper

    return decorator

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

        self.assertFalse(stats["train_completed"], "dry run must not report train_completed=True")
        self.assertFalse(stats["exported_openvino"], "dry run must not export fabricated IR")
        self.assertIsNone(stats["xml_path"])
        self.assertIsNone(stats["bin_path"])
        self.assertEqual(stats["model_size_mb"], 0.0)
        self.assertFalse((export_dir / "yolov8n_seg.bin").exists())

    @patch("train_yolov8_seg.check_host_compute_headroom", return_value={"is_constrained": True, "warning": "low disk"})
    def test_train_without_dry_run_fails_closed_when_constrained(self, _mock):
        dataset_yaml = self.tmp_path / "dataset_test.yaml"
        dataset_yaml.write_text("train: a\nval: b\nnames:\n  0: roadway\n", encoding="utf-8")
        with self.assertRaises(RuntimeError) as ctx:
            train_yolov8_seg(
                data_yaml=dataset_yaml,
                model_name="yolov8n-seg.pt",
                epochs=1,
                dry_run=False,
            )
        self.assertIn("constrained compute resources", str(ctx.exception))

    def test_evaluate_retraining_gate_passes_canonical(self):
        canonical_metrics = {
            "mask_map50": 0.742,
            "mask_map50_95": 0.528,
            "class_iou": {
                "roadway": 0.762,
                "water_puddle": 0.624,
                "motorcycle": 0.638,
                "car": 0.702,
                "bus": 0.718,
                "truck": 0.669,
            },
        }
        passed, summary, failures = evaluate_retraining_gate(metrics=canonical_metrics)
        self.assertTrue(passed)
        self.assertEqual(summary["verdict"], "PASSED")
        self.assertEqual(len(failures), 0)
        self.assertGreaterEqual(summary["puddle_iou"], V2_GATE_THRESHOLDS["water_puddle_iou_min"])
        self.assertGreaterEqual(summary["mask_map50_95"], V2_GATE_THRESHOLDS["mask_map50_95_min"])

    def test_evaluate_retraining_gate_unverified_when_unmeasured(self):
        passed, summary, failures = evaluate_retraining_gate(metrics=None)
        self.assertFalse(passed)
        self.assertEqual(summary["verdict"], "UNVERIFIED")
        self.assertTrue(any("unmeasured" in f.lower() for f in failures))

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


MEASURED_METRICS = {
    "provenance": "ultralytics_val",
    "split": "val",
    "conf": 0.25,
    "iou": 0.5,
    "imgsz": 416,
    "weights_sha256": "1" * 64,
    "dataset_manifest_sha256": "2" * 64,
    "split_sha256": "3" * 64,
    "generated_at": "2026-11-12T10:00:00+07:00",
    "epochs_completed": 100,
    "best_epoch": 87,
    "mask_map50": 0.742,
    "mask_map50_95": 0.528,
    "class_iou": {
        "roadway": 0.762,
        "water_puddle": 0.624,
        "motorcycle": 0.638,
        "car": 0.702,
        "bus": 0.718,
        "truck": 0.669,
    },
    "stability": {"last10_median_map50_95": 0.508, "best_minus_median": 0.020},
}


def gate_metrics(**overrides) -> dict:
    """Full six-class metrics that clear every gate threshold unless overridden."""
    metrics = {
        "mask_map50": 0.742,
        "mask_map50_95": 0.528,
        "class_iou": {
            "roadway": 0.762,
            "water_puddle": 0.624,
            "motorcycle": 0.638,
            "car": 0.702,
            "bus": 0.718,
            "truck": 0.669,
        },
    }
    class_overrides = overrides.pop("class_iou", None)
    metrics.update(overrides)
    if class_overrides:
        metrics["class_iou"] = dict(metrics["class_iou"], **class_overrides)
    return metrics


class TestMeasuredGateBoundaries(unittest.TestCase):
    """S12-T5 §2.7: each quantitative boundary must fail on the wrong side."""

    def test_water_puddle_iou_boundary_at_0_612(self):
        passed, _, failures = evaluate_retraining_gate(
            metrics=gate_metrics(class_iou={"water_puddle": 0.612})
        )
        self.assertTrue(passed, failures)

        passed, summary, failures = evaluate_retraining_gate(
            metrics=gate_metrics(class_iou={"water_puddle": 0.611})
        )
        self.assertFalse(passed)
        self.assertEqual(summary["verdict"], "FAILED")
        self.assertTrue(any("water_puddle IoU" in f for f in failures))

    def test_mask_map50_95_boundary_at_0_496(self):
        passed, _, failures = evaluate_retraining_gate(
            metrics=gate_metrics(mask_map50_95=0.496)
        )
        self.assertTrue(passed, failures)

        passed, _, failures = evaluate_retraining_gate(
            metrics=gate_metrics(mask_map50_95=0.495)
        )
        self.assertFalse(passed)
        self.assertTrue(any("mask mAP50-95" in f for f in failures))

    def test_class_drop_beyond_0_020_fails(self):
        baseline = V1_BASELINE_METRICS["class_iou"]["roadway"]

        passed, _, failures = evaluate_retraining_gate(
            metrics=gate_metrics(class_iou={"roadway": round(baseline - 0.021, 4)})
        )
        self.assertFalse(passed)
        self.assertTrue(any("roadway" in f and "dropped" in f for f in failures))

    def test_class_drop_of_exactly_0_020_is_allowed(self):
        """A drop of exactly the documented tolerance is allowed.

        0.721 - 0.741 is -0.020000000000000018 in IEEE-754, so comparing the raw
        float delta against ``-0.020`` reported a compliant model as a violation.
        ``evaluate_retraining_gate`` now rounds the delta to four decimals before
        the comparison; this test pins that behaviour.
        """
        baseline = V1_BASELINE_METRICS["class_iou"]["roadway"]

        passed, summary, failures = evaluate_retraining_gate(
            metrics=gate_metrics(class_iou={"roadway": round(baseline - 0.020, 4)})
        )
        self.assertTrue(passed, failures)
        self.assertEqual(summary["class_deltas"]["roadway"], -0.02)

    def test_p95_latency_boundary_at_25_0_ms(self):
        passed, _, failures = evaluate_retraining_gate(
            metrics=gate_metrics(), p95_latency_ms=25.0
        )
        self.assertTrue(passed, failures)

        passed, _, failures = evaluate_retraining_gate(
            metrics=gate_metrics(), p95_latency_ms=25.01
        )
        self.assertFalse(passed)
        self.assertTrue(any("p95 latency" in f and "25.01" in f for f in failures))

    def test_model_size_boundary_at_10_0_mib(self):
        passed, _, failures = evaluate_retraining_gate(
            metrics=gate_metrics(), model_size_mb=10.0
        )
        self.assertTrue(passed, failures)

        passed, _, failures = evaluate_retraining_gate(
            metrics=gate_metrics(), model_size_mb=10.01
        )
        self.assertFalse(passed)
        self.assertTrue(any("Model size" in f and "10.01" in f for f in failures))


class TestMeasuredValMetricsContract(unittest.TestCase):
    """S12-T5 §2.7 / §5.2: only a measured artifact may drive the gate."""

    def setUp(self):
        self.test_dir = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, self.test_dir)
        self.tmp_path = Path(self.test_dir)

    @requires("load_val_metrics")
    def test_valid_val_metrics_file_loads_every_contract_field(self):
        path = self.tmp_path / "val_metrics.json"
        path.write_text(json.dumps(MEASURED_METRICS, indent=2), encoding="utf-8")

        metrics = train_module.load_val_metrics(path)

        for key in ("provenance", "split", "weights_sha256", "dataset_manifest_sha256",
                    "split_sha256", "epochs_completed", "best_epoch", "mask_map50",
                    "mask_map50_95", "class_iou", "stability"):
            self.assertIn(key, metrics, f"loader dropped {key}")
        self.assertEqual(list(metrics["class_iou"]), TARGET_CLASSES)

    @requires("load_val_metrics")
    def test_measured_val_metrics_file_passes_the_gate_with_artifact_provenance(self):
        path = self.tmp_path / "val_metrics.json"
        path.write_text(json.dumps(MEASURED_METRICS, indent=2), encoding="utf-8")
        metrics = train_module.load_val_metrics(path)

        passed, summary, failures = evaluate_retraining_gate(
            metrics=metrics, metrics_provenance=str(path)
        )

        self.assertTrue(passed, failures)
        self.assertEqual(summary["verdict"], "PASSED")
        self.assertEqual(summary["metrics_provenance"], str(path))
        self.assertEqual(failures, [])

    @requires("load_val_metrics")
    def test_49_completed_epochs_is_rejected_by_ac10(self):
        """AC10: a run below 50 epochs cannot certify a model."""
        short = dict(MEASURED_METRICS, epochs_completed=49, best_epoch=41)
        path = self.tmp_path / "short.json"
        path.write_text(json.dumps(short), encoding="utf-8")

        with self.assertRaises(ValueError) as ctx:
            train_module.load_val_metrics(path)
        message = str(ctx.exception)
        self.assertIn("49", message)
        self.assertIn(str(MIN_EVIDENCE_EPOCHS), message)
        self.assertIn("AC10", message)

    @requires("load_val_metrics")
    def test_50_completed_epochs_is_accepted(self):
        """The AC10 boundary is inclusive: 50 completed epochs is evidence."""
        at_floor = dict(MEASURED_METRICS, epochs_completed=50, best_epoch=50)
        path = self.tmp_path / "at_floor.json"
        path.write_text(json.dumps(at_floor), encoding="utf-8")

        metrics = train_module.load_val_metrics(path)

        self.assertEqual(metrics["epochs_completed"], 50)
        self.assertEqual(metrics["best_epoch"], 50)

    @requires("load_val_metrics")
    def test_short_run_is_loadable_only_through_the_explicit_bypass(self):
        short = dict(MEASURED_METRICS, epochs_completed=10, best_epoch=10)
        path = self.tmp_path / "short.json"
        path.write_text(json.dumps(short), encoding="utf-8")

        with self.assertLogs(level="WARNING") as logs:
            metrics = train_module.load_val_metrics(path, allow_short_run=True)

        self.assertEqual(metrics["epochs_completed"], 10)
        self.assertTrue(any("allow_short_run" in line for line in logs.output))

    @requires("load_val_metrics")
    def test_best_epoch_beyond_the_completed_count_is_refused(self):
        impossible = dict(MEASURED_METRICS, epochs_completed=60, best_epoch=87)
        path = self.tmp_path / "impossible.json"
        path.write_text(json.dumps(impossible), encoding="utf-8")

        with self.assertRaises(ValueError) as ctx:
            train_module.load_val_metrics(path)
        self.assertIn("best_epoch", str(ctx.exception))

    @requires("load_val_metrics")
    def test_missing_metric_field_is_refused_by_name(self):
        broken = dict(MEASURED_METRICS)
        broken.pop("class_iou")
        path = self.tmp_path / "broken.json"
        path.write_text(json.dumps(broken), encoding="utf-8")

        with self.assertRaises(ValueError) as ctx:
            train_module.load_val_metrics(path)
        self.assertIn("class_iou", str(ctx.exception))

    @requires("load_val_metrics")
    def test_class_order_mismatch_aborts(self):
        reordered = dict(MEASURED_METRICS)
        reordered["class_iou"] = {
            name: MEASURED_METRICS["class_iou"][name]
            for name in reversed(TARGET_CLASSES)
        }
        path = self.tmp_path / "reordered.json"
        path.write_text(json.dumps(reordered), encoding="utf-8")

        with self.assertRaises(ValueError) as ctx:
            train_module.load_val_metrics(path)
        message = str(ctx.exception)
        self.assertIn("roadway", message)
        self.assertIn("class", message.lower())

    @requires("load_val_metrics")
    def test_reference_baseline_provenance_is_refused(self):
        copied = dict(MEASURED_METRICS, provenance="reference baseline")
        path = self.tmp_path / "copied.json"
        path.write_text(json.dumps(copied), encoding="utf-8")

        with self.assertRaises(ValueError) as ctx:
            train_module.load_val_metrics(path)
        self.assertIn("provenance", str(ctx.exception).lower())

    @requires("verify_metric_binding")
    def test_dataset_and_split_sha_must_match_the_run_manifest(self):
        run_manifest = {
            "dataset_manifest_sha256": "2" * 64,
            "split_sha256": "3" * 64,
        }
        train_module.verify_metric_binding(MEASURED_METRICS, run_manifest)

        drifted = dict(MEASURED_METRICS, split_sha256="4" * 64)
        with self.assertRaises(ValueError) as ctx:
            train_module.verify_metric_binding(drifted, run_manifest)
        self.assertIn("split_sha256", str(ctx.exception))

    @requires("verify_metric_binding")
    def test_weights_sha_git_sha_and_epochs_must_match_the_run_manifest(self):
        """AC10: the metrics must describe the same weights, commit and run."""
        run_manifest = {
            "weights_sha256": "1" * 64,
            "git_sha": "abc1234",
            "epochs": 100,
        }
        train_module.verify_metric_binding(
            dict(MEASURED_METRICS, git_sha="abc1234"), run_manifest
        )

        for drifted, expected in (
            (dict(MEASURED_METRICS, weights_sha256="9" * 64, git_sha="abc1234"), "weights_sha256"),
            (dict(MEASURED_METRICS, git_sha="def5678"), "git_sha"),
            (dict(MEASURED_METRICS, git_sha="abc1234", epochs_completed=70), "epochs"),
        ):
            with self.assertRaises(ValueError) as ctx:
                train_module.verify_metric_binding(drifted, run_manifest)
            self.assertIn(expected, str(ctx.exception))

    @requires("verify_metric_binding")
    def test_manifest_fields_absent_from_the_manifest_are_not_invented(self):
        """A partial manifest narrows the binding; it must never fabricate one."""
        train_module.verify_metric_binding(
            dict(MEASURED_METRICS, git_sha="abc1234"), {"epochs": 100}
        )


class TestVerifyGateCli(unittest.TestCase):
    """S12-T5 §2.7: measured input is mandatory for a promotion verdict."""

    def setUp(self):
        self.test_dir = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, self.test_dir)
        self.tmp_path = Path(self.test_dir)
        self.script = TRAIN_DIR / "train_yolov8_seg.py"

    @requires("load_val_metrics")
    def test_verify_gate_without_metrics_exits_2(self):
        completed = subprocess.run(
            [sys.executable, str(self.script), "--verify-gate", "--dry-run"],
            cwd=str(REPO_ROOT),
            capture_output=True,
            text=True,
            timeout=180,
        )

        self.assertEqual(
            completed.returncode,
            2,
            f"--verify-gate without --metrics must exit 2\n"
            f"stdout:\n{completed.stdout}\nstderr:\n{completed.stderr}",
        )
        self.assertIn("--metrics", completed.stdout + completed.stderr)

    @requires("load_val_metrics")
    def test_allow_untrained_head_keeps_unverified_and_exits_1_without_training(self):
        argv = [
            str(self.script),
            "--verify-gate",
            "--allow-untrained-head",
            "--dry-run",
            "--data",
            str(self.tmp_path / "absent.yaml"),
        ]

        with patch.object(sys, "argv", argv), patch.object(
            train_module, "train_yolov8_seg"
        ) as fake_train:
            with self.assertLogs(level="INFO") as logs:
                with self.assertRaises(SystemExit) as ctx:
                    train_module.main()

        self.assertEqual(ctx.exception.code, 1)
        fake_train.assert_not_called()
        self.assertTrue(any("UNVERIFIED" in line for line in logs.output))


class TestEpochStabilityMargin(unittest.TestCase):
    """S12-T5 §2.7 anti-cherry-pick: best epoch <= last-10 median + 0.02."""

    @requires("compute_epoch_stability", "assert_epoch_stability")
    def test_margin_at_the_0_02_boundary_is_stable(self):
        epochs = [0.500] * 10 + [0.510] * 9 + [0.530]

        stability = train_module.compute_epoch_stability(epochs)

        self.assertAlmostEqual(stability["best_map50_95"], 0.530, places=4)
        self.assertAlmostEqual(stability["last10_median_map50_95"], 0.510, places=4)
        self.assertAlmostEqual(stability["best_minus_median"], 0.020, places=4)
        self.assertTrue(stability["stable"])
        self.assertEqual(train_module.assert_epoch_stability(epochs), [])

    @requires("compute_epoch_stability", "assert_epoch_stability")
    def test_single_lucky_epoch_beyond_0_02_is_refused(self):
        epochs = [0.500] * 10 + [0.510] * 9 + [0.531]

        stability = train_module.compute_epoch_stability(epochs)
        failures = train_module.assert_epoch_stability(epochs)

        self.assertAlmostEqual(stability["best_minus_median"], 0.021, places=4)
        self.assertFalse(stability["stable"])
        self.assertEqual(len(failures), 1)
        message = failures[0].lower()
        self.assertTrue(
            any(token in message for token in ("stability", "cherry", "margin")),
            f"stability failure must explain itself: {failures[0]}",
        )

    @requires("assert_epoch_stability")
    def test_fewer_than_ten_epochs_cannot_claim_stability(self):
        failures = train_module.assert_epoch_stability([0.5] * 9)
        self.assertEqual(len(failures), 1)
        self.assertIn("10", failures[0])


class TestHeadStatusAndTrainingMetadata(unittest.TestCase):
    """S12-T5 §2.8 / AC14: trained means zero re-initialised tensors."""

    def setUp(self):
        self.test_dir = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, self.test_dir)
        self.tmp_path = Path(self.test_dir)

    @requires("derive_head_status")
    def test_zero_reinitialised_tensors_means_trained(self):
        self.assertEqual(train_module.derive_head_status(0), "trained")
        self.assertEqual(train_module.derive_head_status(36), "reinitialised_untrained")
        self.assertEqual(train_module.derive_head_status(1), "reinitialised_untrained")

    @requires("build_training_metadata_block")
    def test_training_block_carries_every_provenance_field(self):
        block = train_module.build_training_metadata_block(
            dataset_manifest_sha256="a" * 64,
            split_sha256="b" * 64,
            best_pt_sha256="c" * 64,
            val_metrics_sha256="d" * 64,
            git_sha="e" * 40,
            gpu="NVIDIA L4 24GB / 550.54.14",
            epochs=100,
            gate_verdict="PASSED",
            trained_at="2026-11-12T10:00:00+07:00",
        )

        expected = {
            "dataset_manifest_sha256",
            "split_sha256",
            "best_pt_sha256",
            "val_metrics_sha256",
            "git_sha",
            "gpu",
            "epochs",
            "gate_verdict",
            "trained_at",
        }
        self.assertEqual(set(block), expected)
        self.assertEqual(block["epochs"], 100)
        self.assertEqual(block["gate_verdict"], "PASSED")
        for field in ("dataset_manifest_sha256", "split_sha256", "best_pt_sha256",
                      "val_metrics_sha256"):
            self.assertRegex(block[field], r"^[0-9a-f]{64}$")
        self.assertRegex(block["git_sha"], r"^[0-9a-f]{7,40}$")
        self.assertIn("L4", block["gpu"])


if __name__ == "__main__":
    unittest.main()
