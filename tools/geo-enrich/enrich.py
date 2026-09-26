#!/usr/bin/env python3
"""
geo-enrich: Batch point-in-polygon Phường enrichment tool for projectKV traffic cameras.

Joins camera (lat, lng) against post-reform Phường boundary polygons (GeoJSON).
Produces enriched camera catalog with `phuong_id` and `phuong_name` fields.
Cameras outside all boundary polygons are assigned null (never fabricated).
The operation is strictly idempotent: running multiple times yields byte-identical output.
"""

import argparse
import json
import os
import sys
from typing import Any, Dict, List, Optional, Tuple

# Optional shapely import for high-performance vectorized operations
try:
    from shapely.geometry import Point, shape
    from shapely.strtree import STRtree
    HAS_SHAPELY = True
except ImportError:
    HAS_SHAPELY = False


def point_in_ring(px: float, py: float, ring: List[List[float]]) -> bool:
    """Ray casting algorithm to determine if point (px, py) is inside a polygon ring."""
    inside = False
    n = len(ring)
    if n < 3:
        return False
    for i in range(n):
        x1, y1 = ring[i][0], ring[i][1]
        x2, y2 = ring[(i + 1) % n][0], ring[(i + 1) % n][1]
        if (y1 > py) != (y2 > py):
            x_intersect = x1 + (py - y1) * (x2 - x1) / (y2 - y1)
            if px < x_intersect:
                inside = not inside
    return inside


def point_in_polygon(px: float, py: float, coordinates: List[Any]) -> bool:
    """Check if point (px, py) is inside polygon coordinates (exterior ring and holes)."""
    if not coordinates:
        return False
    # Must be inside exterior boundary ring
    if not point_in_ring(px, py, coordinates[0]):
        return False
    # Must not be inside any interior ring (hole)
    for hole in coordinates[1:]:
        if point_in_ring(px, py, hole):
            return False
    return True


def compute_bbox(coords: Any) -> Tuple[float, float, float, float]:
    """Compute bounding box (min_x, min_y, max_x, max_y) for nested coordinate lists."""
    min_x = min_y = float("inf")
    max_x = max_y = float("-inf")

    def _traverse(node: Any):
        nonlocal min_x, min_y, max_x, max_y
        if isinstance(node, (list, tuple)):
            if len(node) >= 2 and isinstance(node[0], (int, float)) and isinstance(node[1], (int, float)):
                x, y = float(node[0]), float(node[1])
                if x < min_x:
                    min_x = x
                if x > max_x:
                    max_x = x
                if y < min_y:
                    min_y = y
                if y > max_y:
                    max_y = y
            else:
                for child in node:
                    _traverse(child)

    _traverse(coords)
    return min_x, min_y, max_x, max_y


class BoundaryFeature:
    """Parsed boundary polygon feature with fast bounding box pre-filter."""

    def __init__(self, feature: Dict[str, Any], id_prop: Optional[str] = None, name_prop: Optional[str] = None):
        props = feature.get("properties") or {}
        self.phuong_id = self._extract_id(feature, props, id_prop)
        self.phuong_name = self._extract_name(props, name_prop)
        self.geometry = feature.get("geometry") or {}
        self.geom_type = self.geometry.get("type", "")
        self.coords = self.geometry.get("coordinates", [])
        self.bbox = compute_bbox(self.coords) if self.coords else (0, 0, 0, 0)
        self.shapely_geom = shape(self.geometry) if HAS_SHAPELY and self.geometry else None

    @staticmethod
    def _extract_id(feature: Dict[str, Any], props: Dict[str, Any], override_key: Optional[str]) -> Optional[str]:
        if override_key and override_key in props:
            return str(props[override_key])
        for key in ["phuong_id", "id", "code", "admin_code", "ma_phuong", "ward_id", "GID_3", "ISO_3"]:
            if key in props and props[key] is not None:
                return str(props[key])
        if "id" in feature and feature["id"] is not None:
            return str(feature["id"])
        return None

    @staticmethod
    def _extract_name(props: Dict[str, Any], override_key: Optional[str]) -> Optional[str]:
        if override_key and override_key in props:
            return str(props[override_key])
        for key in ["phuong_name", "name", "ten_phuong", "ten", "ward_name", "NAME_3", "VARNAME_3"]:
            if key in props and props[key] is not None:
                return str(props[key])
        return None

    def contains_point(self, px: float, py: float) -> bool:
        """Check point containment (px = longitude, py = latitude)."""
        # Bbox fast reject
        min_x, min_y, max_x, max_y = self.bbox
        if not (min_x <= px <= max_x and min_y <= py <= max_y):
            return False

        if HAS_SHAPELY and self.shapely_geom:
            return bool(self.shapely_geom.contains(Point(px, py)))

        if self.geom_type == "Polygon":
            return point_in_polygon(px, py, self.coords)
        elif self.geom_type == "MultiPolygon":
            for poly in self.coords:
                if point_in_polygon(px, py, poly):
                    return True
            return False
        return False


def load_boundaries(path: str, id_prop: Optional[str] = None, name_prop: Optional[str] = None) -> List[BoundaryFeature]:
    """Load and parse GeoJSON boundary features."""
    if not os.path.exists(path):
        raise FileNotFoundError(f"Boundary file not found: {path}")

    with open(path, "r", encoding="utf-8") as f:
        data = json.load(f)

    raw_features = []
    if data.get("type") == "FeatureCollection":
        raw_features = data.get("features", [])
    elif data.get("type") == "Feature":
        raw_features = [data]
    else:
        raise ValueError("Invalid GeoJSON: expected FeatureCollection or Feature")

    features = []
    for f in raw_features:
        if f.get("geometry"):
            features.append(BoundaryFeature(f, id_prop, name_prop))
    return features


def enrich_cameras(
    cameras: List[Dict[str, Any]],
    boundaries: Optional[List[BoundaryFeature]] = None
) -> Tuple[List[Dict[str, Any]], int, int]:
    """
    Enrich camera records with phuong_id and phuong_name.
    Returns (enriched_cameras, matched_count, unmatched_count).
    """
    enriched: List[Dict[str, Any]] = []
    matched = 0
    unmatched = 0

    for cam in cameras:
        # Shallow copy dictionary while preserving keys
        item = dict(cam)
        lat = item.get("latitude")
        lon = item.get("longitude")

        phuong_id = None
        phuong_name = None

        if boundaries and lat is not None and lon is not None:
            try:
                px = float(lon)
                py = float(lat)
                for b in boundaries:
                    if b.contains_point(px, py):
                        phuong_id = b.phuong_id
                        phuong_name = b.phuong_name
                        break
            except (ValueError, TypeError):
                pass

        if phuong_id is not None or phuong_name is not None:
            matched += 1
        else:
            unmatched += 1

        # Strict specification: outside or missing -> null, not fabricated values
        item["phuong_id"] = phuong_id
        item["phuong_name"] = phuong_name
        enriched.append(item)

    return enriched, matched, unmatched


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Enrich projectKV camera catalog with post-reform Phường boundaries (point-in-polygon)."
    )
    parser.add_argument(
        "--cameras",
        default="cameras_all.json",
        help="Path to camera catalog JSON file (default: cameras_all.json)"
    )
    parser.add_argument(
        "--boundaries",
        default="tools/geo-enrich/data/phuong_boundaries.geojson",
        help="Path to Phường boundary GeoJSON file"
    )
    parser.add_argument(
        "--output",
        default="cameras_enriched.json",
        help="Path for output enriched JSON file (default: cameras_enriched.json)"
    )
    parser.add_argument(
        "--id-prop",
        default=None,
        help="Explicit GeoJSON property name for Phường ID"
    )
    parser.add_argument(
        "--name-prop",
        default=None,
        help="Explicit GeoJSON property name for Phường Name"
    )
    parser.add_argument(
        "--in-place",
        action="store_true",
        help="Overwrite input camera file directly instead of writing to --output"
    )

    args = parser.parse_args()

    if not os.path.exists(args.cameras):
        print(f"Error: Cameras file not found: {args.cameras}", file=sys.stderr)
        return 1

    with open(args.cameras, "r", encoding="utf-8") as f:
        cameras = json.load(f)

    if not isinstance(cameras, list):
        print("Error: Cameras file must be a JSON array", file=sys.stderr)
        return 1

    boundaries: Optional[List[BoundaryFeature]] = None
    if os.path.exists(args.boundaries):
        print(f"Loading boundary polygons from: {args.boundaries}")
        boundaries = load_boundaries(args.boundaries, args.id_prop, args.name_prop)
        print(f"Loaded {len(boundaries)} boundary features (Shapely acceleration: {HAS_SHAPELY})")
    else:
        print(f"Notice: Boundary file not found at '{args.boundaries}'.", file=sys.stderr)
        print("All cameras will be enriched with null phuong_id/phuong_name per specification.", file=sys.stderr)

    enriched, matched, unmatched = enrich_cameras(cameras, boundaries)

    output_path = args.cameras if args.in_place else args.output
    output_dir = os.path.dirname(output_path)
    if output_dir:
        os.makedirs(output_dir, exist_ok=True)

    with open(output_path, "w", encoding="utf-8") as f:
        # indent=2 and ensure_ascii=False for UTF-8 Vietnamese text and reproducible idempotency
        json.dump(enriched, f, indent=2, ensure_ascii=False)
        f.write("\n")

    print("==================================================")
    print(" geo-enrich: Batch Enrichment Completed")
    print(f" - Total cameras:   {len(cameras)}")
    print(f" - Matched Phường:  {matched}")
    print(f" - Unmatched/Null:  {unmatched}")
    print(f" - Output saved to: {output_path}")
    print("==================================================")
    return 0


if __name__ == "__main__":
    sys.exit(main())
