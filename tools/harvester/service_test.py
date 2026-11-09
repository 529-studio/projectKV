#!/usr/bin/env python3
"""
Unit and integration tests for tools/harvester/service.py.

Sprint 11 Wave 1 (Issue #396 / S11-T2):
  * Test harvester scheduler loop (interval parsing, camera iteration, error handling)
  * Test snapshot downloading and timeout handling
  * Test writing verdicts and saving proof frames
  * Test telemetry emission (unbroken soak duration, consecutive cycles, failure count)
  * Test health check, status report, and --once execution mode

Adheres strictly to AGENTS.md (zero-emoji, English only, clean tear-down of temp files).
"""

from __future__ import annotations

import argparse
import datetime
import io
import json
import os
import sqlite3
import subprocess
import sys
import tempfile
import time
import unittest
from pathlib import Path
from typing import Any, Dict, List
from unittest.mock import MagicMock, patch

# Ensure repository root is on Python path
REPO_ROOT = Path(__file__).resolve().parent.parent.parent
if str(REPO_ROOT) not in sys.path:
    sys.path.insert(0, str(REPO_ROOT))

import tools.harvester.service as harvester_mod
import tools.eval.ingest_proof_archive as ingest_mod

# Minimal 1x1 valid JPEG byte sequence (placeholder frame, below the minimum resolution)
TINY_JPEG_BYTES = (
    b"\xff\xd8\xff\xe0\x00\x10JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00"
    b"\xff\xdb\x00C\x00\x08\x06\x06\x07\x06\x05\x08\x07\x07\x07\t\t\x08\n\x0c"
    b"\x14\r\x0c\x0b\x0b\x0c\x19\x12\x13\x0f\x14\x1d\x1a\x1f\x1e\x1d\x1a\x1c"
    b"\x1c $.' \",#\x1c\x1c(7),01444\x1f'9=82<.342\xff\xc0\x00\x0b\x08\x00"
    b"\x01\x00\x01\x01\x01\x11\x00\xff\xc4\x00\x1f\x00\x00\x01\x05\x01\x01"
    b"\x01\x01\x01\x01\x00\x00\x00\x00\x00\x00\x00\x00\x01\x02\x03\x04\x05"
    b"\x06\x07\x08\t\n\x0b\xff\xda\x00\x08\x01\x01\x00\x00?\x00\xbf\x00\xff\xd9"
)


def make_jpeg(width: int = 640, height: int = 480) -> bytes:
    """Return the fixture JPEG with its Start-Of-Frame dimensions patched."""
    sof_prefix = b"\xff\xc0\x00\x0b\x08"
    return TINY_JPEG_BYTES.replace(
        sof_prefix + b"\x00\x01\x00\x01",
        sof_prefix + height.to_bytes(2, "big") + width.to_bytes(2, "big"),
        1,
    )


VALID_JPEG_BYTES = make_jpeg()


class TestCameraLoadingAndScheduler(unittest.TestCase):
    """Tests for camera catalog filtering and scheduler configuration."""

    def setUp(self) -> None:
        self.temp_dir = tempfile.TemporaryDirectory()
        self.catalog_path = Path(self.temp_dir.name) / "cameras.json"

    def tearDown(self) -> None:
        self.temp_dir.cleanup()

    def test_load_hotset_cameras_filtering(self) -> None:
        """Verify selecting only online, non-ghost Hot Set cameras."""
        catalog = [
            # Eligible camera
            {
                "id": "cam_eligible_1",
                "name": "Arterial Camera 1",
                "isOnline": True,
                "hotset": True,
                "isGhost": False,
            },
            # Offline camera (should be skipped)
            {
                "id": "cam_offline",
                "name": "Offline Camera",
                "isOnline": False,
                "hotset": True,
                "isGhost": False,
            },
            # Ghost camera (should be skipped)
            {
                "id": "cam_ghost",
                "name": "Ghost Camera",
                "isOnline": True,
                "hotset": True,
                "isGhost": True,
            },
            # Duplicate camera (should be skipped)
            {
                "id": "cam_dup",
                "name": "Duplicate Camera",
                "isOnline": True,
                "hotset": True,
                "duplicateOf": "cam_eligible_1",
            },
            # Non-hotset camera (should be skipped)
            {
                "id": "cam_non_hotset",
                "name": "Regular Camera",
                "isOnline": True,
                "hotset": False,
            },
            # Another eligible camera
            {
                "id": "cam_eligible_2",
                "name": "Arterial Camera 2",
                "isOnline": True,
                "hotset": True,
            },
        ]
        self.catalog_path.write_text(json.dumps(catalog), encoding="utf-8")

        cameras = harvester_mod.load_hotset_cameras(self.catalog_path)
        self.assertEqual(len(cameras), 2)
        selected_ids = {c["id"] for c in cameras}
        self.assertEqual(selected_ids, {"cam_eligible_1", "cam_eligible_2"})

    def test_load_hotset_cameras_by_hotset_camera_ids(self) -> None:
        """Verify catalog with top-level hotset_camera_ids list is respected."""
        catalog_dict = {
            "hotset_camera_ids": ["cam_list_1"],
            "cameras": [
                {"id": "cam_list_1", "isOnline": True},
                {"id": "cam_list_2", "isOnline": True},
            ],
        }
        self.catalog_path.write_text(json.dumps(catalog_dict), encoding="utf-8")

        cameras = harvester_mod.load_hotset_cameras(self.catalog_path)
        self.assertEqual(len(cameras), 1)
        self.assertEqual(cameras[0]["id"], "cam_list_1")

    def test_load_hotset_cameras_capped_at_57(self) -> None:
        """Verify the canonical 57-camera roster cap is enforced."""
        catalog = [
            {"id": f"cam_cap_{index:02d}", "isOnline": True, "hotset": True}
            for index in range(60)
        ]
        self.catalog_path.write_text(json.dumps(catalog), encoding="utf-8")

        cameras = harvester_mod.load_hotset_cameras(self.catalog_path)
        self.assertEqual(len(cameras), harvester_mod.MAX_HOTSET_CAMERAS)
        self.assertEqual(len(cameras), 57)

    def test_parser_mode_mutex(self) -> None:
        """Verify CLI enforces mutual exclusivity between daemon, once, status, and health."""
        parser = harvester_mod.build_parser()
        with patch("sys.stderr", new_callable=io.StringIO):
            with self.assertRaises(SystemExit):
                parser.parse_args([])

            with self.assertRaises(SystemExit):
                parser.parse_args(["--daemon", "--once"])

        args_once = parser.parse_args(["--once", "--interval", "180"])
        self.assertTrue(args_once.once)
        self.assertEqual(args_once.interval, 180)


class TestSnapshotFetchingAndTimeout(unittest.TestCase):
    """Tests for snapshot fetching, URL building, and timeout / error resilience."""

    def test_build_snapshot_urls(self) -> None:
        """Verify URL generation with and without proxy base."""
        cam = {
            "id": "cam_test_01",
            "snapshotUrl": "https://traffic.example.com/snapshot?id=cam_test_01",
        }
        # Direct upstream only
        urls_direct = harvester_mod.build_snapshot_urls(cam, "")
        self.assertEqual(len(urls_direct), 1)
        self.assertIn("cam_test_01", urls_direct[0])

        # With proxy base: proxy URL should precede upstream URL
        urls_proxy = harvester_mod.build_snapshot_urls(cam, "http://localhost:8080")
        self.assertEqual(len(urls_proxy), 2)
        self.assertTrue(urls_proxy[0].startswith("http://localhost:8080/cam_test_01"))
        self.assertTrue(urls_proxy[1].startswith("https://traffic.example.com/snapshot"))

    @patch("urllib.request.urlopen")
    def test_fetch_snapshot_rejects_placeholder_resolution(self, mock_urlopen: MagicMock) -> None:
        """Verify 1x1 upstream frames are rejected before analysis."""
        mock_resp = MagicMock()
        mock_resp.status = 200
        mock_resp.read.return_value = TINY_JPEG_BYTES
        mock_resp.__enter__.return_value = mock_resp
        mock_urlopen.return_value = mock_resp

        cam = {"id": "cam_tiny", "snapshotUrl": "https://traffic.example.com/snap.jpg"}
        payload, source, err = harvester_mod.fetch_snapshot(cam, "", timeout=5.0)

        self.assertIsNone(payload)
        self.assertIn("resolution_too_small", err)

    @patch.dict("os.environ", {"DEV_ACCESS_TOKEN": "staging-token"}, clear=False)
    @patch("urllib.request.urlopen")
    def test_dev_token_only_sent_to_proxy(self, mock_urlopen: MagicMock) -> None:
        """Verify X-Dev-Token is scoped to the local proxy and never reaches upstream."""
        ok_response = MagicMock()
        ok_response.status = 200
        ok_response.read.return_value = VALID_JPEG_BYTES
        ok_response.__enter__.return_value = ok_response

        # First candidate (proxy) times out, second candidate (upstream) succeeds.
        mock_urlopen.side_effect = [TimeoutError("proxy timeout"), ok_response]

        cam = {"id": "cam_token", "snapshotUrl": "https://traffic.example.com/snap.jpg"}
        payload, source, err = harvester_mod.fetch_snapshot(
            cam, "http://localhost:3000/api/snapshot", timeout=5.0
        )

        self.assertIsNotNone(payload)
        self.assertEqual(err, "")
        self.assertEqual(mock_urlopen.call_count, 2)

        proxy_request = mock_urlopen.call_args_list[0].args[0]
        upstream_request = mock_urlopen.call_args_list[1].args[0]
        self.assertEqual(proxy_request.get_header("X-dev-token"), "staging-token")
        self.assertIsNone(upstream_request.get_header("X-dev-token"))
        self.assertTrue(source.startswith("https://traffic.example.com"))

    @patch.dict("os.environ", {"DEV_ACCESS_TOKEN": "staging-token"}, clear=False)
    @patch("urllib.request.urlopen")
    def test_dev_token_never_sent_without_proxy(self, mock_urlopen: MagicMock) -> None:
        """Verify upstream-only fetches never attach the dev token."""
        ok_response = MagicMock()
        ok_response.status = 200
        ok_response.read.return_value = VALID_JPEG_BYTES
        ok_response.__enter__.return_value = ok_response
        mock_urlopen.return_value = ok_response

        cam = {"id": "cam_token_up", "snapshotUrl": "https://traffic.example.com/snap.jpg"}
        harvester_mod.fetch_snapshot(cam, "", timeout=5.0)

        request = mock_urlopen.call_args.args[0]
        self.assertIsNone(request.get_header("X-dev-token"))

    @patch("urllib.request.urlopen")
    def test_fetch_snapshot_success(self, mock_urlopen: MagicMock) -> None:
        """Verify successful snapshot retrieval and JPEG validation."""
        mock_resp = MagicMock()
        mock_resp.status = 200
        mock_resp.read.return_value = VALID_JPEG_BYTES
        mock_resp.__enter__.return_value = mock_resp
        mock_urlopen.return_value = mock_resp

        cam = {"id": "cam_ok", "snapshotUrl": "https://traffic.example.com/snap.jpg"}
        payload, source, err = harvester_mod.fetch_snapshot(cam, "", timeout=5.0)

        self.assertIsNotNone(payload)
        self.assertEqual(payload, VALID_JPEG_BYTES)
        self.assertEqual(err, "")
        self.assertTrue(source.startswith("https://traffic.example.com"))

    @patch("urllib.request.urlopen")
    def test_fetch_snapshot_timeout_handling(self, mock_urlopen: MagicMock) -> None:
        """Verify TimeoutError is caught gracefully and returns error string."""
        mock_urlopen.side_effect = TimeoutError("Connection timed out")

        cam = {"id": "cam_timeout", "snapshotUrl": "https://traffic.example.com/snap.jpg"}
        payload, source, err = harvester_mod.fetch_snapshot(cam, "", timeout=1.0)

        self.assertIsNone(payload)
        self.assertEqual(source, "")
        self.assertIn("timed out", err.lower())

    @patch("urllib.request.urlopen")
    def test_fetch_snapshot_corrupt_payload(self, mock_urlopen: MagicMock) -> None:
        """Verify corrupt non-JPEG payload fails JPEG validation."""
        mock_resp = MagicMock()
        mock_resp.status = 200
        mock_resp.read.return_value = b"NOT_A_VALID_JPEG_IMAGE"
        mock_resp.__enter__.return_value = mock_resp
        mock_urlopen.return_value = mock_resp

        cam = {"id": "cam_corrupt", "snapshotUrl": "https://traffic.example.com/snap.jpg"}
        payload, source, err = harvester_mod.fetch_snapshot(cam, "", timeout=5.0)

        self.assertIsNone(payload)
        self.assertIn("invalid snapshot", err)


class TestVerdictWritingAndSnapshotArchiving(unittest.TestCase):
    """Tests for persisting verdicts to SQLite and archiving snapshots to disk."""

    def setUp(self) -> None:
        self.temp_dir = tempfile.TemporaryDirectory()
        self.proof_root = Path(self.temp_dir.name) / "proofs"
        self.db_path = Path(self.temp_dir.name) / "camera-view.db"

    def tearDown(self) -> None:
        self.temp_dir.cleanup()

    def test_archive_snapshot_atomic_write(self) -> None:
        """Verify atomic snapshot storage at YYYY-MM-DD/<camera_id>/<timestamp>_sample.jpg."""
        epoch = 1793681792  # 2026-11-03
        saved_path, created = harvester_mod.archive_snapshot(
            VALID_JPEG_BYTES,
            self.proof_root,
            "cam_arch_01",
            epoch,
        )
        self.assertTrue(created)
        self.assertTrue(saved_path.is_file())
        self.assertEqual(saved_path.read_bytes(), VALID_JPEG_BYTES)

        expected_subpath = Path("2026-11-03") / "cam_arch_01" / "1793681792_sample.jpg"
        self.assertTrue(str(saved_path).endswith(str(expected_subpath)))

        # Idempotency: second call returns created=False
        saved_path2, created2 = harvester_mod.archive_snapshot(
            VALID_JPEG_BYTES,
            self.proof_root,
            "cam_arch_01",
            epoch,
        )
        self.assertFalse(created2)
        self.assertEqual(saved_path, saved_path2)

    def test_run_cycle_persists_verdicts_to_sqlite(self) -> None:
        """Verify run_cycle persists verdicts into hotset_verdict_history."""
        conn = sqlite3.connect(str(self.db_path))
        ingest_mod.ensure_history_schema(conn)

        cam = {
            "id": "cam_cycle_01",
            "snapshotUrl": "https://traffic.example.com/snap.jpg",
            "capacity": 100.0,
        }

        with patch.object(harvester_mod, "fetch_snapshot", return_value=(VALID_JPEG_BYTES, "https://traffic.example.com/snap.jpg", "")):
            args = argparse.Namespace(
                proof_root=self.proof_root,
                db_path=self.db_path,
                batch_size=500,
                dry_run=False,
                concurrency=1,
                snapshot_proxy_url="",
                timeout=5.0,
            )
            summary = harvester_mod.run_cycle(
                args=args,
                cameras=[cam],
                capacities={"cam_cycle_01": 100.0},
                regions_cache={},
                cv_client=None,
                db_connection=conn,
            )

        self.assertTrue(summary["success"])
        self.assertEqual(summary["cameras_harvested"], 1)
        self.assertEqual(summary["records_inserted"], 1)

        cursor = conn.cursor()
        cursor.execute("SELECT camera_id, severity, source FROM hotset_verdict_history WHERE camera_id='cam_cycle_01'")
        row = cursor.fetchone()
        self.assertIsNotNone(row)
        self.assertEqual(row[0], "cam_cycle_01")
        self.assertEqual(row[2], "harvester_daemon")
        conn.close()


class TestSoakTelemetryAndTracking(unittest.TestCase):
    """Tests for tracking unbroken soak duration, consecutive cycles, and emitting telemetry."""

    def test_unbroken_soak_seconds_calculation(self) -> None:
        """Verify unbroken soak seconds computation from daemon_started_at."""
        t0 = datetime.datetime(2026, 11, 3, 10, 0, 0, tzinfo=datetime.timezone.utc)
        t1 = datetime.datetime(2026, 11, 3, 11, 30, 0, tzinfo=datetime.timezone.utc)

        state = {"daemon_started_at": "2026-11-03T10:00:00.000000000Z"}
        soak_sec = harvester_mod.unbroken_soak_seconds(state, t1)
        self.assertEqual(soak_sec, 5400.0)

    def test_apply_cycle_to_state_streaks(self) -> None:
        """Verify consecutive cycle streak increments on success and resets on failure."""
        now = datetime.datetime(2026, 11, 3, 10, 0, 0, tzinfo=datetime.timezone.utc)
        state = harvester_mod.default_state(now)
        state["daemon_started_at"] = "2026-11-03T10:00:00.000000000Z"

        # Cycle 1: Success
        summary_success = {
            "success": True,
            "harvested_frames": 57,
            "completed_at": "2026-11-03T10:05:00.000000000Z",
            "duration_seconds": 12.5,
            "cameras_total": 57,
            "cameras_harvested": 57,
            "cameras_failed": 0,
            "frames_analyzed": 57,
            "records_attempted": 57,
            "records_inserted": 57,
            "methods": {"image_heuristic": 57},
        }
        state = harvester_mod.apply_cycle_to_state(state, summary_success)
        self.assertEqual(state["completed_cycles"], 1)
        self.assertEqual(state["consecutive_successful_cycles"], 1)
        self.assertEqual(state["total_frames_harvested"], 57)

        # Cycle 2: Success
        state = harvester_mod.apply_cycle_to_state(state, summary_success)
        self.assertEqual(state["completed_cycles"], 2)
        self.assertEqual(state["consecutive_successful_cycles"], 2)

        # Cycle 3: Failure
        summary_failure = {
            "success": False,
            "error": "upstream network gateway error",
            "harvested_frames": 0,
            "completed_at": "2026-11-03T10:15:00.000000000Z",
        }
        state = harvester_mod.apply_cycle_to_state(state, summary_failure)
        self.assertEqual(state["completed_cycles"], 3)
        self.assertEqual(state["consecutive_successful_cycles"], 0)
        self.assertEqual(state["failed_cycles"], 1)
        self.assertEqual(state["last_error"], "upstream network gateway error")

    def test_build_telemetry_payload(self) -> None:
        """Verify building comprehensive soak telemetry payload."""
        now = datetime.datetime(2026, 11, 3, 10, 0, 0, tzinfo=datetime.timezone.utc)
        state = harvester_mod.default_state(now)
        state["daemon_started_at"] = "2026-11-03T10:00:00.000000000Z"
        state["completed_cycles"] = 10
        state["consecutive_successful_cycles"] = 10
        state["total_frames_harvested"] = 570
        state["last_cycle_timestamp"] = "2026-11-03T10:50:00.000000000Z"

        args = argparse.Namespace(
            interval=300,
            coverage_window_hours=336,
            dry_run=False,
            db_path=Path("services/go-server/data/camera-view.db"),
            proof_root=Path("services/go-server/data/proofs"),
        )
        coverage = {
            "main": {"window_hours": 336, "coverage_ratio": 0.85, "observed_bins": 3400},
            "recent_72h": {"window_hours": 72, "coverage_ratio": 0.92},
            "coverage_14d": 0.85,
            "coverage_72h": 0.92,
        }

        telemetry = harvester_mod.build_telemetry(state, 57, coverage, args)
        self.assertEqual(telemetry["completed_cycles"], 10)
        self.assertEqual(telemetry["consecutive_successful_cycles"], 10)
        self.assertEqual(telemetry["total_frames_harvested"], 570)
        self.assertEqual(telemetry["cameras_configured"], 57)
        self.assertEqual(telemetry["coverage_window_hours"], 336)
        self.assertEqual(telemetry["coverage_14d"], 0.85)
        self.assertEqual(telemetry["coverage_72h"], 0.92)


class TestHealthCheckAndOnceMode(unittest.TestCase):
    """Tests for health check assertions and --once execution mode."""

    def setUp(self) -> None:
        self.temp_dir = tempfile.TemporaryDirectory()
        self.state_path = Path(self.temp_dir.name) / "soak_state.json"
        self.telemetry_path = Path(self.temp_dir.name) / "telemetry.json"
        self.db_path = Path(self.temp_dir.name) / "camera-view.db"
        self.proof_root = Path(self.temp_dir.name) / "proofs"
        self.cameras_file = Path(self.temp_dir.name) / "cameras.json"

        # Create 1 hotset camera in catalog
        self.cameras_file.write_text(
            json.dumps([{"id": "cam_health_01", "isOnline": True, "hotset": True}]),
            encoding="utf-8",
        )

    def tearDown(self) -> None:
        self.temp_dir.cleanup()

    def test_health_check_healthy(self) -> None:
        """Verify health_check passes when soak state is active and fresh."""
        now = datetime.datetime.now(datetime.timezone.utc)
        recent_iso = (now - datetime.timedelta(seconds=120)).strftime("%Y-%m-%dT%H:%M:%S.000000000Z")

        state = {
            "daemon_started_at": (now - datetime.timedelta(hours=2)).strftime("%Y-%m-%dT%H:%M:%S.000000000Z"),
            "last_cycle_timestamp": recent_iso,
            "consecutive_successful_cycles": 5,
            "unbroken_soak_seconds": 7200.0,
            "clean_shutdown": False,
        }
        coverage = {"main": {"coverage_ratio": 0.88}}
        args = argparse.Namespace(
            health_max_age_seconds=600,
            health_min_coverage=0.0,
            health_min_soak_seconds=0.0,
            interval=300,
            coverage_window_hours=336,
        )

        healthy, reasons = harvester_mod.health_check(state, True, coverage, args, now)
        self.assertTrue(healthy)
        self.assertEqual(len(reasons), 0)

    def test_health_check_fails_on_missing_or_stale_state(self) -> None:
        """Verify health_check fails when state file does not exist or last cycle is stale."""
        now = datetime.datetime.now(datetime.timezone.utc)
        args = argparse.Namespace(
            health_max_age_seconds=300,
            health_min_coverage=0.0,
            health_min_soak_seconds=0.0,
            interval=300,
            coverage_window_hours=336,
        )
        # Missing state file
        healthy, reasons = harvester_mod.health_check({}, False, {}, args, now)
        self.assertFalse(healthy)
        self.assertIn("soak state file does not exist", reasons)

        # Stale state (last cycle 1 hour ago > max age 300s)
        stale_iso = (now - datetime.timedelta(hours=1)).strftime("%Y-%m-%dT%H:%M:%S.000000000Z")
        stale_state = {
            "daemon_started_at": (now - datetime.timedelta(hours=5)).strftime("%Y-%m-%dT%H:%M:%S.000000000Z"),
            "last_cycle_timestamp": stale_iso,
            "consecutive_successful_cycles": 5,
        }
        healthy_stale, reasons_stale = harvester_mod.health_check(stale_state, True, {}, args, now)
        self.assertFalse(healthy_stale)
        self.assertTrue(any("stale" in r for r in reasons_stale))

    def test_cli_once_mode_execution(self) -> None:
        """Verify executing service with --once completes cycle and persists state and telemetry."""
        with patch.object(harvester_mod, "fetch_snapshot", return_value=(VALID_JPEG_BYTES, "http://example.com", "")):
            argv = [
                "--once",
                "--cameras-file", str(self.cameras_file),
                "--state-path", str(self.state_path),
                "--telemetry-out", str(self.telemetry_path),
                "--db-path", str(self.db_path),
                "--proof-root", str(self.proof_root),
                "--cv-engine-url", "",
            ]
            exit_code = harvester_mod.main(argv)
            self.assertEqual(exit_code, 0)

            # State and telemetry must be written to disk
            self.assertTrue(self.state_path.is_file())
            self.assertTrue(self.telemetry_path.is_file())

            with open(self.state_path, "r", encoding="utf-8") as f:
                state_data = json.load(f)
            self.assertEqual(state_data["completed_cycles"], 1)
            self.assertEqual(state_data["consecutive_successful_cycles"], 1)

    def test_cli_once_mode_dry_run_does_not_persist(self) -> None:
        """Verify executing service with --once and --dry-run does not write to disk."""
        with patch.object(harvester_mod, "fetch_snapshot", return_value=(VALID_JPEG_BYTES, "http://example.com", "")):
            argv = [
                "--once",
                "--dry-run",
                "--cameras-file", str(self.cameras_file),
                "--state-path", str(self.state_path),
                "--telemetry-out", str(self.telemetry_path),
                "--db-path", str(self.db_path),
                "--proof-root", str(self.proof_root),
                "--cv-engine-url", "",
            ]
            exit_code = harvester_mod.main(argv)
            self.assertEqual(exit_code, 0)

            # In dry-run mode, state and telemetry files should not be created
            self.assertFalse(self.state_path.exists())
            self.assertFalse(self.telemetry_path.exists())

    def test_cli_subprocess_status(self) -> None:
        """Verify calling service directly via subprocess with --status."""
        self.state_path.write_text(
            json.dumps({"completed_cycles": 42, "daemon_started_at": "2026-11-03T00:00:00.000000000Z"}),
            encoding="utf-8",
        )
        script_path = REPO_ROOT / "tools" / "harvester" / "service.py"
        res = subprocess.run(
            [
                sys.executable, str(script_path),
                "--status",
                "--state-path", str(self.state_path),
                "--telemetry-out", str(self.telemetry_path),
                "--cameras-file", str(self.cameras_file),
                "--db-path", str(self.db_path),
            ],
            capture_output=True,
            text=True,
        )
        self.assertEqual(res.returncode, 0)
        status_json = json.loads(res.stdout)
        self.assertEqual(status_json["completed_cycles"], 42)


class TestSoakWindowAnchoring(unittest.TestCase):
    """Tests that soak time is anchored to a single daemon process lifetime."""

    def test_prepare_state_resets_soak_on_process_start(self) -> None:
        """Verify a new process start zeroes the persisted soak and re-anchors it."""
        old_now = datetime.datetime(2026, 11, 3, 8, 0, 0, tzinfo=datetime.timezone.utc)
        now = datetime.datetime(2026, 11, 3, 10, 0, 0, tzinfo=datetime.timezone.utc)
        state = harvester_mod.default_state(old_now)
        state["daemon_started_at"] = harvester_mod.format_timestamp(old_now)
        state["unbroken_soak_seconds"] = 7200.0
        state["clean_shutdown"] = True
        state["last_cycle_timestamp"] = ""

        prepared, notes = harvester_mod.prepare_state_for_run(state, True, now, 300)

        self.assertEqual(prepared["unbroken_soak_seconds"], 0.0)
        self.assertEqual(prepared["daemon_started_at"], harvester_mod.format_timestamp(now))
        self.assertGreaterEqual(int(prepared["soak_reset_count"]), 1)
        self.assertIn("soak_window_reset_on_process_start", notes)

    def test_soak_bounded_by_current_process_lifetime(self) -> None:
        """Verify a daemon-owned state can never report more than its process uptime."""
        now = datetime.datetime.now(datetime.timezone.utc)
        state = {
            "daemon_started_at": harvester_mod.format_timestamp(
                now - datetime.timedelta(hours=3)
            ),
            "daemon_pid": os.getpid(),
        }
        with patch.object(harvester_mod, "process_uptime_seconds", return_value=120.0):
            self.assertEqual(harvester_mod.unbroken_soak_seconds(state, now), 120.0)

    def test_health_observer_reads_daemon_wall_soak(self) -> None:
        """Verify a separate --health process measures the daemon's own soak window."""
        now = datetime.datetime.now(datetime.timezone.utc)
        state = {
            "daemon_started_at": harvester_mod.format_timestamp(
                now - datetime.timedelta(hours=2)
            ),
            "daemon_pid": os.getpid() + 1,
        }
        self.assertAlmostEqual(harvester_mod.unbroken_soak_seconds(state, now), 7200.0, places=0)


class TestFrozenFrameDetection(unittest.TestCase):
    """Tests for rejecting identical upstream frames per camera."""

    def setUp(self) -> None:
        harvester_mod.reset_frame_history()

    def tearDown(self) -> None:
        harvester_mod.reset_frame_history()

    def test_duplicate_frame_per_camera(self) -> None:
        """Verify two identical consecutive frames for one camera are detected."""
        self.assertFalse(harvester_mod.is_duplicate_frame("cam_frozen", VALID_JPEG_BYTES))
        self.assertTrue(harvester_mod.is_duplicate_frame("cam_frozen", VALID_JPEG_BYTES))
        self.assertFalse(harvester_mod.is_duplicate_frame("cam_other", VALID_JPEG_BYTES))

    def test_reset_frame_history(self) -> None:
        """Verify a new daemon run forgets the previous frame hashes."""
        harvester_mod.is_duplicate_frame("cam_frozen", VALID_JPEG_BYTES)
        harvester_mod.reset_frame_history()
        self.assertFalse(harvester_mod.is_duplicate_frame("cam_frozen", VALID_JPEG_BYTES))

    def test_run_cycle_rejects_frozen_camera(self) -> None:
        """Verify run_cycle reports a failure when every camera serves a frozen frame."""
        temp_dir = tempfile.TemporaryDirectory()
        self.addCleanup(temp_dir.cleanup)
        conn = sqlite3.connect(":memory:")
        self.addCleanup(conn.close)
        ingest_mod.ensure_history_schema(conn)

        cam = {"id": "cam_frozen_cycle", "snapshotUrl": "https://example.com/snap.jpg"}
        args = argparse.Namespace(
            proof_root=Path(temp_dir.name) / "proofs",
            db_path=Path(temp_dir.name) / "camera-view.db",
            batch_size=500,
            dry_run=False,
            concurrency=1,
            snapshot_proxy_url="",
            timeout=5.0,
        )

        with patch.object(
            harvester_mod,
            "fetch_snapshot",
            return_value=(VALID_JPEG_BYTES, "https://example.com/snap.jpg", ""),
        ):
            first = harvester_mod.run_cycle(
                args, [cam], {"cam_frozen_cycle": 100.0}, {}, None, conn
            )
            second = harvester_mod.run_cycle(
                args, [cam], {"cam_frozen_cycle": 100.0}, {}, None, conn
            )

        self.assertTrue(first["success"])
        self.assertFalse(second["success"])
        self.assertEqual(second["cameras_harvested"], 0)
        self.assertIn("duplicate_frame", second["errors"][0]["error"])


class TestDatabasePragmasAndCoverage(unittest.TestCase):
    """Tests for SQLite WAL pragmas and full-roster coverage denominators."""

    def setUp(self) -> None:
        self.temp_dir = tempfile.TemporaryDirectory()
        self.db_path = Path(self.temp_dir.name) / "camera-view.db"

    def tearDown(self) -> None:
        self.temp_dir.cleanup()

    def test_connect_database_uses_wal_and_busy_timeout(self) -> None:
        """Verify harvester database connections enable WAL and a 5000 ms timeout."""
        conn = harvester_mod.connect_database(self.db_path)
        try:
            self.assertEqual(str(conn.execute("PRAGMA journal_mode").fetchone()[0]).lower(), "wal")
            self.assertGreaterEqual(int(conn.execute("PRAGMA busy_timeout").fetchone()[0]), 5000)
        finally:
            conn.close()

    def test_coverage_uses_full_roster_denominator(self) -> None:
        """Verify coverage expected bins are computed over all 57 Hot Set cameras."""
        conn = sqlite3.connect(str(self.db_path))
        ingest_mod.ensure_history_schema(conn)
        now = datetime.datetime(2026, 11, 9, 12, 0, 0, tzinfo=datetime.timezone.utc)
        observed_at = harvester_mod.format_timestamp(now - datetime.timedelta(minutes=5))
        ingest_mod.insert_verdict_batch(
            conn,
            [
                ingest_mod.VerdictRecord(
                    camera_id="cam_coverage",
                    direction="",
                    region_id="",
                    pcu_total=10.0,
                    saturation_index=0.3,
                    severity="CLEAR",
                    severity_class="CLEAR",
                    confidence=0.9,
                    source="harvester_daemon",
                    observed_at=observed_at,
                    method="heuristic",
                )
            ],
        )

        coverage = harvester_mod.compute_coverage(conn, ["cam_coverage"], now, 72)
        conn.close()

        expected_per_camera = int((72 * 3600) / (harvester_mod.COVERAGE_BIN_MINUTES * 60))
        self.assertEqual(
            coverage["main"]["expected_bins"],
            expected_per_camera * harvester_mod.MAX_HOTSET_CAMERAS,
        )
        self.assertEqual(coverage["main"]["observed_bins"], 1)


class TestHealthResetAndDatabaseFailure(unittest.TestCase):
    """Tests for health failures caused by soak resets or an inaccessible database."""

    def setUp(self) -> None:
        self.temp_dir = tempfile.TemporaryDirectory()
        self.state_path = Path(self.temp_dir.name) / "soak_state.json"
        self.telemetry_path = Path(self.temp_dir.name) / "telemetry.json"
        self.db_path = Path(self.temp_dir.name) / "missing.db"
        self.cameras_file = Path(self.temp_dir.name) / "cameras.json"
        self.cameras_file.write_text(
            json.dumps([{"id": "cam_health_02", "isOnline": True, "hotset": True}]),
            encoding="utf-8",
        )

    def tearDown(self) -> None:
        self.temp_dir.cleanup()

    def _recent_state(self) -> Dict[str, Any]:
        now = datetime.datetime.now(datetime.timezone.utc)
        return {
            "daemon_started_at": (now - datetime.timedelta(hours=1)).strftime(
                "%Y-%m-%dT%H:%M:%S.000000000Z"
            ),
            "last_cycle_timestamp": (now - datetime.timedelta(seconds=60)).strftime(
                "%Y-%m-%dT%H:%M:%S.000000000Z"
            ),
            "consecutive_successful_cycles": 4,
            "unbroken_soak_seconds": 3600.0,
            "clean_shutdown": False,
        }

    def test_health_fails_when_soak_was_reset(self) -> None:
        """Verify a zero persisted soak marks the daemon unhealthy."""
        now = datetime.datetime.now(datetime.timezone.utc)
        state = self._recent_state()
        state["unbroken_soak_seconds"] = 0.0
        args = argparse.Namespace(
            health_max_age_seconds=600,
            health_min_coverage=0.0,
            health_min_soak_seconds=0.0,
            interval=300,
            coverage_window_hours=336,
        )
        healthy, reasons = harvester_mod.health_check(state, True, {}, args, now)
        self.assertFalse(healthy)
        self.assertTrue(any("reset at daemon start" in reason for reason in reasons))

    def test_health_fails_below_coverage_threshold(self) -> None:
        """Verify coverage below the 0.90 default threshold is unhealthy."""
        now = datetime.datetime.now(datetime.timezone.utc)
        args = argparse.Namespace(
            health_max_age_seconds=600,
            health_min_coverage=0.90,
            health_min_soak_seconds=0.0,
            interval=300,
            coverage_window_hours=336,
        )
        healthy, reasons = harvester_mod.health_check(
            self._recent_state(), True, {"main": {"coverage_ratio": 0.5}}, args, now
        )
        self.assertFalse(healthy)
        self.assertTrue(any("coverage_ratio" in reason for reason in reasons))

    def test_health_cli_returns_1_for_missing_database(self) -> None:
        """Verify --health exits 1 when the SQLite database is inaccessible."""
        self.state_path.write_text(json.dumps(self._recent_state()), encoding="utf-8")
        exit_code = harvester_mod.main(
            [
                "--health",
                "--cameras-file", str(self.cameras_file),
                "--state-path", str(self.state_path),
                "--telemetry-out", str(self.telemetry_path),
                "--db-path", str(self.db_path),
                "--health-min-coverage", "0",
                "--health-max-age-seconds", "600",
            ]
        )
        self.assertEqual(exit_code, 1)


if __name__ == "__main__":
    unittest.main()
