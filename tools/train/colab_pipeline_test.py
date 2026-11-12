#!/usr/bin/env python3
"""
Unit tests for tools/train/colab_pipeline.py and the Colab notebook it drives.

The GPU path (ULTRA-10) cannot execute in CI, so this suite pins every pure
contract the off-box run depends on:

  * `results.csv` parsing selects the checkpoint epoch by Ultralytics fitness and
    derives the anti-cherry-pick margin from the mask series;
  * mask-pixel IoU is TP/(TP+FP+FN), never the box confusion matrix;
  * `val_metrics.json` satisfies the S12-T5 §5.2 loader and the run-manifest
    binding (dataset, split, weights, git SHA, epochs);
  * exported `metadata.yaml` must be trained/0 with matching training SHAs;
  * latency evidence must be bound to the same IR and a trained head;
  * the deployment bundle is deterministic and complete;
  * the notebook itself is valid, unexecuted and contains the required steps.

The synthetic values below are unit-test fixtures only. They are never written
under docs/reports/ and cannot satisfy tools/ci/check_evidence_integrity.py.
"""

from __future__ import annotations

import importlib.util
import json
import shutil
import sys
import tempfile
import unittest
import zipfile
from contextlib import ExitStack
from pathlib import Path
from unittest.mock import patch

TRAIN_DIR = Path(__file__).resolve().parent
if str(TRAIN_DIR) not in sys.path:
    sys.path.insert(0, str(TRAIN_DIR))

REPO_ROOT = TRAIN_DIR.parents[1]
NOTEBOOK_PATH = TRAIN_DIR / "colab_train_yolov8_seg.ipynb"

import colab_pipeline as pipeline  # noqa: E402
from train_yolov8_seg import (  # noqa: E402
    TARGET_CLASSES,
    evaluate_retraining_gate,
    load_val_metrics,
)

GOOD_IOU = {
    "roadway": 0.762,
    "water_puddle": 0.624,
    "motorcycle": 0.638,
    "car": 0.702,
    "bus": 0.718,
    "truck": 0.669,
}
GOOD_STABILITY = {
    "best_map50_95": 0.528,
    "last10_median_map50_95": 0.512,
    "best_minus_median": 0.016,
    "stable": True,
    "epochs": 100,
}


def measured_best() -> dict:
    return {
        "mask_map50": 0.742,
        "mask_map50_95": 0.528,
        "class_iou": dict(GOOD_IOU),
        "pixel_counts": {name: [100, 10, 10] for name in TARGET_CLASSES},
        "iou_method": "mask_pixel_union",
    }


def write_results_csv(path: Path, rows: list[tuple[float, float]]) -> None:
    lines = ["epoch, metrics/mAP50(B), metrics/mAP50-95(M)"]
    for index, (box_map50, mask_map) in enumerate(rows, start=1):
        lines.append(f"{index}, {box_map50}, {mask_map}")
    path.write_text("\n".join(lines) + "\n", encoding="utf-8")


def create_fake_dataset(root: Path) -> Path:
    """Minimal materialized-tree shape: real digests, dummy JPEG bytes."""
    root.mkdir(parents=True, exist_ok=True)
    for split, count in (("train", 2), ("val", 1)):
        (root / "images" / split).mkdir(parents=True)
        (root / "labels" / split).mkdir(parents=True)
        for index in range(count):
            stem = f"{split}_{index}"
            (root / "images" / split / f"{stem}.jpg").write_bytes(b"jpeg")
            lines = ["0 0.1 0.1 0.2 0.1 0.2 0.2"] if split == "train" else [
                f"{class_index} 0.1 0.1 0.2 0.1 0.2 0.2"
                for class_index in range(len(TARGET_CLASSES))
            ]
            (root / "labels" / split / f"{stem}.txt").write_text(
                "\n".join(lines) + "\n", encoding="utf-8"
            )
    (root / "dataset.yaml").write_text(
        "path: /stale/path\n"
        "train: images/train\n"
        "val: images/val\n"
        "names:\n"
        + "\n".join(f"  {index}: {name}" for index, name in enumerate(TARGET_CLASSES))
        + "\n",
        encoding="utf-8",
    )
    return root


def write_frozen_provenance(root: Path) -> tuple[str, str]:
    """Write a self-consistent split.json + manifest.json into a fake dataset."""
    materializer = pipeline.load_materializer_module()
    split = {
        "seed": 42,
        "algorithm": "group_shuffle_camera_date",
        "groups": {},
        "train": ["train_0", "train_1"],
        "val": ["val_0"],
        "train_groups": ["g1"],
        "val_groups": ["g2"],
        "group_of": {"train_0": "g1", "train_1": "g1", "val_0": "g2"},
    }
    split_sha = materializer.split_sha256(split)
    manifest = {
        "version": "3.0",
        "total_frames": 3,
        "labels": [],
        "split_sha256": split_sha,
    }
    manifest_sha = materializer.manifest_sha256(manifest)
    frozen = dict(split)
    frozen.update(
        {"version": "3.0", "split_sha256": split_sha, "manifest_sha256": manifest_sha}
    )
    (root / "split.json").write_text(json.dumps(frozen), encoding="utf-8")
    (root / "manifest.json").write_text(json.dumps(manifest), encoding="utf-8")
    return split_sha, manifest_sha


class TestResultsParsing(unittest.TestCase):
    """AC10/AC13: the epoch series and the best.pt selection epoch are derived."""

    def setUp(self) -> None:
        self.temp_dir = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, self.temp_dir)

    def test_fitness_selects_the_checkpoint_epoch_and_stability_uses_mask_series(self):
        rows = [(0.70, 0.49)] * 15
        rows[4] = (1.00, 0.49)  # 0.1*1.00 + 0.9*0.49 = 0.541
        rows[-1] = (0.60, 0.51)  # 0.1*0.60 + 0.9*0.51 = 0.519
        path = self.temp_dir / "results.csv"
        write_results_csv(path, rows)

        parsed = pipeline.parse_results_csv(path)

        self.assertEqual(parsed["epochs_completed"], 15)
        self.assertEqual(parsed["best_epoch"], 5)
        self.assertEqual(parsed["best_mask_map50_95"], 0.51)
        self.assertEqual(parsed["stability"]["epochs"], 15)
        self.assertAlmostEqual(parsed["stability"]["best_minus_median"], 0.02, places=4)

    def test_nan_rows_are_not_counted_as_completed_epochs(self):
        path = self.temp_dir / "results.csv"
        path.write_text(
            "epoch,metrics/mAP50(B),metrics/mAP50-95(M)\n"
            "1,0.7,nan\n"
            "2,0.7,0.50\n"
            "3,0.7,\n",
            encoding="utf-8",
        )

        parsed = pipeline.parse_results_csv(path)

        self.assertEqual(parsed["epochs_completed"], 1)
        self.assertEqual(parsed["best_epoch"], 1)

    def test_a_detection_only_csv_is_refused(self):
        path = self.temp_dir / "results.csv"
        path.write_text(
            "epoch,metrics/mAP50(B),metrics/mAP50-95(B)\n1,0.7,0.5\n", encoding="utf-8"
        )

        with self.assertRaises(pipeline.PipelineError) as ctx:
            pipeline.parse_results_csv(path)
        self.assertIn("mask", str(ctx.exception).lower())


class TestMaskPixelIoU(unittest.TestCase):
    """§2.7: per-class IoU is mask-pixel, not box-instance agreement."""

    def test_iou_formula_and_zero_union(self):
        counts = {
            "roadway": [9, 1, 0],
            "water_puddle": [8, 2, 2],
            "motorcycle": [0, 0, 0],
            "car": [5, 5, 0],
            "bus": [4, 0, 1],
            "truck": [3, 1, 1],
        }

        iou = pipeline.iou_from_pixel_counts(counts)

        self.assertEqual(list(iou), TARGET_CLASSES)
        self.assertAlmostEqual(iou["roadway"], 0.9)
        self.assertAlmostEqual(iou["water_puddle"], 0.666667, places=6)
        self.assertEqual(iou["motorcycle"], 0.0)
        self.assertAlmostEqual(iou["bus"], 0.8)

    def test_missing_class_is_refused(self):
        with self.assertRaises(pipeline.PipelineError):
            pipeline.iou_from_pixel_counts({"roadway": [1, 0, 0]})

    def test_mask_union_counts_tp_fp_and_fn_per_class(self):
        try:
            import numpy as np
        except ImportError:  # pragma: no cover - CI has numpy
            raise unittest.SkipTest("numpy unavailable")

        pred = np.zeros((2, 10, 10), dtype=bool)
        pred[0, :5, :5] = True
        pred[1, :2, :2] = True
        gt = np.zeros((2, 10, 10), dtype=bool)
        gt[0, :4, :5] = True
        gt[1, :2, :2] = True
        gt[1, 5, 5] = True

        counts = pipeline.accumulate_mask_pixel_counts(
            pred, np.array([0, 1]), gt, np.array([0, 1])
        )

        self.assertEqual(counts["roadway"], [20, 5, 0])
        self.assertEqual(counts["water_puddle"], [4, 0, 1])
        self.assertEqual(counts["car"], [0, 0, 0])
        self.assertAlmostEqual(pipeline.iou_from_pixel_counts(counts)["roadway"], 0.8)

    def test_empty_predictions_count_as_false_negatives(self):
        try:
            import numpy as np
        except ImportError:  # pragma: no cover - CI has numpy
            raise unittest.SkipTest("numpy unavailable")

        gt = np.zeros((1, 10, 10), dtype=bool)
        gt[0, :2, :2] = True

        counts = pipeline.accumulate_mask_pixel_counts(
            np.zeros((0, 10, 10), dtype=bool),
            np.array([], dtype=int),
            gt,
            np.array([0]),
        )

        self.assertEqual(counts["roadway"], [0, 0, 4])
        self.assertEqual(counts["truck"], [0, 0, 0])


class TestValMetricsContract(unittest.TestCase):
    """AC10-AC13 / §5.2: only a measured, bound artifact may reach the gate."""

    def setUp(self) -> None:
        self.temp_dir = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, self.temp_dir)

    def _payload(self, **overrides) -> dict:
        kwargs = {
            "best": measured_best(),
            "dataset_manifest_sha256": "a" * 64,
            "split_sha256": "b" * 64,
            "weights_sha256": "c" * 64,
            "epochs_completed": 100,
            "best_epoch": 87,
            "stability": GOOD_STABILITY,
            "generated_at": pipeline._now_iso(),
            "last": measured_best(),
            "last_weights_sha256": "d" * 64,
            "git_sha": "e" * 40,
            "gpu": "Tesla T4 / driver 550.54.14",
        }
        kwargs.update(overrides)
        return pipeline.build_val_metrics(**kwargs)

    def test_artifact_loads_and_passes_the_gate_with_artifact_provenance(self):
        path = self.temp_dir / "val_metrics.json"
        pipeline._write_json(path, self._payload())

        metrics = load_val_metrics(path)
        passed, summary, failures = evaluate_retraining_gate(
            metrics=metrics,
            metrics_provenance=str(path),
            p95_latency_ms=14.2,
            model_size_mb=6.42,
        )

        self.assertTrue(passed, failures)
        self.assertEqual(summary["verdict"], "PASSED")
        self.assertEqual(summary["metrics_provenance"], str(path))
        self.assertEqual(metrics["epochs_completed"], 100)
        self.assertEqual(metrics["best_epoch"], 87)
        self.assertEqual(metrics["git_sha"], "e" * 40)
        self.assertEqual(metrics["last_pt"]["weights_sha256"], "d" * 64)
        self.assertEqual(metrics["iou_method"], "mask_pixel_union")
        self.assertNotIn("reference baseline", metrics["provenance"].lower())

    def test_short_run_is_refused_by_the_loader(self):
        path = self.temp_dir / "short.json"
        pipeline._write_json(
            path, self._payload(epochs_completed=49, best_epoch=41)
        )

        with self.assertRaises(ValueError) as ctx:
            load_val_metrics(path)
        self.assertIn("AC10", str(ctx.exception))

    def test_reference_provenance_is_refused(self):
        payload = self._payload()
        payload["provenance"] = "CANONICAL_V2_METRICS reference baseline"
        path = self.temp_dir / "reference.json"
        pipeline._write_json(path, payload)

        with self.assertRaises(ValueError):
            load_val_metrics(path)

    def test_run_manifest_binds_every_field(self):
        metrics = self._payload()
        run_manifest = {
            "dataset_manifest_sha256": "a" * 64,
            "split_sha256": "b" * 64,
            "weights_sha256": "c" * 64,
            "git_sha": "e" * 40,
            "epochs": 100,
        }

        pipeline.validate_metric_provenance(metrics, run_manifest)

        drifted = dict(run_manifest, split_sha256="f" * 64)
        with self.assertRaises(pipeline.PipelineError) as ctx:
            pipeline.validate_metric_provenance(metrics, drifted)
        self.assertIn("split_sha256", str(ctx.exception))

    def test_drifted_epoch_count_is_refused(self):
        with self.assertRaises(pipeline.PipelineError):
            pipeline.validate_metric_provenance(
                self._payload(), {"epochs": 99, "git_sha": "e" * 40}
            )


class TestDatasetContract(unittest.TestCase):
    """§2.4/§2.5: frozen split, zero orphans, every class represented in val."""

    def setUp(self) -> None:
        self.temp_dir = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, self.temp_dir)
        self.data_dir = create_fake_dataset(self.temp_dir / "yolo_seg")

    def test_tree_summary_counts_both_splits(self):
        summary = pipeline.inspect_dataset_tree(self.data_dir)

        self.assertEqual(summary["splits"], {"train": 2, "val": 1})
        self.assertEqual(summary["class_names"], TARGET_CLASSES)
        self.assertEqual(summary["val_classes_present"], sorted(TARGET_CLASSES))

    def test_orphan_label_is_refused(self):
        (self.data_dir / "labels" / "train" / "orphan.txt").write_text(
            "0 0.1 0.1 0.2 0.1 0.2 0.2\n", encoding="utf-8"
        )

        with self.assertRaises(pipeline.PipelineError) as ctx:
            pipeline.inspect_dataset_tree(self.data_dir)
        self.assertIn("orphan", str(ctx.exception).lower())

    def test_val_missing_a_class_is_refused(self):
        label = self.data_dir / "labels" / "val" / "val_0.txt"
        label.write_text("0 0.1 0.1 0.2 0.1 0.2 0.2\n", encoding="utf-8")

        with self.assertRaises(pipeline.PipelineError) as ctx:
            pipeline.inspect_dataset_tree(self.data_dir)
        self.assertIn("no instance", str(ctx.exception))

    def test_runtime_yaml_re_roots_path_without_touching_the_source(self):
        destination = self.temp_dir / "runtime.yaml"

        pipeline.write_runtime_dataset_yaml(self.data_dir, destination)

        text = destination.read_text(encoding="utf-8")
        self.assertIn(f"path: {self.data_dir.resolve().as_posix()}", text)
        self.assertIn("/stale/path", (self.data_dir / "dataset.yaml").read_text())

    def test_frozen_split_digest_must_match_its_assignment(self):
        materializer = pipeline.load_materializer_module()
        split = {
            "seed": 42,
            "algorithm": "group_shuffle_camera_date",
            "groups": {},
            "train": ["a"],
            "val": ["b"],
            "train_groups": [],
            "val_groups": [],
            "group_of": {"a": "g1", "b": "g2"},
        }
        manifest = {
            "version": "3.0",
            "total_frames": 2,
            "labels": [],
            "split_sha256": materializer.split_sha256(split),
        }
        manifest_sha = materializer.manifest_sha256(manifest)
        frozen = dict(split)
        frozen.update(
            {
                "version": "3.0",
                "split_sha256": materializer.split_sha256(split),
                "manifest_sha256": manifest_sha,
            }
        )
        (self.data_dir / "split.json").write_text(json.dumps(frozen), encoding="utf-8")
        (self.data_dir / "manifest.json").write_text(json.dumps(manifest), encoding="utf-8")

        bound = pipeline.bind_dataset_provenance(self.data_dir, seed=42)

        self.assertEqual(bound["split_sha256"], frozen["split_sha256"])
        self.assertEqual(bound["dataset_manifest_sha256"], manifest_sha)
        self.assertEqual((bound["train"], bound["val"]), (1, 1))

        frozen["val"] = ["a"]
        (self.data_dir / "split.json").write_text(json.dumps(frozen), encoding="utf-8")
        with self.assertRaises(pipeline.PipelineError) as ctx:
            pipeline.bind_dataset_provenance(self.data_dir, seed=42)
        self.assertIn("edited", str(ctx.exception).lower())


class TestMetadataAssertions(unittest.TestCase):
    """AC14: trained/0 + a training block bound to this run's SHAs."""

    def _metadata(self) -> dict:
        return {
            "projectkv": {
                "head_status": "trained",
                "reinitialised_tensors": 0,
                "class_count": 6,
                "class_names": list(TARGET_CLASSES),
                "output_channels": 42,
                "training": {
                    "dataset_manifest_sha256": "a" * 64,
                    "split_sha256": "b" * 64,
                    "best_pt_sha256": "c" * 64,
                    "val_metrics_sha256": "d" * 64,
                    "git_sha": "e" * 40,
                    "epochs": 100,
                    "trained_at": "2026-11-12T10:00:00+00:00",
                },
            }
        }

    def test_complete_training_block_is_accepted(self):
        expected = {
            "dataset_manifest_sha256": "a" * 64,
            "split_sha256": "b" * 64,
            "best_pt_sha256": "c" * 64,
            "val_metrics_sha256": "d" * 64,
            "git_sha": "e" * 40,
            "epochs": 100,
        }

        block = pipeline.validate_metadata_payload(self._metadata(), expected)

        self.assertEqual(block["epochs"], 100)
        self.assertEqual(block["git_sha"], "e" * 40)

    def test_untrained_head_is_refused(self):
        payload = self._metadata()
        payload["projectkv"]["head_status"] = "reinitialised_untrained"

        with self.assertRaises(pipeline.PipelineError) as ctx:
            pipeline.validate_metadata_payload(payload, {})
        self.assertIn("head_status", str(ctx.exception))

    def test_nonzero_reinitialised_tensors_is_refused(self):
        payload = self._metadata()
        payload["projectkv"]["reinitialised_tensors"] = 36

        with self.assertRaises(pipeline.PipelineError) as ctx:
            pipeline.validate_metadata_payload(payload, {})
        self.assertIn("reinitialised_tensors", str(ctx.exception))

    def test_sha_mismatch_is_refused(self):
        expected = {"best_pt_sha256": "f" * 64}

        with self.assertRaises(pipeline.PipelineError) as ctx:
            pipeline.validate_metadata_payload(self._metadata(), expected)
        self.assertIn("best_pt_sha256", str(ctx.exception))


class TestLatencyEvidence(unittest.TestCase):
    """AC18: only a trained head's own IR may supply the gate p95."""

    def setUp(self) -> None:
        self.temp_dir = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, self.temp_dir)
        self.bin_path = self.temp_dir / "yolov8n_seg.bin"
        self.bin_path.write_bytes(b"weights")
        self.evidence_path = self.temp_dir / "latency.json"

    def _evidence(self, **overrides) -> dict:
        payload = {
            "head_status": "trained",
            "ir_sha256": pipeline.sha256_file(self.bin_path),
            "latency": {"p95_ms": 13.7},
            "host": {"cpu": "AMD Ryzen 7 6800HS"},
        }
        payload.update(overrides)
        return payload

    def test_bound_evidence_becomes_the_gate_number(self):
        pipeline._write_json(self.evidence_path, self._evidence())

        resolved = pipeline.resolve_latency(
            {"latency_p95_ms": 99.0}, self.evidence_path, self.bin_path
        )

        self.assertTrue(resolved["authoritative"])
        self.assertEqual(resolved["p95_latency_ms"], 13.7)
        self.assertEqual(resolved["provenance"], str(self.evidence_path))

    def test_a_different_ir_is_refused(self):
        pipeline._write_json(self.evidence_path, self._evidence(ir_sha256="f" * 64))

        with self.assertRaises(pipeline.PipelineError) as ctx:
            pipeline.resolve_latency({}, self.evidence_path, self.bin_path)
        self.assertIn("different IR", str(ctx.exception))

    def test_untrained_evidence_is_refused(self):
        pipeline._write_json(
            self.evidence_path, self._evidence(head_status="reinitialised_untrained")
        )

        with self.assertRaises(pipeline.PipelineError) as ctx:
            pipeline.resolve_latency({}, self.evidence_path, self.bin_path)
        self.assertIn("trained", str(ctx.exception))

    def test_local_measurement_is_explicitly_provisional(self):
        resolved = pipeline.resolve_latency({"latency_p95_ms": 18.5}, None, self.bin_path)

        self.assertFalse(resolved["authoritative"])
        self.assertIn("provisional", resolved["provenance"])


class TestBundle(unittest.TestCase):
    """The deployable archive is complete, deterministic and self-verifying."""

    def setUp(self) -> None:
        self.temp_dir = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, self.temp_dir)
        self.source_dir = self.temp_dir / "sources"
        self.source_dir.mkdir()
        (self.source_dir / "best.pt").write_bytes(b"weights")
        (self.source_dir / "val_metrics.json").write_text("{}", encoding="utf-8")

    def test_bundle_contains_inputs_readme_and_checksums(self):
        zip_path, summary = pipeline.create_bundle(
            self.temp_dir,
            {
                "best.pt": self.source_dir / "best.pt",
                "evidence/val_metrics.json": self.source_dir / "val_metrics.json",
            },
            "readme",
        )

        self.assertTrue(zip_path.is_file())
        self.assertEqual(
            summary["files"],
            sorted(["best.pt", "evidence/val_metrics.json", "README.txt", "checksums.sha256"]),
        )
        with zipfile.ZipFile(zip_path) as archive:
            names = archive.namelist()
            self.assertTrue(all(name.startswith("projectkv_s13_yolov8n_seg/") for name in names))
            checksums = archive.read("projectkv_s13_yolov8n_seg/checksums.sha256").decode()
            self.assertIn("best.pt", checksums)
            self.assertIn("README.txt", checksums)
            self.assertEqual(
                {info.date_time for info in archive.infolist()}, {(1980, 1, 1, 0, 0, 0)}
            )

    def test_missing_input_is_refused(self):
        with self.assertRaises(pipeline.PipelineError):
            pipeline.create_bundle(
                self.temp_dir, {"best.pt": self.source_dir / "absent.pt"}, "readme"
            )


class TestWorkDirPreparation(unittest.TestCase):
    """A resume must not delete the weights it is meant to reuse."""

    def test_skip_training_preserves_the_existing_run(self):
        temp_dir = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, temp_dir)
        run_dir = temp_dir / "runs" / "segment" / pipeline.RUN_NAME
        (run_dir / "weights").mkdir(parents=True)
        best = run_dir / "weights" / "best.pt"
        best.write_bytes(b"weights")

        pipeline._prepare_work_dir(temp_dir, overwrite=True, keep_run=True)

        self.assertTrue(best.is_file())
        pipeline._prepare_work_dir(temp_dir, overwrite=True, keep_run=False)
        self.assertFalse(run_dir.exists())


class TestPipelineOrchestration(unittest.TestCase):
    """The full AC10-AC16 ordering, with only the hardware/ML calls stubbed.

    Everything that decides whether evidence is honest stays real: dataset digests,
    results.csv parsing, val_metrics validation, the gate, the run-manifest binding,
    metadata assertions and the bundle. Only training, model.val, export and host
    detection are replaced, because CI has no CUDA GPU or OpenVINO model.
    """

    def setUp(self) -> None:
        self.temp_dir = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, self.temp_dir)
        self.data_dir = create_fake_dataset(self.temp_dir / "dataset")
        self.split_sha, self.manifest_sha = write_frozen_provenance(self.data_dir)
        self.work_dir = self.temp_dir / "work"
        self.export_dir = self.work_dir / "models" / "yolov8n_seg_openvino_416"

    def _args(self):
        return pipeline.parse_args(
            [
                "--data-dir", str(self.data_dir),
                "--work-dir", str(self.work_dir),
                "--export-dir", str(self.export_dir),
                "--overwrite",
            ]
        )

    def test_complete_run_writes_bound_metrics_trained_metadata_and_bundle(self):
        run_dir = self.work_dir / "runs" / "segment" / pipeline.RUN_NAME

        def fake_training(**kwargs):
            (run_dir / "weights").mkdir(parents=True, exist_ok=True)
            (run_dir / "weights" / "best.pt").write_bytes(b"best-weights")
            (run_dir / "weights" / "last.pt").write_bytes(b"last-weights")
            write_results_csv(run_dir / "results.csv", [(0.70, 0.50)] * 100)
            (kwargs["log_path"]).write_text("stub training log\n", encoding="utf-8")
            return {"command": ["stub-training"], "seconds": 0.1}

        def fake_val(*args, **kwargs):
            return measured_best()

        def fake_export(source_weights, export_dir, **kwargs):
            export_dir = Path(export_dir)
            export_dir.mkdir(parents=True, exist_ok=True)
            xml_path = export_dir / pipeline.EXPORT_XML
            bin_path = export_dir / pipeline.EXPORT_BIN
            xml_path.write_text("<net/>", encoding="utf-8")
            bin_path.write_bytes(b"W" * 4096)
            metrics_path = kwargs.get("metrics_path")
            if metrics_path:
                metrics = json.loads(Path(metrics_path).read_text(encoding="utf-8"))
                context = kwargs.get("training_context") or {}
                metadata = {
                    "projectkv": {
                        "head_status": "trained",
                        "reinitialised_tensors": 0,
                        "class_count": len(TARGET_CLASSES),
                        "class_names": list(TARGET_CLASSES),
                        "output_channels": 42,
                        "training": {
                            "dataset_manifest_sha256": metrics["dataset_manifest_sha256"],
                            "split_sha256": metrics["split_sha256"],
                            "best_pt_sha256": pipeline.sha256_file(source_weights),
                            "val_metrics_sha256": pipeline.sha256_file(metrics_path),
                            "git_sha": context["git_sha"],
                            "epochs": metrics["epochs_completed"],
                            "trained_at": metrics["generated_at"],
                        },
                    }
                }
            else:
                metadata = {
                    "projectkv": {"head_status": "trained", "reinitialised_tensors": 0}
                }
            (export_dir / pipeline.EXPORT_METADATA).write_text(
                json.dumps(metadata), encoding="utf-8"
            )
            return xml_path, bin_path

        def fake_verify(xml_path, bin_path, **kwargs):
            return {
                "latency_p95_ms": 14.2,
                "total_size_mb": 6.5,
                "bin_sha256": pipeline.sha256_file(bin_path),
                "shannon_entropy_bits_per_byte": 7.9,
            }

        def fake_read_metadata(path):
            return json.loads(Path(path).read_text(encoding="utf-8"))

        host = {
            "index": 0,
            "name": "Tesla T4",
            "vram_gb": 15.0,
            "driver": "stub",
            "cuda_version": "12.4",
            "torch_version": "2.5",
            "device": "Tesla T4 / driver stub",
        }
        with ExitStack() as stack:
            stack.enter_context(patch.object(pipeline, "detect_cuda", lambda *a, **k: dict(host)))
            stack.enter_context(
                patch.object(
                    pipeline,
                    "check_disk_and_platform",
                    lambda work_dir: {
                        "platform": "Linux-stub",
                        "machine": "x86_64",
                        "python": "3.11",
                        "free_disk_gb": 80.0,
                        "total_disk_gb": 100.0,
                    },
                )
            )
            stack.enter_context(
                patch.object(
                    pipeline,
                    "git_state",
                    lambda allow_dirty=False: {
                        "git_sha": "e" * 40,
                        "branch": "test",
                        "dirty": False,
                    },
                )
            )
            stack.enter_context(patch.object(pipeline, "_run_training", fake_training))
            stack.enter_context(
                patch.object(pipeline, "_capture_mask_pixel_iou", fake_val)
            )
            stack.enter_context(
                patch.object(pipeline, "export_to_openvino_fp16", fake_export)
            )
            stack.enter_context(patch.object(pipeline, "verify_openvino_ir", fake_verify))
            stack.enter_context(patch.object(pipeline, "read_metadata", fake_read_metadata))

            result = pipeline.run_pipeline(self._args())

        self.assertEqual(result["status"], "ok")
        self.assertEqual(result["verdict"], "PASSED")

        metrics = json.loads(
            (run_dir / "val_metrics.json").read_text(encoding="utf-8")
        )
        self.assertEqual(metrics["epochs_completed"], 100)
        self.assertEqual(metrics["dataset_manifest_sha256"], self.manifest_sha)
        self.assertEqual(metrics["split_sha256"], self.split_sha)
        self.assertEqual(metrics["git_sha"], "e" * 40)

        manifest = json.loads(
            (run_dir / "run_manifest.json").read_text(encoding="utf-8")
        )
        self.assertEqual(manifest["dataset_manifest_sha256"], self.manifest_sha)
        self.assertEqual(manifest["split_sha256"], self.split_sha)
        self.assertEqual(manifest["git_sha"], "e" * 40)

        gate = json.loads((run_dir / "gate_summary.json").read_text(encoding="utf-8"))
        self.assertEqual(gate["verdict"], "PASSED")
        self.assertFalse(gate["gate_latency_authoritative"])

        bundle = Path(result["bundle"]["zip"])
        self.assertTrue(bundle.is_file())
        with zipfile.ZipFile(bundle) as archive:
            names = set(archive.namelist())
            prefix = "projectkv_s13_yolov8n_seg/"
            for required in (
                "best.pt",
                "last.pt",
                "val_metrics.json",
                "run_manifest.json",
                "gate_summary.json",
                "models/yolov8n_seg_openvino_416/metadata.yaml",
                "dataset/split.json",
                "dataset/manifest.json",
                "checksums.sha256",
            ):
                self.assertIn(prefix + required, names)


class TestSkipTrainingResume(unittest.TestCase):
    """A gate re-run with AVX2 evidence must preserve the original training record."""

    def setUp(self) -> None:
        self.temp_dir = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, self.temp_dir)
        self.data_dir = create_fake_dataset(self.temp_dir / "dataset")
        self.split_sha, self.manifest_sha = write_frozen_provenance(self.data_dir)
        self.work_dir = self.temp_dir / "work"
        self.export_dir = self.work_dir / "models" / "yolov8n_seg_openvino_416"
        self.run_dir = self.work_dir / "runs" / "segment" / pipeline.RUN_NAME
        (self.run_dir / "weights").mkdir(parents=True)
        (self.run_dir / "weights" / "best.pt").write_bytes(b"best-weights")
        (self.run_dir / "weights" / "last.pt").write_bytes(b"last-weights")
        write_results_csv(self.run_dir / "results.csv", [(0.70, 0.50)] * 100)
        weights_sha = pipeline.sha256_file(self.run_dir / "weights" / "best.pt")
        self.original_started = "2026-11-12T01:00:00+00:00"
        self.original_finished = "2026-11-12T04:00:00+00:00"
        pipeline._write_json(
            self.run_dir / "run_manifest.json",
            {
                "version": "1.0",
                "run_name": pipeline.RUN_NAME,
                "git_sha": "e" * 40,
                "dataset_manifest_sha256": self.manifest_sha,
                "split_sha256": self.split_sha,
                "weights_sha256": weights_sha,
                "epochs": 100,
                "epochs_requested": 100,
                "command": ["original", "training", "command"],
                "training_seconds": 10800,
                "started_at": self.original_started,
                "finished_at": self.original_finished,
                "library_versions": {"ultralytics": "8.4.190"},
                "device": "Tesla T4 / driver original",
                "gpu": {"name": "Tesla T4"},
            },
        )
        self.original_generated_at = "2026-11-12T04:05:00+00:00"
        val_metrics = pipeline.build_val_metrics(
            best=measured_best(),
            dataset_manifest_sha256=self.manifest_sha,
            split_sha256=self.split_sha,
            weights_sha256=weights_sha,
            epochs_completed=100,
            best_epoch=100,
            stability=GOOD_STABILITY,
            generated_at=self.original_generated_at,
            git_sha="e" * 40,
            gpu="Tesla T4 / driver original",
        )
        pipeline._write_json(self.run_dir / "val_metrics.json", val_metrics)

    def _args(self):
        return pipeline.parse_args(
            [
                "--data-dir", str(self.data_dir),
                "--work-dir", str(self.work_dir),
                "--export-dir", str(self.export_dir),
                "--skip-training",
                "--overwrite",
            ]
        )

    def test_resume_preserves_original_command_and_timestamps(self):
        def fake_export(source_weights, export_dir, **kwargs):
            export_dir = Path(export_dir)
            export_dir.mkdir(parents=True, exist_ok=True)
            xml = export_dir / pipeline.EXPORT_XML
            binary = export_dir / pipeline.EXPORT_BIN
            xml.write_text("<net/>", encoding="utf-8")
            binary.write_bytes(b"W" * 4096)
            metrics_path = kwargs.get("metrics_path")
            context = kwargs.get("training_context") or {}
            if metrics_path:
                metrics = json.loads(Path(metrics_path).read_text(encoding="utf-8"))
                metadata = {
                    "projectkv": {
                        "head_status": "trained",
                        "reinitialised_tensors": 0,
                        "class_count": 6,
                        "class_names": list(TARGET_CLASSES),
                        "output_channels": 42,
                        "training": {
                            "dataset_manifest_sha256": metrics["dataset_manifest_sha256"],
                            "split_sha256": metrics["split_sha256"],
                            "best_pt_sha256": pipeline.sha256_file(source_weights),
                            "val_metrics_sha256": pipeline.sha256_file(metrics_path),
                            "git_sha": context["git_sha"],
                            "epochs": metrics["epochs_completed"],
                            "trained_at": metrics["generated_at"],
                        },
                    }
                }
            else:
                metadata = {
                    "projectkv": {"head_status": "trained", "reinitialised_tensors": 0}
                }
            (export_dir / pipeline.EXPORT_METADATA).write_text(
                json.dumps(metadata), encoding="utf-8"
            )
            return xml, binary

        def fake_verify(xml_path, bin_path, **kwargs):
            return {
                "latency_p95_ms": 13.7,
                "total_size_mb": 6.4,
                "bin_sha256": pipeline.sha256_file(bin_path),
                "shannon_entropy_bits_per_byte": 7.9,
            }

        with ExitStack() as stack:
            stack.enter_context(
                patch.object(
                    pipeline,
                    "detect_cuda",
                    lambda *a, **k: {
                        "name": "Tesla T4",
                        "vram_gb": 15.0,
                        "device": "Tesla T4 / driver current",
                        "index": 0,
                        "driver": "current",
                        "cuda_version": "12.4",
                        "torch_version": "2.5",
                    },
                )
            )
            stack.enter_context(
                patch.object(
                    pipeline,
                    "check_disk_and_platform",
                    lambda work_dir: {
                        "platform": "Linux-stub",
                        "machine": "x86_64",
                        "python": "3.11",
                        "free_disk_gb": 80.0,
                        "total_disk_gb": 100.0,
                    },
                )
            )
            stack.enter_context(
                patch.object(
                    pipeline,
                    "git_state",
                    lambda allow_dirty=False: {
                        "git_sha": "e" * 40,
                        "branch": "test",
                        "dirty": False,
                    },
                )
            )
            stack.enter_context(
                patch.object(pipeline, "_capture_mask_pixel_iou", lambda *a, **k: measured_best())
            )
            stack.enter_context(patch.object(pipeline, "export_to_openvino_fp16", fake_export))
            stack.enter_context(patch.object(pipeline, "verify_openvino_ir", fake_verify))
            stack.enter_context(
                patch.object(
                    pipeline,
                    "read_metadata",
                    lambda path: json.loads(Path(path).read_text(encoding="utf-8")),
                )
            )
            result = pipeline.run_pipeline(self._args())

        self.assertEqual(result["status"], "ok")
        manifest = json.loads(
            (self.run_dir / "run_manifest.json").read_text(encoding="utf-8")
        )
        self.assertEqual(manifest["command"], ["original", "training", "command"])
        self.assertEqual(manifest["started_at"], self.original_started)
        self.assertEqual(manifest["finished_at"], self.original_finished)
        self.assertEqual(manifest["training_seconds"], 10800)
        self.assertEqual(manifest["device"], "Tesla T4 / driver original")
        self.assertIn("resumed_at", manifest)

        metrics = json.loads(
            (self.run_dir / "val_metrics.json").read_text(encoding="utf-8")
        )
        self.assertEqual(metrics["generated_at"], self.original_generated_at)
        self.assertIn("revalidated_at", metrics)
        self.assertEqual(metrics["weights_sha256"], manifest["weights_sha256"])


class TestNotebook(unittest.TestCase):
    """The deliverable notebook must be valid, unexecuted and complete."""

    @classmethod
    def setUpClass(cls):
        if not NOTEBOOK_PATH.is_file():
            raise unittest.SkipTest(f"notebook not written yet: {NOTEBOOK_PATH}")
        cls.payload = json.loads(NOTEBOOK_PATH.read_text(encoding="utf-8"))
        cls.code = "\n".join(
            "".join(cell.get("source") or [])
            for cell in cls.payload.get("cells", [])
            if cell.get("cell_type") == "code"
        )
        cls.all_text = "\n".join(
            "".join(cell.get("source") or []) for cell in cls.payload.get("cells", [])
        )

    def test_notebook_is_valid_nbformat_and_has_no_saved_outputs(self):
        self.assertEqual(self.payload.get("nbformat"), 4)
        self.assertGreaterEqual(int(self.payload.get("nbformat_minor", 0)), 4)
        self.assertTrue(self.payload.get("cells"))
        for index, cell in enumerate(self.payload["cells"]):
            if cell.get("cell_type") != "code":
                continue
            self.assertEqual(cell.get("outputs"), [], f"cell {index} has saved output")
            self.assertIsNone(
                cell.get("execution_count"), f"cell {index} has an execution count"
            )

    def test_dependency_install_covers_the_export_and_validation_stack(self):
        for package in ("ultralytics", "openvino", "pillow", "pyyaml"):
            self.assertIn(package, self.code.lower())

    def test_repository_and_branch_are_pinned(self):
        self.assertIn("529-studio/projectKV", self.all_text)
        self.assertIn("feat/s13-t1-gpu-finetune-and-avx2-soak", self.all_text)

    def test_training_arguments_are_the_spec_run(self):
        self.assertIn("--epochs", self.code)
        self.assertIn("100", self.code)
        self.assertIn("--seed", self.code)
        self.assertIn("42", self.code)
        self.assertIn("--data-dir", self.code)
        self.assertIn("colab_pipeline.py", self.code)

    def test_steps_assert_trained_head_and_build_the_deployment_bundle(self):
        self.assertIn("head_status", self.code)
        self.assertIn("trained", self.code)
        self.assertIn("reinitialised_tensors", self.code)
        self.assertIn("models/yolov8n_seg_openvino_416", self.all_text)
        self.assertIn("bundle.zip", self.code)

    def test_notebook_contains_no_credential_literals(self):
        # Env-var *names* are documentation; an assigned literal would be a leak.
        lowered = self.all_text.lower()
        forbidden = (
            "akia",
            "-----begin",
            "secret_access_key = \"",
            "password = \"",
            "token = \"",
        )
        for token in forbidden:
            self.assertNotIn(token, lowered)


if __name__ == "__main__":
    unittest.main()
