#!/usr/bin/env python3
"""
Unit tests for build_roadway_registry.py (S10-T3 / Issue #374).
"""

from __future__ import annotations

import json
import shutil
import sys
import tempfile
import unittest
from pathlib import Path

# Add project root to sys.path
PROJECT_ROOT = Path(__file__).resolve().parent.parent.parent
if str(PROJECT_ROOT) not in sys.path:
    sys.path.insert(0, str(PROJECT_ROOT))

from tools.eval.build_roadway_registry import (
    build_and_export_registry,
    build_hot_set_registry,
    load_calibration_manifest,
    parse_labels_into_registry,
    validate_registry_dict,
    verify_registry_dir,
    PILOT_CORRIDORS,
    HOT_SET_CAMERA_IDS,
)
from unittest import mock


def compute_bbox_iou(poly_a: list[list[float]], poly_b: list[list[float]]) -> float:
    """
    Computes Bounding Box Intersection-over-Union (IoU) between two polygons.
    Returns a float in [0.0, 1.0].
    """
    xs_a = [p[0] for p in poly_a]
    ys_a = [p[1] for p in poly_a]
    xs_b = [p[0] for p in poly_b]
    ys_b = [p[1] for p in poly_b]

    min_x_a, max_x_a = min(xs_a), max(xs_a)
    min_y_a, max_y_a = min(ys_a), max(ys_a)
    min_x_b, max_x_b = min(xs_b), max(xs_b)
    min_y_b, max_y_b = min(ys_b), max(ys_b)

    inter_x1 = max(min_x_a, min_x_b)
    inter_y1 = max(min_y_a, min_y_b)
    inter_x2 = min(max_x_a, max_x_b)
    inter_y2 = min(max_y_a, max_y_b)

    inter_w = max(0.0, inter_x2 - inter_x1)
    inter_h = max(0.0, inter_y2 - inter_y1)
    inter_area = inter_w * inter_h

    area_a = max(0.0, max_x_a - min_x_a) * max(0.0, max_y_a - min_y_a)
    area_b = max(0.0, max_x_b - min_x_b) * max(0.0, max_y_b - min_y_b)
    union_area = area_a + area_b - inter_area
    if union_area <= 0.0:
        return 0.0
    return inter_area / union_area


class TestBuildRoadwayRegistry(unittest.TestCase):
    def setUp(self):
        self.test_dir = Path(tempfile.mkdtemp(prefix="roadway_test_"))

    def tearDown(self):
        shutil.rmtree(self.test_dir, ignore_errors=True)

    def test_validate_registry_valid_pilot(self):
        for cam_id, data in PILOT_CORRIDORS.items():
            valid, errors = validate_registry_dict(data)
            self.assertTrue(valid, f"Pilot corridor {cam_id} failed: {errors}")
            self.assertEqual(len(errors), 0)

    def test_validate_registry_inconsistent_capacity_share(self):
        # Case 1: sum < 0.99
        bad_data_low = {
            "camera_id": "test_cam_01",
            "regions": [
                {
                    "region_id": "test_cam_01_dir_a",
                    "direction": "a",
                    "polygon": [[0.1, 0.9], [0.4, 0.9], [0.3, 0.5]],
                    "capacity_share": 0.40,
                },
                {
                    "region_id": "test_cam_01_dir_b",
                    "direction": "b",
                    "polygon": [[0.6, 0.9], [0.9, 0.9], [0.7, 0.5]],
                    "capacity_share": 0.40,
                },
            ],
        }
        valid, errors = validate_registry_dict(bad_data_low)
        self.assertFalse(valid)
        self.assertTrue(any("capacity_share sum" in e for e in errors))

        # Case 2: sum > 1.01
        bad_data_high = {
            "camera_id": "test_cam_02",
            "regions": [
                {
                    "region_id": "test_cam_02_dir_a",
                    "direction": "a",
                    "polygon": [[0.1, 0.9], [0.4, 0.9], [0.3, 0.5]],
                    "capacity_share": 0.60,
                },
                {
                    "region_id": "test_cam_02_dir_b",
                    "direction": "b",
                    "polygon": [[0.6, 0.9], [0.9, 0.9], [0.7, 0.5]],
                    "capacity_share": 0.50,
                },
            ],
        }
        valid, errors = validate_registry_dict(bad_data_high)
        self.assertFalse(valid)
        self.assertTrue(any("capacity_share sum" in e for e in errors))

    def test_validate_registry_invalid_direction(self):
        bad_dir = {
            "camera_id": "test_cam_03",
            "regions": [
                {
                    "region_id": "test_cam_03_dir_x",
                    "direction": "x",  # invalid: only 'a' or 'b'
                    "polygon": [[0.1, 0.9], [0.4, 0.9], [0.3, 0.5]],
                    "capacity_share": 1.0,
                }
            ],
        }
        valid, errors = validate_registry_dict(bad_dir)
        self.assertFalse(valid)
        self.assertTrue(any("direction must be 'a' or 'b'" in e for e in errors))

    def test_validate_registry_filename_mismatch(self):
        data = {
            "camera_id": "cam_real",
            "regions": [
                {
                    "region_id": "cam_real_dir_a",
                    "direction": "a",
                    "polygon": [[0.1, 0.9], [0.4, 0.9], [0.3, 0.5]],
                    "capacity_share": 1.0,
                }
            ],
        }
        file_path = self.test_dir / "cam_wrong.json"
        valid, errors = validate_registry_dict(data, file_path=file_path)
        self.assertFalse(valid)
        self.assertTrue(any("filename stem" in e for e in errors))

    def test_parse_labels_and_export(self):
        labels_dir = self.test_dir / "labels"
        labels_dir.mkdir()
        out_dir = self.test_dir / "output"

        # Create a mock ground-truth label with directional roadway polygons
        label_file = labels_dir / "cam123_20261106.json"
        label_content = {
            "camera_id": "cam123",
            "annotations": [
                {
                    "class": "roadway",
                    "direction": "a",
                    "capacity_share": 0.5,
                    "polygon": [[0.1, 0.9], [0.4, 0.9], [0.3, 0.5]],
                },
                {
                    "class": "roadway",
                    "direction": "b",
                    "capacity_share": 0.5,
                    "polygon": [[0.6, 0.9], [0.9, 0.9], [0.7, 0.5]],
                },
            ],
        }
        with open(label_file, "w", encoding="utf-8") as f:
            json.dump(label_content, f)

        # Build and export
        count = build_and_export_registry(
            output_dir=out_dir,
            labels_dir=labels_dir,
            include_pilot=False,
        )
        self.assertEqual(count, 1)

        dest_file = out_dir / "cam123.json"
        self.assertTrue(dest_file.exists())
        with open(dest_file, "r", encoding="utf-8") as f:
            exported = json.load(f)
        self.assertEqual(exported["camera_id"], "cam123")
        self.assertEqual(exported["direction_coverage"], "dual")
        self.assertEqual(len(exported["regions"]), 2)

        # Verify output directory
        valid, errors = verify_registry_dir(out_dir)
        self.assertTrue(valid)
        self.assertEqual(len(errors), 0)

    def test_compute_bbox_iou_unit(self):
        # Case 1: Disjoint polygons (IoU == 0.0)
        poly_a = [[0.05, 0.95], [0.48, 0.95], [0.44, 0.42], [0.12, 0.42]]
        poly_b = [[0.52, 0.95], [0.95, 0.95], [0.86, 0.42], [0.54, 0.42]]
        iou = compute_bbox_iou(poly_a, poly_b)
        self.assertEqual(iou, 0.0)

        # Case 2: Overlapping bounding boxes (> 5% IoU)
        poly_overlap_1 = [[0.1, 0.9], [0.6, 0.9], [0.6, 0.4], [0.1, 0.4]]
        poly_overlap_2 = [[0.4, 0.9], [0.9, 0.9], [0.9, 0.4], [0.4, 0.4]]
        iou_overlap = compute_bbox_iou(poly_overlap_1, poly_overlap_2)
        self.assertGreater(iou_overlap, 0.05)

        # Case 3: Negligible overlap (< 5% IoU)
        poly_minimal_1 = [[0.0, 1.0], [0.50, 1.0], [0.50, 0.0], [0.0, 0.0]]
        poly_minimal_2 = [[0.495, 1.0], [1.0, 1.0], [1.0, 0.0], [0.495, 0.0]]
        iou_minimal = compute_bbox_iou(poly_minimal_1, poly_minimal_2)
        self.assertLess(iou_minimal, 0.05)
        self.assertGreater(iou_minimal, 0.0)

    def test_hotset_directional_scaleout_invariants(self):
        """
        Verify that when all 59 Hot Set cameras are present in data/roadway_regions/,
        every single one strictly adheres to ADR-012 D1 and geometry invariants:
        - camera_id matching filename stem
        - direction_coverage == 'dual'
        - exactly 2 regions with direction 'a' and 'b'
        - polygons have >= 3 valid vertices within [0.0, 1.0]
        - capacity_share > 0 and sum = 1.0 +- 0.01
        - minimal or zero bounding box IoU (< 5%) so directions do not collide
        - 100% camera-specific perspective calibration (all geometries unique)
        """
        registry_dir = PROJECT_ROOT / "data" / "roadway_regions"
        self.assertTrue(registry_dir.exists(), f"Registry dir not found: {registry_dir}")

        hotset_cams = list(HOT_SET_CAMERA_IDS)
        self.assertEqual(len(hotset_cams), 59, f"Expected 59 Hot Set cameras, found {len(hotset_cams)}")

        unique_geometries = set()

        # Verify every single hot set camera has a registered geometry file
        for cam_id in hotset_cams:
            reg_file = registry_dir / f"{cam_id}.json"
            self.assertTrue(reg_file.exists(), f"Missing roadway registry file for camera: {cam_id}")

            with open(reg_file, "r", encoding="utf-8") as f:
                data = json.load(f)

            # 1. camera_id matches filename stem
            self.assertEqual(data.get("camera_id"), cam_id, f"camera_id mismatch in {reg_file.name}")

            # 2. direction_coverage == 'dual'
            self.assertEqual(
                data.get("direction_coverage"),
                "dual",
                f"Camera {cam_id} direction_coverage must be 'dual', got {data.get('direction_coverage')}",
            )

            # 3. Exactly 2 regions with direction 'a' and 'b'
            regions = data.get("regions", [])
            self.assertEqual(len(regions), 2, f"Camera {cam_id} must have exactly 2 regions, got {len(regions)}")

            reg_a = next((r for r in regions if r.get("direction") == "a"), None)
            reg_b = next((r for r in regions if r.get("direction") == "b"), None)
            self.assertIsNotNone(reg_a, f"Camera {cam_id} missing region with direction 'a'")
            self.assertIsNotNone(reg_b, f"Camera {cam_id} missing region with direction 'b'")

            # 4. Each polygon has >= 3 valid vertices within [0.0, 1.0]
            for reg in (reg_a, reg_b):
                poly = reg.get("polygon", [])
                self.assertGreaterEqual(
                    len(poly), 3, f"Camera {cam_id} region {reg.get('region_id')} polygon has < 3 vertices"
                )
                for pt_idx, pt in enumerate(poly):
                    self.assertEqual(len(pt), 2, f"Camera {cam_id} vertex {pt_idx} must be [x, y]")
                    x, y = pt[0], pt[1]
                    self.assertTrue(
                        0.0 <= x <= 1.0 and 0.0 <= y <= 1.0,
                        f"Camera {cam_id} vertex {pt_idx} [{x}, {y}] out of bounds [0, 1]",
                    )

            # 5. Capacity share for each region > 0 and sum = 1.0 +- 0.01
            share_a = float(reg_a.get("capacity_share", 0.0))
            share_b = float(reg_b.get("capacity_share", 0.0))
            self.assertGreater(share_a, 0.0, f"Camera {cam_id} dir_a capacity_share must be > 0")
            self.assertGreater(share_b, 0.0, f"Camera {cam_id} dir_b capacity_share must be > 0")
            total_share = share_a + share_b
            self.assertAlmostEqual(
                total_share,
                1.0,
                delta=0.01,
                msg=f"Camera {cam_id} capacity share sum {total_share} must equal 1.0 +- 0.01",
            )

            # 6. Geometry non-overlap: Polygon A and Polygon B have < 5% bounding box IoU
            iou = compute_bbox_iou(reg_a["polygon"], reg_b["polygon"])
            self.assertLess(
                iou,
                0.05,
                f"Camera {cam_id} polygons overlap excessively: bounding box IoU={iou:.4f} (must be < 0.05)",
            )

            # 7. Uniqueness of geometry across cameras (no cloned templates)
            geom_sig = json.dumps([reg_a["polygon"], reg_b["polygon"]])
            unique_geometries.add(geom_sig)

        self.assertEqual(
            len(unique_geometries),
            59,
            f"Expected 59 unique camera geometries, got {len(unique_geometries)}",
        )

    def test_critical_arterial_corridors_cong_hoa_and_truong_chinh(self):
        """
        Verify critical arterial corridors cong_hoa and truong_chinh have calibrated
        dual directional polygons and minimal/zero bounding box overlap (< 5% IoU).
        """
        registry_dir = PROJECT_ROOT / "data" / "roadway_regions"

        # Critical Cong Hoa arterial cameras
        critical_cong_hoa_cams = [
            "586e1f18f9fab7001111b0a5",  # Cong Hoa - Truong Chinh junction
            "5874769eb807da0011e33cec",  # Cong Hoa - Ut Tich
            "587476e3b807da0011e33cee",  # Cong Hoa - Ut Tich
        ]
        for cam_id in critical_cong_hoa_cams:
            reg_file = registry_dir / f"{cam_id}.json"
            self.assertTrue(reg_file.exists(), f"Critical Cong Hoa camera {cam_id} missing from registry")
            with open(reg_file, "r", encoding="utf-8") as f:
                data = json.load(f)

            self.assertEqual(data.get("direction_coverage"), "dual")
            regions = data.get("regions", [])
            self.assertEqual(len(regions), 2)
            reg_a = next(r for r in regions if r["direction"] == "a")
            reg_b = next(r for r in regions if r["direction"] == "b")

            iou = compute_bbox_iou(reg_a["polygon"], reg_b["polygon"])
            self.assertLess(iou, 0.05, f"Camera {cam_id} bounding box IoU={iou:.4f} must be < 0.05")

        # Specific assertion on Truong Chinh junction camera
        truong_chinh_cam = "586e1f18f9fab7001111b0a5"
        with open(registry_dir / f"{truong_chinh_cam}.json", "r", encoding="utf-8") as f:
            tc_data = json.load(f)
        self.assertEqual(tc_data.get("camera_id"), truong_chinh_cam)
        self.assertEqual(tc_data.get("direction_coverage"), "dual")
        self.assertEqual(len(tc_data.get("regions", [])), 2)
        total_share = sum(r["capacity_share"] for r in tc_data["regions"])
        self.assertAlmostEqual(total_share, 1.0, delta=0.01)

    def test_verify_registry_dir_fails_on_duplicate_geometry(self):
        """Regression test: verify_registry_dir must reject duplicate geometry templates."""
        import copy
        with tempfile.TemporaryDirectory() as tmp_str:
            tmp_dir = Path(tmp_str)
            cam1 = {
                "camera_id": "cam_001",
                "direction_coverage": "dual",
                "regions": [
                    {
                        "region_id": "cam_001_dir_a",
                        "direction": "a",
                        "polygon": [[0.1, 0.9], [0.4, 0.9], [0.35, 0.4], [0.15, 0.4]],
                        "capacity_share": 0.5,
                    },
                    {
                        "region_id": "cam_001_dir_b",
                        "direction": "b",
                        "polygon": [[0.5, 0.9], [0.9, 0.9], [0.85, 0.4], [0.55, 0.4]],
                        "capacity_share": 0.5,
                    },
                ],
                "source_label_ids": ["src_1"],
            }
            # cam2 has identical polygons to cam1
            cam2 = copy.deepcopy(cam1)
            cam2["camera_id"] = "cam_002"
            cam2["regions"][0]["region_id"] = "cam_002_dir_a"
            cam2["regions"][1]["region_id"] = "cam_002_dir_b"

            with open(tmp_dir / "cam_001.json", "w") as f:
                json.dump(cam1, f)
            with open(tmp_dir / "cam_002.json", "w") as f:
                json.dump(cam2, f)

            valid, errors = verify_registry_dir(tmp_dir, require_dual=True, check_drift=False)
            self.assertFalse(valid, "Expected verify_registry_dir to fail on duplicate geometry")
            self.assertTrue(any("Duplicate or near-identical geometry detected" in err for err in errors))

    def test_verify_registry_dir_fails_on_near_identical_jittered_clone(self):
        """Regression test: verify_registry_dir must reject near-identical jittered clones (< 1e-3)."""
        import copy
        with tempfile.TemporaryDirectory() as tmp_str:
            tmp_dir = Path(tmp_str)
            cam1 = {
                "camera_id": "cam_001",
                "direction_coverage": "dual",
                "regions": [
                    {
                        "region_id": "cam_001_dir_a",
                        "direction": "a",
                        "polygon": [[0.1000, 0.9000], [0.4000, 0.9000], [0.3500, 0.4000], [0.1500, 0.4000]],
                        "capacity_share": 0.5,
                    },
                    {
                        "region_id": "cam_001_dir_b",
                        "direction": "b",
                        "polygon": [[0.5000, 0.9000], [0.9000, 0.9000], [0.8500, 0.4000], [0.5500, 0.4000]],
                        "capacity_share": 0.5,
                    },
                ],
                "source_label_ids": ["src_1"],
            }
            # cam2 has 1e-4 perturbed jittered vertices
            cam2 = copy.deepcopy(cam1)
            cam2["camera_id"] = "cam_002"
            cam2["regions"][0]["region_id"] = "cam_002_dir_a"
            cam2["regions"][1]["region_id"] = "cam_002_dir_b"
            cam2["regions"][0]["polygon"][0][0] += 0.0001
            cam2["regions"][1]["polygon"][0][0] -= 0.0001

            with open(tmp_dir / "cam_001.json", "w") as f:
                json.dump(cam1, f)
            with open(tmp_dir / "cam_002.json", "w") as f:
                json.dump(cam2, f)

            valid, errors = verify_registry_dir(tmp_dir, require_dual=True, check_drift=False)
            self.assertFalse(valid, "Expected verify_registry_dir to reject jittered clone")
            self.assertTrue(any("Duplicate or near-identical geometry detected" in err for err in errors))

    def test_load_calibration_manifest_raises_on_error(self):
        """Verify load_calibration_manifest raises ValueError on corrupted JSON or missing file."""
        with tempfile.TemporaryDirectory() as tmp_str:
            bad_json = Path(tmp_str) / "bad.json"
            bad_json.write_text("{ corrupt json: [", encoding="utf-8")
            with self.assertRaises(ValueError):
                load_calibration_manifest(bad_json)
            with self.assertRaises(FileNotFoundError):
                load_calibration_manifest(Path(tmp_str) / "nonexistent.json")

    def test_verify_registry_dir_detects_drift_against_manifest(self):
        """Verify verify_registry_dir detects when disk geometry drifts from committed manifest."""
        with tempfile.TemporaryDirectory() as tmp_str:
            tmp_dir = Path(tmp_str)
            manifest = load_calibration_manifest()
            # Copy a camera and modify its polygon
            cam_id = list(manifest.keys())[0]
            cam_data = {
                "camera_id": cam_id,
                "direction_coverage": "dual",
                "regions": [
                    {
                        "region_id": f"{cam_id}_dir_a",
                        "direction": "a",
                        "polygon": [[0.0, 0.0], [0.1, 0.0], [0.1, 0.1], [0.0, 0.1]],
                        "capacity_share": 0.5,
                    },
                    {
                        "region_id": f"{cam_id}_dir_b",
                        "direction": "b",
                        "polygon": [[0.5, 0.5], [0.6, 0.5], [0.6, 0.6], [0.5, 0.6]],
                        "capacity_share": 0.5,
                    },
                ],
                "source_label_ids": ["test"],
            }
            with open(tmp_dir / f"{cam_id}.json", "w") as f:
                json.dump(cam_data, f)

            valid, errors = verify_registry_dir(tmp_dir, check_drift=True)
            self.assertFalse(valid)
            self.assertTrue(any("drifted from committed calibration manifest" in err for err in errors))

    def test_build_hot_set_registry_reproducible_from_manifest(self):
        """Verify that when snapshots are absent, registry builds reproducibly from committed manifest."""
        with mock.patch("tools.eval.build_roadway_registry.find_camera_snapshot", return_value=None):
            registry = build_hot_set_registry()
            self.assertEqual(len(registry), 59)
            # Assert all 59 geometries remain unique
            sigs = set()
            for cid, data in registry.items():
                reg_a = next(r for r in data["regions"] if r["direction"] == "a")
                reg_b = next(r for r in data["regions"] if r["direction"] == "b")
                sigs.add(json.dumps([reg_a["polygon"], reg_b["polygon"]]))
            self.assertEqual(len(sigs), 59, "All 59 camera geometries must remain unique without snapshots")

    def test_build_hot_set_registry_zero_drift_against_disk(self):
        """Verify build_hot_set_registry produces byte/geometry-identical output to data/roadway_regions/."""
        registry = build_hot_set_registry()
        disk_dir = PROJECT_ROOT / "data" / "roadway_regions"
        for cid, generated_data in registry.items():
            disk_file = disk_dir / f"{cid}.json"
            self.assertTrue(disk_file.exists(), f"Missing disk file {disk_file}")
            with open(disk_file, "r", encoding="utf-8") as f:
                disk_data = json.load(f)
            self.assertEqual(generated_data["camera_id"], disk_data["camera_id"])
            self.assertEqual(generated_data["direction_coverage"], disk_data["direction_coverage"])
            self.assertEqual(len(generated_data["regions"]), len(disk_data["regions"]))
            for r_gen, r_disk in zip(generated_data["regions"], disk_data["regions"]):
                self.assertEqual(r_gen["direction"], r_disk["direction"])
                self.assertEqual(r_gen["polygon"], r_disk["polygon"])
                self.assertEqual(r_gen["capacity_share"], r_disk["capacity_share"])

    def test_build_hot_set_registry_fails_loudly_when_uncalibrated(self):
        """Verify build_hot_set_registry raises ValueError if a camera has no snapshot and no manifest."""
        with mock.patch("tools.eval.build_roadway_registry.find_camera_snapshot", return_value=None), \
             mock.patch("tools.eval.build_roadway_registry.load_calibration_manifest", return_value={}):
            with self.assertRaises(ValueError) as ctx:
                build_hot_set_registry()
            self.assertIn("strictly prohibited", str(ctx.exception))

    def test_find_camera_snapshot_pinned_stem_camera_scoped(self):
        """Verify find_camera_snapshot scopes lookup to the target camera's own directory."""
        from tools.eval.build_roadway_registry import find_camera_snapshot
        manifest = load_calibration_manifest()
        for cid, entry in list(manifest.items())[:15]:
            source_ids = entry.get("source_label_ids", [])
            if source_ids and "snapshot_" in source_ids[0]:
                stem = source_ids[0].split("snapshot_")[-1]
                path = find_camera_snapshot(cid, pinned_stem=stem)
                if path is not None:
                    self.assertIn(cid, path.parts, f"Camera {cid} resolved to foreign directory: {path}")

    def test_build_hot_set_registry_recalibrate_matches_disk(self):
        """Verify recalibrate=True from authentic proof snapshots matches disk geometry exactly."""
        from tools.eval.build_roadway_registry import HAS_PIL_NUMPY
        if not HAS_PIL_NUMPY:
            self.skipTest("PIL and NumPy required for recalibration test")
        if not (PROJECT_ROOT / "data" / "proofs").exists():
            self.skipTest("Local proofs directory not present")

        recalibrated = build_hot_set_registry(recalibrate=True)
        disk_dir = PROJECT_ROOT / "data" / "roadway_regions"
        for cid, gen_data in recalibrated.items():
            disk_file = disk_dir / f"{cid}.json"
            if disk_file.exists():
                with open(disk_file, "r", encoding="utf-8") as f:
                    disk_data = json.load(f)
                gen_polys = {r["direction"]: r["polygon"] for r in gen_data["regions"]}
                disk_polys = {r["direction"]: r["polygon"] for r in disk_data["regions"]}
                self.assertEqual(gen_polys, disk_polys, f"Recalibrated geometry for {cid} drifted from disk")


if __name__ == "__main__":
    unittest.main()
