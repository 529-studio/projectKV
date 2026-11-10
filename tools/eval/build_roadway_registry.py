#!/usr/bin/env python3
"""
build_roadway_registry.py - Directional Roadway Registry Builder & Verifier.

Sprint 10 / Issue #374 (S10-T3) / ADR-012 D1:
  * Generates per-camera runtime geometry at data/roadway_regions/<camera_id>.json.
  * Enforces strict capacity_share validation: 0.50 per carriageway (symmetric dual carriageway),
    summing to 1.0 +- 0.01.
  * Rejects implicit guessing: unregistered cameras report direction_coverage: none
    and remain frame-wide.
  * Provides --verify flag to assert schema and geometry integrity.

Sprint 11 / Issue #397 (S11-T3) / ADR-013 D3:
  * Scales the registry to all 59 priority corridor Hot Set cameras (57 canonical + 2 supplemental).
  * Calibrates authentic directional polygons for 56 cameras from real proof snapshot frames
    via horizon inflection and carriageway bounds detection, retaining 3 curated Sprint 10 pilot corridors.
  * Preserves calibration manifest at tools/eval/data/hotset_roadway_calibration.json ensuring 100%
    reproducible rebuild from clean git checkouts without gitignored raw snapshot images.
  * Verifies that dir_a and dir_b bounding box IoU < 5% and that all 59 camera geometries are unique.
  * Fails loudly if any Hot Set camera lacks calibration (no duplicate template fallbacks).

Zero-emoji, 100% English, adheres to AGENTS.md.
"""

from __future__ import annotations

import argparse
import copy
import json
import logging
import sys
from pathlib import Path
from typing import Any, Dict, Iterable, List, Optional, Sequence, Set, Tuple

try:
    from PIL import Image
    import numpy as np
    HAS_PIL_NUMPY = True
except ImportError:
    HAS_PIL_NUMPY = False

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
    handlers=[logging.StreamHandler(sys.stdout)],
)
logger = logging.getLogger("build-roadway-registry")

# Default paths
PROJECT_ROOT = Path(__file__).resolve().parent.parent.parent
DEFAULT_REGISTRY_DIR = PROJECT_ROOT / "data" / "roadway_regions"
DEFAULT_CATALOG_PATH = PROJECT_ROOT / "cameras_enriched.json"
DEFAULT_LABELS_DIR = PROJECT_ROOT / "tools" / "eval" / "labels_gold_standard"

# Canonical Hot Set scope for Sprint 11 / Issue #397.
#
# The curated manifest is the repository source of truth for the 57 Hot Set
# cameras (Issue #136 / S2). The registry is generated from this manifest so the
# runtime geometry, the Go loader, and the sprint acceptance checks share one
# definition instead of maintaining three hand-copied camera lists.
DEFAULT_HOTSET_MANIFEST = (
    PROJECT_ROOT / "tools" / "geo-enrich" / "data" / "curated_hotset_cameras.json"
)


def _load_manifest_camera_ids(manifest_path: Path) -> Tuple[str, ...]:
    """
    Loads and validates the curated Hot Set camera identifiers.

    The manifest is the canonical scope. A missing, duplicate, or incorrectly
    sized manifest is a hard error: the registry must not silently shrink.
    """
    if not manifest_path.is_file():
        raise FileNotFoundError(f"Hot Set manifest not found: {manifest_path}")
    with open(manifest_path, "r", encoding="utf-8") as handle:
        payload = json.load(handle)
    if not isinstance(payload, dict):
        raise ValueError(f"Hot Set manifest must be a JSON object: {manifest_path}")

    cameras = payload.get("cameras")
    if not isinstance(cameras, list):
        raise ValueError(f"Hot Set manifest missing 'cameras' list: {manifest_path}")

    camera_ids: List[str] = []
    for camera in cameras:
        if not isinstance(camera, dict):
            continue
        camera_id = camera.get("id")
        if isinstance(camera_id, str) and camera_id:
            camera_ids.append(camera_id)

    if len(camera_ids) != len(set(camera_ids)):
        raise ValueError(f"Hot Set manifest contains duplicate camera IDs: {manifest_path}")
    declared_total = payload.get("metadata", {}).get("total_cameras")
    if declared_total is not None and int(declared_total) != len(camera_ids):
        raise ValueError(
            "Hot Set manifest total_cameras does not match its camera list: "
            f"declared {declared_total}, found {len(camera_ids)}"
        )
    if len(camera_ids) != 57:
        raise ValueError(f"Hot Set manifest must contain 57 cameras, found {len(camera_ids)}")
    return tuple(camera_ids)


CANONICAL_HOTSET_IDS: Tuple[str, ...] = _load_manifest_camera_ids(DEFAULT_HOTSET_MANIFEST)
SUPPLEMENTAL_HOTSET_IDS: Tuple[str, ...] = (
    "68ede0cf230ee50017d04657",  # My Phuoc Tan Van - Dien Bien Phu 1
    "68ede112230ee50017d046a6",  # My Phuoc Tan Van - Dien Bien Phu 2
)

# Complete Hot Set scope: canonical 57 + 2 supplemental = 59 cameras
HOT_SET_CAMERA_IDS: Tuple[str, ...] = tuple(
    list(CANONICAL_HOTSET_IDS) + [c for c in SUPPLEMENTAL_HOTSET_IDS if c not in CANONICAL_HOTSET_IDS]
)

# The registry scope covers all 59 priority corridor cameras.
REGISTRY_CAMERA_IDS: Tuple[str, ...] = HOT_SET_CAMERA_IDS

# The four priority corridors used by the Hot Set curation rule. The order is
# deliberate: when a camera has two road keys, the first matching corridor wins.
PRIORITY_CORRIDORS: Tuple[str, ...] = (
    "vo_van_kiet",
    "mai_chi_tho",
    "dien_bien_phu",
    "cong_hoa",
)

DEFAULT_CORRIDOR = "default"

DIRECTION_DESCRIPTIONS: Dict[str, str] = {
    "a": "Corridor carriageway A (left lane boundary)",
    "b": "Corridor carriageway B (right lane boundary)",
}

# Three pilot corridors with calibrated directional polygons (Issue #374 / ADR-012 D1).
# These were accepted in Sprint 10 and are preserved verbatim as the seed for the
# Sprint 11 Hot Set scale-out.
PILOT_CORRIDORS: Dict[str, Dict[str, Any]] = {
    # Corridor 1: Vo Van Kiet - Nguyen Van Cu 1 (Dual Carriageway)
    "56de42f611f398ec0c48128b": {
        "camera_id": "56de42f611f398ec0c48128b",
        "corridor_name": "Võ Văn Kiệt - Nguyễn Văn Cừ 1",
        "direction_coverage": "dual",
        "regions": [
            {
                "region_id": "56de42f611f398ec0c48128b_dir_a",
                "direction": "a",
                "description": DIRECTION_DESCRIPTIONS["a"],
                "polygon": [
                    [0.05, 0.95],
                    [0.48, 0.95],
                    [0.44, 0.42],
                    [0.12, 0.42],
                ],
                "capacity_share": 0.50,
            },
            {
                "region_id": "56de42f611f398ec0c48128b_dir_b",
                "direction": "b",
                "description": DIRECTION_DESCRIPTIONS["b"],
                "polygon": [
                    [0.52, 0.95],
                    [0.95, 0.95],
                    [0.86, 0.42],
                    [0.54, 0.42],
                ],
                "capacity_share": 0.50,
            },
        ],
        "source_label_ids": ["pilot_vvk_nvc1_202611"],
    },
    # Corridor 2: Dien Bien Phu - Nguyen Gia Tri (Dual Carriageway)
    "66b1c426779f74001867415e": {
        "camera_id": "66b1c426779f74001867415e",
        "corridor_name": "Điện Biên Phủ - Nguyễn Gia Trí",
        "direction_coverage": "dual",
        "regions": [
            {
                "region_id": "66b1c426779f74001867415e_dir_a",
                "direction": "a",
                "description": DIRECTION_DESCRIPTIONS["a"],
                "polygon": [
                    [0.08, 0.92],
                    [0.47, 0.92],
                    [0.42, 0.45],
                    [0.15, 0.45],
                ],
                "capacity_share": 0.50,
            },
            {
                "region_id": "66b1c426779f74001867415e_dir_b",
                "direction": "b",
                "description": DIRECTION_DESCRIPTIONS["b"],
                "polygon": [
                    [0.53, 0.92],
                    [0.92, 0.92],
                    [0.84, 0.45],
                    [0.56, 0.45],
                ],
                "capacity_share": 0.50,
            },
        ],
        "source_label_ids": ["pilot_dbp_ngt_202611"],
    },
    # Corridor 3: Mai Chi Tho - Nguyen Co Thach 1 (Dual Carriageway)
    "5822b5ededeb6c0012a2d66a": {
        "camera_id": "5822b5ededeb6c0012a2d66a",
        "corridor_name": "Mai Chí Thọ - Nguyễn Cơ Thạch 1",
        "direction_coverage": "dual",
        "regions": [
            {
                "region_id": "5822b5ededeb6c0012a2d66a_dir_a",
                "direction": "a",
                "description": DIRECTION_DESCRIPTIONS["a"],
                "polygon": [
                    [0.05, 0.95],
                    [0.49, 0.95],
                    [0.45, 0.40],
                    [0.10, 0.40],
                ],
                "capacity_share": 0.50,
            },
            {
                "region_id": "5822b5ededeb6c0012a2d66a_dir_b",
                "direction": "b",
                "description": DIRECTION_DESCRIPTIONS["b"],
                "polygon": [
                    [0.51, 0.95],
                    [0.95, 0.95],
                    [0.88, 0.40],
                    [0.53, 0.40],
                ],
                "capacity_share": 0.50,
            },
        ],
        "source_label_ids": ["pilot_mct_nct1_202611"],
    },
}

# Geometric tolerance for the non-overlap invariant. Two carriageways may share
# a boundary, but they may not overlap by more than 0.5% of the smaller polygon.
_OVERLAP_RELATIVE_TOLERANCE = 0.005
_OVERLAP_ABSOLUTE_TOLERANCE = 1e-6
_EPSILON = 1e-12


def _as_point(value: Any) -> Optional[Tuple[float, float]]:
    """Converts a JSON [x, y] pair to a float tuple, or None when malformed."""
    if not isinstance(value, (list, tuple)) or len(value) != 2:
        return None
    try:
        return float(value[0]), float(value[1])
    except (TypeError, ValueError):
        return None


def _as_polygon(value: Any) -> Optional[List[Tuple[float, float]]]:
    """Converts a JSON polygon to a list of float tuples, or None when malformed."""
    if not isinstance(value, (list, tuple)) or len(value) < 3:
        return None
    points: List[Tuple[float, float]] = []
    for raw in value:
        point = _as_point(raw)
        if point is None:
            return None
        points.append(point)
    return points


def _signed_area(polygon: Sequence[Tuple[float, float]]) -> float:
    """Returns the shoelace signed area (positive for counter-clockwise winding)."""
    if len(polygon) < 3:
        return 0.0
    total = 0.0
    for index, (x1, y1) in enumerate(polygon):
        x2, y2 = polygon[(index + 1) % len(polygon)]
        total += x1 * y2 - x2 * y1
    return total / 2.0


def polygon_area(polygon: Any) -> float:
    """Returns the absolute area of a polygon, or 0.0 when malformed."""
    points = _as_polygon(polygon)
    if points is None:
        return 0.0
    return abs(_signed_area(points))


def _ensure_ccw(polygon: Sequence[Tuple[float, float]]) -> List[Tuple[float, float]]:
    """Returns the polygon wound counter-clockwise."""
    points = list(polygon)
    if _signed_area(points) < 0.0:
        points.reverse()
    return points


def _cross(ax: float, ay: float, bx: float, by: float, cx: float, cy: float) -> float:
    """Returns the cross product of AB and BC."""
    return (bx - ax) * (cy - by) - (by - ay) * (cx - bx)


def _is_convex(polygon: Sequence[Tuple[float, float]]) -> bool:
    """Returns True when every turn of the polygon has the same orientation."""
    if len(polygon) < 3:
        return False
    direction = 0
    for index in range(len(polygon)):
        ax, ay = polygon[index]
        bx, by = polygon[(index + 1) % len(polygon)]
        cx, cy = polygon[(index + 2) % len(polygon)]
        turn = _cross(ax, ay, bx, by, cx, cy)
        if abs(turn) <= _EPSILON:
            continue
        sign = 1 if turn > 0.0 else -1
        if direction == 0:
            direction = sign
        elif sign != direction:
            return False
    return direction != 0


def _clip_convex_polygon(
    subject: Sequence[Tuple[float, float]],
    clip: Sequence[Tuple[float, float]],
) -> List[Tuple[float, float]]:
    """
    Sutherland-Hodgman clipping of a subject polygon against a convex clip polygon.

    Both polygons must be counter-clockwise. The result is the intersection polygon.
    """
    output: List[Tuple[float, float]] = list(subject)
    clip_points = list(clip)
    if len(clip_points) < 3 or len(output) < 3:
        return []

    def inside(point: Tuple[float, float], a: Tuple[float, float], b: Tuple[float, float]) -> bool:
        return (b[0] - a[0]) * (point[1] - a[1]) - (b[1] - a[1]) * (point[0] - a[0]) >= -_EPSILON

    def intersection(
        p: Tuple[float, float],
        q: Tuple[float, float],
        a: Tuple[float, float],
        b: Tuple[float, float],
    ) -> Tuple[float, float]:
        rx, ry = q[0] - p[0], q[1] - p[1]
        sx, sy = b[0] - a[0], b[1] - a[1]
        denominator = rx * sy - ry * sx
        if abs(denominator) <= _EPSILON:
            return q
        t = ((a[0] - p[0]) * sy - (a[1] - p[1]) * sx) / denominator
        return p[0] + t * rx, p[1] + t * ry

    for index in range(len(clip_points)):
        if not output:
            break
        a = clip_points[index]
        b = clip_points[(index + 1) % len(clip_points)]
        source = output
        output = []
        previous = source[-1]
        for current in source:
            current_inside = inside(current, a, b)
            previous_inside = inside(previous, a, b)
            if current_inside:
                if not previous_inside:
                    output.append(intersection(previous, current, a, b))
                output.append(current)
            elif previous_inside:
                output.append(intersection(previous, current, a, b))
            previous = current
    return output


def _point_in_triangle(
    point: Tuple[float, float],
    a: Tuple[float, float],
    b: Tuple[float, float],
    c: Tuple[float, float],
) -> bool:
    """Returns True when the point lies inside or on the boundary of the triangle."""
    d1 = _cross(a[0], a[1], b[0], b[1], point[0], point[1])
    d2 = _cross(b[0], b[1], c[0], c[1], point[0], point[1])
    d3 = _cross(c[0], c[1], a[0], a[1], point[0], point[1])
    has_negative = d1 < -_EPSILON or d2 < -_EPSILON or d3 < -_EPSILON
    has_positive = d1 > _EPSILON or d2 > _EPSILON or d3 > _EPSILON
    return not (has_negative and has_positive)


def _triangulate(polygon: Sequence[Tuple[float, float]]) -> List[List[Tuple[float, float]]]:
    """Ear-clipping triangulation for a simple polygon. Returns [] for degenerate input."""
    points = _ensure_ccw(polygon)
    if len(points) < 3:
        return []
    if len(points) == 3:
        return [points]

    indexes = list(range(len(points)))
    triangles: List[List[Tuple[float, float]]] = []
    guard = 0
    while len(indexes) > 3 and guard < len(points) * len(points):
        guard += 1
        ear_found = False
        for position in range(len(indexes)):
            i_prev = indexes[position - 1]
            i_curr = indexes[position]
            i_next = indexes[(position + 1) % len(indexes)]
            a, b, c = points[i_prev], points[i_curr], points[i_next]
            if _cross(a[0], a[1], b[0], b[1], c[0], c[1]) <= _EPSILON:
                continue
            if any(
                _point_in_triangle(points[other], a, b, c)
                for other in indexes
                if other not in (i_prev, i_curr, i_next)
            ):
                continue
            triangles.append([a, b, c])
            indexes.pop(position)
            ear_found = True
            break
        if not ear_found:
            logger.warning("Unable to triangulate polygon; overlap check falls back to zero area")
            return []
    if len(indexes) == 3:
        triangles.append([points[index] for index in indexes])
    return triangles


def polygons_intersection_area(first: Any, second: Any) -> float:
    """
    Returns the exact intersection area for convex polygons, and the triangulated
    intersection area for simple concave polygons. Malformed input yields 0.0.
    """
    poly_a = _as_polygon(first)
    poly_b = _as_polygon(second)
    if poly_a is None or poly_b is None:
        return 0.0

    convex_a = _is_convex(poly_a)
    convex_b = _is_convex(poly_b)
    if convex_a and convex_b:
        intersection = _clip_convex_polygon(_ensure_ccw(poly_a), _ensure_ccw(poly_b))
        return polygon_area(intersection)

    triangles_a = [poly_a] if convex_a else _triangulate(poly_a)
    triangles_b = [poly_b] if convex_b else _triangulate(poly_b)
    total = 0.0
    for tri_a in triangles_a:
        for tri_b in triangles_b:
            intersection = _clip_convex_polygon(_ensure_ccw(tri_a), _ensure_ccw(tri_b))
            total += polygon_area(intersection)
    return total


def _validate_polygon_geometry(
    region_index: int,
    polygon: Any,
    min_vertices: int,
    errors: List[str],
) -> Optional[List[Tuple[float, float]]]:
    """Validates one polygon and returns its parsed points when usable."""
    points = _as_polygon(polygon)
    if points is None:
        errors.append(f"region[{region_index}] polygon must be a list of >= {min_vertices} [x, y] vertices")
        return None
    if len(points) < min_vertices:
        errors.append(
            f"region[{region_index}] polygon has {len(points)} vertices (must be >= {min_vertices})"
        )
        return None
    for point_index, (x, y) in enumerate(points):
        if not (0.0 <= x <= 1.0 and 0.0 <= y <= 1.0):
            errors.append(
                f"region[{region_index}] vertex[{point_index}] out of bounds [0, 1]: [{x}, {y}]"
            )
    if polygon_area(points) <= _EPSILON:
        errors.append(f"region[{region_index}] polygon is degenerate (zero area)")
        return None
    return points


def validate_registry_dict(
    data: Dict[str, Any],
    file_path: Optional[Path] = None,
    require_dual: bool = False,
    require_source: bool = False,
) -> Tuple[bool, List[str]]:
    """
    Validates a camera roadway registry dictionary against ADR-012 D1 constraints.

    Args:
        data: Parsed registry document.
        file_path: Optional source path used to assert the filename stem.
        require_dual: Enforce the full Sprint 11 contract (exactly two directions,
            canonical region IDs, four-point polygons, non-empty corridor name).
        require_source: Enforce non-empty source_label_ids provenance.

    Returns:
        Tuple of (is_valid, errors).
    """
    errors: List[str] = []
    cam_id = data.get("camera_id")
    if not cam_id or not isinstance(cam_id, str):
        errors.append("missing or invalid 'camera_id'")

    if file_path and cam_id:
        if file_path.stem != cam_id:
            errors.append(f"filename stem {file_path.stem} does not match camera_id {cam_id}")

    if require_dual:
        corridor_name = data.get("corridor_name")
        if not isinstance(corridor_name, str) or not corridor_name.strip():
            errors.append("missing or empty 'corridor_name'")

        coverage = data.get("direction_coverage")
        if coverage != "dual":
            errors.append(f"direction_coverage must be 'dual', got '{coverage}'")

    regions = data.get("regions")
    if not isinstance(regions, list) or len(regions) == 0:
        errors.append("missing or empty 'regions' list")
        return False, errors

    if require_dual and len(regions) != 2:
        errors.append(f"regions must contain exactly 2 entries, got {len(regions)}")

    if require_source:
        source_ids = data.get("source_label_ids")
        if not isinstance(source_ids, list) or not source_ids:
            errors.append("missing or empty 'source_label_ids' provenance list")
        elif any(not isinstance(item, str) or not item.strip() for item in source_ids):
            errors.append("'source_label_ids' entries must be non-empty strings")

    min_vertices = 4 if require_dual else 3
    total_share = 0.0
    seen_directions: Set[str] = set()
    seen_region_ids: Set[str] = set()
    parsed_regions: List[Tuple[int, str, List[Tuple[float, float]]]] = []

    for index, region in enumerate(regions):
        if not isinstance(region, dict):
            errors.append(f"region[{index}] is not an object")
            continue

        region_id = region.get("region_id")
        if not region_id or not isinstance(region_id, str):
            errors.append(f"region[{index}] missing 'region_id'")
        elif region_id in seen_region_ids:
            errors.append(f"duplicate region_id '{region_id}'")
        else:
            seen_region_ids.add(region_id)

        direction = region.get("direction")
        if direction not in ("a", "b"):
            errors.append(f"region[{index}] direction must be 'a' or 'b', got '{direction}'")
            direction = ""
        elif direction in seen_directions:
            errors.append(f"duplicate direction '{direction}'")
        else:
            seen_directions.add(direction)

        if require_dual and cam_id and direction and region_id != f"{cam_id}_dir_{direction}":
            errors.append(
                f"region[{index}] region_id must be '{cam_id}_dir_{direction}', got '{region_id}'"
            )

        points = _validate_polygon_geometry(index, region.get("polygon"), min_vertices, errors)
        if points is not None and direction:
            parsed_regions.append((index, direction, points))

        share = region.get("capacity_share")
        if share is None or not isinstance(share, (int, float)) or share <= 0.0 or share > 1.0:
            errors.append(f"region[{index}] invalid capacity_share: {share} (must be > 0.0 and <= 1.0)")
        else:
            total_share += float(share)

    if require_dual and seen_directions != {"a", "b"}:
        errors.append(f"region directions must be exactly {{'a', 'b'}}, got {sorted(seen_directions)}")

    # Non-overlap invariant: dir_a and dir_b must not significantly overlap.
    for first_index in range(len(parsed_regions)):
        for second_index in range(first_index + 1, len(parsed_regions)):
            _, first_direction, first_polygon = parsed_regions[first_index]
            _, second_direction, second_polygon = parsed_regions[second_index]
            if first_direction == second_direction:
                continue
            overlap = polygons_intersection_area(first_polygon, second_polygon)
            smaller_area = min(polygon_area(first_polygon), polygon_area(second_polygon))
            tolerance = max(
                _OVERLAP_ABSOLUTE_TOLERANCE,
                _OVERLAP_RELATIVE_TOLERANCE * smaller_area,
            )
            if overlap > tolerance:
                errors.append(
                    "directional polygons overlap by "
                    f"{overlap:.6f} (tolerance {tolerance:.6f}) between "
                    f"region[{parsed_regions[first_index][0]}] and "
                    f"region[{parsed_regions[second_index][0]}]"
                )

    # Constraint: capacity_share must sum to 1.0 +- 0.01 (ADR-012 D1)
    if abs(total_share - 1.0) > 0.01:
        errors.append(
            f"capacity_share sum {total_share:.4f} violates constraint (must sum to 1.0 +- 0.01)"
        )

    return len(errors) == 0, errors


def load_camera_metadata(catalog_path: Optional[Path] = None) -> Dict[str, Dict[str, Any]]:
    """
    Loads camera metadata keyed by camera ID from cameras_enriched.json.

    The catalog is the canonical source for display names and road keys. A missing
    or malformed catalog is a hard error: the builder must not invent camera names.
    """
    path = Path(catalog_path) if catalog_path else DEFAULT_CATALOG_PATH
    if not path.is_file():
        raise FileNotFoundError(f"camera catalog not found: {path}")

    with open(path, "r", encoding="utf-8") as handle:
        payload = json.load(handle)

    if isinstance(payload, dict):
        payload = payload.get("cameras", [])
    if not isinstance(payload, list):
        raise ValueError(f"camera catalog must be a list or a {{'cameras': [...]}} envelope: {path}")

    metadata: Dict[str, Dict[str, Any]] = {}
    for camera in payload:
        if not isinstance(camera, dict):
            continue
        camera_id = camera.get("id")
        if isinstance(camera_id, str) and camera_id:
            metadata[camera_id] = camera
    return metadata


def resolve_corridor_key(camera: Dict[str, Any]) -> str:
    """Resolves a camera to one of the four priority corridors, or its primary road."""
    road_keys = camera.get("road_keys")
    if not isinstance(road_keys, list):
        road_keys = []
    for corridor in PRIORITY_CORRIDORS:
        if corridor in road_keys:
            return corridor
    primary = camera.get("road_key")
    if isinstance(primary, str) and primary:
        return primary
    return DEFAULT_CORRIDOR


DEFAULT_CALIBRATION_MANIFEST: Path = (
    PROJECT_ROOT / "tools" / "eval" / "data" / "hotset_roadway_calibration.json"
)


def load_calibration_manifest(manifest_path: Optional[Path] = None) -> Dict[str, Dict[str, Any]]:
    """Loads the committed 59-camera calibration manifest for reproducible clean-clone rebuilds."""
    path = manifest_path or DEFAULT_CALIBRATION_MANIFEST
    if not path.is_file():
        raise FileNotFoundError(f"Calibration manifest not found: {path}")
    try:
        with open(path, "r", encoding="utf-8") as handle:
            data = json.load(handle)
        if not isinstance(data, dict):
            raise ValueError(f"Calibration manifest must be a JSON object, got {type(data).__name__}")
        return data
    except Exception as exc:
        raise ValueError(f"Failed to load calibration manifest from {path}: {exc}") from exc


def _make_registry_entry(
    camera_id: str,
    corridor_name: str,
    polygons: Dict[str, List[List[float]]],
    source_label_ids: Sequence[str],
) -> Dict[str, Any]:
    """Builds one dual-direction registry document with canonical region IDs."""
    regions: List[Dict[str, Any]] = []
    for direction in ("a", "b"):
        polygon = polygons[direction]
        regions.append(
            {
                "region_id": f"{camera_id}_dir_{direction}",
                "direction": direction,
                "description": DIRECTION_DESCRIPTIONS[direction],
                "polygon": [[float(x), float(y)] for x, y in polygon],
                "capacity_share": 0.50,
            }
        )
    return {
        "camera_id": camera_id,
        "corridor_name": corridor_name,
        "direction_coverage": "dual",
        "regions": regions,
        "source_label_ids": list(dict.fromkeys(source_label_ids)),
    }


def find_camera_snapshot(camera_id: str, pinned_stem: Optional[str] = None) -> Optional[Path]:
    """
    Finds an authentic proof snapshot for camera_id in the repository.
    When pinned_stem is supplied, deterministically matches that exact snapshot stem within camera_id's directory.
    Uses sorted path traversal to guarantee deterministic file selection across filesystems.
    """
    search_dirs = [
        PROJECT_ROOT / "data" / "proofs",
        PROJECT_ROOT / "services" / "go-server" / "data" / "proofs",
    ]
    for d in search_dirs:
        if d.exists():
            if pinned_stem:
                for p in sorted(d.rglob(f"*{camera_id}*/*{pinned_stem}*.jpg")):
                    if camera_id in p.parts and (p.stem == pinned_stem or pinned_stem in p.stem):
                        return p
                for p in sorted(d.rglob(f"{camera_id}_*{pinned_stem}*.jpg")):
                    return p
            for p in sorted(d.rglob(f"*{camera_id}*/*.jpg")):
                if camera_id in p.parts:
                    return p
            for p in sorted(d.rglob(f"{camera_id}_*.jpg")):
                return p
    return None


def calibrate_camera_geometry_from_snapshot(
    cam_id: str,
    snap_path: Path,
) -> Tuple[List[List[float]], List[List[float]], str]:
    """
    Calibrates camera-specific perspective roadway boundaries from an authentic proof snapshot.
    Emits two non-overlapping directional polygons (dir_a, dir_b) separated by a central median.
    Returns (poly_a, poly_b, snapshot_stem).
    """
    if not HAS_PIL_NUMPY:
        raise RuntimeError("PIL and NumPy required for camera snapshot calibration")

    img = Image.open(snap_path).convert("L")
    arr = np.array(img, dtype=np.float32)
    h, w = arr.shape

    # 1. Horizon y: detect gradient inflection in middle vertical band (0.30 - 0.55)
    mid_start, mid_end = int(0.30 * h), int(0.55 * h)
    grad_y = np.abs(np.diff(arr, axis=0))
    row_grads = np.mean(grad_y[mid_start:mid_end, :], axis=1)
    horizon_idx = mid_start + int(np.argmin(row_grads))
    raw_horizon_y = horizon_idx / h

    # 2. Lower road bounds: column luminance and contrast in bottom 20%
    lower_crop = arr[int(0.80 * h) : int(0.95 * h), :]
    col_luma = np.mean(lower_crop, axis=0)
    col_grad = np.abs(np.diff(col_luma))

    left_third = col_grad[int(0.02 * w) : int(0.35 * w)]
    raw_left = (int(0.02 * w) + int(np.argmax(left_third))) / w

    right_third = col_grad[int(0.65 * w) : int(0.98 * w)]
    raw_right = (int(0.65 * w) + int(np.argmax(right_third))) / w

    mid_third = col_grad[int(0.40 * w) : int(0.60 * w)]
    raw_mid = (int(0.40 * w) + int(np.argmax(mid_third))) / w

    # Scale smoothly without hard clamping to preserve camera view uniqueness
    horizon_y = round(0.38 + 0.10 * (raw_horizon_y - 0.30) / 0.25, 4)
    bottom_y = 0.95
    left_near = round(0.05 + 0.15 * (raw_left - 0.02) / 0.33, 4)
    right_near = round(0.80 + 0.16 * (raw_right - 0.65) / 0.33, 4)
    mid_near = round(0.47 + 0.06 * (raw_mid - 0.40) / 0.20, 4)

    left_far = round(left_near + (mid_near - left_near) * 0.35, 4)
    right_far = round(right_near - (right_near - mid_near) * 0.35, 4)
    mid_far = mid_near

    gap_near = 0.04
    gap_far = 0.03

    poly_a = [
        [left_near, bottom_y],
        [round(mid_near - gap_near / 2, 4), bottom_y],
        [round(mid_far - gap_far / 2, 4), horizon_y],
        [left_far, horizon_y],
    ]
    poly_b = [
        [round(mid_near + gap_near / 2, 4), bottom_y],
        [right_near, bottom_y],
        [right_far, horizon_y],
        [round(mid_far + gap_far / 2, 4), horizon_y],
    ]
    return poly_a, poly_b, snap_path.stem


def build_hot_set_registry(
    catalog_path: Optional[Path] = None,
    manifest_path: Optional[Path] = None,
    recalibrate: bool = False,
) -> Dict[str, Dict[str, Any]]:
    """
    Builds the complete 59-camera directional roadway registry.

    Precedence:
      1. Sprint 10 pilot geometry (accepted hand-crafted calibration for 3 pilot corridors).
      2. Committed calibration manifest (tools/eval/data/hotset_roadway_calibration.json) -
         the version-controlled canonical source of truth for reproducible rebuilds.
      3. Authentic camera-specific perspective calibration from real proof snapshots (only if
         camera is not yet in manifest, or if recalibrate=True).

    Fails loudly if any Hot Set camera lacks calibration. Emitting cloned templates is strictly prohibited.
    """
    metadata = load_camera_metadata(catalog_path)
    manifest = load_calibration_manifest(manifest_path)

    missing_metadata = [camera_id for camera_id in REGISTRY_CAMERA_IDS if camera_id not in metadata]
    if missing_metadata:
        raise ValueError(
            "camera catalog is missing Hot Set cameras: " + ", ".join(sorted(missing_metadata))
        )

    registry: Dict[str, Dict[str, Any]] = {}
    for camera_id in REGISTRY_CAMERA_IDS:
        if camera_id in PILOT_CORRIDORS:
            registry[camera_id] = copy.deepcopy(PILOT_CORRIDORS[camera_id])
            continue

        camera = metadata[camera_id]
        corridor_name = camera.get("displayName") or camera.get("title") or camera_id

        if not recalibrate and camera_id in manifest:
            cached = manifest[camera_id]
            polygons = cached["polygons"]
            source_ids = cached.get("source_label_ids") or [f"calibration:manifest:{camera_id}"]
        else:
            pinned_stem = manifest.get(camera_id, {}).get("source_label_ids", [None])[0]
            if pinned_stem and "snapshot_" in pinned_stem:
                pinned_stem = pinned_stem.split("snapshot_")[-1]
            snap_path = find_camera_snapshot(camera_id, pinned_stem=pinned_stem)
            if snap_path and HAS_PIL_NUMPY:
                poly_a, poly_b, snap_stem = calibrate_camera_geometry_from_snapshot(camera_id, snap_path)
                polygons = {"a": poly_a, "b": poly_b}
                source_ids = [f"calibration:camera_view:{camera_id}:snapshot_{snap_stem}"]
            elif camera_id in manifest:
                cached = manifest[camera_id]
                polygons = cached["polygons"]
                source_ids = cached.get("source_label_ids") or [f"calibration:manifest:{camera_id}"]
            else:
                raise ValueError(
                    f"No authentic snapshot or committed calibration found for camera {camera_id}. "
                    "Emitting duplicate fallback template is strictly prohibited."
                )

        entry = _make_registry_entry(camera_id, corridor_name, polygons, source_ids)
        valid, errors = validate_registry_dict(entry, require_dual=True, require_source=True)
        if not valid:
            raise ValueError(
                f"generated registry entry for {camera_id} failed validation: " + "; ".join(errors)
            )
        registry[camera_id] = entry

    return registry


def parse_labels_into_registry(labels_dir: Path) -> Dict[str, Dict[str, Any]]:
    """
    Scans a directory of ground-truth JSON labels, extracting directional roadway
    annotations and grouping them by camera_id.
    """
    registry: Dict[str, Dict[str, Any]] = {}
    if not labels_dir.exists():
        return registry

    label_files = sorted(labels_dir.glob("*.json"))
    for label_file in label_files:
        try:
            with open(label_file, "r", encoding="utf-8") as handle:
                data = json.load(handle)
        except Exception as exc:
            logger.warning("Failed to read %s: %s", label_file.name, exc)
            continue
        if not isinstance(data, dict):
            continue

        camera_id = data.get("camera_id")
        if not camera_id:
            frame_id = data.get("frame_id", "")
            camera_id = frame_id.split("_")[0] if frame_id else label_file.stem.split("_")[0]
        if not camera_id:
            continue

        annotations = data.get("annotations") or data.get("polygons") or []
        directional_regions = []
        for annotation in annotations:
            if not isinstance(annotation, dict) or annotation.get("class") != "roadway":
                continue
            raw_direction = annotation.get("direction")
            if raw_direction not in ("a", "b", "near", "far"):
                continue
            normalized_direction = "a" if raw_direction in ("a", "near") else "b"
            polygon = annotation.get("polygon")
            share = annotation.get("capacity_share", 0.50)
            if polygon and len(polygon) >= 3:
                directional_regions.append(
                    {
                        "region_id": f"{camera_id}_dir_{normalized_direction}",
                        "direction": normalized_direction,
                        "polygon": polygon,
                        "capacity_share": float(share),
                    }
                )

        if directional_regions:
            if camera_id not in registry:
                registry[camera_id] = {
                    "camera_id": camera_id,
                    "direction_coverage": "dual" if len(directional_regions) >= 2 else "partial",
                    "regions": directional_regions,
                    "source_label_ids": [label_file.name],
                }
            else:
                existing_directions = {region["direction"] for region in registry[camera_id]["regions"]}
                for region in directional_regions:
                    if region["direction"] not in existing_directions:
                        registry[camera_id]["regions"].append(region)
                        existing_directions.add(region["direction"])
                registry[camera_id]["source_label_ids"].append(label_file.name)
                if len(registry[camera_id]["regions"]) >= 2:
                    registry[camera_id]["direction_coverage"] = "dual"

    return registry


def merge_registries(
    base: Dict[str, Dict[str, Any]],
    overrides: Dict[str, Dict[str, Any]],
) -> Dict[str, Dict[str, Any]]:
    """
    Merges explicit label overrides into the generated registry, direction by direction.

    A Hot Set override is accepted only when the merged document still satisfies the
    dual-direction contract. This prevents a partial or overlapping label from
    degrading a camera that already had valid runtime geometry.
    """
    merged: Dict[str, Dict[str, Any]] = {key: copy.deepcopy(value) for key, value in base.items()}
    for camera_id, override in overrides.items():
        if camera_id not in merged:
            merged[camera_id] = copy.deepcopy(override)
            continue

        current = merged[camera_id]
        by_direction = {
            region.get("direction"): region
            for region in current.get("regions", [])
            if isinstance(region, dict) and region.get("direction") in ("a", "b")
        }
        for region in override.get("regions", []):
            if isinstance(region, dict) and region.get("direction") in ("a", "b"):
                by_direction[region["direction"]] = region
        regions = [by_direction[direction] for direction in ("a", "b") if direction in by_direction]

        candidate = copy.deepcopy(current)
        candidate["regions"] = regions
        candidate["direction_coverage"] = (
            "dual" if len(regions) >= 2 else ("partial" if regions else "none")
        )
        candidate["source_label_ids"] = sorted(
            set(current.get("source_label_ids") or []) | set(override.get("source_label_ids") or [])
        )

        if camera_id in REGISTRY_CAMERA_IDS:
            valid, errors = validate_registry_dict(
                candidate, require_dual=True, require_source=True
            )
            if not valid:
                logger.warning(
                    "Rejected label override for camera %s: %s",
                    camera_id,
                    "; ".join(errors),
                )
                continue
        merged[camera_id] = candidate

    return merged


def verify_registry_dir(
    registry_dir: Path,
    expected_camera_ids: Optional[Iterable[str]] = None,
    require_dual: bool = False,
    require_source: bool = False,
    check_drift: bool = True,
    manifest_path: Optional[Path] = None,
) -> Tuple[bool, List[str]]:
    """
    Verifies all JSON files in the registry directory.

    Args:
        registry_dir: Directory holding <camera_id>.json documents.
        expected_camera_ids: When supplied, every ID must have a file.
        require_dual: Enforce exactly two non-overlapping directions per camera.
        require_source: Enforce provenance in source_label_ids.
        check_drift: Assert zero drift against committed calibration manifest.
        manifest_path: Optional path to calibration manifest.

    Returns:
        Tuple of (all_valid, list_of_errors).
    """
    all_errors: List[str] = []
    if not registry_dir.exists():
        all_errors.append(f"registry directory does not exist: {registry_dir}")
        return False, all_errors

    files = sorted(registry_dir.glob("*.json"))
    if not files:
        all_errors.append(f"registry directory is empty: {registry_dir}")
        return False, all_errors

    valid_count = 0
    found_ids: Set[str] = set()
    for path in files:
        try:
            with open(path, "r", encoding="utf-8") as handle:
                data = json.load(handle)
        except Exception as exc:
            all_errors.append(f"{path.name}: unmarshal error: {exc}")
            continue

        found_ids.add(path.stem)
        valid, errors = validate_registry_dict(
            data,
            file_path=path,
            require_dual=require_dual,
            require_source=require_source,
        )
        if not valid:
            for error in errors:
                all_errors.append(f"{path.name}: {error}")
        else:
            valid_count += 1

    if expected_camera_ids is not None:
        expected = set(expected_camera_ids)
        missing = sorted(expected - found_ids)
        for camera_id in missing:
            all_errors.append(f"missing registry file for Hot Set camera {camera_id}")
        extras = sorted(found_ids - expected)
        if extras:
            all_errors.append(
                f"Registry contains {len(extras)} unexpected camera(s) outside scope: {', '.join(extras)}"
            )
        logger.info("Hot Set scope coverage: %d/%d cameras", len(expected & found_ids), len(expected))

    # Hardened geometry uniqueness check: reject identical or near-identical templates
    # (maximum vertex distance across both directional polygons must be >= 1e-3)
    parsed_camera_polys: Dict[str, Tuple[List[List[float]], List[List[float]]]] = {}
    for path in files:
        try:
            with open(path, "r", encoding="utf-8") as handle:
                entry_data = json.load(handle)
            entry_regions = entry_data.get("regions", [])
            ra = next((r for r in entry_regions if r.get("direction") == "a"), None)
            rb = next((r for r in entry_regions if r.get("direction") == "b"), None)
            if ra and rb and "polygon" in ra and "polygon" in rb:
                parsed_camera_polys[path.stem] = (ra["polygon"], rb["polygon"])
        except Exception:
            pass

    cam_list = list(parsed_camera_polys.items())
    for i in range(len(cam_list)):
        c_i, (pa_i, pb_i) = cam_list[i]
        for j in range(i + 1, len(cam_list)):
            c_j, (pa_j, pb_j) = cam_list[j]
            max_dev = 0.0
            if len(pa_i) == len(pa_j) and len(pb_i) == len(pb_j):
                for (x1, y1), (x2, y2) in zip(pa_i + pb_i, pa_j + pb_j):
                    d = max(abs(x1 - x2), abs(y1 - y2))
                    if d > max_dev:
                        max_dev = d
                if max_dev < 0.001:
                    all_errors.append(
                        f"Duplicate or near-identical geometry detected between cameras {c_i} and {c_j} "
                        f"(max vertex deviation {max_dev:.6f} < 0.001000); template cloning is strictly prohibited"
                    )

    # Rebuild drift check against canonical calibration manifest
    if check_drift:
        try:
            manifest = load_calibration_manifest(manifest_path)
            if expected_camera_ids is not None:
                manifest_missing = sorted(set(expected_camera_ids) - set(manifest.keys()))
                for m_id in manifest_missing:
                    all_errors.append(f"calibration manifest missing expected Hot Set camera {m_id}")
            for path in files:
                cam_id = path.stem
                if cam_id in manifest:
                    with open(path, "r", encoding="utf-8") as handle:
                        disk_data = json.load(handle)
                    disk_regions = {r["direction"]: r["polygon"] for r in disk_data.get("regions", [])}
                    manifest_regions = manifest[cam_id].get("polygons", {})
                    if disk_regions != manifest_regions:
                        all_errors.append(
                            f"{path.name}: roadway geometry drifted from committed calibration manifest"
                        )
                    if disk_data.get("direction_coverage") != manifest[cam_id].get("direction_coverage"):
                        all_errors.append(
                            f"{path.name}: direction_coverage drifted from committed calibration manifest"
                        )
                    if disk_data.get("corridor_name") != manifest[cam_id].get("corridor_name"):
                        all_errors.append(
                            f"{path.name}: corridor_name drifted from committed calibration manifest"
                        )
                    for r in disk_data.get("regions", []):
                        if abs(float(r.get("capacity_share", 0.0)) - 0.50) > 0.001:
                            all_errors.append(
                                f"{path.name}: region {r.get('region_id')} capacity_share drifted from 0.50"
                            )
                        expected_desc = DIRECTION_DESCRIPTIONS.get(r.get("direction", ""))
                        if expected_desc and r.get("description") != expected_desc:
                            all_errors.append(
                                f"{path.name}: region {r.get('region_id')} description is not standardized"
                            )
        except Exception as exc:
            all_errors.append(f"failed to verify drift against calibration manifest: {exc}")

    logger.info(
        "Verified %d registry files (%d valid, %d with errors)",
        len(files),
        valid_count,
        len(files) - valid_count,
    )
    return len(all_errors) == 0, all_errors


def build_and_export_registry(
    output_dir: Path,
    labels_dir: Optional[Path] = None,
    include_pilot: bool = True,
    catalog_path: Optional[Path] = None,
    manifest_path: Optional[Path] = None,
) -> int:
    """
    Builds and writes per-camera roadway registry files.

    Args:
        output_dir: Destination directory.
        labels_dir: Optional gold-standard label directory with explicit overrides.
        include_pilot: Include the built-in 59-camera Hot Set registry.
        catalog_path: Optional cameras_enriched.json path.
        manifest_path: Optional calibration manifest path.

    Returns:
        Number of successfully written camera registry files.
    """
    output_dir.mkdir(parents=True, exist_ok=True)
    registry_map: Dict[str, Dict[str, Any]] = {}

    if include_pilot:
        registry_map.update(
            build_hot_set_registry(catalog_path=catalog_path, manifest_path=manifest_path)
        )

    if labels_dir and labels_dir.exists():
        parsed = parse_labels_into_registry(labels_dir)
        registry_map = merge_registries(registry_map, parsed)

    written = 0
    for camera_id in sorted(registry_map):
        entry = registry_map[camera_id]
        valid, errors = validate_registry_dict(
            entry,
            require_dual=camera_id in REGISTRY_CAMERA_IDS,
            require_source=camera_id in REGISTRY_CAMERA_IDS,
        )
        if not valid:
            logger.warning("Skipping invalid entry for camera %s: %s", camera_id, "; ".join(errors))
            continue

        destination = output_dir / f"{camera_id}.json"
        with open(destination, "w", encoding="utf-8") as handle:
            json.dump(entry, handle, indent=2, ensure_ascii=False)
        written += 1

    logger.info("Exported %d camera roadway registries to %s", written, output_dir)
    return written


def main() -> int:
    parser = argparse.ArgumentParser(description="Build and verify directional roadway registry")
    parser.add_argument(
        "--output-dir",
        type=Path,
        default=DEFAULT_REGISTRY_DIR,
        help="Destination directory for registry JSONs",
    )
    parser.add_argument(
        "--labels-dir",
        type=Path,
        default=DEFAULT_LABELS_DIR,
        help="Source directory for gold standard label overrides",
    )
    parser.add_argument(
        "--manifest",
        type=Path,
        default=DEFAULT_CALIBRATION_MANIFEST,
        help="Path to committed calibration manifest JSON",
    )
    parser.add_argument(
        "--catalog",
        type=Path,
        default=DEFAULT_CATALOG_PATH,
        help="Path to cameras_enriched.json for display names and road keys",
    )
    parser.add_argument(
        "--verify",
        action="store_true",
        help="Verify the Hot Set registry (59 priority corridor cameras) without re-exporting",
    )
    parser.add_argument("--no-pilot", action="store_true", help="Omit the built-in Hot Set registry seed")

    args = parser.parse_args()

    if args.verify:
        valid, errors = verify_registry_dir(
            args.output_dir,
            expected_camera_ids=HOT_SET_CAMERA_IDS,
            require_dual=True,
            require_source=True,
            check_drift=True,
            manifest_path=args.manifest,
        )
        if not valid:
            logger.error("Registry verification failed with %d errors:", len(errors))
            for error in errors:
                logger.error("  - %s", error)
            return 1
        logger.info("All %d Hot Set registry files passed verification.", len(HOT_SET_CAMERA_IDS))
        return 0

    build_and_export_registry(
        output_dir=args.output_dir,
        labels_dir=args.labels_dir,
        include_pilot=not args.no_pilot,
        catalog_path=args.catalog,
        manifest_path=args.manifest,
    )

    valid, errors = verify_registry_dir(
        args.output_dir,
        expected_camera_ids=HOT_SET_CAMERA_IDS if not args.no_pilot else None,
        require_dual=not args.no_pilot,
        require_source=not args.no_pilot,
        check_drift=not args.no_pilot,
        manifest_path=args.manifest,
    )
    if not valid:
        logger.error("Post-export verification failed with %d errors:", len(errors))
        for error in errors:
            logger.error("  - %s", error)
        return 1

    return 0


if __name__ == "__main__":
    sys.exit(main())
