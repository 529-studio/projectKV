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
import sqlite3
import subprocess
import tempfile
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


if __name__ == "__main__":
    unittest.main()
