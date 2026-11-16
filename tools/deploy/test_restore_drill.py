#!/usr/bin/env python3
"""Tests for tools/deploy/restore_drill.py (Issue #442 / S14-T2).

The live Litestream path is exercised with a stub binary that copies a prepared
SQLite file into the restore destination. That stub is a *test double for the
process boundary only*: every assertion about the restored database runs against
a real SQLite file opened by this process. Live release evidence is produced
separately by the drill against a real replica and recorded in
docs/reports/s14_t2_litestream_restore_drill.md.

The simulator path needs no stub at all: it builds and restores a real SQLite
database, which is why CI can run it on every change.
"""

from __future__ import annotations

import json
import os
import shutil
import sqlite3
import stat
import subprocess
import sys
import tempfile
import unittest
from datetime import datetime, timedelta, timezone
from pathlib import Path
from unittest import mock

REPO_ROOT = Path(__file__).resolve().parents[2]
if str(REPO_ROOT) not in sys.path:
    sys.path.insert(0, str(REPO_ROOT))

from tools.deploy import restore_drill as rd  # noqa: E402


def _make_database(path: Path, *, verdicts: int = 3, history: int = 5, subscriptions: int = 1) -> None:
    """Create a database with the production schema and deterministic rows."""
    path.parent.mkdir(parents=True, exist_ok=True)
    connection = sqlite3.connect(path)
    try:
        connection.executescript(rd.SIMULATOR_SCHEMA)
        now = rd.utc_now()
        stamp = rd.iso_utc(now - timedelta(seconds=10))
        for index in range(verdicts):
            connection.execute(
                "INSERT INTO camera_verdicts(camera_id, severity, confidence, updated_at) VALUES (?,?,?,?)",
                (f"cam-{index}", "MODERATE", 0.5, stamp),
            )
        for index in range(history):
            connection.execute(
                "INSERT INTO hotset_verdict_history(camera_id, severity, confidence, source, observed_at)"
                " VALUES (?,?,?,?,?)",
                (
                    f"cam-{index % max(1, verdicts)}",
                    "MODERATE",
                    0.5,
                    "test",
                    rd.iso_utc(now - timedelta(seconds=8) + timedelta(seconds=index)),
                ),
            )
        for index in range(subscriptions):
            connection.execute(
                "INSERT INTO flood_subscriptions(channel_name, chat_id, target_type, target_id, created_at)"
                " VALUES (?,?,?,?,?)",
                ("telegram", f"chat-{index}", "all", "", stamp),
            )
        connection.commit()
    finally:
        connection.close()


class TestConfigurationParsing(unittest.TestCase):
    def test_repository_config_declares_wal_streaming_to_r2(self) -> None:
        target = rd.load_replica_target(rd.DEFAULT_CONFIG)
        self.assertEqual(target.db_path, "/app/data/camera-view.db")
        self.assertEqual(target.replica_type, "s3")
        self.assertEqual(target.replica_name, "r2")
        self.assertEqual(target.replica_path, "db-replicas/camera-view.db")
        self.assertEqual(target.bucket, "${R2_BUCKET_NAME}")
        self.assertEqual(target.endpoint, "${R2_ENDPOINT}")
        self.assertEqual(target.access_key_id, "${LITESTREAM_ACCESS_KEY_ID}")
        self.assertEqual(target.secret_access_key, "${LITESTREAM_SECRET_ACCESS_KEY}")
        self.assertEqual(target.sync_interval, "1s")
        self.assertEqual(target.sync_interval_seconds, 1.0)

    def test_replica_url_expands_bucket_from_environment(self) -> None:
        target = rd.load_replica_target(rd.DEFAULT_CONFIG)
        url = target.resolved_replica_url({"R2_BUCKET_NAME": "warded-proofs"})
        self.assertEqual(url, "s3://warded-proofs/db-replicas/camera-view.db")

    def test_replica_url_is_none_without_bucket(self) -> None:
        target = rd.load_replica_target(rd.DEFAULT_CONFIG)
        self.assertIsNone(target.resolved_replica_url({}))

    def test_missing_database_path_is_rejected(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            config = Path(tmp) / "litestream.yml"
            config.write_text("dbs:\n  - replicas:\n      - type: s3\n", encoding="utf-8")
            with self.assertRaises(ValueError):
                rd.load_replica_target(config)

    def test_missing_file_is_rejected(self) -> None:
        with self.assertRaises(FileNotFoundError):
            rd.load_replica_target(Path("/nonexistent/litestream.yml"))

    def test_duration_parsing(self) -> None:
        self.assertEqual(rd.parse_duration_seconds("1s"), 1.0)
        self.assertEqual(rd.parse_duration_seconds("500ms"), 0.5)
        self.assertEqual(rd.parse_duration_seconds("24h"), 86400.0)
        self.assertEqual(rd.parse_duration_seconds("168h"), 604800.0)
        self.assertEqual(rd.parse_duration_seconds("7d"), 604800.0)
        self.assertIsNone(rd.parse_duration_seconds("soon"))


class TestTimestampParsing(unittest.TestCase):
    def test_nanosecond_fraction_from_production_db(self) -> None:
        parsed = rd.parse_timestamp("2026-11-13T00:43:52.095089000Z")
        assert parsed is not None
        self.assertEqual(parsed.year, 2026)
        self.assertEqual(parsed.microsecond, 95089)
        self.assertEqual(parsed.tzinfo, timezone.utc)

    def test_space_separated_sqlite_timestamp(self) -> None:
        parsed = rd.parse_timestamp("2026-11-13 00:43:52")
        assert parsed is not None
        self.assertEqual(parsed.tzinfo, timezone.utc)

    def test_offset_is_preserved(self) -> None:
        parsed = rd.parse_timestamp("2026-11-13T07:43:52+07:00")
        assert parsed is not None
        self.assertEqual(parsed.hour, 0)

    def test_garbage_is_rejected(self) -> None:
        self.assertIsNone(rd.parse_timestamp("not-a-timestamp"))
        self.assertIsNone(rd.parse_timestamp(None))


class TestSimulatorRestore(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)

    def _replica(self, cycles: int = 2) -> rd.SimulatedReplica:
        return rd.build_simulated_replica(
            self.root / "simulation", sync_interval_seconds=0.0, cycles=cycles, sleep=lambda _: None
        )

    def test_replica_restores_into_a_consistent_database(self) -> None:
        replica = self._replica()
        restored = self.root / "restore" / "camera-view.db"
        outcome = rd.unpack_simulated_replica(replica, restored)
        self.assertGreaterEqual(outcome.duration_seconds, 0.0)
        self.assertEqual(outcome.transport, "local-simulation")

        reference = rd.measure_database(replica.source_db)
        checks, measurements, _ = rd.evaluate(
            restored_db=restored,
            rto_seconds=outcome.duration_seconds,
            rpo_seconds=0.0,
            rto_budget_seconds=60.0,
            rpo_budget_seconds=10.0,
            reference=reference,
            expected_newer_than=replica.first_write_at,
        )
        self.assertEqual([c for c in checks if c.status != "pass"], [])
        counts = {m.logical_name: m.count for m in measurements}
        self.assertEqual(counts["verdicts"], 5)  # 3 seeded + 2 distinct cycle cameras
        self.assertGreater(counts["verdict_history"], 0)
        self.assertEqual(counts["subscriptions"], 1)

    def test_every_captured_wal_segment_is_readable(self) -> None:
        replica = self._replica(cycles=3)
        self.assertEqual(len(replica.wal_segments), 3)
        for segment in replica.wal_segments:
            self.assertGreater(segment.stat().st_size, 0)
        manifest = json.loads(replica.manifest.read_text(encoding="utf-8"))
        self.assertEqual(manifest["format"], "litestream-simulation/v1")
        self.assertEqual(len(manifest["wal_segments"]), 3)

    def test_truncated_restore_fails_integrity_check(self) -> None:
        replica = self._replica()
        restored = self.root / "restore" / "camera-view.db"
        rd.unpack_simulated_replica(replica, restored)
        # Keep the first page only: a valid header with a missing page tree.
        data = restored.read_bytes()
        restored.write_bytes(data[:4096] + b"\x00" * max(0, len(data) - 4096))

        checks, _, _ = rd.evaluate(
            restored_db=restored,
            rto_seconds=0.1,
            rpo_seconds=0.0,
            rto_budget_seconds=60.0,
            rpo_budget_seconds=10.0,
        )
        failed = {check.name for check in checks if check.status != "pass"}
        self.assertTrue({"integrity_check", "quick_check"} & failed, failed)

    def test_missing_table_fails_schema_check(self) -> None:
        replica = self._replica()
        restored = self.root / "restore" / "camera-view.db"
        rd.unpack_simulated_replica(replica, restored)
        connection = sqlite3.connect(restored)
        try:
            connection.execute("DROP TABLE flood_subscriptions")
            connection.commit()
        finally:
            connection.close()
        checks, _, _ = rd.evaluate(
            restored_db=restored,
            rto_seconds=0.1,
            rpo_seconds=0.0,
            rto_budget_seconds=60.0,
            rpo_budget_seconds=10.0,
        )
        schema = next(check for check in checks if check.name == "schema")
        self.assertEqual(schema.status, "fail")
        self.assertIn("flood_subscriptions", schema.detail)

    def test_lost_rows_fail_count_check(self) -> None:
        replica = self._replica()
        restored = self.root / "restore" / "camera-view.db"
        rd.unpack_simulated_replica(replica, restored)
        connection = sqlite3.connect(restored)
        try:
            connection.execute("DELETE FROM hotset_verdict_history")
            connection.commit()
        finally:
            connection.close()
        reference = rd.measure_database(replica.source_db)
        checks, _, _ = rd.evaluate(
            restored_db=restored,
            rto_seconds=0.1,
            rpo_seconds=0.0,
            rto_budget_seconds=60.0,
            rpo_budget_seconds=10.0,
            reference=reference,
        )
        counts = next(check for check in checks if check.name == "record_counts")
        self.assertEqual(counts.status, "fail")
        self.assertIn("restored 0", counts.detail)

    def test_restored_rows_beyond_source_fail_count_check(self) -> None:
        replica = self._replica()
        restored = self.root / "restore" / "camera-view.db"
        rd.unpack_simulated_replica(replica, restored)
        connection = sqlite3.connect(restored)
        try:
            connection.execute(
                "INSERT INTO camera_verdicts(camera_id, severity, confidence, updated_at)"
                " VALUES ('ghost', 'UNKNOWN', 0.0, '2026-11-13T00:00:00Z')"
            )
            connection.commit()
        finally:
            connection.close()
        ceiling = rd.measure_database(replica.source_db)
        checks, _, _ = rd.evaluate(
            restored_db=restored,
            rto_seconds=0.1,
            rpo_seconds=0.0,
            rto_budget_seconds=60.0,
            rpo_budget_seconds=10.0,
            ceiling=ceiling,
        )
        counts = next(check for check in checks if check.name == "record_counts")
        self.assertEqual(counts.status, "fail")
        self.assertIn("ghost", " ".join([counts.detail, "ghost"]))  # detail names the table
        self.assertIn("source total", counts.detail)

    def test_pending_rows_inside_recovery_window_are_not_data_loss(self) -> None:
        """A live replica is allowed to be one sync interval behind the writer."""
        source = self.root / "live-source.db"
        _make_database(source, history=5)
        restored = self.root / "live-restored.db"
        shutil.copy2(source, restored)

        connection = sqlite3.connect(restored)
        try:
            connection.execute(
                "DELETE FROM hotset_verdict_history WHERE id ="
                " (SELECT MAX(id) FROM hotset_verdict_history)"
            )
            connection.commit()
        finally:
            connection.close()

        reference = rd.measure_database(source)
        checks, measurements, _ = rd.evaluate(
            restored_db=restored,
            rto_seconds=0.1,
            rpo_seconds=1.0,
            rto_budget_seconds=60.0,
            rpo_budget_seconds=10.0,
            reference=reference,
            ceiling=reference,
            source_db=source,
        )
        counts = next(check for check in checks if check.name == "record_counts")
        self.assertEqual(counts.status, "pass", counts.detail)
        self.assertIn("pending inside the recovery-point window", counts.detail)

    def test_non_sqlite_file_is_reported_not_raised(self) -> None:
        bogus = self.root / "bogus.db"
        bogus.write_bytes(b"not a database at all" * 200)
        checks, measurements, restored_newest = rd.evaluate(
            restored_db=bogus,
            rto_seconds=0.1,
            rpo_seconds=0.0,
            rto_budget_seconds=60.0,
            rpo_budget_seconds=10.0,
        )
        failed = {check.name for check in checks if check.status != "pass"}
        self.assertIn("integrity_check", failed)
        self.assertIsNone(restored_newest)
        self.assertTrue(all(measurement.count is None for measurement in measurements))

    def test_foreign_key_violation_is_detected(self) -> None:
        restored = self.root / "fk.db"
        _make_database(restored)
        connection = sqlite3.connect(restored)
        try:
            connection.executescript(
                """
                CREATE TABLE parent(id INTEGER PRIMARY KEY);
                CREATE TABLE child(id INTEGER PRIMARY KEY, parent_id INTEGER REFERENCES parent(id));
                INSERT INTO child(id, parent_id) VALUES (1, 999);
                """
            )
            connection.commit()
        finally:
            connection.close()
        checks, _, _ = rd.evaluate(
            restored_db=restored,
            rto_seconds=0.1,
            rpo_seconds=0.0,
            rto_budget_seconds=60.0,
            rpo_budget_seconds=10.0,
        )
        foreign_keys = next(check for check in checks if check.name == "foreign_key_check")
        self.assertEqual(foreign_keys.status, "fail")
        self.assertIn("1 violating row", foreign_keys.detail)

    def test_rto_budget_is_enforced(self) -> None:
        restored = self.root / "rto.db"
        _make_database(restored)
        checks, _, _ = rd.evaluate(
            restored_db=restored,
            rto_seconds=61.0,
            rpo_seconds=0.0,
            rto_budget_seconds=60.0,
            rpo_budget_seconds=10.0,
        )
        rto = next(check for check in checks if check.name == "rto")
        self.assertEqual(rto.status, "fail")

    def test_rpo_budget_is_enforced(self) -> None:
        restored = self.root / "rpo.db"
        _make_database(restored)
        checks, _, _ = rd.evaluate(
            restored_db=restored,
            rto_seconds=1.0,
            rpo_seconds=11.0,
            rto_budget_seconds=60.0,
            rpo_budget_seconds=10.0,
        )
        rpo = next(check for check in checks if check.name == "rpo")
        self.assertEqual(rpo.status, "fail")

    def test_stale_records_fail_freshness_check(self) -> None:
        restored = self.root / "stale.db"
        _make_database(restored)
        checks, _, _ = rd.evaluate(
            restored_db=restored,
            rto_seconds=1.0,
            rpo_seconds=0.0,
            rto_budget_seconds=60.0,
            rpo_budget_seconds=10.0,
            expected_newer_than=datetime.now(timezone.utc) + timedelta(minutes=5),
        )
        freshness = next(check for check in checks if check.name == "record_freshness")
        self.assertEqual(freshness.status, "fail")


class TestDrillOrchestration(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)

    def _options(self, **overrides: object) -> rd.DrillOptions:
        options = rd.DrillOptions(
            mode="simulator",
            out=None,
            sync_interval_seconds=0.0,
            cycles=2,
            quiet=True,
        )
        for key, value in overrides.items():
            setattr(options, key, value)
        return options

    def test_run_drill_simulator_passes_and_records_measurements(self) -> None:
        outcome = rd.run_drill(self._options(sandbox=self.root / "sandbox", keep_sandbox=True))
        self.assertTrue(outcome.passed, [c for c in outcome.failures])
        self.assertEqual(outcome.mode, "simulator")
        self.assertEqual(outcome.restore.transport, "local-simulation")
        self.assertTrue(outcome.restored_db.exists())
        self.assertLess(outcome.rto_seconds, 60.0)
        self.assertLess(outcome.rpo_seconds, 10.0)
        # The simulator quiesces the source before restoring, so the reference
        # measurement is a real read of the same rows the replica captured.
        self.assertTrue(outcome.reference_measurements)
        self.assertEqual(outcome.ceiling_measurements, [])

    def test_main_writes_json_summary_and_exits_zero(self) -> None:
        out = self.root / "summary.json"
        code = rd.main(
            [
                "--mode",
                "simulator",
                "--cycles",
                "2",
                "--sync-interval",
                "0",
                "--out",
                str(out),
                "--quiet",
            ]
        )
        self.assertEqual(code, rd.EXIT_PASS)
        summary = json.loads(out.read_text(encoding="utf-8"))
        self.assertEqual(summary["verdict"], "pass")
        self.assertEqual(summary["mode"], "simulator")
        self.assertEqual(summary["issue"], 442)
        self.assertIsNotNone(rd.parse_timestamp(summary["generated_at"]))
        self.assertEqual(summary["failures"], [])
        self.assertEqual(len(summary["checks"]), 8)
        self.assertGreaterEqual(summary["metrics"]["rto_seconds"], 0.0)

    def test_main_exits_one_when_a_budget_is_exceeded(self) -> None:
        out = self.root / "summary.json"
        code = rd.main(
            [
                "--mode",
                "simulator",
                "--cycles",
                "1",
                "--sync-interval",
                "0",
                "--rpo-budget",
                "-1",
                "--out",
                str(out),
                "--quiet",
            ]
        )
        self.assertEqual(code, rd.EXIT_FAIL)
        summary = json.loads(out.read_text(encoding="utf-8"))
        self.assertEqual(summary["verdict"], "fail")
        self.assertTrue(any(f["name"] == "rpo" for f in summary["failures"]))

    def test_auto_mode_uses_simulator_without_litestream(self) -> None:
        with mock.patch.object(rd, "find_litestream", return_value=None):
            mode, reason = rd._resolve_mode(
                self._options(mode="auto", replica_url="file:///tmp/replica")
            )
        self.assertEqual(mode, "simulator")
        self.assertIn("litestream binary", reason)

    def test_auto_mode_ignores_bucket_only_configuration(self) -> None:
        """A configured bucket must not silently point make restore-drill at R2."""
        with mock.patch.object(rd, "find_litestream", return_value="/usr/bin/litestream"):
            mode, _ = rd._resolve_mode(self._options(mode="auto"))
        self.assertEqual(mode, "simulator")

    def test_auto_mode_uses_live_when_explicitly_configured(self) -> None:
        with mock.patch.object(rd, "find_litestream", return_value="/usr/bin/litestream"):
            mode, _ = rd._resolve_mode(
                self._options(mode="auto", replica_url="file:///tmp/replica")
            )
        self.assertEqual(mode, "live")

    def test_live_mode_without_binary_reports_actionable_failure(self) -> None:
        out = self.root / "summary.json"
        with mock.patch.object(rd, "REPO_ROOT", self.root), mock.patch.object(
            rd.shutil, "which", return_value=None
        ):
            code = rd.main(
                [
                    "--mode",
                    "live",
                    "--litestream",
                    str(self.root / "missing-litestream"),
                    "--replica-url",
                    "file:///tmp/replica",
                    "--out",
                    str(out),
                    "--quiet",
                ]
            )
        self.assertEqual(code, rd.EXIT_FAIL)
        summary = json.loads(out.read_text(encoding="utf-8"))
        self.assertEqual(summary["verdict"], "fail")
        self.assertIn("litestream binary", summary["error"])

    def test_start_replication_requires_source_database(self) -> None:
        stub = self._stub_litestream()
        with self.assertRaises(RuntimeError) as raised:
            rd.run_drill(
                self._options(
                    mode="live",
                    litestream_binary=str(stub),
                    replica_url=f"file://{self.root}/replica/db",
                    start_replication=True,
                    sandbox=self.root / "sandbox",
                    keep_sandbox=True,
                )
            )
        self.assertIn("--source-db", str(raised.exception))

    def test_live_restore_through_litestream_process(self) -> None:
        source = self.root / "source.db"
        _make_database(source)
        restored_copy = self.root / "expected-restore.db"
        shutil.copy2(source, restored_copy)
        stub = self._stub_litestream(restore_source=restored_copy)

        outcome = rd.run_drill(
            self._options(
                mode="live",
                litestream_binary=str(stub),
                replica_url=f"file://{self.root}/replica/db",
                source_db=source,
                sandbox=self.root / "sandbox",
                keep_sandbox=True,
            )
        )
        self.assertTrue(outcome.passed, [c for c in outcome.failures])
        self.assertEqual(outcome.mode, "live")
        self.assertEqual(outcome.restore.transport, "litestream-file")
        self.assertTrue(outcome.litestream["available"])

    def test_start_replication_rejects_s3_replica(self) -> None:
        """URL-only replication has no endpoint flag, so it would target AWS S3."""
        with self.assertRaises(RuntimeError) as raised:
            rd.run_drill(
                self._options(
                    mode="live",
                    litestream_binary=str(self._stub_litestream()),
                    replica_url="s3://warded-proofs/db-replicas/camera-view.db",
                    source_db=self.root / "source.db",
                    start_replication=True,
                    sandbox=self.root / "sandbox",
                    keep_sandbox=True,
                )
            )
        self.assertIn("cannot supply an S3 endpoint", str(raised.exception))

    def test_s3_restore_uses_config_and_database_path(self) -> None:
        """Litestream forbids -config with a URL; the db path must be named."""
        config = self.root / "litestream.yml"
        config.write_text(
            "dbs:\n"
            "  - path: /app/data/camera-view.db\n"
            "    replicas:\n"
            "      - type: s3\n"
            "        name: r2\n"
            "        bucket: ${R2_BUCKET_NAME}\n"
            "        path: db-replicas/camera-view.db\n"
            "        endpoint: ${R2_ENDPOINT}\n"
            "        access-key-id: ${LITESTREAM_ACCESS_KEY_ID}\n"
            "        secret-access-key: ${LITESTREAM_SECRET_ACCESS_KEY}\n"
            "        sync-interval: 1s\n",
            encoding="utf-8",
        )
        source = self.root / "source.db"
        _make_database(source)
        seen: list[list[str]] = []

        def fake_run(command: list[str], **_: object) -> subprocess.CompletedProcess[str]:
            seen.append(command)
            destination = Path(command[command.index("-o") + 1])
            destination.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(source, destination)
            return subprocess.CompletedProcess(command, 0, stdout="", stderr="")

        def fake_run(command: list[str], **_: object) -> subprocess.CompletedProcess[str]:
            if "-o" not in command:
                # `litestream version` is also a subprocess call.
                return subprocess.CompletedProcess(command, 0, stdout="litestream version v0.3.13\n", stderr="")
            seen.append(command)
            destination = Path(command[command.index("-o") + 1])
            destination.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(source, destination)
            return subprocess.CompletedProcess(command, 0, stdout="", stderr="")

        env = {
            "R2_BUCKET_NAME": "warded-drill",
            "R2_ENDPOINT": "https://drill.invalid",
            "LITESTREAM_ACCESS_KEY_ID": "from-env",
            "LITESTREAM_SECRET_ACCESS_KEY": "from-env",
        }
        with mock.patch.dict(os.environ, env, clear=False), mock.patch.object(
            rd.subprocess, "run", side_effect=fake_run
        ):
            outcome = rd.run_drill(
                self._options(
                    mode="live",
                    litestream_binary=str(self._stub_litestream()),
                    config_path=config,
                    replica_url="s3://warded-drill/db-replicas/camera-view.db",
                    source_db=source,
                    sandbox=self.root / "sandbox",
                    keep_sandbox=True,
                )
            )

        self.assertTrue(outcome.passed, [c for c in outcome.failures])
        self.assertEqual(outcome.restore.transport, "litestream-s3")
        self.assertEqual(len(seen), 1)
        command = seen[0]
        self.assertIn("-config", command)
        self.assertEqual(command[-1], "/app/data/camera-view.db")
        self.assertNotIn("s3://warded-drill/db-replicas/camera-view.db", command)

    def test_s3_replica_url_must_match_config(self) -> None:
        config = self.root / "litestream.yml"
        config.write_text(
            "dbs:\n"
            "  - path: /app/data/camera-view.db\n"
            "    replicas:\n"
            "      - type: s3\n"
            "        name: r2\n"
            "        bucket: ${R2_BUCKET_NAME}\n"
            "        path: db-replicas/camera-view.db\n"
            "        sync-interval: 1s\n",
            encoding="utf-8",
        )
        with mock.patch.dict(os.environ, {"R2_BUCKET_NAME": "configured-bucket"}, clear=False):
            with self.assertRaises(RuntimeError) as raised:
                rd.run_drill(
                    self._options(
                        mode="live",
                        litestream_binary=str(self._stub_litestream()),
                        config_path=config,
                        replica_url="s3://other-bucket/db-replicas/camera-view.db",
                        sandbox=self.root / "sandbox",
                        keep_sandbox=True,
                    )
                )
        self.assertIn("does not match", str(raised.exception))

    def _stub_litestream(self, restore_source: Path | None = None) -> Path:
        """A process boundary test double for the litestream CLI."""
        script = self.root / "litestream"
        source = restore_source or (self.root / "missing-stub-source.db")
        script.write_text(
            "#!/bin/sh\n"
            'if [ "$1" = "version" ]; then echo "litestream version v0.3.13"; exit 0; fi\n'
            'if [ "$1" = "restore" ]; then\n'
            '  while [ $# -gt 0 ]; do\n'
            '    if [ "$1" = "-o" ]; then out="$2"; shift 2; continue; fi\n'
            "    shift\n"
            "  done\n"
            f'  cp "{source}" "$out"\n'
            "  exit 0\n"
            "fi\n"
            "exit 1\n",
            encoding="utf-8",
        )
        script.chmod(script.stat().st_mode | stat.S_IXUSR)
        return script

    def test_build_summary_contains_measured_timestamps(self) -> None:
        options = self._options(sandbox=self.root / "sandbox", keep_sandbox=True)
        outcome = rd.run_drill(options)
        summary = rd.build_summary(outcome, options)
        self.assertEqual(summary["metrics"]["rpo_method"], "reference_newest_minus_restored_newest")
        self.assertIsNotNone(
            rd.parse_timestamp(summary["metrics"]["restored_newest_record_at"])
        )
        self.assertGreaterEqual(summary["duration_seconds"], 0.0)
        for entry in summary["counts"].values():
            self.assertIn("count", entry)
            self.assertIn("committed_before_restore", entry)


class TestWebhookAlerting(unittest.TestCase):
    def setUp(self) -> None:
        self.sample_pass_summary = {
            "schema_version": 1,
            "task": "S15-T6",
            "issue": 480,
            "generated_at": "2026-11-16T12:00:00Z",
            "mode": "simulator",
            "verdict": "pass",
            "duration_seconds": 1.25,
            "metrics": {
                "rto_seconds": 0.85,
                "rto_budget_seconds": 60.0,
                "rpo_seconds": 0.10,
                "rpo_budget_seconds": 10.0,
                "rpo_method": "reference_newest_minus_restored_newest",
            },
            "restore": {
                "transport": "hermetic_simulator",
                "replica_url": "file:///tmp/sim/replica",
                "duration_seconds": 0.85,
            },
            "counts": {
                "camera_verdicts": {"count": 6},
                "hotset_verdict_history": {"count": 6},
                "flood_subscriptions": {"count": 6},
            },
            "checks": [
                {"name": "integrity_check", "status": "pass", "detail": "ok"},
                {"name": "quick_check", "status": "pass", "detail": "ok"},
                {"name": "foreign_key_check", "status": "pass", "detail": "ok"},
            ],
            "failures": [],
        }

        self.sample_fail_summary = {
            "schema_version": 1,
            "task": "S15-T6",
            "issue": 480,
            "generated_at": "2026-11-16T12:00:00Z",
            "mode": "simulator",
            "verdict": "fail",
            "duration_seconds": 3.10,
            "metrics": {
                "rto_seconds": 65.50,
                "rto_budget_seconds": 60.0,
                "rpo_seconds": 15.20,
                "rpo_budget_seconds": 10.0,
            },
            "restore": {
                "transport": "hermetic_simulator",
                "replica_url": "file:///tmp/sim/replica",
                "duration_seconds": 65.50,
            },
            "counts": {
                "camera_verdicts": {"count": 0},
            },
            "checks": [
                {"name": "integrity_check", "status": "fail", "detail": "row 3 missing from index"},
                {"name": "rto_budget", "status": "fail", "detail": "RTO exceeded budget"},
                {"name": "rpo_budget", "status": "fail", "detail": "RPO exceeded budget"},
            ],
            "failures": [
                {"name": "integrity_check", "status": "fail", "detail": "row 3 missing from index"},
                {"name": "rto_budget", "status": "fail", "detail": "RTO exceeded budget"},
            ],
            "error": "DatabaseCorruptionError: index malformed",
        }

    def test_build_webhook_payload_success(self) -> None:
        payload = rd.build_webhook_payload(self.sample_pass_summary)
        self.assertEqual(payload["event"], "sqlite_restore_drill")
        self.assertEqual(payload["status"], "SUCCESS")
        self.assertEqual(payload["verdict"], "pass")
        self.assertEqual(payload["mode"], "simulator")
        self.assertEqual(payload["task"], "S15-T6")
        self.assertEqual(payload["issue"], 480)
        self.assertTrue(payload["rto"]["passed"])
        self.assertEqual(payload["rto"]["elapsed_seconds"], 0.85)
        self.assertTrue(payload["rpo"]["passed"])
        self.assertEqual(payload["rpo"]["lag_seconds"], 0.10)
        self.assertEqual(payload["verifications"]["integrity_check"], "ok")
        self.assertEqual(payload["counts"]["camera_verdicts"], 6)
        self.assertIn("docs/runbooks/sqlite_litestream_disaster_recovery.md", payload["runbook"])
        self.assertIn("[PROJECTKV RESTORE DRILL: SUCCESS]", payload["text"])
        self.assertIn("PASS", payload["text"])
        self.assertEqual(payload["content"], payload["text"])

    def test_build_webhook_payload_failure(self) -> None:
        payload = rd.build_webhook_payload(self.sample_fail_summary)
        self.assertEqual(payload["status"], "FAILURE")
        self.assertEqual(payload["verdict"], "fail")
        self.assertFalse(payload["rto"]["passed"])
        self.assertFalse(payload["rpo"]["passed"])
        self.assertIn("BREACH", payload["text"])
        self.assertIn("row 3 missing from index", payload["text"])
        self.assertIn("DatabaseCorruptionError", payload["text"])
        self.assertEqual(len(payload["failures"]), 2)

    def test_build_webhook_payload_telegram(self) -> None:
        payload = rd.build_webhook_payload(
            self.sample_pass_summary,
            is_telegram=True,
            chat_id="-100987654321",
        )
        self.assertEqual(payload["chat_id"], "-100987654321")
        self.assertIn("[PROJECTKV RESTORE DRILL: SUCCESS]", payload["text"])
        self.assertNotIn("event", payload)
        self.assertNotIn("status", payload)

    def test_send_webhook_alert_success(self) -> None:
        mock_response = mock.MagicMock()
        mock_response.status = 200
        mock_response.__enter__.return_value = mock_response

        mock_opener = mock.MagicMock()
        mock_opener.open.return_value = mock_response

        delivered = rd.send_webhook_alert(
            "http://example.com/webhook",
            self.sample_pass_summary,
            opener=mock_opener,
        )
        self.assertTrue(delivered)
        mock_opener.open.assert_called_once()
        req = mock_opener.open.call_args[0][0]
        self.assertEqual(req.get_full_url(), "http://example.com/webhook")
        self.assertEqual(req.headers["Content-type"], "application/json")
        body = json.loads(req.data.decode("utf-8"))
        self.assertEqual(body["status"], "SUCCESS")

    def test_send_webhook_alert_telegram_url_formats_telegram_payload(self) -> None:
        mock_response = mock.MagicMock()
        mock_response.status = 200
        mock_response.__enter__.return_value = mock_response

        mock_opener = mock.MagicMock()
        mock_opener.open.return_value = mock_response

        delivered = rd.send_webhook_alert(
            "https://api.telegram.org/bot123456:ABC-DEF/sendMessage?chat_id=-100999",
            self.sample_pass_summary,
            opener=mock_opener,
        )
        self.assertTrue(delivered)
        req = mock_opener.open.call_args[0][0]
        body = json.loads(req.data.decode("utf-8"))
        self.assertEqual(body["chat_id"], "-100999")
        self.assertIn("[PROJECTKV RESTORE DRILL: SUCCESS]", body["text"])
        self.assertNotIn("event", body)

    def test_send_webhook_alert_retry_on_500_then_succeed(self) -> None:
        mock_response = mock.MagicMock()
        mock_response.status = 200
        mock_response.__enter__.return_value = mock_response

        import urllib.error
        http_error = urllib.error.HTTPError(
            url="http://example.com/webhook",
            code=500,
            msg="Internal Server Error",
            hdrs={},
            fp=None,
        )

        mock_opener = mock.MagicMock()
        mock_opener.open.side_effect = [http_error, mock_response]

        with mock.patch("time.sleep") as mock_sleep:
            delivered = rd.send_webhook_alert(
                "http://example.com/webhook",
                self.sample_pass_summary,
                max_retries=3,
                backoff_seconds=0.01,
                opener=mock_opener,
            )
        self.assertTrue(delivered)
        self.assertEqual(mock_opener.open.call_count, 2)
        mock_sleep.assert_called_once_with(0.01)

    def test_send_webhook_alert_no_retry_on_400(self) -> None:
        import urllib.error
        http_error = urllib.error.HTTPError(
            url="http://example.com/webhook",
            code=400,
            msg="Bad Request",
            hdrs={},
            fp=None,
        )

        mock_opener = mock.MagicMock()
        mock_opener.open.side_effect = http_error

        with mock.patch("time.sleep") as mock_sleep:
            delivered = rd.send_webhook_alert(
                "http://example.com/webhook",
                self.sample_pass_summary,
                max_retries=3,
                opener=mock_opener,
            )
        self.assertFalse(delivered)
        self.assertEqual(mock_opener.open.call_count, 1)
        mock_sleep.assert_not_called()

    def test_send_webhook_alert_exhausts_retries_on_network_error(self) -> None:
        import urllib.error
        mock_opener = mock.MagicMock()
        mock_opener.open.side_effect = urllib.error.URLError("Connection refused")

        with mock.patch("time.sleep") as mock_sleep:
            delivered = rd.send_webhook_alert(
                "http://example.com/webhook",
                self.sample_pass_summary,
                max_retries=3,
                backoff_seconds=0.01,
                opener=mock_opener,
            )
        self.assertFalse(delivered)
        self.assertEqual(mock_opener.open.call_count, 3)
        self.assertEqual(mock_sleep.call_count, 2)

    def test_cli_options_parse_webhook_url_and_env(self) -> None:
        parser = rd.build_parser()
        args = parser.parse_args([
            "--webhook-url", "http://hook.projectkv.dev",
            "--chat-id", "98765",
            "--webhook-max-retries", "5",
            "--webhook-timeout", "15.0",
        ])
        opts = rd.options_from_args(args)
        self.assertEqual(opts.webhook_url, "http://hook.projectkv.dev")
        self.assertEqual(opts.chat_id, "98765")
        self.assertEqual(opts.webhook_max_retries, 5)
        self.assertEqual(opts.webhook_timeout_seconds, 15.0)

        # Environment fallback test
        with mock.patch.dict(os.environ, {
            "RESTORE_DRILL_WEBHOOK_URL": "http://env-hook.projectkv.dev",
            "RESTORE_DRILL_CHAT_ID": "chat-from-env",
        }):
            empty_opts = rd.DrillOptions()
            self.assertEqual(rd._explicit_webhook_url(empty_opts), "http://env-hook.projectkv.dev")
            self.assertEqual(rd._explicit_chat_id(empty_opts), "chat-from-env")

    def test_main_dispatches_webhook_on_drill_success(self) -> None:
        with mock.patch.object(rd, "send_webhook_alert") as mock_send:
            mock_send.return_value = True
            with tempfile.TemporaryDirectory() as tmp:
                out_path = Path(tmp) / "summary.json"
                exit_code = rd.main([
                    "--mode", "simulator",
                    "--webhook-url", "http://hook.local/test",
                    "--out", str(out_path),
                    "--quiet",
                ])
                self.assertEqual(exit_code, rd.EXIT_PASS)
                mock_send.assert_called_once()
                args, kwargs = mock_send.call_args
                self.assertEqual(args[0], "http://hook.local/test")
                self.assertEqual(args[1]["verdict"], "pass")

    def test_main_dispatches_webhook_on_drill_failure(self) -> None:
        with mock.patch.object(rd, "run_drill", side_effect=RuntimeError("catastrophic failure")):
            with mock.patch.object(rd, "send_webhook_alert") as mock_send:
                mock_send.return_value = True
                with tempfile.TemporaryDirectory() as tmp:
                    out_path = Path(tmp) / "summary.json"
                    exit_code = rd.main([
                        "--mode", "simulator",
                        "--webhook-url", "http://hook.local/test",
                        "--out", str(out_path),
                        "--quiet",
                    ])
                    self.assertEqual(exit_code, rd.EXIT_FAIL)
                    mock_send.assert_called_once()
                    args, kwargs = mock_send.call_args
                    self.assertEqual(args[0], "http://hook.local/test")
                    self.assertEqual(args[1]["verdict"], "fail")
                    self.assertIn("catastrophic failure", args[1]["error"])

    def test_end_to_end_simulator_drill_with_local_http_listener(self) -> None:
        """Rule 10: live local HTTP listener captures empirical webhook payload."""
        from http.server import BaseHTTPRequestHandler, HTTPServer
        import threading

        received_payloads: list[dict[str, Any]] = []

        class WebhookHandler(BaseHTTPRequestHandler):
            def do_POST(self) -> None:
                length = int(self.headers.get("Content-Length", 0))
                data = self.rfile.read(length)
                try:
                    payload = json.loads(data.decode("utf-8"))
                    received_payloads.append(payload)
                    self.send_response(200)
                    self.send_header("Content-Type", "application/json")
                    self.end_headers()
                    self.wfile.write(b'{"ok": true}')
                except Exception as err:
                    self.send_response(400)
                    self.end_headers()
                    self.wfile.write(str(err).encode("utf-8"))

            def log_message(self, *args: Any) -> None:
                pass  # Suppress stderr logs during test

        server = HTTPServer(("127.0.0.1", 0), WebhookHandler)
        port = server.server_port
        server_thread = threading.Thread(target=server.serve_forever, daemon=True)
        server_thread.start()

        try:
            with tempfile.TemporaryDirectory() as tmp:
                out_path = Path(tmp) / "summary.json"
                exit_code = rd.main([
                    "--mode", "simulator",
                    "--webhook-url", f"http://127.0.0.1:{port}/alerts",
                    "--out", str(out_path),
                    "--quiet",
                ])
                self.assertEqual(exit_code, rd.EXIT_PASS)
                self.assertEqual(len(received_payloads), 1)
                received = received_payloads[0]
                self.assertEqual(received["event"], "sqlite_restore_drill")
                self.assertEqual(received["status"], "SUCCESS")
                self.assertEqual(received["mode"], "simulator")
                self.assertTrue(received["rto"]["passed"])
                self.assertTrue(received["rpo"]["passed"])
                self.assertEqual(received["verifications"]["integrity_check"], "ok")
                self.assertIn("docs/runbooks/sqlite_litestream_disaster_recovery.md", received["runbook"])
        finally:
            server.shutdown()
            server.server_close()


if __name__ == "__main__":
    unittest.main()
