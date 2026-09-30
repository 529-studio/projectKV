#!/usr/bin/env python3
"""
Unit tests for road name normalization and corridor grouping keys (Issue #68 / ADR-004 §1).
"""

from __future__ import annotations

import json
import os
import unittest
from pathlib import Path

from normalize_roads import (
    ALIAS_TABLE,
    clean_road_segment,
    extract_road_keys,
    normalize_camera_record,
    normalize_road_name,
    process_cameras,
    split_intersection_roads,
    strip_vietnamese_diacritics,
)


class TestRoadNormalization(unittest.TestCase):
    def test_strip_vietnamese_diacritics(self):
        self.assertEqual(strip_vietnamese_diacritics("Nguyễn Huệ"), "Nguyen Hue")
        self.assertEqual(strip_vietnamese_diacritics("Lê Lợi"), "Le Loi")
        self.assertEqual(strip_vietnamese_diacritics("Điện Biên Phủ"), "Dien Bien Phu")
        self.assertEqual(strip_vietnamese_diacritics("Võ Văn Kiệt"), "Vo Van Kiet")
        self.assertEqual(strip_vietnamese_diacritics("Đường số 10"), "Duong so 10")

    def test_highway_aliases_produce_identical_keys(self):
        # QL1 and Quốc lộ 1 must produce identical road_key
        key_ql1 = normalize_road_name("QL1")
        key_quoc_lo_1 = normalize_road_name("Quốc lộ 1")
        self.assertEqual(key_ql1, "quoc_lo_1")
        self.assertEqual(key_quoc_lo_1, "quoc_lo_1")
        self.assertEqual(key_ql1, key_quoc_lo_1)

        # Variations of QL1
        self.assertEqual(normalize_road_name("QL 1"), "quoc_lo_1")
        self.assertEqual(normalize_road_name("Quốc Lộ 1"), "quoc_lo_1")
        self.assertEqual(normalize_road_name("Quốc Lộ 1A"), "quoc_lo_1")
        self.assertEqual(normalize_road_name("QL1A"), "quoc_lo_1")
        self.assertEqual(normalize_road_name("IDICO-QL1 04"), "quoc_lo_1")

        # Other National Highways
        self.assertEqual(normalize_road_name("QL13"), "quoc_lo_13")
        self.assertEqual(normalize_road_name("Quốc Lộ 13"), "quoc_lo_13")
        self.assertEqual(normalize_road_name("QL 22"), "quoc_lo_22")
        self.assertEqual(normalize_road_name("Quốc Lộ 22"), "quoc_lo_22")
        self.assertEqual(normalize_road_name("QL50"), "quoc_lo_50")
        self.assertEqual(normalize_road_name("Quốc lộ 50"), "quoc_lo_50")
        self.assertEqual(normalize_road_name("QL51"), "quoc_lo_51")
        self.assertEqual(normalize_road_name("Quốc Lộ 51"), "quoc_lo_51")
        self.assertEqual(normalize_road_name("QL1K"), "quoc_lo_1k")
        self.assertEqual(normalize_road_name("Quốc Lộ 1K"), "quoc_lo_1k")

    def test_intersection_names_yield_two_road_keys(self):
        primary, keys = extract_road_keys("Nguyễn Huệ - Lê Lợi")
        self.assertEqual(primary, "nguyen_hue")
        self.assertEqual(keys, ["nguyen_hue", "le_loi"])

        # En-dash delimiter
        primary, keys = extract_road_keys("Ba Tháng Hai – Sư Vạn Hạnh")
        self.assertEqual(primary, "ba_thang_hai")
        self.assertEqual(keys, ["ba_thang_hai", "su_van_hanh"])

        # Em-dash delimiter
        primary, keys = extract_road_keys("Võ Văn Kiệt — An Dương Vương")
        self.assertEqual(primary, "vo_van_kiet")
        self.assertEqual(keys, ["vo_van_kiet", "an_duong_vuong"])

        # Compound without space: QL1-Cty Pouyuen (1)
        primary, keys = extract_road_keys("QL1-Cty Pouyuen (1)")
        self.assertEqual(primary, "quoc_lo_1")
        self.assertEqual(keys, ["quoc_lo_1", "cty_pouyuen"])

    def test_camera_indexing_stripped_but_street_numbers_preserved(self):
        # Camera instance numbers at intersections should be stripped
        primary, keys = extract_road_keys("Võ Văn Kiệt - An Dương Vương 2")
        self.assertEqual(keys, ["vo_van_kiet", "an_duong_vuong"])

        primary, keys = extract_road_keys("Quốc lộ 1 - Rạch Láng Le 1")
        self.assertEqual(keys, ["quoc_lo_1", "rach_lang_le"])

        primary, keys = extract_road_keys("Võ Nguyên Giáp - Đỗ Xuân Hợp (1)")
        self.assertEqual(keys, ["vo_nguyen_giap", "do_xuan_hop"])

        # Numbered streets and routes must PRESERVE their route numbers
        self.assertEqual(normalize_road_name("Đường số 1"), "duong_so_1")
        self.assertEqual(normalize_road_name("Đường số 10"), "duong_so_10")
        self.assertEqual(normalize_road_name("Tỉnh lộ 10"), "tinh_lo_10")
        self.assertEqual(normalize_road_name("TL10"), "tinh_lo_10")
        self.assertEqual(normalize_road_name("Hương Lộ 2"), "huong_lo_2")
        self.assertEqual(normalize_road_name("HL2"), "huong_lo_2")
        self.assertEqual(normalize_road_name("Kênh 4"), "kenh_4")
        self.assertEqual(normalize_road_name("TX25"), "tx25")

    def test_known_urban_arterial_aliases(self):
        self.assertEqual(normalize_road_name("Xa lộ Hà Nội"), "xa_lo_ha_noi")
        self.assertEqual(normalize_road_name("XLHN"), "xa_lo_ha_noi")

        self.assertEqual(normalize_road_name("Võ Văn Kiệt"), "vo_van_kiet")
        self.assertEqual(normalize_road_name("VVK"), "vo_van_kiet")
        self.assertEqual(normalize_road_name("Đại lộ Đông Tây"), "vo_van_kiet")

        self.assertEqual(normalize_road_name("Phạm Văn Đồng"), "pham_van_dong")
        self.assertEqual(normalize_road_name("PVĐ"), "pham_van_dong")
        self.assertEqual(normalize_road_name("PVD"), "pham_van_dong")

        self.assertEqual(normalize_road_name("Điện Biên Phủ"), "dien_bien_phu")
        self.assertEqual(normalize_road_name("ĐBP"), "dien_bien_phu")

        self.assertEqual(normalize_road_name("Cách Mạng Tháng Tám"), "cach_mang_thang_tam")
        self.assertEqual(normalize_road_name("Cách Mạng Tháng 8"), "cach_mang_thang_tam")
        self.assertEqual(normalize_road_name("CMTT"), "cach_mang_thang_tam")

        self.assertEqual(normalize_road_name("Mai Chí Thọ"), "mai_chi_tho")
        self.assertEqual(normalize_road_name("MCT"), "mai_chi_tho")

        self.assertEqual(normalize_road_name("Nguyễn Văn Linh"), "nguyen_van_linh")
        self.assertEqual(normalize_road_name("NVL"), "nguyen_van_linh")

    def test_expressway_tth_handling(self):
        # Trung Lương expressway markers
        self.assertEqual(normalize_road_name("TTH 48.4"), "cao_toc_trung_luong")
        self.assertEqual(normalize_road_name("TTH 10.3"), "cao_toc_trung_luong")
        self.assertEqual(normalize_road_name("Cao tốc Trung Lương"), "cao_toc_trung_luong")

        # Long Thành - Dầu Giây expressway
        self.assertEqual(normalize_road_name("Cao tốc LTDG"), "cao_toc_long_thanh_dau_giay")
        self.assertEqual(normalize_road_name("CT LTDG"), "cao_toc_long_thanh_dau_giay")

    def test_normalize_camera_record(self):
        sample = {
            "id": "test_cam_01",
            "displayName": "Quốc Lộ 1 - Tô Ký",
            "title": "Camera 01",
            "latitude": 10.85,
            "longitude": 106.62,
        }
        res = normalize_camera_record(sample)
        self.assertEqual(res["id"], "test_cam_01")
        self.assertEqual(res["road_key"], "quoc_lo_1")
        self.assertEqual(res["road_keys"], ["quoc_lo_1", "to_ky"])
        # Preserves coordinates and metadata
        self.assertEqual(res["latitude"], 10.85)
        self.assertEqual(res["longitude"], 106.62)

    def test_six_known_duplicate_camera_names_in_dataset(self):
        dataset_path = Path("cameras_enriched.json")
        if not dataset_path.exists():
            dataset_path = Path("../../cameras_enriched.json")
        if not dataset_path.exists():
            self.skipTest("cameras_enriched.json not found in test environment")

        with open(dataset_path, "r", encoding="utf-8") as f:
            cameras = json.load(f)

        enriched, _ = process_cameras(cameras)

        # The 6 known duplicate names identified in ADR-004 §1
        known_duplicates = [
            "Nút giao Ngã sáu Cộng Hòa",
            "Nút giao Công Trường Dân Chủ",
            "Đường hầm (Đầu hầm Tp Thủ Đức)",
            "Nút giao Ngã sáu Nguyễn Tri Phương",
            "Cộng Hòa - Hoàng Hoa Thám",
            "Nút giao Ngã bảy Lý Thái Tổ",
        ]

        for dup_name in known_duplicates:
            matching = [
                c for c in enriched
                if (c.get("displayName") or c.get("title") or "").strip() == dup_name
            ]
            self.assertEqual(
                len(matching),
                2,
                f"Duplicate '{dup_name}' should appear exactly twice in catalog",
            )
            # Both cameras must have distinct IDs
            self.assertNotEqual(matching[0]["id"], matching[1]["id"])
            # Both cameras must have identical stable road_key
            self.assertEqual(matching[0]["road_key"], matching[1]["road_key"])
            self.assertIsNotNone(matching[0]["road_key"])
            self.assertEqual(matching[0]["road_keys"], matching[1]["road_keys"])

    def test_idempotent_processing(self):
        dataset_path = Path("cameras_enriched.json")
        if not dataset_path.exists():
            dataset_path = Path("../../cameras_enriched.json")
        if not dataset_path.exists():
            self.skipTest("cameras_enriched.json not found in test environment")

        with open(dataset_path, "r", encoding="utf-8") as f:
            cameras = json.load(f)

        enriched_1, stats_1 = process_cameras(cameras)
        enriched_2, stats_2 = process_cameras(enriched_1)

        self.assertEqual(stats_1, stats_2)
        json_1 = json.dumps(enriched_1, ensure_ascii=False, indent=2)
        json_2 = json.dumps(enriched_2, ensure_ascii=False, indent=2)
        self.assertEqual(json_1, json_2)


if __name__ == "__main__":
    unittest.main()
