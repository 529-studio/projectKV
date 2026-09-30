#!/usr/bin/env python3
"""
Road Name Normalization Tool for Camera View (projectKV).

Computes stable, canonical road_key and road_keys for corridor grouping,
addressing ADR-004 §1 data anomalies:
- Normalizes aliases: QL1 and Quốc lộ 1 produce identical road_key ("quoc_lo_1").
- Decomposes intersection camera titles ("A - B") into multiple road keys.
- Strips camera instance numbering while preserving numbered highway/street designations.
- Handles known duplicate camera titles deterministically.
"""

from __future__ import annotations

import argparse
import json
import os
import re
import sys
import unicodedata
from typing import Any, Dict, List, Optional, Tuple

# Canonical alias mapping table (slug -> canonical road_key)
ALIAS_TABLE: Dict[str, str] = {
    # National Highways (Quốc lộ)
    "ql1": "quoc_lo_1",
    "ql_1": "quoc_lo_1",
    "quoc_lo_1": "quoc_lo_1",
    "ql1a": "quoc_lo_1",
    "ql_1a": "quoc_lo_1",
    "quoc_lo_1a": "quoc_lo_1",
    "idico_ql1": "quoc_lo_1",
    "idico_ql_1": "quoc_lo_1",
    "ql1k": "quoc_lo_1k",
    "ql_1k": "quoc_lo_1k",
    "quoc_lo_1k": "quoc_lo_1k",
    "ql13": "quoc_lo_13",
    "ql_13": "quoc_lo_13",
    "quoc_lo_13": "quoc_lo_13",
    "ql22": "quoc_lo_22",
    "ql_22": "quoc_lo_22",
    "quoc_lo_22": "quoc_lo_22",
    "ql50": "quoc_lo_50",
    "ql_50": "quoc_lo_50",
    "quoc_lo_50": "quoc_lo_50",
    "ql51": "quoc_lo_51",
    "ql_51": "quoc_lo_51",
    "quoc_lo_51": "quoc_lo_51",

    # Provincial & Rural Routes (Tỉnh lộ / Hương lộ)
    "tl10": "tinh_lo_10",
    "tl_10": "tinh_lo_10",
    "tinh_lo_10": "tinh_lo_10",
    "tl8": "tinh_lo_8",
    "tl_8": "tinh_lo_8",
    "tinh_lo_8": "tinh_lo_8",
    "hl2": "huong_lo_2",
    "hl_2": "huong_lo_2",
    "huong_lo_2": "huong_lo_2",

    # Expressways (Cao tốc)
    "cao_toc_ltdg": "cao_toc_long_thanh_dau_giay",
    "ct_ltdg": "cao_toc_long_thanh_dau_giay",
    "cao_toc_long_thanh_dau_giay": "cao_toc_long_thanh_dau_giay",
    "cao_toc_tp_hcm_long_thanh_dau_giay": "cao_toc_long_thanh_dau_giay",
    "cao_toc_trung_luong": "cao_toc_trung_luong",
    "cao_toc_tp_hcm_trung_luong": "cao_toc_trung_luong",
    "cao_toc_thanh_pho_ho_chi_minh_trung_luong": "cao_toc_trung_luong",
    "duong_dan_cao_toc_thanh_pho_ho_chi_minh": "cao_toc_trung_luong",

    # Major Arterials & Urban Corridors
    "xlhn": "xa_lo_ha_noi",
    "xa_lo_ha_noi": "xa_lo_ha_noi",
    "vvk": "vo_van_kiet",
    "dai_lo_dong_tay": "vo_van_kiet",
    "vo_van_kiet": "vo_van_kiet",
    "pvd": "pham_van_dong",
    "pham_van_dong": "pham_van_dong",
    "dbp": "dien_bien_phu",
    "dien_bien_phu": "dien_bien_phu",
    "cmtt": "cach_mang_thang_tam",
    "cach_mang_thang_8": "cach_mang_thang_tam",
    "cach_mang_thang_tam": "cach_mang_thang_tam",
    "mct": "mai_chi_tho",
    "mai_chi_tho": "mai_chi_tho",
    "nvl": "nguyen_van_linh",
    "nguyen_van_linh": "nguyen_van_linh",
    "ntmk": "nguyen_thi_minh_khai",
    "nguyen_thi_minh_khai": "nguyen_thi_minh_khai",
    "nkkn": "nam_ky_khoi_nghia",
    "nam_ky_khoi_nghia": "nam_ky_khoi_nghia",
    "thd": "tran_hung_dao",
    "tran_hung_dao": "tran_hung_dao",
}


def strip_vietnamese_diacritics(text: str) -> str:
    """Strips Vietnamese diacritics and converts to ASCII representation."""
    if not text:
        return ""
    text = text.replace("đ", "d").replace("Đ", "D")
    text = unicodedata.normalize("NFD", text)
    text = "".join(c for c in text if unicodedata.category(c) != "Mn")
    return unicodedata.normalize("NFC", text)


def clean_road_segment(segment: str) -> str:
    """
    Cleans metadata suffixes and camera indexing numbers from a road segment.
    Preserves numbers that identify routes (e.g. QL1, Đường số 1, Tỉnh lộ 10, Kênh 4, Tháng 8).
    """
    s = segment.strip()
    if not s:
        return ""

    # 1. Remove bracketed camera annotations: e.g. (1), (2), (Cổng), (Hướng An Lạc)
    s = re.sub(r"\s*\([^)]*\)$", "", s).strip()

    # 2. Check if trailing number is part of a designated route or date (e.g. 'Đường số 1', 'Tháng 8', 'Kênh 4')
    no_acc = strip_vietnamese_diacritics(s).lower()
    is_route_number = bool(
        re.search(
            r"\b(quoc\s*lo|ql|tinh\s*lo|tl|huong\s*lo|hl|duong\s*so|so|duong|thang|kenh|tx|km)\s+\d+[a-z]?$",
            no_acc,
        )
    )

    # 3. Strip trailing camera instance numbers (e.g. 'An Dương Vương 2' -> 'An Dương Vương')
    # only when not part of a numbered route or date.
    if not is_route_number:
        m = re.search(r"^(.*?)\s+\d+$", s)
        if m and len(m.group(1).strip()) >= 3:
            s = m.group(1).strip()

    return s


def normalize_road_name(name: str) -> Optional[str]:
    """
    Normalizes a single road segment into a stable, canonical road_key.
    Returns None if the segment is empty or invalid.
    """
    cleaned = clean_road_segment(name)
    if not cleaned:
        return None

    no_acc = strip_vietnamese_diacritics(cleaned).lower()

    # 1. Special Case: Toll plaza on QL1
    if "idico-ql1" in no_acc or "idico_ql1" in no_acc or "idico-ql 1" in no_acc:
        return "quoc_lo_1"

    # 2. Special Case: Trung Lương Expressway markers (TTH 48.4, TTH-CV...)
    if re.match(r"^tth\s*[\d.]+", no_acc) or re.match(r"^tth-cv", no_acc):
        return "cao_toc_trung_luong"

    # 3. National Highways: QL <N> / Quốc lộ <N>
    m_ql = re.match(r"^(?:quoc\s*lo|ql)\s*(\d+[a-z]?)$", no_acc)
    if m_ql:
        key = f"quoc_lo_{m_ql.group(1)}"
        return ALIAS_TABLE.get(key, key)

    # 4. Provincial Routes: Tỉnh lộ <N> / TL <N>
    m_tl = re.match(r"^(?:tinh\s*lo|tl)\s*(\d+[a-z]?)$", no_acc)
    if m_tl:
        key = f"tinh_lo_{m_tl.group(1)}"
        return ALIAS_TABLE.get(key, key)

    # 5. Rural Routes: Hương lộ <N> / HL <N>
    m_hl = re.match(r"^(?:huong\s*lo|hl)\s*(\d+[a-z]?)$", no_acc)
    if m_hl:
        key = f"huong_lo_{m_hl.group(1)}"
        return ALIAS_TABLE.get(key, key)

    # 6. Numbered Streets: Đường số <N> / Số <N>
    m_so = re.match(r"^(?:duong\s+)?so\s+(\d+[a-z]?)$", no_acc)
    if m_so:
        return f"duong_so_{m_so.group(1)}"

    # 7. Strip generic prefix 'đường ' when followed by a named street
    # (e.g. 'đường Nguyễn Huệ' -> 'nguyen_hue'), but keep short letters like 'đường D'
    m_duong = re.match(r"^duong\s+([a-z0-9_].*)$", no_acc)
    if m_duong:
        sub = m_duong.group(1).strip()
        if len(sub) > 2 and not sub.startswith("so "):
            no_acc = sub

    # 8. Standardize slug
    slug = re.sub(r"[^a-z0-9]+", "_", no_acc).strip("_")
    if not slug:
        return None

    # 9. Lookup in canonical alias table
    return ALIAS_TABLE.get(slug, slug)


def split_intersection_roads(raw_name: str) -> List[str]:
    """
    Decomposes an intersection camera name (e.g. 'A - B') into individual road names.
    Supports standard dashes (-), en-dashes (–), em-dashes (—), and minus signs (−).
    """
    if not raw_name:
        return []

    s = raw_name.strip()
    # Normalize dash characters
    s = re.sub(r"[–—−]", "-", s)

    # Split on dash with surrounding whitespace, or boundary dashes
    parts = re.split(r"\s+-\s*|\s*-\s+", s)

    # Handle unspaced compound case: e.g. QL1-Cty Pouyuen
    if len(parts) == 1:
        m = re.match(r"^(QL\s*\d+[A-Z]?|Quốc\s*Lộ\s*\d+[A-Z]?)-(.*)$", s, re.IGNORECASE)
        if m:
            parts = [m.group(1), m.group(2)]

    return [p.strip() for p in parts if p.strip()]


def extract_road_keys(camera_name: str) -> Tuple[Optional[str], List[str]]:
    """
    Extracts the primary road_key and list of road_keys from a camera name.
    Preserves road order and removes duplicate keys.
    """
    if not camera_name:
        return None, []

    segments = split_intersection_roads(camera_name)
    keys: List[str] = []
    seen = set()

    for seg in segments:
        key = normalize_road_name(seg)
        if key and key not in seen:
            seen.add(key)
            keys.append(key)

    primary_key = keys[0] if keys else None
    return primary_key, keys


def normalize_camera_record(cam: Dict[str, Any]) -> Dict[str, Any]:
    """
    Enriches a camera record with road_key and road_keys fields.
    Preserves all existing fields and ordering.
    """
    name = (cam.get("displayName") or cam.get("title") or "").strip()
    primary_key, keys = extract_road_keys(name)

    # Build updated record with road_key and road_keys
    updated = dict(cam)
    updated["road_key"] = primary_key
    updated["road_keys"] = keys
    return updated


def process_cameras(cameras: List[Dict[str, Any]]) -> Tuple[List[Dict[str, Any]], Dict[str, Any]]:
    """Processes a catalog of cameras, returning enriched cameras and statistics."""
    enriched = [normalize_camera_record(c) for c in cameras]

    total = len(enriched)
    with_key = sum(1 for c in enriched if c.get("road_key") is not None)
    with_multi = sum(1 for c in enriched if len(c.get("road_keys", [])) > 1)
    unique_primary = len({c["road_key"] for c in enriched if c.get("road_key")})
    all_keys = {k for c in enriched for k in c.get("road_keys", [])}

    stats = {
        "total_cameras": total,
        "cameras_with_road_key": with_key,
        "cameras_with_multiple_keys": with_multi,
        "unique_primary_road_keys": unique_primary,
        "unique_total_road_keys": len(all_keys),
    }
    return enriched, stats


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Normalize road names in camera catalog into stable road_key."
    )
    parser.add_argument(
        "--input",
        default="cameras_enriched.json",
        help="Path to input camera JSON (default: cameras_enriched.json)",
    )
    parser.add_argument(
        "--output",
        default="cameras_enriched.json",
        help="Path to output camera JSON (default: cameras_enriched.json)",
    )
    parser.add_argument(
        "--in-place",
        action="store_true",
        help="Overwrite input file in place",
    )
    args = parser.parse_args()

    input_path = args.input
    output_path = args.input if args.in_place else args.output

    if not os.path.exists(input_path):
        print(f"Error: input file not found: {input_path}", file=sys.stderr)
        return 1

    with open(input_path, "r", encoding="utf-8") as f:
        cameras = json.load(f)

    enriched_cameras, stats = process_cameras(cameras)

    # Write formatted JSON (2-space indent, UTF-8, trailing newline for Git idempotency)
    tmp_output = f"{output_path}.tmp"
    with open(tmp_output, "w", encoding="utf-8") as f:
        json.dump(enriched_cameras, f, ensure_ascii=False, indent=2)
        f.write("\n")

    os.replace(tmp_output, output_path)

    print("========================================================")
    print("      ROAD NAME NORMALIZATION COMPLETE (ADR-004 A6)     ")
    print("========================================================")
    print(f" - Input file:                  {input_path}")
    print(f" - Output file:                 {output_path}")
    print(f" - Total cameras:               {stats['total_cameras']}")
    print(f" - Cameras with road_key:       {stats['cameras_with_road_key']}")
    print(f" - Intersection cameras (>1):   {stats['cameras_with_multiple_keys']}")
    print(f" - Distinct primary road keys:  {stats['unique_primary_road_keys']}")
    print(f" - Distinct total road keys:    {stats['unique_total_road_keys']}")
    print("========================================================")
    return 0


if __name__ == "__main__":
    sys.exit(main())
