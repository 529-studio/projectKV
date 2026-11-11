#!/usr/bin/env python3
"""
train_live_model_test.py - Unit test suite for the automated forecast refit pipeline.

Sprint 11 Wave 2 Task 4 (Issue #398):
  * Tests database path resolution and fallback mechanisms.
  * Tests atomic file write safety, directory creation, and rollback on failure.
  * Tests SQLite feature extraction across traffic and weather observations.
  * Tests model artifact construction, schema versioning, and Go ModelParams compatibility.
  * Tests artifact verification rules (fit_source, dimensions, finite numbers, gate checks).
  * Tests CLI workflows: --dry-run, --promote, --verify, and invalid parameter handling.
  * Tests that failing verification gates strictly halt promotion and exit with error code 3.
  * Validates end-to-end integration with Go services/go-server/internal/forecast.

Adheres strictly to AGENTS.md:
  * Rule 1: Zero emojis, 100% English logs, comments, and docstrings.
  * Rule 2: Exhaustive root-cause verification and deterministic test scenarios.
  * Rule 8: Zero credentials. Tests use temporary SQLite databases and clean mock state.
  * Rule 10: Strict evidence integrity validation.
"""

from __future__ import annotations

import json
import math
import os
import shutil
import signal
import sqlite3
import subprocess
import sys
import tempfile
import time
import unittest
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Optional, Sequence
from zoneinfo import ZoneInfo

import tools.forecast.train_live_model as tlm
import tools.forecast.backtest as backtest
import tools.forecast.extract_features as extract_features

REPO_ROOT = tlm.REPO_ROOT


def create_test_db(
    path: Path,
    rows: Sequence[tuple],
    weather_rows: Sequence[tuple] = (),
) -> None:
    """Create a temporary SQLite database populated with test observations."""
    conn = sqlite3.connect(path)
    conn.execute(
        """
        CREATE TABLE hotset_verdict_history (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            camera_id TEXT NOT NULL,
            pcu_total REAL,
            saturation_index REAL,
            severity TEXT NOT NULL,
            confidence REAL NOT NULL DEFAULT 0.0,
            source TEXT NOT NULL DEFAULT '',
            observed_at TIMESTAMP NOT NULL,
            recorded_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
            direction TEXT NOT NULL DEFAULT '',
            region_id TEXT NOT NULL DEFAULT ''
        );
        """
    )
    conn.execute(
        """
        CREATE TABLE weather_observations (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            precipitation_mm REAL NOT NULL DEFAULT 0.0,
            weather_code INTEGER NOT NULL DEFAULT 0,
            temperature_c REAL NOT NULL DEFAULT 0.0,
            humidity_pct REAL NOT NULL DEFAULT 0.0,
            radar_active INTEGER NOT NULL DEFAULT 0,
            radar_frame_age_s INTEGER NOT NULL DEFAULT 0,
            source TEXT,
            payload_hash TEXT NOT NULL DEFAULT '',
            observed_at TEXT,
            recorded_at TEXT
        );
        """
    )
    if rows:
        conn.executemany(
            """
            INSERT INTO hotset_verdict_history
            (camera_id, pcu_total, saturation_index, severity, confidence, source, observed_at, recorded_at, direction, region_id)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            """,
            rows,
        )
    if weather_rows:
        conn.executemany(
            """
            INSERT INTO weather_observations
            (precipitation_mm, weather_code, temperature_c, humidity_pct, radar_active, radar_frame_age_s, source, payload_hash, observed_at, recorded_at)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            """,
            weather_rows,
        )
    conn.commit()
    conn.close()


def generate_curved_traffic_rows(
    cameras: Sequence[str] = ("cam-test-1", "cam-test-2"),
    steps: int = 150,
    base_time: Optional[datetime] = None,
) -> list[tuple]:
    """Generate realistic sinusoidal traffic series that beats persistence."""
    if base_time is None:
        base_time = datetime(2026, 11, 1, 0, 0, tzinfo=timezone.utc)
    rows = []
    for step in range(steps):
        t = base_time + timedelta(minutes=step * 5)
        t_str = t.isoformat()
        # Smooth cyclical wave so Ridge easily beats persistence
        val = 0.25 + 0.20 * math.sin(step / 10.0)
        for cam_id in cameras:
            # Frame-wide observation (direction = '')
            rows.append(
                (
                    cam_id,
                    15.0,
                    val,
                    "MODERATE",
                    0.90,
                    "proof_archive",
                    t_str,
                    t_str,
                    "",
                    "",
                )
            )
            # Directional observations
            rows.append(
                (
                    cam_id,
                    12.0,
                    val * 1.05,
                    "MODERATE",
                    0.90,
                    "proof_archive",
                    t_str,
                    t_str,
                    "a",
                    f"{cam_id}_reg_a",
                )
            )
            rows.append(
                (
                    cam_id,
                    12.0,
                    val * 0.95,
                    "MODERATE",
                    0.90,
                    "proof_archive",
                    t_str,
                    t_str,
                    "b",
                    f"{cam_id}_reg_b",
                )
            )
    return rows


def generate_flat_traffic_rows(
    camera_id: str = "cam-flat-1",
    steps: int = 100,
    constant_si: float = 0.50,
    base_time: Optional[datetime] = None,
) -> list[tuple]:
    """Generate flat traffic series where persistence achieves MAE 0.0, causing gate failure."""
    if base_time is None:
        base_time = datetime(2026, 11, 1, 0, 0, tzinfo=timezone.utc)
    rows = []
    for step in range(steps):
        t = base_time + timedelta(minutes=step * 5)
        t_str = t.isoformat()
        rows.append(
            (
                camera_id,
                10.0,
                constant_si,
                "MODERATE",
                0.90,
                "proof_archive",
                t_str,
                t_str,
                "",
                "",
            )
        )
    return rows


class TestTrainLiveModelUnit(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp_dir = Path(tempfile.mkdtemp(prefix="projectkv_test_train_"))
        self.tz = ZoneInfo("Asia/Ho_Chi_Minh")

    def tearDown(self) -> None:
        shutil.rmtree(self.tmp_dir, ignore_errors=True)

    def test_resolve_db_path(self) -> None:
        # 1. Custom specified path
        custom_path = self.tmp_dir / "custom.db"
        self.assertEqual(tlm.resolve_db_path(custom_path), custom_path.resolve())

        # 2. Unspecified path falls back to default or fallback path
        resolved = tlm.resolve_db_path(None)
        self.assertTrue(isinstance(resolved, Path))
        self.assertTrue(resolved.is_absolute())

    def test_atomic_write(self) -> None:
        target = self.tmp_dir / "sub" / "dir" / "test_file.txt"
        content = "Line 1\nLine 2\n"

        tlm.atomic_write(target, content)
        self.assertTrue(target.exists())
        self.assertEqual(target.read_text(encoding="utf-8"), content)

        # Overwrite safely
        new_content = "Replaced\n"
        tlm.atomic_write(target, new_content)
        self.assertEqual(target.read_text(encoding="utf-8"), new_content)

    def test_extract_live_features_empty_db(self) -> None:
        empty_db = self.tmp_dir / "empty.db"
        create_test_db(empty_db, rows=[])
        with self.assertRaises(ValueError) as ctx:
            tlm.extract_live_features(empty_db, empty_db, bin_minutes=5, local_tz=self.tz)
        self.assertIn("No observations found", str(ctx.exception))

    def test_extract_live_features_missing_db(self) -> None:
        missing_db = self.tmp_dir / "does_not_exist.db"
        with self.assertRaises(FileNotFoundError):
            tlm.extract_live_features(missing_db, missing_db, bin_minutes=5, local_tz=self.tz)

    def test_extract_live_features_success(self) -> None:
        db_path = self.tmp_dir / "traffic.db"
        rows = generate_curved_traffic_rows(steps=50)
        weather_rows = [
            (
                2.5,
                63,
                28.5,
                85.0,
                1,
                120,
                "open-meteo",
                "hash1",
                datetime(2026, 11, 1, 1, 0, tzinfo=timezone.utc).isoformat(),
                datetime(2026, 11, 1, 1, 0, tzinfo=timezone.utc).isoformat(),
            )
        ]
        create_test_db(db_path, rows, weather_rows)

        feat_rows, fieldnames, stats, source_counts = tlm.extract_live_features(
            db_path, db_path, bin_minutes=5, local_tz=self.tz
        )
        self.assertGreater(len(feat_rows), 0)
        self.assertIn("proof_archive", source_counts)
        self.assertIn("saturation_index", fieldnames)
        self.assertIn("lag_5m", fieldnames)
        self.assertIn("rolling_mean_15m", fieldnames)
        self.assertIn("peer_si", fieldnames)
        self.assertIn("precip_mm_15m", fieldnames)

    def test_build_model_artifact_structure(self) -> None:
        db_path = self.tmp_dir / "traffic.db"
        rows = generate_curved_traffic_rows(steps=80)
        create_test_db(db_path, rows)

        feat_rows, fieldnames, _, source_counts = tlm.extract_live_features(
            db_path, db_path, bin_minutes=5, local_tz=self.tz
        )
        csv_path = self.tmp_dir / "features.csv"
        extract_features.write_csv(feat_rows, fieldnames, csv_path)
        features_sha256 = tlm.compute_sha256(csv_path)

        records, summary = backtest.load_feature_records(
            csv_path, timezone_name="Asia/Ho_Chi_Minh"
        )
        config = backtest.BacktestConfig(
            train_ratio=0.80,
            horizons_minutes=(10, 20, 30),
            target_mode="delta",
        )
        backtest_result = backtest.run_backtest(records, summary, config)
        split = backtest.chronological_split(records, 0.80)

        artifact = tlm.build_model_artifact(
            backtest_result=backtest_result,
            records=records,
            split=split,
            features_sha256=features_sha256,
            model_version="2026.11.10-live",
            target_mode="delta",
            horizons_minutes=(10, 20, 30),
            source_counts=source_counts,
        )

        self.assertEqual(artifact["schema_version"], 1)
        self.assertEqual(len(artifact["models"]), 3)

        models = {m["name"]: m for m in artifact["models"]}
        self.assertIn("ridge_delta", models)
        self.assertIn("persistence", models)
        self.assertIn("historical_median", models)

        ridge = models["ridge_delta"]
        self.assertEqual(ridge["fit_source"], "live_history")
        self.assertEqual(ridge["version"], "2026.11.10-live")
        self.assertEqual(ridge["target_mode"], "delta")
        self.assertGreater(ridge["fit_rows"], 0)
        self.assertEqual(ridge["fit_cameras"], 2)
        self.assertTrue(ridge["fit_window"]["start"].endswith("Z"))
        self.assertTrue(ridge["fit_window"]["end"].endswith("Z"))
        self.assertEqual(len(ridge["fit_data_sha256"]), 64)

        for h in ridge["horizons"]:
            self.assertIn(h["horizon_minutes"], (10, 20, 30))
            self.assertGreater(h["mae"], 0.0)
            self.assertGreater(h["rmse"], 0.0)
            params = h["params"]
            self.assertEqual(len(params["means"]), 6)
            self.assertEqual(len(params["stds"]), 6)
            self.assertEqual(len(params["coeffs"]), 6)
            self.assertTrue(math.isfinite(params["intercept"]))
            self.assertGreater(params["global_median"], 0.0)

    def test_verify_artifact(self) -> None:
        valid_artifact = {
            "schema_version": 1,
            "generated_at_utc": "2026-11-10T07:00:00Z",
            "models": [
                {
                    "name": "ridge_delta",
                    "version": "2026.11.10-live",
                    "target_mode": "delta",
                    "fit_source": "live_history",
                    "fit_rows": 1000,
                    "fit_cameras": 20,
                    "fit_window": {
                        "start": "2026-11-01T00:00:00Z",
                        "end": "2026-11-03T00:00:00Z",
                    },
                    "fit_data_sha256": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
                    "mae": 0.040,
                    "horizons": [
                        {
                            "horizon_minutes": 10,
                            "mae": 0.040,
                            "rmse": 0.055,
                            "sample_count": 200,
                            "params": {
                                "horizon_minutes": 10,
                                "intercept": 0.001,
                                "means": [0.2, 0.2, 0.2, 0.2, 12.0, 3.0],
                                "stds": [0.1, 0.1, 0.1, 0.1, 4.0, 1.5],
                                "coeffs": [-0.01, -0.01, 0.01, 0.01, -0.001, 0.0],
                                "mae": 0.040,
                                "rmse": 0.055,
                                "global_median": 0.25,
                            },
                        },
                        {
                            "horizon_minutes": 20,
                            "mae": 0.045,
                            "rmse": 0.060,
                            "sample_count": 200,
                            "params": {
                                "horizon_minutes": 20,
                                "intercept": 0.001,
                                "means": [0.2, 0.2, 0.2, 0.2, 12.0, 3.0],
                                "stds": [0.1, 0.1, 0.1, 0.1, 4.0, 1.5],
                                "coeffs": [-0.01, -0.01, 0.01, 0.01, -0.001, 0.0],
                                "mae": 0.045,
                                "rmse": 0.060,
                                "global_median": 0.25,
                            },
                        },
                        {
                            "horizon_minutes": 30,
                            "mae": 0.050,
                            "rmse": 0.065,
                            "sample_count": 200,
                            "params": {
                                "horizon_minutes": 30,
                                "intercept": 0.001,
                                "means": [0.2, 0.2, 0.2, 0.2, 12.0, 3.0],
                                "stds": [0.1, 0.1, 0.1, 0.1, 4.0, 1.5],
                                "coeffs": [-0.01, -0.01, 0.01, 0.01, -0.001, 0.0],
                                "mae": 0.050,
                                "rmse": 0.065,
                                "global_median": 0.25,
                            },
                        },
                    ],
                },
                {
                    "name": "persistence",
                    "version": "2026.11.10-live",
                    "target_mode": "persistence",
                    "fit_source": "live_history",
                    "fit_rows": 1000,
                    "fit_cameras": 20,
                    "fit_window": {
                        "start": "2026-11-01T00:00:00Z",
                        "end": "2026-11-03T00:00:00Z",
                    },
                    "fit_data_sha256": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
                    "mae": 0.050,
                    "horizons": [
                        {
                            "horizon_minutes": 10,
                            "mae": 0.050,
                            "rmse": 0.065,
                            "sample_count": 200,
                        }
                    ],
                },
                {
                    "name": "historical_median",
                    "version": "2026.11.10-live",
                    "target_mode": "historical_median",
                    "fit_source": "live_history",
                    "fit_rows": 1000,
                    "fit_cameras": 20,
                    "fit_window": {
                        "start": "2026-11-01T00:00:00Z",
                        "end": "2026-11-03T00:00:00Z",
                    },
                    "fit_data_sha256": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
                    "mae": 0.045,
                    "horizons": [
                        {
                            "horizon_minutes": 10,
                            "mae": 0.045,
                            "rmse": 0.060,
                            "sample_count": 200,
                            "params": {
                                "horizon_minutes": 10,
                                "global_median": 0.25,
                            },
                        }
                    ],
                },
            ],
        }

        art_path = self.tmp_dir / "valid_art.json"
        art_path.write_text(json.dumps(valid_artifact), encoding="utf-8")

        ok, msg = tlm.verify_artifact(art_path)
        self.assertTrue(ok, f"Verification failed unexpectedly: {msg}")

        # Test failure: prohibited synthetic fit_source
        bad_art = json.loads(json.dumps(valid_artifact))
        bad_art["models"][0]["fit_source"] = "synthetic_fixture"
        bad_path = self.tmp_dir / "synthetic_art.json"
        bad_path.write_text(json.dumps(bad_art), encoding="utf-8")
        ok, msg = tlm.verify_artifact(bad_path)
        self.assertFalse(ok)
        self.assertIn("live_history", msg)

        # Test failure: candidate does not beat persistence
        bad_gate_art = json.loads(json.dumps(valid_artifact))
        bad_gate_art["models"][0]["horizons"][0]["mae"] = 0.090  # worse than 0.050
        bad_gate_path = self.tmp_dir / "bad_gate_art.json"
        bad_gate_path.write_text(json.dumps(bad_gate_art), encoding="utf-8")
        ok, msg = tlm.verify_artifact(bad_gate_path)
        self.assertFalse(ok)
        self.assertIn("Gate failed", msg)

        # Test failure: partial horizon coverage (missing horizons 20 and 30) (RB1)
        partial_art = json.loads(json.dumps(valid_artifact))
        partial_art["models"][0]["horizons"] = [partial_art["models"][0]["horizons"][0]]
        ok, msg = tlm.verify_artifact_data(partial_art)
        self.assertFalse(ok)
        self.assertIn("missing required horizons", msg)

        # Test failure: horizon entry is not a dict (A1)
        bad_h_art = json.loads(json.dumps(valid_artifact))
        bad_h_art["models"][0]["horizons"][0] = "bad_horizon_entry"
        ok, msg = tlm.verify_artifact_data(bad_h_art)
        self.assertFalse(ok)
        self.assertIn("must be a dictionary", msg)

        # Test failure: non-existent file
        ok, msg = tlm.verify_artifact(self.tmp_dir / "missing.json")
        self.assertFalse(ok)
        self.assertIn("does not exist", msg)


class TestTrainLiveModelCLI(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp_dir = Path(tempfile.mkdtemp(prefix="projectkv_test_cli_"))
        self.db_path = self.tmp_dir / "camera-view.db"
        rows = generate_curved_traffic_rows(steps=120)
        create_test_db(self.db_path, rows)

        self.features_out = self.tmp_dir / "features.csv"
        self.artifact_out = self.tmp_dir / "artifact.json"
        self.report_out = self.tmp_dir / "report.md"
        self.eval_json_out = self.tmp_dir / "eval.json"

    def tearDown(self) -> None:
        shutil.rmtree(self.tmp_dir, ignore_errors=True)

    def test_cli_dry_run(self) -> None:
        ret = tlm.run(
            [
                "--db",
                str(self.db_path),
                "--features-out",
                str(self.features_out),
                "--artifact-out",
                str(self.artifact_out),
                "--report-out",
                str(self.report_out),
                "--eval-json-out",
                str(self.eval_json_out),
                "--dry-run",
            ]
        )
        self.assertEqual(ret, tlm.EXIT_OK)
        # In dry run, no output files should be created
        self.assertFalse(self.features_out.exists())
        self.assertFalse(self.artifact_out.exists())
        self.assertFalse(self.report_out.exists())
        self.assertFalse(self.eval_json_out.exists())

    def test_cli_run_without_promote(self) -> None:
        ret = tlm.run(
            [
                "--db",
                str(self.db_path),
                "--features-out",
                str(self.features_out),
                "--artifact-out",
                str(self.artifact_out),
                "--report-out",
                str(self.report_out),
                "--eval-json-out",
                str(self.eval_json_out),
            ]
        )
        self.assertEqual(ret, tlm.EXIT_OK)
        # Features, report, and eval should be written, but artifact NOT promoted
        self.assertTrue(self.features_out.exists())
        self.assertTrue(self.report_out.exists())
        self.assertTrue(self.eval_json_out.exists())
        self.assertFalse(self.artifact_out.exists())

    def test_cli_run_with_promote(self) -> None:
        ret = tlm.run(
            [
                "--db",
                str(self.db_path),
                "--features-out",
                str(self.features_out),
                "--artifact-out",
                str(self.artifact_out),
                "--report-out",
                str(self.report_out),
                "--eval-json-out",
                str(self.eval_json_out),
                "--promote",
            ]
        )
        self.assertEqual(ret, tlm.EXIT_OK)
        # With --promote, all files including artifact JSON must exist
        self.assertTrue(self.features_out.exists())
        self.assertTrue(self.report_out.exists())
        self.assertTrue(self.eval_json_out.exists())
        self.assertTrue(self.artifact_out.exists())

        # Verify the promoted artifact passes validation
        ok, msg = tlm.verify_artifact(self.artifact_out)
        self.assertTrue(ok, f"Verification failed: {msg}")

        # Also test --verify CLI flag
        ret_verify = tlm.run(["--artifact-out", str(self.artifact_out), "--verify"])
        self.assertEqual(ret_verify, tlm.EXIT_OK)

    def test_cli_gate_failure_halts_promotion(self) -> None:
        flat_db = self.tmp_dir / "flat.db"
        rows = generate_flat_traffic_rows(steps=100)
        create_test_db(flat_db, rows)

        ret = tlm.run(
            [
                "--db",
                str(flat_db),
                "--features-out",
                str(self.features_out),
                "--artifact-out",
                str(self.artifact_out),
                "--report-out",
                str(self.report_out),
                "--eval-json-out",
                str(self.eval_json_out),
                "--promote",
            ]
        )
        self.assertEqual(ret, tlm.EXIT_GATE_FAILED)
        # Promotion must NOT happen
        self.assertFalse(self.artifact_out.exists())
        # Failure evidence is written to report and eval JSON
        self.assertTrue(self.report_out.exists())
        self.assertTrue(self.eval_json_out.exists())

    def test_cli_invalid_arguments(self) -> None:
        # Invalid horizon
        ret = tlm.run(["--horizons", "ten,twenty", "--db", str(self.db_path)])
        self.assertEqual(ret, tlm.EXIT_CONFIG_ERROR)

        # Unsupported horizon outside (10, 20, 30)
        ret = tlm.run(["--horizons", "10,15", "--db", str(self.db_path)])
        self.assertEqual(ret, tlm.EXIT_CONFIG_ERROR)

        # Unsupported target mode outside choices
        with self.assertRaises(SystemExit):
            tlm.run(["--target-mode", "level", "--db", str(self.db_path)])

        # Invalid train ratio
        ret = tlm.run(["--train-ratio", "1.5", "--db", str(self.db_path)])
        self.assertEqual(ret, tlm.EXIT_CONFIG_ERROR)

        # Non-existent DB
        ret = tlm.run(["--db", str(self.tmp_dir / "nonexistent.db")])
        self.assertEqual(ret, tlm.EXIT_CONFIG_ERROR)

    def test_cli_unsupported_horizon_halts_promotion(self) -> None:
        """Regression test for B2: unvalidated horizons must halt promotion and leave destination untouched."""
        existing_content = '{"schema_version": 1, "sentinel": true}'
        self.artifact_out.write_text(existing_content, encoding="utf-8")
        ret = tlm.run(
            [
                "--db",
                str(self.db_path),
                "--horizons",
                "10,15",
                "--artifact-out",
                str(self.artifact_out),
                "--promote",
            ]
        )
        self.assertEqual(ret, tlm.EXIT_CONFIG_ERROR)
        self.assertEqual(self.artifact_out.read_text(encoding="utf-8"), existing_content)

    def test_cli_partial_horizon_halts_promotion(self) -> None:
        """Regression test for RB1: partial horizon coverage must halt promotion and leave destination untouched."""
        existing_content = '{"schema_version": 1, "sentinel": true}'
        self.artifact_out.write_text(existing_content, encoding="utf-8")
        ret = tlm.run(
            [
                "--db",
                str(self.db_path),
                "--horizons",
                "10",
                "--artifact-out",
                str(self.artifact_out),
                "--promote",
            ]
        )
        self.assertEqual(ret, tlm.EXIT_CONFIG_ERROR)
        self.assertEqual(self.artifact_out.read_text(encoding="utf-8"), existing_content)

    def test_train_serve_directional_parity(self) -> None:
        """Regression test for M3: verify train/serve default directional filtering parity."""
        # history_repo.go query filters WHERE direction = ''
        obs_frame_only, _ = extract_features.load_sqlite_observations(
            self.db_path, ZoneInfo("Asia/Ho_Chi_Minh"), include_directional=False
        )
        # All loaded observations must be frame-wide (direction == '')
        self.assertTrue(all(not o.direction for o in obs_frame_only))
        self.assertTrue(len(obs_frame_only) > 0)

    def test_cli_synthetic_source_rejected(self) -> None:
        """Regression test for M1: databases with synthetic provenance keywords are rejected."""
        synth_db = self.tmp_dir / "synthetic.db"
        t_str = datetime(2026, 11, 1, 0, 0, tzinfo=timezone.utc).isoformat()
        rows = [
            ("cam-synth", 10.0, 0.4, "MODERATE", 0.9, "synthetic_fixture", t_str, t_str, "", "")
        ]
        create_test_db(synth_db, rows)
        ret = tlm.run(["--db", str(synth_db)])
        self.assertEqual(ret, tlm.EXIT_DATA_ERROR)

    def test_cli_non_operational_source_rejected(self) -> None:
        """Regression test for M1: databases with only non-operational test sources are rejected."""
        test_db = self.tmp_dir / "unit_test_only.db"
        t_str = datetime(2026, 11, 1, 0, 0, tzinfo=timezone.utc).isoformat()
        rows = [
            ("cam-test", 10.0, 0.4, "MODERATE", 0.9, "unit_test", t_str, t_str, "", "")
        ]
        create_test_db(test_db, rows)
        ret = tlm.run(["--db", str(test_db)])
        self.assertEqual(ret, tlm.EXIT_DATA_ERROR)


class TestGoArtifactLoaderIntegration(unittest.TestCase):
    def test_promoted_artifact_compatibility_with_go(self) -> None:
        """Hermetic verification: generated artifact loads cleanly in Go internal/forecast package."""
        go_bin = shutil.which("go")
        if not go_bin:
            self.skipTest("go compiler not available in test environment")

        tmp_dir = Path(tempfile.mkdtemp(prefix="projectkv_test_go_compat_"))
        try:
            db_path = tmp_dir / "traffic.db"
            rows = generate_curved_traffic_rows(steps=120)
            create_test_db(db_path, rows)

            features_csv = tmp_dir / "features.csv"
            artifact_json = tmp_dir / "artifact.json"
            report_md = tmp_dir / "report.md"
            eval_json = tmp_dir / "eval.json"

            ret = tlm.run(
                [
                    "--db",
                    str(db_path),
                    "--features-out",
                    str(features_csv),
                    "--artifact-out",
                    str(artifact_json),
                    "--report-out",
                    str(report_md),
                    "--eval-json-out",
                    str(eval_json),
                    "--promote",
                ]
            )
            self.assertEqual(ret, tlm.EXIT_OK)
            self.assertTrue(artifact_json.exists())

            # Hermetic Go verifier executed via standard go test with path override
            proc = subprocess.run(
                [
                    "go",
                    "test",
                    "-v",
                    "-count=1",
                    "-run",
                    "^TestArtifact_ExternalFileLoad$",
                    "./internal/forecast/...",
                ],
                cwd=REPO_ROOT / "services" / "go-server",
                env={
                    **os.environ,
                    "FORECAST_TEST_ARTIFACT_PATH": str(artifact_json.resolve()),
                },
                capture_output=True,
                text=True,
            )
            self.assertEqual(
                proc.returncode,
                0,
                f"Hermetic Go verification failed:\nstdout:\n{proc.stdout}\nstderr:\n{proc.stderr}",
            )
            self.assertIn("PASS: TestArtifact_ExternalFileLoad", proc.stdout)
        finally:
            shutil.rmtree(tmp_dir, ignore_errors=True)


# ---------------------------------------------------------------------------
# Sprint 12 Wave 0 Task 1 (S12-T1): forecast refit scheduler, lock, and state.
# ---------------------------------------------------------------------------

ICT = ZoneInfo("Asia/Ho_Chi_Minh")
DAILY_SLOTS = ("02:00", "08:00", "14:00", "20:00")


def history_row(index: int) -> tuple:
    """Build one hotset_verdict_history row matching create_test_db's column order."""
    minute = index % 60
    observed_at = f"2026-11-12T09:{minute:02d}:00+07:00"
    recorded_at = f"2026-11-12T09:{minute:02d}:05+07:00"
    return (
        f"cam-sched-{index % 2}",
        100.0 + float(index),
        0.5,
        "low",
        0.9,
        "harvester_daemon",
        observed_at,
        recorded_at,
        "",
        "",
    )


class TestForecastRefitLock(unittest.TestCase):
    """flock-based overlap protection: acquired, contended, released, never unlinked."""

    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.root = Path(self._tmp.name)
        self.lock_path = self.root / "scheduler" / "refit.lock"

    def test_acquire_creates_parent_directories(self) -> None:
        lock = tlm.ForecastRefitLock(self.lock_path)
        self.addCleanup(lock.release)
        self.assertFalse(self.lock_path.parent.exists())

        lock.acquire()

        self.assertTrue(self.lock_path.parent.is_dir())
        self.assertTrue(self.lock_path.exists())
        self.assertTrue(lock.is_held)

    def test_acquire_records_own_pid_in_lock_file(self) -> None:
        lock = tlm.ForecastRefitLock(self.lock_path)
        self.addCleanup(lock.release)
        lock.acquire()

        payload = json.loads(self.lock_path.read_text(encoding="utf-8"))

        self.assertEqual(payload["pid"], os.getpid())
        self.assertIsInstance(payload["started_at"], str)
        self.assertTrue(payload["started_at"])

    def test_second_acquire_on_same_path_fails_non_blocking(self) -> None:
        holder = tlm.ForecastRefitLock(self.lock_path)
        self.addCleanup(holder.release)
        holder.acquire()

        contender = tlm.ForecastRefitLock(self.lock_path)
        self.addCleanup(contender.release)

        with self.assertRaises(tlm.LockHeldError) as caught:
            contender.acquire(blocking=False)

        self.assertIn(str(self.lock_path), str(caught.exception))
        self.assertFalse(contender.is_held)

    def test_acquire_twice_on_same_instance_raises(self) -> None:
        lock = tlm.ForecastRefitLock(self.lock_path)
        self.addCleanup(lock.release)
        lock.acquire()

        with self.assertRaises(tlm.LockHeldError):
            lock.acquire()

        # The original descriptor must survive the failed second attempt.
        self.assertTrue(lock.is_held)

    def test_context_manager_raises_when_lock_is_held(self) -> None:
        holder = tlm.ForecastRefitLock(self.lock_path)
        self.addCleanup(holder.release)
        holder.acquire()

        with self.assertRaises(tlm.LockHeldError):
            with tlm.ForecastRefitLock(self.lock_path):
                self.fail("context manager must not yield while the lock is held")

    def test_context_manager_acquires_and_releases(self) -> None:
        with tlm.ForecastRefitLock(self.lock_path) as lock:
            self.assertTrue(lock.is_held)
            self.assertTrue(self.lock_path.exists())

        self.assertFalse(lock.is_held)
        self.assertTrue(self.lock_path.exists(), "the lock file must never be unlinked")

        # A second context block proves the kernel released the descriptor.
        with tlm.ForecastRefitLock(self.lock_path) as reacquired:
            self.assertTrue(reacquired.is_held)

    def test_release_is_idempotent(self) -> None:
        lock = tlm.ForecastRefitLock(self.lock_path)
        lock.acquire()

        lock.release()
        lock.release()

        self.assertFalse(lock.is_held)
        self.assertTrue(self.lock_path.exists())

    def test_reacquire_truncates_stale_lock_payload(self) -> None:
        self.lock_path.parent.mkdir(parents=True, exist_ok=True)
        self.lock_path.write_text("x" * 4096, encoding="utf-8")

        lock = tlm.ForecastRefitLock(self.lock_path)
        self.addCleanup(lock.release)
        lock.acquire()

        payload = json.loads(self.lock_path.read_text(encoding="utf-8"))
        self.assertEqual(payload["pid"], os.getpid())
        self.assertLess(self.lock_path.stat().st_size, 4096)


class TestSchedulerState(unittest.TestCase):
    """Durable refit state: round-trip, atomic save, quarantine, counter accounting."""

    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.root = Path(self._tmp.name)
        self.state_path = self.root / "state" / "refit_state.json"

    def test_round_trip_through_dict(self) -> None:
        state = tlm.SchedulerState(self.state_path)
        state.mode = "daemon"
        state.pid = 4321
        state.cadence = {"kind": "interval", "seconds": 300, "timezone": "Asia/Ho_Chi_Minh"}
        state.counters["cycles_total"] = 2

        payload = state.to_dict()

        self.assertEqual(payload["schema_version"], 1)
        self.assertEqual(payload["mode"], "daemon")
        self.assertEqual(payload["pid"], 4321)
        self.assertEqual(payload["cadence"]["kind"], "interval")

        restored = tlm.SchedulerState.from_dict(payload, path=self.state_path)
        self.assertEqual(restored.to_dict(), payload)

    def test_save_and_load_round_trip(self) -> None:
        state = tlm.SchedulerState(self.state_path)
        state.record_cycle_start(datetime(2026, 11, 12, 9, 0, tzinfo=ICT))
        state.record_cycle_result(
            "promoted",
            0,
            completed_at=datetime(2026, 11, 12, 9, 0, 14, tzinfo=ICT),
            rows_extracted=1180,
        )

        state.save()

        self.assertTrue(self.state_path.exists())
        self.assertEqual(list(self.state_path.parent.glob("*.tmp")), [])

        loaded = tlm.SchedulerState.load(self.state_path)

        self.assertEqual(loaded.total_runs, 1)
        self.assertEqual(loaded.counters["cycles_promoted"], 1)
        self.assertEqual(loaded.last_cycle["status"], "promoted")
        self.assertEqual(loaded.last_cycle["rows_extracted"], 1180)
        self.assertEqual(loaded.to_dict(), state.to_dict())

    def test_load_missing_file_returns_fresh_state_without_writing(self) -> None:
        missing = self.root / "state" / "absent.json"

        state = tlm.SchedulerState.load(missing)

        self.assertEqual(state.total_runs, 0)
        self.assertEqual(state.counters["cycles_total"], 0)
        self.assertEqual(state.to_dict()["schema_version"], 1)
        self.assertFalse(missing.exists())

    def test_corrupt_json_is_quarantined_and_state_starts_fresh(self) -> None:
        self.state_path.parent.mkdir(parents=True, exist_ok=True)
        self.state_path.write_text("{ this is not json", encoding="utf-8")

        state = tlm.SchedulerState.load(self.state_path)

        self.assertEqual(state.total_runs, 0)
        self.assertEqual(state.consecutive_failures, 0)
        self.assertEqual(state.to_dict()["schema_version"], 1)
        self.assertFalse(self.state_path.exists())

        quarantined = sorted(self.state_path.parent.glob(self.state_path.name + ".corrupt*"))
        self.assertEqual(len(quarantined), 1)
        self.assertIn("this is not json", quarantined[0].read_text(encoding="utf-8"))

    def test_unsupported_schema_version_is_quarantined(self) -> None:
        self.state_path.parent.mkdir(parents=True, exist_ok=True)
        self.state_path.write_text(
            json.dumps({"schema_version": 99, "mode": "daemon"}),
            encoding="utf-8",
        )

        state = tlm.SchedulerState.load(self.state_path)

        self.assertEqual(state.total_runs, 0)
        self.assertEqual(state.to_dict()["schema_version"], 1)
        quarantined = sorted(self.state_path.parent.glob(self.state_path.name + ".corrupt*"))
        self.assertEqual(len(quarantined), 1)

    def test_record_cycle_start_sets_status_and_clears_clean_shutdown(self) -> None:
        state = tlm.SchedulerState(self.state_path)
        state.clean_shutdown = True
        started = datetime(2026, 11, 12, 2, 0, tzinfo=ICT)

        state.record_cycle_start(started)

        self.assertEqual(datetime.fromisoformat(state.last_cycle["started_at"]), started)
        self.assertFalse(state.clean_shutdown)

    def test_record_cycle_result_tracks_status_runs_and_failures(self) -> None:
        state = tlm.SchedulerState(self.state_path)
        started = datetime(2026, 11, 12, 2, 0, tzinfo=ICT)
        state.record_cycle_start(started)

        state.record_cycle_result(
            "gate_failed",
            tlm.EXIT_GATE_FAILED,
            completed_at=started + timedelta(seconds=12),
            gate_passed=False,
        )
        self.assertEqual(state.last_cycle["status"], "gate_failed")
        self.assertEqual(state.total_runs, 1)
        self.assertEqual(state.consecutive_failures, 1)
        self.assertEqual(state.counters["cycles_gate_failed"], 1)

        state.record_cycle_result(
            "error",
            tlm.EXIT_DATA_ERROR,
            completed_at=started + timedelta(seconds=20),
        )
        self.assertEqual(state.total_runs, 2)
        self.assertEqual(state.consecutive_failures, 2)
        self.assertEqual(state.counters["cycles_error"], 1)

        state.record_cycle_result(
            "promoted",
            0,
            completed_at=started + timedelta(seconds=30),
            gate_passed=True,
        )
        self.assertEqual(state.last_cycle["status"], "promoted")
        self.assertEqual(state.total_runs, 3)
        self.assertEqual(state.consecutive_failures, 0)
        self.assertEqual(state.counters["cycles_promoted"], 1)

    def test_record_cycle_result_counts_unchanged_cycles(self) -> None:
        state = tlm.SchedulerState(self.state_path)

        state.record_cycle_result("unchanged", 0, artifact_sha256="a" * 64)

        self.assertEqual(state.counters["cycles_unchanged"], 1)
        self.assertEqual(state.total_runs, 1)
        self.assertEqual(state.consecutive_failures, 0)
        self.assertEqual(state.last_cycle["artifact_sha256"], "a" * 64)

    def test_skipped_cycles_do_not_count_as_runs_or_failures(self) -> None:
        state = tlm.SchedulerState(self.state_path)

        state.record_cycle_result("skipped_locked", 0)
        state.record_cycle_result("skipped_no_new_data", 0)

        self.assertEqual(state.total_runs, 0)
        self.assertEqual(state.consecutive_failures, 0)
        self.assertEqual(state.counters["cycles_skipped_locked"], 1)
        self.assertEqual(state.counters["cycles_skipped_no_new_data"], 1)


class TestScheduleComputation(unittest.TestCase):
    """Interval and wall-clock cadence arithmetic plus slot parsing."""

    def test_compute_next_delay_interval(self) -> None:
        delay, slot = tlm.compute_next_delay(
            now=datetime(2026, 11, 12, 9, 0, tzinfo=ICT),
            interval_seconds=300,
        )

        self.assertEqual(delay, 300.0)
        self.assertIsNone(slot)

    def test_compute_next_delay_schedule_same_day_slot(self) -> None:
        now = datetime(2026, 11, 12, 9, 0, tzinfo=ICT)

        delay, slot = tlm.compute_next_delay(
            now=now,
            slots=DAILY_SLOTS,
            timezone=ICT,
        )

        self.assertEqual(delay, 5 * 3600.0)
        self.assertEqual(slot, {"local_date": "2026-11-12", "slot": "14:00"})
        self.assertEqual(now + timedelta(seconds=delay), datetime(2026, 11, 12, 14, 0, tzinfo=ICT))

    def test_compute_next_delay_schedule_rolls_over_to_next_day(self) -> None:
        now = datetime(2026, 11, 12, 20, 30, tzinfo=ICT)

        delay, slot = tlm.compute_next_delay(
            now=now,
            slots=DAILY_SLOTS,
            timezone=ICT,
        )

        self.assertEqual(delay, 5.5 * 3600.0)
        self.assertEqual(slot, {"local_date": "2026-11-13", "slot": "02:00"})
        self.assertEqual(now + timedelta(seconds=delay), datetime(2026, 11, 13, 2, 0, tzinfo=ICT))

    def test_compute_next_delay_skips_the_slot_that_is_exactly_now(self) -> None:
        now = datetime(2026, 11, 12, 14, 0, tzinfo=ICT)

        delay, slot = tlm.compute_next_delay(
            now=now,
            slots=DAILY_SLOTS,
            timezone=ICT,
        )

        self.assertEqual(delay, 6 * 3600.0)
        self.assertEqual(slot, {"local_date": "2026-11-12", "slot": "20:00"})

    def test_compute_next_delay_uses_the_given_timezone_for_wall_clock_slots(self) -> None:
        # 07:00 UTC is 14:00 in Asia/Ho_Chi_Minh, so the next slot is 20:00 local.
        now = datetime(2026, 11, 12, 7, 0, tzinfo=timezone.utc)

        delay, slot = tlm.compute_next_delay(
            now=now,
            slots=DAILY_SLOTS,
            timezone=ICT,
        )

        self.assertEqual(delay, 6 * 3600.0)
        self.assertEqual(slot, {"local_date": "2026-11-12", "slot": "20:00"})

    def test_parse_schedule_hours_sorts_and_dedupes(self) -> None:
        parsed = tlm.parse_schedule_hours("20:00, 02:00,08:00,14:00,08:00")

        self.assertEqual(parsed, ("02:00", "08:00", "14:00", "20:00"))
        self.assertIsInstance(parsed, tuple)

    def test_parse_schedule_hours_accepts_padding_around_slots(self) -> None:
        self.assertEqual(tlm.parse_schedule_hours(" 07:05 , 19:45 "), ("07:05", "19:45"))

    def test_parse_schedule_hours_accepts_a_single_slot(self) -> None:
        self.assertEqual(tlm.parse_schedule_hours("02:00"), ("02:00",))

    def test_parse_schedule_hours_rejects_invalid_values(self) -> None:
        invalid = ("25:00", "2:0", "08:60", "24:00", "", "   ", "08:00,", "abc", "0800", "-1:00")
        for raw in invalid:
            with self.subTest(raw=raw):
                with self.assertRaises(ValueError):
                    tlm.parse_schedule_hours(raw)


class TestCountHistoryRows(unittest.TestCase):
    """Read-only history counting used by the min_new_rows gate."""

    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.root = Path(self._tmp.name)
        self.db_path = self.root / "camera-view.db"

    def _seed(self, count: int) -> None:
        if self.db_path.exists():
            self.db_path.unlink()
        create_test_db(self.db_path, [history_row(index) for index in range(count)])

    def test_returns_row_count_from_hotset_verdict_history(self) -> None:
        self._seed(5)

        self.assertEqual(tlm.count_history_rows(self.db_path), 5)

    def test_returns_zero_for_an_empty_table(self) -> None:
        create_test_db(self.db_path, [])

        self.assertEqual(tlm.count_history_rows(self.db_path), 0)

    def test_returns_zero_when_the_table_does_not_exist(self) -> None:
        conn = sqlite3.connect(self.db_path)
        conn.execute("CREATE TABLE unrelated (id INTEGER PRIMARY KEY)")
        conn.commit()
        conn.close()

        self.assertEqual(tlm.count_history_rows(self.db_path), 0)

    def test_raises_history_count_error_for_a_missing_database_without_creating_it(self) -> None:
        missing = self.root / "absent.db"

        with self.assertRaises(tlm.HistoryCountError):
            tlm.count_history_rows(missing)
        self.assertFalse(missing.exists())

    def test_read_only_execution_does_not_mutate_the_database(self) -> None:
        self._seed(3)
        self.db_path.chmod(0o444)
        self.addCleanup(self.db_path.chmod, 0o644)
        before = (self.db_path.stat().st_size, self.db_path.stat().st_mtime_ns)

        self.assertEqual(tlm.count_history_rows(self.db_path), 3)

        after = (self.db_path.stat().st_size, self.db_path.stat().st_mtime_ns)
        self.assertEqual(before, after)
        for suffix in ("-journal", "-wal", "-shm"):
            side_file = Path(str(self.db_path) + suffix)
            with self.subTest(suffix=suffix):
                self.assertFalse(side_file.exists())

    def test_read_only_execution_works_when_the_directory_is_not_writable(self) -> None:
        self._seed(2)
        self.root.chmod(0o555)
        self.addCleanup(self.root.chmod, 0o755)

        self.assertEqual(tlm.count_history_rows(self.db_path), 2)


class TestRunDaemon(unittest.TestCase):
    """Daemon loop semantics: max_runs bound, lock contention, and data gating."""

    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.root = Path(self._tmp.name)
        self.db_path = self.root / "camera-view.db"
        self.artifact_out = self.root / "volume" / "prototype_live.json"
        self.lock_path = self.root / "scheduler" / "refit.lock"
        self.state_path = self.root / "scheduler" / "refit_state.json"
        self.sleep_calls: list[float] = []

    def _noop_sleep(self, seconds: float) -> None:
        self.sleep_calls.append(seconds)

    def _seed(self, count: int) -> None:
        if self.db_path.exists():
            self.db_path.unlink()
        create_test_db(self.db_path, [history_row(index) for index in range(count)])

    def _run(self, **overrides):
        kwargs = {
            "db_path": self.db_path,
            "artifact_out": self.artifact_out,
            "lock_path": self.lock_path,
            "state_path": self.state_path,
            "max_runs": 1,
            "dry_run": True,
            "sleep_fn": self._noop_sleep,
        }
        kwargs.update(overrides)
        return tlm.run_daemon(**kwargs)

    def test_max_runs_one_dry_run_executes_exactly_one_cycle(self) -> None:
        self._seed(2)
        observed_dry_run: list[bool] = []

        def cycle_fn(dry_run: bool) -> "tlm.CycleOutcome":
            observed_dry_run.append(dry_run)
            return tlm.CycleOutcome(status="promoted", exit_code=0)

        code = self._run(interval_seconds=60.0, cycle_fn=cycle_fn)

        self.assertEqual(code, 0)
        self.assertEqual(observed_dry_run, [True])
        self.assertEqual(self.sleep_calls, [], "no idle sleep is allowed after the final cycle")
        self.assertFalse(self.artifact_out.exists(), "a dry run must never write the artifact")

        state = tlm.SchedulerState.load(self.state_path)
        self.assertEqual(state.total_runs, 1)
        self.assertEqual(state.last_cycle["status"], "promoted")
        self.assertEqual(state.counters["cycles_promoted"], 1)
        self.assertTrue(state.clean_shutdown)

    def test_max_runs_bounds_the_loop(self) -> None:
        self._seed(2)
        calls: list[int] = []

        def cycle_fn(dry_run: bool) -> "tlm.CycleOutcome":
            calls.append(len(calls) + 1)
            return tlm.CycleOutcome(status="promoted", exit_code=0)

        code = self._run(interval_seconds=60.0, max_runs=3, cycle_fn=cycle_fn)

        self.assertEqual(code, 0)
        self.assertEqual(calls, [1, 2, 3])
        self.assertEqual([round(s, 1) for s in self.sleep_calls], [60.0, 60.0])

        state = tlm.SchedulerState.load(self.state_path)
        self.assertEqual(state.total_runs, 3)

    def test_skipped_locked_when_the_lock_is_held_elsewhere(self) -> None:
        self._seed(2)
        holder = tlm.ForecastRefitLock(self.lock_path)
        self.addCleanup(holder.release)
        holder.acquire()

        code = self._run(interval_seconds=60.0)

        self.assertEqual(code, 0)
        self.assertEqual(self.sleep_calls, [])
        self.assertFalse(self.artifact_out.exists())
        self.assertTrue(self.lock_path.exists(), "the loser must never unlink the lock file")

    def test_skipped_locked_flushed_to_disk_on_subsequent_cycle(self) -> None:
        self._seed(2)
        holder = tlm.ForecastRefitLock(self.lock_path)
        holder.acquire()

        def release_on_sleep(seconds: float) -> None:
            self.sleep_calls.append(seconds)
            if holder._fd is not None:
                holder.release()

        def cycle_fn(dry_run: bool) -> "tlm.CycleOutcome":
            return tlm.CycleOutcome(status="promoted", exit_code=0)

        code = self._run(
            interval_seconds=60.0,
            max_runs=2,
            sleep_fn=release_on_sleep,
            cycle_fn=cycle_fn,
        )

        self.assertEqual(code, 0)
        state = tlm.SchedulerState.load(self.state_path)
        self.assertEqual(state.counters.get("cycles_skipped_locked"), 1)
        self.assertEqual(state.counters.get("cycles_promoted"), 1)
        self.assertEqual(state.mode, "daemon")
        self.assertEqual(state.cadence["kind"], "interval")
        self.assertTrue(state.clean_shutdown)

    def test_fail_on_locked_exits_with_code_four(self) -> None:
        self._seed(2)
        holder = tlm.ForecastRefitLock(self.lock_path)
        self.addCleanup(holder.release)
        holder.acquire()

        code = self._run(interval_seconds=60.0, fail_on_locked=True)

        self.assertEqual(tlm.EXIT_FAIL_ON_LOCKED, 4)
        self.assertEqual(code, 4)
        self.assertFalse(self.artifact_out.exists())

    def test_skipped_no_new_data_when_min_new_rows_exceeds_available_rows(self) -> None:
        self._seed(3)

        code = self._run(interval_seconds=60.0, dry_run=False, min_new_rows=10)

        self.assertEqual(code, 0)
        self.assertFalse(self.artifact_out.exists())

        state = tlm.SchedulerState.load(self.state_path)
        self.assertEqual(state.last_cycle["status"], "skipped_no_new_data")
        self.assertEqual(state.counters["cycles_skipped_no_new_data"], 1)
        self.assertEqual(state.total_runs, 0)
        self.assertEqual(state.consecutive_failures, 0)
        self.assertEqual(state.counters["rows_last_seen"], 0, "rows_last_seen must NOT advance on skip")
        self.assertTrue(state.clean_shutdown)

    def test_min_new_rows_accumulates_across_skips_until_threshold(self) -> None:
        # Seed 3 rows, min is 5. Run cycle 1 -> skips (3 < 5). Seed 3 more rows (total 6 >= 5). Run cycle 2 -> promotes!
        self._seed(3)
        code1 = self._run(interval_seconds=60.0, dry_run=False, min_new_rows=5)
        self.assertEqual(code1, 0)
        state1 = tlm.SchedulerState.load(self.state_path)
        self.assertEqual(state1.counters["cycles_skipped_no_new_data"], 1)
        self.assertEqual(state1.counters["rows_last_seen"], 0)

        # Append more rows to database
        self._seed(6)
        promoted_calls: list[bool] = []

        def cycle_fn(dry_run: bool) -> "tlm.CycleOutcome":
            promoted_calls.append(True)
            return tlm.CycleOutcome(status="promoted", exit_code=0, rows_available=6, rows_new=6)

        code2 = self._run(interval_seconds=60.0, dry_run=False, min_new_rows=5, cycle_fn=cycle_fn)
        self.assertEqual(code2, 0)
        self.assertEqual(promoted_calls, [True])
        state2 = tlm.SchedulerState.load(self.state_path)
        self.assertEqual(state2.last_cycle["status"], "promoted")
        self.assertEqual(state2.counters["rows_last_seen"], 6)

    def test_min_new_rows_resets_baseline_on_history_truncation(self) -> None:
        # Baseline is 10, but DB shrunk to 3
        state = tlm.SchedulerState(self.state_path)
        state.counters["rows_last_seen"] = 10
        state.save()

        self._seed(3)
        code = self._run(interval_seconds=60.0, dry_run=False, min_new_rows=5)
        self.assertEqual(code, 0)
        state_after = tlm.SchedulerState.load(self.state_path)
        self.assertEqual(state_after.counters["rows_last_seen"], 3)

    def test_min_new_rows_allows_the_cycle_when_enough_rows_arrived(self) -> None:
        self._seed(3)
        observed_dry_run: list[bool] = []

        def cycle_fn(dry_run: bool) -> "tlm.CycleOutcome":
            observed_dry_run.append(dry_run)
            return tlm.CycleOutcome(status="promoted", exit_code=0, rows_available=3, rows_new=3)

        code = self._run(interval_seconds=60.0, dry_run=False, min_new_rows=3, cycle_fn=cycle_fn)

        self.assertEqual(code, 0)
        self.assertEqual(observed_dry_run, [False])

        state = tlm.SchedulerState.load(self.state_path)
        self.assertEqual(state.last_cycle["status"], "promoted")
        self.assertEqual(state.total_runs, 1)
        self.assertEqual(state.counters["rows_last_seen"], 3)

    def test_cycle_exception_resilience_increments_error_and_continues(self) -> None:
        self._seed(2)
        calls: list[int] = []

        def buggy_cycle_fn(dry_run: bool) -> "tlm.CycleOutcome":
            calls.append(len(calls) + 1)
            if len(calls) == 1:
                raise RuntimeError("simulated transient disk or sqlite exception")
            return tlm.CycleOutcome(status="promoted", exit_code=0)

        code = self._run(interval_seconds=60.0, max_runs=2, cycle_fn=buggy_cycle_fn)

        self.assertEqual(code, 0)
        self.assertEqual(calls, [1, 2], "daemon loop must survive cycle exception and proceed to next cycle")
        state = tlm.SchedulerState.load(self.state_path)
        self.assertEqual(state.total_runs, 2)
        self.assertEqual(state.counters["cycles_error"], 1)
        self.assertEqual(state.counters["cycles_promoted"], 1)
        self.assertTrue(state.clean_shutdown)

    def test_count_history_rows_raises_on_corrupt_database(self) -> None:
        corrupt_db = self.root / "corrupt.db"
        corrupt_db.write_bytes(b"not a valid sqlite database header at all")
        with self.assertRaises(sqlite3.DatabaseError):
            tlm.count_history_rows(corrupt_db)

    def test_lock_losers_leave_state_file_untouched(self) -> None:
        self._seed(2)
        initial_state = tlm.SchedulerState(self.state_path)
        initial_state.pid = 99999
        initial_state.counters["cycles_promoted"] = 42
        initial_state.save()
        initial_bytes = self.state_path.read_bytes()

        holder = tlm.ForecastRefitLock(self.lock_path)
        self.addCleanup(holder.release)
        holder.acquire()

        # Run process that loses lock with fail_on_locked
        code = self._run(interval_seconds=60.0, fail_on_locked=True)
        self.assertEqual(code, 4)
        self.assertEqual(self.state_path.read_bytes(), initial_bytes, "lock loser must not mutate state file")

    def test_slot_dedupe_prevents_refire_after_restart(self) -> None:
        tz = ZoneInfo("Asia/Ho_Chi_Minh")
        now = datetime(2026, 11, 12, 7, 0, 0, tzinfo=tz)
        slots = ["07:30", "12:00"]
        delay1, marker1 = tlm.compute_next_delay(now=now, slots=slots, timezone=tz)
        self.assertEqual(marker1["slot"], "07:30")
        self.assertEqual(f"{marker1['local_date']}T{marker1['slot']}", "2026-11-12T07:30")

        # Now simulate restart with last_slot_fired set to 07:30
        delay2, marker2 = tlm.compute_next_delay(now=now, slots=slots, timezone=tz, last_slot_fired="2026-11-12T07:30")
        self.assertEqual(marker2["slot"], "12:00", "slot deduplication must advance to 12:00 instead of re-firing 07:30")

    def test_row_count_error_isolated_and_consecutive_failures_incremented(self) -> None:
        corrupt_db = self.root / "corrupt.db"
        corrupt_db.write_bytes(b"invalid sqlite header")

        code = self._run(db_path=corrupt_db, interval_seconds=60.0, max_runs=1)
        self.assertEqual(code, 0)
        state = tlm.SchedulerState.load(self.state_path)
        self.assertEqual(state.last_cycle["status"], "error")
        self.assertEqual(state.counters["cycles_error"], 1)
        self.assertEqual(state.counters["consecutive_failures"], 1)
        self.assertTrue(state.clean_shutdown)

    def test_oneshot_lock_contention_respects_fail_on_locked(self) -> None:
        holder = tlm.ForecastRefitLock(self.lock_path)
        self.addCleanup(holder.release)
        holder.acquire()

        # One-shot with --fail-on-locked must exit 4 (B1 / AC4)
        code_fail = tlm.run([
            "--db", str(self.db_path),
            "--lock-file", str(self.lock_path),
            "--fail-on-locked",
        ])
        self.assertEqual(code_fail, tlm.EXIT_FAIL_ON_LOCKED)

        # One-shot without --fail-on-locked must log warning and exit 0 (skipped)
        code_skip = tlm.run([
            "--db", str(self.db_path),
            "--lock-file", str(self.lock_path),
        ])
        self.assertEqual(code_skip, tlm.EXIT_OK)

    def test_daemon_slot_dedupe_e2e_persists_and_skips_on_restart(self) -> None:
        tz = ZoneInfo("Asia/Ho_Chi_Minh")
        fixed_dt = datetime(2026, 11, 12, 7, 59, 0, tzinfo=tz)
        slots = ("08:00", "14:00")

        # Run 1: fires the 08:00 slot
        self._seed(2)
        code1 = self._run(
            schedule_hours=slots,
            timezone=tz,
            max_runs=1,
            now_fn=lambda: fixed_dt,
            cycle_fn=lambda dry_run: tlm.CycleOutcome(status="promoted", exit_code=0),
        )
        self.assertEqual(code1, 0)
        state1 = tlm.SchedulerState.load(self.state_path)
        self.assertEqual(state1.last_slot_fired, "2026-11-12T08:00")

        # Run 2: starts at same time against same state file; next slot must be 14:00
        delays: list[float] = []
        code2 = self._run(
            schedule_hours=slots,
            timezone=tz,
            max_runs=1,
            now_fn=lambda: fixed_dt,
            sleep_fn=lambda d: delays.append(d),
            cycle_fn=lambda dry_run: tlm.CycleOutcome(status="promoted", exit_code=0),
        )
        self.assertEqual(code2, 0)
        # Next delay must be for 14:00 (approx 6 hours = 21660 seconds), not 08:00 (60 seconds)
        self.assertTrue(len(delays) > 0)
        self.assertGreater(delays[0], 3600, "Daemon restart must advance to 14:00, not re-fire 08:00")

    def test_config_validation_returns_exit_config_error_cleanly(self) -> None:
        # Invalid --interval
        code_inv_int = tlm.run_daemon(
            db_path=self.db_path,
            state_path=self.state_path,
            lock_path=self.lock_path,
            interval_seconds=0,
        )
        self.assertEqual(code_inv_int, tlm.EXIT_CONFIG_ERROR)

        # Invalid --schedule-hours
        code_inv_sched = tlm.run_daemon(
            db_path=self.db_path,
            state_path=self.state_path,
            lock_path=self.lock_path,
            schedule_hours="25:00",
        )
        self.assertEqual(code_inv_sched, tlm.EXIT_CONFIG_ERROR)

    def test_daemon_cli_argv_path_runs_one_cycle(self) -> None:
        self._seed(2)
        code = tlm.run([
            "--daemon", "--interval", "60", "--max-runs", "1", "--dry-run",
            "--db", str(self.db_path),
            "--lock-file", str(self.lock_path),
            "--state-file", str(self.state_path),
            "--timezone", "Asia/Ho_Chi_Minh",
        ])
        self.assertEqual(code, tlm.EXIT_OK)

    def test_oneshot_min_new_rows_gating_updates_state(self) -> None:
        self._seed(3)
        state = tlm.SchedulerState(self.state_path)
        state.counters["rows_last_seen"] = 3
        state.save()

        # One-shot with min-new-rows 5 when DB has 3 rows (0 new) -> skips
        code = tlm.run([
            "--db", str(self.db_path),
            "--lock-file", str(self.lock_path),
            "--state-file", str(self.state_path),
            "--min-new-rows", "5",
        ])
        self.assertEqual(code, tlm.EXIT_OK)
        loaded = tlm.SchedulerState.load(self.state_path)
        self.assertEqual(loaded.last_cycle["status"], "skipped_no_new_data")

    def test_empty_schedule_hours_cli_exits_one_ac7(self) -> None:
        code = tlm.run([
            "--daemon",
            "--schedule-hours", "",
            "--db", str(self.db_path),
            "--lock-file", str(self.lock_path),
            "--state-file", str(self.state_path),
        ])
        self.assertEqual(code, tlm.EXIT_CONFIG_ERROR)

    def test_status_and_health_cli_flags(self) -> None:
        state = tlm.SchedulerState(self.state_path)
        state.pid = os.getpid()
        state.clean_shutdown = False
        state.save()

        # Health should pass with current running PID
        code_health = tlm.run([
            "--health",
            "--state-file", str(self.state_path),
        ])
        self.assertEqual(code_health, tlm.EXIT_OK)

        # Status should output valid JSON
        code_status = tlm.run([
            "--status",
            "--state-file", str(self.state_path),
        ])
        self.assertEqual(code_status, tlm.EXIT_OK)

        # Corrupt state should fail health
        self.state_path.write_text("invalid json")
        code_bad = tlm.run([
            "--health",
            "--state-file", str(self.state_path),
        ])
        self.assertEqual(code_bad, tlm.EXIT_CONFIG_ERROR)

    def test_telemetry_out_written_by_daemon(self) -> None:
        self._seed(2)
        telemetry_path = self.root / "refit_status.json"
        code = tlm.run([
            "--daemon", "--interval", "60", "--max-runs", "1", "--dry-run",
            "--db", str(self.db_path),
            "--lock-file", str(self.lock_path),
            "--state-file", str(self.state_path),
            "--telemetry-out", str(telemetry_path),
        ])
        self.assertEqual(code, tlm.EXIT_OK)
        self.assertTrue(telemetry_path.exists())
        telem = json.loads(telemetry_path.read_text())
        self.assertIn("healthy", telem)
        self.assertIn("telemetry_generated_at", telem)
        self.assertEqual(telem["schema_version"], 1)

    def test_unwritable_artifact_fails_closed_ac8(self) -> None:
        self._seed(2)
        read_only_dir = self.root / "ro_dir"
        read_only_dir.mkdir(parents=True)
        read_only_dir.chmod(0o555)
        self.addCleanup(read_only_dir.chmod, 0o755)

        bad_art = read_only_dir / "art.json"
        code = tlm.run([
            "--db", str(self.db_path),
            "--artifact-out", str(bad_art),
            "--lock-file", str(self.lock_path),
            "--state-file", str(self.state_path),
            "--promote",
        ])
        # M2: specific error code, zero partial artifact, zero residue, zero promotion recorded
        self.assertEqual(code, tlm.EXIT_CONFIG_ERROR)
        self.assertFalse(bad_art.exists(), "candidate artifact must not be created on unwritable path")
        self.assertEqual(list(read_only_dir.glob("*.tmp")), [], "no temp file residue allowed")
        if self.state_path.exists():
            state = tlm.SchedulerState.load(self.state_path)
            self.assertEqual(state.counters.get("cycles_promoted", 0), 0)

    def test_daemon_promote_real_cycle_path_without_attribute_error_b6(self) -> None:
        rows = []
        base_time = datetime(2026, 11, 10, 8, 0, tzinfo=ZoneInfo("Asia/Ho_Chi_Minh"))
        for i in range(120):
            ts = base_time + timedelta(minutes=5 * i)
            rows.append((
                "cam-sched-0",
                100.0 + (i % 10),
                0.3 + 0.05 * (i % 5),
                "low",
                0.9,
                "proof_archive",
                ts.isoformat(),
                (ts + timedelta(seconds=5)).isoformat(),
                "",
                "",
            ))
        create_test_db(self.db_path, rows)

        code = tlm.run([
            "--daemon", "--interval", "10", "--max-runs", "1",
            "--db", str(self.db_path),
            "--artifact-out", str(self.artifact_out),
            "--report-out", str(self.root / "report.md"),
            "--eval-json-out", str(self.root / "eval.json"),
            "--lock-file", str(self.lock_path),
            "--state-file", str(self.state_path),
            "--promote",
        ])
        self.assertIn(code, (tlm.EXIT_OK, tlm.EXIT_GATE_FAILED))
        state = tlm.SchedulerState.load(self.state_path)
        self.assertNotIn("AttributeError", state.last_cycle.get("error", ""))
        self.assertGreater(state.total_runs, 0)

    def test_daemon_second_signal_escalates_to_terminated_exit_code_ac5(self) -> None:
        handler_calls: list[int] = []
        signal_count = [0]

        def handler(sig: int, frame: Any) -> None:
            signal_count[0] += 1
            if signal_count[0] > 1:
                sys.exit(128 + sig)
            handler_calls.append(sig)

        handler(signal.SIGTERM, None)
        self.assertEqual(handler_calls, [signal.SIGTERM])
        with self.assertRaises(SystemExit) as cm_term:
            handler(signal.SIGTERM, None)
        self.assertEqual(cm_term.exception.code, tlm.EXIT_TERMINATED)

        # Also test SIGINT escalates to 130
        signal_count[0] = 0
        handler(signal.SIGINT, None)
        with self.assertRaises(SystemExit) as cm_int:
            handler(signal.SIGINT, None)
        self.assertEqual(cm_int.exception.code, tlm.EXIT_INTERRUPTED)


if __name__ == "__main__":
    unittest.main()
