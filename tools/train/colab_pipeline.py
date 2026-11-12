#!/usr/bin/env python3
"""
colab_pipeline.py — Reproducible off-box CUDA fine-tuning, measured validation,
OpenVINO FP16 export and retraining-gate evaluation (Sprint 13, S13-T1).

`tools/train/train_yolov8_seg.py` deliberately stops at "weights + IR": it cannot
invent a measured `val_metrics.json`, and the training branch of its CLI exports
before any metric exists. This driver supplies the missing orchestration around
the *same* shipped functions so an off-box GPU run produces the complete AC10-AC16
evidence set:

  1. Pre-flight: Linux x86-64, CUDA GPU with >= 8 GB VRAM, >= 30 GB free disk,
     clean git tree, resolved git SHA, library versions.
  2. Dataset contract: frozen `split.json`, dataset `manifest.json`, six-class
     names, zero orphans, every class present in `val`.
  3. Fine-tune via the shipped CLI (`--epochs`, `--seed`, fixed hyperparameters
     from docs/sprints/s12_t5_spec.md §2.6).
  4. `model.val()` on the frozen val split for `best.pt` and `last.pt`; mask
     mAP50/mAP50-95 and per-class **mask-pixel IoU** accumulated from the
     validator's own mask batches (never the box confusion matrix).
  5. `val_metrics.json` validated by `load_val_metrics` (>= 50 epochs, frozen
     split, canonical class order, anti-cherry-pick stability margin).
  6. Gate evaluation through `evaluate_retraining_gate`, with measured IR size
     and p95 from `verify_openvino_ir` or an authoritative AVX2 evidence file.
  7. Final `export_to_openvino_fp16` with `--metrics` provenance, then an
     assertion that `metadata.yaml` reports `head_status: trained`,
     `reinitialised_tensors: 0` and a training block whose SHAs match the run.
  8. Deterministic zip bundle (weights, metrics, manifests, IR, reports, hashes).

The driver fails closed: no synthetic dataset, no CPU/MPS training, no gate PASS
from copied constants, and no bundle when the gate fails.

Usage:
  python3 tools/train/colab_pipeline.py \
      --data-dir /content/datasets/yolo_seg \
      --work-dir /content/kv_s13 \
      --export-dir models/yolov8n_seg_openvino_416 \
      --epochs 100 --seed 42 --device 0

Exit codes: 0 evidence complete, 1 pipeline/gate failure, 2 usage or host refusal.

Contract tests: tools/train/colab_pipeline_test.py
"""

from __future__ import annotations

import argparse
import csv
import hashlib
import importlib.util
import json
import logging
import os
import platform
import re
import shutil
import subprocess
import sys
import time
import zipfile
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Dict, List, Mapping, Optional, Sequence, Tuple

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(message)s",
    datefmt="%H:%M:%S",
)

TRAIN_DIR = Path(__file__).resolve().parent
REPO_ROOT = TRAIN_DIR.parents[1]
if str(TRAIN_DIR) not in sys.path:
    sys.path.insert(0, str(TRAIN_DIR))

from train_yolov8_seg import (  # noqa: E402 - sys.path must be extended first
    MIN_EVIDENCE_EPOCHS,
    TARGET_CLASSES,
    compute_epoch_stability,
    evaluate_retraining_gate,
    export_to_openvino_fp16,
    load_val_metrics,
    verify_metric_binding,
    verify_openvino_ir,
)

# ---------------------------------------------------------------------------
# Constants
# ---------------------------------------------------------------------------

EXIT_OK = 0
EXIT_FAILED = 1
EXIT_REFUSED = 2

MIN_VRAM_GB = 8.0
MIN_FREE_DISK_GB = 30.0
MIN_EPOCHS = MIN_EVIDENCE_EPOCHS
DEFAULT_EPOCHS = 100
DEFAULT_SEED = 42
DEFAULT_IMGSZ = 416
DEFAULT_BATCH = 16
DEFAULT_WORKERS = 4
DEFAULT_MODEL = "yolov8n-seg.pt"
RUN_NAME = "train_yolov8n_seg"
EXPORT_XML = "yolov8n_seg.xml"
EXPORT_BIN = "yolov8n_seg.bin"
EXPORT_METADATA = "metadata.yaml"
REFERENCE_PROVENANCE_TOKENS = ("reference baseline", "reference_baseline", "canonical_v2_metrics")


class PipelineError(RuntimeError):
    """The run cannot produce gate-eligible evidence."""


class HostRefusedError(PipelineError):
    """The host is not an accepted CUDA training host."""


# ---------------------------------------------------------------------------
# Small primitives
# ---------------------------------------------------------------------------


def sha256_file(path: Path) -> str:
    """SHA-256 of a file; used for every provenance binding."""
    digest = hashlib.sha256()
    with Path(path).open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def _now_iso() -> str:
    """Timezone-aware timestamp; every evidence artifact must carry an offset."""
    return datetime.now(timezone.utc).isoformat(timespec="seconds")


def _load_json(path: Path) -> Dict[str, Any]:
    try:
        payload = json.loads(Path(path).read_text(encoding="utf-8"))
    except FileNotFoundError as exc:
        raise PipelineError(f"required JSON artifact is missing: {path}") from exc
    except json.JSONDecodeError as exc:
        raise PipelineError(f"{path} is not valid JSON: {exc}") from exc
    if not isinstance(payload, dict):
        raise PipelineError(f"{path} must contain a JSON object")
    return payload


def _write_json(path: Path, payload: Mapping[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = path.with_suffix(path.suffix + ".tmp")
    temporary.write_text(json.dumps(payload, indent=2) + "\n", encoding="utf-8")
    os.replace(temporary, path)


def _is_sha256(value: Any) -> bool:
    return bool(re.fullmatch(r"[0-9a-f]{64}", str(value or "")))


def _require_sha256(value: Any, field: str) -> str:
    if not _is_sha256(value):
        raise PipelineError(f"{field} is not a 64-hex SHA-256: {value!r}")
    return str(value)


def _is_reference_provenance(value: Any) -> bool:
    text = str(value or "").strip().lower()
    return any(token in text for token in REFERENCE_PROVENANCE_TOKENS)


# ---------------------------------------------------------------------------
# Dataset contract
# ---------------------------------------------------------------------------


def read_dataset_yaml(path: Path) -> Dict[str, Any]:
    """Read the three fields the pipeline needs, with a no-PyYAML fallback.

    A stale absolute ``path:`` from another machine must never leak into a Colab
    run, so the runtime YAML is always rebuilt from the resolved data directory.
    """
    path = Path(path)
    if not path.is_file():
        raise PipelineError(f"dataset YAML not found: {path}")
    text = path.read_text(encoding="utf-8")
    try:
        import yaml  # type: ignore

        loaded = yaml.safe_load(text)
        if not isinstance(loaded, dict):
            raise PipelineError(f"dataset YAML {path} is not a mapping")
        names_raw = loaded.get("names") or {}
        if isinstance(names_raw, dict):
            names = {int(key): str(value) for key, value in names_raw.items()}
        elif isinstance(names_raw, list):
            names = {index: str(value) for index, value in enumerate(names_raw)}
        else:
            raise PipelineError(f"dataset YAML {path} has an unreadable names block")
        return {
            "path": str(loaded.get("path") or ""),
            "train": str(loaded.get("train") or ""),
            "val": str(loaded.get("val") or ""),
            "names": names,
        }
    except ImportError:
        pass

    # Minimal fallback: the materializer writes one canonical shape, so a tiny
    # state machine is enough for tests and bootstrap environments.
    names: Dict[int, str] = {}
    train = val = root = ""
    in_names = False
    for line in text.splitlines():
        stripped = line.split("#", 1)[0].rstrip()
        if not stripped.strip():
            continue
        if not line.startswith((" ", "\t")):
            in_names = stripped.strip() == "names:"
            if stripped.startswith("path:"):
                root = stripped.split(":", 1)[1].strip()
            elif stripped.startswith("train:"):
                train = stripped.split(":", 1)[1].strip()
            elif stripped.startswith("val:"):
                val = stripped.split(":", 1)[1].strip()
            continue
        if in_names:
            match = re.match(r"\s+(\d+)\s*:\s*(.+?)\s*$", stripped)
            if match:
                names[int(match.group(1))] = match.group(2).strip().strip("'\"")
    if not (train and val and names):
        raise PipelineError(f"dataset YAML {path} is missing train, val or names")
    return {"path": root, "train": train, "val": val, "names": names}


def inspect_dataset_tree(data_dir: Path) -> Dict[str, Any]:
    """Assert the materializer contract: 0 orphans, non-empty val, 6 classes.

    This is intentionally a *shape* check. Pixel/label provenance is bound by
    `split.json` and `manifest.json`, and the materializer is the only supported
    producer of both.
    """
    data_dir = Path(data_dir)
    manifest = read_dataset_yaml(data_dir / "dataset.yaml")
    names = [manifest["names"].get(index) for index in range(len(manifest["names"]))]
    if names != TARGET_CLASSES:
        raise PipelineError(
            f"dataset class order {names} does not match the canonical order "
            f"{TARGET_CLASSES}; a reordered dataset silently swaps water_puddle with another class"
        )

    summary: Dict[str, Any] = {"data_dir": str(data_dir), "splits": {}}
    for split in ("train", "val"):
        image_dir = data_dir / "images" / split
        label_dir = data_dir / "labels" / split
        if not image_dir.is_dir() or not label_dir.is_dir():
            raise PipelineError(
                f"dataset split '{split}' is incomplete: need both {image_dir} and {label_dir}"
            )
        images = {path.stem for path in image_dir.glob("*.jpg")}
        labels = {path.stem for path in label_dir.glob("*.txt")}
        if not images and not labels:
            raise PipelineError(
                f"dataset split '{split}' is empty; training on it would be fabricated"
            )
        if images != labels:
            missing_images = sorted(labels - images)[:5]
            missing_labels = sorted(images - labels)[:5]
            raise PipelineError(
                f"split '{split}' has {len(images - labels)} image(s) without labels "
                f"({missing_labels}) and {len(labels - images)} label(s) without images "
                f"({missing_images}); the dataset contract forbids orphans"
            )
        summary["splits"][split] = len(images)

    if summary["splits"]["val"] == 0:
        raise PipelineError("the frozen val split is empty; model.val() would measure nothing")

    observed: set[str] = set()
    val_labels = data_dir / "labels" / "val"
    for label_path in sorted(val_labels.glob("*.txt")):
        for line in label_path.read_text(encoding="utf-8").splitlines():
            token = line.strip().split()
            if not token:
                continue
            try:
                class_index = int(token[0])
            except ValueError as exc:
                raise PipelineError(f"label {label_path} has a non-integer class id") from exc
            if not 0 <= class_index < len(TARGET_CLASSES):
                raise PipelineError(
                    f"label {label_path} declares class id {class_index}, which is outside "
                    f"the 6-class taxonomy"
                )
            observed.add(TARGET_CLASSES[class_index])
    missing = [name for name in TARGET_CLASSES if name not in observed]
    if missing:
        raise PipelineError(
            f"the val split has no instance for {', '.join(missing)}; at least one per class "
            f"is required before the gate can be meaningful (spec AC8)"
        )
    summary["val_classes_present"] = sorted(observed)
    summary["class_names"] = list(TARGET_CLASSES)
    return summary


def load_materializer_module():
    """Import the materializer so its canonical digests are reused, not copied."""
    path = REPO_ROOT / "tools" / "eval" / "materialize_seg_dataset.py"
    if not path.is_file():
        raise PipelineError(f"materializer not found at {path}")
    spec = importlib.util.spec_from_file_location("kv_materialize_seg_dataset", path)
    if spec is None or spec.loader is None:
        raise PipelineError(f"cannot load materializer from {path}")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def bind_dataset_provenance(data_dir: Path, seed: int) -> Dict[str, Any]:
    """Return the frozen split digest and canonical dataset digest.

    Both values come from the materializer's own canonicalisers, so a hand-edited
    manifest cannot produce a different digest here than at build time.
    """
    data_dir = Path(data_dir)
    split_path = data_dir / "split.json"
    manifest_path = data_dir / "manifest.json"
    split = _load_json(split_path)
    manifest = _load_json(manifest_path)

    if int(split.get("seed", -1)) != int(seed):
        raise PipelineError(
            f"frozen split was created with seed {split.get('seed')} but this run uses "
            f"{seed}; re-splitting is a deliberate act, not a training option"
        )
    split_sha = _require_sha256(split.get("split_sha256"), "split.json split_sha256")
    bound_manifest_sha = _require_sha256(
        split.get("manifest_sha256"), "split.json manifest_sha256"
    )

    materializer = load_materializer_module()
    try:
        recomputed_split_sha = materializer.split_sha256(split)
    except Exception as exc:  # noqa: BLE001 - a malformed freeze is a refusal
        raise PipelineError(f"cannot recompute the frozen split digest: {exc}") from exc
    if recomputed_split_sha != split_sha:
        raise PipelineError(
            f"split.json digest {split_sha} does not match the assignment it describes "
            f"({recomputed_split_sha}); the freeze was edited after it was written"
        )
    canonical_manifest_sha = _require_sha256(
        materializer.manifest_sha256(manifest), "manifest.json canonical digest"
    )
    if canonical_manifest_sha != bound_manifest_sha:
        raise PipelineError(
            f"manifest.json digest {canonical_manifest_sha} does not match the digest bound "
            f"by split.json ({bound_manifest_sha}); the frame set drifted from the freeze"
        )
    if str(manifest.get("split_sha256") or "") != split_sha:
        raise PipelineError(
            "manifest.json split_sha256 does not match split.json; the manifest describes a "
            "different split than the one being trained"
        )

    return {
        "split_path": split_path,
        "manifest_path": manifest_path,
        "split_sha256": split_sha,
        "dataset_manifest_sha256": canonical_manifest_sha,
        "train": len(split.get("train") or []),
        "val": len(split.get("val") or []),
        "groups": int((split.get("groups") or {}).get("total", 0) or 0),
    }


def write_runtime_dataset_yaml(data_dir: Path, destination: Path) -> Path:
    """Re-root dataset.yaml at the local data directory without touching the source."""
    data_dir = Path(data_dir)
    lines = [
        "# Generated by tools/train/colab_pipeline.py: only `path` differs from",
        "# the materialized dataset.yaml, so a Colab path cannot leak into evidence.",
        f"path: {data_dir.resolve().as_posix()}",
        "train: images/train",
        "val: images/val",
        "names:",
    ]
    lines.extend(f"  {index}: {name}" for index, name in enumerate(TARGET_CLASSES))
    destination.parent.mkdir(parents=True, exist_ok=True)
    destination.write_text("\n".join(lines) + "\n", encoding="utf-8")
    return destination


# ---------------------------------------------------------------------------
# Training-run parsing
# ---------------------------------------------------------------------------


def _float_or_none(value: Any) -> Optional[float]:
    text = str(value or "").strip()
    if not text or text.lower() in {"nan", "none"}:
        return None
    try:
        parsed = float(text)
    except ValueError:
        return None
    return parsed if parsed == parsed else None


def parse_results_csv(path: Path) -> Dict[str, Any]:
    """Extract the epoch series and the checkpoint-selection epoch.

    Ultralytics selects `best.pt` by fitness = 0.1 * box mAP50 + 0.9 * mask
    mAP50-95, so `best_epoch` is derived from the same formula rather than from
    the mask maximum. The mask series drives the anti-cherry-pick margin.
    """
    path = Path(path)
    if not path.is_file():
        raise PipelineError(f"results.csv missing: {path}")
    with path.open("r", encoding="utf-8-sig", newline="") as handle:
        reader = csv.DictReader(handle)
        if reader.fieldnames is None:
            raise PipelineError(f"results.csv has no header: {path}")
        reader.fieldnames = [name.strip() for name in reader.fieldnames]

        mask_column = next(
            (
                name
                for name in reader.fieldnames
                if name == "metrics/mAP50-95(M)"
                or ("map50-95" in name.lower() and "(m)" in name.lower())
            ),
            None,
        )
        box_column = next(
            (
                name
                for name in reader.fieldnames
                if name == "metrics/mAP50(B)"
                or ("map50" in name.lower() and "(b)" in name.lower())
            ),
            None,
        )
        epoch_column = "epoch" if "epoch" in reader.fieldnames else None
        if mask_column is None:
            raise PipelineError(
                f"results.csv has no mask mAP50-95 column; found {reader.fieldnames}. "
                f"A detection-only column would measure boxes, not masks."
            )

        mask_series: List[float] = []
        fitness_series: List[float] = []
        epochs: List[int] = []
        for row in reader:
            mask_value = _float_or_none(row.get(mask_column))
            if mask_value is None:
                continue
            box_value = _float_or_none(row.get(box_column)) if box_column else None
            epoch_value = _float_or_none(row.get(epoch_column)) if epoch_column else None
            epochs.append(int(epoch_value) if epoch_value is not None else len(epochs) + 1)
            mask_series.append(mask_value)
            fitness_series.append(0.1 * (box_value or 0.0) + 0.9 * mask_value)

    if not mask_series:
        raise PipelineError(f"results.csv has no completed epochs: {path}")

    best_index = max(range(len(fitness_series)), key=fitness_series.__getitem__)
    stability = compute_epoch_stability(mask_series)
    return {
        "epochs_completed": len(mask_series),
        "epochs": epochs,
        "best_epoch": best_index + 1,
        "best_mask_map50_95": round(max(mask_series), 4),
        "last_mask_map50_95": round(mask_series[-1], 4),
        "mask_map50_95_series": [round(value, 6) for value in mask_series],
        "stability": stability,
        "mask_column": mask_column,
        "box_map50_column": box_column,
    }


# ---------------------------------------------------------------------------
# Mask-pixel IoU (not the box confusion matrix)
# ---------------------------------------------------------------------------


def iou_from_pixel_counts(counts: Mapping[str, Sequence[int]]) -> Dict[str, float]:
    """IoU_c = TP_c / (TP_c + FP_c + FN_c) for the canonical six classes."""
    result: Dict[str, float] = {}
    for name in TARGET_CLASSES:
        if name not in counts:
            raise PipelineError(f"mask-pixel counts are missing class '{name}'")
        tp, fp, fn = (int(value) for value in counts[name])
        union = tp + fp + fn
        result[name] = tp / union if union else 0.0
    return result


def accumulate_mask_pixel_counts(
    pred_masks: Any,
    pred_classes: Any,
    gt_masks: Any,
    gt_classes: Any,
    counts: Optional[Mapping[str, Sequence[int]]] = None,
) -> Dict[str, List[int]]:
    """Accumulate per-class TP/FP/FN pixels from one validator batch.

    Semantic union per class is the mask-pixel definition: two overlapping
    predicted puddles count once against the puddle GT union, exactly as a pixel
    confusion matrix would. Tensors may be torch or numpy; they are normalised to
    numpy here so this math is testable without a GPU.
    """
    import numpy as np

    def _to_numpy(value: Any) -> Any:
        if hasattr(value, "detach"):
            value = value.detach().cpu().numpy()
        return np.asarray(value)

    pred = _to_numpy(pred_masks) > 0.5
    gt = _to_numpy(gt_masks) > 0.5
    pred_cls = _to_numpy(pred_classes).astype(int).reshape(-1)
    gt_cls = _to_numpy(gt_classes).astype(int).reshape(-1)
    if pred.ndim != 3 or gt.ndim != 3:
        raise PipelineError(
            f"mask batches must be (n, h, w); got prediction {pred.shape} and GT {gt.shape}"
        )
    if pred.shape[1:] != gt.shape[1:]:
        raise PipelineError(
            f"mask batch geometry differs: prediction {pred.shape[1:]} vs GT {gt.shape[1:]}; "
            f"the validator was expected to align them before IoU accumulation"
        )
    if pred.shape[0] != pred_cls.size or gt.shape[0] != gt_cls.size:
        raise PipelineError(
            f"mask/class count mismatch: {pred.shape[0]} pred masks for {pred_cls.size} "
            f"classes, {gt.shape[0]} GT masks for {gt_cls.size} classes"
        )

    result: Dict[str, List[int]] = {
        name: [int(value) for value in (counts or {}).get(name, [0, 0, 0])]
        for name in TARGET_CLASSES
    }
    empty = np.zeros(pred.shape[1:], dtype=bool)
    for class_index, class_name in enumerate(TARGET_CLASSES):
        pred_sel = pred[pred_cls == class_index]
        gt_sel = gt[gt_cls == class_index]
        pred_union = pred_sel.any(axis=0) if pred_sel.shape[0] else empty
        gt_union = gt_sel.any(axis=0) if gt_sel.shape[0] else empty
        result[class_name][0] += int(np.count_nonzero(pred_union & gt_union))
        result[class_name][1] += int(np.count_nonzero(pred_union & ~gt_union))
        result[class_name][2] += int(np.count_nonzero(~pred_union & gt_union))
    return result


def _capture_mask_pixel_iou(
    model_path: Path,
    data_yaml: Path,
    device: str,
    imgsz: int,
    conf: float,
    iou: float,
    project: Path,
    name: str,
) -> Dict[str, Any]:
    """Run `model.val()` and accumulate per-class mask-pixel counts.

    Current Ultralytics exposes the *box* confusion matrix on `SegmentMetrics`.
    Using it for class IoU would label a box metric as a mask metric, so the
    counts are accumulated from the validator's own mask batches: predictions and
    ground truth are already letterboxed and at prototype resolution there.
    """
    try:
        import torch
        from ultralytics import YOLO
        from ultralytics.models.yolo.segment import SegmentationValidator
    except ImportError as exc:  # pragma: no cover - host dependency guard
        raise PipelineError(
            f"GPU validation requires ultralytics + torch + openvino: {exc}"
        ) from exc

    captured: List[Any] = []

    class CapturingSegmentValidator(SegmentationValidator):
        def __init__(self, *args: Any, **kwargs: Any) -> None:
            super().__init__(*args, **kwargs)
            self.pixel_counts: Dict[str, List[int]] = {
                class_name: [0, 0, 0] for class_name in TARGET_CLASSES
            }
            captured.append(self)

        def _process_batch(self, preds: Dict[str, Any], batch: Dict[str, Any]) -> Any:
            result = super()._process_batch(preds, batch)
            try:
                self._accumulate_mask_pixels(preds, batch)
            except Exception as exc:  # noqa: BLE001 - a metric failure is fatal, never silent
                raise PipelineError(
                    f"cannot accumulate mask-pixel IoU from the validator batch: {exc}. "
                    f"The Ultralytics validator internals changed; refusing to substitute "
                    f"the box confusion matrix."
                ) from exc
            return result

        def _accumulate_mask_pixels(
            self, preds: Dict[str, Any], batch: Dict[str, Any]
        ) -> None:
            pred_masks = preds.get("masks")
            pred_cls = preds.get("cls")
            gt_masks = batch.get("masks")
            gt_cls = batch.get("cls")
            if pred_masks is None or gt_masks is None:
                raise PipelineError("validator batch exposes no masks")
            if pred_masks.numel() and gt_masks.numel() and (
                pred_masks.shape[1:] != gt_masks.shape[1:]
            ):
                gt_masks = torch.nn.functional.interpolate(
                    gt_masks.float().unsqueeze(0),
                    size=tuple(pred_masks.shape[1:]),
                    mode="nearest",
                ).squeeze(0)
            self.pixel_counts = accumulate_mask_pixel_counts(
                pred_masks, pred_cls, gt_masks, gt_cls, self.pixel_counts
            )

    model = YOLO(str(model_path))
    metrics = model.val(
        validator=CapturingSegmentValidator,
        data=str(data_yaml),
        split="val",
        imgsz=int(imgsz),
        conf=float(conf),
        iou=float(iou),
        device=str(device),
        plots=True,
        project=str(project),
        name=str(name),
        exist_ok=True,
        verbose=True,
    )
    if not captured:
        raise PipelineError(
            "model.val() did not instantiate the capturing validator; mask-pixel IoU "
            "cannot be reported"
        )
    validator = captured[-1]

    names = getattr(metrics, "names", None) or getattr(validator, "names", None)
    if isinstance(names, dict):
        ordered_names = [names[index] for index in sorted(names)]
    else:
        ordered_names = list(names or [])
    if ordered_names != TARGET_CLASSES:
        raise PipelineError(
            f"validation class order {ordered_names} does not match the canonical order "
            f"{TARGET_CLASSES}; a reordered confusion matrix swaps class IoU silently"
        )

    try:
        mask_map50 = float(metrics.seg.map50)
        mask_map50_95 = float(metrics.seg.map)
    except AttributeError as exc:
        raise PipelineError(
            "model.val() returned no segmentation metrics; the checkpoint is not a "
            "segmentation model"
        ) from exc

    class_iou = iou_from_pixel_counts(validator.pixel_counts)
    return {
        "mask_map50": mask_map50,
        "mask_map50_95": mask_map50_95,
        "class_iou": class_iou,
        "pixel_counts": {name: list(values) for name, values in validator.pixel_counts.items()},
        "iou_method": "mask_pixel_union",
        "validator": type(validator).__name__,
    }


# ---------------------------------------------------------------------------
# Metrics artifact
# ---------------------------------------------------------------------------


def build_val_metrics(
    *,
    best: Mapping[str, Any],
    dataset_manifest_sha256: str,
    split_sha256: str,
    weights_sha256: str,
    epochs_completed: int,
    best_epoch: int,
    stability: Mapping[str, Any],
    generated_at: Optional[str] = None,
    conf: float = 0.25,
    iou: float = 0.5,
    imgsz: int = DEFAULT_IMGSZ,
    last: Optional[Mapping[str, Any]] = None,
    last_weights_sha256: Optional[str] = None,
    epochs_requested: int = DEFAULT_EPOCHS,
    seed: int = DEFAULT_SEED,
    git_sha: str = "",
    gpu: str = "",
) -> Dict[str, Any]:
    """Build the §5.2 artifact that `load_val_metrics` will accept."""
    if list(best.get("class_iou") or {}) != TARGET_CLASSES:
        raise PipelineError(
            f"class_iou order {list(best.get('class_iou') or {})} is not canonical "
            f"{TARGET_CLASSES}"
        )
    provenance = (
        "ultralytics model.val() on frozen val split; artifact=val_metrics.json; "
        f"git_sha={git_sha or 'unknown'}; gpu={gpu or 'unknown'}"
    )
    if _is_reference_provenance(provenance):
        raise PipelineError("internal provenance string names a reference baseline")

    payload: Dict[str, Any] = {
        "version": "1.0",
        "provenance": provenance,
        "split": "val",
        "conf": float(conf),
        "iou": float(iou),
        "imgsz": int(imgsz),
        "weights_sha256": _require_sha256(weights_sha256, "weights_sha256"),
        "git_sha": str(git_sha or ""),
        "dataset_manifest_sha256": _require_sha256(
            dataset_manifest_sha256, "dataset_manifest_sha256"
        ),
        "split_sha256": _require_sha256(split_sha256, "split_sha256"),
        "generated_at": generated_at or _now_iso(),
        "epochs_completed": int(epochs_completed),
        "epochs_requested": int(epochs_requested),
        "best_epoch": int(best_epoch),
        "seed": int(seed),
        "mask_map50": float(best["mask_map50"]),
        "mask_map50_95": float(best["mask_map50_95"]),
        "class_iou": {name: float(best["class_iou"][name]) for name in TARGET_CLASSES},
        "class_pixel_counts": dict(best.get("pixel_counts") or {}),
        "iou_method": str(best.get("iou_method") or "mask_pixel_union"),
        "stability": {
            "best_map50_95": float(stability.get("best_map50_95", best["mask_map50_95"])),
            "last10_median_map50_95": float(stability.get("last10_median_map50_95", 0.0)),
            "best_minus_median": float(stability.get("best_minus_median", 0.0)),
            "stable": bool(stability.get("stable", False)),
            "epochs": int(stability.get("epochs", epochs_completed)),
        },
    }
    if last is not None:
        payload["last_pt"] = {
            "weights_sha256": _require_sha256(
                last_weights_sha256 or "", "last_pt weights_sha256"
            ),
            "mask_map50": round(float(last["mask_map50"]), 6),
            "mask_map50_95": round(float(last["mask_map50_95"]), 6),
            "class_iou": {name: float(last["class_iou"][name]) for name in TARGET_CLASSES},
        }
    return payload


def validate_metric_provenance(
    metrics: Mapping[str, Any], run_manifest: Mapping[str, Any]
) -> None:
    """Bind the measured metrics to the run before the gate may see them."""
    try:
        verify_metric_binding(dict(metrics), dict(run_manifest))
    except ValueError as exc:
        raise PipelineError(str(exc)) from exc
    for field in (
        "dataset_manifest_sha256",
        "split_sha256",
        "weights_sha256",
    ):
        if field in run_manifest:
            _require_sha256(metrics.get(field), f"val_metrics.{field}")
    bound_git_sha = str(run_manifest.get("git_sha") or "")
    measured_git_sha = str(metrics.get("git_sha") or "")
    if bound_git_sha and not re.fullmatch(r"[0-9a-f]{7,40}", bound_git_sha):
        raise PipelineError(f"run_manifest.git_sha is not a git SHA: {bound_git_sha!r}")
    if bound_git_sha and measured_git_sha and measured_git_sha != bound_git_sha:
        raise PipelineError(
            f"val_metrics git_sha {measured_git_sha} does not match the run manifest "
            f"{bound_git_sha}"
        )
    if int(run_manifest.get("epochs", -1)) != int(metrics.get("epochs_completed", -2)):
        raise PipelineError(
            f"run manifest binds {run_manifest.get('epochs')} epoch(s) but val_metrics "
            f"reports {metrics.get('epochs_completed')}"
        )


# ---------------------------------------------------------------------------
# Host, export and metadata checks
# ---------------------------------------------------------------------------


def detect_cuda(gpu_index: int = 0) -> Dict[str, Any]:
    """Refuse anything that is not a CUDA GPU with enough VRAM (spec §2.6)."""
    try:
        import torch
    except ImportError as exc:  # pragma: no cover - host dependency guard
        raise HostRefusedError(f"torch is not installed: {exc}") from exc
    if not torch.cuda.is_available():
        raise HostRefusedError(
            "CUDA is not available. Off-box evidence requires a CUDA GPU (>= 8 GB VRAM); "
            "MPS and CPU runs are explicitly not accepted (spec §2.6)."
        )
    if gpu_index >= torch.cuda.device_count():
        raise HostRefusedError(
            f"requested CUDA device {gpu_index}, but only {torch.cuda.device_count()} "
            f"device(s) are visible"
        )
    properties = torch.cuda.get_device_properties(gpu_index)
    vram_gb = properties.total_memory / (1024**3)
    if vram_gb < MIN_VRAM_GB:
        raise HostRefusedError(
            f"GPU {properties.name} has {vram_gb:.2f} GB VRAM, below the {MIN_VRAM_GB} GB "
            f"floor; batch=16 at 416 px would OOM and a smaller batch is not the spec's run"
        )
    driver = "unknown"
    try:
        completed = subprocess.run(
            [
                "nvidia-smi",
                "--query-gpu=driver_version",
                "--format=csv,noheader",
                f"--id={gpu_index}",
            ],
            capture_output=True,
            text=True,
            timeout=15,
            check=False,
        )
        if completed.returncode == 0 and completed.stdout.strip():
            driver = completed.stdout.strip().splitlines()[0]
    except (OSError, subprocess.SubprocessError):
        pass
    return {
        "index": int(gpu_index),
        "name": str(properties.name),
        "vram_gb": round(vram_gb, 2),
        "driver": driver,
        "cuda_version": str(getattr(torch.version, "cuda", "") or ""),
        "torch_version": str(torch.__version__),
        "device": f"{properties.name} / driver {driver}",
    }


def check_disk_and_platform(work_dir: Path) -> Dict[str, Any]:
    if platform.system() != "Linux" or platform.machine().lower() not in {
        "x86_64",
        "amd64",
    }:
        raise HostRefusedError(
            f"off-box evidence requires Linux x86-64, got {platform.system()}/"
            f"{platform.machine()}"
        )
    work_dir.mkdir(parents=True, exist_ok=True)
    total, used, free = shutil.disk_usage(work_dir)
    free_gb = free / (1024**3)
    if free_gb < MIN_FREE_DISK_GB:
        raise HostRefusedError(
            f"only {free_gb:.1f} GB free under {work_dir}; the run needs >= {MIN_FREE_DISK_GB} "
            f"GB for the dataset, run artifacts and two IRs"
        )
    return {
        "platform": platform.platform(),
        "machine": platform.machine(),
        "python": platform.python_version(),
        "free_disk_gb": round(free_gb, 2),
        "total_disk_gb": round(total / (1024**3), 2),
    }


def git_state(allow_dirty: bool = False) -> Dict[str, Any]:
    """Resolve the commit SHA and refuse a dirty tree for AC10 evidence."""
    def _run(*args: str) -> subprocess.CompletedProcess:
        return subprocess.run(
            ["git", *args],
            cwd=REPO_ROOT,
            capture_output=True,
            text=True,
            timeout=30,
            check=False,
        )

    head = _run("rev-parse", "HEAD")
    sha = head.stdout.strip() if head.returncode == 0 else ""
    if not re.fullmatch(r"[0-9a-f]{7,40}", sha):
        raise PipelineError(
            f"cannot resolve the repository HEAD under {REPO_ROOT}; evidence requires a git SHA"
        )
    status = _run("status", "--porcelain")
    dirty_files = [line for line in status.stdout.splitlines() if line.strip()]
    branch = _run("rev-parse", "--abbrev-ref", "HEAD").stdout.strip()
    if dirty_files and not allow_dirty:
        preview = "; ".join(dirty_files[:5])
        raise PipelineError(
            f"working tree is dirty ({len(dirty_files)} path(s): {preview}); AC10 binds a "
            f"clean-tree SHA. Commit the run inputs or pass --allow-dirty for a diagnostic run."
        )
    return {"git_sha": sha, "branch": branch, "dirty": bool(dirty_files)}


def library_versions() -> Dict[str, str]:
    versions: Dict[str, str] = {}
    for module_name in ("ultralytics", "torch", "openvino", "numpy", "PIL", "yaml"):
        try:
            module = __import__(module_name)
            versions[module_name] = str(getattr(module, "__version__", "unknown"))
        except Exception:  # noqa: BLE001 - optional provenance, never fatal
            versions[module_name] = "not-installed"
    return versions


def validate_metadata_payload(
    payload: Mapping[str, Any], expected: Mapping[str, Any]
) -> Dict[str, Any]:
    """Assert AC14 on the exporter's metadata without trusting a human edit."""
    projectkv = payload.get("projectkv")
    if not isinstance(projectkv, dict):
        raise PipelineError("metadata.yaml has no projectkv block")
    if projectkv.get("head_status") != "trained":
        raise PipelineError(
            f"metadata head_status is {projectkv.get('head_status')!r}, not 'trained'; "
            f"an untrained head emits zero detections and must never be promoted"
        )
    if int(projectkv.get("reinitialised_tensors", -1)) != 0:
        raise PipelineError(
            f"metadata reinitialised_tensors is {projectkv.get('reinitialised_tensors')!r}, "
            f"not 0; the class head was rebuilt from random weights"
        )
    if int(projectkv.get("class_count", 0)) != len(TARGET_CLASSES):
        raise PipelineError(
            f"metadata class_count is {projectkv.get('class_count')!r}; expected "
            f"{len(TARGET_CLASSES)}"
        )
    if int(projectkv.get("output_channels", 0)) != 4 + len(TARGET_CLASSES) + 32:
        raise PipelineError(
            f"metadata output_channels is {projectkv.get('output_channels')!r}; the cv-engine "
            f"decodes 42 channels (4 box + 6 class + 32 mask)"
        )
    if projectkv.get("class_names") != list(TARGET_CLASSES):
        raise PipelineError("metadata class_names do not match the canonical order")

    training = projectkv.get("training")
    if not isinstance(training, dict):
        raise PipelineError(
            "metadata has no training block; export without --metrics cannot bind the "
            "weights to the dataset, split and measured val_metrics"
        )
    for field in (
        "dataset_manifest_sha256",
        "split_sha256",
        "best_pt_sha256",
        "val_metrics_sha256",
    ):
        _require_sha256(training.get(field), f"training.{field}")
        if field in expected and str(training[field]) != str(expected[field]):
            raise PipelineError(
                f"training.{field} is {training[field]}, expected {expected[field]}; the "
                f"export is not bound to this run's artifacts"
            )
    if int(training.get("epochs", -1)) != int(expected.get("epochs", -2)):
        raise PipelineError(
            f"training.epochs is {training.get('epochs')!r}, expected {expected.get('epochs')}"
        )
    if str(training.get("git_sha") or "") != str(expected.get("git_sha") or ""):
        raise PipelineError(
            f"training.git_sha is {training.get('git_sha')!r}, expected {expected.get('git_sha')!r}"
        )
    if not str(training.get("trained_at") or "").strip():
        raise PipelineError("training.trained_at is empty")
    return dict(training)


def read_metadata(path: Path) -> Dict[str, Any]:
    try:
        import yaml  # type: ignore
    except ImportError as exc:  # pragma: no cover - installed in the notebook
        raise PipelineError(f"reading metadata.yaml requires pyyaml: {exc}") from exc
    payload = yaml.safe_load(Path(path).read_text(encoding="utf-8"))
    if not isinstance(payload, dict):
        raise PipelineError(f"{path} is not a YAML mapping")
    return payload


def resolve_latency(
    export_evidence: Mapping[str, Any],
    latency_evidence_path: Optional[Path],
    bin_path: Path,
) -> Dict[str, Any]:
    """Use AVX2 worker evidence when supplied; otherwise label a provisional number."""
    if latency_evidence_path is not None:
        payload = _load_json(Path(latency_evidence_path))
        if payload.get("head_status") != "trained":
            raise PipelineError(
                f"latency evidence {latency_evidence_path} was measured on head_status "
                f"{payload.get('head_status')!r}, not 'trained'"
            )
        if str(payload.get("ir_sha256") or "") != sha256_file(bin_path):
            raise PipelineError(
                f"latency evidence {latency_evidence_path} was measured on a different IR "
                f"({payload.get('ir_sha256')} vs {sha256_file(bin_path)})"
            )
        latency = payload.get("latency") or {}
        p95 = _float_or_none(latency.get("p95_ms"))
        if p95 is None:
            raise PipelineError(f"latency evidence {latency_evidence_path} has no p95_ms")
        return {
            "p95_latency_ms": float(p95),
            "provenance": str(latency_evidence_path),
            "authoritative": True,
            "host": payload.get("host") or {},
        }
    return {
        "p95_latency_ms": float(export_evidence["latency_p95_ms"]),
        "provenance": "local OpenVINO CPU measurement (provisional; AVX2 worker evidence pending)",
        "authoritative": False,
        "host": {},
    }


# ---------------------------------------------------------------------------
# Bundle
# ---------------------------------------------------------------------------


def _bundle_checksums(root: Path) -> str:
    lines = []
    for path in sorted(root.rglob("*")):
        if not path.is_file() or path.name == "checksums.sha256":
            continue
        lines.append(f"{sha256_file(path)}  {path.relative_to(root).as_posix()}")
    return "\n".join(lines) + "\n"


def create_bundle(
    work_dir: Path, items: Mapping[str, Path], readme: str
) -> Tuple[Path, Dict[str, Any]]:
    """Create a deterministic zip from a flat manifest of relative -> source paths."""
    work_dir = Path(work_dir)
    staging = work_dir / "_bundle_staging"
    shutil.rmtree(staging, ignore_errors=True)
    staging.mkdir(parents=True, exist_ok=True)
    copied: List[str] = []
    for relative, source in items.items():
        source = Path(source)
        if not source.is_file():
            raise PipelineError(f"bundle input is missing: {source}")
        destination = staging / relative
        destination.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(source, destination)
        copied.append(relative)

    readme_path = staging / "README.txt"
    readme_path.write_text(readme, encoding="utf-8")
    checksums_path = staging / "checksums.sha256"
    checksums_path.write_text(_bundle_checksums(staging), encoding="utf-8")

    files = sorted(path for path in staging.rglob("*") if path.is_file())
    zip_path = work_dir / "projectkv_s13_yolov8n_seg_bundle.zip"
    zip_path.parent.mkdir(parents=True, exist_ok=True)
    temporary = zip_path.with_suffix(zip_path.suffix + ".tmp")
    with zipfile.ZipFile(temporary, "w", compression=zipfile.ZIP_DEFLATED) as archive:
        for path in files:
            relative = path.relative_to(staging).as_posix()
            info = zipfile.ZipInfo(f"projectkv_s13_yolov8n_seg/{relative}")
            # Fixed timestamp makes the archive stable across runs of the same bytes.
            info.date_time = (1980, 1, 1, 0, 0, 0)
            info.external_attr = 0o644 << 16
            info.compress_type = zipfile.ZIP_DEFLATED
            archive.writestr(info, path.read_bytes())
    os.replace(temporary, zip_path)
    return zip_path, {
        "zip": str(zip_path),
        "sha256": sha256_file(zip_path),
        "bytes": zip_path.stat().st_size,
        "files": sorted(copied + ["README.txt", "checksums.sha256"]),
    }


# ---------------------------------------------------------------------------
# Orchestration
# ---------------------------------------------------------------------------


def _run_training(
    *,
    data_yaml: Path,
    work_dir: Path,
    export_dir: Path,
    epochs: int,
    seed: int,
    imgsz: int,
    batch: int,
    workers: int,
    device: str,
    model: str,
    log_path: Path,
) -> Dict[str, Any]:
    run_root = work_dir / "runs" / "segment"
    command = [
        sys.executable,
        str(TRAIN_DIR / "train_yolov8_seg.py"),
        "--data",
        str(data_yaml),
        "--model",
        model,
        "--epochs",
        str(epochs),
        "--imgsz",
        str(imgsz),
        "--batch",
        str(batch),
        "--workers",
        str(workers),
        "--device",
        device,
        "--seed",
        str(seed),
        "--output-dir",
        str(run_root),
        "--export-openvino",
        str(export_dir),
        "--report",
        str(work_dir / "train_report.json"),
    ]
    log_path.parent.mkdir(parents=True, exist_ok=True)
    logging.info("Starting training: %s", " ".join(command))
    started = time.monotonic()
    with log_path.open("w", encoding="utf-8") as log:
        process = subprocess.Popen(
            command,
            cwd=REPO_ROOT,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
            bufsize=1,
        )
        assert process.stdout is not None
        for line in process.stdout:
            log.write(line)
            log.flush()
            sys.stdout.write(line)
            sys.stdout.flush()
        returncode = process.wait()
    if returncode != 0:
        raise PipelineError(
            f"training exited {returncode}; see {log_path} for the last lines"
        )
    return {"command": command, "seconds": round(time.monotonic() - started, 1)}


def _prepare_work_dir(work_dir: Path, overwrite: bool, keep_run: bool = False) -> None:
    run_dir = work_dir / "runs" / "segment" / RUN_NAME
    if run_dir.exists() and not keep_run:
        if not overwrite:
            raise PipelineError(
                f"{run_dir} already exists; Ultralytics would create a suffixed run and the "
                f"evidence paths would drift. Pass --overwrite to start a clean run."
            )
        shutil.rmtree(run_dir)
    for stale in (
        work_dir / "ir_provisional",
        work_dir / "ir_final",
        work_dir / "val",
        work_dir / "_bundle_staging",
    ):
        shutil.rmtree(stale, ignore_errors=True)


def run_pipeline(args: argparse.Namespace) -> Dict[str, Any]:
    data_dir = Path(args.data_dir).resolve()
    work_dir = Path(args.work_dir).resolve()
    export_dir = Path(args.export_dir)
    if not export_dir.is_absolute():
        export_dir = (REPO_ROOT / export_dir).resolve()

    # 1. Host and repository.
    host = detect_cuda(args.gpu_index)
    host.update(check_disk_and_platform(work_dir))
    repo = git_state(allow_dirty=args.allow_dirty)
    logging.info("Host accepted: %s (%.1f GB VRAM)", host["name"], host["vram_gb"])
    logging.info("Repository: %s @ %s", repo["branch"], repo["git_sha"])

    # 2. Dataset contract.
    tree = inspect_dataset_tree(data_dir)
    provenance = bind_dataset_provenance(data_dir, seed=args.seed)
    logging.info(
        "Dataset accepted: train=%d val=%d dataset_sha=%s split_sha=%s",
        tree["splits"]["train"],
        tree["splits"]["val"],
        provenance["dataset_manifest_sha256"][:16],
        provenance["split_sha256"][:16],
    )

    _prepare_work_dir(work_dir, overwrite=args.overwrite, keep_run=args.skip_training)
    runtime_yaml = write_runtime_dataset_yaml(data_dir, work_dir / "dataset.runtime.yaml")

    # 3. Fine-tune through the shipped CLI.
    started_at = _now_iso()
    if args.skip_training:
        run_dir = work_dir / "runs" / "segment" / RUN_NAME
        if not (run_dir / "weights" / "best.pt").is_file():
            raise PipelineError(
                f"--skip-training requires an existing {run_dir / 'weights' / 'best.pt'}"
            )
        training = {"command": ["<skipped>"], "seconds": 0}
    else:
        training = _run_training(
            data_yaml=runtime_yaml,
            work_dir=work_dir,
            export_dir=work_dir / "ir_provisional",
            epochs=args.epochs,
            seed=args.seed,
            imgsz=args.imgsz,
            batch=args.batch,
            workers=args.workers,
            device=str(args.gpu_index),
            model=args.model,
            log_path=work_dir / "train.log",
        )
    run_dir = work_dir / "runs" / "segment" / RUN_NAME
    best_pt = run_dir / "weights" / "best.pt"
    last_pt = run_dir / "weights" / "last.pt"
    if not best_pt.is_file():
        raise PipelineError(
            f"training produced no best.pt under {best_pt}; there is nothing to validate"
        )
    best_sha = sha256_file(best_pt)
    last_sha = sha256_file(last_pt) if last_pt.is_file() else None

    # 4. Measured validation on the frozen split, best and last.
    best_val = _capture_mask_pixel_iou(
        best_pt,
        runtime_yaml,
        device=str(args.gpu_index),
        imgsz=args.imgsz,
        conf=0.25,
        iou=0.5,
        project=work_dir / "val",
        name="best",
    )
    last_val = None
    if last_pt.is_file():
        last_val = _capture_mask_pixel_iou(
            last_pt,
            runtime_yaml,
            device=str(args.gpu_index),
            imgsz=args.imgsz,
            conf=0.25,
            iou=0.5,
            project=work_dir / "val",
            name="last",
        )
    logging.info(
        "Measured best.pt: mask mAP50=%.4f mAP50-95=%.4f water_puddle IoU=%.4f",
        best_val["mask_map50"],
        best_val["mask_map50_95"],
        best_val["class_iou"]["water_puddle"],
    )

    # 5. Stability and the measured artifact.
    results = parse_results_csv(run_dir / "results.csv")
    if results["epochs_completed"] < MIN_EPOCHS:
        message = (
            f"only {results['epochs_completed']} epoch(s) completed; AC10 requires "
            f">= {MIN_EPOCHS} for evidence"
        )
        if not args.allow_short_run:
            raise PipelineError(message + ". Re-run to at least the AC10 floor.")
        logging.warning("%s (permitted by --allow-short-run for diagnostics)", message)
    elif results["epochs_completed"] < args.epochs:
        logging.warning(
            "Training early-stopped at epoch %d of %d (patience=25). The run still clears "
            "the AC10 floor of %d, but the bundle records the actual count.",
            results["epochs_completed"],
            args.epochs,
            MIN_EPOCHS,
        )

    # A resume must bind to the same job; check before overwriting any artifact.
    prior_manifest: Dict[str, Any] = {}
    prior_manifest_path = run_dir / "run_manifest.json"
    if args.skip_training:
        if not prior_manifest_path.is_file():
            raise PipelineError(
                f"--skip-training requires the original {prior_manifest_path}; without it the "
                f"re-export cannot preserve the training command, timestamps and GPU provenance"
            )
        prior_manifest = _load_json(prior_manifest_path)
        for field, current in (
            ("git_sha", repo["git_sha"]),
            ("dataset_manifest_sha256", provenance["dataset_manifest_sha256"]),
            ("split_sha256", provenance["split_sha256"]),
            ("weights_sha256", best_sha),
        ):
            if prior_manifest.get(field) and str(prior_manifest[field]) != str(current):
                raise PipelineError(
                    f"--skip-training cannot resume across a changed {field}: the existing "
                    f"run manifest binds {prior_manifest.get(field)!r} but the current run is "
                    f"{current!r}"
                )
        if prior_manifest.get("epochs_requested") not in (None, args.epochs):
            raise PipelineError(
                f"--skip-training requested {args.epochs} epoch(s) but the original run "
                f"trained for {prior_manifest.get('epochs_requested')}"
            )

    val_metrics_path = run_dir / "val_metrics.json"
    prior_metrics: Dict[str, Any] = {}
    if args.skip_training and val_metrics_path.is_file():
        prior_metrics = _load_json(val_metrics_path)
    payload = build_val_metrics(
        best=best_val,
        last=last_val,
        last_weights_sha256=last_sha,
        dataset_manifest_sha256=provenance["dataset_manifest_sha256"],
        split_sha256=provenance["split_sha256"],
        weights_sha256=best_sha,
        epochs_completed=results["epochs_completed"],
        best_epoch=results["best_epoch"],
        stability=results["stability"],
        conf=0.25,
        iou=0.5,
        imgsz=args.imgsz,
        epochs_requested=args.epochs,
        seed=args.seed,
        git_sha=repo["git_sha"],
        gpu=host["device"],
        generated_at=prior_metrics.get("generated_at") or None,
    )
    if prior_metrics:
        payload["revalidated_at"] = _now_iso()
        payload["provenance"] = (
            f"{payload['provenance']}; revalidated_at={payload['revalidated_at']}"
        )
    _write_json(val_metrics_path, payload)
    measured = load_val_metrics(val_metrics_path, allow_short_run=args.allow_short_run)
    logging.info(
        "val_metrics.json written and accepted: %d epochs, best=%d, stability margin=%.4f",
        measured["epochs_completed"],
        measured["best_epoch"],
        measured["stability"]["best_minus_median"],
    )

    # 6. Run manifest binds the measurement to this exact job.
    finished_at = _now_iso()
    run_manifest = {
        "version": "1.0",
        "run_name": RUN_NAME,
        "git_sha": repo["git_sha"],
        "git_branch": repo["branch"],
        "git_dirty": repo["dirty"],
        "dataset_manifest_sha256": provenance["dataset_manifest_sha256"],
        "split_sha256": provenance["split_sha256"],
        "weights_sha256": best_sha,
        "last_weights_sha256": last_sha,
        "epochs": measured["epochs_completed"],
        "epochs_requested": args.epochs,
        "best_epoch": measured["best_epoch"],
        "seed": args.seed,
        "imgsz": args.imgsz,
        "batch": args.batch,
        "conf": 0.25,
        "iou": 0.5,
        "device": prior_manifest.get("device") or host["device"],
        "gpu": prior_manifest.get("gpu") or host,
        "library_versions": prior_manifest.get("library_versions") or library_versions(),
        "command": prior_manifest.get("command") or training["command"],
        "training_seconds": prior_manifest.get("training_seconds", training["seconds"]),
        "started_at": prior_manifest.get("started_at") or started_at,
        "finished_at": prior_manifest.get("finished_at") or finished_at,
        "dataset": {
            "path": str(data_dir),
            "train": tree["splits"]["train"],
            "val": tree["splits"]["val"],
        },
        "artifacts": {
            "best_pt": str(best_pt),
            "last_pt": str(last_pt) if last_pt.is_file() else None,
            "val_metrics": str(val_metrics_path),
            "results_csv": str(run_dir / "results.csv"),
        },
    }
    if prior_manifest:
        run_manifest["resumed_at"] = finished_at
    run_manifest_path = run_dir / "run_manifest.json"
    _write_json(run_manifest_path, run_manifest)
    validate_metric_provenance(measured, run_manifest)
    logging.info("run_manifest.json binds metrics, dataset, split, weights and git SHA")

    # 7. Gate inputs: measured IR size/p95, authoritative latency when provided.
    provisional_dir = work_dir / "ir_provisional"
    provisional_xml = provisional_dir / EXPORT_XML
    provisional_bin = provisional_dir / EXPORT_BIN
    if not provisional_xml.is_file() or not provisional_bin.is_file():
        logging.info("Provisional IR absent; exporting it from best.pt to measure latency")
        export_to_openvino_fp16(
            best_pt, provisional_dir, imgsz=args.imgsz, nc=len(TARGET_CLASSES), dry_run=False
        )
    provisional_evidence = verify_openvino_ir(
        provisional_xml,
        provisional_bin,
        imgsz=args.imgsz,
        nc=len(TARGET_CLASSES),
        warmup_runs=args.warmup_runs,
        timed_runs=args.timed_runs,
        also_verify=tuple(size for size in (320, args.imgsz) if size <= args.imgsz),
    )
    latency = resolve_latency(
        provisional_evidence,
        Path(args.latency_evidence) if args.latency_evidence else None,
        provisional_bin,
    )
    passed, summary, failures = evaluate_retraining_gate(
        metrics=measured,
        metrics_provenance=str(val_metrics_path),
        p95_latency_ms=latency["p95_latency_ms"],
        model_size_mb=float(provisional_evidence["total_size_mb"]),
    )
    summary.update(
        {
            "latency_provenance": latency["provenance"],
            "gate_latency_authoritative": latency["authoritative"],
            "latency_host": latency["host"],
            "ir_provisional_sha256": provisional_evidence["bin_sha256"],
            "ir_total_size_mb": provisional_evidence["total_size_mb"],
        }
    )
    if measured["epochs_completed"] < MIN_EPOCHS:
        summary["promotion_blocked"] = (
            f"{measured['epochs_completed']} completed epochs is below the AC10 floor of "
            f"{MIN_EPOCHS}"
        )
    gate_path = run_dir / "gate_summary.json"
    _write_json(gate_path, summary)
    logging.info("Gate verdict: %s", summary["verdict"])
    logging.info("Gate metrics provenance: %s", summary["metrics_provenance"])
    if not latency["authoritative"]:
        logging.warning(
            "p95 %.2f ms came from this host's OpenVINO CPU path, not the AVX2 worker; "
            "re-run the gate with --latency-evidence before promoting (AC18).",
            latency["p95_latency_ms"],
        )
    if not passed:
        for failure in failures:
            logging.error("  - %s", failure)
        raise PipelineError(
            f"retraining gate {summary['verdict']} with {len(failures)} violation(s); "
            f"no bundle was written (see {gate_path})"
        )

    # 8. Final export with complete provenance, validate it in staging, then deploy.
    training_context = {
        "gpu": host["device"],
        "epochs": measured["epochs_completed"],
        "gate_verdict": summary["verdict"],
        "git_sha": repo["git_sha"],
    }
    final_staging = work_dir / "ir_final"
    staged_xml, staged_bin = export_to_openvino_fp16(
        best_pt,
        final_staging,
        imgsz=args.imgsz,
        nc=len(TARGET_CLASSES),
        dry_run=False,
        dynamic=True,
        allow_untrained_head=False,
        metrics_path=val_metrics_path,
        dataset_manifest_path=provenance["manifest_path"],
        training_context=training_context,
    )
    if staged_xml is None or staged_bin is None:
        raise PipelineError("export returned no IR paths")
    final_evidence = verify_openvino_ir(
        staged_xml,
        staged_bin,
        imgsz=args.imgsz,
        nc=len(TARGET_CLASSES),
        warmup_runs=args.warmup_runs,
        timed_runs=args.timed_runs,
        also_verify=tuple(size for size in (320, args.imgsz) if size <= args.imgsz),
    )
    staged_metadata = final_staging / EXPORT_METADATA
    expected_training = {
        "dataset_manifest_sha256": provenance["dataset_manifest_sha256"],
        "split_sha256": provenance["split_sha256"],
        "best_pt_sha256": best_sha,
        "val_metrics_sha256": sha256_file(val_metrics_path),
        "git_sha": repo["git_sha"],
        "epochs": measured["epochs_completed"],
    }
    training_block = validate_metadata_payload(
        read_metadata(staged_metadata), expected_training
    )
    logging.info(
        "Staged IR verified: %.2f MiB, p95 %.2f ms, entropy %.2f; metadata trained/0",
        final_evidence["total_size_mb"],
        final_evidence["latency_p95_ms"],
        final_evidence["shannon_entropy_bits_per_byte"],
    )

    # The gate was evaluated on the provisional IR so its verdict could be exported
    # into metadata. Re-evaluate on the staged IR to prove the shipped bytes also
    # clear the size and latency ceilings; a verdict that only held for a temp file
    # would be evidence for an artifact nobody deploys.
    final_passed, final_summary, final_failures = evaluate_retraining_gate(
        metrics=measured,
        metrics_provenance=str(val_metrics_path),
        p95_latency_ms=latency["p95_latency_ms"],
        model_size_mb=float(final_evidence["total_size_mb"]),
    )
    if not final_passed:
        for failure in final_failures:
            logging.error("  - final IR: %s", failure)
        raise PipelineError(
            "the staged IR failed the gate that the provisional IR passed; "
            f"{len(final_failures)} violation(s)"
        )

    # Deploy only after every check passed, so a failed run can never leave a
    # half-written IR under models/ that a deploy script might pick up.
    staged_hashes = {
        EXPORT_XML: sha256_file(staged_xml),
        EXPORT_BIN: sha256_file(staged_bin),
        EXPORT_METADATA: sha256_file(staged_metadata),
    }
    export_dir.mkdir(parents=True, exist_ok=True)
    for staged, filename in (
        (staged_xml, EXPORT_XML),
        (staged_bin, EXPORT_BIN),
        (staged_metadata, EXPORT_METADATA),
    ):
        destination = export_dir / filename
        temporary = destination.with_suffix(destination.suffix + ".tmp")
        shutil.copy2(staged, temporary)
        os.replace(temporary, destination)
    xml_path = export_dir / EXPORT_XML
    bin_path = export_dir / EXPORT_BIN
    metadata_path = export_dir / EXPORT_METADATA
    for path in (xml_path, bin_path, metadata_path):
        if sha256_file(path) != staged_hashes[path.name]:
            raise PipelineError(
                f"deployed {path.name} differs from the validated staging copy"
            )
    validate_metadata_payload(read_metadata(metadata_path), expected_training)
    logging.info("IR deployed to %s", export_dir)

    summary["final_ir"] = {
        "total_size_mb": final_evidence["total_size_mb"],
        "latency_p95_ms": latency["p95_latency_ms"],
        "verdict": final_summary["verdict"],
        "deployed": str(export_dir),
    }
    _write_json(gate_path, summary)

    # 9. Evidence report and deterministic bundle.
    report = {
        "generated_at": _now_iso(),
        "verdict": summary["verdict"],
        "gate": summary,
        "failures": failures,
        "host": host,
        "repository": repo,
        "dataset": {**tree, **{k: str(v) for k, v in provenance.items()}},
        "training": {
            "command": training["command"],
            "seconds": training["seconds"],
            "epochs_completed": measured["epochs_completed"],
            "best_epoch": measured["best_epoch"],
            "stability": measured["stability"],
        },
        "metrics": {
            "best": {
                "mask_map50": best_val["mask_map50"],
                "mask_map50_95": best_val["mask_map50_95"],
                "class_iou": best_val["class_iou"],
            },
            "last": (
                {
                    "mask_map50": last_val["mask_map50"],
                    "mask_map50_95": last_val["mask_map50_95"],
                    "class_iou": last_val["class_iou"],
                }
                if last_val
                else None
            ),
            "val_metrics_sha256": sha256_file(val_metrics_path),
        },
        "export": {
            "xml": str(xml_path),
            "bin": str(bin_path),
            "xml_sha256": sha256_file(xml_path),
            "bin_sha256": sha256_file(bin_path),
            "metadata_sha256": sha256_file(metadata_path),
            "verification": final_evidence,
            "metadata_training": training_block,
        },
        "latency": latency,
    }
    report_path = work_dir / "evidence_report.json"
    _write_json(report_path, report)

    if measured["epochs_completed"] < MIN_EPOCHS:
        logging.warning(
            "Short diagnostic run (%d epochs): metrics, gate summary and IR were written, but "
            "no deployment bundle is produced and promotion remains blocked.",
            measured["epochs_completed"],
        )
        return {
            "status": "short_run",
            "verdict": summary["verdict"],
            "bundle": None,
            "val_metrics": str(val_metrics_path),
            "gate_summary": str(gate_path),
            "evidence_report": str(report_path),
            "metadata": str(metadata_path),
            "export_dir": str(export_dir),
        }

    mandatory: Dict[str, Path] = {
        "best.pt": best_pt,
        "val_metrics.json": val_metrics_path,
        "run_manifest.json": run_manifest_path,
        "gate_summary.json": gate_path,
        "evidence_report.json": report_path,
        "dataset/split.json": provenance["split_path"],
        "dataset/manifest.json": provenance["manifest_path"],
        "dataset/dataset.yaml": data_dir / "dataset.yaml",
        "dataset/dataset.runtime.yaml": runtime_yaml,
        f"models/{export_dir.name}/{EXPORT_XML}": xml_path,
        f"models/{export_dir.name}/{EXPORT_BIN}": bin_path,
        f"models/{export_dir.name}/{EXPORT_METADATA}": metadata_path,
    }
    if last_pt.is_file():
        mandatory["last.pt"] = last_pt
    for optional in ("train.log", "train_report.json"):
        candidate = work_dir / optional
        if candidate.is_file():
            mandatory[optional] = candidate
    for candidate in (
        run_dir / "results.csv",
        run_dir / "args.yaml",
        work_dir / "val" / "best" / "confusion_matrix.png",
    ):
        if candidate.is_file():
            mandatory[candidate.relative_to(work_dir).as_posix()] = candidate

    readme = (
        "ProjectKV S13-T1 YOLOv8n-seg deployment bundle\n"
        "================================================\n\n"
        f"Gate verdict: {summary['verdict']}\n"
        f"Val metrics provenance: {summary['metrics_provenance']}\n"
        f"Latency provenance: {latency['provenance']}\n"
        f"Git SHA: {repo['git_sha']} ({repo['branch']})\n"
        f"Dataset manifest SHA-256: {provenance['dataset_manifest_sha256']}\n"
        f"Split SHA-256: {provenance['split_sha256']}\n"
        f"best.pt SHA-256: {best_sha}\n"
        f"IR bin SHA-256: {sha256_file(bin_path)}\n"
        f"Epochs: {measured['epochs_completed']} (best epoch {measured['best_epoch']})\n"
        f"GPU: {host['device']}\n\n"
        "The IR is a real FP16 conversion of best.pt (head_status: trained,\n"
        "reinitialised_tensors: 0). Verify with checksums.sha256 before copying to the\n"
        "model volume; promote only after the AVX2 worker latency evidence passes.\n"
    )
    zip_path, bundle = create_bundle(work_dir, mandatory, readme)
    logging.info("Bundle: %s (%.2f MiB)", zip_path, bundle["bytes"] / (1024 * 1024))

    return {
        "status": "ok",
        "verdict": summary["verdict"],
        "bundle": bundle,
        "val_metrics": str(val_metrics_path),
        "gate_summary": str(gate_path),
        "evidence_report": str(report_path),
        "metadata": str(metadata_path),
        "export_dir": str(export_dir),
    }


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------


def parse_args(argv: Optional[Sequence[str]] = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description=(
            "Reproducible off-box CUDA fine-tune, measured gate and OpenVINO "
            "export (S13-T1)."
        )
    )
    parser.add_argument(
        "--data-dir",
        type=Path,
        required=True,
        help=(
            "Materialized Dataset v3 directory (dataset.yaml, split.json, "
            "manifest.json, images/, labels/)"
        ),
    )
    parser.add_argument(
        "--work-dir",
        type=Path,
        default=Path("runs/colab_s13_t1"),
        help=(
            "Run directory for logs, validation output and the bundle "
            "(default: runs/colab_s13_t1)"
        ),
    )
    parser.add_argument(
        "--export-dir",
        type=Path,
        default=Path("models/yolov8n_seg_openvino_416"),
        help="Final IR directory (default: models/yolov8n_seg_openvino_416)",
    )
    parser.add_argument("--epochs", type=int, default=DEFAULT_EPOCHS)
    parser.add_argument("--seed", type=int, default=DEFAULT_SEED)
    parser.add_argument("--imgsz", type=int, default=DEFAULT_IMGSZ)
    parser.add_argument("--batch", type=int, default=DEFAULT_BATCH)
    parser.add_argument("--workers", type=int, default=DEFAULT_WORKERS)
    parser.add_argument(
        "--gpu-index", type=int, default=0, help="CUDA device index (default: 0)"
    )
    parser.add_argument("--model", default=DEFAULT_MODEL)
    parser.add_argument(
        "--latency-evidence",
        type=Path,
        default=None,
        help="bench_seg_latency.py evidence JSON from the AVX2 worker; binds the IR SHA",
    )
    parser.add_argument("--warmup-runs", type=int, default=3)
    parser.add_argument("--timed-runs", type=int, default=25)
    parser.add_argument(
        "--overwrite",
        action="store_true",
        help="Remove a previous runs/segment/train_yolov8n_seg before training",
    )
    parser.add_argument(
        "--skip-training",
        action="store_true",
        help="Resume from an existing best.pt (validation, gate, export only)",
    )
    parser.add_argument(
        "--allow-short-run",
        action="store_true",
        help="Diagnostic only: accept < 50 epochs; promotion remains blocked",
    )
    parser.add_argument(
        "--allow-dirty",
        action="store_true",
        help="Diagnostic only: record a dirty-tree SHA (AC10 evidence requires clean)",
    )
    return parser.parse_args(argv)


def main(argv: Optional[Sequence[str]] = None) -> int:
    args = parse_args(argv)
    try:
        result = run_pipeline(args)
    except HostRefusedError as exc:
        logging.error("host refused: %s", exc)
        return EXIT_REFUSED
    except PipelineError as exc:
        logging.error("pipeline failed: %s", exc)
        return EXIT_FAILED
    except Exception as exc:  # noqa: BLE001 - never report a traceback as success
        logging.error("unexpected failure: %s", exc, exc_info=True)
        return EXIT_FAILED
    print(json.dumps(result, indent=2))
    return EXIT_FAILED if result.get("status") == "short_run" else EXIT_OK


if __name__ == "__main__":
    sys.exit(main())
