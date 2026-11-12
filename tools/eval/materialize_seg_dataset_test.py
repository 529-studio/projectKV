#!/usr/bin/env python3
"""
Contract tests for tools/eval/materialize_seg_dataset.py (S12-T5 / Issue #411).

These tests are written before the implementation (TDD red phase) and are the
executable specification of §2.4, §2.5 and §5.1 of docs/sprints/s12_t5_spec.md.

Expected public contract of ``materialize_seg_dataset``
-------------------------------------------------------
Constants
    TARGET_CLASSES            canonical 6-class order, roadway first
    EXIT_OK = 0, EXIT_BUILD_FAILED = 1, EXIT_USAGE = 2
    MIN_FRAME_WIDTH = 320, MIN_FRAME_HEIGHT = 240, MAX_FRAME_PX = 4000

Exceptions (all subclass ``DatasetBuildError``)
    MissingFrameError         .missing_keys -> list[str]
    CameraOnlyPairingError    .camera_only  -> list[dict] (label metadata)
    FrameRejectedError        .reasons      -> list[str]
    DuplicateFrameError       .duplicates   -> list[str]
    FrozenSplitError

Functions
    sha256_file(path) -> str
    validate_frame_image(path) -> {"sha256", "width", "height", "bytes"}
        Rejects zero-byte ("empty"), a missing SOI ``FF D8 FF`` ("jpeg"),
        truncated/decode failures ("truncat"/"decode"), CMYK ("cmyk") and
        frames below 320x240 or above 4000 px ("resolution").
    resolve_frame(snapshot_key, roots, r2_fetch=None) -> {"path", "source"}
        ``roots`` is searched in order; ``source`` is "local" or "r2".
        ``r2_fetch(key)`` returns bytes and is only called after every local
        root missed (local precedence). Total miss raises MissingFrameError.
    match_pairs(frame_keys, labels) -> {"matches", "unmatched_frames", "camera_only"}
        Labels pair on the exact ``(camera_id, timestamp)`` key only.
        "camera_only" holds labels whose camera occurs among the frames but
        whose timestamp does not; they are never converted into matches.
    assert_pairs_buildable(result) -> None
        Raises CameraOnlyPairingError first (dangerous fallback), then
        MissingFrameError (fatal miss).
    assert_unique_frames(entries) -> None
        Rejects a duplicate stem and a duplicate image sha256.
    build_group_split(entries, val_ratio=0.2, seed=42) -> dict
        Keys: seed, algorithm, groups {"train","val","overlap"},
        train [stem], val [stem], group_of {stem: "camera|date"},
        train_groups [group], val_groups [group].
        val group count is ``max(1, int(round(len(groups) * val_ratio)))``.
    assert_split_invariants(split, entries) -> None
        Enforces, in this order: group overlap, val stem leaked into train,
        val sha256 leaked into train, train+val == total, and >= 1 val
        instance for each of the 6 classes. Each message names the offending
        group, stem, digest or class.
    load_frozen_split(path) -> dict
    assert_frozen_split_compatible(frozen, seed=..., manifest_sha256=...) -> None
    freeze_split(split, manifest_sha256) -> dict
        Returns the split with the manifest digest bound in; persisting it is
        the deliberate act that freezes the frame set.
    plan_replacement(out_dir, replace=False) -> {"action", "backup"}
        action is "create", "replace" or "refuse".
    swap_build_dir(tmp_dir, out_dir, replace=False, timestamp=None) -> dict
        Archives a non-empty out to ``<out>.bak-<timestamp>`` and moves the
        build directory into place; returns {"backup", "build_dir"}.
    assert_no_orphans(images_dir, labels_dir) -> {"images", "labels"}
    build_manifest(entries, split, generated_at, seed=42,
                   source="r2_live_archive") -> dict
    manifest_sha256(manifest) -> str
        Canonical, order independent, and independent of ``generated_at``.
    main(argv=None) -> int

CLI additions required for automated verification
    --labels-dir PATH   reviewed label JSON directory
    --proof-root PATH   repeatable local proof root (in addition to
                        $KV_PROOF_ROOT, data/proofs/proofs, data/proofs,
                        data/harvested)

Red phase behaviour
-------------------
Until the module exists the tests skip with a reason naming the missing API.
Run ``KV_TDD_STRICT=1 python3 -m unittest tools/eval/materialize_seg_dataset_test.py``
to turn every missing-contract skip into a hard failure.
"""

from __future__ import annotations

import functools
import hashlib
import io
import json
import os
import shutil
import subprocess
import sys
import tempfile
import unittest
from datetime import datetime, timedelta, timezone
from pathlib import Path

EVAL_DIR = Path(__file__).resolve().parent
REPO_ROOT = EVAL_DIR.parents[1]
if str(EVAL_DIR) not in sys.path:
    sys.path.insert(0, str(EVAL_DIR))

STRICT_RED_PHASE = os.environ.get("KV_TDD_STRICT") == "1"
MODULE_PATH = EVAL_DIR / "materialize_seg_dataset.py"
MODULE_NAME = "materialize_seg_dataset"

try:  # TDD red phase: the module is written by the Lead Coder after this suite.
    import materialize_seg_dataset as materializer  # noqa: E402
except ImportError as exc:  # pragma: no cover - exercised on every red-phase run
    materializer = None
    IMPORT_ERROR = f"{type(exc).__name__}: {exc}"
else:
    IMPORT_ERROR = None


def requires(*attrs):
    """Skip (or fail under KV_TDD_STRICT) when the implementation is absent."""

    def decorator(func):
        @functools.wraps(func)
        def wrapper(self, *args, **kwargs):
            if materializer is None:
                missing = list(attrs)
                reason = (
                    f"TDD red phase: {MODULE_PATH.relative_to(REPO_ROOT)} is not implemented "
                    f"(missing contract: {', '.join(missing)}; import error: {IMPORT_ERROR})"
                )
            else:
                missing = [name for name in attrs if not hasattr(materializer, name)]
                reason = (
                    f"TDD red phase: {MODULE_NAME} does not expose "
                    f"{', '.join(missing)}"
                )
            if missing:
                if STRICT_RED_PHASE:
                    raise AssertionError(reason)
                raise unittest.SkipTest(reason)
            return func(self, *args, **kwargs)

        return wrapper

    return decorator


# ---------------------------------------------------------------------------
# Fixture builders
# ---------------------------------------------------------------------------

CAM_A = "cam_aaaaaaaaaaaaaaaaaaaaaaaa"
CAM_B = "cam_bbbbbbbbbbbbbbbbbbbbbbbb"
CAM_C = "cam_cccccccccccccccccccccccc"
ALL_CLASSES = ["roadway", "water_puddle", "motorcycle", "car", "bus", "truck"]


def jpeg_bytes(
    width: int = 640,
    height: int = 480,
    mode: str = "RGB",
    quality: int = 80,
    color: tuple = (32, 64, 96),
) -> bytes:
    """Render a real JPEG with Pillow so SOF dimensions and decoding are genuine."""
    from PIL import Image

    buffer = io.BytesIO()
    Image.new(mode, (width, height), color if mode == "RGB" else None).save(
        buffer, format="JPEG", quality=quality
    )
    return buffer.getvalue()


def png_bytes(width: int = 640, height: int = 480) -> bytes:
    from PIL import Image

    buffer = io.BytesIO()
    Image.new("RGB", (width, height), (10, 20, 30)).save(buffer, format="PNG")
    return buffer.getvalue()


def make_label(
    camera_id: str,
    timestamp: int,
    *,
    date: str = "2026-11-08",
    classes: list[str] | None = None,
    annotator: str = "alice",
    reviewed_at: str | None = "2026-11-10T09:00:00+07:00",
) -> dict:
    classes = list(classes or ["water_puddle"])
    return {
        "snapshot_key": f"{date}/{camera_id}/{timestamp}_sample.jpg",
        "camera_id": camera_id,
        "timestamp": int(timestamp),
        "capture_date": date,
        "annotator": annotator,
        "reviewed_at": reviewed_at,
        "source": "sam_prelabel",
        "annotations": [
            {"class": name, "polygon": [[0.10, 0.10], [0.30, 0.10], [0.30, 0.30]]}
            for name in classes
        ],
    }


def make_entry(
    camera: str,
    date: str,
    timestamp: int,
    *,
    classes: list[str] | None = None,
    **overrides,
) -> dict:
    entry = {
        "stem": f"{camera}_{timestamp}",
        "key": f"{date}/{camera}/{timestamp}_sample.jpg",
        "camera_id": camera,
        "timestamp": int(timestamp),
        "capture_date": date,
        "group": f"{camera}|{date}",
        "classes": list(classes if classes is not None else ["water_puddle"]),
        "sha256": hashlib.sha256(f"{camera}|{date}|{timestamp}".encode()).hexdigest(),
        "width": 1920,
        "height": 1080,
        "bytes": 250_000,
        "annotator": "alice",
        "reviewed_at": "2026-11-10T09:00:00+07:00",
        "source": "r2",
        "weather": "rain",
        "time_of_day": "night",
        "dual_carriageway": False,
        "split": None,
    }
    entry.update(overrides)
    return entry


def entries_for_groups(group_count: int = 20, frames_per_group: int = 2, **overrides) -> list[dict]:
    entries: list[dict] = []
    for group_index in range(group_count):
        camera = f"cam{group_index:02d}"
        date = f"2026-11-{8 + (group_index % 4):02d}"
        for frame_index in range(frames_per_group):
            entries.append(
                make_entry(
                    camera,
                    date,
                    1_794_000_000 + group_index * 100 + frame_index,
                    **overrides,
                )
            )
    return entries


class CliFixture:
    """Minimal on-disk fixture tree for end-to-end CLI assertions."""

    def __init__(self, root: Path) -> None:
        self.root = root
        self.proofs = root / "proofs"
        self.labels = root / "labels"
        self.out = root / "out"
        self.manifest = root / "manifest.json"
        self.proofs.mkdir(parents=True, exist_ok=True)
        self.labels.mkdir(parents=True, exist_ok=True)

    def add_frame(self, key: str, payload: bytes | None = None) -> Path:
        path = self.proofs / key
        path.parent.mkdir(parents=True, exist_ok=True)
        if payload is None:
            # Distinct solid colours keep the JPEG bytes (and sha256) distinct.
            payload = jpeg_bytes(color=tuple(hashlib.sha256(key.encode()).digest()[:3]))
        path.write_bytes(payload)
        return path

    def add_label(self, label: dict) -> Path:
        path = self.labels / f"{label['camera_id']}_{label['timestamp']}.json"
        path.write_text(json.dumps(label), encoding="utf-8")
        return path

    def argv(self, *extra: str, seed: str = "42") -> list[str]:
        return [
            "--dates",
            "2026-11-08",
            "--min-frames",
            "1",
            "--out",
            str(self.out),
            "--val-split",
            "0.2",
            "--seed",
            seed,
            "--manifest",
            str(self.manifest),
            "--labels-dir",
            str(self.labels),
            "--proof-root",
            str(self.proofs),
            "--dry-run",
            "--limit",
            "10",
            *extra,
        ]

    def run(self, *extra: str, seed: str = "42") -> subprocess.CompletedProcess:
        env = dict(os.environ)
        env.pop("KV_PROOF_ROOT", None)
        env["KV_TDD_STRICT"] = "0"
        return subprocess.run(
            [sys.executable, str(MODULE_PATH), *self.argv(*extra, seed=seed)],
            cwd=str(self.root),
            env=env,
            capture_output=True,
            text=True,
            timeout=180,
        )


# ---------------------------------------------------------------------------
# 1. Exact-key resolution and camera-only rejection
# ---------------------------------------------------------------------------


class TestExactKeyResolution(unittest.TestCase):
    @requires("match_pairs", "MissingFrameError", "CameraOnlyPairingError")
    def test_exact_key_hit_produces_one_match(self):
        frame_key = f"2026-11-08/{CAM_A}/1794000000_sample.jpg"
        label = make_label(CAM_A, 1794000000)

        result = materializer.match_pairs([frame_key], [label])

        self.assertEqual(len(result["matches"]), 1)
        self.assertEqual(result["unmatched_frames"], [])
        self.assertEqual(result["camera_only"], [])
        match = result["matches"][0]
        self.assertEqual(match["key"], frame_key)
        self.assertEqual(match["camera_id"], CAM_A)
        self.assertEqual(match["timestamp"], 1794000000)
        self.assertEqual(match["label"]["annotator"], "alice")

    @requires("match_pairs", "MissingFrameError", "assert_pairs_buildable")
    def test_exact_key_miss_is_fatal_and_listed(self):
        frame_key = f"2026-11-08/{CAM_A}/1794000000_sample.jpg"
        # A label one second away on another camera: no exact key, and no
        # camera-only ambiguity to mask the miss.
        label = make_label(CAM_B, 1794000001)

        result = materializer.match_pairs([frame_key], [label])

        self.assertEqual(result["matches"], [])
        self.assertEqual(result["camera_only"], [])
        self.assertEqual(result["unmatched_frames"], [frame_key])
        with self.assertRaises(materializer.MissingFrameError) as ctx:
            materializer.assert_pairs_buildable(result)
        self.assertEqual(ctx.exception.missing_keys, [frame_key])
        self.assertIn(frame_key, str(ctx.exception))

    @requires("match_pairs", "CameraOnlyPairingError", "assert_pairs_buildable")
    def test_camera_only_match_is_rejected_and_never_bound(self):
        # Every frame has its exact label; one extra label shares a camera but
        # not a timestamp. That extra label must be an error, not a fallback.
        frames = [
            f"2026-11-08/{CAM_A}/1794000000_sample.jpg",
            f"2026-11-08/{CAM_B}/1794000100_sample.jpg",
        ]
        labels = [
            make_label(CAM_A, 1794000000, classes=["water_puddle"]),
            make_label(CAM_B, 1794000100, classes=["roadway"]),
            make_label(CAM_A, 1794009999, classes=["truck"]),
        ]

        result = materializer.match_pairs(frames, labels)

        self.assertEqual(len(result["matches"]), 2)
        self.assertEqual(result["unmatched_frames"], [])
        self.assertEqual(len(result["camera_only"]), 1)
        self.assertEqual(result["camera_only"][0]["camera_id"], CAM_A)
        self.assertEqual(result["camera_only"][0]["timestamp"], 1794009999)
        # The stray October-style polygon must not be attached to either match.
        bound_timestamps = {match["timestamp"] for match in result["matches"]}
        self.assertNotIn(1794009999, bound_timestamps)
        bound_classes = {cls["class"] for m in result["matches"] for cls in m["label"]["annotations"]}
        self.assertNotIn("truck", bound_classes)

        with self.assertRaises(materializer.CameraOnlyPairingError) as ctx:
            materializer.assert_pairs_buildable(result)
        self.assertEqual(len(ctx.exception.camera_only), 1)
        self.assertIn(CAM_A, str(ctx.exception))

    @requires("match_pairs")
    def test_timestamp_can_be_derived_from_snapshot_key_only(self):
        # The label may omit the explicit timestamp; the key is authoritative.
        label = make_label(CAM_A, 1794000000)
        label.pop("timestamp")

        result = materializer.match_pairs(
            [f"2026-11-08/{CAM_A}/1794000000_sample.jpg"], [label]
        )

        self.assertEqual(len(result["matches"]), 1)
        self.assertEqual(result["matches"][0]["timestamp"], 1794000000)

    @requires("scan_root_keys")
    def test_root_scan_returns_relative_frame_keys_for_the_pairing_set(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp) / "proofs"
            frame = root / "2026-11-08" / CAM_A / "1794000000_sample.jpg"
            frame.parent.mkdir(parents=True)
            frame.write_bytes(b"\xff\xd8\xff")
            (frame.parent / "notes.txt").write_text("not a frame", encoding="utf-8")
            other_date = root / "2026-11-09" / CAM_B / "1794000100_sample.jpg"
            other_date.parent.mkdir(parents=True)
            other_date.write_bytes(b"\xff\xd8\xff")

            keys = materializer.scan_root_keys([root], dates=["2026-11-08"])

        self.assertEqual(keys, [f"2026-11-08/{CAM_A}/1794000000_sample.jpg"])


# ---------------------------------------------------------------------------
# 2. Local proof directory precedence over R2
# ---------------------------------------------------------------------------


class TestFrameResolution(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, self.tmp, True)
        self.root_a = self.tmp / "root_a"
        self.root_b = self.tmp / "root_b"
        self.key = f"2026-11-08/{CAM_A}/1794000000_sample.jpg"

    def _write(self, root: Path, payload: bytes) -> Path:
        path = root / self.key
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(payload)
        return path

    @requires("resolve_frame")
    def test_local_root_hit_is_reused_without_r2(self):
        local_file = self._write(self.root_a, jpeg_bytes())

        calls: list[str] = []

        def r2_fetch(key: str) -> bytes:
            calls.append(key)
            raise AssertionError("local precedence violated: R2 was consulted")

        resolved = materializer.resolve_frame(self.key, [self.root_a, self.root_b], r2_fetch)

        self.assertEqual(resolved["source"], "local")
        self.assertEqual(Path(resolved["path"]).resolve(), local_file.resolve())
        self.assertEqual(calls, [])

    @requires("resolve_frame")
    def test_first_local_root_wins_when_two_roots_hold_the_key(self):
        first = self._write(self.root_a, jpeg_bytes(width=640, height=480))
        second = self._write(self.root_b, jpeg_bytes(width=800, height=600))
        self.assertNotEqual(first.read_bytes(), second.read_bytes())

        resolved = materializer.resolve_frame(self.key, [self.root_a, self.root_b])

        self.assertEqual(Path(resolved["path"]).resolve(), first.resolve())
        self.assertEqual(materializer.sha256_file(Path(resolved["path"])),
                         hashlib.sha256(first.read_bytes()).hexdigest())

    @requires("resolve_frame", "MissingFrameError")
    def test_r2_is_the_last_resort_and_bytes_match(self):
        payload = jpeg_bytes(width=800, height=600)
        seen: list[str] = []

        def r2_fetch(key: str) -> bytes:
            seen.append(key)
            return payload

        resolved = materializer.resolve_frame(self.key, [self.root_a, self.root_b], r2_fetch)

        self.assertEqual(seen, [self.key])
        self.assertEqual(resolved["source"], "r2")
        self.assertEqual(Path(resolved["path"]).read_bytes(), payload)

    @requires("resolve_frame", "MissingFrameError")
    def test_total_miss_is_fatal_naming_the_key(self):
        def r2_fetch(key: str) -> bytes:
            raise KeyError(key)

        with self.assertRaises(materializer.MissingFrameError) as ctx:
            materializer.resolve_frame(self.key, [self.root_a, self.root_b], r2_fetch)
        self.assertIn(self.key, str(ctx.exception))
        self.assertEqual(ctx.exception.missing_keys, [self.key])


# ---------------------------------------------------------------------------
# 3. Image integrity: SOI, decode, CMYK, resolution, duplicates
# ---------------------------------------------------------------------------


class TestFrameValidation(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, self.tmp, True)

    def _frame(self, name: str, payload: bytes) -> Path:
        path = self.tmp / name
        path.write_bytes(payload)
        return path

    @requires("validate_frame_image", "FrameRejectedError")
    def test_valid_jpeg_records_sha_dimensions_and_bytes(self):
        payload = jpeg_bytes(width=1280, height=720)
        path = self._frame("ok.jpg", payload)

        record = materializer.validate_frame_image(path)

        self.assertEqual(record["sha256"], hashlib.sha256(payload).hexdigest())
        self.assertEqual((record["width"], record["height"]), (1280, 720))
        self.assertEqual(record["bytes"], len(payload))

    @requires("validate_frame_image", "FrameRejectedError")
    def test_zero_byte_frame_is_rejected(self):
        path = self._frame("empty.jpg", b"")
        with self.assertRaises(materializer.FrameRejectedError) as ctx:
            materializer.validate_frame_image(path)
        self.assertIn("empty", " ".join(ctx.exception.reasons).lower())

    @requires("validate_frame_image", "FrameRejectedError")
    def test_non_jpeg_payload_is_rejected_by_soi_check(self):
        path = self._frame("actually_a_png.jpg", png_bytes())
        with self.assertRaises(materializer.FrameRejectedError) as ctx:
            materializer.validate_frame_image(path)
        reasons = " ".join(ctx.exception.reasons).lower()
        self.assertIn("jpeg", reasons)
        self.assertIn("ff d8 ff", reasons)

    @requires("validate_frame_image", "FrameRejectedError")
    def test_truncated_jpeg_is_rejected(self):
        payload = jpeg_bytes(width=1920, height=1080)
        path = self._frame("truncated.jpg", payload[: int(len(payload) * 0.6)])
        with self.assertRaises(materializer.FrameRejectedError) as ctx:
            materializer.validate_frame_image(path)
        reasons = " ".join(ctx.exception.reasons).lower()
        self.assertTrue(
            "truncat" in reasons or "decode" in reasons or "corrupt" in reasons,
            f"truncation reason not specific enough: {reasons}",
        )

    @requires("validate_frame_image", "FrameRejectedError")
    def test_soi_only_shell_is_rejected_as_undecodable(self):
        path = self._frame("soi_only.jpg", b"\xff\xd8\xff" + b"\x00" * 256)
        with self.assertRaises(materializer.FrameRejectedError):
            materializer.validate_frame_image(path)

    @requires("validate_frame_image", "FrameRejectedError")
    def test_cmyk_jpeg_is_rejected(self):
        path = self._frame("cmyk.jpg", jpeg_bytes(mode="CMYK"))
        with self.assertRaises(materializer.FrameRejectedError) as ctx:
            materializer.validate_frame_image(path)
        self.assertIn("cmyk", " ".join(ctx.exception.reasons).lower())

    @requires("validate_frame_image", "FrameRejectedError")
    def test_frame_below_minimum_resolution_is_rejected(self):
        path = self._frame("tiny.jpg", jpeg_bytes(width=200, height=150))
        with self.assertRaises(materializer.FrameRejectedError) as ctx:
            materializer.validate_frame_image(path)
        self.assertIn("resolution", " ".join(ctx.exception.reasons).lower())

    @requires("validate_frame_image", "FrameRejectedError")
    def test_frame_above_maximum_dimension_is_rejected(self):
        path = self._frame("huge.jpg", jpeg_bytes(width=4100, height=240))
        with self.assertRaises(materializer.FrameRejectedError) as ctx:
            materializer.validate_frame_image(path)
        self.assertIn("resolution", " ".join(ctx.exception.reasons).lower())


class TestDuplicateRejection(unittest.TestCase):
    @requires("assert_unique_frames", "DuplicateFrameError")
    def test_duplicate_stem_is_rejected(self):
        first = make_entry(CAM_A, "2026-11-08", 1794000000)
        second = dict(first, key=str(first["key"]) + "?copy=1")

        with self.assertRaises(materializer.DuplicateFrameError) as ctx:
            materializer.assert_unique_frames([first, second])
        self.assertIn(first["stem"], ctx.exception.duplicates)
        self.assertIn(first["stem"], str(ctx.exception))

    @requires("assert_unique_frames", "DuplicateFrameError")
    def test_duplicate_image_sha256_is_rejected(self):
        first = make_entry(CAM_A, "2026-11-08", 1794000000)
        second = make_entry(CAM_B, "2026-11-09", 1794000100, sha256=first["sha256"])

        with self.assertRaises(materializer.DuplicateFrameError) as ctx:
            materializer.assert_unique_frames([first, second])
        message = str(ctx.exception)
        self.assertIn(first["sha256"], message)
        self.assertIn(first["sha256"], ctx.exception.duplicates)

    @requires("assert_unique_frames")
    def test_distinct_frames_pass(self):
        entries = [
            make_entry(CAM_A, "2026-11-08", 1794000000),
            make_entry(CAM_A, "2026-11-08", 1794000001),
            make_entry(CAM_B, "2026-11-08", 1794000002),
        ]
        materializer.assert_unique_frames(entries)


# ---------------------------------------------------------------------------
# 4. Atomic replace, backup and orphan accounting
# ---------------------------------------------------------------------------


class TestAtomicSwap(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, self.tmp, True)
        self.out = self.tmp / "data" / "yolo_seg"
        self.build = self.tmp / "data" / "yolo_seg.tmp-99999"

    def _stage(self, marker: str) -> None:
        (self.build / "images" / "train").mkdir(parents=True, exist_ok=True)
        (self.build / "images" / "train" / f"{marker}.jpg").write_bytes(jpeg_bytes())
        (self.build / "split.json").write_text(json.dumps({"marker": marker}), encoding="utf-8")

    @requires("swap_build_dir", "plan_replacement")
    def test_fresh_out_directory_is_created_without_backup(self):
        self._stage("fresh")

        result = materializer.swap_build_dir(self.build, self.out, replace=False)

        self.assertIsNone(result["backup"])
        self.assertTrue((self.out / "split.json").is_file())
        self.assertEqual(json.loads((self.out / "split.json").read_text())["marker"], "fresh")
        self.assertFalse(self.build.exists(), "build directory must be consumed by the swap")

    @requires("swap_build_dir", "plan_replacement")
    def test_non_empty_out_without_replace_is_refused(self):
        self.out.mkdir(parents=True)
        (self.out / "stale.txt").write_text("stale", encoding="utf-8")
        self._stage("new")

        plan = materializer.plan_replacement(self.out, replace=False)
        self.assertEqual(plan["action"], "refuse")
        with self.assertRaises(materializer.DatasetBuildError):
            materializer.swap_build_dir(self.build, self.out, replace=False)
        self.assertEqual((self.out / "stale.txt").read_text(encoding="utf-8"), "stale")

    @requires("swap_build_dir", "plan_replacement")
    def test_replace_archives_stale_tree_to_timestamped_backup(self):
        self.out.mkdir(parents=True)
        (self.out / "stale.txt").write_text("stale", encoding="utf-8")
        self._stage("new")

        result = materializer.swap_build_dir(
            self.build, self.out, replace=True, timestamp="20261112T101500"
        )

        backup = Path(result["backup"])
        self.assertEqual(backup.name, "yolo_seg.bak-20261112T101500")
        self.assertTrue((backup / "stale.txt").is_file())
        self.assertFalse((self.out / "stale.txt").exists())
        self.assertTrue((self.out / "images" / "train" / "new.jpg").is_file())
        self.assertFalse(self.build.exists())

    @requires("swap_build_dir")
    def test_build_directory_is_a_sibling_of_out_for_rename_atomicity(self):
        self._stage("atomic")
        materializer.swap_build_dir(self.build, self.out, replace=False)
        # The staging path must live on the same filesystem as the target.
        self.assertEqual(self.build.parent, self.out.parent)
        self.assertRegex(
            self.build.name, r"^yolo_seg\.tmp-\d+$", "build path must be <out>.tmp-<pid>"
        )

    @requires("assert_no_orphans")
    def test_orphan_images_and_labels_are_counted_both_directions(self):
        images = self.tmp / "images"
        labels = self.tmp / "labels"
        for directory in (images, labels):
            directory.mkdir()
        (images / "a.jpg").write_bytes(jpeg_bytes())
        (labels / "a.txt").write_text("0 0.1 0.1 0.2 0.1 0.2 0.2", encoding="utf-8")
        (images / "b.jpg").write_bytes(jpeg_bytes())
        (labels / "c.txt").write_text("0 0.1 0.1 0.2 0.1 0.2 0.2", encoding="utf-8")

        orphans = materializer.assert_no_orphans(images, labels)

        self.assertEqual(orphans["images"], 1)  # b.jpg has no label
        self.assertEqual(orphans["labels"], 1)  # c.txt has no image


# ---------------------------------------------------------------------------
# 5. Group split and leakage invariants
# ---------------------------------------------------------------------------


class TestGroupSplit(unittest.TestCase):
    @requires("build_group_split", "assert_split_invariants")
    def test_eighty_twenty_group_split_has_no_overlap(self):
        entries = entries_for_groups(group_count=20, frames_per_group=2, classes=ALL_CLASSES)

        split = materializer.build_group_split(entries, val_ratio=0.2, seed=42)
        materializer.assert_split_invariants(split, entries)

        self.assertEqual(len(split["train_groups"]), 16)
        self.assertEqual(len(split["val_groups"]), 4)
        self.assertEqual(split["groups"]["overlap"], 0)
        self.assertFalse(set(split["train_groups"]) & set(split["val_groups"]))
        self.assertEqual(len(split["train"]), 32)
        self.assertEqual(len(split["val"]), 8)
        self.assertEqual(len(split["train"]) + len(split["val"]), len(entries))

    @requires("build_group_split")
    def test_group_members_never_span_splits(self):
        entries = entries_for_groups(group_count=20, frames_per_group=3, classes=ALL_CLASSES)
        split = materializer.build_group_split(entries, val_ratio=0.2, seed=42)
        train = set(split["train"])
        val = set(split["val"])

        for group in set(split["group_of"].values()):
            members = {stem for stem, key in split["group_of"].items() if key == group}
            self.assertTrue(
                members <= train or members <= val,
                f"group {group} spans both splits",
            )

    @requires("build_group_split")
    def test_same_seed_is_reproducible_and_different_seed_changes_assignment(self):
        entries = entries_for_groups(group_count=20, frames_per_group=2, classes=ALL_CLASSES)

        first = materializer.build_group_split(entries, val_ratio=0.2, seed=42)
        again = materializer.build_group_split(entries, val_ratio=0.2, seed=42)
        other = materializer.build_group_split(entries, val_ratio=0.2, seed=7)

        self.assertEqual(first["train"], again["train"])
        self.assertEqual(first["val"], again["val"])
        self.assertNotEqual(set(first["val_groups"]), set(other["val_groups"]))

    @requires("build_group_split", "assert_split_invariants")
    def test_group_key_is_camera_and_capture_date_not_filename(self):
        entries = [
            make_entry(CAM_A, "2026-11-08", 1794000000, classes=ALL_CLASSES),
            make_entry(CAM_A, "2026-11-09", 1794000100, classes=ALL_CLASSES),
            make_entry(CAM_B, "2026-11-08", 1794000200, classes=ALL_CLASSES),
            make_entry(CAM_C, "2026-11-08", 1794000300, classes=ALL_CLASSES),
        ]
        split = materializer.build_group_split(entries, val_ratio=0.5, seed=42)

        self.assertEqual(set(split["group_of"].values()),
                         {f"{CAM_A}|2026-11-08", f"{CAM_A}|2026-11-09",
                          f"{CAM_B}|2026-11-08", f"{CAM_C}|2026-11-08"})
        self.assertEqual(split["group_of"][entries[0]["stem"]], f"{CAM_A}|2026-11-08")

    @requires("build_group_split", "assert_split_invariants")
    def test_every_class_must_have_a_val_instance(self):
        entries = entries_for_groups(group_count=4, frames_per_group=1, classes=ALL_CLASSES)
        # 'truck' only exists inside the first group, which we force into train.
        for entry in entries:
            entry["classes"] = [name for name in ALL_CLASSES if name != "truck"]
        entries[0]["classes"] = list(ALL_CLASSES)

        split = materializer.build_group_split(entries, val_ratio=0.5, seed=42)
        first_group = entries[0]["group"]
        other_stems = [e["stem"] for e in entries if e["group"] != first_group]
        split["train"] = [e["stem"] for e in entries if e["group"] == first_group]
        split["val"] = other_stems

        with self.assertRaises(AssertionError) as ctx:
            materializer.assert_split_invariants(split, entries)
        self.assertIn("truck", str(ctx.exception))

    @requires("build_group_split", "assert_split_invariants")
    def test_val_stem_smuggled_into_train_fails_naming_the_stem(self):
        entries = entries_for_groups(group_count=5, frames_per_group=1, classes=ALL_CLASSES)
        split = materializer.build_group_split(entries, val_ratio=0.4, seed=42)
        smuggled = split["val"][0]
        split["train"].append(smuggled)

        with self.assertRaises(AssertionError) as ctx:
            materializer.assert_split_invariants(split, entries)
        message = str(ctx.exception)
        self.assertTrue(
            smuggled in message or split["group_of"][smuggled] in message,
            f"leak failure must name the stem or its group: {message}",
        )

    @requires("build_group_split", "assert_split_invariants")
    def test_val_sha256_collision_in_train_is_refused(self):
        entries = entries_for_groups(group_count=5, frames_per_group=1, classes=ALL_CLASSES)
        split = materializer.build_group_split(entries, val_ratio=0.4, seed=42)
        val_entry = next(e for e in entries if e["stem"] == split["val"][0])
        train_entry = next(e for e in entries if e["stem"] == split["train"][0])
        train_entry["sha256"] = val_entry["sha256"]

        with self.assertRaises(AssertionError) as ctx:
            materializer.assert_split_invariants(split, entries)
        self.assertIn(val_entry["sha256"], str(ctx.exception))


# ---------------------------------------------------------------------------
# 6. Frozen split
# ---------------------------------------------------------------------------


class TestFrozenSplit(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, self.tmp, True)

    def _frozen(self, *, seed: int = 42, sha: str = "a" * 64) -> dict:
        return {
            "version": "3.0",
            "seed": seed,
            "algorithm": "group_shuffle_camera_date",
            "groups": {"train": 16, "val": 4, "overlap": 0},
            "train": ["stem_a"],
            "val": ["stem_b"],
            "train_groups": ["camA|2026-11-08"],
            "val_groups": ["camB|2026-11-08"],
            "group_of": {"stem_a": "camA|2026-11-08", "stem_b": "camB|2026-11-08"},
            "manifest_sha256": sha,
        }

    @requires("load_frozen_split", "assert_frozen_split_compatible")
    def test_same_seed_and_manifest_sha_is_accepted(self):
        path = self.tmp / "split.json"
        path.write_text(json.dumps(self._frozen()), encoding="utf-8")

        frozen = materializer.load_frozen_split(path)
        materializer.assert_frozen_split_compatible(
            frozen, seed=42, manifest_sha256="a" * 64
        )
        self.assertEqual(frozen["val"], ["stem_b"])

    @requires("load_frozen_split", "assert_frozen_split_compatible", "FrozenSplitError")
    def test_different_seed_against_a_freeze_is_refused(self):
        path = self.tmp / "split.json"
        path.write_text(json.dumps(self._frozen(seed=42)), encoding="utf-8")
        frozen = materializer.load_frozen_split(path)

        with self.assertRaises(materializer.FrozenSplitError) as ctx:
            materializer.assert_frozen_split_compatible(frozen, seed=7)
        self.assertIn("seed", str(ctx.exception).lower())
        self.assertIn("42", str(ctx.exception))
        self.assertIn("7", str(ctx.exception))

    @requires("load_frozen_split", "assert_frozen_split_compatible", "FrozenSplitError")
    def test_manifest_sha_drift_is_refused(self):
        path = self.tmp / "split.json"
        path.write_text(json.dumps(self._frozen(sha="a" * 64)), encoding="utf-8")
        frozen = materializer.load_frozen_split(path)

        with self.assertRaises(materializer.FrozenSplitError) as ctx:
            materializer.assert_frozen_split_compatible(
                frozen, seed=42, manifest_sha256="b" * 64
            )
        self.assertIn("split", str(ctx.exception).lower())

    @requires("load_frozen_split", "FrozenSplitError")
    def test_malformed_freeze_is_refused(self):
        path = self.tmp / "split.json"
        path.write_text(json.dumps({"seed": 42, "train": []}), encoding="utf-8")
        with self.assertRaises(materializer.FrozenSplitError):
            materializer.load_frozen_split(path)

    @requires("main", "load_frozen_split")
    def test_cli_exits_2_on_seed_mismatch_and_writes_nothing(self):
        fixture = CliFixture(self.tmp / "repo")
        label = make_label(CAM_A, 1794000000)
        key = label["snapshot_key"]
        fixture.add_frame(key)
        fixture.add_label(label)
        fixture.out.mkdir(parents=True)
        frozen = self._frozen(seed=42)
        (fixture.out / "split.json").write_text(json.dumps(frozen), encoding="utf-8")
        before = {
            path.relative_to(fixture.out).as_posix(): path.read_bytes()
            for path in fixture.out.rglob("*")
            if path.is_file()
        }

        completed = fixture.run("--replace", seed="7")

        self.assertEqual(
            completed.returncode,
            2,
            f"expected exit 2 on frozen-split seed mismatch\n"
            f"stdout:\n{completed.stdout}\nstderr:\n{completed.stderr}",
        )
        combined = (completed.stdout + completed.stderr).lower()
        self.assertIn("frozen", combined)
        after = {
            path.relative_to(fixture.out).as_posix(): path.read_bytes()
            for path in fixture.out.rglob("*")
            if path.is_file()
        }
        self.assertEqual(before, after, "a refused freeze must not touch the split")
        self.assertEqual(list(fixture.out.parent.glob("yolo_seg.bak-*")), [])
        self.assertFalse((fixture.out / "images").exists())


# ---------------------------------------------------------------------------
# 7. 301-set negative control (AC3 / AC4)
# ---------------------------------------------------------------------------


class TestRetiredOctoberSetNegativeControl(unittest.TestCase):
    @requires("match_pairs", "assert_pairs_buildable", "CameraOnlyPairingError")
    def test_october_labels_never_bind_to_november_proofs(self):
        labels = [
            make_label(f"cam{i:02d}", 1_791_400_000 + i, date="2026-10-08")
            for i in range(301)
        ]
        proofs = [
            f"2026-11-06/cam{i:02d}/{1_793_900_000 + i}_sample.jpg" for i in range(301)
        ]

        result = materializer.match_pairs(proofs, labels)

        self.assertEqual(result["matches"], [], "camera-only pairing must not fabricate matches")
        self.assertEqual(len(result["camera_only"]), 301)
        self.assertEqual(len(result["unmatched_frames"]), 301)
        with self.assertRaises(materializer.CameraOnlyPairingError):
            materializer.assert_pairs_buildable(result)

    @unittest.skipUnless(
        (REPO_ROOT / "data" / "yolo_seg" / "labels").is_dir()
        and (REPO_ROOT / "data" / "proofs" / "proofs").is_dir(),
        "retired staged set or November proof tree is not present",
    )
    @requires("match_pairs", "assert_unique_frames", "DuplicateFrameError")
    def test_real_retired_set_scores_zero_and_keeps_twelve_duplicate_stems(self):
        staged = REPO_ROOT / "data" / "yolo_seg" / "labels"
        stems_txt = sorted(
            path.stem for path in list(staged.rglob("*.txt"))
        )
        if len(stems_txt) != 301:
            self.skipTest(f"expected the 301-file staged set, found {len(stems_txt)}")

        proofs_root = REPO_ROOT / "data" / "proofs" / "proofs"

        def split_stem(stem: str) -> tuple[str, int]:
            camera, _, tail = stem.partition("_sam_prelabel_")
            return camera, int(tail)

        labels = [
            {"camera_id": split_stem(stem)[0], "timestamp": split_stem(stem)[1],
             "snapshot_key": stem, "annotator": "sam_prelabel"}
            for stem in stems_txt
        ]
        proof_keys = [
            path.relative_to(proofs_root).as_posix()
            for path in sorted(proofs_root.rglob("*_sample.jpg"))
        ]
        self.assertGreater(len(proof_keys), 0)

        result = materializer.match_pairs(proof_keys, labels)
        self.assertEqual(result["matches"], [], "0/301 exact matches is the expected control")
        self.assertGreater(
            len(result["camera_only"]),
            0,
            "at least one October camera reappears in the November archive: that is the "
            "camera-only poison the control detects",
        )
        self.assertLessEqual(len(result["camera_only"]), 301)

        entries = [
            {
                "stem": stem,
                "sha256": hashlib.sha256(stem.encode()).hexdigest(),
                "key": f"2026-10-08/{split_stem(stem)[0]}/{split_stem(stem)[1]}_sample.jpg",
            }
            for stem in stems_txt
        ]
        with self.assertRaises(materializer.DuplicateFrameError) as ctx:
            materializer.assert_unique_frames(entries)
        self.assertEqual(len(ctx.exception.duplicates), 12)
        self.assertEqual(len(set(stems_txt)), 289)


# ---------------------------------------------------------------------------
# 8. Manifest schema and SHA stability
# ---------------------------------------------------------------------------


class TestManifest(unittest.TestCase):
    def _build(self, *, generated_at: str = "2026-11-12T10:00:00+07:00"):
        entries = entries_for_groups(group_count=20, frames_per_group=2, classes=ALL_CLASSES)
        entries[0]["weather"] = "dry"
        entries[1]["time_of_day"] = "day"
        entries[2]["dual_carriageway"] = True
        split = materializer.build_group_split(entries, val_ratio=0.2, seed=42)
        manifest = materializer.build_manifest(
            entries, split, generated_at=generated_at, seed=42
        )
        return entries, split, manifest

    @requires("build_manifest", "build_group_split", "manifest_sha256")
    def test_manifest_matches_the_published_schema(self):
        entries, split, manifest = self._build()

        for key in ("version", "generated_at", "source", "total_frames", "train", "val",
                    "seed", "split_sha256", "groups", "strata", "labels"):
            self.assertIn(key, manifest, f"manifest is missing {key}")
        self.assertEqual(manifest["version"], "3.0")
        self.assertEqual(manifest["source"], "r2_live_archive")
        self.assertEqual(manifest["total_frames"], len(entries))
        self.assertEqual(manifest["train"], len(split["train"]))
        self.assertEqual(manifest["val"], len(split["val"]))
        self.assertEqual(manifest["seed"], 42)
        self.assertRegex(manifest["split_sha256"], r"^[0-9a-f]{64}$")
        self.assertEqual(manifest["groups"], {"train": 16, "val": 4, "overlap": 0})
        self.assertEqual(
            manifest["strata"],
            {"rain_wet": 39, "dusk_night": 39, "dual_carriageway": 1},
        )

        self.assertEqual(len(manifest["labels"]), len(entries))
        for record in manifest["labels"]:
            for field in ("stem", "key", "sha256", "width", "height", "split", "group",
                          "annotator", "reviewed_at", "source"):
                self.assertIn(field, record, f"label record is missing {field}")
            self.assertIn(record["split"], {"train", "val"})
            self.assertIn(record["source"], {"local", "r2"})
            self.assertRegex(record["sha256"], r"^[0-9a-f]{64}$")
            self.assertGreater(record["width"], 0)

        self.assertEqual({record["stem"] for record in manifest["labels"]},
                         {entry["stem"] for entry in entries})

    @requires("build_manifest", "build_group_split", "manifest_sha256")
    def test_manifest_sha256_is_stable_across_time_and_input_order(self):
        entries, split, manifest = self._build()
        _, _, later = self._build(generated_at="2026-11-13T23:59:59+07:00")
        _, _, reversed_manifest = self._build()
        reversed_manifest["labels"] = list(reversed(reversed_manifest["labels"]))

        digest = materializer.manifest_sha256(manifest)
        self.assertEqual(digest, materializer.manifest_sha256(manifest))
        self.assertEqual(digest, materializer.manifest_sha256(later),
                         "generated_at must not affect the dataset digest")
        self.assertEqual(digest, materializer.manifest_sha256(reversed_manifest),
                         "label order must not affect the dataset digest")

        tampered = json.loads(json.dumps(manifest))
        tampered["labels"][0]["sha256"] = "0" * 64
        self.assertNotEqual(digest, materializer.manifest_sha256(tampered))

    @requires("build_manifest", "build_group_split", "manifest_sha256")
    def test_generated_at_is_never_in_the_future(self):
        _, _, manifest = self._build(generated_at="2026-11-12T10:00:00+07:00")

        generated = datetime.fromisoformat(manifest["generated_at"])
        self.assertIsNotNone(generated.tzinfo, "generated_at must carry an offset")
        self.assertLessEqual(generated, datetime.now(timezone.utc) + timedelta(seconds=5))

    @requires("build_manifest", "build_group_split", "manifest_sha256", "freeze_split")
    def test_split_freeze_binds_the_manifest_digest(self):
        _, split, manifest = self._build()
        digest = materializer.manifest_sha256(manifest)

        frozen = materializer.freeze_split(split, digest)

        self.assertEqual(len(digest), 64)
        self.assertEqual(frozen["manifest_sha256"], digest)
        self.assertEqual(frozen["seed"], 42)
        self.assertIn("algorithm", frozen)
        self.assertRegex(frozen["algorithm"], r"^[a-z0-9_]+$")
        materializer.assert_frozen_split_compatible(
            frozen, seed=42, manifest_sha256=digest
        )


# ---------------------------------------------------------------------------
# 9. Full CLI pipeline smoke (dry run)
# ---------------------------------------------------------------------------


class TestCliDryRun(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = Path(tempfile.mkdtemp())
        self.addCleanup(shutil.rmtree, self.tmp, True)

    @requires("main")
    def test_dry_run_writes_nothing_outside_the_build_directory(self):
        fixture = CliFixture(self.tmp / "repo")
        for index in range(4):
            camera = f"cam{index:02d}"
            label = make_label(camera, 1_794_000_000 + index, classes=ALL_CLASSES)
            fixture.add_frame(label["snapshot_key"])
            fixture.add_label(label)

        completed = fixture.run()

        self.assertEqual(
            completed.returncode,
            0,
            f"dry run failed\nstdout:\n{completed.stdout}\nstderr:\n{completed.stderr}",
        )
        self.assertFalse(fixture.out.exists(), "dry run must not materialize --out")
        self.assertFalse(fixture.manifest.exists(), "dry run must not write the manifest")
        self.assertEqual(list(fixture.proofs.rglob("*.tmp*")), [])

    @requires("main", "MissingFrameError")
    def test_missing_frame_fails_the_build_with_a_non_zero_exit(self):
        fixture = CliFixture(self.tmp / "repo")
        present = make_label(CAM_A, 1794000000)
        missing = make_label(CAM_B, 1794000100)
        fixture.add_frame(present["snapshot_key"])
        fixture.add_label(present)
        fixture.add_label(missing)

        completed = fixture.run()

        self.assertNotEqual(completed.returncode, 0)
        self.assertIn(missing["snapshot_key"], completed.stdout + completed.stderr)
        self.assertFalse(fixture.out.exists())
        self.assertFalse(fixture.manifest.exists())

    @requires("main", "CameraOnlyPairingError")
    def test_stray_label_from_a_known_camera_fails_as_camera_only(self):
        """A label whose camera is in the proof scan but whose timestamp is not
        must be refused as camera-only, not silently accepted or misreported as
        an R2 configuration gap."""
        fixture = CliFixture(self.tmp / "repo")
        present = make_label(CAM_A, 1794000000)
        stray = make_label(CAM_A, 1794009999)
        fixture.add_frame(present["snapshot_key"])
        fixture.add_label(present)
        fixture.add_label(stray)

        completed = fixture.run()

        output = completed.stdout + completed.stderr
        self.assertNotEqual(completed.returncode, 0)
        self.assertIn("camera-only", output.lower())
        self.assertIn(CAM_A, output)
        self.assertIn("1794009999", output)
        self.assertFalse(fixture.out.exists())
        self.assertFalse(fixture.manifest.exists())


if __name__ == "__main__":
    unittest.main(verbosity=2)
