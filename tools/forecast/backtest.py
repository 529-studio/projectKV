#!/usr/bin/env python3
"""
backtest.py - Chronological backtest for the short-horizon congestion forecast.

Sprint 9 / Issue #302 (S9-T6) Part 2:
  * Reads the per-camera feature CSV written by extract_features.py.
  * Splits the timeline chronologically (default 70/30) and never shuffles.
  * Aligns each camera's future saturation index at t+10, t+20 and t+30 minutes.
  * Compares persistence, historical median, and a regularized ridge model.
  * Reports MAE, RMSE and directional accuracy per horizon, then enforces the
    gate that the candidate must beat persistence on at least one horizon.

The implementation uses only the Python standard library. Ridge regression is
solved through the regularized normal equations with partial pivoting, so the
harness runs on a laptop or CI box without numpy or scikit-learn.

Outputs:
  * JSON results (default: docs/reports/forecast_prototype_v1.json)
  * Markdown report (default: docs/reports/forecast_prototype_v1.md)

Adheres strictly to AGENTS.md:
  * Rule 1: zero emojis, English-only logs and comments.
  * Rule 2: targets must exist at the exact future bin; missing values are
    excluded and reported, never imputed. Training rows whose target falls past
    the split cutoff are purged to prevent boundary leakage.
  * Rule 8: no credentials. The input CSV is read-only.

Example:
    python3 tools/forecast/backtest.py \
        --input data/forecast/features_5m.csv \
        --json-out docs/reports/forecast_prototype_v1.json \
        --markdown-out docs/reports/forecast_prototype_v1.md
"""

from __future__ import annotations

import argparse
import csv
import hashlib
import json
import logging
import math
import os
import shlex
import statistics
import sys
import tempfile
from collections import Counter, defaultdict
from dataclasses import dataclass
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Any, Iterable, Optional, Sequence
from zoneinfo import ZoneInfo

LOG = logging.getLogger("forecast.backtest")

SCRIPT_VERSION = "2.0.0"
BACKTEST_SCHEMA_VERSION = 1

REPO_ROOT = Path(__file__).resolve().parent.parent.parent
DEFAULT_JSON_OUT = REPO_ROOT / "docs" / "reports" / "forecast_prototype_v1.json"
DEFAULT_MARKDOWN_OUT = REPO_ROOT / "docs" / "reports" / "forecast_prototype_v1.md"
DEFAULT_ABLATION_MARKDOWN_OUT = REPO_ROOT / "docs" / "reports" / "forecast_ablation_v2.md"
DEFAULT_ABLATION_JSON_OUT = REPO_ROOT / "docs" / "reports" / "forecast_ablation_v2.json"

DEFAULT_HORIZONS_MINUTES = (10, 20, 30)
DEFAULT_FEATURE_COLUMNS = (
    "lag_5m",
    "lag_10m",
    "lag_15m",
    "rolling_mean_15m",
    "hour_of_day",
    "day_of_week",
)

VARIANT_A_COLUMNS = DEFAULT_FEATURE_COLUMNS
WEATHER_COLUMNS = (
    "precip_mm_15m",
    "precip_mm_60m",
    "rain_flag",
    "hours_since_rain",
    "weather_code_class",
    "radar_active",
)
DIRECTIONAL_COLUMNS = (
    "peer_si",
    "si_delta_ab",
)
VARIANT_B_COLUMNS = VARIANT_A_COLUMNS + WEATHER_COLUMNS
VARIANT_C_COLUMNS = VARIANT_B_COLUMNS + DIRECTIONAL_COLUMNS
ALL_ABLATION_COLUMNS = tuple(
    sorted(set(VARIANT_A_COLUMNS + WEATHER_COLUMNS + DIRECTIONAL_COLUMNS))
)
DEFAULT_TRAIN_RATIO = 0.70
DEFAULT_RIDGE_LAMBDA = 1.0
DEFAULT_TIMEZONE = "Asia/Ho_Chi_Minh"
DEFAULT_TARGET_MODE = "delta"

# Numerical noise below this absolute delta is treated as "no movement" when
# scoring direction. A prediction such as 1e-16 must not be counted as bullish.
DIRECTION_EPSILON = 1e-12

# Predictions are floored at zero because saturation index is a non-negative
# ratio. The raw ridge output is kept in the JSON as the model response.
PREDICTION_FLOOR = 0.0

BASE_REQUIRED_COLUMNS = ("camera_id", "bin_start_utc", "saturation_index")
DERIVED_CALENDAR_COLUMNS = ("hour_of_day", "day_of_week")

MODEL_LABELS = {
    "persistence": "Persistence",
    "historical_median": "Historical median",
    "ridge_delta": "Ridge regression (delta)",
    "ridge_level": "Ridge regression (level)",
}

EXIT_OK = 0
EXIT_CONFIG_ERROR = 1
EXIT_DATA_ERROR = 2
EXIT_GATE_FAILED = 3


class BacktestError(Exception):
    """Raised when the input data or configuration cannot support a backtest."""


@dataclass(frozen=True)
class FeatureRecord:
    """One regular-bin feature row parsed from the extractor CSV."""

    camera_id: str
    bin_start: datetime
    saturation_index: float
    hour_of_day: int
    day_of_week: int
    feature_values: tuple[Optional[float], ...]
    direction: str = ""
    feature_map: Optional[dict[str, Optional[float]]] = None

    @property
    def series_id(self) -> str:
        return f"{self.camera_id}:{self.direction}" if self.direction else self.camera_id

    @property
    def features_complete(self) -> bool:
        return all(value is not None for value in self.feature_values)

    def is_complete_for(self, columns: Sequence[str]) -> bool:
        if self.feature_map is None:
            return self.features_complete
        return all(self.feature_map.get(col) is not None for col in columns)

    def feature_vector(self) -> tuple[float, ...]:
        values: list[float] = []
        for value in self.feature_values:
            if value is None:
                raise BacktestError(
                    f"incomplete feature row for camera {self.camera_id} at "
                    f"{self.bin_start.isoformat()}"
                )
            values.append(value)
        return tuple(values)

    def feature_vector_for(self, columns: Sequence[str]) -> tuple[float, ...]:
        if self.feature_map is None:
            return self.feature_vector()
        vals: list[float] = []
        for col in columns:
            v = self.feature_map.get(col)
            if v is None:
                raise BacktestError(
                    f"incomplete feature {col} for series {self.series_id} at "
                    f"{self.bin_start.isoformat()}"
                )
            vals.append(v)
        return tuple(vals)


@dataclass(frozen=True)
class ChronologicalSplit:
    """Global chronological partition over distinct bin start timestamps."""

    cutoff: datetime
    train_times: tuple[datetime, ...]
    test_times: tuple[datetime, ...]


@dataclass(frozen=True)
class EvaluationSample:
    """One current/future pair aligned by camera and exact future bin."""

    record: FeatureRecord
    current: float
    target: float
    target_time: datetime


@dataclass(frozen=True)
class EvaluationSamples:
    """Eligible train and test samples for one horizon plus exclusion counts.

    ``train_all`` contains every valid training pair, including rows whose lag
    features are incomplete. The historical median baseline uses it because it
    needs only camera, target hour, and target value. ``train`` is the subset
    with complete features that the ridge candidate can fit on.
    """

    train: tuple[EvaluationSample, ...]
    train_all: tuple[EvaluationSample, ...]
    test: tuple[EvaluationSample, ...]
    stats: dict[str, int]


@dataclass(frozen=True)
class Metrics:
    """Regression and direction metrics over one model prediction set."""

    sample_count: int
    mae: float
    rmse: float
    bias: float
    directional_accuracy: float
    directional_matches: int

    def to_dict(self) -> dict[str, Any]:
        return {
            "sample_count": self.sample_count,
            "mae": round(self.mae, 8),
            "rmse": round(self.rmse, 8),
            "bias": round(self.bias, 8),
            "directional_accuracy": round(self.directional_accuracy, 8),
            "directional_matches": self.directional_matches,
        }


@dataclass(frozen=True)
class BacktestConfig:
    """Validated backtest configuration shared by every horizon."""

    train_ratio: float = DEFAULT_TRAIN_RATIO
    horizons_minutes: tuple[int, ...] = DEFAULT_HORIZONS_MINUTES
    feature_columns: tuple[str, ...] = DEFAULT_FEATURE_COLUMNS
    target_mode: str = DEFAULT_TARGET_MODE
    ridge_lambda: float = DEFAULT_RIDGE_LAMBDA
    timezone: str = DEFAULT_TIMEZONE
    direction_epsilon: float = DIRECTION_EPSILON

    def validate(self) -> None:
        if not 0.0 < self.train_ratio < 1.0:
            raise BacktestError("train_ratio must be strictly between 0 and 1")
        if not self.horizons_minutes:
            raise BacktestError("at least one horizon is required")
        for horizon in self.horizons_minutes:
            if horizon <= 0:
                raise BacktestError("horizons must be positive")
        if len(set(self.horizons_minutes)) != len(self.horizons_minutes):
            raise BacktestError("horizons must be unique")
        if not self.feature_columns:
            raise BacktestError("at least one feature column is required")
        if self.target_mode not in ("delta", "level"):
            raise BacktestError("target_mode must be 'delta' or 'level'")
        if self.ridge_lambda <= 0.0 or not math.isfinite(self.ridge_lambda):
            raise BacktestError("ridge_lambda must be a positive finite number")
        if self.direction_epsilon < 0.0:
            raise BacktestError("direction_epsilon cannot be negative")

    @property
    def candidate_id(self) -> str:
        return f"ridge_{self.target_mode}"

    def to_dict(self) -> dict[str, Any]:
        return {
            "train_ratio": self.train_ratio,
            "horizons_minutes": list(self.horizons_minutes),
            "feature_columns": list(self.feature_columns),
            "target_mode": self.target_mode,
            "ridge_lambda": self.ridge_lambda,
            "timezone": self.timezone,
            "direction_epsilon": self.direction_epsilon,
            "prediction_floor": PREDICTION_FLOOR,
        }


def parse_utc_timestamp(value: Any) -> Optional[datetime]:
    """Parse a CSV timestamp into an aware UTC datetime.

    The extractor writes RFC3339 UTC with a trailing ``Z``. Naive values are
    interpreted as UTC because the source column is explicitly named
    ``bin_start_utc``. Malformed values return ``None`` so the caller can count
    them instead of accepting a silently shifted timestamp.
    """
    if value is None:
        return None
    text = str(value).strip()
    if not text:
        return None
    if text.endswith("Z"):
        text = text[:-1] + "+00:00"
    try:
        parsed = datetime.fromisoformat(text)
    except ValueError:
        return None
    if parsed.tzinfo is None:
        parsed = parsed.replace(tzinfo=timezone.utc)
    return parsed.astimezone(timezone.utc)


def parse_float(value: Any) -> Optional[float]:
    """Return a finite float or ``None`` for blank/non-finite values."""
    if value is None or isinstance(value, bool):
        return None
    text = str(value).strip()
    if not text:
        return None
    try:
        number = float(text)
    except ValueError:
        return None
    if not math.isfinite(number):
        return None
    return number


def parse_int(value: Any) -> Optional[int]:
    if value is None:
        return None
    text = str(value).strip()
    if not text:
        return None
    try:
        return int(text)
    except ValueError:
        return None


def _validated_calendar_value(
    raw_value: Any,
    expected: int,
    label: str,
    camera_id: str,
    bin_start: datetime,
    timezone_name: str,
    lower: int,
    upper: int,
) -> int:
    """Use a supplied calendar column only when it matches the local timestamp.

    ``extract_features.py`` derives ``hour_of_day`` and ``day_of_week`` in the
    configured IANA timezone. A CSV generated with a different timezone, or one
    that accidentally wrote UTC values, would otherwise train the model on
    silently wrong seasonality. The timestamp is the source of truth: a blank
    value is derived, a malformed value fails, and a mismatch fails with the
    timezone named in the error.
    """
    if raw_value is None or not str(raw_value).strip():
        return expected
    supplied = parse_int(raw_value)
    if supplied is None or not lower <= supplied <= upper:
        raise BacktestError(
            f"invalid {label} for camera {camera_id} at {bin_start.isoformat()}: "
            f"{raw_value!r}"
        )
    if supplied != expected:
        raise BacktestError(
            f"{label} mismatch for camera {camera_id} at {bin_start.isoformat()}: "
            f"CSV value {supplied}, expected {expected} in timezone "
            f"{timezone_name}. Regenerate the CSV with the same --timezone as "
            "the backtest."
        )
    return supplied


def _sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def _infer_bin_minutes(records: Sequence[FeatureRecord]) -> tuple[int, str]:
    """Return the modal bin width and whether it came from a column or deltas."""
    by_camera: dict[str, list[datetime]] = defaultdict(list)
    for record in records:
        by_camera[record.camera_id].append(record.bin_start)
    differences: Counter[int] = Counter()
    for times in by_camera.values():
        ordered = sorted(times)
        for earlier, later in zip(ordered, ordered[1:]):
            minutes = int((later - earlier).total_seconds() // 60)
            if minutes > 0:
                differences[minutes] += 1
    if not differences:
        return 0, "unknown"
    return differences.most_common(1)[0][0], "inferred"


def load_feature_records(
    path: Path,
    feature_columns: Sequence[str] = DEFAULT_FEATURE_COLUMNS,
    timezone_name: str = DEFAULT_TIMEZONE,
) -> tuple[list[FeatureRecord], dict[str, Any]]:
    """Read and validate the extractor CSV.

    Rows without a usable camera, timestamp, or non-negative saturation index
    are counted and skipped. Every accepted ``(camera_id, bin_start_utc)`` pair
    must be unique: duplicate keys make the future target ambiguous, so they
    fail the run instead of being silently overwritten. Supplied calendar
    columns are validated against the local timestamp and ``timezone_name``;
    a mismatch fails instead of training on the wrong seasonality.
    """
    path = Path(path)
    if not path.exists():
        raise BacktestError(f"input CSV does not exist: {path}")
    if not path.is_file():
        raise BacktestError(f"input path is not a file: {path}")

    try:
        local_tz = ZoneInfo(timezone_name)
    except Exception as exc:  # ZoneInfoNotFoundError varies with tzdata packaging
        raise BacktestError(f"invalid timezone {timezone_name!r}: {exc}") from exc

    required = set(BASE_REQUIRED_COLUMNS)
    required.update(
        column
        for column in feature_columns
        if column not in DERIVED_CALENDAR_COLUMNS
    )

    records: list[FeatureRecord] = []
    seen: set[tuple[str, datetime]] = set()
    stats: dict[str, Any] = {
        "total_rows": 0,
        "accepted_rows": 0,
        "invalid_rows": 0,
        "rows_without_saturation_index": 0,
        "sha256": _sha256_file(path),
    }
    column_bin_counts: Counter[int] = Counter()
    min_time: Optional[datetime] = None
    max_time: Optional[datetime] = None

    try:
        handle = path.open("r", encoding="utf-8-sig", newline="")
    except OSError as exc:
        raise BacktestError(f"cannot open input CSV {path}: {exc}") from exc

    with handle:
        reader = csv.DictReader(handle)
        fieldnames = tuple(reader.fieldnames or ())
        if not fieldnames:
            raise BacktestError(f"input CSV has no header: {path}")
        missing = sorted(required - set(fieldnames))
        if missing:
            raise BacktestError(
                "input CSV is missing required columns: " + ", ".join(missing)
            )

        for raw in reader:
            stats["total_rows"] += 1

            camera_id = str(raw.get("camera_id") or "").strip()
            if not camera_id:
                stats["invalid_rows"] += 1
                continue

            bin_start = parse_utc_timestamp(raw.get("bin_start_utc"))
            if bin_start is None:
                stats["invalid_rows"] += 1
                continue

            raw_si = raw.get("saturation_index")
            saturation_index = parse_float(raw_si)
            if saturation_index is None:
                if raw_si is None or not str(raw_si).strip():
                    stats["rows_without_saturation_index"] += 1
                else:
                    stats["invalid_rows"] += 1
                continue
            if saturation_index < 0.0:
                stats["invalid_rows"] += 1
                continue

            local_start = bin_start.astimezone(local_tz)
            hour_of_day = _validated_calendar_value(
                raw.get("hour_of_day"),
                expected=local_start.hour,
                label="hour_of_day",
                camera_id=camera_id,
                bin_start=bin_start,
                timezone_name=timezone_name,
                lower=0,
                upper=23,
            )
            day_of_week = _validated_calendar_value(
                raw.get("day_of_week"),
                expected=local_start.weekday(),
                label="day_of_week",
                camera_id=camera_id,
                bin_start=bin_start,
                timezone_name=timezone_name,
                lower=0,
                upper=6,
            )

            direction = str(raw.get("direction") or "").strip()
            series_id = f"{camera_id}:{direction}" if direction else camera_id

            feature_map: dict[str, Optional[float]] = {
                "hour_of_day": float(hour_of_day),
                "day_of_week": float(day_of_week),
            }
            for col_name, val_str in raw.items():
                if col_name not in feature_map:
                    feature_map[col_name] = parse_float(val_str)

            source_values: dict[str, Optional[float]] = {}
            for column in feature_columns:
                if column == "hour_of_day":
                    source_values[column] = float(hour_of_day)
                elif column == "day_of_week":
                    source_values[column] = float(day_of_week)
                else:
                    source_values[column] = parse_float(raw.get(column))
            feature_values = tuple(source_values[column] for column in feature_columns)

            key = (series_id, bin_start)
            if key in seen:
                dup_label = (
                    f"camera {series_id}" if direction else f"camera {camera_id}"
                )
                raise BacktestError(
                    f"duplicate feature row for {dup_label} at {bin_start.isoformat()}"
                )
            seen.add(key)

            column_bin = parse_float(raw.get("bin_minutes"))
            if (
                column_bin is not None
                and column_bin > 0
                and float(column_bin).is_integer()
            ):
                column_bin_counts[int(column_bin)] += 1

            records.append(
                FeatureRecord(
                    camera_id=camera_id,
                    bin_start=bin_start,
                    saturation_index=saturation_index,
                    hour_of_day=hour_of_day,
                    day_of_week=day_of_week,
                    feature_values=feature_values,
                    direction=direction,
                    feature_map=feature_map,
                )
            )
            stats["accepted_rows"] += 1
            min_time = bin_start if min_time is None else min(min_time, bin_start)
            max_time = bin_start if max_time is None else max(max_time, bin_start)

    if not records:
        raise BacktestError(
            "no usable feature rows in "
            f"{path}; accepted={stats['accepted_rows']} "
            f"invalid={stats['invalid_rows']} "
            f"without_si={stats['rows_without_saturation_index']}"
        )

    records.sort(key=lambda item: (item.camera_id, item.bin_start))

    if column_bin_counts:
        bin_minutes, bin_minutes_source = column_bin_counts.most_common(1)[0][0], "column"
    else:
        bin_minutes, bin_minutes_source = _infer_bin_minutes(records)

    stats.update(
        {
            "camera_count": len({record.camera_id for record in records}),
            "bin_minutes": bin_minutes,
            "bin_minutes_source": bin_minutes_source,
            "time_range_utc": {
                "min": min_time.isoformat().replace("+00:00", "Z") if min_time else None,
                "max": max_time.isoformat().replace("+00:00", "Z") if max_time else None,
            },
        }
    )
    return records, stats


def chronological_split(
    records: Sequence[FeatureRecord], train_ratio: float = DEFAULT_TRAIN_RATIO
) -> ChronologicalSplit:
    """Split distinct bin timestamps in ascending order, without shuffling.

    The first ``round(n * train_ratio)`` unique timestamps form the training
    window. All rows sharing a timestamp land on the same side of the cutoff,
    so two cameras observed in the same cycle cannot leak across the split.
    """
    if not 0.0 < train_ratio < 1.0:
        raise BacktestError("train_ratio must be strictly between 0 and 1")
    timestamps = sorted({record.bin_start for record in records})
    if len(timestamps) < 2:
        raise BacktestError(
            "chronological split needs at least two distinct bin timestamps"
        )
    index = int(round(len(timestamps) * train_ratio))
    index = max(1, min(len(timestamps) - 1, index))
    cutoff = timestamps[index]
    return ChronologicalSplit(
        cutoff=cutoff,
        train_times=tuple(timestamps[:index]),
        test_times=tuple(timestamps[index:]),
    )


def build_saturation_index(
    records: Sequence[FeatureRecord],
) -> dict[tuple[str, datetime], float]:
    """Index saturation index by camera/direction and exact bin start."""
    index: dict[tuple[str, datetime], float] = {}
    for record in records:
        key = (record.series_id, record.bin_start)
        if key in index:
            dup_label = (
                f"camera {record.series_id}"
                if record.direction
                else f"camera {record.camera_id}"
            )
            raise BacktestError(
                f"duplicate saturation-index key for {dup_label} at {record.bin_start.isoformat()}"
            )
        index[key] = record.saturation_index
    return index


def build_evaluation_samples(
    records: Sequence[FeatureRecord],
    saturation_index: dict[tuple[str, datetime], float],
    split: ChronologicalSplit,
    horizon_minutes: int,
    feature_columns: Optional[Sequence[str]] = None,
) -> EvaluationSamples:
    """Align current rows with their same-camera future target.

    The target must exist at the exact ``bin_start + horizon`` bin. It is never
    interpolated or carried forward. Training samples whose target lands at or
    after the test cutoff are purged so the future value cannot leak into fit.
    """
    if horizon_minutes <= 0:
        raise BacktestError("horizon_minutes must be positive")

    delta = timedelta(minutes=horizon_minutes)
    train: list[EvaluationSample] = []
    train_all: list[EvaluationSample] = []
    test: list[EvaluationSample] = []
    stats = {
        "train_missing_target": 0,
        "train_missing_features": 0,
        "train_purged_boundary": 0,
        "test_missing_target": 0,
        "test_missing_features": 0,
    }

    for record in records:
        is_train = record.bin_start < split.cutoff
        side = "train" if is_train else "test"
        target_time = record.bin_start + delta
        # Boundary purging is decided from timestamps, not from whether the
        # target happens to exist. A missing future row must be counted as
        # purged when it would have crossed into the test window.
        if is_train and target_time >= split.cutoff:
            stats["train_purged_boundary"] += 1
            continue
        target = saturation_index.get((record.series_id, target_time))
        if target is None:
            stats[f"{side}_missing_target"] += 1
            continue
        sample = EvaluationSample(
            record=record,
            current=record.saturation_index,
            target=target,
            target_time=target_time,
        )
        is_complete = (
            record.is_complete_for(feature_columns)
            if feature_columns
            else record.features_complete
        )
        if is_train:
            train_all.append(sample)
            if is_complete:
                train.append(sample)
            else:
                stats["train_missing_features"] += 1
        elif is_complete:
            test.append(sample)
        else:
            stats["test_missing_features"] += 1

    train.sort(key=lambda item: (item.record.bin_start, item.record.camera_id))
    train_all.sort(key=lambda item: (item.record.bin_start, item.record.camera_id))
    test.sort(key=lambda item: (item.record.bin_start, item.record.camera_id))
    return EvaluationSamples(
        train=tuple(train),
        train_all=tuple(train_all),
        test=tuple(test),
        stats=stats,
    )


def direction(value: float, epsilon: float = DIRECTION_EPSILON) -> int:
    """Return -1, 0, or 1 for a change, treating numerical noise as flat."""
    if value > epsilon:
        return 1
    if value < -epsilon:
        return -1
    return 0


def compute_metrics(
    predictions: Sequence[float],
    currents: Sequence[float],
    targets: Sequence[float],
    epsilon: float = DIRECTION_EPSILON,
) -> Metrics:
    """Compute MAE, RMSE, bias, and directional accuracy."""
    if not (len(predictions) == len(currents) == len(targets)):
        raise BacktestError("predictions, currents, and targets must have equal length")
    if not predictions:
        raise BacktestError("cannot compute metrics over an empty prediction set")

    absolute_errors = 0.0
    squared_errors = 0.0
    signed_errors = 0.0
    directional_matches = 0

    for prediction, current, target in zip(predictions, currents, targets):
        error = prediction - target
        absolute_errors += abs(error)
        squared_errors += error * error
        signed_errors += error
        if direction(prediction - current, epsilon) == direction(target - current, epsilon):
            directional_matches += 1

    count = len(predictions)
    return Metrics(
        sample_count=count,
        mae=absolute_errors / count,
        rmse=math.sqrt(squared_errors / count),
        bias=signed_errors / count,
        directional_accuracy=directional_matches / count,
        directional_matches=directional_matches,
    )


def solve_linear_system(
    matrix: Sequence[Sequence[float]], vector: Sequence[float]
) -> list[float]:
    """Solve a dense linear system with partial-pivoting Gaussian elimination."""
    size = len(vector)
    if size == 0:
        raise BacktestError("cannot solve an empty linear system")
    if len(matrix) != size or any(len(row) != size for row in matrix):
        raise BacktestError("linear system matrix must be square and match the vector")
    if any(not math.isfinite(value) for value in vector):
        raise BacktestError("linear system vector contains non-finite values")

    augmented = []
    for row_index in range(size):
        row = list(matrix[row_index])
        if any(not math.isfinite(value) for value in row):
            raise BacktestError("linear system matrix contains non-finite values")
        augmented.append(row + [float(vector[row_index])])

    for column in range(size):
        pivot = max(range(column, size), key=lambda row: abs(augmented[row][column]))
        if abs(augmented[pivot][column]) < 1e-12:
            raise BacktestError("linear system is singular")
        if pivot != column:
            augmented[column], augmented[pivot] = augmented[pivot], augmented[column]

        pivot_value = augmented[column][column]
        for row in range(column + 1, size):
            factor = augmented[row][column] / pivot_value
            if factor == 0.0:
                continue
            for entry in range(column, size + 1):
                augmented[row][entry] -= factor * augmented[column][entry]

    solution = [0.0] * size
    for row in range(size - 1, -1, -1):
        total = augmented[row][size]
        for column in range(row + 1, size):
            total -= augmented[row][column] * solution[column]
        solution[row] = total / augmented[row][row]
    return solution


class RidgeRegression:
    """Ridge regression on standardized features with an unpenalized intercept."""

    def __init__(self, lambda_: float = DEFAULT_RIDGE_LAMBDA):
        if lambda_ <= 0.0 or not math.isfinite(lambda_):
            raise BacktestError("ridge lambda must be a positive finite number")
        self.lambda_ = lambda_
        self.feature_means: list[float] = []
        self.feature_stds: list[float] = []
        self.coefficients: list[float] = []
        self.intercept: float = 0.0
        self._fitted = False

    def fit(
        self, features: Sequence[Sequence[float]], targets: Sequence[float]
    ) -> "RidgeRegression":
        if not features:
            raise BacktestError("cannot fit ridge regression without features")
        if len(features) != len(targets):
            raise BacktestError("feature and target counts differ")
        width = len(features[0])
        if width == 0:
            raise BacktestError("feature rows are empty")
        if any(len(row) != width for row in features):
            raise BacktestError("feature rows have inconsistent width")

        count = len(features)
        self.feature_means = [0.0] * width
        for row in features:
            for column in range(width):
                value = row[column]
                if not math.isfinite(value):
                    raise BacktestError("features contain non-finite values")
                self.feature_means[column] += value
        for column in range(width):
            self.feature_means[column] /= count

        self.feature_stds = [0.0] * width
        for row in features:
            for column in range(width):
                difference = row[column] - self.feature_means[column]
                self.feature_stds[column] += difference * difference
        for column in range(width):
            variance = self.feature_stds[column] / count
            std = math.sqrt(variance)
            self.feature_stds[column] = std if std > 0.0 else 1.0

        standardized = [
            [
                (row[column] - self.feature_means[column]) / self.feature_stds[column]
                for column in range(width)
            ]
            for row in features
        ]

        dimension = width + 1
        normal_matrix = [[0.0] * dimension for _ in range(dimension)]
        normal_vector = [0.0] * dimension
        for row, target in zip(standardized, targets):
            if not math.isfinite(target):
                raise BacktestError("targets contain non-finite values")
            augmented = row + [1.0]
            for left in range(dimension):
                normal_vector[left] += augmented[left] * target
                for right in range(left, dimension):
                    normal_matrix[left][right] += augmented[left] * augmented[right]

        for left in range(dimension):
            for right in range(left):
                normal_matrix[left][right] = normal_matrix[right][left]
        for column in range(width):
            normal_matrix[column][column] += self.lambda_

        solution = solve_linear_system(normal_matrix, normal_vector)
        self.coefficients = solution[:width]
        self.intercept = solution[width]
        self._fitted = True
        return self

    def predict_row(self, features: Sequence[float]) -> float:
        if not self._fitted:
            raise BacktestError("ridge regression is not fitted")
        if len(features) != len(self.feature_means):
            raise BacktestError("feature width does not match the fitted model")
        total = self.intercept
        for value, mean, std, coefficient in zip(
            features, self.feature_means, self.feature_stds, self.coefficients
        ):
            if not math.isfinite(value):
                raise BacktestError("features contain non-finite values")
            total += ((value - mean) / std) * coefficient
        return total

    def predict(self, features: Sequence[Sequence[float]]) -> list[float]:
        return [self.predict_row(row) for row in features]


class HistoricalMedianModel:
    """Median of training future targets by camera and target local hour.

    The bucket is the hour of the forecast target, not the hour of the current
    observation. At a 30-minute horizon a sample can cross an hour boundary, and
    keying on the current hour would score it against the wrong climatology.
    Only training targets are used, and the boundary purge guarantees those
    targets lie inside the training window.
    """

    def __init__(self) -> None:
        self.camera_hour_medians: dict[tuple[str, int], float] = {}
        self.camera_medians: dict[str, float] = {}
        self.global_median: float = 0.0
        self._fitted = False

    def fit(
        self,
        samples: Sequence[EvaluationSample],
        local_tz: ZoneInfo,
    ) -> "HistoricalMedianModel":
        if not samples:
            raise BacktestError(
                "cannot fit historical median without training samples"
            )
        camera_hour: dict[tuple[str, int], list[float]] = defaultdict(list)
        camera: dict[str, list[float]] = defaultdict(list)
        global_values: list[float] = []
        for sample in samples:
            target_hour = sample.target_time.astimezone(local_tz).hour
            camera_hour[(sample.record.camera_id, target_hour)].append(sample.target)
            camera[sample.record.camera_id].append(sample.target)
            global_values.append(sample.target)
        self.camera_hour_medians = {
            key: statistics.median(values) for key, values in camera_hour.items()
        }
        self.camera_medians = {
            key: statistics.median(values) for key, values in camera.items()
        }
        self.global_median = statistics.median(global_values)
        self._fitted = True
        return self

    def predict_one(self, camera_id: str, target_hour: int) -> float:
        if not self._fitted:
            raise BacktestError("historical median model is not fitted")
        key = (camera_id, target_hour)
        if key in self.camera_hour_medians:
            return self.camera_hour_medians[key]
        if camera_id in self.camera_medians:
            return self.camera_medians[camera_id]
        return self.global_median


def _clamp_prediction(value: float) -> float:
    return value if value > PREDICTION_FLOOR else PREDICTION_FLOOR


def evaluate_horizon(
    records: Sequence[FeatureRecord],
    saturation_index: dict[tuple[str, datetime], float],
    split: ChronologicalSplit,
    horizon_minutes: int,
    config: BacktestConfig,
) -> dict[str, Any]:
    """Fit and score all models on one horizon over a common evaluation set."""
    samples = build_evaluation_samples(
        records,
        saturation_index,
        split,
        horizon_minutes,
        feature_columns=config.feature_columns,
    )
    if not samples.train:
        raise BacktestError(
            f"horizon {horizon_minutes}m has no usable training samples "
            f"({samples.stats})"
        )
    if not samples.test:
        raise BacktestError(
            f"horizon {horizon_minutes}m has no usable test samples "
            f"({samples.stats})"
        )

    try:
        local_tz = ZoneInfo(config.timezone)
    except Exception as exc:  # ZoneInfoNotFoundError varies with tzdata packaging
        raise BacktestError(f"invalid timezone {config.timezone!r}: {exc}") from exc
    # The median baseline does not need lag features, so it gets every valid
    # training target. The ridge candidate keeps the feature-complete subset.
    median_model = HistoricalMedianModel().fit(samples.train_all, local_tz)

    train_features = [
        sample.record.feature_vector_for(config.feature_columns)
        for sample in samples.train
    ]
    if config.target_mode == "delta":
        train_targets = [sample.target - sample.current for sample in samples.train]
    else:
        train_targets = [sample.target for sample in samples.train]
    ridge = RidgeRegression(config.ridge_lambda).fit(train_features, train_targets)

    predictions: dict[str, list[float]] = {
        "persistence": [],
        "historical_median": [],
        config.candidate_id: [],
    }
    currents: list[float] = []
    targets: list[float] = []

    for sample in samples.test:
        current = sample.current
        persistence = _clamp_prediction(current)
        target_hour = sample.target_time.astimezone(local_tz).hour
        median = _clamp_prediction(
            median_model.predict_one(sample.record.camera_id, target_hour)
        )
        raw_candidate = ridge.predict_row(
            sample.record.feature_vector_for(config.feature_columns)
        )
        if config.target_mode == "delta":
            candidate = _clamp_prediction(current + raw_candidate)
        else:
            candidate = _clamp_prediction(raw_candidate)

        predictions["persistence"].append(persistence)
        predictions["historical_median"].append(median)
        predictions[config.candidate_id].append(candidate)
        currents.append(current)
        targets.append(sample.target)

    metrics = {
        model_id: compute_metrics(
            values, currents, targets, epsilon=config.direction_epsilon
        )
        for model_id, values in predictions.items()
    }

    directional_metrics: dict[str, dict[str, Any]] = {}
    directions = sorted(
        {s.record.direction for s in samples.test if s.record.direction}
    )
    if directions:
        for d in directions:
            d_idx = [i for i, s in enumerate(samples.test) if s.record.direction == d]
            if d_idx:
                d_currents = [currents[i] for i in d_idx]
                d_targets = [targets[i] for i in d_idx]
                directional_metrics[d] = {}
                for m_id, m_preds in predictions.items():
                    d_preds = [m_preds[i] for i in d_idx]
                    directional_metrics[d][m_id] = compute_metrics(
                        d_preds, d_currents, d_targets, epsilon=config.direction_epsilon
                    ).to_dict()

    persistence_mae = metrics["persistence"].mae
    candidate_mae = metrics[config.candidate_id].mae
    improvement = persistence_mae - candidate_mae
    improvement_percent = (
        100.0 * improvement / persistence_mae if persistence_mae > 0.0 else None
    )

    gate = {
        "candidate_model": config.candidate_id,
        "persistence_mae": round(persistence_mae, 8),
        "candidate_mae": round(candidate_mae, 8),
        "mae_improvement": round(improvement, 8),
        "mae_improvement_percent": (
            round(improvement_percent, 4) if improvement_percent is not None else None
        ),
        "beats_persistence": candidate_mae < persistence_mae,
    }

    model_results: dict[str, Any] = {
        "persistence": {
            "label": MODEL_LABELS["persistence"],
            "metrics": metrics["persistence"].to_dict(),
            "parameters": {"rule": "y_hat(t+h) = saturation_index(t)"},
        },
        "historical_median": {
            "label": MODEL_LABELS["historical_median"],
            "metrics": metrics["historical_median"].to_dict(),
            "parameters": {
                "fallback_order": [
                    "camera_id + target_hour_of_day",
                    "camera_id",
                    "global",
                ],
                "target_hour_source": "target bin in the configured timezone",
                "camera_hour_groups": len(median_model.camera_hour_medians),
                "camera_groups": len(median_model.camera_medians),
                "global_median": round(median_model.global_median, 8),
            },
        },
        config.candidate_id: {
            "label": MODEL_LABELS[config.candidate_id],
            "metrics": metrics[config.candidate_id].to_dict(),
            "parameters": {
                "ridge_lambda": config.ridge_lambda,
                "target_mode": config.target_mode,
                "feature_columns": list(config.feature_columns),
                "standardized_coefficients": {
                    name: round(value, 8)
                    for name, value in zip(config.feature_columns, ridge.coefficients)
                },
                "intercept": round(ridge.intercept, 8),
                "means": [round(value, 8) for value in ridge.feature_means],
                "stds": [round(value, 8) for value in ridge.feature_stds],
                "coeffs": [round(value, 8) for value in ridge.coefficients],
            },
        },
    }

    test_cameras = {sample.record.camera_id for sample in samples.test}
    return {
        "horizon_minutes": horizon_minutes,
        "sample_accounting": {
            **samples.stats,
            "train_samples": len(samples.train),
            "train_median_samples": len(samples.train_all),
            "test_samples": len(samples.test),
        },
        "evaluation": {
            "test_samples": len(samples.test),
            "test_cameras": len(test_cameras),
        },
        "models": model_results,
        "directional_metrics": directional_metrics,
        "gate": gate,
    }


def build_verification(
    results: dict[str, Any], candidate_id: str
) -> dict[str, Any]:
    """Apply the gate: candidate MAE must beat persistence on >= 1 horizon."""
    beats: list[int] = []
    for horizon_key in sorted(results, key=int):
        if results[horizon_key]["gate"]["beats_persistence"]:
            beats.append(int(horizon_key))
    return {
        "candidate_model": candidate_id,
        "criterion": (
            "candidate MAE < persistence MAE on at least one forecast horizon"
        ),
        "beats_persistence_horizons": beats,
        "passed": bool(beats),
    }


def run_backtest(
    records: Sequence[FeatureRecord],
    input_summary: dict[str, Any],
    config: BacktestConfig,
    data_note: str = "",
    command: str = "",
    generated_at: Optional[datetime] = None,
) -> dict[str, Any]:
    """Run the split, every horizon, and the verification gate."""
    config.validate()
    split = chronological_split(records, config.train_ratio)
    saturation_index = build_saturation_index(records)

    results: dict[str, Any] = {}
    for horizon in config.horizons_minutes:
        results[str(horizon)] = evaluate_horizon(
            records, saturation_index, split, horizon, config
        )

    verification = build_verification(results, config.candidate_id)
    train_row_count = sum(1 for record in records if record.bin_start < split.cutoff)
    test_row_count = len(records) - train_row_count
    generated = generated_at or datetime.now(timezone.utc)

    return {
        "schema_version": BACKTEST_SCHEMA_VERSION,
        "script_version": SCRIPT_VERSION,
        "generated_at_utc": generated.astimezone(timezone.utc)
        .isoformat()
        .replace("+00:00", "Z"),
        "command": command,
        "data_provenance": {
            "input": str(input_summary.get("path", "")),
            "sha256": input_summary.get("sha256"),
            "note": data_note.strip(),
        },
        "input_summary": input_summary,
        "config": config.to_dict(),
        "split": {
            "strategy": "global_chronological_by_bin_start_utc",
            "train_ratio": config.train_ratio,
            "cutoff_utc": split.cutoff.isoformat().replace("+00:00", "Z"),
            "train_timestamp_count": len(split.train_times),
            "test_timestamp_count": len(split.test_times),
            "train_row_count": train_row_count,
            "test_row_count": test_row_count,
            "no_shuffle": True,
        },
        "results": results,
        "verification": verification,
    }


def _format_float(value: Optional[float], digits: int = 4) -> str:
    if value is None:
        return "n/a"
    return f"{value:.{digits}f}"


def _format_percent(value: Optional[float]) -> str:
    if value is None:
        return "n/a"
    return f"{value * 100.0:.1f}%"


def render_markdown(result: dict[str, Any]) -> str:
    """Render the machine-produced Markdown report."""
    config = result["config"]
    summary = result["input_summary"]
    split = result["split"]
    results = result["results"]
    verification = result["verification"]
    candidate_id = verification["candidate_model"]
    provenance = result.get("data_provenance", {})

    lines: list[str] = []
    lines.append("# Short-Horizon Congestion Forecast Prototype v1")
    lines.append("")
    generator = result.get("generator") or f"`tools/forecast/backtest.py` (script version `{result.get('script_version', SCRIPT_VERSION)}`)"
    lines.append(
        "Backtest report for short-horizon congestion forecasting. Generated by "
        f"{generator} at `{result['generated_at_utc']}`."
    )
    lines.append("")
    lines.append(
        "Feature store contract: `docs/reports/forecast_feature_store_v1.md`."
    )
    lines.append("")

    note = " ".join(str(provenance.get("note") or "").split())
    if note:
        lines.append(f"> Data note: {note}")
        lines.append("")

    lines.append("## 1. Data provenance")
    lines.append("")
    lines.append("| Field | Value |")
    lines.append("| --- | --- |")
    lines.append(f"| Input CSV | `{provenance.get('input', '')}` |")
    lines.append(f"| SHA-256 | `{provenance.get('sha256', '')}` |")
    lines.append(
        f"| Rows (accepted / total) | {summary['accepted_rows']} / {summary['total_rows']} |"
    )
    lines.append(
        f"| Invalid rows / rows without saturation index | "
        f"{summary['invalid_rows']} / {summary['rows_without_saturation_index']} |"
    )
    lines.append(f"| Cameras | {summary['camera_count']} |")
    lines.append(
        f"| Bin width | {summary['bin_minutes']} minutes "
        f"(source: {summary['bin_minutes_source']}) |"
    )
    time_range = summary.get("time_range_utc") or {}
    lines.append(
        f"| Time range (UTC) | {time_range.get('min')} to {time_range.get('max')} |"
    )
    lines.append("")

    lines.append("## 2. Method")
    lines.append("")
    lines.append(
        f"- Chronological split: {config['train_ratio']:.0%} train / "
        f"{1.0 - config['train_ratio']:.0%} test over distinct `bin_start_utc` "
        "timestamps. Rows are never shuffled."
    )
    lines.append(
        f"- Cutoff: `{split['cutoff_utc']}` (first test bin); "
        f"{split['train_timestamp_count']} train timestamps, "
        f"{split['test_timestamp_count']} test timestamps."
    )
    lines.append(
        "- Training samples whose target lands at or after the cutoff are purged "
        "per horizon, so a future value cannot leak across the split."
    )
    lines.append(
        f"- Horizons: {', '.join(str(value) for value in config['horizons_minutes'])} "
        "minutes. Targets must exist at the exact future bin for the same camera; "
        "missing targets and incomplete lag features are excluded, never imputed."
    )
    lines.append(
        f"- Candidate: ridge regression with L2 penalty `lambda={config['ridge_lambda']}`, "
        f"target mode `{config['target_mode']}`, features "
        f"{', '.join(f'`{column}`' for column in config['feature_columns'])}."
    )
    lines.append(
        "- Persistence predicts the current saturation index; the historical median "
        "uses the training median of future targets by camera and target hour, "
        "then camera, then global."
    )
    lines.append(
        f"- Calendar columns are validated against `bin_start_utc` in "
        f"`{config['timezone']}`; a mismatch fails the run instead of training on "
        "the wrong seasonality."
    )
    lines.append(
        f"- Directional accuracy compares `sign(prediction - current)` with "
        f"`sign(actual - current)` using epsilon `{config['direction_epsilon']}`. "
        "Persistence predicts a zero change, so it only receives credit for "
        "genuinely flat actuals."
    )
    lines.append(
        f"- Predictions are floored at {config['prediction_floor']} because saturation "
        "index is non-negative."
    )
    lines.append("")

    lines.append("## 3. Results")
    lines.append("")
    lines.append("| Horizon | Model | MAE | RMSE | Directional accuracy | Samples |")
    lines.append("| --- | --- | --- | --- | --- | --- |")
    order = ["persistence", "historical_median", candidate_id]
    for horizon_key in sorted(results, key=int):
        models = results[horizon_key]["models"]
        for model_id in order:
            model = models[model_id]
            metrics = model["metrics"]
            lines.append(
                f"| {horizon_key} min | {model['label']} | "
                f"{_format_float(metrics['mae'])} | "
                f"{_format_float(metrics['rmse'])} | "
                f"{_format_percent(metrics['directional_accuracy'])} | "
                f"{metrics['sample_count']} |"
            )
    lines.append("")

    lines.append("## 4. Verification gate")
    lines.append("")
    lines.append(f"Criterion: {verification['criterion']}.")
    lines.append("")
    lines.append(
        "| Horizon | Persistence MAE | Candidate MAE | Improvement | Beats persistence |"
    )
    lines.append("| --- | --- | --- | --- | --- |")
    for horizon_key in sorted(results, key=int):
        gate = results[horizon_key]["gate"]
        percent = gate["mae_improvement_percent"]
        percent_text = f"{percent:+.1f}%" if percent is not None else "n/a"
        lines.append(
            f"| {horizon_key} min | {_format_float(gate['persistence_mae'])} | "
            f"{_format_float(gate['candidate_mae'])} | "
            f"{gate['mae_improvement']:+.4f} ({percent_text}) | "
            f"{'yes' if gate['beats_persistence'] else 'no'} |"
        )
    lines.append("")
    if verification["passed"]:
        horizons = ", ".join(
            f"{value} min" for value in verification["beats_persistence_horizons"]
        )
        lines.append(f"Gate result: PASSED. The candidate beats persistence at {horizons}.")
    else:
        lines.append(
            "Gate result: FAILED. The candidate does not beat persistence on any "
            "horizon. Do not expose a forecast endpoint or UI (ADR-011 D7)."
        )
    lines.append("")

    lines.append("## 5. Sample accounting")
    lines.append("")
    lines.append(
        "| Horizon | Train (ridge) | Train (median) | Purged at boundary | Test | "
        "Missing target (train/test) | Missing features (train/test) |"
    )
    lines.append("| --- | --- | --- | --- | --- | --- | --- |")
    for horizon_key in sorted(results, key=int):
        accounting = results[horizon_key]["sample_accounting"]
        lines.append(
            f"| {horizon_key} min | {accounting['train_samples']} | "
            f"{accounting['train_median_samples']} | "
            f"{accounting['train_purged_boundary']} | {accounting['test_samples']} | "
            f"{accounting['train_missing_target']} / {accounting['test_missing_target']} | "
            f"{accounting['train_missing_features']} / "
            f"{accounting['test_missing_features']} |"
        )
    lines.append("")

    lines.append("## 6. Caveats")
    lines.append("")
    lines.append(
        "- The v1 feature store does not carry rain columns, so rain is not part of "
        "this model despite being listed as a possible signal in Issue #302."
    )
    lines.append(
        "- Ridge regression is linear in the supplied features. Non-linear temporal "
        "structure and rare congestion events may remain unexplained."
    )
    lines.append(
        "- A passing gate is necessary but not sufficient for serving: the internal "
        "endpoint still needs its own staleness guard and live verification."
    )
    lines.append(
        "- All models are scored on the same feature-complete test rows. The "
        "historical-median baseline may be fitted on more training rows because it "
        "does not need lag features; the ridge candidate and persistence do not."
    )
    lines.append("")

    lines.append("## 7. Reproduce")
    lines.append("")
    lines.append("```bash")
    lines.append(
        result.get("command")
        or "python3 tools/forecast/backtest.py --input <features.csv>"
    )
    lines.append("```")
    lines.append("")
    return "\n".join(lines)


def _temporary_sibling(path: Path) -> Path:
    descriptor, name = tempfile.mkstemp(
        prefix=path.name + ".", suffix=".tmp", dir=str(path.parent)
    )
    os.close(descriptor)
    return Path(name)


def _write_outputs(
    json_path: Path,
    json_text: str,
    markdown_path: Path,
    markdown_text: str,
) -> None:
    """Write both outputs through unique sibling temp files.

    Rendering happens before this function is called, and both temp files are
    populated before either destination is replaced. A fixed ``<name>.tmp`` path
    would race a concurrent run; an identical JSON and Markdown destination is
    rejected by the caller before this point.
    """
    json_path.parent.mkdir(parents=True, exist_ok=True)
    markdown_path.parent.mkdir(parents=True, exist_ok=True)
    json_temporary = _temporary_sibling(json_path)
    markdown_temporary = _temporary_sibling(markdown_path)
    try:
        json_temporary.write_text(json_text, encoding="utf-8")
        markdown_temporary.write_text(markdown_text, encoding="utf-8")
        json_temporary.replace(json_path)
        markdown_temporary.replace(markdown_path)
    finally:
        for temporary in (json_temporary, markdown_temporary):
            try:
                temporary.unlink()
            except FileNotFoundError:
                pass


def run_ablation(
    records: Sequence[FeatureRecord],
    input_summary: dict[str, Any],
    train_ratio: float = DEFAULT_TRAIN_RATIO,
    horizons: Sequence[int] = DEFAULT_HORIZONS_MINUTES,
    ridge_lambda: float = DEFAULT_RIDGE_LAMBDA,
    target_mode: str = DEFAULT_TARGET_MODE,
    timezone_name: str = DEFAULT_TIMEZONE,
    data_note: str = "",
    command: str = "",
    generated_at: Optional[datetime] = None,
) -> dict[str, Any]:
    """Execute 3-way ablation: Variant A (lags), Variant B (+weather), Variant C (+directional)."""
    split = chronological_split(records, train_ratio)
    saturation_index = build_saturation_index(records)

    cfg_a = BacktestConfig(
        train_ratio=train_ratio,
        horizons_minutes=horizons,
        feature_columns=VARIANT_A_COLUMNS,
        ridge_lambda=ridge_lambda,
        target_mode=target_mode,
        timezone=timezone_name,
    )
    cfg_b = BacktestConfig(
        train_ratio=train_ratio,
        horizons_minutes=horizons,
        feature_columns=VARIANT_B_COLUMNS,
        ridge_lambda=ridge_lambda,
        target_mode=target_mode,
        timezone=timezone_name,
    )
    cfg_c = BacktestConfig(
        train_ratio=train_ratio,
        horizons_minutes=horizons,
        feature_columns=VARIANT_C_COLUMNS,
        ridge_lambda=ridge_lambda,
        target_mode=target_mode,
        timezone=timezone_name,
    )

    results_a: dict[str, Any] = {}
    results_b: dict[str, Any] = {}
    results_c: dict[str, Any] = {}
    horizon_comparisons: dict[str, Any] = {}

    for h in horizons:
        res_a = evaluate_horizon(records, saturation_index, split, h, cfg_a)
        res_b = evaluate_horizon(records, saturation_index, split, h, cfg_b)
        res_c = evaluate_horizon(records, saturation_index, split, h, cfg_c)
        results_a[str(h)] = res_a
        results_b[str(h)] = res_b
        results_c[str(h)] = res_c

        cand_id = cfg_a.candidate_id
        p_mae = res_a["models"]["persistence"]["metrics"]["mae"]
        p_rmse = res_a["models"]["persistence"]["metrics"]["rmse"]
        p_da = res_a["models"]["persistence"]["metrics"]["directional_accuracy"]

        a_m = res_a["models"][cand_id]["metrics"]
        b_m = res_b["models"][cand_id]["metrics"]
        c_m = res_c["models"][cand_id]["metrics"]

        delta_b_vs_a_abs = a_m["mae"] - b_m["mae"]
        delta_b_vs_a_pct = (
            (delta_b_vs_a_abs / a_m["mae"] * 100.0) if a_m["mae"] > 0 else 0.0
        )

        delta_c_vs_b_abs = b_m["mae"] - c_m["mae"]
        delta_c_vs_b_pct = (
            (delta_c_vs_b_abs / b_m["mae"] * 100.0) if b_m["mae"] > 0 else 0.0
        )

        horizon_comparisons[str(h)] = {
            "horizon_minutes": h,
            "persistence": {
                "mae": p_mae,
                "rmse": p_rmse,
                "directional_accuracy": p_da,
            },
            "variant_a": {
                "mae": a_m["mae"],
                "rmse": a_m["rmse"],
                "directional_accuracy": a_m["directional_accuracy"],
            },
            "variant_b": {
                "mae": b_m["mae"],
                "rmse": b_m["rmse"],
                "directional_accuracy": b_m["directional_accuracy"],
                "delta_b_vs_a_abs": round(delta_b_vs_a_abs, 8),
                "delta_b_vs_a_pct": round(delta_b_vs_a_pct, 4),
            },
            "variant_c": {
                "mae": c_m["mae"],
                "rmse": c_m["rmse"],
                "directional_accuracy": c_m["directional_accuracy"],
                "delta_c_vs_b_abs": round(delta_c_vs_b_abs, 8),
                "delta_c_vs_b_pct": round(delta_c_vs_b_pct, 4),
            },
            "sample_counts": res_b["sample_accounting"],
        }

    horizon_checks: dict[str, Any] = {}
    for h_str, comp in horizon_comparisons.items():
        delta_pct = comp["variant_b"]["delta_b_vs_a_pct"]
        horizon_checks[h_str] = {
            "delta_b_vs_a_pct": delta_pct,
            "improves_ge_2pct": delta_pct >= 2.0,
            "degrades_gt_5pct": delta_pct < -5.0,
        }

    improves_ge_2pct = any(
        chk["improves_ge_2pct"] for chk in horizon_checks.values()
    )
    degrades_gt_5pct = any(
        chk["degrades_gt_5pct"] for chk in horizon_checks.values()
    )
    gate_passed = improves_ge_2pct and not degrades_gt_5pct

    gate = {
        "rule": (
            "Variant B improves MAE by >= 2% on at least one horizon without "
            "degrading any horizon by > 5%"
        ),
        "passed": gate_passed,
        "improves_ge_2pct": improves_ge_2pct,
        "degrades_gt_5pct": degrades_gt_5pct,
        "horizon_checks": horizon_checks,
        "retained": gate_passed,
        "decision": (
            "PASSED: Weather features retained (+2% MAE improvement achieved without >5% regression)"
            if gate_passed
            else "NULL_RESULT: Weather features did not meet the +2% improvement threshold or degraded a horizon > 5%; retained null result, weather columns stay unused"
        ),
    }

    train_row_count = sum(1 for record in records if record.bin_start < split.cutoff)
    test_row_count = len(records) - train_row_count
    generated = generated_at or datetime.now(timezone.utc)

    return {
        "schema_version": BACKTEST_SCHEMA_VERSION,
        "script_version": SCRIPT_VERSION,
        "report_type": "3-way-ablation",
        "generated_at_utc": (
            generated.astimezone(timezone.utc).isoformat().replace("+00:00", "Z")
        ),
        "command": command,
        "data_provenance": {
            "input": str(input_summary.get("path", "")),
            "sha256": input_summary.get("sha256"),
            "note": data_note.strip(),
        },
        "input_summary": input_summary,
        "split": {
            "strategy": "global_chronological_by_bin_start_utc",
            "train_ratio": train_ratio,
            "cutoff_utc": split.cutoff.isoformat().replace("+00:00", "Z"),
            "train_timestamp_count": len(split.train_times),
            "test_timestamp_count": len(split.test_times),
            "train_row_count": train_row_count,
            "test_row_count": test_row_count,
        },
        "variants": {
            "variant_a": {"features": list(VARIANT_A_COLUMNS), "results": results_a},
            "variant_b": {"features": list(VARIANT_B_COLUMNS), "results": results_b},
            "variant_c": {"features": list(VARIANT_C_COLUMNS), "results": results_c},
        },
        "horizon_comparisons": horizon_comparisons,
        "gate": gate,
    }


def render_ablation_markdown(result: dict[str, Any]) -> str:
    """Render empirical 3-way forecast ablation report into GitHub-flavored Markdown."""
    lines = [
        "# Short-Horizon Congestion Forecast Ablation Report v2",
        "",
        (
            f"Ablation backtest report for Issue #376 (S10-T5) Part 2. "
            f"Generated by `tools/forecast/backtest.py` (script version `{result['script_version']}`) "
            f"at `{result['generated_at_utc']}`."
        ),
        "",
        "Feature store contract: `docs/reports/forecast_feature_store_v2.md`.",
        "",
        "## 1. Data provenance",
        "",
        "| Field | Value |",
        "| --- | --- |",
        f"| Input CSV | `{result['data_provenance']['input']}` |",
        f"| SHA-256 | `{result['data_provenance']['sha256']}` |",
        (
            f"| Rows (accepted / total) | {result['input_summary']['accepted_rows']} / "
            f"{result['input_summary']['total_rows']} |"
        ),
        (
            f"| Invalid rows / rows without saturation index | "
            f"{result['input_summary']['invalid_rows']} / "
            f"{result['input_summary']['rows_without_saturation_index']} |"
        ),
        f"| Cameras | {result['input_summary']['camera_count']} |",
        (
            f"| Bin width | {result['input_summary']['bin_minutes']} minutes "
            f"(source: {result['input_summary']['bin_minutes_source']}) |"
        ),
        (
            f"| Time range (UTC) | {result['input_summary']['time_range_utc']['min']} "
            f"to {result['input_summary']['time_range_utc']['max']} |"
        ),
    ]
    if result["data_provenance"]["note"]:
        lines.append(f"| Provenance note | {result['data_provenance']['note']} |")

    lines.extend(
        [
            "",
            "## 2. Method & 3-Way Ablation Protocol",
            "",
            "- Chronological split: 70% train / 30% test over distinct `bin_start_utc` timestamps. Rows are never shuffled.",
            f"- Cutoff: `{result['split']['cutoff_utc']}`; {result['split']['train_timestamp_count']} train timestamps, {result['split']['test_timestamp_count']} test timestamps.",
            "- Training samples whose target lands at or after the cutoff are purged per horizon to eliminate boundary leakage.",
            "- Three model variants evaluated on the identical test split:",
            "  - **Variant A (Baseline)**: Traffic lags only (`lag_5m`, `lag_10m`, `lag_15m`, `rolling_mean_15m`, `hour_of_day`, `day_of_week`).",
            "  - **Variant B (+Weather)**: Variant A + meteorological observations (`precip_mm_15m`, `precip_mm_60m`, `rain_flag`, `hours_since_rain`, `weather_code_class`, `radar_active`).",
            "  - **Variant C (+Directional)**: Variant B + directional roadway polygon signals (`peer_si`, `si_delta_ab`).",
            "- As-of join discipline: strict 30-minute age bound on weather data; missing observations remain null without forward fill (ADR-012 D3, Lesson 67).",
            "",
            "## 3. Results across Horizons",
            "",
            "| Horizon | Model | MAE | RMSE | Directional Accuracy | Delta vs A (%) |",
            "| --- | --- | --- | --- | --- | --- |",
        ]
    )

    comparisons = result["horizon_comparisons"]
    for h_str in sorted(comparisons.keys(), key=int):
        comp = comparisons[h_str]
        h = comp["horizon_minutes"]
        p = comp["persistence"]
        a = comp["variant_a"]
        b = comp["variant_b"]
        c = comp["variant_c"]

        lines.append(
            f"| {h} min | Persistence | {p['mae']:.4f} | {p['rmse']:.4f} | {p['directional_accuracy'] * 100.0:.1f}% | - |"
        )
        lines.append(
            f"| {h} min | Variant A (lags) | {a['mae']:.4f} | {a['rmse']:.4f} | {a['directional_accuracy'] * 100.0:.1f}% | baseline |"
        )
        delta_b_sign = "+" if b["delta_b_vs_a_pct"] >= 0 else ""
        lines.append(
            f"| {h} min | Variant B (+weather) | {b['mae']:.4f} | {b['rmse']:.4f} | {b['directional_accuracy'] * 100.0:.1f}% | {delta_b_sign}{b['delta_b_vs_a_pct']:.2f}% |"
        )
        delta_c_sign = "+" if c["delta_c_vs_b_pct"] >= 0 else ""
        lines.append(
            f"| {h} min | Variant C (+directional) | {c['mae']:.4f} | {c['rmse']:.4f} | {c['directional_accuracy'] * 100.0:.1f}% | {delta_c_sign}{c['delta_c_vs_b_pct']:.2f}% (vs B) |"
        )

    lines.extend(
        [
            "",
            "## 4. Directional Roadway Breakdown",
            "",
            "| Horizon | Direction | Variant A MAE | Variant B MAE | Variant C MAE | Best Variant |",
            "| --- | --- | --- | --- | --- | --- |",
        ]
    )

    for h_str in sorted(comparisons.keys(), key=int):
        h = int(h_str)
        res_a_dir = result["variants"]["variant_a"]["results"][h_str].get(
            "directional_metrics", {}
        )
        res_b_dir = result["variants"]["variant_b"]["results"][h_str].get(
            "directional_metrics", {}
        )
        res_c_dir = result["variants"]["variant_c"]["results"][h_str].get(
            "directional_metrics", {}
        )
        dirs = sorted(
            set(res_a_dir.keys()) | set(res_b_dir.keys()) | set(res_c_dir.keys())
        )
        if not dirs:
            dirs = ["(frame-wide)"]
        for d in dirs:
            mae_a = None
            for k, m in res_a_dir.get(d, {}).items():
                if k.startswith("ridge"):
                    mae_a = m.get("mae")
            mae_b = None
            for k, m in res_b_dir.get(d, {}).items():
                if k.startswith("ridge"):
                    mae_b = m.get("mae")
            mae_c = None
            for k, m in res_c_dir.get(d, {}).items():
                if k.startswith("ridge"):
                    mae_c = m.get("mae")
            if mae_a is not None and mae_b is not None and mae_c is not None:
                best = (
                    "Variant C"
                    if mae_c <= min(mae_a, mae_b)
                    else ("Variant B" if mae_b <= mae_a else "Variant A")
                )
                lines.append(
                    f"| {h} min | {d} | {mae_a:.4f} | {mae_b:.4f} | {mae_c:.4f} | {best} |"
                )
            else:
                lines.append(f"| {h} min | {d} | - | - | - | - |")

    gate = result["gate"]
    status_str = "PASSED" if gate["passed"] else "NULL_RESULT"
    lines.extend(
        [
            "",
            "## 5. Verification Gate & Ablation Protocol",
            "",
            "Criterion: Weather features are retained only if Variant B improves MAE by >= 2% on at least one horizon without degrading any horizon by > 5% (ADR-012 D3).",
            "",
            "| Horizon | Variant A MAE | Variant B MAE | Delta MAE | Delta % | Meets >= 2% Target | No Regression > 5% |",
            "| --- | --- | --- | --- | --- | --- | --- |",
        ]
    )

    for h_str in sorted(comparisons.keys(), key=int):
        comp = comparisons[h_str]
        h = comp["horizon_minutes"]
        chk = gate["horizon_checks"][h_str]
        d_pct = chk["delta_b_vs_a_pct"]
        d_abs = comp["variant_b"]["delta_b_vs_a_abs"]
        m2 = "yes" if chk["improves_ge_2pct"] else "no"
        no_reg = "yes" if not chk["degrades_gt_5pct"] else "FAILED"
        sign = "+" if d_pct >= 0 else ""
        lines.append(
            f"| {h} min | {comp['variant_a']['mae']:.4f} | {comp['variant_b']['mae']:.4f} | {sign}{d_abs:.4f} | {sign}{d_pct:.2f}% | {m2} | {no_reg} |"
        )

    lines.extend(
        [
            "",
            f"**Gate Result**: {status_str}.",
            f"**Decision**: {gate['decision']}.",
            "",
            "## 6. Sample Accounting",
            "",
            "| Horizon | Train Samples | Test Samples | Boundary Purged | Missing Target (Train/Test) | Missing Features (Train/Test) |",
            "| --- | --- | --- | --- | --- | --- |",
        ]
    )

    for h_str in sorted(comparisons.keys(), key=int):
        h = int(h_str)
        ac = comparisons[h_str]["sample_counts"]
        lines.append(
            f"| {h} min | {ac['train_samples']} | {ac['test_samples']} | "
            f"{ac['train_purged_boundary']} | {ac['train_missing_target']} / {ac['test_missing_target']} | "
            f"{ac['train_missing_features']} / {ac['test_missing_features']} |"
        )

    lines.extend(
        [
            "",
            "## 7. Reproduce",
            "",
            "```bash",
            (
                result.get("command")
                or "python3 tools/forecast/backtest.py --input <features.csv> --ablation"
            ),
            "```",
            "",
        ]
    )
    return "\n".join(lines)


def _parse_horizons(raw: str) -> tuple[int, ...]:
    values: list[int] = []
    for part in raw.split(","):
        part = part.strip()
        if not part:
            continue
        try:
            horizon = int(part)
        except ValueError as exc:
            raise argparse.ArgumentTypeError(f"invalid horizon: {part!r}") from exc
        if horizon <= 0:
            raise argparse.ArgumentTypeError("horizons must be positive")
        values.append(horizon)
    if not values:
        raise argparse.ArgumentTypeError("at least one horizon is required")
    return tuple(sorted(set(values)))


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description=(
            "Backtest persistence, historical median, and ridge regression for "
            "the 10/20/30-minute congestion forecast prototype (Issue #302)."
        )
    )
    parser.add_argument(
        "--input",
        type=Path,
        required=True,
        help="Feature CSV produced by extract_features.py",
    )
    parser.add_argument(
        "--json-out",
        type=Path,
        default=DEFAULT_JSON_OUT,
        help=f"JSON results path (default: {DEFAULT_JSON_OUT})",
    )
    parser.add_argument(
        "--markdown-out",
        type=Path,
        default=DEFAULT_MARKDOWN_OUT,
        help=f"Markdown report path (default: {DEFAULT_MARKDOWN_OUT})",
    )
    parser.add_argument(
        "--train-ratio",
        type=float,
        default=DEFAULT_TRAIN_RATIO,
        help=f"Chronological train fraction (default: {DEFAULT_TRAIN_RATIO})",
    )
    parser.add_argument(
        "--horizons",
        default=",".join(str(value) for value in DEFAULT_HORIZONS_MINUTES),
        help="Comma-separated horizons in minutes (default: 10,20,30)",
    )
    parser.add_argument(
        "--ridge-lambda",
        type=float,
        default=DEFAULT_RIDGE_LAMBDA,
        help=f"L2 penalty for the ridge model (default: {DEFAULT_RIDGE_LAMBDA})",
    )
    parser.add_argument(
        "--target-mode",
        choices=("delta", "level"),
        default=DEFAULT_TARGET_MODE,
        help="Predict the future level or the delta from the current value",
    )
    parser.add_argument(
        "--timezone",
        default=DEFAULT_TIMEZONE,
        help=(
            "IANA timezone for calendar features and validation; it must match "
            "the extractor when hour_of_day/day_of_week are present "
            f"(default: {DEFAULT_TIMEZONE})"
        ),
    )
    parser.add_argument(
        "--ablation",
        action="store_true",
        help="Run 3-way ablation comparison: Variant A (lags), Variant B (+weather), Variant C (+directional)",
    )
    parser.add_argument(
        "--ablation-markdown-out",
        type=Path,
        default=DEFAULT_ABLATION_MARKDOWN_OUT,
        help=f"Ablation Markdown report path (default: {DEFAULT_ABLATION_MARKDOWN_OUT})",
    )
    parser.add_argument(
        "--ablation-json-out",
        type=Path,
        default=DEFAULT_ABLATION_JSON_OUT,
        help=f"Ablation JSON report path (default: {DEFAULT_ABLATION_JSON_OUT})",
    )
    parser.add_argument(
        "--variant",
        choices=("A", "B", "C", "all"),
        default=None,
        help="Feature variant to evaluate: A (lags), B (+weather), C (+directional), or all (ablation)",
    )
    parser.add_argument(
        "--data-note",
        default="",
        help="Optional provenance note embedded in the JSON and Markdown report",
    )
    parser.add_argument(
        "--log-level",
        choices=("DEBUG", "INFO", "WARNING", "ERROR"),
        default="INFO",
        help="Log verbosity (default: INFO)",
    )
    return parser


def run(argv: Optional[Sequence[str]] = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)

    logging.basicConfig(
        level=getattr(logging, args.log_level),
        format="%(levelname)s %(message)s",
        stream=sys.stderr,
    )

    try:
        horizons = _parse_horizons(args.horizons)
    except argparse.ArgumentTypeError as exc:
        LOG.error("%s", exc)
        return EXIT_CONFIG_ERROR

    config = BacktestConfig(
        train_ratio=args.train_ratio,
        horizons_minutes=horizons,
        target_mode=args.target_mode,
        ridge_lambda=args.ridge_lambda,
        timezone=args.timezone,
    )
    try:
        config.validate()
    except BacktestError as exc:
        LOG.error("%s", exc)
        return EXIT_CONFIG_ERROR

    if args.json_out.resolve() == args.markdown_out.resolve():
        LOG.error("--json-out and --markdown-out must be different files")
        return EXIT_CONFIG_ERROR

    raw_argv = list(argv) if argv is not None else sys.argv[1:]
    command = "python3 tools/forecast/backtest.py " + shlex.join(raw_argv)

    if args.ablation or args.variant == "all":
        if args.ablation_json_out.resolve() == args.ablation_markdown_out.resolve():
            LOG.error(
                "--ablation-json-out and --ablation-markdown-out must be different files"
            )
            return EXIT_CONFIG_ERROR

        try:
            records, stats = load_feature_records(
                args.input,
                feature_columns=ALL_ABLATION_COLUMNS,
                timezone_name=config.timezone,
            )
        except BacktestError as exc:
            LOG.error("%s", exc)
            return EXIT_DATA_ERROR

        input_summary = {"path": str(args.input), **stats}
        try:
            ablation_result = run_ablation(
                records=records,
                input_summary=input_summary,
                train_ratio=args.train_ratio,
                horizons=horizons,
                ridge_lambda=args.ridge_lambda,
                target_mode=args.target_mode,
                timezone_name=args.timezone,
                data_note=args.data_note,
                command=command.strip(),
            )
        except BacktestError as exc:
            LOG.error("%s", exc)
            return EXIT_CONFIG_ERROR

        md_text = render_ablation_markdown(ablation_result)
        json_text = json.dumps(ablation_result, indent=2, sort_keys=True) + "\n"
        _write_outputs(
            args.ablation_json_out,
            json_text,
            args.ablation_markdown_out,
            md_text,
        )

        LOG.info(
            "Ablation backtest complete; results written to %s and %s (Gate: %s)",
            args.ablation_json_out,
            args.ablation_markdown_out,
            ablation_result["gate"]["decision"],
        )
        return EXIT_OK

    if args.variant == "A":
        config.feature_columns = VARIANT_A_COLUMNS
    elif args.variant == "B":
        config.feature_columns = VARIANT_B_COLUMNS
    elif args.variant == "C":
        config.feature_columns = VARIANT_C_COLUMNS

    try:
        records, stats = load_feature_records(
            args.input,
            feature_columns=config.feature_columns,
            timezone_name=config.timezone,
        )
    except BacktestError as exc:
        LOG.error("%s", exc)
        return EXIT_DATA_ERROR

    input_summary = {"path": str(args.input), **stats}

    try:
        result = run_backtest(
            records,
            input_summary=input_summary,
            config=config,
            data_note=args.data_note,
            command=command.strip(),
        )
    except BacktestError as exc:
        LOG.error("%s", exc)
        return EXIT_DATA_ERROR

    # Render both documents before touching either destination, so a report
    # rendering failure cannot leave a fresh JSON beside a stale Markdown file.
    json_text = json.dumps(result, indent=2, sort_keys=True) + "\n"
    markdown_text = render_markdown(result)
    try:
        _write_outputs(args.json_out, json_text, args.markdown_out, markdown_text)
    except OSError as exc:
        LOG.error("cannot write backtest outputs: %s", exc)
        return EXIT_DATA_ERROR

    verification = result["verification"]
    if verification["passed"]:
        LOG.info(
            "wrote %s and %s; gate passed on horizon(s): %s",
            args.json_out,
            args.markdown_out,
            ", ".join(str(value) for value in verification["beats_persistence_horizons"]),
        )
        return EXIT_OK

    LOG.error(
        "gate failed: candidate did not beat persistence on any horizon; "
        "results written to %s and %s",
        args.json_out,
        args.markdown_out,
    )
    return EXIT_GATE_FAILED


def main() -> None:
    sys.exit(run())


if __name__ == "__main__":
    main()
