#!/usr/bin/env python3
"""
generate_hex_polygons: Offline generator for H3 hexagonal cell boundary lookup.
Reads cameras_enriched.json, extracts unique H3 cell IDs (res 7 and res 9),
computes 6-vertex boundary coordinates [lat, lng] via h3-py, and outputs
a compact JSON lookup dictionary mapping H3 ID -> [[lat, lng], ...].

Strictly offline: Go server only reads this static JSON, zero H3 math or cgo in Go.
"""

import argparse
import json
import os
import sys

try:
    import h3
    HAS_H3 = True
except ImportError:
    h3 = None
    HAS_H3 = False


def generate_polygons(cameras_path: str, output_path: str, precision: int = 7) -> int:
    if not HAS_H3 or h3 is None:
        raise ImportError("The 'h3' package is required. Install via pip install h3>=4.0.0")
    with open(cameras_path, "r", encoding="utf-8") as f:
        cameras = json.load(f)

    polygons = {}
    for cam in cameras:
        for res_key in ("h3_res7", "h3_res9"):
            h3_id = cam.get(res_key)
            if h3_id and h3_id not in polygons:
                boundary = h3.cell_to_boundary(h3_id)
                # cell_to_boundary in h3 v4 returns tuple of (lat, lng)
                polygons[h3_id] = [
                    [round(lat, precision), round(lng, precision)]
                    for lat, lng in boundary
                ]

    # Write formatted JSON with compact lines per polygon for readability and clean diffs
    with open(output_path, "w", encoding="utf-8") as f:
        f.write("{\n")
        keys = sorted(polygons.keys())
        for i, k in enumerate(keys):
            poly_str = json.dumps(polygons[k])
            comma = "," if i < len(keys) - 1 else ""
            f.write(f'  "{k}": {poly_str}{comma}\n')
        f.write("}\n")

    return len(polygons)


def main():
    parser = argparse.ArgumentParser(description="Generate H3 hexagon boundary lookup JSON")
    parser.add_argument(
        "--cameras",
        default="cameras_enriched.json",
        help="Path to cameras_enriched.json (default: cameras_enriched.json)",
    )
    parser.add_argument(
        "--output",
        default="h3_hex_polygons.json",
        help="Output path for polygon lookup JSON (default: h3_hex_polygons.json)",
    )
    args = parser.parse_args()

    if not os.path.exists(args.cameras):
        print(f"Error: Cameras file not found: {args.cameras}", file=sys.stderr)
        return 1

    if not HAS_H3 or h3 is None:
        print("Error: 'h3' package is required. Install via pip install h3>=4.0.0", file=sys.stderr)
        return 1

    count = generate_polygons(args.cameras, args.output)
    print(f"Successfully generated {count} H3 polygon boundaries -> {args.output}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
