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
import re
import sys
from typing import Any, Dict, List, Optional, Tuple


def parse_phuong_name(full_name: Optional[str]) -> Tuple[Optional[str], Optional[str]]:
    """
    Splits administrative unit prefix from Phường/Xã/Thị trấn name.
    Returns (phuong_type, prefix_less_name).
    If no prefix is detected, returns (None, full_name).
    If full_name is None, returns (None, None).
    """
    if not full_name:
        return None, None
    raw = full_name.strip()
    if not raw:
        return None, None

    m = re.match(r'^(?:Phường|phường)\s+(.+)$', raw)
    if m:
        return "Phường", m.group(1).strip()

    m = re.match(r'^(?:Xã|xã)\s+(.+)$', raw)
    if m:
        return "Xã", m.group(1).strip()

    m = re.match(r'^(?:Thị\s*trấn|thị\s*trấn)\s+(.+)$', raw)
    if m:
        return "Thị trấn", m.group(1).strip()

    return None, raw


def extract_road_fields(display_name: Optional[str], title: Optional[str] = None) -> Tuple[Optional[str], Optional[str]]:
    """
    Extracts road_type and prefix-less road_name from camera displayName (format 'A - B').
    Returns (road_type, road_name).
    """
    raw = (display_name or title or "").strip()
    if not raw:
        return None, None

    parts = re.split(r'\s+[-–—−]\s+', raw)
    first_segment = parts[0].strip() if parts else raw
    if not first_segment:
        return None, None

    # Check known road type prefixes
    m = re.match(r'^(?:Cầu\s+vượt)\s+(.+)$', first_segment, re.IGNORECASE)
    if m:
        return "Cầu vượt", m.group(1).strip()

    m = re.match(r'^(?:Cầu)\s+(.+)$', first_segment, re.IGNORECASE)
    if m:
        return "Cầu", m.group(1).strip()

    m = re.match(r'^(?:Nút\s+giao)\s+(.+)$', first_segment, re.IGNORECASE)
    if m:
        return "Nút giao", m.group(1).strip()

    m = re.match(r'^(?:Vòng\s+xoay)\s+(.+)$', first_segment, re.IGNORECASE)
    if m:
        return "Vòng xoay", m.group(1).strip()

    m = re.match(r'^(?:Cao\s+tốc)\s+(.+)$', first_segment, re.IGNORECASE)
    if m:
        return "Cao tốc", m.group(1).strip()

    m = re.match(r'^(?:Quốc\s*lộ|QL)\s*(.+)$', first_segment, re.IGNORECASE)
    if m:
        return "Quốc lộ", m.group(1).strip()

    m = re.match(r'^(?:Tỉnh\s*lộ|TL)\s*(.+)$', first_segment, re.IGNORECASE)
    if m:
        return "Tỉnh lộ", m.group(1).strip()

    m = re.match(r'^(?:Hương\s*lộ|HL)\s*(.+)$', first_segment, re.IGNORECASE)
    if m:
        return "Hương lộ", m.group(1).strip()

    m = re.match(r'^(?:Xa\s+lộ)\s+(.+)$', first_segment, re.IGNORECASE)
    if m:
        return "Xa lộ", m.group(1).strip()

    m = re.match(r'^(?:Đại\s+lộ)\s+(.+)$', first_segment, re.IGNORECASE)
    if m:
        return "Đại lộ", m.group(1).strip()

    m = re.match(r'^(?:Đường\s+hầm|Hầm)(?:\s+(.+))?$', first_segment, re.IGNORECASE)
    if m:
        sub = (m.group(1) or "").strip()
        return "Hầm", sub if sub else first_segment

    m = re.match(r'^(?:Đường\s+số)\s+(.+)$', first_segment, re.IGNORECASE)
    if m:
        return "Đường", f"số {m.group(1).strip()}"

    m = re.match(r'^(?:Đường)\s+(.+)$', first_segment, re.IGNORECASE)
    if m:
        return "Đường", m.group(1).strip()

    # Default to standard street
    return "Đường", first_segment


# Optional shapely import for high-performance vectorized operations
try:
    from shapely.geometry import Point, shape
    from shapely.strtree import STRtree
    HAS_SHAPELY = True
except ImportError:
    HAS_SHAPELY = False

# Optional h3 import for hexagonal spatial indexing (offline enrichment)
try:
    import h3
    HAS_H3 = True
except ImportError:
    HAS_H3 = False


def parse_h3_resolutions(res_str: Optional[str]) -> List[int]:
    """
    Parse comma-separated H3 resolution integers (e.g. '7,9').
    Returns an empty list if res_str is empty or represents disabling ('none', 'false', 'off').
    """
    if not res_str or res_str.strip().lower() in ("none", "false", "off", "0", ""):
        return []
    resolutions: List[int] = []
    for item in res_str.split(","):
        item = item.strip()
        if not item:
            continue
        try:
            val = int(item)
            if not (0 <= val <= 15):
                raise ValueError(f"Resolution must be between 0 and 15, got {val}")
            resolutions.append(val)
        except ValueError as err:
            raise ValueError(f"Invalid H3 resolution value '{item}': {err}")
    return sorted(list(set(resolutions)))


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
    boundaries: Optional[List[BoundaryFeature]] = None,
    h3_resolutions: Optional[List[int]] = None,
) -> Tuple[List[Dict[str, Any]], int, int]:
    """
    Enrich camera records with phuong_id, phuong_name, and H3 cell IDs.
    Returns (enriched_cameras, matched_count, unmatched_count).
    """
    if h3_resolutions and not HAS_H3:
        raise ImportError(
            "The 'h3' package is required to compute H3 cell IDs. "
            "Install it via 'pip install -r tools/geo-enrich/requirements.txt' or 'pip install h3>=4.0.0'."
        )

    enriched: List[Dict[str, Any]] = []
    matched = 0
    unmatched = 0

    for cam in cameras:
        # Shallow copy dictionary while preserving keys
        item = dict(cam)
        lat = item.get("latitude")
        lon = item.get("longitude")

        phuong_id = None
        phuong_type = None
        phuong_name = None

        road_type, road_name = extract_road_fields(item.get("displayName"), item.get("title"))

        valid_coords = False
        px = py = 0.0

        if lat is not None and lon is not None:
            try:
                px = float(lon)
                py = float(lat)
                valid_coords = True
            except (ValueError, TypeError):
                valid_coords = False

        if boundaries and valid_coords:
            for b in boundaries:
                if b.contains_point(px, py):
                    phuong_id = b.phuong_id
                    phuong_type, phuong_name = parse_phuong_name(b.phuong_name)
                    break

        if phuong_id is not None or phuong_name is not None:
            matched += 1
        else:
            unmatched += 1

        # Strict specification: outside or missing -> null, not fabricated values
        item["phuong_id"] = phuong_id
        item["phuong_type"] = phuong_type
        item["phuong_name"] = phuong_name
        item["road_type"] = road_type
        item["road_name"] = road_name

        # Compute H3 cell IDs if requested
        if h3_resolutions:
            for res in h3_resolutions:
                field_name = f"h3_res{res}"
                if valid_coords:
                    # h3.latlng_to_cell expects (lat, lng, resolution)
                    item[field_name] = h3.latlng_to_cell(py, px, res)
                else:
                    item[field_name] = None

        enriched.append(item)

    return enriched, matched, unmatched


def write_unmatched_report(cameras: List[Dict[str, Any]], report_path: str) -> None:
    """Generate CSV report of unmatched cameras with reasons."""
    import csv

    report_dir = os.path.dirname(report_path)
    if report_dir:
        os.makedirs(report_dir, exist_ok=True)

    with open(report_path, "w", newline="", encoding="utf-8") as f:
        writer = csv.writer(f)
        writer.writerow(["id", "title", "displayName", "latitude", "longitude", "zone", "reason"])
        for c in cameras:
            if not c.get("phuong_name"):
                lat = c.get("latitude")
                lon = c.get("longitude")
                if lat is None or lon is None:
                    reason = "Missing coordinates in upstream catalog"
                else:
                    reason = "Outside administrative boundary polygon extent (e.g. Long An expressway corridor)"
                writer.writerow([
                    c.get("id", ""),
                    c.get("title", ""),
                    c.get("displayName", ""),
                    lat if lat is not None else "",
                    lon if lon is not None else "",
                    c.get("zone", ""),
                    reason
                ])


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
    parser.add_argument(
        "--unmatched-report",
        default="tools/geo-enrich/unmatched_cameras.csv",
        help="Path for unmatched cameras CSV report (default: tools/geo-enrich/unmatched_cameras.csv)"
    )
    parser.add_argument(
        "--h3-resolutions",
        default="7,9",
        help="Comma-separated H3 resolutions to compute (default: '7,9'; pass 'none' or '' to disable)"
    )

    args = parser.parse_args()

    try:
        h3_res = parse_h3_resolutions(args.h3_resolutions)
    except ValueError as err:
        print(f"Error: {err}", file=sys.stderr)
        return 1

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

    enriched, matched, unmatched = enrich_cameras(cameras, boundaries, h3_resolutions=h3_res)

    output_path = args.cameras if args.in_place else args.output
    output_dir = os.path.dirname(output_path)
    if output_dir:
        os.makedirs(output_dir, exist_ok=True)

    with open(output_path, "w", encoding="utf-8") as f:
        # indent=2 and ensure_ascii=False for UTF-8 Vietnamese text and reproducible idempotency
        json.dump(enriched, f, indent=2, ensure_ascii=False)
        f.write("\n")

    if args.unmatched_report:
        write_unmatched_report(enriched, args.unmatched_report)

    print("==================================================")
    print(" geo-enrich: Batch Enrichment Completed")
    print(f" - Total cameras:   {len(cameras)}")
    print(f" - Matched Phường:  {matched}")
    print(f" - Unmatched/Null:  {unmatched}")
    if h3_res:
        print(f" - H3 resolutions:  {h3_res}")
    print(f" - Output saved to: {output_path}")
    if args.unmatched_report:
        print(f" - Unmatched report: {args.unmatched_report}")
    print("==================================================")
    return 0


if __name__ == "__main__":
    sys.exit(main())
