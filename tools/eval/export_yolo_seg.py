#!/usr/bin/env python3
"""
export_yolo_seg.py — Standalone Road Surface Water Segmentation Dataset Export CLI.

Part of Sprint 8 Issue #290 (S8-T8, Kanban [KV-20], ADR-009).
Exports authentic R2/local camera frames and ground-truth polygon/depth annotations
into Ultralytics YOLO segmentation directory structure:
  data/yolo_seg/
    images/train/, images/val/
    labels/train/, labels/val/
    dataset.yaml

Class Mapping (ADR-009 §4.2):
  0: roadway       (polygonal mask of drivable carriageway)
  1: water_puddle  (polygonal mask of accumulated standing water)
  2: motorcycle    (bounding polygon / box for motorcycles)
  3: car           (bounding polygon / box for passenger cars)
  4: bus           (bounding polygon / box for buses)
  5: truck         (bounding polygon / box for commercial trucks)

Usage:
  python3 tools/eval/export_yolo_seg.py --labels-dir tools/eval/labels_gold_standard --output-dir data/yolo_seg --verify

By default labels without explicit polygon `annotations` are SKIPPED (never
synthesized): fabricated geometry is opt-in via --allow-synthetic and is meant
for pipeline smoke tests only (ADR-011 D2).
"""

from __future__ import annotations

import argparse
import json
import logging
import os
import random
import shutil
import sys
from pathlib import Path
from typing import Any, Dict, List, Optional, Set, Tuple

try:
    from PIL import Image
    HAS_PIL = True
except ImportError:
    HAS_PIL = False

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(message)s",
    datefmt="%H:%M:%S",
)

TARGET_CLASSES = {
    0: "roadway",
    1: "water_puddle",
    2: "motorcycle",
    3: "car",
    4: "bus",
    5: "truck",
}

CLASS_NAME_TO_ID = {name: idx for idx, name in TARGET_CLASSES.items()}
# Synonyms or alternate names from other label conventions
CLASS_ALIASES = {
    "road": "roadway",
    "asphalt": "roadway",
    "carriageway": "roadway",
    "water": "water_puddle",
    "puddle": "water_puddle",
    "flood": "water_puddle",
    "flooded": "water_puddle",
    "motorbike": "motorcycle",
    "bike": "motorcycle",
    "mc": "motorcycle",
    "automobile": "car",
    "passenger_car": "car",
    "van": "car",
    "suv": "car",
    "coach": "bus",
    "container": "truck",
    "lorry": "truck",
}

# Roadway direction aliases (Issue #309 / S9-T7)
DIRECTION_ALIASES = {
    "a": "a",
    "dir_a": "a",
    "traffic_dir_a": "a",
    "near": "a",
    "b": "b",
    "dir_b": "b",
    "traffic_dir_b": "b",
    "far": "b",
}


def normalize_class_name(name: str) -> Optional[int]:
    """Resolves arbitrary class string to standard target class id (0..5)."""
    norm = name.strip().lower()
    if norm in CLASS_NAME_TO_ID:
        return CLASS_NAME_TO_ID[norm]
    if norm in CLASS_ALIASES:
        return CLASS_NAME_TO_ID[CLASS_ALIASES[norm]]
    return None


def normalize_direction(raw: Any) -> Optional[str]:
    """Resolves arbitrary direction string to canonical 'a' or 'b' (Issue #309 / S9-T7)."""
    if not raw or not isinstance(raw, str):
        return None
    norm = raw.strip().lower()
    return DIRECTION_ALIASES.get(norm)


# Sidecar filenames this exporter writes (`roadway_regions.json` at the output root and
# per split, plus `roadway_regions_{split}.json`). They hold region ground truth keyed
# to an existing split, not frame labels, so they must never be read back as input.
ROADWAY_SIDECAR_NAME = "roadway_regions.json"
ROADWAY_SIDECAR_PREFIX = "roadway_regions_"


def is_roadway_sidecar(path: Path) -> bool:
    """True for any roadway_regions sidecar this exporter emits."""
    name = path.name
    return name == ROADWAY_SIDECAR_NAME or (
        name.startswith(ROADWAY_SIDECAR_PREFIX) and name.endswith(".json")
    )


def extract_file_group_key(label_path: Path) -> Tuple[str, str]:
    """
    Extracts (camera_id, capture_date) group key for group-aware train/val split.
    Prevents burst leakage across the same camera on the same day (Lesson 68 / ADR-012 D6).
    """
    try:
        with open(label_path, "r", encoding="utf-8") as f:
            data = json.load(f)
        cam_id = data.get("camera_id") or ""
        date = data.get("date") or ""

        # If missing, try from snapshot_key (e.g. 2026-10-13/56de42f6.../1791...jpg)
        snapshot_key = data.get("snapshot_key") or data.get("image_path") or ""
        if (not cam_id or not date) and snapshot_key:
            parts = snapshot_key.replace("\\", "/").split("/")
            for p in parts:
                if len(p) == 10 and p.count("-") == 2:
                    if not date:
                        date = p
                elif len(p) >= 20 and not p.endswith((".jpg", ".png")):
                    if not cam_id:
                        cam_id = p

        # If still missing, check frame_id (e.g. cam_56de42f611f398ec0c481283_1792045040)
        frame_id = data.get("frame_id") or label_path.stem
        if not cam_id and frame_id.startswith("cam_"):
            parts = frame_id.split("_")
            if len(parts) >= 2:
                cam_id = parts[1]

        # Fall back to parent dir if date-like (YYYY-MM-DD)
        if not date and len(label_path.parent.name) == 10 and label_path.parent.name.count("-") == 2:
            date = label_path.parent.name

        cam_id = cam_id or label_path.stem.split("_")[0]
        date = date or "unknown"
        return cam_id, date
    except Exception:
        return label_path.stem, "unknown"


def partition_files_by_group(
    label_files: List[Path],
    val_split: float = 0.2,
    seed: int = 42,
) -> Tuple[List[Path], List[Path], Dict[str, Any]]:
    """
    Partitions label_files into (train_files, val_files) using group-aware splitting
    grouped strictly by (camera_id, capture_date).
    Guarantees zero overlapping camera-date groups between train and validation (Lesson 68).
    """
    groups: Dict[Tuple[str, str], List[Path]] = {}
    for p in label_files:
        gk = extract_file_group_key(p)
        groups.setdefault(gk, []).append(p)

    total_files = len(label_files)
    target_val = int(round(total_files * val_split))

    rng = random.Random(seed)
    unique_groups = sorted(groups.keys(), key=lambda g: f"{g[0]}:{g[1]}")
    rng.shuffle(unique_groups)

    train_files: List[Path] = []
    val_files: List[Path] = []
    train_groups: Set[Tuple[str, str]] = set()
    val_groups: Set[Tuple[str, str]] = set()

    current_val_count = 0
    for gk in unique_groups:
        file_list = groups[gk]
        if current_val_count + len(file_list) <= target_val or (current_val_count == 0 and target_val > 0):
            val_groups.add(gk)
            val_files.extend(file_list)
            current_val_count += len(file_list)
        else:
            train_groups.add(gk)
            train_files.extend(file_list)

    overlap = train_groups & val_groups
    split_info = {
        "total_groups": len(unique_groups),
        "train_groups": len(train_groups),
        "val_groups": len(val_groups),
        "train_files": len(train_files),
        "val_files": len(val_files),
        "overlap_count": len(overlap),
    }
    return train_files, val_files, split_info


def bbox_to_polygon_norm(
    bbox: List[float],
    img_w: float = 1.0,
    img_h: float = 1.0,
    is_norm: bool = False,
    fmt: str = "xyxy",
) -> List[float]:
    """
    Converts a bounding box into 4-point polygon coordinates [x1, y1, x2, y2, x3, y3, x4, y4]
    normalized in [0, 1].
    Supported formats:
      - 'xyxy': [x_min, y_min, x_max, y_max]
      - 'xywh': [x_min, y_min, width, height]
      - 'cxcywh': [center_x, center_y, width, height]
    """
    if len(bbox) != 4:
        raise ValueError(f"Expected 4 bbox elements, got {len(bbox)}")

    if not is_norm and (img_w <= 0 or img_h <= 0):
        img_w, img_h = 1920.0, 1080.0

    b0, b1, b2, b3 = bbox
    fmt_lower = fmt.lower()
    if fmt_lower == "cxcywh":
        xmin = b0 - b2 / 2.0
        ymin = b1 - b3 / 2.0
        xmax = b0 + b2 / 2.0
        ymax = b1 + b3 / 2.0
    elif fmt_lower == "xywh":
        xmin = b0
        ymin = b1
        xmax = b0 + b2
        ymax = b1 + b3
    else:  # xyxy default
        xmin, ymin, xmax, ymax = b0, b1, b2, b3

    if not is_norm:
        xmin /= img_w
        xmax /= img_w
        ymin /= img_h
        ymax /= img_h

    # Clamp to [0.0, 1.0]
    xmin = max(0.0, min(1.0, xmin))
    xmax = max(0.0, min(1.0, xmax))
    ymin = max(0.0, min(1.0, ymin))
    ymax = max(0.0, min(1.0, ymax))

    # Return 4 vertices clockwise: top-left, top-right, bottom-right, bottom-left
    return [xmin, ymin, xmax, ymin, xmax, ymax, xmin, ymax]


def get_image_dimensions(image_path: Path) -> Tuple[int, int]:
    """Reads image dimensions via PIL if available, falling back to 1920x1080."""
    if HAS_PIL and image_path.is_file():
        try:
            with Image.open(image_path) as img:
                return img.size[0], img.size[1]
        except Exception:
            pass
    return 1920, 1080


def synthesize_polygons_from_metadata(labels_meta: Dict[str, Any]) -> List[Tuple[int, List[float]]]:
    """
    Synthesizes standard perspective road and water polygons from scene metadata
    when explicit vertex annotations are not yet recorded in the label JSON.
    Adheres strictly to ADR-009 §4.2 and §5.1 specifications.
    """
    results: List[Tuple[int, List[float]]] = []

    # 1. Roadway canonical perspective carriageway mask (trapezoid covering active road)
    roadway_poly = [0.10, 0.95, 0.90, 0.95, 0.65, 0.40, 0.35, 0.40]
    results.append((0, roadway_poly))

    # 2. Water puddle mask based on flood severity and waterline depth
    flood_status = str(labels_meta.get("flood", "NONE")).upper()
    depth_anchor = str(labels_meta.get("waterline_depth", "DRY")).upper()
    submersion_anchor = str(labels_meta.get("flood_submersion_anchor", "dry")).lower()

    if flood_status == "DEEP" or depth_anchor in ("TIRE_HALF_SUBMERGED", "IMPASSABLE") or "over_15cm" in submersion_anchor:
        # Deep inundation covering large portion of roadway surface
        puddle_poly = [0.15, 0.95, 0.85, 0.95, 0.62, 0.48, 0.38, 0.48]
        results.append((1, puddle_poly))
    elif flood_status == "SHALLOW" or depth_anchor in ("TIRE_QUARTER_SUBMERGED",) or "under_10cm" in submersion_anchor:
        # Shallow curbside / carriageway pooling
        puddle_poly = [0.20, 0.92, 0.80, 0.92, 0.60, 0.60, 0.40, 0.60]
        results.append((1, puddle_poly))
    elif depth_anchor == "WET_SURFACE" or submersion_anchor == "wet_asphalt_no_standing":
        # Wet asphalt without standing puddle displacement (ADR-009 §4.3 Rule 1: do NOT mark as water_puddle)
        pass

    return results


def parse_label_file(
    label_path: Path,
    repo_root: Path,
    images_dir: Optional[Path] = None,
    allow_synthetic: bool = False,
) -> Tuple[Optional[Path], List[Tuple[int, List[float]]], bool, List[Dict[str, Any]]]:
    """
    Parses a single ground-truth JSON label file.
    Returns (resolved_image_path, list_of_(class_id, normalized_polygon_floats),
    used_synthetic, directional_roadway_regions).
    By default synthetic geometry is DISABLED (ADR-011 D2): labels without
    explicit polygon annotations return an empty segment list so the exporter
    skips them instead of training on fabricated shapes.
    """
    with open(label_path, "r", encoding="utf-8") as f:
        data = json.load(f)

    # 1. Resolve image path
    img_rel = data.get("image_path", "")
    resolved_img: Optional[Path] = None

    candidate_paths = []
    if img_rel:
        candidate_paths.append(repo_root / img_rel)
        if images_dir:
            candidate_paths.append(images_dir / img_rel)
            candidate_paths.append(images_dir / Path(img_rel).name)

    frame_id = data.get("frame_id", "")
    if frame_id and images_dir:
        candidate_paths.append(images_dir / f"{frame_id}.jpg")
        candidate_paths.append(images_dir / f"{frame_id}.png")

    for p in candidate_paths:
        if p.is_file():
            resolved_img = p
            break

    # Get dimensions
    img_w, img_h = get_image_dimensions(resolved_img) if resolved_img else (1920, 1080)

    # 2. Extract polygon segments and directional roadway regions (Issue #309 / S9-T7)
    segments: List[Tuple[int, List[float]]] = []
    directional_regions: List[Dict[str, Any]] = []

    # Check explicit polygon annotations
    raw_annotations = (
        data.get("annotations")
        or data.get("polygons")
        or data.get("segments")
        or data.get("labels", {}).get("annotations")
    )

    if raw_annotations and isinstance(raw_annotations, list):
        for ann in raw_annotations:
            if not isinstance(ann, dict):
                continue
            cls_raw = ann.get("class") or ann.get("label") or ann.get("category") or ""
            cls_id = normalize_class_name(str(cls_raw))
            if cls_id is None:
                continue

            pts = ann.get("polygon") or ann.get("points") or ann.get("vertices")
            bbox = ann.get("bbox") or ann.get("box")

            if pts and isinstance(pts, list) and len(pts) >= 3:
                flat_poly: List[float] = []
                norm_pts: List[List[float]] = []
                for pt in pts:
                    if isinstance(pt, (list, tuple)) and len(pt) >= 2:
                        px, py = float(pt[0]), float(pt[1])
                    elif isinstance(pt, dict) and "x" in pt and "y" in pt:
                        px, py = float(pt["x"]), float(pt["y"])
                    else:
                        continue
                    # Normalize if in pixel coords
                    if px > 1.0 or py > 1.0:
                        px = px / img_w
                        py = py / img_h
                    px = max(0.0, min(1.0, px))
                    py = max(0.0, min(1.0, py))
                    flat_poly.extend([round(px, 6), round(py, 6)])
                    norm_pts.append([round(px, 6), round(py, 6)])
                if len(flat_poly) >= 6:
                    segments.append((cls_id, flat_poly))
                    if cls_id == 0:  # roadway
                        norm_dir = normalize_direction(ann.get("direction"))
                        if norm_dir:
                            directional_regions.append({
                                "direction": norm_dir,
                                "polygon": norm_pts,
                                "coordinates": norm_pts,
                            })
            elif bbox and isinstance(bbox, list) and len(bbox) == 4:
                flat_poly = bbox_to_polygon_norm(
                    [float(b) for b in bbox],
                    img_w=float(img_w),
                    img_h=float(img_h),
                    is_norm=all(0.0 <= float(b) <= 1.0 for b in bbox),
                )
                segments.append((cls_id, [round(p, 6) for p in flat_poly]))

    # Synthetic geometry is opt-in only (ADR-011 D2): metadata-derived shapes
    # are fabricated ground truth and must never silently enter a dataset.
    if not segments and allow_synthetic:
        labels_meta = data.get("labels", {})
        synthesized = synthesize_polygons_from_metadata(labels_meta)
        segments.extend(synthesized)
        return resolved_img, segments, True, []

    return resolved_img, segments, False, directional_regions


def export_dataset(
    labels_dir: Path,
    output_dir: Path,
    images_dir: Optional[Path] = None,
    val_split: float = 0.2,
    seed: int = 42,
    copy_images: bool = True,
    verify: bool = False,
    allow_synthetic: bool = False,
    group_split: bool = True,
) -> Dict[str, Any]:
    """
    Exports gold-standard label JSONs into YOLO segmentation format.
    By default, labels without explicit polygon annotations are SKIPPED and
    counted in `skipped_no_annotations` (ADR-011 D2). Pass allow_synthetic=True
    (CLI: --allow-synthetic) to opt into metadata-derived geometry.

    Scans labels_dir (and any partition subdirectories like YYYY-MM-DD), but
    strictly ignores existing split directories (train, val, labels, images) to
    prevent inadvertent re-splitting of an existing dataset. Roadway sidecars are
    also skipped: they are region ground truth bound to a split, not frame labels.
    """
    repo_root = Path(__file__).resolve().parent.parent.parent
    labels_dir = labels_dir.resolve()
    output_dir = output_dir.resolve()

    if not labels_dir.is_dir():
        raise FileNotFoundError(f"Labels directory not found: {labels_dir}")

    discovered: List[Path] = []
    # If subdirectories exist (e.g. partition folders like 2026-10-08/), discover their JSONs,
    # strictly ignoring split directories (train, val, labels, images).
    subdirs = [
        p for p in labels_dir.iterdir()
        if p.is_dir() and not p.name.startswith((".", "train", "val", "labels", "images"))
    ]
    if subdirs:
        for s in sorted(subdirs):
            for p in sorted(s.glob("*.json")):
                if not is_roadway_sidecar(p):
                    discovered.append(p)
    # Also discover top-level JSONs in labels_dir
    for p in sorted(labels_dir.glob("*.json")):
        if not is_roadway_sidecar(p):
            discovered.append(p)

    discovered = sorted(list(dict.fromkeys(discovered)))
    label_files = discovered

    if not label_files:
        raise ValueError(
            f"No JSON label files found in {labels_dir} (subdirectories are not "
            "scanned; re-reading an existing train/val split would re-split it)."
        )

    # Prepare output directories
    for split in ("train", "val"):
        (output_dir / "images" / split).mkdir(parents=True, exist_ok=True)
        (output_dir / "labels" / split).mkdir(parents=True, exist_ok=True)

    # Split train/val
    split_info: Dict[str, Any] = {}
    if group_split:
        train_list, val_list, split_info = partition_files_by_group(
            label_files, val_split=val_split, seed=seed
        )
        val_files = set(val_list)
        ordered_files = train_list + val_list
        logging.info(
            "Group-aware split: %d train files (%d groups), %d val files (%d groups), 0 overlap",
            split_info["train_files"],
            split_info["train_groups"],
            split_info["val_files"],
            split_info["val_groups"],
        )
    else:
        rng = random.Random(seed)
        shuffled = list(label_files)
        rng.shuffle(shuffled)
        val_count = int(round(len(shuffled) * val_split))
        val_files = set(shuffled[:val_count])
        ordered_files = shuffled

    stats: Dict[str, Any] = {
        "total_labels": len(label_files),
        "train_samples": 0,
        "val_samples": 0,
        "images_copied": 0,
        "class_counts": {name: 0 for name in TARGET_CLASSES.values()},
        "directional_roadway_regions": 0,
        "skipped_no_annotations": 0,
        "synthesized_labels": 0,
        "allow_synthetic": allow_synthetic,
        "group_split": group_split,
        "total_groups": split_info.get("total_groups", len(label_files)),
        "train_groups": split_info.get("train_groups", len(label_files) - len(val_files)),
        "val_groups": split_info.get("val_groups", len(val_files)),
        "group_overlap": split_info.get("overlap_count", 0),
        "errors": [],
    }

    directional_by_split: Dict[str, List[Dict[str, Any]]] = {"train": [], "val": []}

    for lf in ordered_files:
        split = "val" if lf in val_files else "train"
        base_name = lf.stem

        try:
            resolved_img, segments, used_synthetic, dir_regions = parse_label_file(
                lf, repo_root=repo_root, images_dir=images_dir, allow_synthetic=allow_synthetic
            )
        except Exception as exc:
            stats["errors"].append(f"{lf.name}: parse error - {exc}")
            continue

        if not segments:
            stats["skipped_no_annotations"] += 1
            logging.debug(
                "Skipping %s: no explicit polygon annotations%s",
                lf.name,
                "" if allow_synthetic else " (pass --allow-synthetic to derive geometry from metadata)",
            )
            continue
        if used_synthetic:
            stats["synthesized_labels"] += 1

        if dir_regions:
            for reg in dir_regions:
                directional_by_split[split].append({
                    "label_id": base_name,
                    "direction": reg["direction"],
                    "polygon": reg["polygon"],
                    "coordinates": reg["coordinates"],
                })

        # Write YOLO segmentation label txt
        label_out = output_dir / "labels" / split / f"{base_name}.txt"
        with open(label_out, "w", encoding="utf-8") as f_out:
            for cls_id, poly in segments:
                cls_name = TARGET_CLASSES.get(cls_id, "unknown")
                if cls_name in stats["class_counts"]:
                    stats["class_counts"][cls_name] += 1
                poly_str = " ".join(f"{coord:.6f}" for coord in poly)
                f_out.write(f"{cls_id} {poly_str}\n")

        # Copy or symlink image if present
        if resolved_img and resolved_img.is_file() and copy_images:
            img_out = output_dir / "images" / split / f"{base_name}{resolved_img.suffix}"
            try:
                shutil.copy2(resolved_img, img_out)
                stats["images_copied"] += 1
            except Exception as exc:
                logging.warning(f"Could not copy image {resolved_img}: {exc}")

        if split == "train":
            stats["train_samples"] += 1
        else:
            stats["val_samples"] += 1

    # Write roadway_regions.json sidecars per split if directional regions present (Issue #309 / S9-T7)
    total_directional = 0
    all_directional: List[Dict[str, Any]] = []
    for split in ("train", "val"):
        regions = directional_by_split[split]
        if regions:
            total_directional += len(regions)
            split_sidecar = output_dir / "labels" / split / "roadway_regions.json"
            with open(split_sidecar, "w", encoding="utf-8") as f_side:
                json.dump(regions, f_side, indent=2)
            split_root_sidecar = output_dir / f"roadway_regions_{split}.json"
            with open(split_root_sidecar, "w", encoding="utf-8") as f_side:
                json.dump(regions, f_side, indent=2)
            for r in regions:
                all_directional.append({**r, "split": split})

    if all_directional:
        root_sidecar = output_dir / "roadway_regions.json"
        with open(root_sidecar, "w", encoding="utf-8") as f_side:
            json.dump(all_directional, f_side, indent=2)

    stats["directional_roadway_regions"] = total_directional

    # Write dataset.yaml
    dataset_yaml_path = output_dir / "dataset.yaml"
    yaml_content = [
        f"# ProjectKV Road Surface Water Segmentation Dataset",
        f"# Generated by tools/eval/export_yolo_seg.py (ADR-009 / S8-T8)",
        f"path: {output_dir.as_posix()}",
        f"train: images/train",
        f"val: images/val",
        f"names:",
    ]
    for idx, name in TARGET_CLASSES.items():
        yaml_content.append(f"  {idx}: {name}")
    yaml_content.append("")

    with open(dataset_yaml_path, "w", encoding="utf-8") as f_yaml:
        f_yaml.write("\n".join(yaml_content))

    logging.info("Export completed successfully!")
    logging.info("  Total labels processed: %d", stats["total_labels"])
    logging.info("  Train samples:          %d", stats["train_samples"])
    logging.info("  Val samples:            %d", stats["val_samples"])
    logging.info("  Images copied:          %d", stats["images_copied"])
    logging.info("  Class distributions:    %s", stats["class_counts"])
    logging.info("  Manifest written:       %s", dataset_yaml_path)
    if stats["skipped_no_annotations"] > 0:
        logging.warning(
            "Skipped %d label(s) without explicit polygon annotations; only annotated "
            "geometry may train the segmentation model (ADR-011 D2). Use --allow-synthetic "
            "explicitly if metadata-derived geometry is intentionally required.",
            stats["skipped_no_annotations"],
        )
    if stats["synthesized_labels"] > 0:
        logging.warning(
            "FABRICATED GEOMETRY: %d label(s) used metadata-derived synthetic polygons "
            "(--allow-synthetic). These shapes are NOT human ground truth.",
            stats["synthesized_labels"],
        )

    if verify:
        verification_passed, v_errors = verify_dataset(output_dir)
        stats["verification_passed"] = verification_passed
        stats["verification_errors"] = v_errors
        if not verification_passed:
            logging.error("Dataset verification failed with %d errors!", len(v_errors))
            for err in v_errors[:10]:
                logging.error("  - %s", err)
        else:
            logging.info("Verification passed 100%%! All segmentation polygons and classes valid.")

    return stats


def verify_dataset(dataset_dir: Path) -> Tuple[bool, List[str]]:
    """
    Validates that exported YOLO segmentation dataset directory is compliant:
    1. dataset.yaml exists and defines names 0..5.
    2. Labels in train and val contain valid class_ids and normalized polygon points [0.0, 1.0].
    3. Polygons contain at least 3 vertices (6 floats).
    """
    errors: List[str] = []
    yaml_path = dataset_dir / "dataset.yaml"
    if not yaml_path.is_file():
        errors.append(f"Missing dataset.yaml at {yaml_path}")

    for split in ("train", "val"):
        lbl_dir = dataset_dir / "labels" / split
        if not lbl_dir.is_dir():
            errors.append(f"Missing split directory: {lbl_dir}")
            continue

        txt_files = list(lbl_dir.glob("*.txt"))
        if not txt_files:
            errors.append(f"No label files found in {lbl_dir}")
            continue

        for tf in txt_files:
            with open(tf, "r", encoding="utf-8") as f:
                lines = [line.strip() for line in f if line.strip()]
            if not lines:
                errors.append(f"{tf.name}: empty label file")
                continue

            for idx, line in enumerate(lines):
                tokens = line.split()
                if len(tokens) < 7:  # 1 class_id + at least 3 points (6 coords)
                    errors.append(f"{tf.name}:{idx+1}: insufficient coordinates ({len(tokens)} tokens)")
                    continue
                try:
                    cls_id = int(tokens[0])
                    if cls_id not in TARGET_CLASSES:
                        errors.append(f"{tf.name}:{idx+1}: invalid class_id {cls_id}")
                except ValueError:
                    errors.append(f"{tf.name}:{idx+1}: class_id must be integer, got {tokens[0]}")
                    continue

                # Check coords
                try:
                    coords = [float(t) for t in tokens[1:]]
                    if len(coords) % 2 != 0:
                        errors.append(f"{tf.name}:{idx+1}: odd number of polygon coordinates")
                    for c in coords:
                        if not (0.0 <= c <= 1.0):
                            errors.append(f"{tf.name}:{idx+1}: coordinate out of bounds [0, 1]: {c}")
                            break
                except ValueError as ve:
                    errors.append(f"{tf.name}:{idx+1}: coordinate parsing error: {ve}")

    return len(errors) == 0, errors


def parse_args():
    parser = argparse.ArgumentParser(
        description="Export authentic camera frames and ground-truth annotations to YOLO segmentation format."
    )
    parser.add_argument(
        "--labels-dir",
        type=Path,
        default=Path("tools/eval/labels_gold_standard"),
        help="Directory containing ground-truth JSON label files (default: tools/eval/labels_gold_standard)",
    )
    parser.add_argument(
        "--output-dir",
        type=Path,
        default=Path("data/yolo_seg"),
        help="Target output directory for YOLO segmentation dataset (default: data/yolo_seg)",
    )
    parser.add_argument(
        "--images-dir",
        type=Path,
        default=Path("data/proofs"),
        help="Directory containing raw camera frames (default: data/proofs)",
    )
    parser.add_argument(
        "--val-split",
        type=float,
        default=0.2,
        help="Validation split ratio between 0.0 and 1.0 (default: 0.2)",
    )
    parser.add_argument(
        "--seed",
        type=int,
        default=42,
        help="Random seed for deterministic train/val splitting (default: 42)",
    )
    parser.add_argument(
        "--no-copy-images",
        action="store_true",
        help="Do not copy images into output directory, generate labels and manifests only",
    )
    parser.add_argument(
        "--verify",
        action="store_true",
        help="Verify exported segmentation manifests and polygon boundaries",
    )
    parser.add_argument(
        "--allow-synthetic",
        action="store_true",
        help=(
            "OPT-IN ONLY (ADR-011 D2): derive polygons from classification metadata when a label "
            "has no explicit annotations. The resulting geometry is fabricated (fixed roadway "
            "trapezoid + severity-based puddles) and is NOT human ground truth; intended solely "
            "for pipeline smoke tests, never for training datasets."
        ),
    )
    parser.add_argument(
        "--group-split",
        action="store_true",
        default=True,
        help="Group-aware train/val split grouped strictly by (camera_id, date) to prevent burst leakage (default: True)",
    )
    parser.add_argument(
        "--no-group-split",
        dest="group_split",
        action="store_false",
        help="Disable group-aware train/val split and use naive random shuffle",
    )
    return parser.parse_args()


def main():
    args = parse_args()
    try:
        stats = export_dataset(
            labels_dir=args.labels_dir,
            output_dir=args.output_dir,
            images_dir=args.images_dir,
            val_split=args.val_split,
            seed=args.seed,
            copy_images=not args.no_copy_images,
            verify=args.verify,
            allow_synthetic=args.allow_synthetic,
            group_split=args.group_split,
        )
        if args.verify and not stats.get("verification_passed", True):
            sys.exit(1)
    except Exception as exc:
        logging.error("Fatal export error: %s", exc)
        sys.exit(1)


if __name__ == "__main__":
    main()
