#!/usr/bin/env python3
"""
Unit and integration tests for tools/eval/ingest_proof_archive.py.

Sprint 11 Wave 1 (Issue #396 / S11-T2):
  * Test discovering proof frames from directory structure <camera_id>/<timestamp>_<event>.jpg
  * Test timestamp parsing (Unix epoch -> UTC RFC3339Nano)
  * Test JPEG validation (valid vs corrupt/empty files)
  * Test SQLite ingestion into hotset_verdict_history:
    - Table creation
    - Schema migration (0007 -> 0008)
    - Idempotency on duplicate (camera_id, direction, observed_at)
  * Test batch transaction commit and rollback on failure
  * Test CLI arguments (--archive-dir, --db-path, --dry-run, --limit, --verify)
  * Test generation of replay manifest index

Adheres strictly to AGENTS.md (zero-emoji, English only, clean tear-down of temp files).
"""

from __future__ import annotations

import argparse
import datetime
import json
import sqlite3
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from typing import Any, Dict, List

# Ensure repository root is on Python path
REPO_ROOT = Path(__file__).resolve().parent.parent.parent
if str(REPO_ROOT) not in sys.path:
    sys.path.insert(0, str(REPO_ROOT))

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


class TestProofFrameDiscovery(unittest.TestCase):
    """Tests for discovering proof frames from the archive directory structure."""

    def setUp(self) -> None:
        self.temp_dir = tempfile.TemporaryDirectory()
        self.archive_dir = Path(self.temp_dir.name) / "proofs"
        self.archive_dir.mkdir(parents=True, exist_ok=True)

    def tearDown(self) -> None:
        self.temp_dir.cleanup()

    def test_parse_frame_filename(self) -> None:
        """Verify parsing valid and invalid frame filenames."""
        # Valid cases
        parsed = ingest_mod.parse_frame_filename("1793681792_sample.jpg")
        self.assertIsNotNone(parsed)
        assert parsed is not None
        self.assertEqual(parsed[0], 1793681792)
        self.assertEqual(parsed[1], "sample")

        parsed_state = ingest_mod.parse_frame_filename("1793681826_state_change.jpeg")
        self.assertIsNotNone(parsed_state)
        assert parsed_state is not None
        self.assertEqual(parsed_state[0], 1793681826)
        self.assertEqual(parsed_state[1], "state_change")

        # Invalid cases
        self.assertIsNone(ingest_mod.parse_frame_filename("invalid_format.jpg"))
        self.assertIsNone(ingest_mod.parse_frame_filename("not_an_image.txt"))
        self.assertIsNone(ingest_mod.parse_frame_filename(".DS_Store"))
        self.assertIsNone(ingest_mod.parse_frame_filename("1793681792.png"))

    def test_discover_frames_directory_structure(self) -> None:
        """Verify discovering frames organized as <camera_id>/<timestamp>_<event>.jpg."""
        cam1 = self.archive_dir / "cam_alpha"
        cam2 = self.archive_dir / "cam_beta"
        cam1.mkdir()
        cam2.mkdir()

        (cam1 / "1793681792_sample.jpg").write_bytes(TINY_JPEG_BYTES)
        (cam1 / "1793681826_state_change.jpg").write_bytes(TINY_JPEG_BYTES)
        (cam2 / "1793681900_sample.jpg").write_bytes(TINY_JPEG_BYTES)

        # Non-matching artifacts should be ignored or reported as skipped
        (cam1 / "notes.txt").write_text("irrelevant notes", encoding="utf-8")
        (self.archive_dir / "root_artifact.jpg").write_bytes(TINY_JPEG_BYTES)

        valid_frames, invalid_entries = ingest_mod.discover_frames(self.archive_dir)

        self.assertEqual(len(valid_frames), 3)
        cam_ids = {f.camera_id for f in valid_frames}
        self.assertEqual(cam_ids, {"cam_alpha", "cam_beta"})

        # Check timestamp ordering and attributes
        timestamps = [f.timestamp for f in valid_frames]
        self.assertEqual(timestamps, sorted(timestamps))

        frame_events = {f.event for f in valid_frames}
        self.assertEqual(frame_events, {"sample", "state_change"})

    def test_discover_frames_with_limit(self) -> None:
        """Verify discovering frames with multiple timestamps."""
        cam = self.archive_dir / "cam_gamma"
        cam.mkdir()
        for i in range(5):
            ts = 1793681000 + i * 60
            (cam / f"{ts}_sample.jpg").write_bytes(TINY_JPEG_BYTES)

        valid_frames, _ = ingest_mod.discover_frames(self.archive_dir)
        self.assertEqual(len(valid_frames), 5)


class TestTimestampParsing(unittest.TestCase):
    """Tests for timestamp parsing and UTC RFC3339Nano string formatting."""

    def test_to_rfc3339nano_fixed_layout(self) -> None:
        """Verify RFC3339Nano formatting matches Go fixed layout (2006-01-02T15:04:05.000000000Z)."""
        # 1793681792 Unix seconds is 2026-11-03 04:56:32 UTC
        epoch = 1793681792
        formatted = ingest_mod.to_rfc3339nano(epoch)
        self.assertEqual(formatted, "2026-11-03T04:56:32.000000000Z")
        self.assertEqual(len(formatted), 30)

    def test_epoch_zero(self) -> None:
        """Verify epoch 0 converts to 1970 UTC RFC3339Nano."""
        self.assertEqual(ingest_mod.to_rfc3339nano(0), "1970-01-01T00:00:00.000000000Z")

    def test_parse_timestamp_variants(self) -> None:
        """Verify parse_timestamp handles str ISO formats and datetime objects."""
        now = datetime.datetime.now(datetime.timezone.utc)
        parsed_dt = ingest_mod.parse_timestamp(now)
        self.assertIsNotNone(parsed_dt)
        assert parsed_dt is not None
        self.assertEqual(parsed_dt.tzinfo, datetime.timezone.utc)

        parsed_iso = ingest_mod.parse_timestamp("2026-11-03T04:56:32.000000000Z")
        self.assertIsNotNone(parsed_iso)
        assert parsed_iso is not None
        self.assertEqual(int(parsed_iso.timestamp()), 1793681792)
        parsed_nano = ingest_mod.parse_timestamp("2026-11-03T04:56:32.123456789Z")
        self.assertIsNotNone(parsed_nano)
        assert parsed_nano is not None
        self.assertEqual(int(parsed_nano.timestamp()), 1793681792)
        self.assertEqual(parsed_nano.microsecond, 123456)


        parsed_space = ingest_mod.parse_timestamp("2026-11-03 04:56:32")
        self.assertIsNotNone(parsed_space)
        assert parsed_space is not None
        self.assertEqual(int(parsed_space.timestamp()), 1793681792)

        # Invalid timestamp string
        self.assertIsNone(ingest_mod.parse_timestamp("not-a-date"))


class TestJpegValidation(unittest.TestCase):
    """Tests for JPEG magic byte checking, integrity, and resolution validation."""

    def test_valid_jpeg(self) -> None:
        """Verify a full-resolution JPEG passes validation."""
        valid, reason = ingest_mod.validate_jpeg_bytes(VALID_JPEG_BYTES)
        self.assertTrue(valid)
        self.assertEqual(reason, "")

    def test_placeholder_resolution_rejected(self) -> None:
        """Verify 1x1 placeholder frames are rejected as resolution_too_small."""
        valid, reason = ingest_mod.validate_jpeg_bytes(TINY_JPEG_BYTES)
        self.assertFalse(valid)
        self.assertEqual(reason, "resolution_too_small")

    def test_minimum_resolution_boundary(self) -> None:
        """Verify the 320x240 floor is inclusive and sub-floor frames are rejected."""
        valid_at_floor, reason = ingest_mod.validate_jpeg_bytes(make_jpeg(320, 240))
        self.assertTrue(valid_at_floor, reason)

        too_narrow, reason_width = ingest_mod.validate_jpeg_bytes(make_jpeg(319, 240))
        self.assertFalse(too_narrow)
        self.assertEqual(reason_width, "resolution_too_small")

        too_short, reason_height = ingest_mod.validate_jpeg_bytes(make_jpeg(320, 239))
        self.assertFalse(too_short)
        self.assertEqual(reason_height, "resolution_too_small")

    def test_empty_file(self) -> None:
        """Verify 0-byte file is rejected."""
        valid, reason = ingest_mod.validate_jpeg_bytes(b"")
        self.assertFalse(valid)
        self.assertEqual(reason, "empty_file")

    def test_corrupt_non_jpeg(self) -> None:
        """Verify non-JPEG random payload is rejected with invalid_magic."""
        valid, reason = ingest_mod.validate_jpeg_bytes(b"PNG\r\n\x1a\n...")
        self.assertFalse(valid)
        self.assertEqual(reason, "invalid_magic")

    def test_truncated_jpeg_header(self) -> None:
        """Verify truncated header (< 4 bytes) is rejected with truncated_header."""
        valid, reason = ingest_mod.validate_jpeg_bytes(b"\xff\xd8")
        self.assertFalse(valid)
        self.assertEqual(reason, "truncated_header")

    def test_truncated_eoi_marker(self) -> None:
        """Verify missing EOI marker is detected as missing_eoi."""
        # Strip the trailing EOI marker
        truncated = TINY_JPEG_BYTES[:-2]
        valid, reason = ingest_mod.validate_jpeg_bytes(truncated)
        self.assertFalse(valid)
        self.assertEqual(reason, "missing_eoi")


class TestCVResponseValidation(unittest.TestCase):
    """Tests for strict validation of local CV engine payloads."""

    def test_rejects_mock_and_standby_sources(self) -> None:
        """Verify mock and standby payloads are never accepted as observations."""
        for source in ("mock", "standby_dummy", "MOCK"):
            valid, reason = ingest_mod.validate_cv_response(
                {"source": source, "flood_severity": "CLEAR"}
            )
            self.assertFalse(valid, source)
            self.assertTrue(reason.startswith("rejected_source:"), reason)

    def test_rejects_error_details(self) -> None:
        """Verify payloads carrying error details are rejected."""
        valid, reason = ingest_mod.validate_cv_response({"error": "inference failed"})
        self.assertFalse(valid)
        self.assertEqual(reason, "error_payload:error")

    def test_rejects_empty_or_invalid_status(self) -> None:
        """Verify missing, empty, and unknown statuses are rejected."""
        valid_empty, reason_empty = ingest_mod.validate_cv_response({"status": "  "})
        self.assertFalse(valid_empty)
        self.assertEqual(reason_empty, "empty_status")

        valid_invalid, reason_invalid = ingest_mod.validate_cv_response({"status": "BANANA"})
        self.assertFalse(valid_invalid)
        self.assertEqual(reason_invalid, "invalid_status:BANANA")

        valid_missing, _ = ingest_mod.validate_cv_response({"vehicle_counts": {}})
        self.assertFalse(valid_missing)

    def test_accepts_real_engine_payload(self) -> None:
        """Verify a real engine payload with traffic and flood severity is accepted."""
        payload = {
            "service": "projectkv-cv-engine",
            "vehicles_detected": 12,
            "traffic_severity": "MODERATE",
            "flood_severity": "CLEAR",
            "confidence": 0.88,
        }
        valid, reason = ingest_mod.validate_cv_response(payload)
        self.assertTrue(valid, reason)


class TestTimestampRobustness(unittest.TestCase):
    """Tests for malformed or out-of-range frame timestamps."""

    def setUp(self) -> None:
        self.temp_dir = tempfile.TemporaryDirectory()
        self.archive_dir = Path(self.temp_dir.name) / "proofs"
        self.db_path = Path(self.temp_dir.name) / "camera-view.db"
        self.manifest_out = Path(self.temp_dir.name) / "manifest.json"

    def tearDown(self) -> None:
        self.temp_dir.cleanup()

    def test_parse_frame_filename_handles_huge_timestamp(self) -> None:
        """Verify a syntactically valid but absurd timestamp does not crash parsing."""
        parsed = ingest_mod.parse_frame_filename("99999999999999999999_sample.jpg")
        self.assertIsNotNone(parsed)
        assert parsed is not None
        self.assertEqual(parsed[0], 99999999999999999999)

    def test_run_ingest_skips_out_of_range_timestamp(self) -> None:
        """Verify an out-of-range timestamp is reported and skipped without crashing."""
        cam = self.archive_dir / "cam_time"
        cam.mkdir(parents=True)
        (cam / "1793681792_sample.jpg").write_bytes(VALID_JPEG_BYTES)
        (cam / "99999999999999999999_sample.jpg").write_bytes(VALID_JPEG_BYTES)

        parser = ingest_mod.build_parser()
        args = parser.parse_args(
            [
                "--archive-dir", str(self.archive_dir),
                "--db-path", str(self.db_path),
                "--manifest-out", str(self.manifest_out),
                "--cv-engine-url", "",
            ]
        )
        exit_code = ingest_mod.run_ingest(args)
        self.assertEqual(exit_code, 0)

        with open(self.manifest_out, "r", encoding="utf-8") as handle:
            manifest_data = json.load(handle)
        self.assertEqual(manifest_data["total_frames"], 2)
        self.assertEqual(manifest_data["valid_frames"], 1)
        self.assertEqual(manifest_data["invalid_reasons"].get("timestamp_out_of_range"), 1)


class TestSQLiteIngestion(unittest.TestCase):
    """Tests for SQLite hotset_verdict_history schema creation, migration, and idempotency."""

    def setUp(self) -> None:
        self.temp_dir = tempfile.TemporaryDirectory()
        self.db_path = Path(self.temp_dir.name) / "test_camera_view.db"
        self.conn = sqlite3.connect(str(self.db_path))

    def tearDown(self) -> None:
        self.conn.close()
        self.temp_dir.cleanup()

    def test_fresh_schema_creation(self) -> None:
        """Verify table creation from scratch with all required columns and unique index."""
        result = ingest_mod.ensure_history_schema(self.conn)
        self.assertTrue(result["created_table"])

        cursor = self.conn.cursor()
        cursor.execute("PRAGMA table_info(hotset_verdict_history)")
        columns = {row[1]: row[2] for row in cursor.fetchall()}

        required_cols = [
            "camera_id",
            "direction",
            "region_id",
            "pcu_total",
            "saturation_index",
            "severity",
            "confidence",
            "source",
            "observed_at",
            "recorded_at",
        ]
        for col in required_cols:
            self.assertIn(col, columns, f"Missing required column: {col}")

        # Verify unique index on (camera_id, direction, observed_at)
        cursor.execute("PRAGMA index_list(hotset_verdict_history)")
        indices = [row[1] for row in cursor.fetchall()]
        self.assertIn("uq_hotset_verdict_history_cam_dir_observed", indices)

    def test_wal_mode_and_busy_timeout(self) -> None:
        """Verify the connection uses WAL journaling and a 5000 ms busy timeout."""
        ingest_mod.ensure_history_schema(self.conn)

        journal_mode = self.conn.execute("PRAGMA journal_mode").fetchone()[0]
        self.assertEqual(str(journal_mode).lower(), "wal")

        busy_timeout = self.conn.execute("PRAGMA busy_timeout").fetchone()[0]
        self.assertGreaterEqual(int(busy_timeout), 5000)

    def test_schema_migrations_synchronized_on_fresh_create(self) -> None:
        """Verify 0007 and 0008 are recorded so Go migrations are not replayed."""
        result = ingest_mod.ensure_history_schema(self.conn)
        self.assertTrue(result["created_table"])
        self.assertEqual(sorted(result["synced_migrations"]), [7, 8])

        rows = self.conn.execute("SELECT version FROM schema_migrations ORDER BY version").fetchall()
        versions = {int(row[0]) for row in rows}
        self.assertIn(7, versions)
        self.assertIn(8, versions)

    def test_schema_migrations_not_duplicated_on_existing_table(self) -> None:
        """Verify a second run is idempotent and keeps one row per migration."""
        ingest_mod.ensure_history_schema(self.conn)
        result = ingest_mod.ensure_history_schema(self.conn)
        self.assertFalse(result["created_table"])
        self.assertEqual(result["synced_migrations"], [])

        rows = self.conn.execute(
            "SELECT version, COUNT(*) FROM schema_migrations GROUP BY version"
        ).fetchall()
        for version, count in rows:
            self.assertEqual(count, 1, f"migration {version} was duplicated")

    def test_recorded_at_uses_go_fixed_layout(self) -> None:
        """Verify recorded_at is written as a UTC RFC3339Nano string, not CURRENT_TIMESTAMP."""
        ingest_mod.ensure_history_schema(self.conn)
        rec = ingest_mod.VerdictRecord(
            camera_id="cam_recorded_at",
            direction="",
            region_id="",
            pcu_total=1.0,
            saturation_index=0.1,
            severity="CLEAR",
            severity_class="CLEAR",
            confidence=0.9,
            source="proof_archive",
            observed_at="2026-11-03T05:00:00.000000000Z",
            method="heuristic",
        )
        ingest_mod.insert_verdict_batch(self.conn, [rec])

        recorded_at = self.conn.execute(
            "SELECT recorded_at FROM hotset_verdict_history WHERE camera_id='cam_recorded_at'"
        ).fetchone()[0]
        self.assertRegex(str(recorded_at), r"^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{9}Z$")

    def test_migration_from_sprint10_schema(self) -> None:
        """Verify migration from 0007 schema (without direction/region_id) to 0008."""
        # Create legacy 0007 table
        cursor = self.conn.cursor()
        cursor.execute(
            """
            CREATE TABLE hotset_verdict_history (
                id                INTEGER PRIMARY KEY AUTOINCREMENT,
                camera_id         TEXT NOT NULL,
                pcu_total         REAL,
                saturation_index  REAL,
                severity          TEXT NOT NULL,
                confidence        REAL NOT NULL DEFAULT 0.0,
                source            TEXT NOT NULL DEFAULT '',
                observed_at       TIMESTAMP NOT NULL,
                recorded_at       TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
            );
            """
        )
        cursor.execute(
            """
            CREATE UNIQUE INDEX uq_hotset_verdict_history_camera_observed
                ON hotset_verdict_history(camera_id, observed_at);
            """
        )
        # Insert a pre-existing legacy row
        cursor.execute(
            """
            INSERT INTO hotset_verdict_history (camera_id, pcu_total, saturation_index, severity, confidence, source, observed_at)
            VALUES ('cam_legacy', 12.0, 0.45, 'moderate', 0.85, 'cv_engine', '2026-11-03T04:00:00.000000000Z')
            """
        )
        self.conn.commit()

        # Run ensure_history_schema to perform migration
        migration_res = ingest_mod.ensure_history_schema(self.conn)
        self.assertFalse(migration_res["created_table"])
        self.assertIn("direction", migration_res["added_columns"])
        self.assertIn("region_id", migration_res["added_columns"])
        self.assertIn("uq_hotset_verdict_history_camera_observed", migration_res["dropped_indexes"])
        self.assertIn("uq_hotset_verdict_history_cam_dir_observed", migration_res["created_indexes"])
        self.assertEqual(sorted(migration_res["synced_migrations"]), [7, 8])

        # Check legacy row preserved with default direction=''
        cursor.execute("SELECT camera_id, direction, region_id, pcu_total FROM hotset_verdict_history WHERE camera_id='cam_legacy'")
        row = cursor.fetchone()
        self.assertIsNotNone(row)
        self.assertEqual(row[0], "cam_legacy")
        self.assertEqual(row[1], "")  # Default direction
        self.assertEqual(row[2], "")  # Default region_id
        self.assertEqual(row[3], 12.0)

    def test_idempotent_ingestion_on_duplicate_keys(self) -> None:
        """Verify inserting the same (camera_id, direction, observed_at) twice is a no-op."""
        ingest_mod.ensure_history_schema(self.conn)

        rec = ingest_mod.VerdictRecord(
            camera_id="cam_idempotent",
            direction="a",
            region_id="reg_1",
            pcu_total=15.5,
            saturation_index=0.55,
            severity="moderate",
            severity_class="MODERATE",
            confidence=0.92,
            source="proof_archive",
            observed_at="2026-11-03T04:56:32.000000000Z",
            method="label",
        )

        # First insert: returns (attempted, inserted)
        attempted1, inserted1 = ingest_mod.insert_verdict_batch(self.conn, [rec])
        self.assertEqual(attempted1, 1)
        self.assertEqual(inserted1, 1)

        # Duplicate insert: attempted=1, inserted=0
        attempted2, inserted2 = ingest_mod.insert_verdict_batch(self.conn, [rec])
        self.assertEqual(attempted2, 1)
        self.assertEqual(inserted2, 0)

        # Row count remains 1
        cursor = self.conn.cursor()
        cursor.execute("SELECT COUNT(*) FROM hotset_verdict_history WHERE camera_id='cam_idempotent'")
        self.assertEqual(cursor.fetchone()[0], 1)


class TestBatchTransactionsAndRollback(unittest.TestCase):
    """Tests for batch transaction commits and rollback on integrity failures."""

    def setUp(self) -> None:
        self.temp_dir = tempfile.TemporaryDirectory()
        self.db_path = Path(self.temp_dir.name) / "test_batch.db"
        self.conn = sqlite3.connect(str(self.db_path))
        ingest_mod.ensure_history_schema(self.conn)

    def tearDown(self) -> None:
        self.conn.close()
        self.temp_dir.cleanup()

    def test_batch_transaction_commit(self) -> None:
        """Verify committing multiple batches of records."""
        records: List[ingest_mod.VerdictRecord] = []
        for i in range(100):
            records.append(
                ingest_mod.VerdictRecord(
                    camera_id=f"cam_{i % 5}",
                    direction="" if i % 2 == 0 else "a",
                    region_id="",
                    pcu_total=float(i),
                    saturation_index=min(1.0, i / 100.0),
                    severity="clear",
                    severity_class="CLEAR",
                    confidence=0.9,
                    source="proof_archive",
                    observed_at=f"2026-11-03T05:{i:02d}:00.000000000Z",
                    method="heuristic",
                )
            )

        attempted, inserted = ingest_mod.insert_verdict_batch(self.conn, records, batch_size=25)
        self.assertEqual(attempted, 100)
        self.assertEqual(inserted, 100)

        cursor = self.conn.cursor()
        cursor.execute("SELECT COUNT(*) FROM hotset_verdict_history")
        self.assertEqual(cursor.fetchone()[0], 100)

    def test_rollback_on_unhandled_error(self) -> None:
        """Verify transaction rollback occurs if an unhandled DB exception arises."""
        bad_conn = sqlite3.connect(":memory:")
        bad_conn.close()

        rec = ingest_mod.VerdictRecord(
            camera_id="cam_err",
            direction="",
            region_id="",
            pcu_total=1.0,
            saturation_index=0.1,
            severity="clear",
            severity_class="CLEAR",
            confidence=0.9,
            source="proof_archive",
            observed_at="2026-11-03T05:00:00.000000000Z",
            method="heuristic",
        )
        with self.assertRaises(sqlite3.Error):
            ingest_mod.insert_verdict_batch(bad_conn, [rec])


class TestReplayManifestAndCLI(unittest.TestCase):
    """Integration tests for replay manifest index generation and CLI options."""

    def setUp(self) -> None:
        self.temp_dir = tempfile.TemporaryDirectory()
        self.archive_dir = Path(self.temp_dir.name) / "proofs"
        self.db_path = Path(self.temp_dir.name) / "camera-view.db"
        self.manifest_out = Path(self.temp_dir.name) / "manifest.json"

        # Create sample archive with 2 cameras
        cam1 = self.archive_dir / "cam_01"
        cam2 = self.archive_dir / "cam_02"
        cam1.mkdir(parents=True, exist_ok=True)
        cam2.mkdir(parents=True, exist_ok=True)

        (cam1 / "1793681792_sample.jpg").write_bytes(VALID_JPEG_BYTES)
        (cam1 / "1793681852_sample.jpg").write_bytes(VALID_JPEG_BYTES)
        (cam2 / "1793681912_sample.jpg").write_bytes(VALID_JPEG_BYTES)
        # Corrupt file
        (cam2 / "1793681972_sample.jpg").write_bytes(b"CORRUPT DATA")

    def tearDown(self) -> None:
        self.temp_dir.cleanup()

    def _parse_cli_args(self, extra_args: List[str] | None = None) -> argparse.Namespace:
        """Helper to create fully populated argument namespace using build_parser."""
        parser = ingest_mod.build_parser()
        cmd = [
            "--archive-dir", str(self.archive_dir),
            "--db-path", str(self.db_path),
            "--manifest-out", str(self.manifest_out),
            "--cv-engine-url", "",  # disable external network calls in unit tests
        ]
        if extra_args:
            cmd.extend(extra_args)
        return parser.parse_args(cmd)

    def test_run_ingest_produces_manifest_and_db_rows(self) -> None:
        """Verify full run_ingest execution generates manifest and persists rows."""
        args = self._parse_cli_args()
        exit_code = ingest_mod.run_ingest(args)
        self.assertEqual(exit_code, 0)

        # Verify manifest generated
        self.assertTrue(self.manifest_out.exists())
        with open(self.manifest_out, "r", encoding="utf-8") as f:
            manifest_data = json.load(f)

        self.assertEqual(manifest_data["total_frames"], 4)
        self.assertEqual(manifest_data["valid_frames"], 3)
        self.assertEqual(manifest_data["invalid_frames"], 1)
        self.assertEqual(manifest_data["unique_cameras"], 2)
        self.assertIn("cam_01", manifest_data["camera_ids"])
        self.assertIn("cam_02", manifest_data["camera_ids"])
        self.assertEqual(manifest_data["timestamp_range"]["start_unix"], 1793681792)

        # Verify SQLite rows
        conn = sqlite3.connect(str(self.db_path))
        cursor = conn.cursor()
        cursor.execute("SELECT COUNT(*) FROM hotset_verdict_history")
        count = cursor.fetchone()[0]
        conn.close()
        self.assertGreaterEqual(count, 3)

    def test_cli_dry_run_does_not_modify_database(self) -> None:
        """Verify --dry-run parses frames and writes manifest without modifying DB."""
        args = self._parse_cli_args(["--dry-run"])
        exit_code = ingest_mod.run_ingest(args)
        self.assertEqual(exit_code, 0)

        self.assertTrue(self.manifest_out.exists())
        with open(self.manifest_out, "r", encoding="utf-8") as f:
            manifest_data = json.load(f)
        self.assertEqual(manifest_data["mode"], "dry_run")

        # Database should not contain rows
        if self.db_path.exists():
            conn = sqlite3.connect(str(self.db_path))
            cursor = conn.cursor()
            cursor.execute("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='hotset_verdict_history'")
            if cursor.fetchone()[0] > 0:
                cursor.execute("SELECT COUNT(*) FROM hotset_verdict_history")
                self.assertEqual(cursor.fetchone()[0], 0)
            conn.close()

    def test_cli_verify_mode(self) -> None:
        """Verify --verify mode checks records against database without ingestion."""
        # First ingest normally
        args_ingest = self._parse_cli_args()
        ingest_mod.run_ingest(args_ingest)

        # Now verify
        args_verify = self._parse_cli_args(["--verify"])
        exit_code = ingest_mod.run_ingest(args_verify)
        self.assertEqual(exit_code, 0)

        with open(self.manifest_out, "r", encoding="utf-8") as f:
            manifest_data = json.load(f)
        self.assertEqual(manifest_data["mode"], "verify")
        self.assertTrue(manifest_data["verification"]["table_present"])
        self.assertEqual(manifest_data["verification"]["missing_records"], 0)

    def test_cli_limit_parameter(self) -> None:
        """Verify --limit caps the number of processed frames."""
        args_limit = self._parse_cli_args(["--limit", "1"])
        exit_code = ingest_mod.run_ingest(args_limit)
        self.assertEqual(exit_code, 0)

        with open(self.manifest_out, "r", encoding="utf-8") as f:
            manifest_data = json.load(f)
        self.assertEqual(manifest_data["total_frames"], 1)

    def test_default_manifest_path_is_docs_proofs(self) -> None:
        """Verify the default manifest is not written under the gitignored data/ tree."""
        self.assertEqual(
            ingest_mod.DEFAULT_MANIFEST_PATH,
            REPO_ROOT / "docs" / "proofs" / "sprint11" / "proof_archive_manifest.json",
        )

    def test_cli_subprocess_help(self) -> None:
        """Verify calling script directly with --help succeeds."""
        script_path = REPO_ROOT / "tools" / "eval" / "ingest_proof_archive.py"
        res = subprocess.run(
            [sys.executable, str(script_path), "--help"],
            capture_output=True,
            text=True,
        )
        self.assertEqual(res.returncode, 0)
        self.assertIn("--archive-dir", res.stdout)
        self.assertIn("--db-path", res.stdout)
        self.assertIn("--dry-run", res.stdout)
        self.assertIn("--verify", res.stdout)


if __name__ == "__main__":
    unittest.main()
