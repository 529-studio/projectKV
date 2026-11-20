from __future__ import annotations

import contextlib
import gzip
import io
import json
import os
import shutil
import sqlite3
import sys
import tempfile
import time
import unittest
from pathlib import Path
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))

from tools.deploy import restore_drill as rd  # noqa: E402

REQUIRED_SUMMARY_KEYS = {
    "mode",
    "started_at",
    "cycles",
    "rto_seconds",
    "rto_budget_seconds",
    "integrity",
    "rows",
    "checksum",
    "rpo_seconds",
    "archive",
    "verdict",
}


def _quiet(callable_, *args, **kwargs):
    """Run ``callable_`` with stdout captured; return (result, output)."""
    buffer = io.StringIO()
    with contextlib.redirect_stdout(buffer):
        result = callable_(*args, **kwargs)
    return result, buffer.getvalue()


class HermeticArchiveTests(unittest.TestCase):
    def test_archive_matches_production_shape(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            fixture = rd.build_hermetic_archive(Path(tmp), rows=64, rows_after_snapshot=7)
            self.assertRegex(fixture.archive.name, r"^warded-\d{8}\.sqlite3\.gz$")
            self.assertTrue(fixture.archive.is_file())
            self.assertEqual(fixture.rows, 64)
            self.assertEqual(fixture.ids, tuple(range(1, 65)))
            self.assertEqual(len(fixture.checksum), 64)
            with gzip.open(fixture.archive, "rb") as handle:
                self.assertTrue(handle.read(16).startswith(b"SQLite format 3\x00"))

    def test_restore_reproduces_committed_rows_and_excludes_later_writes(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            fixture = rd.build_hermetic_archive(root, rows=40, rows_after_snapshot=9)
            # Fixture construction already asserted the snapshot holds exactly
            # the rows committed before the backup.
            verification = rd.verify_archive(
                fixture.archive,
                root / "sandbox",
                expected_rows=fixture.rows,
                expected_checksum=fixture.checksum,
                expected_ids=fixture.ids,
            )
            self.assertTrue(verification.ok, verification.errors)
            self.assertEqual(verification.integrity, "ok")
            self.assertEqual(verification.rows, 40)
            self.assertEqual(verification.checksum, fixture.checksum)
            self.assertGreaterEqual(verification.rto_seconds, 0.0)

    def test_checksum_is_stable_across_two_builds(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            first = rd.build_hermetic_archive(Path(tmp) / "a", rows=32)
            second = rd.build_hermetic_archive(Path(tmp) / "b", rows=32)
            self.assertEqual(first.checksum, second.checksum)


class CorruptionDetectionTests(unittest.TestCase):
    def test_corrupted_gzip_is_reported(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            archive = Path(tmp) / "warded-20261120.sqlite3.gz"
            archive.write_bytes(b"this is not a gzip stream")
            with self.assertRaises(rd.DrillError) as caught:
                rd.decompress_archive(archive, Path(tmp) / "restored.sqlite3")
            self.assertIn("gzip decode failed", str(caught.exception))

            verification = rd.verify_archive(archive, Path(tmp) / "sandbox")
            self.assertEqual(verification.integrity, "fail")
            self.assertFalse(verification.ok)
            self.assertIsNone(verification.rows)
            self.assertTrue(any("gzip" in error for error in verification.errors))

    def test_truncated_gzip_is_reported(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            fixture = rd.build_hermetic_archive(root, rows=16)
            payload = fixture.archive.read_bytes()
            fixture.archive.write_bytes(payload[: len(payload) // 2])
            verification = rd.verify_archive(fixture.archive, root / "sandbox")
            self.assertEqual(verification.integrity, "fail")
            self.assertTrue(any("gzip" in error for error in verification.errors))

    def test_truncated_snapshot_reports_row_mismatch(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            fixture = rd.build_hermetic_archive(root, rows=50)
            verification = rd.verify_archive(
                fixture.archive,
                root / "sandbox",
                expected_rows=200,
                expected_checksum=fixture.checksum,
            )
            self.assertFalse(verification.ok)
            self.assertEqual(verification.integrity, "ok")
            self.assertEqual(verification.rows, 50)
            self.assertTrue(
                any("row count mismatch" in error for error in verification.errors),
                verification.errors,
            )

    def test_missing_committed_row_is_reported(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            fixture = rd.build_hermetic_archive(root, rows=20)
            verification = rd.verify_archive(
                fixture.archive,
                root / "sandbox",
                expected_rows=fixture.rows,
                expected_checksum=fixture.checksum,
                expected_ids=tuple(range(1, 22)),
            )
            self.assertFalse(verification.ok)
            self.assertTrue(
                any("missing from the restore" in error for error in verification.errors),
                verification.errors,
            )

    def test_checksum_mismatch_is_reported(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            fixture = rd.build_hermetic_archive(root, rows=12)
            verification = rd.verify_archive(
                fixture.archive,
                root / "sandbox",
                expected_rows=fixture.rows,
                expected_checksum="0" * 64,
            )
            self.assertFalse(verification.ok)
            self.assertTrue(
                any("checksum mismatch" in error for error in verification.errors),
                verification.errors,
            )


class RtoBudgetTests(unittest.TestCase):
    def _options(self, out: Path, budget: float, cycles: int = 1) -> rd.DrillOptions:
        return rd.DrillOptions(
            mode="hermetic",
            out=out,
            cycles=cycles,
            rto_budget_seconds=budget,
        )

    def test_generous_budget_passes(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            options = self._options(root / "summary.json", 300.0, cycles=2)
            result, _ = _quiet(rd.run_hermetic, options)
            self.assertEqual(result.verdict, "pass")
            self.assertEqual(result.errors, [])
            self.assertEqual(result.integrity, "ok")
            self.assertEqual(result.rows, rd.COMMITTED_ROWS)
            self.assertEqual(result.cycles, 2)

    def test_budget_breach_fails_the_drill(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            options = self._options(root / "summary.json", 0.0)
            result, output = _quiet(rd.run_hermetic, options)
            self.assertEqual(result.verdict, "fail")
            self.assertEqual(result.integrity, "ok")
            self.assertTrue(
                any("RTO budget" in error for error in result.errors),
                result.errors,
            )
            self.assertIn("[ERR]", output)

    def test_main_returns_one_on_budget_breach(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            out = Path(tmp) / "summary.json"
            code, _ = _quiet(
                rd.main,
                [
                    "--mode",
                    "hermetic",
                    "--cycles",
                    "1",
                    "--rto-budget-seconds",
                    "0",
                    "--out",
                    str(out),
                ],
            )
            self.assertEqual(code, 1)
            summary = json.loads(out.read_text(encoding="utf-8"))
            self.assertEqual(summary["verdict"], "fail")


class LiveDrillTests(unittest.TestCase):
    def test_local_archive_passes(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            fixture = rd.build_hermetic_archive(root / "fixture", rows=24)
            options = rd.DrillOptions(mode="live", archive=fixture.archive)
            result, _ = _quiet(rd.run_live, options)
            self.assertEqual(result.verdict, "pass", result.errors)
            self.assertEqual(result.rows, 24)
            self.assertEqual(result.integrity, "ok")
            self.assertIsNotNone(result.rpo_seconds)
            self.assertLess(result.rpo_seconds, 24 * 3600)

    def test_stale_archive_fails_the_rpo_budget(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            fixture = rd.build_hermetic_archive(root / "fixture", rows=10)
            old = time.time() - 48 * 3600
            os.utime(fixture.archive, (old, old))
            options = rd.DrillOptions(mode="live", archive=fixture.archive)
            result, output = _quiet(rd.run_live, options)
            self.assertEqual(result.verdict, "fail")
            self.assertTrue(
                any("RPO budget" in error for error in result.errors), result.errors
            )
            self.assertIn("[ERR]", output)

    def test_auto_skips_live_and_runs_hermetic_when_rclone_is_absent(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            out = Path(tmp) / "summary.json"
            with mock.patch.object(rd, "rclone_binary", return_value=None):
                code, output = _quiet(
                    rd.main, ["--mode", "auto", "--out", str(out), "--cycles", "1"]
                )
            self.assertEqual(code, 0)
            self.assertIn("[INFO] live drill skipped: rclone is not on PATH", output)
            self.assertIn("[INFO] running the hermetic drill instead", output)
            self.assertNotIn("r2:homelab-backup", output)
            summary = json.loads(out.read_text(encoding="utf-8"))
            self.assertEqual(summary["verdict"], "pass")
            self.assertEqual(summary["integrity"], "ok")
            self.assertEqual(summary["rows"], rd.COMMITTED_ROWS)
            self.assertEqual(summary["mode"], "auto")
            self.assertTrue(REQUIRED_SUMMARY_KEYS.issubset(summary))
            self.assertTrue(
                any("live drill skipped" in note for note in summary["notes"]),
                summary["notes"],
            )

    def test_auto_uses_the_live_archive_when_rclone_is_configured(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            fixture = rd.build_hermetic_archive(root / "fixture", rows=30)
            out = root / "summary.json"
            environ = {name: "x" for name in rd.RCLONE_ENV_VARS}

            def fake_download(
                rclone: str, bucket: str, name: str, destination: Path
            ) -> Path:
                shutil.copyfile(fixture.archive, destination)
                return destination

            with mock.patch.object(rd, "rclone_binary", return_value="/usr/bin/rclone"):
                with mock.patch.dict(os.environ, environ):
                    with mock.patch.object(
                        rd,
                        "list_remote_archives",
                        return_value=["warded-20261120.sqlite3.gz"],
                    ):
                        with mock.patch.object(rd, "download_archive", fake_download):
                            code, output = _quiet(
                                rd.main, ["--mode", "auto", "--out", str(out)]
                            )
            self.assertEqual(code, 0)
            self.assertNotIn("live drill skipped", output)
            summary = json.loads(out.read_text(encoding="utf-8"))
            self.assertEqual(summary["verdict"], "pass")
            self.assertEqual(summary["mode"], "auto")
            self.assertEqual(
                summary["archive"], "r2:homelab-backup/warded/warded-20261120.sqlite3.gz"
            )
            self.assertEqual(summary["rows"], 30)

    def test_explicit_live_without_rclone_fails(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            out = Path(tmp) / "summary.json"
            with mock.patch.object(rd, "rclone_binary", return_value=None):
                code, output = _quiet(rd.main, ["--mode", "live", "--out", str(out)])
            self.assertEqual(code, 1)
            self.assertIn("[ERR] live drill failed: rclone is not on PATH", output)
            summary = json.loads(out.read_text(encoding="utf-8"))
            self.assertEqual(summary["verdict"], "fail")

    def test_auto_falls_back_when_bucket_is_unreachable(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            out = Path(tmp) / "summary.json"
            with mock.patch.object(rd, "rclone_binary", return_value="/usr/bin/rclone"):
                with mock.patch.object(rd, "missing_rclone_env", return_value=[]):
                    with mock.patch.object(
                        rd,
                        "list_remote_archives",
                        side_effect=rd.DrillSkip("cannot list r2:bucket/warded/: boom"),
                    ):
                        code, output = _quiet(
                            rd.main, ["--mode", "auto", "--out", str(out)]
                        )
            self.assertEqual(code, 0)
            self.assertIn("[INFO] live drill skipped: cannot list", output)
            summary = json.loads(out.read_text(encoding="utf-8"))
            self.assertEqual(summary["verdict"], "pass")
            self.assertTrue(
                any("cannot list" in note for note in summary["notes"]),
                summary["notes"],
            )

    def test_reachable_bucket_without_archives_fails(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            options = rd.DrillOptions(mode="live", bucket="bucket")
            with mock.patch.object(rd, "rclone_binary", return_value="/usr/bin/rclone"):
                with mock.patch.object(rd, "missing_rclone_env", return_value=[]):
                    with mock.patch.object(rd, "list_remote_archives", return_value=[]):
                        result, _ = _quiet(rd.run_drill, options)
            self.assertEqual(result.verdict, "fail")
            self.assertTrue(any("no warded-" in error for error in result.errors))


class LivePreconditionTests(unittest.TestCase):
    def test_missing_env_is_named_without_values(self) -> None:
        environ = {"RCLONE_CONFIG_R2_TYPE": "s3"}
        missing = rd.missing_rclone_env(environ)
        self.assertIn("RCLONE_CONFIG_R2_ACCESS_KEY_ID", missing)
        self.assertIn("RCLONE_CONFIG_R2_SECRET_ACCESS_KEY", missing)
        self.assertNotIn("RCLONE_CONFIG_R2_TYPE", missing)

    def test_skip_reason_requires_rclone(self) -> None:
        with mock.patch.object(rd, "rclone_binary", return_value=None):
            self.assertEqual(rd.live_skip_reason({}), "rclone is not on PATH")

    def test_skip_reason_requires_r2_environment(self) -> None:
        with mock.patch.object(rd, "rclone_binary", return_value="/usr/bin/rclone"):
            reason = rd.live_skip_reason({})
        self.assertIsNotNone(reason)
        self.assertIn("RCLONE_CONFIG_R2_TYPE", reason)

    def test_no_configuration_needed_when_rclone_and_env_are_present(self) -> None:
        environ = {name: "x" for name in rd.RCLONE_ENV_VARS}
        with mock.patch.object(rd, "rclone_binary", return_value="/usr/bin/rclone"):
            self.assertIsNone(rd.live_skip_reason(environ))


class ArchiveSelectionTests(unittest.TestCase):
    def test_newest_archive_wins(self) -> None:
        names = [
            "warded-20261118.sqlite3.gz",
            "warded-20261120.sqlite3.gz",
            "warded-20261119.sqlite3.gz",
        ]
        self.assertEqual(rd.newest_archive(names), "warded-20261120.sqlite3.gz")

    def test_junk_is_ignored(self) -> None:
        names = ["notes.txt", "warded-2026112.sqlite3.gz", "warded-20261120.sqlite3"]
        self.assertIsNone(rd.newest_archive(names))


class SummaryOutputTests(unittest.TestCase):
    def test_hermetic_summary_has_required_fields(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            out = Path(tmp) / "nested" / "summary.json"
            code, output = _quiet(
                rd.main,
                ["--mode", "hermetic", "--cycles", "1", "--out", str(out)],
            )
            self.assertEqual(code, 0)
            self.assertIn("verdict=pass", output)
            summary = json.loads(out.read_text(encoding="utf-8"))
            for key in REQUIRED_SUMMARY_KEYS:
                self.assertIn(key, summary)
            self.assertEqual(summary["mode"], "hermetic")
            self.assertEqual(summary["cycles"], 1)
            self.assertEqual(summary["rows"], rd.COMMITTED_ROWS)
            self.assertEqual(len(summary["checksum"]), 64)
            self.assertEqual(summary["verdict"], "pass")
            self.assertEqual(summary["errors"], [])

    def test_mode_is_clamped_to_at_least_one_cycle(self) -> None:
        args = rd.build_parser().parse_args(["--cycles", "0"])
        self.assertEqual(rd.options_from_args(args).cycles, 1)


class DatabaseHelperTests(unittest.TestCase):
    def test_table_measurement_is_order_stable(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            fixture = rd.build_hermetic_archive(root, rows=8)
            restored = rd.decompress_archive(fixture.archive, root / "restored.sqlite3")
            connection = sqlite3.connect(f"file:{restored}?mode=ro", uri=True)
            try:
                self.assertEqual(rd.select_sample_table(connection), rd.SAMPLE_TABLE)
                rows, checksum = rd.table_measurement(connection, rd.SAMPLE_TABLE)
                again = rd.table_measurement(connection, rd.SAMPLE_TABLE)
                self.assertEqual((rows, checksum), again)
                self.assertEqual(rd.integrity_check(connection), "ok")
            finally:
                connection.close()


if __name__ == "__main__":
    unittest.main()
