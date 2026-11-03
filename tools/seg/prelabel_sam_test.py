#!/usr/bin/env python3
"""
prelabel_sam_test.py — Unit tests for the SAM pre-labeling CLI pipeline.

Runs the per-frame pipeline end-to-end against duck-typed fake SAM/detector
models and a fake R2 client: no network, no weights, deterministic.
"""

import json
import os
import sys
import tempfile
import time
import unittest
from pathlib import Path
from unittest import mock

import numpy as np

sys.path.insert(0, str(Path(__file__).resolve().parent.parent.parent))

try:
    from tools.seg import contract as seg_contract
    from tools.seg import prelabel_sam
except ImportError:  # Direct execution from tools/seg/.
    import contract as seg_contract  # type: ignore
    import prelabel_sam  # type: ignore

PIL_AVAILABLE = True
try:
    from PIL import Image  # noqa: F401
except ImportError:
    PIL_AVAILABLE = False


class FakeSam:
    """Duck-typed SamOnnxModel returning deterministic rectangular masks."""

    def __init__(self, w=400, h=300):
        self.w, self.h = w, h

    def preprocess_frame(self, img_rgb):
        return np.zeros((1024, 1024, 3), dtype=np.float32), 1.0, (0, 0)

    def encode(self, canvas):
        return np.zeros((1, 256, 64, 64), dtype=np.float32)

    def segment_box(self, embeddings, box_norm, scale, pads, orig_h, orig_w):
        h, w = orig_h, orig_w
        mask = np.zeros((h, w), dtype=bool)
        x1 = max(0, int(box_norm[0] * w))
        y1 = max(0, int(box_norm[1] * h))
        x2 = min(w, int(box_norm[2] * w))
        y2 = min(h, int(box_norm[3] * h))
        if x2 > x1 and y2 > y1:
            mask[y1:y2, x1:x2] = True
        return mask, 0.95

    def segment_points(self, embeddings, points_norm, scale, pads, orig_h, orig_w):
        mask = np.zeros((orig_h, orig_w), dtype=bool)
        mask[int(orig_h * 0.5):, int(orig_w * 0.1):int(orig_w * 0.9)] = True
        return mask, 0.9


class FakeDetector:
    def __init__(self):
        self.calls = 0

    def detect(self, img_rgb, conf=0.25, iou=0.45):
        self.calls += 1
        h, w = img_rgb.shape[:2]
        return [
            {"class": "car", "box_norm": [0.3, 0.5, 0.45, 0.7], "score": 0.9},
            {"class": "motorcycle", "box_norm": [0.6, 0.55, 0.7, 0.75], "score": 0.8},
        ]


def make_jpeg_bytes(w=400, h=300) -> bytes:
    import io

    from PIL import Image

    img = Image.new("RGB", (w, h), (120, 120, 120))
    buf = io.BytesIO()
    img.save(buf, format="JPEG", quality=90)
    return buf.getvalue()


@unittest.skipUnless(PIL_AVAILABLE, "Pillow is required for the pipeline test")
class TestBuildCandidate(unittest.TestCase):
    def _frame(self):
        return {
            "key": "2026-10-25/camABC/1760000000_sample.jpg",
            "camera_id": "camABC",
            "timestamp": 1760000000,
            "reason": "sample",
            "size": 30000,
        }

    def test_payload_shape_and_provenance(self):
        sam, detector = FakeSam(), FakeDetector()
        payload, telemetry = prelabel_sam.build_candidate(
            self._frame(), make_jpeg_bytes(), sam, detector, "2026-10-25",
            horizon_frac=0.4, max_vertices=48, conf=0.25, max_vehicle_polygons=12,
        )
        self.assertIsNotNone(payload)
        self.assertEqual(payload["annotator"], "sam_prelabel")
        self.assertEqual(payload["camera_id"], "camABC")
        self.assertEqual(payload["snapshot_key"], self._frame()["key"])
        self.assertEqual(payload["date"], "2026-10-25")
        anns = payload["annotations"]
        # Roadway + 2 vehicles; every annotation carries SAM provenance.
        self.assertGreaterEqual(len(anns), 3)
        self.assertTrue(all(a["source"] == "sam_prelabel" for a in anns))
        classes = [a["class"] for a in anns]
        self.assertIn("roadway", classes)
        self.assertIn("car", classes)
        self.assertIn("motorcycle", classes)
        # Whole payload passes the S9-T1 contract.
        self.assertEqual(seg_contract.validate_label_payload(payload), [])
        self.assertEqual(telemetry["status"], "ok")

    def test_undecodable_frame_skipped(self):
        payload, telemetry = prelabel_sam.build_candidate(
            self._frame(), b"not a jpeg", FakeSam(), FakeDetector(), "2026-10-25",
            horizon_frac=0.4, max_vertices=48, conf=0.25, max_vehicle_polygons=12,
        )
        self.assertIsNone(payload)
        self.assertEqual(telemetry["status"], "decode_failed")

    def test_annotation_cap_respected(self):
        sam = FakeSam()
        detector = FakeDetector()
        with mock.patch.object(detector, "detect") as many:
            many.return_value = [
                {"class": "car", "box_norm": [0.01 * i, 0.5, 0.01 * i + 0.02, 0.7], "score": 0.5}
                for i in range(40)
            ]
            payload, _ = prelabel_sam.build_candidate(
                self._frame(), make_jpeg_bytes(), sam, detector, "2026-10-25",
                horizon_frac=0.4, max_vertices=48, conf=0.25, max_vehicle_polygons=12,
            )
        self.assertLessEqual(len(payload["annotations"]), seg_contract.MAX_ANNOTATIONS_PER_LABEL)


class TestTrafficLevelAndTimeOfDay(unittest.TestCase):
    def test_traffic_level_boundaries(self):
        cases = [
            ({"car": 35}, "jam"),          # PCU 35.0
            ({"car": 34}, "heavy"),        # PCU 34.0
            ({"car": 22}, "heavy"),
            ({"car": 21}, "moderate"),
            ({"car": 10}, "moderate"),
            ({"car": 9}, "clear"),
            ({}, "clear"),
            ({"motorcycle": 100}, "heavy"),     # PCU 30.0
        ]
        for counts, expected in cases:
            self.assertEqual(prelabel_sam.calculate_traffic_level(counts), expected, counts)

    def test_time_of_day_boundaries(self):
        # Base 1760140800 = 2026-10-11 00:00 UTC = 07:00 ICT.
        self.assertEqual(prelabel_sam.determine_time_of_day(1760140800 + 9600), "day")    # 09:40 ICT
        self.assertEqual(prelabel_sam.determine_time_of_day(1760140800 + 54000), "night") # 22:00 ICT
        self.assertEqual(prelabel_sam.determine_time_of_day(1760140800 - 3600), "dawn_dusk")  # 06:00 ICT
        self.assertEqual(prelabel_sam.determine_time_of_day(1760140800 + 38700), "dawn_dusk")  # 17:45 ICT


class TestHostGuard(unittest.TestCase):
    def test_avx2_host_allowed(self):
        with mock.patch.object(prelabel_sam, "host_has_avx2", return_value=(True, "Linux/x86_64")):
            self.assertTrue(prelabel_sam.enforce_host_guard(allow_unsafe=False))

    def test_non_avx2_host_refused_by_default(self):
        with mock.patch.object(prelabel_sam, "host_has_avx2", return_value=(False, "Linux/x86_64")):
            self.assertFalse(prelabel_sam.enforce_host_guard(allow_unsafe=False))

    def test_explicit_override_allowed(self):
        with mock.patch.object(prelabel_sam, "host_has_avx2", return_value=(False, "Darwin/arm64")):
            self.assertTrue(prelabel_sam.enforce_host_guard(allow_unsafe=True))

    def test_host_has_avx2_runs_on_this_machine(self):
        # Smoke: the real detector returns a (bool, str) tuple without raising.
        supported, desc = prelabel_sam.host_has_avx2()
        self.assertIsInstance(supported, bool)
        self.assertTrue(desc)


class TestRateLimiterAndRetry(unittest.TestCase):
    def test_limiter_paces_calls(self):
        limiter = prelabel_sam.TokenBucketRateLimiter(rps=50.0, burst=2.0)
        start = time.time()
        for _ in range(6):
            limiter.acquire()
        elapsed = time.time() - start
        # 6 calls at 50 rps with burst 2 -> at least ~4/50 = 0.08s of pacing.
        self.assertGreaterEqual(elapsed, 0.06)

    def test_retry_succeeds_after_transient_failures(self):
        attempts = {"n": 0}

        def flaky():
            attempts["n"] += 1
            if attempts["n"] < 3:
                raise IOError("transient")
            return "ok"

        result = prelabel_sam.retry_with_backoff(flaky, limiter=None, base_delay=0.01)
        self.assertEqual(result, "ok")
        self.assertEqual(attempts["n"], 3)

    def test_retry_raises_after_max(self):
        def always_fails():
            raise IOError("permanent")

        with self.assertRaises(RuntimeError):
            prelabel_sam.retry_with_backoff(always_fails, limiter=None, max_retries=3, base_delay=0.01)


class _FakePage:
    def __init__(self, contents):
        self._contents = [{"Key": k, "Size": s} for k, s in contents]

    def get(self, key, default=None):
        if key == "Contents":
            return self._contents
        return default


class _FakePaginator:
    def __init__(self, contents):
        self.contents = contents

    def paginate(self, Bucket, Prefix):
        return [_FakePage(self.contents)]


class FakeS3Client:
    def __init__(self, contents):
        self._paginator = _FakePaginator(contents)

    def get_paginator(self, operation):
        return self._paginator


class TestListingAndKeys(unittest.TestCase):
    def test_list_frames_parses_both_layouts(self):
        s3 = FakeS3Client([
            ("2026-10-25/camA/1760000000_sample.jpg", 30000),
            ("proofs/2026-10-25/camB/1760000100_rain.jpg", 30000),
            ("2026-10-25/camA/1760000200_tiny.jpg", 100),  # below min size
            ("2026-10-25/camA/labels.json", 30000),        # not a frame
        ])
        limiter = prelabel_sam.TokenBucketRateLimiter(rps=100, burst=10)
        frames = prelabel_sam.list_frames(s3, "bucket", "2026-10-25", limiter)
        keys = {f["key"] for f in frames}
        self.assertEqual(len(keys), 2)
        by_cam = {f["camera_id"] for f in frames}
        self.assertEqual(by_cam, {"camA", "camB"})
        # Sorted newest first.
        self.assertGreaterEqual(frames[0]["timestamp"], frames[1]["timestamp"])

    def test_existing_prelabel_keys_roundtrip(self):
        s3 = FakeS3Client([
            ("labels/2026-10-25/camA_sam_prelabel_1760000000.json", 1000),
            ("labels/2026-10-25/camA_human_1760000000.json", 1000),
            ("labels/2026-10-25/camB_sam_prelabel_1760000500.json", 1000),
        ])
        limiter = prelabel_sam.TokenBucketRateLimiter(rps=100, burst=10)
        seen = prelabel_sam.existing_prelabel_keys(s3, "bucket", "2026-10-25", limiter)
        self.assertEqual(seen, {("camA", 1760000000), ("camB", 1760000500)})

    def test_upload_key_naming(self):
        frame = {"camera_id": "cam with space", "timestamp": 123}
        key = prelabel_sam.upload_key_for(frame, "2026-10-25", "sam_prelabel")
        self.assertEqual(key, "labels/2026-10-25/cam with space_sam_prelabel_123.json")


class TestIsValidJpeg(unittest.TestCase):
    def test_soi_and_min_size(self):
        self.assertTrue(prelabel_sam.is_valid_jpeg(b"\xff\xd8" + b"\x00" * 30000))
        self.assertFalse(prelabel_sam.is_valid_jpeg(b"\xff\xd8" + b"\x00" * 10))
        self.assertFalse(prelabel_sam.is_valid_jpeg(b"\x00\x00" + b"\x00" * 30000))


class TestLocalFramesMode(unittest.TestCase):
    def setUp(self):
        import tempfile

        self._tmp = tempfile.TemporaryDirectory()
        self.root = Path(self._tmp.name)

    def tearDown(self):
        self._tmp.cleanup()

    def _write_frame(self, cam: str, ts: int, size: int = 30000) -> None:
        cam_dir = self.root / "2026-10-23" / cam
        cam_dir.mkdir(parents=True, exist_ok=True)
        (cam_dir / f"{ts}_sample.jpg").write_bytes(b"\xff\xd8" + b"\x00" * (size - 2))

    def test_list_local_frames_parses_layout(self):
        self._write_frame("camA", 1792729787)
        self._write_frame("camB", 1792729800)
        (self.root / "2026-10-23" / "camA" / "notes.txt").write_text("not a frame")
        self._write_frame("camC", 1792729810, size=100)  # below min size

        frames = prelabel_sam.list_local_frames(self.root, "2026-10-23")
        cams = {f["camera_id"] for f in frames}
        self.assertEqual(cams, {"camA", "camB"})
        self.assertTrue(all(f["local"] for f in frames))
        # Newest first.
        self.assertEqual(frames[0]["camera_id"], "camB")

    def test_list_local_frames_missing_date(self):
        self.assertEqual(prelabel_sam.list_local_frames(self.root, "1999-01-01"), [])

    def test_existing_local_prelabel_keys(self):
        mirror = self.root / "mirror"
        label_dir = mirror / "2026-10-23"
        label_dir.mkdir(parents=True, exist_ok=True)
        (label_dir / "camA_sam_prelabel_1792729787.json").write_text("{}")
        (label_dir / "camA_human_1792729787.json").write_text("{}")
        seen = prelabel_sam.existing_local_prelabel_keys(mirror, "2026-10-23")
        self.assertEqual(seen, {("camA", 1792729787)})

    def test_download_frame_local_reads_disk(self):
        self._write_frame("camA", 1792729787)
        frame_path = self.root / "2026-10-23" / "camA" / "1792729787_sample.jpg"
        data = prelabel_sam.download_frame(None, "", str(frame_path), None, local=True)
        self.assertTrue(data.startswith(b"\xff\xd8"))

    def test_dry_run_local_mode_needs_no_r2(self):
        # The host guard runs before the dry-run branch; ARM CI hosts pass
        # the explicit override to exercise the offline plan path.
        args = prelabel_sam.parse_args([
            "--date", "2026-10-23", "--local-frames", str(self.root), "--dry-run",
            "--allow-unsafe-host",
        ])
        self.assertIsNotNone(args.local_frames)
        rc = prelabel_sam.run_pipeline(args)
        self.assertEqual(rc, 0)


class TestParseArgs(unittest.TestCase):
    def test_defaults(self):
        args = prelabel_sam.parse_args([])
        self.assertEqual(args.rps, 4.0)
        self.assertEqual(args.concurrency, 2)
        self.assertFalse(args.dry_run)
        self.assertFalse(args.upload_r2)
        self.assertFalse(args.allow_unsafe_host)

    def test_explicit_args(self):
        args = prelabel_sam.parse_args([
            "--date", "2026-10-25", "--limit", "10", "--dry-run",
            "--upload-r2", "--allow-unsafe-host",
        ])
        self.assertEqual(args.date, "2026-10-25")
        self.assertEqual(args.limit, 10)
        self.assertTrue(args.dry_run)
        self.assertTrue(args.upload_r2)
        self.assertTrue(args.allow_unsafe_host)


if __name__ == "__main__":
    unittest.main()
