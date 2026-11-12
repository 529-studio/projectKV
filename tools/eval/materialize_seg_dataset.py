#!/usr/bin/env python3
"""
materialize_seg_dataset.py — Dataset v3 materializer (S12-T5 / Issue #411).

Builds a YOLO segmentation dataset whose pixels and polygons provably
correspond, from the live proof archive. This is the only sanctioned path to a
training set after the October label staging was retired as orphaned (spec §1.3,
§2.1): a label that cannot be paired with its own frame is not data, it is a
liability that poisons every metric computed from it.

Contract (executable specification: tools/eval/materialize_seg_dataset_test.py,
docs/sprints/s12_t5_spec.md §2.4, §2.5, §5.1):

  1. Frames resolve by exact ``snapshot_key`` from the local proof roots first
     ($KV_PROOF_ROOT, --proof-root, data/proofs/proofs, data/proofs,
     data/harvested) and only then from R2. A local hit never touches R2.
  2. Labels pair on the exact ``(camera_id, timestamp)`` key only. A camera-only
     match is a hard error, never a fallback: October polygons must never bind
     to November pixels.
  3. Every frame is verified (JPEG SOI, decode, CMYK, resolution, sha256) and
     duplicate stems or duplicate image digests fail the build.
  4. The build happens in ``<out>.tmp-<pid>`` on the target filesystem, is
     verified (0 orphans both ways), then swapped atomically. A non-empty
     ``--out`` requires ``--replace`` and is archived to ``<out>.bak-<stamp>``.
  5. The split is grouped by ``(camera_id, capture_date)``, frozen in
     ``split.json`` with a manifest digest, and re-running against a freeze with
     a different seed exits 2 without touching the tree.
  6. Labels must be human-reviewed: a named annotator, a ``reviewed_at``
     timestamp, and no polygon from the retired template vocabulary. There is no
     ``--allow-unreviewed`` escape hatch, and ``--allow-synthetic`` is refused
     for Dataset v3 (spec §2.11).

Usage:
  python3 tools/eval/materialize_seg_dataset.py \
      --dates 2026-10-23,2026-10-24 \
      --min-frames 550 \
      --out data/yolo_seg --val-split 0.2 --seed 42 \
      --manifest docs/reports/s12_t5_dataset_manifest.json [--dry-run]

Exit codes: 0 success, 1 build failed, 2 usage / frozen-split / missing R2 config.
"""

from __future__ import annotations

import argparse
import hashlib
import io
import json
import logging
import os
import random
import shutil
import sys
import tempfile
import time
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Callable, Dict, Iterable, List, Optional, Sequence, Tuple

try:  # Pillow is the only decoder that can prove a frame is really an image
    from PIL import Image
except ImportError:  # pragma: no cover - exercised only on a bare interpreter
    Image = None  # type: ignore[assignment]

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(message)s",
    datefmt="%H:%M:%S",
)

# ---------------------------------------------------------------------------
# Constants
# ---------------------------------------------------------------------------

TARGET_CLASSES: List[str] = [
    "roadway",
    "water_puddle",
    "motorcycle",
    "car",
    "bus",
    "truck",
]

EXIT_OK = 0
EXIT_BUILD_FAILED = 1
EXIT_USAGE = 2

MIN_FRAME_WIDTH = 320
MIN_FRAME_HEIGHT = 240
MAX_FRAME_PX = 4000
MIN_JPEG_BYTES = 512
JPEG_SOI = b"\xff\xd8\xff"

MANIFEST_VERSION = "3.0"
SPLIT_ALGORITHM = "group_shuffle_camera_date"

# Release-scale invariants (spec §2.2). They are asserted when the build claims
# the v3 release floor; a smaller --min-frames build is a smoke/staging build by
# construction and reports the same counts without pretending to be the release.
V3_RELEASE_FRAMES = 550
V3_MIN_CAMERAS = 28
V3_MIN_DATES = 8
V3_MAX_FRAMES_PER_GROUP = 3
STRATA_TARGETS = {
    "rain_wet": 200,
    "dusk_night": 150,
    "dual_carriageway": 84,
}

RAIN_WET_VALUES = {"rain", "wet", "heavy_rain", "drizzle", "showers"}
NIGHT_VALUES = {"night", "dusk", "dawn_dusk", "dusk_night", "dawn"}
PROPOSAL_ONLY_ANNOTATORS = {"sam_prelabel", "auto_harvester", "template", "curate_v2_dataset"}

# R2 configuration is read from the environment only (Rule 8: no fallback literals).
R2_REQUIRED_ENV = ("R2_ENDPOINT", "R2_BUCKET_NAME", "R2_ACCESS_KEY_ID", "R2_SECRET_ACCESS_KEY")

DEFAULT_PROOF_ROOTS = ("data/proofs/proofs", "data/proofs", "data/harvested")


# ---------------------------------------------------------------------------
# Errors
# ---------------------------------------------------------------------------


class DatasetBuildError(Exception):
    """Base class for every materializer failure."""


class MissingFrameError(DatasetBuildError):
    """A snapshot key has no bytes in any local root and none on R2."""

    def __init__(self, missing_keys: Sequence[str], message: Optional[str] = None) -> None:
        self.missing_keys = list(missing_keys)
        detail = message or (
            f"{len(self.missing_keys)} frame(s) could not be resolved by exact snapshot_key: "
            + ", ".join(self.missing_keys[:10])
            + (" ..." if len(self.missing_keys) > 10 else "")
        )
        super().__init__(detail)


class CameraOnlyPairingError(DatasetBuildError):
    """A label shares a camera with a frame but not its timestamp."""

    def __init__(self, camera_only: Sequence[Dict[str, Any]]) -> None:
        self.camera_only = list(camera_only)
        cameras = sorted({str(item.get("camera_id")) for item in self.camera_only})
        super().__init__(
            f"{len(self.camera_only)} label(s) match a frame camera but no frame timestamp "
            f"(cameras: {', '.join(cameras[:10])}); camera-only pairing is forbidden - "
            f"it would bind stale polygons to unrelated pixels"
        )


class FrameRejectedError(DatasetBuildError):
    """A frame failed JPEG / decode / colour-space / resolution validation."""

    def __init__(self, reasons: Sequence[str]) -> None:
        self.reasons = list(reasons)
        super().__init__("frame rejected: " + "; ".join(self.reasons))


class DuplicateFrameError(DatasetBuildError):
    """A duplicate stem or duplicate image digest was found."""

    def __init__(self, duplicates: Sequence[str]) -> None:
        self.duplicates = list(duplicates)
        super().__init__(
            f"{len(self.duplicates)} duplicate frame identifier(s): "
            + ", ".join(self.duplicates[:10])
        )


class FrozenSplitError(DatasetBuildError):
    """The frozen split is malformed or incompatible with this run."""


class LabelTrustError(DatasetBuildError):
    """A label is not human-reviewed or reuses retired template geometry."""


class GroupLeakageError(AssertionError, DatasetBuildError):
    """A split invariant was violated (group overlap, leaked stem/digest, no val class)."""


# Aliases used by the task contract and by callers that prefer the shorter names.
CameraOnlyMatchError = CameraOnlyPairingError
DuplicateStemError = DuplicateFrameError
ImageValidationError = FrameRejectedError
FrozenSplitConflictError = FrozenSplitError


# ---------------------------------------------------------------------------
# Primitives
# ---------------------------------------------------------------------------


def sha256_file(path: Path) -> str:
    """Return the hex SHA-256 of a file, streamed so 4 MB frames stay cheap."""
    digest = hashlib.sha256()
    with Path(path).open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def _now_iso() -> str:
    return datetime.now(timezone.utc).astimezone().isoformat(timespec="seconds")


def _key_identity(snapshot_key: str) -> Optional[Tuple[str, int]]:
    """Parse ``<date>/<camera>/<timestamp>_sample.jpg`` into ``(camera, timestamp)``.

    The key is authoritative: a label that carries no explicit timestamp is still
    bindable when its key encodes one.
    """
    text = str(snapshot_key or "").replace("\\", "/").strip()
    if not text or "/" not in text:
        return None
    parts = [part for part in text.split("/") if part]
    if len(parts) < 2:
        return None
    camera_id = parts[-2]
    stem = parts[-1].split(".")[0]
    token = stem.split("_")[0]
    if not camera_id or not token.isdigit():
        return None
    return camera_id, int(token)


def _label_identity(label: Dict[str, Any]) -> Optional[Tuple[str, int]]:
    identity = _key_identity(str(label.get("snapshot_key") or ""))
    if identity:
        return identity
    camera_id = label.get("camera_id")
    timestamp = label.get("timestamp")
    if camera_id and timestamp is not None:
        try:
            return str(camera_id), int(timestamp)
        except (TypeError, ValueError):
            return None
    return None


def match_pairs(
    frame_keys: Sequence[str], labels: Sequence[Dict[str, Any]]
) -> Dict[str, Any]:
    """Pair frames to labels on the exact ``(camera_id, timestamp)`` key only.

    Returns ``{"matches", "unmatched_frames", "camera_only"}``. Labels whose
    camera appears among the frames but whose timestamp does not are reported as
    ``camera_only``: they are never converted into matches.
    """
    label_index: Dict[Tuple[str, int], Dict[str, Any]] = {}
    ordered_labels: List[Tuple[Optional[Tuple[str, int]], Dict[str, Any]]] = []
    for label in labels:
        identity = _label_identity(label)
        ordered_labels.append((identity, label))
        if identity is not None and identity not in label_index:
            label_index[identity] = label

    frame_cameras: set = set()
    matches: List[Dict[str, Any]] = []
    unmatched_frames: List[str] = []
    matched_identities: set = set()

    for key in frame_keys:
        identity = _key_identity(str(key))
        if identity is not None:
            frame_cameras.add(identity[0])
        label = label_index.get(identity) if identity is not None else None
        if label is None:
            unmatched_frames.append(str(key))
            continue
        matched_identities.add(identity)
        matches.append(
            {
                "key": str(key),
                "camera_id": identity[0],
                "timestamp": identity[1],
                "label": label,
            }
        )

    camera_only = [
        label
        for identity, label in ordered_labels
        if identity is not None
        and identity not in matched_identities
        and identity[0] in frame_cameras
    ]

    return {
        "matches": matches,
        "unmatched_frames": unmatched_frames,
        "camera_only": camera_only,
    }


def assert_pairs_buildable(result: Dict[str, Any]) -> None:
    """Refuse camera-only fallbacks first, then fatal exact-key misses."""
    camera_only = result.get("camera_only") or []
    if camera_only:
        raise CameraOnlyPairingError(camera_only)
    unmatched = result.get("unmatched_frames") or []
    if unmatched:
        raise MissingFrameError(unmatched)


_R2_CACHE_DIR: Optional[Path] = None


def _r2_cache_dir() -> Path:
    global _R2_CACHE_DIR
    if _R2_CACHE_DIR is None or not _R2_CACHE_DIR.is_dir():
        _R2_CACHE_DIR = Path(tempfile.mkdtemp(prefix="kv_proof_r2_"))
    return _R2_CACHE_DIR


def resolve_frame(
    snapshot_key: str,
    roots: Sequence[Path],
    r2_fetch: Optional[Callable[[str], bytes]] = None,
) -> Dict[str, str]:
    """Resolve one frame by exact key, local roots first and R2 last.

    ``r2_fetch`` is only consulted after every local root missed, so a local
    proof tree always wins over a network round trip.
    """
    for root in roots or ():
        candidate = Path(root) / str(snapshot_key)
        if candidate.is_file():
            return {"path": str(candidate), "source": "local"}

    if r2_fetch is not None:
        try:
            payload = r2_fetch(str(snapshot_key))
        except (KeyError, FileNotFoundError):
            payload = None
        if payload:
            destination = _r2_cache_dir() / str(snapshot_key)
            destination.parent.mkdir(parents=True, exist_ok=True)
            destination.write_bytes(payload)
            return {"path": str(destination), "source": "r2"}

    raise MissingFrameError([str(snapshot_key)])


def validate_frame_image(path: Path) -> Dict[str, Any]:
    """Verify a frame is a real, decodable JPEG of usable size.

    Rejection reasons are returned as a list so a build can report every broken
    frame it found instead of dying on the first.
    """
    path = Path(path)
    if Image is None:
        raise RuntimeError("Pillow is required to validate frames (pip install Pillow)")

    try:
        payload = path.read_bytes()
    except OSError as exc:
        raise FrameRejectedError([f"unreadable frame {path}: {exc}"]) from exc

    if not payload:
        raise FrameRejectedError([f"empty file: {path.name} is 0 bytes"])
    if not payload.startswith(JPEG_SOI):
        raise FrameRejectedError(
            [
                f"not a JPEG: {path.name} does not start with the ff d8 ff (JPEG SOI) marker "
                f"(got {payload[:3].hex()})"
            ]
        )
    if len(payload) < MIN_JPEG_BYTES:
        raise FrameRejectedError(
            [
                f"truncated JPEG payload: {path.name} is {len(payload)} bytes "
                f"(< {MIN_JPEG_BYTES}), too small to decode"
            ]
        )

    try:
        with Image.open(io.BytesIO(payload)) as image:
            image.load()
            width, height = image.size
            mode = image.mode
    except Exception as exc:  # noqa: BLE001 - any decoder failure is a rejection
        raise FrameRejectedError(
            [
                f"decode failed for {path.name}: truncated or corrupt JPEG "
                f"({type(exc).__name__}: {exc})"
            ]
        ) from exc

    if mode == "CMYK":
        raise FrameRejectedError(
            [f"cmyk colour space: {path.name} is {mode}, only RGB/L greyscale is accepted"]
        )
    if (
        width < MIN_FRAME_WIDTH
        or height < MIN_FRAME_HEIGHT
        or max(width, height) > MAX_FRAME_PX
    ):
        raise FrameRejectedError(
            [
                f"resolution {width}x{height} outside the accepted box "
                f"({MIN_FRAME_WIDTH}x{MIN_FRAME_HEIGHT} minimum, {MAX_FRAME_PX} px maximum side)"
            ]
        )

    return {
        "sha256": hashlib.sha256(payload).hexdigest(),
        "width": int(width),
        "height": int(height),
        "bytes": len(payload),
    }


# Name used by the task contract; kept as an alias so both spellings work.
validate_image = validate_frame_image


def assert_unique_frames(entries: Sequence[Dict[str, Any]]) -> None:
    """Reject duplicate stems and duplicate image digests.

    A digest collision is only reported separately when it spans two *different*
    stems: when the same stem was listed twice, the stem is the actionable
    identifier and counting the digest again would double-report one defect.
    """
    stem_counts: Dict[str, int] = {}
    digest_stems: Dict[str, set] = {}

    for entry in entries:
        stem = str(entry.get("stem") or "")
        if stem:
            stem_counts[stem] = stem_counts.get(stem, 0) + 1
        digest = str(entry.get("sha256") or "")
        if digest:
            digest_stems.setdefault(digest, set()).add(stem)

    duplicates: List[str] = [stem for stem, count in stem_counts.items() if count > 1]
    for digest, stems in digest_stems.items():
        if len(stems) > 1:
            duplicates.append(digest)

    if duplicates:
        raise DuplicateFrameError(duplicates)


# ---------------------------------------------------------------------------
# Label trust (spec §2.3 / AC5)
# ---------------------------------------------------------------------------

_TEMPLATE_VERTEX_SETS: Optional[frozenset] = None


def _vertex_set(polygon: Iterable[Sequence[float]]) -> frozenset:
    return frozenset((round(float(point[0]), 4), round(float(point[1]), 4)) for point in polygon)


def template_vertex_sets() -> frozenset:
    """Return the vertex sets of every polygon the retired generator emitted.

    ``tools/eval/curate_v2_dataset.py`` fabricated 261 frames by stamping fixed,
    hash-jittered rectangles and calling them ``sam_prelabel``. The shapes are
    reproduced here arithmetically on purpose: the materializer must not import
    the generator (spec §2.11), but it must still recognise its output.
    """
    global _TEMPLATE_VERTEX_SETS
    if _TEMPLATE_VERTEX_SETS is not None:
        return _TEMPLATE_VERTEX_SETS

    shapes: List[List[Tuple[float, float]]] = [
        # Vehicles (constant rectangles)
        [(0.35, 0.80), (0.42, 0.80), (0.42, 0.92), (0.35, 0.92)],
        [(0.55, 0.65), (0.75, 0.65), (0.75, 0.82), (0.55, 0.82)],
        [(0.25, 0.72), (0.31, 0.72), (0.31, 0.83), (0.25, 0.83)],
        [(0.45, 0.52), (0.62, 0.52), (0.62, 0.70), (0.45, 0.70)],
        [(0.68, 0.55), (0.85, 0.55), (0.85, 0.75), (0.68, 0.75)],
        # Standing water body
        [(0.22, 0.92), (0.78, 0.92), (0.68, 0.68), (0.32, 0.68)],
    ]
    for offset in (-0.05, 0.0, 0.05):
        shapes.append(
            [
                (round(0.20 + offset, 4), 0.90),
                (round(0.45 + offset, 4), 0.90),
                (round(0.40 + offset, 4), 0.74),
                (round(0.25 + offset, 4), 0.74),
            ]
        )
        shapes.append(
            [
                (round(0.60 + offset, 4), 0.86),
                (round(0.82 + offset, 4), 0.86),
                (round(0.76 + offset, 4), 0.72),
                (round(0.64 + offset, 4), 0.72),
            ]
        )

    # Roadway templates: same clamps and jitter grid as the generator.
    for dx_step in range(-3, 4):
        for dy_step in range(-2, 3):
            dx = dx_step * 0.01
            dy = dy_step * 0.01
            bottom = round(min(0.98, 0.95 + dy), 4)
            top = round(max(0.10, 0.42 + dy), 4)
            shapes.append(
                [
                    (round(max(0.02, 0.05 + dx), 4), bottom),
                    (round(max(0.02, 0.48 + dx), 4), bottom),
                    (round(max(0.02, 0.44 + dx), 4), top),
                    (round(max(0.02, 0.12 + dx), 4), top),
                ]
            )
            shapes.append(
                [
                    (round(max(0.02, 0.52 + dx), 4), bottom),
                    (round(min(0.98, 0.95 + dx), 4), bottom),
                    (round(min(0.98, 0.86 + dx), 4), top),
                    (round(max(0.02, 0.54 + dx), 4), top),
                ]
            )
    for dx_step in range(-2, 3):
        for dy_step in range(-2, 3):
            dx = dx_step * 0.01
            dy = dy_step * 0.01
            bottom = round(min(0.98, 0.95 + dy), 4)
            top = round(max(0.10, 0.42 + dy), 4)
            shapes.append(
                [
                    (round(max(0.02, 0.08 + dx), 4), bottom),
                    (round(min(0.98, 0.92 + dx), 4), bottom),
                    (round(min(0.98, 0.66 + dx), 4), top),
                    (round(max(0.02, 0.34 + dx), 4), top),
                ]
            )

    _TEMPLATE_VERTEX_SETS = frozenset(_vertex_set(shape) for shape in shapes)
    return _TEMPLATE_VERTEX_SETS


def assert_label_trusted(label: Dict[str, Any]) -> None:
    """A label is trusted only if a named human reviewed real geometry."""
    snapshot_key = str(label.get("snapshot_key") or label.get("stem") or "<unknown>")
    annotator = str(label.get("annotator") or "").strip()
    if not annotator:
        raise LabelTrustError(f"label {snapshot_key} has no annotator; human review required")
    if annotator.lower() in PROPOSAL_ONLY_ANNOTATORS:
        raise LabelTrustError(
            f"label {snapshot_key} is proposal-only (annotator={annotator}); "
            f"SAM proposals are never ground truth (spec §2.3)"
        )
    if not label.get("reviewed_at"):
        raise LabelTrustError(
            f"label {snapshot_key} has no reviewed_at timestamp; "
            f"an unreviewed polygon cannot enter Dataset v3"
        )

    vocabulary = template_vertex_sets()
    for annotation in label.get("annotations") or []:
        polygon = annotation.get("polygon") or []
        if len(polygon) < 3:
            raise LabelTrustError(
                f"label {snapshot_key} class '{annotation.get('class')}' has "
                f"{len(polygon)} vertices; a polygon needs at least 3"
            )
        if _vertex_set(polygon) in vocabulary:
            raise LabelTrustError(
                f"label {snapshot_key} class '{annotation.get('class')}' uses retired template "
                f"geometry; re-annotate the frame instead of reusing the fabricated polygon"
            )


# ---------------------------------------------------------------------------
# Group split (Lesson 75 / spec §2.5)
# ---------------------------------------------------------------------------


def _entry_group(entry: Dict[str, Any]) -> str:
    group = entry.get("group")
    if group:
        return str(group)
    return f"{entry.get('camera_id')}|{entry.get('capture_date')}"


def _entry_classes(entry: Dict[str, Any]) -> List[str]:
    classes = entry.get("classes")
    if isinstance(classes, (list, tuple, set)):
        return [str(name) for name in classes]
    return []


def build_group_split(
    entries: Sequence[Dict[str, Any]], val_ratio: float = 0.2, seed: int = 42
) -> Dict[str, Any]:
    """80/20 group split where a group is ``(camera_id, capture_date)``."""
    group_of: Dict[str, str] = {}
    group_order: List[str] = []
    for entry in entries:
        stem = str(entry["stem"])
        group = _entry_group(entry)
        group_of[stem] = group
        if group not in group_order:
            group_order.append(group)

    groups = sorted(group_order)
    val_group_count = max(1, int(round(len(groups) * float(val_ratio))))
    val_group_count = min(val_group_count, max(1, len(groups) - 1)) if len(groups) > 1 else 1

    shuffled = list(groups)
    random.Random(int(seed)).shuffle(shuffled)
    val_groups = sorted(shuffled[:val_group_count])
    train_groups = sorted(shuffled[val_group_count:])

    val_set = set(val_groups)
    train: List[str] = []
    val: List[str] = []
    for stem in sorted(group_of):
        (val if group_of[stem] in val_set else train).append(stem)

    return {
        "seed": int(seed),
        "algorithm": SPLIT_ALGORITHM,
        "groups": {
            "train": len(train_groups),
            "val": len(val_groups),
            "overlap": len(set(train_groups) & set(val_groups)),
        },
        "train": train,
        "val": val,
        "train_groups": train_groups,
        "val_groups": val_groups,
        "group_of": group_of,
    }


def assert_split_invariants(
    split: Dict[str, Any], entries: Sequence[Dict[str, Any]]
) -> None:
    """Enforce, in order: group overlap, stem leak, digest leak, totals, classes."""
    entries_by_stem = {str(entry["stem"]): entry for entry in entries}
    group_of = dict(split.get("group_of") or {})
    for entry in entries:
        group_of.setdefault(str(entry["stem"]), _entry_group(entry))

    train_groups = set(split.get("train_groups") or [])
    val_groups = set(split.get("val_groups") or [])
    overlap = sorted(train_groups & val_groups)
    if overlap:
        group = overlap[0]
        raise GroupLeakageError(
            f"group '{group}' is present in both train_groups and val_groups; "
            f"every frame of a camera/date group must live in exactly one split"
        )

    train_stems = set(split.get("train") or [])
    val_stems = set(split.get("val") or [])

    leaked_stems = sorted(train_stems & val_stems)
    if leaked_stems:
        stem = leaked_stems[0]
        raise GroupLeakageError(
            f"val stem '{stem}' (group '{group_of.get(stem, '<unknown>')}') also appears in train; "
            f"a validation frame must never be trained on"
        )

    val_digests = {
        str(entries_by_stem[stem].get("sha256")): stem
        for stem in val_stems
        if stem in entries_by_stem and entries_by_stem[stem].get("sha256")
    }
    for stem in sorted(train_stems):
        entry = entries_by_stem.get(stem)
        if entry is None:
            continue
        digest = str(entry.get("sha256") or "")
        if digest and digest in val_digests:
            raise GroupLeakageError(
                f"image sha256 {digest} of train stem '{stem}' also belongs to val stem "
                f"'{val_digests[digest]}'; duplicate pixels across splits are leakage"
            )

    total = len(train_stems) + len(val_stems)
    if total != len(entries):
        raise GroupLeakageError(
            f"train ({len(train_stems)}) + val ({len(val_stems)}) = {total} frames, "
            f"but {len(entries)} frames were materialized"
        )

    val_classes = set()
    for stem in val_stems:
        entry = entries_by_stem.get(stem)
        if entry is not None:
            val_classes.update(_entry_classes(entry))
    missing = [name for name in TARGET_CLASSES if name not in val_classes]
    if missing:
        raise GroupLeakageError(
            f"class '{missing[0]}' has no val instance "
            f"(missing classes: {', '.join(missing)}); every class needs at least one "
            f"validation frame before the split can be frozen"
        )


def _canonical_split_payload(split: Dict[str, Any]) -> Dict[str, Any]:
    return {
        "seed": int(split.get("seed", 0)),
        "algorithm": str(split.get("algorithm", SPLIT_ALGORITHM)),
        "groups": dict(split.get("groups") or {}),
        "train": sorted(str(stem) for stem in (split.get("train") or [])),
        "val": sorted(str(stem) for stem in (split.get("val") or [])),
        "train_groups": sorted(str(group) for group in (split.get("train_groups") or [])),
        "val_groups": sorted(str(group) for group in (split.get("val_groups") or [])),
        "group_of": {str(k): str(v) for k, v in (split.get("group_of") or {}).items()},
    }


def split_sha256(split: Dict[str, Any]) -> str:
    """Canonical digest of the split assignment, independent of key order."""
    payload = _canonical_split_payload(split)
    encoded = json.dumps(payload, sort_keys=True, separators=(",", ":")).encode("utf-8")
    return hashlib.sha256(encoded).hexdigest()


def freeze_split(split: Dict[str, Any], manifest_sha256: str) -> Dict[str, Any]:
    """Bind the split assignment to a dataset digest; persisting it is the freeze."""
    frozen = _canonical_split_payload(split)
    frozen["version"] = MANIFEST_VERSION
    frozen["split_sha256"] = split_sha256(split)
    frozen["manifest_sha256"] = str(manifest_sha256)
    return frozen


def load_frozen_split(path: Path) -> Dict[str, Any]:
    """Load ``split.json`` and refuse anything that is not a complete freeze."""
    path = Path(path)
    try:
        payload = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise FrozenSplitError(f"cannot read frozen split {path}: {exc}") from exc
    if not isinstance(payload, dict):
        raise FrozenSplitError(f"frozen split {path} is not a JSON object")

    required = (
        "seed",
        "algorithm",
        "train",
        "val",
        "train_groups",
        "val_groups",
        "manifest_sha256",
    )
    missing = [key for key in required if key not in payload]
    if missing:
        raise FrozenSplitError(
            f"frozen split {path} is malformed: missing {', '.join(missing)}"
        )
    for key in ("train", "val", "train_groups", "val_groups"):
        if not isinstance(payload[key], list):
            raise FrozenSplitError(f"frozen split {path} field '{key}' must be a list")
    return payload


def assert_frozen_split_compatible(
    frozen: Dict[str, Any],
    seed: Optional[int] = None,
    manifest_sha256: Optional[str] = None,
) -> None:
    """Refuse a re-split with a different seed, or a drifted frame set."""
    if seed is not None and int(frozen.get("seed", -1)) != int(seed):
        raise FrozenSplitError(
            f"frozen split was created with seed {frozen.get('seed')} but this run requests "
            f"seed {seed}; refusing to re-split a frozen frame set. Delete split.json "
            f"deliberately to re-split."
        )
    bound = frozen.get("manifest_sha256")
    if manifest_sha256 is not None and bound and str(bound) != str(manifest_sha256):
        raise FrozenSplitError(
            f"frozen split manifest_sha256 {bound} does not match the freshly built split "
            f"{manifest_sha256}: the frame set drifted from the freeze"
        )


# ---------------------------------------------------------------------------
# Atomic materialization
# ---------------------------------------------------------------------------


def _directory_is_empty(path: Path) -> bool:
    try:
        return not any(Path(path).iterdir())
    except OSError:
        return False


def plan_replacement(out_dir: Path, replace: bool = False) -> Dict[str, Any]:
    """Decide whether ``out_dir`` is created, replaced or refused."""
    out_dir = Path(out_dir)
    if not out_dir.exists():
        return {"action": "create", "backup": None}
    if out_dir.is_file():
        return {"action": "refuse", "backup": None}
    if _directory_is_empty(out_dir):
        return {"action": "create", "backup": None}
    if replace:
        return {"action": "replace", "backup": None}
    return {"action": "refuse", "backup": None}


def swap_build_dir(
    tmp_dir: Path,
    out_dir: Path,
    replace: bool = False,
    timestamp: Optional[str] = None,
) -> Dict[str, Any]:
    """Archive a non-empty ``out_dir`` then atomically move the build into place."""
    tmp_dir, out_dir = Path(tmp_dir), Path(out_dir)
    plan = plan_replacement(out_dir, replace=replace)
    if plan["action"] == "refuse":
        raise DatasetBuildError(
            f"{out_dir} is not empty; pass --replace to archive it to "
            f"{out_dir.name}.bak-<timestamp> first (never merge new labels into stale ones)"
        )

    out_dir.parent.mkdir(parents=True, exist_ok=True)
    backup: Optional[Path] = None
    if plan["action"] == "replace":
        stamp = timestamp or time.strftime("%Y%m%dT%H%M%S")
        backup = out_dir.parent / f"{out_dir.name}.bak-{stamp}"
        if backup.exists():
            raise DatasetBuildError(
                f"backup target {backup} already exists; remove it or pass an explicit timestamp"
            )
        out_dir.rename(backup)
    elif out_dir.exists() and _directory_is_empty(out_dir):
        out_dir.rmdir()

    os.replace(tmp_dir, out_dir)
    return {
        "backup": str(backup) if backup is not None else None,
        "build_dir": str(out_dir),
    }


def assert_no_orphans(images_dir: Path, labels_dir: Path) -> Dict[str, int]:
    """Count images without labels and labels without images."""
    images = {path.stem for path in Path(images_dir).glob("*.jpg")}
    labels = {path.stem for path in Path(labels_dir).glob("*.txt")}
    return {
        "images": len(images - labels),
        "labels": len(labels - images),
    }


def count_strata(entries: Sequence[Dict[str, Any]]) -> Dict[str, int]:
    """Count the three stratification targets recorded in the manifest."""
    rain_wet = 0
    dusk_night = 0
    dual_carriageway = 0
    for entry in entries:
        weather = str(entry.get("weather") or "").lower()
        flood = str(entry.get("flood_level") or "").lower()
        if weather in RAIN_WET_VALUES or flood in {"moderate", "deep", "heavy"}:
            rain_wet += 1
        if str(entry.get("time_of_day") or "").lower() in NIGHT_VALUES:
            dusk_night += 1
        if entry.get("dual_carriageway"):
            dual_carriageway += 1
    return {
        "rain_wet": rain_wet,
        "dusk_night": dusk_night,
        "dual_carriageway": dual_carriageway,
    }


def assert_release_scale_invariants(entries: Sequence[Dict[str, Any]]) -> None:
    """Assert the v3 strata, camera/date breadth and burst cap (spec §2.2)."""
    strata = count_strata(entries)
    shortfalls = [
        f"{name} {strata[name]} < {target}" for name, target in STRATA_TARGETS.items()
        if strata[name] < target
    ]
    cameras = {str(entry.get("camera_id")) for entry in entries}
    dates = {str(entry.get("capture_date")) for entry in entries}
    if len(cameras) < V3_MIN_CAMERAS:
        shortfalls.append(f"cameras {len(cameras)} < {V3_MIN_CAMERAS}")
    if len(dates) < V3_MIN_DATES:
        shortfalls.append(f"dates {len(dates)} < {V3_MIN_DATES}")

    per_group: Dict[str, int] = {}
    for entry in entries:
        group = _entry_group(entry)
        per_group[group] = per_group.get(group, 0) + 1
    over_burst = {group: count for group, count in per_group.items() if count > V3_MAX_FRAMES_PER_GROUP}
    if over_burst:
        group, count = sorted(over_burst.items())[0]
        shortfalls.append(
            f"burst control: group '{group}' has {count} frames > {V3_MAX_FRAMES_PER_GROUP}"
        )

    if shortfalls:
        raise DatasetBuildError(
            "Dataset v3 stratification shortfall: " + "; ".join(shortfalls)
        )


def build_manifest(
    entries: Sequence[Dict[str, Any]],
    split: Dict[str, Any],
    generated_at: str,
    seed: int = 42,
    source: str = "r2_live_archive",
) -> Dict[str, Any]:
    """Build the published manifest document (spec §5.1)."""
    split_of: Dict[str, str] = {}
    for stem in split.get("train") or []:
        split_of[str(stem)] = "train"
    for stem in split.get("val") or []:
        split_of[str(stem)] = "val"

    records: List[Dict[str, Any]] = []
    for entry in sorted(entries, key=lambda item: str(item["stem"])):
        stem = str(entry["stem"])
        if stem not in split_of:
            raise DatasetBuildError(
                f"stem '{stem}' has no split assignment; refusing to publish a manifest that "
                f"guesses one"
            )
        records.append(
            {
                "stem": stem,
                "key": str(entry.get("key") or ""),
                "sha256": str(entry.get("sha256") or ""),
                "width": int(entry.get("width") or 0),
                "height": int(entry.get("height") or 0),
                "split": split_of[stem],
                "group": str(entry.get("group") or _entry_group(entry)),
                "annotator": str(entry.get("annotator") or ""),
                "reviewed_at": str(entry.get("reviewed_at") or ""),
                "source": str(entry.get("source") or "local"),
                "classes": _entry_classes(entry),
                "bytes": int(entry.get("bytes") or 0),
                "weather": str(entry.get("weather") or ""),
                "time_of_day": str(entry.get("time_of_day") or ""),
                "dual_carriageway": bool(entry.get("dual_carriageway")),
            }
        )

    cameras = {str(entry.get("camera_id")) for entry in entries}
    dates = {str(entry.get("capture_date")) for entry in entries}
    return {
        "version": MANIFEST_VERSION,
        "generated_at": str(generated_at),
        "source": str(source),
        "total_frames": len(entries),
        "train": len(split.get("train") or []),
        "val": len(split.get("val") or []),
        "seed": int(seed),
        "split_sha256": split_sha256(split),
        "groups": dict(split.get("groups") or {}),
        "strata": count_strata(entries),
        "cameras": len(cameras),
        "dates": len(dates),
        "labels": records,
    }


def manifest_sha256(manifest: Dict[str, Any]) -> str:
    """Canonical dataset digest: order independent and independent of time."""
    payload = {
        key: value for key, value in manifest.items() if key != "generated_at"
    }
    payload["labels"] = sorted(
        (dict(record) for record in payload.get("labels") or []),
        key=lambda record: str(record.get("stem")),
    )
    payload["classes"] = list(TARGET_CLASSES)
    encoded = json.dumps(payload, sort_keys=True, separators=(",", ":")).encode("utf-8")
    return hashlib.sha256(encoded).hexdigest()


def _yolo_label_line(annotation: Dict[str, Any]) -> str:
    class_name = str(annotation.get("class") or "")
    if class_name not in TARGET_CLASSES:
        raise DatasetBuildError(
            f"annotation class '{class_name}' is not one of {TARGET_CLASSES}"
        )
    polygon = annotation.get("polygon") or []
    if len(polygon) < 3:
        raise DatasetBuildError(
            f"class '{class_name}' polygon has {len(polygon)} vertices, need >= 3"
        )
    coordinates: List[str] = []
    for point in polygon:
        try:
            x, y = float(point[0]), float(point[1])
        except (TypeError, ValueError, IndexError) as exc:
            raise DatasetBuildError(f"class '{class_name}' has a malformed vertex {point!r}") from exc
        if not (-0.001 <= x <= 1.001 and -0.001 <= y <= 1.001):
            raise DatasetBuildError(
                f"class '{class_name}' vertex ({x}, {y}) is outside normalized [0, 1]"
            )
        coordinates.append(f"{min(max(x, 0.0), 1.0):.6f} {min(max(y, 0.0), 1.0):.6f}")
    return f"{TARGET_CLASSES.index(class_name)} " + " ".join(coordinates)


def materialize_dataset(
    entries: Sequence[Dict[str, Any]],
    split: Dict[str, Any],
    manifest: Dict[str, Any],
    out_dir: Path,
    replace: bool = False,
    dry_run: bool = False,
) -> Dict[str, Any]:
    """Write the dataset tree into ``<out>.tmp-<pid>``, verify it, then swap."""
    out_dir = Path(out_dir)
    split_of: Dict[str, str] = {}
    for stem in split.get("train") or []:
        split_of[str(stem)] = "train"
    for stem in split.get("val") or []:
        split_of[str(stem)] = "val"

    if dry_run:
        logging.info(
            "[DRY RUN] Would materialize %d frame(s) into %s (train=%d, val=%d); "
            "manifest digest %s; nothing written.",
            len(entries),
            out_dir,
            len(split.get("train") or []),
            len(split.get("val") or []),
            manifest_sha256(manifest)[:16],
        )
        return {"materialized": False, "out_dir": str(out_dir), "frames": len(entries)}

    tmp_dir = out_dir.parent / f"{out_dir.name}.tmp-{os.getpid()}"
    if tmp_dir.exists():
        shutil.rmtree(tmp_dir)

    try:
        for sub in ("images/train", "images/val", "labels/train", "labels/val"):
            (tmp_dir / sub).mkdir(parents=True, exist_ok=True)

        for entry in entries:
            stem = str(entry["stem"])
            destination_split = split_of.get(stem)
            if destination_split not in {"train", "val"}:
                raise DatasetBuildError(
                    f"stem '{stem}' is not assigned to either split; the split is incomplete"
                )
            source = Path(str(entry.get("path") or ""))
            if not source.is_file():
                raise DatasetBuildError(f"source frame vanished before copy: {source}")
            image_path = tmp_dir / "images" / destination_split / f"{stem}.jpg"
            shutil.copyfile(source, image_path)

            lines = [_yolo_label_line(item) for item in entry.get("annotations") or []]
            label_path = tmp_dir / "labels" / destination_split / f"{stem}.txt"
            label_path.write_text(
                "\n".join(lines) + ("\n" if lines else ""), encoding="utf-8"
            )

        dataset_yaml = tmp_dir / "dataset.yaml"
        names = "\n".join(f"  {index}: {name}" for index, name in enumerate(TARGET_CLASSES))
        dataset_yaml.write_text(
            f"path: {out_dir.resolve().as_posix()}\n"
            f"train: images/train\n"
            f"val: images/val\n"
            f"names:\n{names}\n",
            encoding="utf-8",
        )

        for destination_split in ("train", "val"):
            orphans = assert_no_orphans(
                tmp_dir / "images" / destination_split,
                tmp_dir / "labels" / destination_split,
            )
            if orphans["images"] or orphans["labels"]:
                raise DatasetBuildError(
                    f"{destination_split} split has {orphans['images']} image(s) without a label "
                    f"and {orphans['labels']} label(s) without an image"
                )

        expected_train = len(split.get("train") or [])
        expected_val = len(split.get("val") or [])
        written_train = len(list((tmp_dir / "images" / "train").glob("*.jpg")))
        written_val = len(list((tmp_dir / "images" / "val").glob("*.jpg")))
        if (written_train, written_val) != (expected_train, expected_val):
            raise DatasetBuildError(
                f"split accounting mismatch: wrote train={written_train}/val={written_val}, "
                f"expected train={expected_train}/val={expected_val}"
            )

        # The freeze (split.json) and the manifest live inside the build so a swap can
        # never leave a dataset without the documents that describe it.
        (tmp_dir / "split.json").write_text(
            json.dumps(freeze_split(split, manifest_sha256(manifest)), indent=2) + "\n",
            encoding="utf-8",
        )
        (tmp_dir / "manifest.json").write_text(
            json.dumps(manifest, indent=2) + "\n", encoding="utf-8"
        )

        swap = swap_build_dir(tmp_dir, out_dir, replace=replace)
    except Exception:
        shutil.rmtree(tmp_dir, ignore_errors=True)
        raise

    return {
        "materialized": True,
        "out_dir": out_dir.as_posix(),
        "frames": len(entries),
        "backup": swap["backup"],
    }


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------


def _split_csv(value: str) -> List[str]:
    return [item.strip() for item in str(value or "").split(",") if item.strip()]


def _label_date(label: Dict[str, Any]) -> str:
    explicit = label.get("capture_date") or label.get("date")
    if explicit:
        return str(explicit)
    snapshot_key = str(label.get("snapshot_key") or "")
    head = snapshot_key.split("/")[0]
    return head if head else ""


def load_labels(labels_dir: Path, dates: Sequence[str] = ()) -> List[Dict[str, Any]]:
    """Load reviewed label JSON files, filtered to the requested capture dates."""
    labels_dir = Path(labels_dir)
    if not labels_dir.is_dir():
        raise DatasetBuildError(f"label directory not found: {labels_dir}")

    wanted = {str(date) for date in dates}
    labels: List[Dict[str, Any]] = []
    for path in sorted(labels_dir.rglob("*.json")):
        try:
            payload = json.loads(path.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError) as exc:
            raise DatasetBuildError(f"label {path} is not valid JSON: {exc}") from exc
        if not isinstance(payload, dict):
            raise DatasetBuildError(f"label {path} is not a JSON object")
        payload.setdefault("snapshot_key", payload.get("key"))
        payload.setdefault("_label_path", str(path))
        if wanted and _label_date(payload) not in wanted:
            continue
        labels.append(payload)

    labels.sort(
        key=lambda item: (
            _label_date(item),
            str(item.get("camera_id") or ""),
            int(item.get("timestamp") or 0),
            str(item.get("_label_path") or ""),
        )
    )
    return labels


def _proof_roots(cli_roots: Sequence[Path]) -> List[Path]:
    roots: List[Path] = [Path(root) for root in cli_roots]
    env_root = os.environ.get("KV_PROOF_ROOT", "")
    if env_root:
        roots.append(Path(env_root))
    roots.extend(Path(default) for default in DEFAULT_PROOF_ROOTS)

    deduped: List[Path] = []
    seen: set = set()
    for root in roots:
        key = str(root)
        if key not in seen:
            seen.add(key)
            deduped.append(root)
    return deduped


def _r2_fetcher() -> Tuple[Optional[Callable[[str], bytes]], List[str]]:
    """Return an R2 fetch callable, or the missing environment variables."""
    missing = [name for name in R2_REQUIRED_ENV if not os.environ.get(name)]
    if missing:
        return None, missing

    try:
        import boto3
        from botocore.config import Config
    except ImportError:
        return None, ["boto3 (pip install boto3)"]

    client = boto3.client(
        service_name="s3",
        endpoint_url=os.environ["R2_ENDPOINT"],
        aws_access_key_id=os.environ["R2_ACCESS_KEY_ID"],
        aws_secret_access_key=os.environ["R2_SECRET_ACCESS_KEY"],
        region_name="auto",
        config=Config(s3={"addressing_style": "path"}),
    )
    bucket = os.environ["R2_BUCKET_NAME"]

    def fetch(key: str) -> bytes:
        for candidate in (key, f"proofs/{key}"):
            try:
                response = client.get_object(Bucket=bucket, Key=candidate)
            except client.exceptions.NoSuchKey:
                continue
            except Exception as exc:  # noqa: BLE001 - surface auth/network failures
                if "NoSuchKey" in type(exc).__name__ or "404" in str(exc):
                    continue
                raise
            return response["Body"].read()
        raise KeyError(key)

    return fetch, []


def _stage_entries(
    matches: Sequence[Dict[str, Any]],
    roots: Sequence[Path],
    r2_fetch: Optional[Callable[[str], bytes]],
) -> Tuple[List[Dict[str, Any]], List[str]]:
    """Resolve and validate every matched frame; return entries and missing keys."""
    entries: List[Dict[str, Any]] = []
    missing: List[str] = []
    rejected: List[str] = []

    for match in matches:
        key = str(match["key"])
        label = match["label"]
        try:
            resolved = resolve_frame(key, roots, r2_fetch)
        except MissingFrameError:
            missing.append(key)
            continue
        try:
            record = validate_frame_image(Path(resolved["path"]))
        except FrameRejectedError as exc:
            rejected.append(f"{key}: {'; '.join(exc.reasons)}")
            continue

        entries.append(
            {
                "stem": f"{match['camera_id']}_{match['timestamp']}",
                "key": key,
                "path": resolved["path"],
                "source": resolved["source"],
                "camera_id": str(match["camera_id"]),
                "timestamp": int(match["timestamp"]),
                "capture_date": _label_date(label) or key.split("/")[0],
                "group": f"{match['camera_id']}|{_label_date(label) or key.split('/')[0]}",
                "classes": sorted(
                    {
                        str(annotation.get("class"))
                        for annotation in label.get("annotations") or []
                        if annotation.get("class")
                    }
                ),
                "annotations": list(label.get("annotations") or []),
                "annotator": str(label.get("annotator") or ""),
                "reviewed_at": str(label.get("reviewed_at") or ""),
                "weather": str(label.get("weather") or ""),
                "time_of_day": str(label.get("time_of_day") or ""),
                "flood_level": str(label.get("flood_level") or ""),
                "dual_carriageway": bool(label.get("dual_carriageway") or False),
                **record,
            }
        )

    if rejected:
        raise DatasetBuildError(
            f"{len(rejected)} frame(s) failed image validation: " + "; ".join(rejected[:10])
        )
    return entries, missing


def parse_args(argv: Optional[Sequence[str]] = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Materialize Dataset v3 from the live proof archive (S12-T5 / Issue #411).",
    )
    parser.add_argument("--dates", default="", help="Comma-separated capture dates to include")
    parser.add_argument(
        "--min-frames", type=int, default=V3_RELEASE_FRAMES,
        help=f"Minimum frame count; below {V3_RELEASE_FRAMES} the build is a smoke build",
    )
    parser.add_argument("--out", type=Path, default=Path("data/yolo_seg"))
    parser.add_argument("--val-split", type=float, default=0.2)
    parser.add_argument("--seed", type=int, default=42)
    parser.add_argument(
        "--manifest", type=Path, default=Path("docs/reports/s12_t5_dataset_manifest.json")
    )
    parser.add_argument("--dry-run", action="store_true", help="Validate only; write nothing")
    parser.add_argument("--limit", type=int, default=0, help="Cap the label count (smoke runs)")
    parser.add_argument(
        "--replace", action="store_true",
        help="Archive a non-empty --out to <out>.bak-<timestamp> before installing the new tree",
    )
    parser.add_argument("--labels-dir", type=Path, default=Path("data/seg_prelabels"))
    parser.add_argument(
        "--proof-root", action="append", default=[], type=Path,
        help="Local proof root, searched before R2 (repeatable)",
    )
    parser.add_argument(
        "--allow-synthetic", action="store_true",
        help="Forbidden for Dataset v3; accepted only so the refusal is explicit",
    )
    return parser.parse_args(argv)


def main(argv: Optional[Sequence[str]] = None) -> int:
    args = parse_args(argv)

    if args.allow_synthetic:
        logging.error(
            "--allow-synthetic is forbidden for Dataset v3: template polygons measure the "
            "template, not the road (spec §2.11). Remove the flag."
        )
        return EXIT_USAGE
    if not 0.0 < float(args.val_split) < 1.0:
        logging.error("--val-split must be strictly between 0 and 1, got %s", args.val_split)
        return EXIT_USAGE
    if int(args.min_frames) < 1:
        logging.error("--min-frames must be >= 1, got %s", args.min_frames)
        return EXIT_USAGE

    out_dir = Path(args.out)
    manifest_path = Path(args.manifest)

    # 1. Frozen split guard runs before any write so a refusal cannot touch the tree.
    frozen: Optional[Dict[str, Any]] = None
    split_path = out_dir / "split.json"
    if split_path.is_file():
        try:
            frozen = load_frozen_split(split_path)
            assert_frozen_split_compatible(frozen, seed=args.seed)
        except FrozenSplitError as exc:
            logging.error("%s", exc)
            return EXIT_USAGE
        logging.info(
            "Frozen split respected: seed=%s groups(train=%s, val=%s) from %s",
            frozen.get("seed"),
            len(frozen.get("train_groups") or []),
            len(frozen.get("val_groups") or []),
            split_path,
        )

    # 2. Labels are the annotated set; the archive only supplies their pixels.
    try:
        labels = load_labels(Path(args.labels_dir), _split_csv(args.dates))
    except DatasetBuildError as exc:
        logging.error("%s", exc)
        return EXIT_BUILD_FAILED
    if int(args.limit) > 0:
        labels = labels[: int(args.limit)]
    if not labels:
        logging.error(
            "No labels found in %s for dates %s; nothing to materialize",
            args.labels_dir,
            _split_csv(args.dates) or "<all>",
        )
        return EXIT_BUILD_FAILED

    # 3. Exact-key pairing only; a camera-only fallback is fatal.
    frame_keys = [str(label.get("snapshot_key") or "") for label in labels]
    if any(not key for key in frame_keys):
        logging.error("Every label must declare a snapshot_key; aborting before pairing")
        return EXIT_BUILD_FAILED
    pairs = match_pairs(frame_keys, labels)
    try:
        assert_pairs_buildable(pairs)
    except DatasetBuildError as exc:
        logging.error("%s", exc)
        return EXIT_BUILD_FAILED

    # 4. Resolve and validate each frame; local roots win over R2.
    roots = _proof_roots(args.proof_root)
    r2_fetch, r2_missing = _r2_fetcher()
    try:
        entries, missing = _stage_entries(pairs["matches"], roots, r2_fetch)
    except DatasetBuildError as exc:
        logging.error("%s", exc)
        return EXIT_BUILD_FAILED
    if missing:
        named = ", ".join(missing[:10]) + (" ..." if len(missing) > 10 else "")
        if r2_fetch is None:
            logging.error(
                "%d frame(s) are absent from every local proof root (%s) and cannot be "
                "fetched from R2 because %s is not configured: %s",
                len(missing),
                ", ".join(str(root) for root in roots) or "<none>",
                ", ".join(r2_missing),
                named,
            )
            return EXIT_USAGE
        logging.error("%d frame(s) absent from local roots and R2: %s", len(missing), named)
        return EXIT_BUILD_FAILED

    # 5. Label trust and duplicate rejection.
    for entry in entries:
        try:
            assert_label_trusted(entry)
        except LabelTrustError as exc:
            logging.error("%s", exc)
            return EXIT_BUILD_FAILED
    try:
        assert_unique_frames(entries)
    except DuplicateFrameError as exc:
        logging.error("%s", exc)
        return EXIT_BUILD_FAILED

    if len(entries) < int(args.min_frames):
        logging.error(
            "Only %d frame(s) materialized, below --min-frames %d (shortfall %d)",
            len(entries),
            args.min_frames,
            int(args.min_frames) - len(entries),
        )
        return EXIT_BUILD_FAILED

    # 6. Release-scale strata only apply to a build that claims the release floor.
    if int(args.min_frames) >= V3_RELEASE_FRAMES:
        try:
            assert_release_scale_invariants(entries)
        except DatasetBuildError as exc:
            logging.error("%s", exc)
            return EXIT_BUILD_FAILED

    # 7. Split: reuse the freeze if it covers exactly this frame set, else build.
    if frozen is not None:
        frozen_stems = set(frozen.get("train") or []) | set(frozen.get("val") or [])
        current = {str(entry["stem"]) for entry in entries}
        if frozen_stems != current:
            logging.error(
                "Frozen split covers %d stem(s) but this build produced %d; the frame set "
                "drifted from the freeze. Re-split deliberately or restore the frozen frames.",
                len(frozen_stems),
                len(current),
            )
            return EXIT_USAGE
        split = {
            "seed": int(frozen["seed"]),
            "algorithm": str(frozen["algorithm"]),
            "groups": {
                "train": len(frozen.get("train_groups") or []),
                "val": len(frozen.get("val_groups") or []),
                "overlap": len(
                    set(frozen.get("train_groups") or []) & set(frozen.get("val_groups") or [])
                ),
            },
            "train": list(frozen.get("train") or []),
            "val": list(frozen.get("val") or []),
            "train_groups": list(frozen.get("train_groups") or []),
            "val_groups": list(frozen.get("val_groups") or []),
            "group_of": {
                str(stem): str(entry.get("group") or _entry_group(entry))
                for stem, entry in ((str(e["stem"]), e) for e in entries)
            },
        }
    else:
        split = build_group_split(entries, val_ratio=args.val_split, seed=args.seed)

    try:
        assert_split_invariants(split, entries)
    except GroupLeakageError as exc:
        logging.error("Split invariant failed: %s", exc)
        return EXIT_BUILD_FAILED

    manifest = build_manifest(entries, split, generated_at=_now_iso(), seed=args.seed)
    digest = manifest_sha256(manifest)

    if frozen is not None:
        try:
            assert_frozen_split_compatible(frozen, seed=args.seed, manifest_sha256=digest)
        except FrozenSplitError as exc:
            logging.error("%s", exc)
            return EXIT_USAGE

    logging.info(
        "Dataset v3 candidate: %d frame(s), train=%d, val=%d, groups(train=%d, val=%d, overlap=%d)",
        len(entries),
        len(split["train"]),
        len(split["val"]),
        split["groups"]["train"],
        split["groups"]["val"],
        split["groups"]["overlap"],
    )
    logging.info("Strata: %s", count_strata(entries))

    try:
        outcome = materialize_dataset(
            entries,
            split,
            manifest,
            out_dir,
            replace=bool(args.replace),
            dry_run=bool(args.dry_run),
        )
    except DatasetBuildError as exc:
        logging.error("%s", exc)
        return EXIT_BUILD_FAILED

    if args.dry_run:
        logging.info("Dry run complete: no dataset tree, manifest or freeze was written")
        return EXIT_OK

    manifest_path.parent.mkdir(parents=True, exist_ok=True)
    manifest_path.write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
    logging.info("Manifest written to %s (dataset_sha256=%s)", manifest_path, digest)
    if outcome.get("backup"):
        logging.info("Previous dataset archived at %s", outcome["backup"])
    logging.info("Materialization complete: %s", outcome["out_dir"])
    return EXIT_OK


if __name__ == "__main__":
    sys.exit(main())
