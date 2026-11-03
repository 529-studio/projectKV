#!/usr/bin/env python3
"""
contract.py — Segmentation polygon annotation contract for SAM pre-labeling.

Mirrors the canonical polygon contract enforced by
services/go-server/internal/archiver/handler.go (S9-T1, ADR-011 D1):
6-class taxonomy, alias canonicalization, 3-64 vertices per polygon,
normalized [0, 1] coordinates (pixel coordinates rejected), at most 32
annotations per label, and a provenance source string of at most 32 chars.

Adheres to AGENTS.md Rules:
- Rule 1: zero-emoji, 100% English code and logs.
- Rule 8: no secrets; this module holds no configuration at all.
"""

from typing import Any, Dict, List, Optional, Tuple

# Polygon annotation contract limits (mirror of internal/archiver/handler.go).
MIN_POLYGON_POINTS = 3
MAX_POLYGON_POINTS = 64
MAX_ANNOTATIONS_PER_LABEL = 32
MAX_ANNOTATION_SRC_LEN = 32

# Fixed 6-class segmentation taxonomy (ADR-009 section 4.2).
CANONICAL_CLASSES = (
    "roadway",
    "water_puddle",
    "motorcycle",
    "car",
    "bus",
    "truck",
)

# Alternate label conventions mapped onto the canonical taxonomy.
# Kept in sync with polygonClassAliases in internal/archiver/handler.go
# and CLASS_ALIASES in tools/eval/export_yolo_seg.py.
CLASS_ALIASES = {
    "road": "roadway", "asphalt": "roadway", "carriageway": "roadway",
    "water": "water_puddle", "puddle": "water_puddle", "flood": "water_puddle", "flooded": "water_puddle",
    "motorbike": "motorcycle", "bike": "motorcycle", "mc": "motorcycle",
    "automobile": "car", "passenger_car": "car", "van": "car", "suv": "car",
    "coach": "bus",
    "container": "truck", "lorry": "truck",
}

# Provenance source stamped on every polygon emitted by this toolchain.
# Matches POLYGON_PROVENANCE_SAM in apps/web/src/utils/labelerUtils.ts so the
# labeler renders the auto provenance badge for pre-labeled candidates.
SOURCE_SAM_PRELABEL = "sam_prelabel"

# COCO detection class id -> canonical segmentation class. Person (0) and
# bicycle (1) have no polygon taxonomy slot and are dropped by the pre-labeler.
COCO_TO_TAXONOMY = {
    2: "car",
    3: "motorcycle",
    5: "bus",
    7: "truck",
}


def normalize_polygon_class(raw: str) -> str:
    """Resolves a raw class string to a canonical class id ("" if unknown)."""
    norm = (raw or "").strip().lower()
    if norm in CANONICAL_CLASSES:
        return norm
    return CLASS_ALIASES.get(norm, "")


def validate_annotations(anns: List[Dict[str, Any]]) -> List[str]:
    """
    Validates a list of polygon annotations against the S9-T1 contract.

    Returns a list of human-readable error strings (empty when valid). The
    rules mirror Go validateAnnotations exactly so a label accepted here is
    accepted by the archiver SaveLabel handler.
    """
    errors: List[str] = []
    if not isinstance(anns, list):
        return ["annotations: expected a list"]
    if len(anns) > MAX_ANNOTATIONS_PER_LABEL:
        errors.append(
            "too many polygon annotations: %d (max %d)" % (len(anns), MAX_ANNOTATIONS_PER_LABEL)
        )
    for i, ann in enumerate(anns):
        if not isinstance(ann, dict):
            errors.append("annotation[%d]: expected an object" % i)
            continue
        cls = ann.get("class")
        if normalize_polygon_class(cls if isinstance(cls, str) else "") == "":
            errors.append(
                "annotation[%d]: unknown class %r (must be one of %s)"
                % (i, cls, ", ".join(CANONICAL_CLASSES))
            )
            continue
        polygon = ann.get("polygon")
        if not isinstance(polygon, list):
            errors.append("annotation[%d] (%s): polygon must be a list" % (i, cls))
            continue
        if not (MIN_POLYGON_POINTS <= len(polygon) <= MAX_POLYGON_POINTS):
            errors.append(
                "annotation[%d] (%s): polygon must have between %d and %d vertices, got %d"
                % (i, cls, MIN_POLYGON_POINTS, MAX_POLYGON_POINTS, len(polygon))
            )
            continue
        for j, pt in enumerate(polygon):
            if not isinstance(pt, (list, tuple)) or len(pt) != 2:
                errors.append(
                    "annotation[%d] (%s): vertex %d must be an [x, y] pair" % (i, cls, j)
                )
                continue
            for k, v in enumerate(pt):
                if not isinstance(v, (int, float)) or isinstance(v, bool) or v < 0.0 or v > 1.0:
                    errors.append(
                        "annotation[%d] (%s): vertex %d coordinate %d is %r, outside normalized "
                        "range [0, 1] (normalize pixel coordinates client-side before saving)"
                        % (i, cls, j, k, v)
                    )
        source = ann.get("source", "")
        if source is not None and len(source) > MAX_ANNOTATION_SRC_LEN:
            errors.append(
                "annotation[%d]: source exceeds %d characters" % (i, MAX_ANNOTATION_SRC_LEN)
            )
    return errors


def canonicalize_annotations(anns: List[Dict[str, Any]]) -> List[Dict[str, Any]]:
    """
    Canonicalizes classes and clamps normalized coordinates in place.

    Vertices are clamped to [0, 1] so an epsilon-overflow vertex from a mask
    boundary tracer does not invalidate the whole candidate; alias classes are
    resolved via normalize_polygon_class, mirroring the Go SaveLabel flow.
    """
    out: List[Dict[str, Any]] = []
    for ann in anns:
        cls = normalize_polygon_class(ann.get("class", ""))
        if not cls:
            continue
        polygon: List[Tuple[float, float]] = []
        for pt in ann.get("polygon", []):
            try:
                x = min(1.0, max(0.0, float(pt[0])))
                y = min(1.0, max(0.0, float(pt[1])))
            except (TypeError, ValueError, IndexError):
                continue
            polygon.append((x, y))
        if len(polygon) < MIN_POLYGON_POINTS:
            continue
        entry: Dict[str, Any] = {"class": cls, "polygon": [[x, y] for x, y in polygon]}
        source = ann.get("source") or SOURCE_SAM_PRELABEL
        entry["source"] = str(source)[:MAX_ANNOTATION_SRC_LEN]
        out.append(entry)
    return out


def validate_label_payload(payload: Dict[str, Any]) -> List[str]:
    """
    Validates a LabelPayload-shaped candidate label (the JSON shape consumed by
    GET /api/archiver/labels and the /labeler UI).

    Required fields mirror LabelPayload binding tags in internal/archiver:
    date, camera_id, snapshot_key. Annotations must satisfy the S9-T1
    polygon contract.
    """
    errors: List[str] = []
    for field in ("date", "camera_id", "snapshot_key"):
        value = payload.get(field)
        if not isinstance(value, str) or not value.strip():
            errors.append("%s: required non-empty string" % field)
    if not isinstance(payload.get("annotator"), str) or not payload.get("annotator"):
        errors.append("annotator: required non-empty string")
    anns = payload.get("annotations", [])
    if anns:
        errors.extend(validate_annotations(anns))
    return errors
