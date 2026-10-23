#!/usr/bin/env python3
"""
auto_harvest_test.py — Unit test suite for Standalone Automated Harvester CLI.

Part of Sprint 7 Issue #263 (S7-T7c) / Kanban [KV-11].
Tests argument parsing, camera scoping, weather resolution, JPEG validation,
PCU calculation, Draft-07 label schema compliance, and dry-run execution.
"""

from datetime import datetime, timezone, timedelta
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch, MagicMock

from tools.eval.auto_harvest import (
    parse_args,
    filter_cameras,
    is_valid_jpeg,
    determine_time_of_day,
    determine_waterline_depth,
    resolve_weather,
    calculate_traffic_severity,
    synthesize_label,
    validate_label_manifest,
    run_pipeline,
    MIN_JPEG_SIZE,
    DEFAULT_SCHEMA_PATH,
)

REPO_ROOT = Path(__file__).resolve().parent.parent.parent


class TestAutoHarvest(unittest.TestCase):
    def setUp(self):
        self.mock_catalog = [
            # Hotset camera: online, 2 road keys, includes vo_van_kiet
            {
                "id": "cam_hotset_1",
                "displayName": "Võ Văn Kiệt - Ký Con",
                "isOnline": True,
                "status": "UP",
                "isGhost": False,
                "road_keys": ["vo_van_kiet", "ky_con"],
                "base_capacity": 45.0,
            },
            # Non-hotset camera (1 road key)
            {
                "id": "cam_midblock_1",
                "displayName": "Võ Văn Kiệt Midblock",
                "isOnline": True,
                "status": "UP",
                "isGhost": False,
                "road_keys": ["vo_van_kiet"],
                "base_capacity": 45.0,
            },
            # Flood-prone corridor camera: ung_van_khiem
            {
                "id": "cam_flood_1",
                "displayName": "Ung Văn Khiêm - D2",
                "isOnline": True,
                "status": "UP",
                "isGhost": False,
                "road_keys": ["ung_van_khiem", "nguyen_gia_tri"],
                "base_capacity": 25.0,
            },
            # Specific flood chokepoint camera
            {
                "id": "cam_chokepoint_99",
                "displayName": "Chokepoint Spot",
                "isOnline": True,
                "status": "UP",
                "isGhost": False,
                "road_keys": ["other_street"],
                "base_capacity": 20.0,
            },
            # Offline camera (should be excluded)
            {
                "id": "cam_offline_1",
                "displayName": "Offline Cam",
                "isOnline": False,
                "status": "NOT_IMAGE",
                "isGhost": False,
                "road_keys": ["vo_van_kiet", "ky_con"],
                "base_capacity": 45.0,
            },
            # Ghost camera (should be excluded)
            {
                "id": "cam_ghost_1",
                "displayName": "Ghost Cam",
                "isOnline": True,
                "status": "UP",
                "isGhost": True,
                "road_keys": ["vo_van_kiet", "ky_con"],
                "base_capacity": 45.0,
            },
        ]
        self.mock_chokepoints = {"cam_chokepoint_99"}

    # 1. Argument Parsing Tests
    def test_parse_args_defaults(self):
        args = parse_args([])
        self.assertEqual(args.trigger, "now")
        self.assertEqual(args.scope, "hotset")
        self.assertEqual(args.weather, "auto")
        self.assertFalse(args.upload_r2)
        self.assertFalse(args.validate)
        self.assertFalse(args.dry_run)

    def test_parse_args_rain_trigger_infers_flood_prone(self):
        args = parse_args(["--trigger", "rain"])
        self.assertEqual(args.trigger, "rain")
        self.assertEqual(args.scope, "flood-prone")

    def test_parse_args_explicit_scope_overrides_trigger(self):
        args = parse_args(["--trigger", "rain", "--scope", "all"])
        self.assertEqual(args.trigger, "rain")
        self.assertEqual(args.scope, "all")

    # 2. Camera Filtering Tests
    def test_filter_cameras_hotset(self):
        result = filter_cameras(self.mock_catalog, "hotset", self.mock_chokepoints)
        self.assertEqual(len(result), 1)
        self.assertEqual(result[0]["id"], "cam_hotset_1")

    def test_filter_cameras_flood_prone(self):
        result = filter_cameras(self.mock_catalog, "flood-prone", self.mock_chokepoints)
        ids = {c["id"] for c in result}
        # cam_flood_1 (ung_van_khiem) + cam_chokepoint_99 (chokepoints) + cam_hotset_1 (vo_van_kiet is in flood_prone too)
        self.assertIn("cam_flood_1", ids)
        self.assertIn("cam_chokepoint_99", ids)
        self.assertNotIn("cam_offline_1", ids)
        self.assertNotIn("cam_ghost_1", ids)

    def test_filter_cameras_all_excludes_offline_and_ghost(self):
        result = filter_cameras(self.mock_catalog, "all", self.mock_chokepoints)
        ids = {c["id"] for c in result}
        self.assertEqual(len(result), 4)
        self.assertNotIn("cam_offline_1", ids)
        self.assertNotIn("cam_ghost_1", ids)

    def test_filter_cameras_invalid_scope(self):
        with self.assertRaises(ValueError):
            filter_cameras(self.mock_catalog, "unknown_scope")

    # 3. Weather Resolution Tests
    @patch("tools.eval.auto_harvest.urllib.request.urlopen")
    def test_resolve_weather_auto_dry(self, mock_urlopen):
        mock_resp = MagicMock()
        mock_resp.read.return_value = json.dumps({
            "current": {"rain": 0.0, "precipitation": 0.0, "weather_code": 1}
        }).encode("utf-8")
        mock_urlopen.return_value.__enter__.return_value = mock_resp

        weather, rain_mm, wmo = resolve_weather("auto")
        self.assertEqual(weather, "dry")
        self.assertEqual(rain_mm, 0.0)
        self.assertEqual(wmo, 1)

    @patch("tools.eval.auto_harvest.urllib.request.urlopen")
    def test_resolve_weather_auto_rain(self, mock_urlopen):
        mock_resp = MagicMock()
        mock_resp.read.return_value = json.dumps({
            "current": {"rain": 2.4, "precipitation": 2.4, "weather_code": 61}
        }).encode("utf-8")
        mock_urlopen.return_value.__enter__.return_value = mock_resp

        weather, rain_mm, wmo = resolve_weather("auto")
        self.assertEqual(weather, "rain")
        self.assertEqual(rain_mm, 2.4)
        self.assertEqual(wmo, 61)

    @patch("tools.eval.auto_harvest.urllib.request.urlopen")
    def test_resolve_weather_auto_heavy_rain(self, mock_urlopen):
        mock_resp = MagicMock()
        mock_resp.read.return_value = json.dumps({
            "current": {"rain": 7.5, "precipitation": 7.5, "weather_code": 65}
        }).encode("utf-8")
        mock_urlopen.return_value.__enter__.return_value = mock_resp

        weather, rain_mm, wmo = resolve_weather("auto")
        self.assertEqual(weather, "heavy_rain")
        self.assertEqual(rain_mm, 7.5)

    @patch("tools.eval.auto_harvest.urllib.request.urlopen")
    def test_resolve_weather_auto_network_fallback(self, mock_urlopen):
        mock_urlopen.side_effect = Exception("Network unreachable")
        weather, rain_mm, wmo = resolve_weather("auto")
        self.assertEqual(weather, "dry")
        self.assertEqual(rain_mm, 0.0)

    def test_resolve_weather_explicit(self):
        weather, rain_mm, wmo = resolve_weather("heavy_rain")
        self.assertEqual(weather, "heavy_rain")
        self.assertEqual(rain_mm, 5.0)

    # 4. JPEG Validation Tests
    def test_is_valid_jpeg(self):
        # Valid JPEG: >= 20,480 bytes, SOI header 0xFF, 0xD8
        valid_bytes = b"\xff\xd8" + b"\x00" * (MIN_JPEG_SIZE)
        self.assertTrue(is_valid_jpeg(valid_bytes))

        # Too short (< 20KB)
        short_bytes = b"\xff\xd8" + b"\x00" * 1000
        self.assertFalse(is_valid_jpeg(short_bytes))

        # Invalid magic bytes
        invalid_magic = b"\x89PNG\r\n" + b"\x00" * (MIN_JPEG_SIZE)
        self.assertFalse(is_valid_jpeg(invalid_magic))

    # 5. Time of Day Tests
    def test_determine_time_of_day(self):
        tz = timezone(timedelta(hours=7))
        # 06:00 ICT -> dawn_dusk
        dt_dawn = datetime(2026, 10, 18, 6, 0, tzinfo=tz)
        self.assertEqual(determine_time_of_day(dt_dawn), "dawn_dusk")

        # 12:00 ICT -> day
        dt_day = datetime(2026, 10, 18, 12, 0, tzinfo=tz)
        self.assertEqual(determine_time_of_day(dt_day), "day")

        # 18:00 ICT -> dawn_dusk
        dt_dusk = datetime(2026, 10, 18, 18, 0, tzinfo=tz)
        self.assertEqual(determine_time_of_day(dt_dusk), "dawn_dusk")

        # 22:00 ICT -> night
        dt_night = datetime(2026, 10, 18, 22, 0, tzinfo=tz)
        self.assertEqual(determine_time_of_day(dt_night), "night")

    # 6. Waterline Depth Mapping Tests
    def test_determine_waterline_depth(self):
        # Dry
        flood, depth, anchor = determine_waterline_depth("dry", 0.0)
        self.assertEqual(flood, "NONE")
        self.assertEqual(depth, "DRY")
        self.assertEqual(anchor, "dry")

        # Rain (deprecated heuristic: defaults to UNKNOWN pending vision/human review)
        flood, depth, anchor = determine_waterline_depth("rain", 1.5)
        self.assertEqual(flood, "UNKNOWN")
        self.assertEqual(depth, "UNKNOWN")
        self.assertEqual(anchor, "unknown")

        # Heavy rain (deprecated heuristic: defaults to UNKNOWN pending vision/human review)
        flood, depth, anchor = determine_waterline_depth("heavy_rain", 8.0)
        self.assertEqual(flood, "UNKNOWN")
        self.assertEqual(depth, "UNKNOWN")
        self.assertEqual(anchor, "unknown")

    # 7. Traffic Severity Calculation Tests
    def test_calculate_traffic_severity(self):
        # Empty road
        sev, pcu, si, reason = calculate_traffic_severity({}, 0, 45.0)
        self.assertEqual(sev, "CLEAR")
        self.assertEqual(pcu, 0.0)
        self.assertEqual(reason, "free_flow")

        # Moderate queue on capacity 25.0: 10 motorcycles (3.0 PCU) + 5 cars (5.0 PCU) = 8.0 PCU (SI = 0.32)
        counts_mod = {"motorcycle": 10, "car": 5}
        sev, pcu, si, reason = calculate_traffic_severity(counts_mod, 15, 25.0)
        self.assertEqual(sev, "MODERATE")
        self.assertEqual(pcu, 8.0)
        self.assertAlmostEqual(si, 0.32, places=2)
        self.assertEqual(reason, "free_flow")

        # Congested bottleneck: 40 cars (40.0 PCU) on capacity 45.0 -> SI = 0.888
        counts_cong = {"car": 40}
        sev, pcu, si, reason = calculate_traffic_severity(counts_cong, 40, 45.0)
        self.assertEqual(sev, "CONGESTED")
        self.assertEqual(reason, "bottleneck")

    # 8. Draft-07 Label Synthesis & Schema Compliance Tests
    def test_synthesize_label_conforms_to_draft07_schema(self):
        camera = self.mock_catalog[0]
        pred_res = {
            "vehicles_detected": 12,
            "vehicle_counts": {"motorcycle": 20, "car": 6},
            "confidence": 0.89,
        }
        ts = 1792253800

        manifest = synthesize_label(
            camera=camera,
            weather="rain",
            rainfall_mm=2.5,
            pred_res=pred_res,
            ts=ts,
            trigger="now",
        )

        # Basic metadata checks
        self.assertEqual(manifest["camera_id"], "cam_hotset_1")
        self.assertEqual(manifest["annotator"], "harvester/auto")
        self.assertIn("rain", manifest["notes"])

        # Strict Draft-07 schema validation against tools/eval/label_schema.json
        is_valid, errors = validate_label_manifest(manifest, DEFAULT_SCHEMA_PATH)
        self.assertTrue(is_valid, f"Schema validation failed: {errors}")
        self.assertEqual(len(errors), 0)

        # Label-specific assertions (rain defaults to UNKNOWN for flood/depth per Lesson 39)
        labels = manifest["labels"]
        self.assertEqual(labels["carriageway_direction"], "bidirectional")
        self.assertEqual(labels["weather"], "rain")
        self.assertEqual(labels["flood"], "UNKNOWN")
        self.assertEqual(labels["waterline_depth"], "UNKNOWN")
        self.assertEqual(labels["flood_submersion_anchor"], "unknown")
        self.assertIn("rain_on_lens", labels["quality_tags"])

    # 9. Dry Run Pipeline Execution Test
    def test_run_pipeline_dry_run(self):
        args = parse_args(["--scope", "hotset", "--weather", "dry", "--dry-run"])
        exit_code = run_pipeline(args)
        self.assertEqual(exit_code, 0)


if __name__ == "__main__":
    unittest.main()
