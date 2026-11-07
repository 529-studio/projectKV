#!/usr/bin/env python3
"""
extract_features_test.py - Unit and integration tests for the forecast feature store.

Sprint 9 / Issue #302 (S9-T6) Part 1. Covers timestamp parsing (including the
Go time.Time.String() encoding that modernc.org/sqlite persists), label note
extraction, regular-bin aggregation, lag/rolling features, SQLite read-only
loading, and the end-to-end CSV/metadata contract.
"""

from __future__ import annotations

import csv
import json
import sqlite3
import tempfile
import unittest
from datetime import datetime, timedelta, timezone
from pathlib import Path
from zoneinfo import ZoneInfo

from tools.forecast.extract_features import (
    Observation,
    WeatherObservation,
    asof_weather_features,
    build_feature_rows,
    classify_weather_code,
    deduplicate_observations,
    load_label_observations,
    load_sqlite_observations,
    load_sqlite_weather_observations,
    parse_label_notes,
    parse_label_record,
    parse_timestamp,
    run,
)

REPO_ROOT = Path(__file__).resolve().parent.parent.parent
MIGRATION = (
    REPO_ROOT
    / "services"
    / "go-server"
    / "internal"
    / "store"
    / "migrations"
    / "0007_hotset_verdict_history.sql"
)
ICT = ZoneInfo("Asia/Ho_Chi_Minh")
UTC = timezone.utc


def obs(
    camera_id: str,
    hour: int,
    minute: int,
    si: float,
    severity: str = "CLEAR",
    pcu: float = 10.0,
    source: str = "onnx",
    origin: str = "sqlite",
    day: int = 5,
) -> Observation:
    return Observation(
        camera_id=camera_id,
        observed_at=datetime(2026, 11, day, hour, minute, tzinfo=UTC),
        pcu_total=pcu,
        saturation_index=si,
        severity=severity,
        confidence=0.9,
        source=source,
        origin=origin,
    )


class TestTimestampParsing(unittest.TestCase):
    def test_go_monotonic_timestamp(self):
        parsed = parse_timestamp(
            "2026-10-11 17:35:13.525256 +0700 +07 m=+0.944197834", ICT
        )
        self.assertEqual(
            parsed,
            datetime(2026, 10, 11, 10, 35, 13, 525256, tzinfo=UTC),
        )

    def test_rfc3339_timestamp(self):
        parsed = parse_timestamp("2026-11-05T08:00:00Z", ICT)
        self.assertEqual(parsed, datetime(2026, 11, 5, 8, 0, tzinfo=UTC))

    def test_naive_timestamp_uses_local_timezone(self):
        parsed = parse_timestamp("2026-11-05 08:00:00", ICT)
        self.assertEqual(parsed, datetime(2026, 11, 5, 1, 0, tzinfo=UTC))

    def test_epoch_seconds(self):
        parsed = parse_timestamp(1792729787, ICT)
        self.assertIsNotNone(parsed)
        self.assertEqual(parsed.tzinfo, UTC)

    def test_numeric_epoch_string(self):
        parsed = parse_timestamp("1792729787", ICT)
        self.assertEqual(parsed, parse_timestamp(1792729787, ICT))

    def test_invalid_timestamp_returns_none(self):
        self.assertIsNone(parse_timestamp("not-a-timestamp", ICT))


class TestLabelParsing(unittest.TestCase):
    def test_notes_lowercase_and_uppercase(self):
        self.assertEqual(
            parse_label_notes("Auto-harvested; pcu=12.3; SI=0.2733"),
            (12.3, 0.2733),
        )
        self.assertEqual(
            parse_label_notes("Rain frame. PCU=10.5, SI=0.70, base_cap=15.0"),
            (10.5, 0.70),
        )

    def test_notes_without_values(self):
        self.assertEqual(parse_label_notes("manual label only"), (None, None))

    def test_label_record_uses_direct_values_first(self):
        record = {
            "camera_id": "cam-1",
            "annotated_at": "2026-11-05T08:00:00+07:00",
            "labels": {"traffic": "HEAVY"},
            "pcu_total": 12.0,
            "saturation_index": 0.61,
            "notes": "pcu=99.0; SI=0.99",
        }
        parsed = parse_label_record(record, ICT)
        self.assertIsNotNone(parsed)
        self.assertEqual(parsed.camera_id, "cam-1")
        self.assertEqual(parsed.pcu_total, 12.0)
        self.assertEqual(parsed.saturation_index, 0.61)
        self.assertEqual(parsed.severity, "HEAVY")

    def test_label_record_skips_without_saturation_index(self):
        record = {
            "camera_id": "cam-1",
            "annotated_at": "2026-11-05T08:00:00+07:00",
            "notes": "pcu=12.0",
        }
        self.assertIsNone(parse_label_record(record, ICT))


class TestFeatureBuilding(unittest.TestCase):
    def test_lag_and_rolling_features(self):
        observations = [
            obs("cam-1", 10, 0, 0.20),
            obs("cam-1", 10, 5, 0.40),
            obs("cam-1", 10, 10, 0.60),
            obs("cam-1", 10, 15, 0.80),
        ]
        rows, fieldnames, stats = build_feature_rows(observations, 5, ICT)
        self.assertEqual(len(rows), 4)
        self.assertIn("lag_5m", fieldnames)
        self.assertIn("lag_10m", fieldnames)
        self.assertIn("lag_15m", fieldnames)
        self.assertIn("rolling_mean_15m", fieldnames)

        last = rows[-1].values
        self.assertEqual(last["bin_start_utc"], "2026-11-05T10:15:00Z")
        self.assertAlmostEqual(last["saturation_index"], 0.80)
        self.assertAlmostEqual(last["lag_5m"], 0.60)
        self.assertAlmostEqual(last["lag_10m"], 0.40)
        self.assertAlmostEqual(last["lag_15m"], 0.20)
        self.assertAlmostEqual(last["rolling_mean_15m"], 0.40)
        self.assertEqual(last["rolling_count_15m"], 3)
        self.assertEqual(last["hour_of_day"], 17)  # 10:15 UTC is 17:15 ICT
        self.assertEqual(last["day_of_week"], 3)  # Thursday
        self.assertEqual(last["is_weekend"], 0)
        # The first two bins cannot have a full lag history; the assertion is
        # that the final bin resolves every lag from the raw timeline.
        self.assertIsNotNone(last["lag_5m"])
        self.assertIsNotNone(last["lag_10m"])
        self.assertIsNotNone(last["lag_15m"])
        self.assertGreater(stats["lags_missing"], 0)

    def test_aggregates_multiple_observations_in_one_bin(self):
        observations = [
            obs("cam-1", 10, 0, 0.20, severity="MODERATE"),
            obs("cam-1", 10, 1, 0.60, severity="HEAVY"),
        ]
        rows, _, _ = build_feature_rows(observations, 5, ICT)
        self.assertEqual(len(rows), 1)
        values = rows[0].values
        self.assertEqual(values["observation_count"], 2)
        self.assertAlmostEqual(values["saturation_index"], 0.40)
        self.assertEqual(values["severity"], "HEAVY")

    def test_severity_tie_breaks_to_worst_observed_label(self):
        observations = [
            obs("cam-1", 10, 0, 0.20, severity="CLEAR"),
            obs("cam-1", 10, 1, 0.60, severity="CONGESTED"),
        ]
        rows, _, _ = build_feature_rows(observations, 5, ICT)
        self.assertEqual(rows[0].values["severity"], "CONGESTED")

    def test_ten_minute_bins_still_use_raw_lag_timeline(self):
        observations = [
            obs("cam-1", 10, 0, 0.20),
            obs("cam-1", 10, 5, 0.30),
            obs("cam-1", 10, 10, 0.50),
            obs("cam-1", 10, 15, 0.70),
        ]
        rows, _, _ = build_feature_rows(observations, 10, ICT)
        self.assertEqual(len(rows), 2)
        second = rows[1].values
        self.assertEqual(second["bin_start_utc"], "2026-11-05T10:10:00Z")
        self.assertAlmostEqual(second["saturation_index"], 0.60)
        # lags are anchored to the bin start, not to the bin width.
        self.assertAlmostEqual(second["lag_5m"], 0.30)
        self.assertAlmostEqual(second["lag_10m"], 0.20)

    def test_missing_lag_outside_tolerance_is_null(self):
        observations = [
            obs("cam-1", 9, 0, 0.20),
            obs("cam-1", 10, 0, 0.40),
        ]
        rows, _, stats = build_feature_rows(observations, 5, ICT)
        last = rows[-1].values
        self.assertIsNone(last["lag_5m"])
        self.assertGreater(stats["lags_missing"], 0)

    def test_bins_without_saturation_index_are_skipped(self):
        observations = [
            Observation(
                camera_id="cam-1",
                observed_at=datetime(2026, 11, 5, 10, 0, tzinfo=UTC),
                pcu_total=10.0,
                saturation_index=None,
                severity="MODERATE",
                confidence=0.8,
                source="onnx",
                origin="sqlite",
            )
        ]
        rows, _, _ = build_feature_rows(observations, 5, ICT)
        self.assertEqual(rows, [])

    def test_deduplicates_label_against_sqlite(self):
        sqlite_row = obs("cam-1", 10, 0, 0.4, origin="sqlite")
        label_row = obs("cam-1", 10, 0, 0.9, origin="label")
        merged, dropped = deduplicate_observations([sqlite_row], [label_row])
        self.assertEqual(dropped, 1)
        self.assertEqual(len(merged), 1)
        self.assertEqual(merged[0].origin, "sqlite")
        self.assertAlmostEqual(merged[0].saturation_index, 0.4)


class TestSqliteLoading(unittest.TestCase):
    def setUp(self):
        self.tempdir = tempfile.TemporaryDirectory()
        self.addCleanup(self.tempdir.cleanup)
        self.root = Path(self.tempdir.name)

    def _create_db(self, path: Path) -> sqlite3.Connection:
        connection = sqlite3.connect(path)
        connection.executescript(MIGRATION.read_text(encoding="utf-8"))
        return connection

    def test_reads_go_style_and_rfc3339_timestamps(self):
        db_path = self.root / "history.db"
        connection = self._create_db(db_path)
        connection.execute(
            """
            INSERT INTO hotset_verdict_history
                (camera_id, pcu_total, saturation_index, severity, confidence, source, observed_at)
            VALUES (?, ?, ?, ?, ?, ?, ?)
            """,
            (
                "cam-1",
                12.0,
                0.42,
                "MODERATE",
                0.9,
                "onnx",
                "2026-10-11 17:35:13.525256 +0700 +07 m=+0.944197834",
            ),
        )
        connection.execute(
            """
            INSERT INTO hotset_verdict_history
                (camera_id, pcu_total, saturation_index, severity, confidence, source, observed_at)
            VALUES (?, ?, ?, ?, ?, ?, ?)
            """,
            ("cam-2", 20.0, 0.80, "CONGESTED", 0.8, "onnx", "2026-11-05T08:00:00Z"),
        )
        connection.commit()
        connection.close()

        observations, stats = load_sqlite_observations(db_path, ICT)
        self.assertEqual(stats["rows"], 2)
        self.assertEqual(stats["accepted"], 2)
        self.assertEqual(len(observations), 2)

    def test_missing_table_returns_empty_without_writing(self):
        db_path = self.root / "empty.db"
        sqlite3.connect(db_path).close()
        observations, stats = load_sqlite_observations(db_path, ICT)
        self.assertEqual(observations, [])
        self.assertEqual(stats["rows"], 0)

    def test_loads_label_json_tree(self):
        labels_root = self.root / "labels"
        labels_root.mkdir()
        (labels_root / "label.json").write_text(
            json.dumps(
                {
                    "camera_id": "cam-1",
                    "annotated_at": "2026-11-05T08:00:00+07:00",
                    "labels": {"traffic": "MODERATE"},
                    "notes": "pcu=12.0; SI=0.42",
                }
            ),
            encoding="utf-8",
        )
        observations, stats = load_label_observations([labels_root], ICT)
        self.assertEqual(stats["accepted"], 1)
        self.assertEqual(len(observations), 1)
        self.assertAlmostEqual(observations[0].saturation_index, 0.42)


class TestEndToEnd(unittest.TestCase):
    def setUp(self):
        self.tempdir = tempfile.TemporaryDirectory()
        self.addCleanup(self.tempdir.cleanup)
        self.root = Path(self.tempdir.name)

    def test_run_writes_csv_and_metadata(self):
        db_path = self.root / "history.db"
        connection = sqlite3.connect(db_path)
        connection.executescript(MIGRATION.read_text(encoding="utf-8"))
        for minute, si in ((0, 0.20), (5, 0.40), (10, 0.60)):
            connection.execute(
                """
                INSERT INTO hotset_verdict_history
                    (camera_id, pcu_total, saturation_index, severity, confidence, source, observed_at)
                VALUES (?, ?, ?, ?, ?, ?, ?)
                """,
                (
                    "cam-1",
                    si * 25,
                    si,
                    "CLEAR",
                    0.9,
                    "onnx",
                    f"2026-11-05T10:{minute:02d}:00Z",
                ),
            )
        connection.commit()
        connection.close()

        out_path = self.root / "features.csv"
        metadata_path = self.root / "features.meta.json"
        exit_code = run(
            [
                "--db",
                str(db_path),
                "--no-labels",
                "--bin-minutes",
                "5",
                "--out",
                str(out_path),
                "--metadata-out",
                str(metadata_path),
                "--log-level",
                "ERROR",
            ]
        )
        self.assertEqual(exit_code, 0)
        self.assertTrue(out_path.exists())
        self.assertTrue(metadata_path.exists())

        with out_path.open("r", encoding="utf-8", newline="") as handle:
            rows = list(csv.DictReader(handle))
        self.assertEqual(len(rows), 3)
        self.assertIn("lag_5m", rows[-1])
        self.assertEqual(rows[-1]["camera_id"], "cam-1")
        self.assertAlmostEqual(float(rows[-1]["saturation_index"]), 0.60)

        metadata = json.loads(metadata_path.read_text(encoding="utf-8"))
        self.assertEqual(metadata["schema_version"], 2)
        self.assertEqual(metadata["row_count"], 3)
        self.assertEqual(metadata["camera_count"], 1)

    def test_load_sqlite_observations_directional_compatibility(self):
        """Migration 0008 adds direction; extract_features must filter to direction = '' by default."""
        db_path = self.root / "directional.db"
        conn = sqlite3.connect(db_path)
        tz = ZoneInfo("Asia/Ho_Chi_Minh")
        conn.execute(
            """
            CREATE TABLE hotset_verdict_history (
                id INTEGER PRIMARY KEY,
                camera_id TEXT,
                direction TEXT,
                region_id TEXT,
                pcu_total REAL,
                saturation_index REAL,
                severity TEXT,
                confidence REAL,
                source TEXT,
                observed_at TIMESTAMP
            )
            """
        )
        # Insert 1 frame-wide row and 2 directional rows for same timestamp
        t_str = "2026-11-06T10:00:00.000000000Z"
        conn.execute(
            """INSERT INTO hotset_verdict_history
            (camera_id, direction, region_id, pcu_total, saturation_index, severity, confidence, source, observed_at)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)""",
            ("cam-dir", "", "", 30.0, 0.60, "MODERATE", 0.9, "onnx", t_str),
        )
        conn.execute(
            """INSERT INTO hotset_verdict_history
            (camera_id, direction, region_id, pcu_total, saturation_index, severity, confidence, source, observed_at)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)""",
            ("cam-dir", "a", "cam-dir_a", 25.0, 1.00, "CONGESTED", 0.9, "onnx", t_str),
        )
        conn.execute(
            """INSERT INTO hotset_verdict_history
            (camera_id, direction, region_id, pcu_total, saturation_index, severity, confidence, source, observed_at)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)""",
            ("cam-dir", "b", "cam-dir_b", 5.0, 0.20, "CLEAR", 0.9, "onnx", t_str),
        )
        conn.commit()
        conn.close()

        # By default (include_directional=False), only loads frame-wide row
        obs, stats = load_sqlite_observations(db_path, tz, include_directional=False)
        self.assertEqual(len(obs), 1)
        self.assertEqual(obs[0].saturation_index, 0.60)
        self.assertEqual(stats["rows"], 1)

        # When include_directional=True, loads all 3 rows
        obs_all, stats_all = load_sqlite_observations(db_path, tz, include_directional=True)
        self.assertEqual(len(obs_all), 3)
        self.assertEqual(stats_all["rows"], 3)


class TestWeatherFeatures(unittest.TestCase):
    def test_classify_weather_code(self):
        self.assertEqual(classify_weather_code(0), 0)
        self.assertEqual(classify_weather_code(1), 0)
        self.assertEqual(classify_weather_code(3), 1)
        self.assertEqual(classify_weather_code(45), 1)
        self.assertEqual(classify_weather_code(51), 2)
        self.assertEqual(classify_weather_code(61), 2)
        self.assertEqual(classify_weather_code(63), 3)
        self.assertEqual(classify_weather_code(65), 3)
        self.assertEqual(classify_weather_code(95), 4)

    def test_asof_weather_30min_boundary_and_null_case(self):
        """Verify strict 30-minute cutoff and no forward fill (ADR-012 D3, Lesson 67)."""
        base_time = datetime(2026, 11, 6, 10, 0, 0, tzinfo=UTC)
        weather_obs = [
            WeatherObservation(
                observed_at=base_time,
                precipitation_mm=2.5,
                weather_code=61,
                temperature_c=27.0,
                humidity_pct=85.0,
                radar_active=1,
                radar_frame_age_s=60,
                source="open-meteo+rainviewer",
            )
        ]
        weather_timestamps = [base_time.timestamp()]

        # 1. Target exactly at observation time (0 min age)
        f0 = asof_weather_features(weather_obs, weather_timestamps, base_time)
        self.assertEqual(f0["precip_mm_15m"], 2.5)
        self.assertEqual(f0["rain_flag"], 1)
        self.assertEqual(f0["radar_active"], 1)
        self.assertEqual(f0["weather_code_class"], 2)
        self.assertAlmostEqual(f0["hours_since_rain"], 0.0)

        # 2. Target at 29 minutes past observation (within 30m window)
        t_29m = base_time + datetime.resolution * 0 + timedelta(minutes=29)
        f29 = asof_weather_features(weather_obs, weather_timestamps, t_29m)
        self.assertEqual(f29["precip_mm_15m"], 2.5)
        self.assertEqual(f29["rain_flag"], 1)
        self.assertAlmostEqual(f29["hours_since_rain"], 29.0 / 60.0)

        # 3. Target at exactly 30 minutes (boundary)
        t_30m = base_time + timedelta(minutes=30)
        f30 = asof_weather_features(weather_obs, weather_timestamps, t_30m)
        self.assertEqual(f30["precip_mm_15m"], 2.5)
        self.assertEqual(f30["rain_flag"], 1)

        # 4. Target at 31 minutes (exceeds 30m cutoff) -> all fields must be None (no forward fill!)
        t_31m = base_time + timedelta(minutes=31)
        f31 = asof_weather_features(weather_obs, weather_timestamps, t_31m)
        self.assertIsNone(f31["precip_mm_15m"])
        self.assertIsNone(f31["precip_mm_60m"])
        self.assertIsNone(f31["rain_flag"])
        self.assertIsNone(f31["hours_since_rain"])
        self.assertIsNone(f31["weather_code_class"])
        self.assertIsNone(f31["radar_active"])

        # 5. Target before any observation -> all fields None
        t_before = base_time - timedelta(minutes=5)
        f_before = asof_weather_features(weather_obs, weather_timestamps, t_before)
        self.assertIsNone(f_before["precip_mm_15m"])
        self.assertIsNone(f_before["rain_flag"])


class TestDirectionalAndWeatherIntegration(unittest.TestCase):
    def test_build_feature_rows_directional_pairing(self):
        """Verify peer_si and si_delta_ab pairing for directional series."""
        t1000 = datetime(2026, 11, 6, 10, 0, tzinfo=UTC)
        observations = [
            Observation(
                camera_id="cam-pair",
                observed_at=t1000,
                pcu_total=25.0,
                saturation_index=0.80,
                severity="CONGESTED",
                confidence=0.9,
                source="onnx",
                origin="sqlite",
                direction="a",
            ),
            Observation(
                camera_id="cam-pair",
                observed_at=t1000,
                pcu_total=10.0,
                saturation_index=0.30,
                severity="CLEAR",
                confidence=0.9,
                source="onnx",
                origin="sqlite",
                direction="b",
            ),
            Observation(
                camera_id="cam-single",
                observed_at=t1000,
                pcu_total=15.0,
                saturation_index=0.50,
                severity="MODERATE",
                confidence=0.9,
                source="onnx",
                origin="sqlite",
                direction="",
            ),
        ]

        rows, fieldnames, stats = build_feature_rows(observations, 5, ICT)
        self.assertEqual(len(rows), 3)

        by_dir = {f"{r.values['camera_id']}_{r.values['direction']}": r.values for r in rows}

        # Direction 'a': peer_si is 0.30, si_delta_ab is 0.80 - 0.30 = 0.50
        row_a = by_dir["cam-pair_a"]
        self.assertEqual(row_a["direction"], "a")
        self.assertAlmostEqual(row_a["peer_si"], 0.30)
        self.assertAlmostEqual(row_a["si_delta_ab"], 0.50)

        # Direction 'b': peer_si is 0.80, si_delta_ab is 0.80 - 0.30 = 0.50 (si_a - si_b)
        row_b = by_dir["cam-pair_b"]
        self.assertEqual(row_b["direction"], "b")
        self.assertAlmostEqual(row_b["peer_si"], 0.80)
        self.assertAlmostEqual(row_b["si_delta_ab"], 0.50)

        # Single frame-wide: peer_si and si_delta_ab are None
        row_single = by_dir["cam-single_"]
        self.assertEqual(row_single["direction"], "")
        self.assertIsNone(row_single["peer_si"])
        self.assertIsNone(row_single["si_delta_ab"])


if __name__ == "__main__":
    unittest.main()
