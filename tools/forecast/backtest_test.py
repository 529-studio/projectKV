#!/usr/bin/env python3
"""
backtest_test.py - Unit and integration tests for the forecast backtest harness.

Sprint 9 / Issue #302 (S9-T6) Part 2. Covers CSV validation, calendar
derivation, duplicate rejection, chronological splitting, exact future-target
alignment, boundary leakage purging, ridge regression, historical median
fallbacks, metrics, the end-to-end JSON/Markdown contract, and the verification
gate (including the honest failure path).
"""

from __future__ import annotations

import csv
import json
import statistics
import tempfile
import unittest
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Callable
from zoneinfo import ZoneInfo

from tools.forecast.backtest import (
    EXIT_CONFIG_ERROR,
    EXIT_DATA_ERROR,
    EXIT_GATE_FAILED,
    EXIT_OK,
    BacktestConfig,
    BacktestError,
    ChronologicalSplit,
    EvaluationSample,
    FeatureRecord,
    HistoricalMedianModel,
    RidgeRegression,
    build_evaluation_samples,
    build_saturation_index,
    build_verification,
    chronological_split,
    compute_metrics,
    direction,
    evaluate_horizon,
    load_feature_records,
    render_markdown,
    run,
    run_backtest,
    solve_linear_system,
)

UTC = timezone.utc
ICT = ZoneInfo("Asia/Ho_Chi_Minh")
START = datetime(2026, 11, 1, 0, 0, tzinfo=UTC)
REPO_ROOT = Path(__file__).resolve().parent.parent.parent
FIXTURE = REPO_ROOT / "tools" / "forecast" / "fixtures" / "synthetic_congestion_5m.csv"
FEATURE_COLUMNS = (
    "lag_5m",
    "lag_10m",
    "lag_15m",
    "rolling_mean_15m",
    "hour_of_day",
    "day_of_week",
)
CSV_COLUMNS = (
    "camera_id",
    "bin_start_utc",
    "bin_minutes",
    "saturation_index",
    *FEATURE_COLUMNS,
)


def make_record(
    camera: str = "cam-1",
    minute: int = 0,
    si: float = 0.5,
    *,
    complete: bool = True,
    hour: int | None = None,
    day: int | None = None,
) -> FeatureRecord:
    """Build one in-memory feature record anchored at START + minute."""
    timestamp = START + timedelta(minutes=minute)
    local = timestamp.astimezone(ICT)
    resolved_hour = local.hour if hour is None else hour
    resolved_day = local.weekday() if day is None else day
    values: tuple[float | None, ...] = (
        si - 0.10,
        si - 0.20,
        si - 0.30,
        si - 0.15,
        float(resolved_hour),
        float(resolved_day),
    )
    if not complete:
        values = (None,) + values[1:]
    return FeatureRecord(
        camera_id=camera,
        bin_start=timestamp,
        saturation_index=si,
        hour_of_day=resolved_hour,
        day_of_week=resolved_day,
        feature_values=values,
    )


def sample_at(
    camera: str,
    current_local: datetime,
    target_local: datetime,
    current_si: float,
    target_si: float,
) -> EvaluationSample:
    """Build an aligned sample with explicit local current and target times."""
    current_utc = current_local.astimezone(UTC)
    record = FeatureRecord(
        camera_id=camera,
        bin_start=current_utc,
        saturation_index=current_si,
        hour_of_day=current_local.hour,
        day_of_week=current_local.weekday(),
        feature_values=(
            current_si - 0.10,
            current_si - 0.20,
            current_si - 0.30,
            current_si - 0.15,
            float(current_local.hour),
            float(current_local.weekday()),
        ),
    )
    return EvaluationSample(
        record=record,
        current=current_si,
        target=target_si,
        target_time=target_local.astimezone(UTC),
    )


def ramp_rows(
    camera: str = "cam-1",
    count: int = 1152,
    si_at: Callable[[int], float] = lambda index: 0.10 + 0.0005 * index,
) -> list[dict[str, str]]:
    """Build a deterministic 5-minute linear ramp with complete lag features."""
    values = [si_at(index) for index in range(count)]
    rows: list[dict[str, str]] = []
    for index, si in enumerate(values):
        timestamp = START + timedelta(minutes=5 * index)
        local = timestamp.astimezone(ICT)
        previous = values[max(0, index - 3) : index]
        rows.append(
            {
                "camera_id": camera,
                "bin_start_utc": timestamp.strftime("%Y-%m-%dT%H:%M:%SZ"),
                "bin_minutes": "5",
                "saturation_index": f"{si:.6f}",
                "lag_5m": f"{values[index - 1]:.6f}" if index >= 1 else "",
                "lag_10m": f"{values[index - 2]:.6f}" if index >= 2 else "",
                "lag_15m": f"{values[index - 3]:.6f}" if index >= 3 else "",
                "rolling_mean_15m": (
                    f"{statistics.fmean(previous):.6f}" if previous else ""
                ),
                "hour_of_day": str(local.hour),
                "day_of_week": str(local.weekday()),
            }
        )
    return rows


def write_csv(path: Path, rows: list[dict[str, str]], columns=CSV_COLUMNS) -> None:
    with path.open("w", encoding="utf-8", newline="") as handle:
        writer = csv.DictWriter(handle, fieldnames=list(columns))
        writer.writeheader()
        for row in rows:
            writer.writerow({column: row.get(column, "") for column in columns})


class TestLinearSolver(unittest.TestCase):
    def test_solves_two_by_two_system(self):
        solution = solve_linear_system([[2.0, 1.0], [1.0, 3.0]], [3.0, 5.0])
        self.assertAlmostEqual(solution[0], 0.8)
        self.assertAlmostEqual(solution[1], 1.4)

    def test_singular_system_is_rejected(self):
        with self.assertRaises(BacktestError):
            solve_linear_system([[1.0, 2.0], [2.0, 4.0]], [3.0, 6.0])

    def test_shape_mismatch_is_rejected(self):
        with self.assertRaises(BacktestError):
            solve_linear_system([[1.0, 0.0]], [1.0])


class TestRidgeRegression(unittest.TestCase):
    def test_recovers_a_linear_relation(self):
        features = []
        targets = []
        for index in range(80):
            first = (index % 10) / 3.0
            second = (index // 10) / 4.0
            features.append((first, second))
            targets.append(3.0 * first - 2.0 * second + 4.0)

        model = RidgeRegression(1e-6).fit(features, targets)
        for row, target in zip(features, targets):
            self.assertAlmostEqual(model.predict_row(row), target, places=4)

    def test_constant_feature_column_does_not_break_standardization(self):
        features = [(1.0, 2.0) for _ in range(10)]
        targets = [5.0 for _ in range(10)]
        model = RidgeRegression(1.0).fit(features, targets)
        self.assertAlmostEqual(model.predict_row((1.0, 2.0)), 5.0, places=6)

    def test_invalid_lambda_is_rejected(self):
        with self.assertRaises(BacktestError):
            RidgeRegression(0.0)


class TestHistoricalMedian(unittest.TestCase):
    def test_target_hour_then_camera_then_global_fallback(self):
        samples = [
            sample_at(
                "cam-1",
                datetime(2026, 11, 5, 9, 30, tzinfo=ICT),
                datetime(2026, 11, 5, 10, 0, tzinfo=ICT),
                0.20,
                0.20,
            ),
            sample_at(
                "cam-1",
                datetime(2026, 11, 5, 9, 35, tzinfo=ICT),
                datetime(2026, 11, 5, 10, 5, tzinfo=ICT),
                0.40,
                0.40,
            ),
            sample_at(
                "cam-1",
                datetime(2026, 11, 5, 10, 50, tzinfo=ICT),
                datetime(2026, 11, 5, 11, 0, tzinfo=ICT),
                0.50,
                0.80,
            ),
            sample_at(
                "cam-2",
                datetime(2026, 11, 5, 9, 30, tzinfo=ICT),
                datetime(2026, 11, 5, 10, 0, tzinfo=ICT),
                0.60,
                0.60,
            ),
        ]
        model = HistoricalMedianModel().fit(samples, ICT)

        self.assertAlmostEqual(model.predict_one("cam-1", 10), 0.30)
        self.assertAlmostEqual(model.predict_one("cam-1", 11), 0.80)
        self.assertAlmostEqual(model.predict_one("cam-2", 10), 0.60)
        self.assertAlmostEqual(model.predict_one("cam-2", 11), 0.60)
        self.assertAlmostEqual(model.predict_one("cam-3", 10), 0.50)

    def test_bucket_uses_target_hour_not_current_hour(self):
        samples = [
            sample_at(
                "cam-1",
                datetime(2026, 11, 5, 10, 30, tzinfo=ICT),
                datetime(2026, 11, 5, 11, 0, tzinfo=ICT),
                0.50,
                0.80,
            ),
            sample_at(
                "cam-1",
                datetime(2026, 11, 5, 10, 35, tzinfo=ICT),
                datetime(2026, 11, 5, 11, 5, tzinfo=ICT),
                0.50,
                1.00,
            ),
            sample_at(
                "cam-1",
                datetime(2026, 11, 5, 9, 30, tzinfo=ICT),
                datetime(2026, 11, 5, 10, 0, tzinfo=ICT),
                0.50,
                0.10,
            ),
        ]
        model = HistoricalMedianModel().fit(samples, ICT)

        self.assertAlmostEqual(model.predict_one("cam-1", 11), 0.90)
        self.assertAlmostEqual(model.predict_one("cam-1", 10), 0.10)


class TestMetrics(unittest.TestCase):
    def test_mae_rmse_and_directional_accuracy(self):
        metrics = compute_metrics(
            predictions=[0.25, 0.60],
            currents=[0.20, 0.50],
            targets=[0.30, 0.40],
        )
        self.assertAlmostEqual(metrics.mae, 0.125)
        self.assertAlmostEqual(metrics.rmse, 0.14577379737113254)
        self.assertAlmostEqual(metrics.bias, 0.075)
        self.assertAlmostEqual(metrics.directional_accuracy, 0.5)
        self.assertEqual(metrics.directional_matches, 1)
        self.assertEqual(metrics.sample_count, 2)

    def test_direction_treats_numerical_noise_as_flat(self):
        self.assertEqual(direction(1e-13), 0)
        self.assertEqual(direction(-1e-13), 0)
        self.assertEqual(direction(0.01), 1)
        self.assertEqual(direction(-0.01), -1)

    def test_length_mismatch_is_rejected(self):
        with self.assertRaises(BacktestError):
            compute_metrics([0.1], [0.1, 0.2], [0.1])


class TestChronologicalSplit(unittest.TestCase):
    def test_split_is_sorted_and_disjoint(self):
        records = [make_record(minute=5 * index, si=0.5) for index in range(10)]
        records.reverse()

        split = chronological_split(records, train_ratio=0.70)

        self.assertEqual(len(split.train_times), 7)
        self.assertEqual(len(split.test_times), 3)
        self.assertEqual(split.cutoff, START + timedelta(minutes=35))
        self.assertTrue(all(value < split.cutoff for value in split.train_times))
        self.assertTrue(all(value >= split.cutoff for value in split.test_times))
        self.assertEqual(
            set(split.train_times) | set(split.test_times),
            {record.bin_start for record in records},
        )

    def test_single_timestamp_is_rejected(self):
        with self.assertRaises(BacktestError):
            chronological_split([make_record(minute=0)], train_ratio=0.7)

    def test_invalid_ratio_is_rejected(self):
        records = [make_record(minute=0), make_record(minute=5)]
        with self.assertRaises(BacktestError):
            chronological_split(records, train_ratio=0.0)


class TestTargetAlignment(unittest.TestCase):
    def test_exact_future_bins_are_required_and_boundary_is_purged(self):
        records = [
            make_record(
                minute=minute,
                si=0.50 + 0.01 * minute,
                complete=(minute not in (0, 25)),
            )
            for minute in range(0, 40, 5)
        ]
        index = build_saturation_index(records)
        split = ChronologicalSplit(
            cutoff=START + timedelta(minutes=20),
            train_times=tuple(START + timedelta(minutes=value) for value in range(0, 20, 5)),
            test_times=tuple(START + timedelta(minutes=value) for value in range(20, 40, 5)),
        )

        samples = build_evaluation_samples(records, index, split, horizon_minutes=10)

        # Train: minute 0 has an incomplete feature row; minute 5 is usable.
        # Minutes 10 and 15 are purged because their targets land in the test window.
        self.assertEqual(len(samples.train), 1)
        self.assertEqual(samples.train[0].record.bin_start, START + timedelta(minutes=5))
        self.assertAlmostEqual(samples.train[0].current, 0.55)
        self.assertAlmostEqual(samples.train[0].target, 0.65)

        # The median baseline trains on every usable target, including rows with
        # incomplete lag features, so minute 0 (incomplete) is retained here.
        self.assertEqual(len(samples.train_all), 2)
        self.assertEqual(
            [sample.record.bin_start for sample in samples.train_all],
            [START + timedelta(minutes=0), START + timedelta(minutes=5)],
        )

        # Test: minute 20 is usable; minute 25 lacks features; 30 and 35 have no t+10 row.
        self.assertEqual(len(samples.test), 1)
        self.assertEqual(samples.test[0].record.bin_start, START + timedelta(minutes=20))
        self.assertAlmostEqual(samples.test[0].target, 0.80)

        self.assertEqual(samples.stats["train_missing_features"], 1)
        self.assertEqual(samples.stats["train_purged_boundary"], 2)
        self.assertEqual(samples.stats["test_missing_features"], 1)
        self.assertEqual(samples.stats["test_missing_target"], 2)

    def test_future_target_is_matched_per_camera(self):
        records = [
            make_record("cam-1", 0, 0.50),
            make_record("cam-1", 10, 0.60),
            make_record("cam-2", 0, 0.70),
            make_record("cam-2", 5, 0.80),
        ]
        index = build_saturation_index(records)
        split = ChronologicalSplit(
            cutoff=START + timedelta(minutes=20),
            train_times=tuple(
                START + timedelta(minutes=value) for value in range(0, 20, 5)
            ),
            test_times=tuple(
                START + timedelta(minutes=value) for value in range(20, 20 + 5, 5)
            ),
        )

        samples = build_evaluation_samples(records, index, split, horizon_minutes=10)

        # cam-1 at minute 0 matches its own minute-10 row. cam-2 at minute 0
        # must not borrow cam-1's future row, even though it exists at the same
        # timestamp. cam-1 at minute 10 is purged at the boundary.
        self.assertEqual(len(samples.train), 1)
        self.assertEqual(samples.train[0].record.camera_id, "cam-1")
        self.assertEqual(samples.train[0].target_time, START + timedelta(minutes=10))
        self.assertEqual(len(samples.train_all), 1)
        self.assertEqual(samples.stats["train_missing_target"], 2)
        self.assertEqual(samples.stats["train_purged_boundary"], 1)
        self.assertEqual(samples.stats["test_missing_target"], 0)

    def test_boundary_purge_precedes_missing_target_check(self):
        records = [
            make_record(minute=0, si=0.50),
            make_record(minute=15, si=0.60),
            make_record(minute=20, si=0.70),
            make_record(minute=30, si=0.80),
        ]
        index = build_saturation_index(records)
        split = ChronologicalSplit(
            cutoff=START + timedelta(minutes=20),
            train_times=tuple(
                START + timedelta(minutes=value) for value in range(0, 20, 5)
            ),
            test_times=tuple(
                START + timedelta(minutes=value) for value in range(20, 35, 5)
            ),
        )

        samples = build_evaluation_samples(records, index, split, horizon_minutes=10)

        # Minute 0 has a gap before the cutoff, so it is a missing target.
        # Minute 15 crosses the cutoff; it is purged even though its target row
        # is also missing, which is what the timestamp-first check guarantees.
        self.assertEqual(samples.stats["train_missing_target"], 1)
        self.assertEqual(samples.stats["train_purged_boundary"], 1)
        self.assertEqual(len(samples.test), 1)
        self.assertEqual(samples.stats["test_missing_target"], 1)

    def test_duplicate_saturation_index_is_rejected(self):
        record = make_record(minute=0)
        with self.assertRaises(BacktestError):
            build_saturation_index([record, record])


class TestFeatureLoading(unittest.TestCase):
    def setUp(self):
        self.tempdir = tempfile.TemporaryDirectory()
        self.addCleanup(self.tempdir.cleanup)
        self.root = Path(self.tempdir.name)

    def test_derives_calendar_features_and_infers_bin_width(self):
        path = self.root / "features.csv"
        columns = (
            "camera_id",
            "bin_start_utc",
            "saturation_index",
            "lag_5m",
            "lag_10m",
            "lag_15m",
            "rolling_mean_15m",
        )
        write_csv(
            path,
            [
                {
                    "camera_id": "cam-1",
                    "bin_start_utc": "2026-11-05T01:00:00Z",
                    "saturation_index": "0.400000",
                    "lag_5m": "",
                    "lag_10m": "",
                    "lag_15m": "",
                    "rolling_mean_15m": "",
                },
                {
                    "camera_id": "cam-1",
                    "bin_start_utc": "2026-11-05T01:05:00Z",
                    "saturation_index": "0.500000",
                    "lag_5m": "0.400000",
                    "lag_10m": "0.300000",
                    "lag_15m": "0.200000",
                    "rolling_mean_15m": "0.400000",
                },
            ],
            columns=columns,
        )

        records, stats = load_feature_records(path)

        self.assertEqual(len(records), 2)
        self.assertEqual(stats["bin_minutes"], 5)
        self.assertEqual(stats["bin_minutes_source"], "inferred")
        self.assertEqual(records[0].hour_of_day, 8)  # 01:00Z is 08:00 ICT
        self.assertEqual(records[0].day_of_week, 3)  # Thursday
        self.assertFalse(records[0].features_complete)
        self.assertTrue(records[1].features_complete)

    def test_calendar_mismatch_is_rejected(self):
        path = self.root / "bad-calendar.csv"
        write_csv(
            path,
            [
                {
                    "camera_id": "cam-1",
                    "bin_start_utc": "2026-11-05T01:00:00Z",
                    "saturation_index": "0.400000",
                    "hour_of_day": "0",
                    "day_of_week": "3",
                }
            ],
        )
        with self.assertRaises(BacktestError) as context:
            load_feature_records(path)
        self.assertIn("hour_of_day mismatch", str(context.exception))

    def test_duplicate_camera_and_timestamp_is_rejected(self):
        path = self.root / "duplicate.csv"
        row = {
            "camera_id": "cam-1",
            "bin_start_utc": "2026-11-05T01:00:00Z",
            "saturation_index": "0.400000",
        }
        write_csv(path, [row, row])
        with self.assertRaises(BacktestError):
            load_feature_records(path)

    def test_missing_required_column_is_rejected(self):
        path = self.root / "missing.csv"
        write_csv(
            path,
            [
                {
                    "camera_id": "cam-1",
                    "bin_start_utc": "2026-11-05T01:00:00Z",
                    "saturation_index": "0.4",
                }
            ],
            columns=("camera_id", "bin_start_utc", "saturation_index"),
        )
        with self.assertRaises(BacktestError) as context:
            load_feature_records(path)
        self.assertIn("lag_5m", str(context.exception))

    def test_invalid_rows_are_counted_and_skipped(self):
        path = self.root / "mixed.csv"
        write_csv(
            path,
            [
                {
                    "camera_id": "cam-1",
                    "bin_start_utc": "2026-11-05T01:00:00Z",
                    "saturation_index": "0.400000",
                },
                {
                    "camera_id": "cam-1",
                    "bin_start_utc": "not-a-timestamp",
                    "saturation_index": "0.500000",
                },
                {
                    "camera_id": "cam-1",
                    "bin_start_utc": "2026-11-05T01:10:00Z",
                    "saturation_index": "",
                },
            ],
        )

        records, stats = load_feature_records(path)

        self.assertEqual(len(records), 1)
        self.assertEqual(stats["total_rows"], 3)
        self.assertEqual(stats["accepted_rows"], 1)
        self.assertEqual(stats["invalid_rows"], 1)
        self.assertEqual(stats["rows_without_saturation_index"], 1)


class TestVerificationGate(unittest.TestCase):
    def test_gate_passes_when_candidate_beats_persistence(self):
        results = {
            "10": {"gate": {"beats_persistence": True}},
            "20": {"gate": {"beats_persistence": False}},
            "30": {"gate": {"beats_persistence": False}},
        }
        verification = build_verification(results, "ridge_delta")
        self.assertTrue(verification["passed"])
        self.assertEqual(verification["beats_persistence_horizons"], [10])

    def test_gate_fails_on_a_tie(self):
        results = {
            "10": {"gate": {"beats_persistence": False}},
            "20": {"gate": {"beats_persistence": False}},
            "30": {"gate": {"beats_persistence": False}},
        }
        verification = build_verification(results, "ridge_delta")
        self.assertFalse(verification["passed"])
        self.assertEqual(verification["beats_persistence_horizons"], [])


class TestHorizonEvaluation(unittest.TestCase):
    def test_candidate_and_baselines_share_the_evaluation_set(self):
        records = [
            make_record(minute=minute, si=0.30 + 0.001 * minute)
            for minute in range(0, 600, 5)
        ]
        index = build_saturation_index(records)
        split = chronological_split(records, train_ratio=0.7)
        config = BacktestConfig(horizons_minutes=(10,))

        result = evaluate_horizon(records, index, split, 10, config)

        for model_id in ("persistence", "historical_median", "ridge_delta"):
            self.assertEqual(
                result["models"][model_id]["metrics"]["sample_count"],
                result["evaluation"]["test_samples"],
            )
        self.assertEqual(result["models"]["ridge_delta"]["metrics"]["bias"], 0.0)
        self.assertTrue(result["gate"]["beats_persistence"])

    def test_level_target_mode_runs_and_reports_its_model_id(self):
        records = [
            make_record(minute=minute, si=0.30 + 0.001 * minute)
            for minute in range(0, 600, 5)
        ]
        index = build_saturation_index(records)
        split = chronological_split(records, train_ratio=0.7)
        config = BacktestConfig(horizons_minutes=(10,), target_mode="level")

        result = evaluate_horizon(records, index, split, 10, config)

        self.assertIn("ridge_level", result["models"])
        self.assertEqual(
            result["models"]["ridge_level"]["parameters"]["target_mode"], "level"
        )
        self.assertGreater(result["evaluation"]["test_samples"], 0)

    def test_historical_median_is_fitted_on_train_targets_only(self):
        records = [
            make_record(
                minute=minute,
                si=0.30 if minute < 210 else 0.90,
            )
            for minute in range(0, 300, 5)
        ]
        index = build_saturation_index(records)
        split = chronological_split(records, train_ratio=0.7)
        config = BacktestConfig(horizons_minutes=(10,))

        result = evaluate_horizon(records, index, split, 10, config)

        self.assertAlmostEqual(
            result["models"]["historical_median"]["parameters"]["global_median"],
            0.30,
            places=6,
        )


class TestEndToEnd(unittest.TestCase):
    def setUp(self):
        self.tempdir = tempfile.TemporaryDirectory()
        self.addCleanup(self.tempdir.cleanup)
        self.root = Path(self.tempdir.name)

    def _run(self, rows, *extra: str):
        input_path = self.root / "features.csv"
        write_csv(input_path, rows)
        json_path = self.root / "backtest.json"
        markdown_path = self.root / "backtest.md"
        exit_code = run(
            [
                "--input",
                str(input_path),
                "--json-out",
                str(json_path),
                "--markdown-out",
                str(markdown_path),
                "--log-level",
                "ERROR",
                *extra,
            ]
        )
        return exit_code, json_path, markdown_path

    def test_writes_json_and_markdown_and_passes_the_gate(self):
        exit_code, json_path, markdown_path = self._run(
            ramp_rows(),
            "--data-note",
            "synthetic linear ramp fixture",
        )
        self.assertEqual(exit_code, EXIT_OK)
        self.assertTrue(json_path.exists())
        self.assertTrue(markdown_path.exists())

        result = json.loads(json_path.read_text(encoding="utf-8"))
        self.assertEqual(result["schema_version"], 1)
        self.assertEqual(set(result["results"]), {"10", "20", "30"})
        self.assertTrue(result["verification"]["passed"])
        self.assertEqual(
            result["verification"]["beats_persistence_horizons"], [10, 20, 30]
        )
        self.assertTrue(result["split"]["no_shuffle"])
        self.assertEqual(result["split"]["train_timestamp_count"], 806)
        self.assertEqual(result["split"]["test_timestamp_count"], 346)
        self.assertEqual(
            result["split"]["train_row_count"] + result["split"]["test_row_count"],
            1152,
        )
        self.assertEqual(
            result["split"]["cutoff_utc"],
            (START + timedelta(minutes=5 * 806)).isoformat().replace("+00:00", "Z"),
        )
        self.assertEqual(result["data_provenance"]["note"], "synthetic linear ramp fixture")
        self.assertEqual(len(result["data_provenance"]["sha256"]), 64)

        for horizon_key in ("10", "20", "30"):
            horizon = result["results"][horizon_key]
            self.assertTrue(horizon["gate"]["beats_persistence"])
            self.assertLess(
                horizon["models"]["ridge_delta"]["metrics"]["mae"],
                horizon["models"]["persistence"]["metrics"]["mae"],
            )

        markdown = markdown_path.read_text(encoding="utf-8")
        self.assertIn("# Short-Horizon Congestion Forecast Prototype v1", markdown)
        self.assertIn("| Horizon | Model | MAE | RMSE |", markdown)
        self.assertIn("| 10 min | Persistence |", markdown)
        self.assertIn("Gate result: PASSED", markdown)
        self.assertIn("synthetic linear ramp fixture", markdown)

    def test_tie_with_persistence_returns_gate_failure_but_still_writes_outputs(self):
        exit_code, json_path, markdown_path = self._run(
            ramp_rows(count=600, si_at=lambda index: 0.5)
        )
        self.assertEqual(exit_code, EXIT_GATE_FAILED)

        result = json.loads(json_path.read_text(encoding="utf-8"))
        self.assertFalse(result["verification"]["passed"])
        self.assertEqual(result["verification"]["beats_persistence_horizons"], [])
        for horizon in result["results"].values():
            self.assertAlmostEqual(horizon["gate"]["persistence_mae"], 0.0)
            self.assertAlmostEqual(horizon["gate"]["candidate_mae"], 0.0)
            self.assertFalse(horizon["gate"]["beats_persistence"])

        markdown = markdown_path.read_text(encoding="utf-8")
        self.assertIn("Gate result: FAILED", markdown)

    def test_missing_input_returns_data_error(self):
        exit_code = run(
            [
                "--input",
                str(self.root / "does-not-exist.csv"),
                "--json-out",
                str(self.root / "out.json"),
                "--markdown-out",
                str(self.root / "out.md"),
                "--log-level",
                "ERROR",
            ]
        )
        self.assertEqual(exit_code, EXIT_DATA_ERROR)

    def test_identical_output_paths_are_rejected(self):
        input_path = self.root / "features.csv"
        write_csv(input_path, ramp_rows(count=100))
        shared_path = self.root / "shared.out"
        exit_code = run(
            [
                "--input",
                str(input_path),
                "--json-out",
                str(shared_path),
                "--markdown-out",
                str(shared_path),
                "--log-level",
                "ERROR",
            ]
        )
        self.assertEqual(exit_code, EXIT_CONFIG_ERROR)
        self.assertFalse(shared_path.exists())

    def test_committed_fixture_runs_and_passes_the_gate(self):
        self.assertTrue(FIXTURE.exists(), f"missing fixture: {FIXTURE}")
        json_path = self.root / "fixture.json"
        markdown_path = self.root / "fixture.md"
        exit_code = run(
            [
                "--input",
                str(FIXTURE),
                "--json-out",
                str(json_path),
                "--markdown-out",
                str(markdown_path),
                "--log-level",
                "ERROR",
            ]
        )
        self.assertEqual(exit_code, EXIT_OK)

        result = json.loads(json_path.read_text(encoding="utf-8"))
        self.assertTrue(result["verification"]["passed"])
        self.assertEqual(result["input_summary"]["camera_count"], 2)
        self.assertEqual(result["input_summary"]["bin_minutes"], 5)
        for horizon in result["results"].values():
            self.assertTrue(horizon["gate"]["beats_persistence"])

    def test_invalid_horizon_returns_config_error(self):
        input_path = self.root / "features.csv"
        write_csv(input_path, ramp_rows(count=50))
        exit_code = run(
            [
                "--input",
                str(input_path),
                "--json-out",
                str(self.root / "out.json"),
                "--markdown-out",
                str(self.root / "out.md"),
                "--horizons",
                "ten",
                "--log-level",
                "ERROR",
            ]
        )
        self.assertEqual(exit_code, 1)


class TestReportRendering(unittest.TestCase):
    def test_report_contains_gate_and_sample_accounting(self):
        records = [
            make_record(minute=minute, si=0.30 + 0.001 * minute)
            for minute in range(0, 600, 5)
        ]
        config = BacktestConfig(
            horizons_minutes=(10, 20, 30),
            target_mode="delta",
        )
        result = run_backtest(
            records,
            input_summary={
                "path": "features.csv",
                "sha256": "0" * 64,
                "accepted_rows": len(records),
                "total_rows": len(records),
                "invalid_rows": 0,
                "rows_without_saturation_index": 0,
                "camera_count": 1,
                "bin_minutes": 5,
                "bin_minutes_source": "column",
                "time_range_utc": {"min": "2026-11-01T00:00:00Z", "max": "2026-11-01T09:55:00Z"},
            },
            config=config,
            data_note="unit-test fixture",
            command="python3 tools/forecast/backtest.py --input features.csv",
            generated_at=datetime(2026, 11, 5, 0, 0, tzinfo=UTC),
        )
        self.assertTrue(result["verification"]["passed"])

        markdown = render_markdown(result)
        self.assertIn("## 1. Data provenance", markdown)
        self.assertIn("unit-test fixture", markdown)
        self.assertIn("## 4. Verification gate", markdown)
        self.assertIn("## 5. Sample accounting", markdown)
        self.assertIn(
            "| Horizon | Train (ridge) | Train (median) | Purged at boundary | Test |",
            markdown,
        )
        self.assertIn("python3 tools/forecast/backtest.py --input features.csv", markdown)
        for horizon in result["results"].values():
            accounting = horizon["sample_accounting"]
            # Every complete training sample is also a median training sample.
            self.assertGreaterEqual(
                accounting["train_median_samples"], accounting["train_samples"]
            )


class TestConfigValidation(unittest.TestCase):
    def test_invalid_values_are_rejected(self):
        with self.assertRaises(BacktestError):
            BacktestConfig(train_ratio=0.0).validate()
        with self.assertRaises(BacktestError):
            BacktestConfig(ridge_lambda=0.0).validate()
        with self.assertRaises(BacktestError):
            BacktestConfig(target_mode="future").validate()
        with self.assertRaises(BacktestError):
            BacktestConfig(horizons_minutes=(10, 10)).validate()


class TestAblationBacktest(unittest.TestCase):
    def setUp(self):
        self.tempdir = tempfile.TemporaryDirectory()
        self.addCleanup(self.tempdir.cleanup)
        self.root = Path(self.tempdir.name)
        self.fixture_v2 = (
            REPO_ROOT
            / "tools"
            / "forecast"
            / "fixtures"
            / "synthetic_congestion_v2_5m.csv"
        )

    def test_ablation_backtest_runs_and_passes_gate(self):
        self.assertTrue(self.fixture_v2.exists(), "v2 fixture must exist")
        json_out = self.root / "ablation.json"
        md_out = self.root / "ablation.md"

        exit_code = run(
            [
                "--input",
                str(self.fixture_v2),
                "--ablation",
                "--ablation-json-out",
                str(json_out),
                "--ablation-markdown-out",
                str(md_out),
                "--log-level",
                "ERROR",
            ]
        )
        self.assertEqual(exit_code, 0)
        self.assertTrue(json_out.exists())
        self.assertTrue(md_out.exists())

        payload = json.loads(json_out.read_text(encoding="utf-8"))
        self.assertEqual(payload["report_type"], "3-way-ablation")
        self.assertIn("variant_a", payload["variants"])
        self.assertIn("variant_b", payload["variants"])
        self.assertIn("variant_c", payload["variants"])

        # Check gate decision
        gate = payload["gate"]
        self.assertTrue(gate["passed"])
        self.assertTrue(gate["retained"])
        self.assertTrue(gate["improves_ge_2pct"])
        self.assertFalse(gate["degrades_gt_5pct"])

        # Check Markdown sections
        markdown = md_out.read_text(encoding="utf-8")
        self.assertIn("# Short-Horizon Congestion Forecast Ablation Report v2", markdown)
        self.assertIn("## 1. Data provenance", markdown)
        self.assertIn("## 2. Method & 3-Way Ablation Protocol", markdown)
        self.assertIn("## 3. Results across Horizons", markdown)
        self.assertIn("## 4. Directional Roadway Breakdown", markdown)
        self.assertIn("## 5. Verification Gate & Ablation Protocol", markdown)
        self.assertIn("## 6. Sample Accounting", markdown)
        self.assertIn("Variant B (+weather)", markdown)
        self.assertIn("Variant C (+directional)", markdown)
        self.assertIn("PASSED", markdown)


if __name__ == "__main__":
    unittest.main()
