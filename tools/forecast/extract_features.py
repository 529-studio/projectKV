#!/usr/bin/env python3
"""
extract_features.py - Build the short-horizon congestion feature store.

Sprint 9 / Issue #302 (S9-T6) Part 1:
  * Reads the append-only hotset_verdict_history table written by go-server.
  * Backfills the same series from harvested / proof label manifests that carry
    PCU and saturation-index values in their notes.
  * Resamples observations into regular 5-minute or 10-minute bins and derives
    lag, rolling, and calendar features.

The output is a deterministic CSV plus a JSON metadata sidecar. It uses only
the Python standard library so the extractor runs on a laptop or dev box
without pandas or scikit-learn.

Adheres strictly to AGENTS.md:
  * Rule 1: zero emojis, English-only logs and comments.
  * Rule 2: timestamps, duplicates, and missing bins are handled explicitly
    instead of silently coercion.
  * Rule 8: no credentials; the SQLite database is opened read-only.

Example:
    python3 tools/forecast/extract_features.py \
        --db data/camera-view.db \
        --bin-minutes 5 \
        --out data/forecast/features_5m.csv
"""

from __future__ import annotations

import argparse
import bisect
import csv
import json
import logging
import math
import re
import sqlite3
import sys
from collections import Counter
from dataclasses import dataclass
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Any, Iterable, Optional, Sequence
from urllib.parse import quote
from zoneinfo import ZoneInfo

LOG = logging.getLogger("forecast.features")

SCRIPT_VERSION = "1.0.0"
FEATURE_SCHEMA_VERSION = 1

REPO_ROOT = Path(__file__).resolve().parent.parent.parent
DEFAULT_DB_PATH = REPO_ROOT / "data" / "camera-view.db"
DEFAULT_LABEL_ROOTS = (
    REPO_ROOT / "data" / "harvested" / "labels",
    REPO_ROOT / "data" / "proofs",
)
DEFAULT_BIN_MINUTES = 5
DEFAULT_LAG_MINUTES = (5, 10, 15)
DEFAULT_ROLLING_WINDOW_MINUTES = 15
DEFAULT_LAG_TOLERANCE_MINUTES = 5.0
DEFAULT_TIMEZONE = "Asia/Ho_Chi_Minh"

# Ranking is used only to break ties when several severities fall in one bin.
# UNKNOWN ranks below CLEAR so a genuinely observed label wins a tie.
SEVERITY_RANK = {
    "UNKNOWN": -1,
    "CLEAR": 0,
    "MODERATE": 1,
    "HEAVY": 2,
    "CONGESTED": 3,
}

# Go's time.Time.String() includes a monotonic suffix such as " m=+0.944".
_GO_MONOTONIC_SUFFIX = re.compile(r"\s+m=[+-][0-9.]+$")
# It also appends a numeric zone label after the numeric offset:
# "2026-10-11 17:35:13.525256 +0700 +07". The trailing token is not ISO 8601.
_GO_ZONE_LABEL_SUFFIX = re.compile(r"^(.*?[+-]\d{4})\s+[+-]\d{2}$")
_NUMERIC_TEXT = re.compile(r"^-?\d+(?:\.\d+)?$")
_PCU_IN_NOTES = re.compile(
    r"\bpcu(?:_total)?\s*[=:]\s*(-?[0-9]+(?:\.[0-9]+)?)", re.IGNORECASE
)
_SI_IN_NOTES = re.compile(
    r"(?:\bsi\b|saturation[_ ]index)\s*[=:]\s*(-?[0-9]+(?:\.[0-9]+)?)",
    re.IGNORECASE,
)
_DATE_ONLY = re.compile(r"^\d{4}-\d{2}-\d{2}$")

# SQLite's default CURRENT_TIMESTAMP and several historical text encodings do
# not parse with datetime.fromisoformat alone.
_TIMESTAMP_FORMATS = (
    "%Y-%m-%d %H:%M:%S.%f %z %Z",
    "%Y-%m-%d %H:%M:%S %z %Z",
    "%Y-%m-%d %H:%M:%S.%f %z",
    "%Y-%m-%d %H:%M:%S %z",
    "%Y-%m-%d %H:%M:%S.%f",
    "%Y-%m-%d %H:%M:%S",
    "%Y-%m-%d",
)


@dataclass(frozen=True)
class Observation:
    """One raw PCU/saturation-index sample for a camera."""

    camera_id: str
    observed_at: datetime
    pcu_total: Optional[float]
    saturation_index: Optional[float]
    severity: str
    confidence: Optional[float]
    source: str
    origin: str


@dataclass(frozen=True)
class FeatureRow:
    """One regular time bin ready for CSV serialization."""

    values: dict[str, Any]


def parse_timestamp(value: Any, default_tz: ZoneInfo) -> Optional[datetime]:
    """Parse a SQLite or label timestamp into an aware UTC datetime.

    Accepts RFC3339, Go's time.Time.String() form (including the monotonic
    suffix), SQLite CURRENT_TIMESTAMP, date-only strings, datetime objects, and
    Unix epoch seconds. Naive values are interpreted in default_tz because the
    Hot Set operates in Asia/Ho_Chi_Minh local time.
    """
    if value is None:
        return None

    if isinstance(value, datetime):
        parsed = value
    elif isinstance(value, (int, float)):
        if not math.isfinite(float(value)):
            return None
        parsed = datetime.fromtimestamp(float(value), tz=timezone.utc)
    else:
        text = str(value).strip()
        if not text:
            return None
        text = _GO_MONOTONIC_SUFFIX.sub("", text)
        if text.endswith(" UTC"):
            text = text[: -len(" UTC")] + "+00:00"
        zone_match = _GO_ZONE_LABEL_SUFFIX.match(text)
        if zone_match:
            text = zone_match.group(1)
        text = text.replace("Z", "+00:00")

        parsed = None
        if _NUMERIC_TEXT.match(text):
            parsed = datetime.fromtimestamp(float(text), tz=timezone.utc)
        else:
            try:
                parsed = datetime.fromisoformat(text)
            except ValueError:
                for layout in _TIMESTAMP_FORMATS:
                    try:
                        parsed = datetime.strptime(text, layout)
                        break
                    except ValueError:
                        continue
        if parsed is None:
            return None

    if parsed.tzinfo is None:
        parsed = parsed.replace(tzinfo=default_tz)
    return parsed.astimezone(timezone.utc)


def parse_bound(value: Optional[str], default_tz: ZoneInfo, upper: bool) -> Optional[datetime]:
    """Parse a --since/--until argument. Date-only upper bounds are exclusive."""
    if not value:
        return None
    text = value.strip()
    parsed = parse_timestamp(text, default_tz)
    if parsed is None:
        raise ValueError(f"invalid timestamp: {value!r}")
    if upper and _DATE_ONLY.match(text):
        # --until 2026-11-05 means "through the end of 2026-11-05" in local time.
        parsed += timedelta(days=1)
    return parsed


def floor_to_bin(observed_at: datetime, bin_minutes: int) -> datetime:
    """Floor an aware datetime to a regular UTC bin boundary."""
    if bin_minutes <= 0:
        raise ValueError("bin_minutes must be positive")
    size = bin_minutes * 60
    epoch = int(observed_at.astimezone(timezone.utc).timestamp())
    return datetime.fromtimestamp(epoch - (epoch % size), tz=timezone.utc)


def normalize_severity(value: Any) -> str:
    text = str(value or "").strip().upper()
    return text if text in SEVERITY_RANK else "UNKNOWN"


def _finite_float(value: Any) -> Optional[float]:
    if value is None or isinstance(value, bool):
        return None
    try:
        number = float(value)
    except (TypeError, ValueError):
        return None
    if not math.isfinite(number):
        return None
    return number


def _valid_pcu(value: Any) -> Optional[float]:
    number = _finite_float(value)
    return number if number is not None and number >= 0.0 else None


def _valid_si(value: Any) -> Optional[float]:
    number = _finite_float(value)
    return number if number is not None and number >= 0.0 else None


def _first_number(data: dict[str, Any], keys: Sequence[str]) -> Optional[float]:
    for key in keys:
        if key in data:
            number = _finite_float(data[key])
            if number is not None:
                return number
    return None


def parse_label_notes(notes: Any) -> tuple[Optional[float], Optional[float]]:
    """Extract (pcu_total, saturation_index) from free-text label notes.

    Harvested manifests currently encode these as "pcu=12.3; SI=0.2733" or
    "PCU=10.5, SI=0.70, base_cap=15.0". Labels without these values are skipped
    by the feature extractor because saturation index is the forecast target.
    """
    text = str(notes or "")
    if not text:
        return None, None
    pcu_match = _PCU_IN_NOTES.search(text)
    si_match = _SI_IN_NOTES.search(text)
    pcu = _valid_pcu(pcu_match.group(1)) if pcu_match else None
    si = _valid_si(si_match.group(1)) if si_match else None
    return pcu, si


def parse_label_record(
    record: Any, default_tz: ZoneInfo
) -> Optional[Observation]:
    """Convert one harvested/proof label object into an Observation.

    Required: camera_id and a parseable timestamp. Saturation index may come
    from a direct key or from the notes string. Records without a saturation
    index are skipped because they cannot train or evaluate the SI target.
    """
    if not isinstance(record, dict):
        return None

    labels = record.get("labels")
    if not isinstance(labels, dict):
        labels = {}

    camera_id = str(
        record.get("camera_id") or record.get("cameraId") or record.get("camera") or ""
    ).strip()
    if not camera_id:
        return None

    raw_time = (
        record.get("observed_at")
        or record.get("annotated_at")
        or record.get("captured_at")
        or record.get("timestamp")
    )
    observed_at = parse_timestamp(raw_time, default_tz)
    if observed_at is None:
        return None

    pcu = _first_number(record, ("pcu_total", "pcu", "PCU", "pcuTotal"))
    si = _first_number(record, ("saturation_index", "si", "SI", "saturationIndex"))
    if pcu is None or si is None:
        note_pcu, note_si = parse_label_notes(record.get("notes"))
        if pcu is None:
            pcu = note_pcu
        if si is None:
            si = note_si
    pcu = _valid_pcu(pcu)
    si = _valid_si(si)
    if si is None:
        return None

    severity = normalize_severity(
        record.get("severity") or labels.get("traffic") or record.get("traffic")
    )
    confidence = _finite_float(record.get("confidence"))
    source = str(record.get("source") or "label_backfill").strip() or "label_backfill"

    return Observation(
        camera_id=camera_id,
        observed_at=observed_at,
        pcu_total=pcu,
        saturation_index=si,
        severity=severity,
        confidence=confidence,
        source=source,
        origin="label",
    )


def _iter_label_files(root: Path) -> Iterable[Path]:
    if not root.exists():
        return ()
    if root.is_file():
        return (root,)
    return root.rglob("*.json")


def load_label_observations(
    roots: Sequence[Path], default_tz: ZoneInfo
) -> tuple[list[Observation], dict[str, int]]:
    """Load label observations from one or more manifest roots."""
    observations: list[Observation] = []
    stats = {"files": 0, "records": 0, "accepted": 0, "invalid": 0, "without_si": 0}
    seen_files: set[Path] = set()

    for root in roots:
        root = Path(root)
        if not root.exists():
            LOG.warning("label root does not exist, skipping: %s", root)
            continue
        for path in sorted(_iter_label_files(root)):
            resolved = path.resolve()
            if resolved in seen_files:
                continue
            seen_files.add(resolved)
            try:
                if path.stat().st_size > 8 * 1024 * 1024:
                    LOG.warning("label file too large, skipping: %s", path)
                    continue
                with path.open("r", encoding="utf-8") as handle:
                    payload = json.load(handle)
            except (OSError, json.JSONDecodeError) as exc:
                LOG.warning("cannot read label file %s: %s", path, exc)
                stats["invalid"] += 1
                continue

            stats["files"] += 1
            records = payload if isinstance(payload, list) else [payload]
            for record in records:
                stats["records"] += 1
                if not isinstance(record, dict):
                    stats["invalid"] += 1
                    continue
                observation = parse_label_record(record, default_tz)
                if observation is None:
                    stats["without_si"] += 1
                    continue
                observations.append(observation)
                stats["accepted"] += 1

    return observations, stats


def _open_sqlite_read_only(db_path: Path) -> Optional[sqlite3.Connection]:
    if not db_path.exists():
        LOG.warning("SQLite database does not exist, skipping: %s", db_path)
        return None
    uri = f"file:{quote(db_path.resolve().as_posix(), safe='/:')}?mode=ro"
    try:
        return sqlite3.connect(uri, uri=True, timeout=10.0)
    except sqlite3.Error as exc:
        LOG.warning("cannot open SQLite database %s: %s", db_path, exc)
        return None


def load_sqlite_observations(
    db_path: Path, default_tz: ZoneInfo
) -> tuple[list[Observation], dict[str, int]]:
    """Read the full hotset_verdict_history table in read-only mode.

    Filtering is done in Python rather than SQL because historical rows may
    contain the Go time.Time.String() encoding, which does not sort as text
    consistently. The table is small for the prototype; the read-only URI
    guarantees the extractor can never mutate production state.
    """
    observations: list[Observation] = []
    stats = {"rows": 0, "accepted": 0, "invalid": 0, "without_si": 0}
    connection = _open_sqlite_read_only(db_path)
    if connection is None:
        return observations, stats

    try:
        table = connection.execute(
            "SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?",
            ("hotset_verdict_history",),
        ).fetchone()
        if table is None:
            LOG.warning(
                "table hotset_verdict_history is missing; apply migration 0007 first"
            )
            return observations, stats

        cursor = connection.execute(
            """
            SELECT camera_id, pcu_total, saturation_index, severity, confidence,
                   source, observed_at
            FROM hotset_verdict_history
            ORDER BY camera_id, observed_at
            """
        )
        for row in cursor:
            stats["rows"] += 1
            camera_id = str(row[0] or "").strip()
            observed_at = parse_timestamp(row[6], default_tz)
            if not camera_id or observed_at is None:
                stats["invalid"] += 1
                continue
            pcu = _valid_pcu(row[1])
            si = _valid_si(row[2])
            if si is None:
                stats["without_si"] += 1
                continue
            observations.append(
                Observation(
                    camera_id=camera_id,
                    observed_at=observed_at,
                    pcu_total=pcu,
                    saturation_index=si,
                    severity=normalize_severity(row[3]),
                    confidence=_finite_float(row[4]),
                    source=str(row[5] or "").strip() or "sqlite",
                    origin="sqlite",
                )
            )
            stats["accepted"] += 1
    except sqlite3.Error as exc:
        LOG.warning("cannot read hotset_verdict_history: %s", exc)
    finally:
        connection.close()

    return observations, stats


def deduplicate_observations(
    sqlite_observations: Sequence[Observation],
    label_observations: Sequence[Observation],
) -> tuple[list[Observation], int]:
    """Drop label rows that duplicate a SQLite row at the same instant.

    SQLite is authoritative: it is the live pipeline rather than a historical
    manifest, so its values win when both sources describe the same camera and
    observation time.
    """
    seen: set[tuple[str, datetime]] = set()
    merged: list[Observation] = []
    for observation in sqlite_observations:
        key = (observation.camera_id, observation.observed_at)
        if key in seen:
            continue
        seen.add(key)
        merged.append(observation)

    dropped = 0
    for observation in label_observations:
        key = (observation.camera_id, observation.observed_at)
        if key in seen:
            dropped += 1
            continue
        seen.add(key)
        merged.append(observation)

    merged.sort(key=lambda item: (item.camera_id, item.observed_at))
    return merged, dropped


def filter_observations(
    observations: Sequence[Observation],
    since: Optional[datetime],
    until: Optional[datetime],
) -> list[Observation]:
    selected = []
    for observation in observations:
        if since is not None and observation.observed_at < since:
            continue
        if until is not None and observation.observed_at >= until:
            continue
        selected.append(observation)
    return selected


def _mode_severity(observations: Sequence[Observation]) -> str:
    counts = Counter(observation.severity for observation in observations)
    if not counts:
        return "UNKNOWN"
    best_count = max(counts.values())
    candidates = [severity for severity, count in counts.items() if count == best_count]
    return max(candidates, key=lambda severity: (SEVERITY_RANK.get(severity, -1), severity))


def _mean(values: Sequence[float]) -> Optional[float]:
    if not values:
        return None
    return sum(values) / len(values)


def _population_std(values: Sequence[float]) -> Optional[float]:
    if len(values) < 2:
        return None
    mean = sum(values) / len(values)
    variance = sum((value - mean) ** 2 for value in values) / len(values)
    return math.sqrt(variance)


def _as_of_saturation(
    observations: Sequence[Observation], timestamps: Sequence[float], target: datetime,
    tolerance_seconds: float,
) -> tuple[Optional[float], Optional[float]]:
    """Return the latest SI at or before target and its age in seconds.

    Observations without a saturation index are skipped so an older valid SI
    can still serve the lag. Age is measured against the selected observation;
    if the newest usable sample is older than the tolerance, the lag is null
    rather than silently stale.
    """
    if not observations:
        return None, None
    target_epoch = target.timestamp()
    index = bisect.bisect_right(timestamps, target_epoch) - 1
    while index >= 0:
        observation = observations[index]
        age = target_epoch - observation.observed_at.timestamp()
        if age > tolerance_seconds:
            return None, None
        if observation.saturation_index is not None:
            return observation.saturation_index, age
        index -= 1
    return None, None


def _rolling_values(
    observations: Sequence[Observation], timestamps: Sequence[float],
    start: datetime, end: datetime,
) -> list[float]:
    left = bisect.bisect_left(timestamps, start.timestamp())
    right = bisect.bisect_left(timestamps, end.timestamp())
    values = []
    for observation in observations[left:right]:
        if observation.saturation_index is not None:
            values.append(observation.saturation_index)
    return values


def build_feature_rows(
    observations: Sequence[Observation],
    bin_minutes: int,
    local_tz: ZoneInfo,
    lag_minutes: Sequence[int] = DEFAULT_LAG_MINUTES,
    rolling_window_minutes: int = DEFAULT_ROLLING_WINDOW_MINUTES,
    lag_tolerance_minutes: float = DEFAULT_LAG_TOLERANCE_MINUTES,
) -> tuple[list[FeatureRow], list[str], dict[str, int]]:
    """Aggregate observations into regular bins and build model features.

    Only bins that contain at least one saturation-index sample are emitted.
    Empty bins carry no training target and would otherwise inflate the
    dataset after long collection gaps. Bin boundaries themselves are always
    regular, and lag/rolling windows are computed from the raw observation
    timeline so a gap cannot silently shift a value into the wrong bucket.
    """
    if bin_minutes <= 0:
        raise ValueError("bin_minutes must be positive")
    if rolling_window_minutes <= 0:
        raise ValueError("rolling_window_minutes must be positive")
    if lag_tolerance_minutes < 0:
        raise ValueError("lag_tolerance_minutes cannot be negative")

    by_camera: dict[str, list[Observation]] = {}
    binned: dict[str, dict[datetime, list[Observation]]] = {}
    for observation in observations:
        by_camera.setdefault(observation.camera_id, []).append(observation)
        bin_start = floor_to_bin(observation.observed_at, bin_minutes)
        binned.setdefault(observation.camera_id, {}).setdefault(bin_start, []).append(observation)

    lag_minutes = sorted({int(value) for value in lag_minutes})
    fieldnames = [
        "camera_id",
        "bin_start_utc",
        "bin_start_local",
        "bin_minutes",
        "observation_count",
        "saturation_sample_count",
        "pcu_total",
        "saturation_index",
        "severity",
        "confidence",
        "source",
    ]
    for minutes in lag_minutes:
        fieldnames.append(f"lag_{minutes}m")
    for minutes in lag_minutes:
        fieldnames.append(f"lag_{minutes}m_age_seconds")
    fieldnames.extend(
        [
            f"rolling_mean_{rolling_window_minutes}m",
            f"rolling_std_{rolling_window_minutes}m",
            f"rolling_count_{rolling_window_minutes}m",
            "hour_of_day",
            "day_of_week",
            "is_weekend",
        ]
    )

    rows: list[FeatureRow] = []
    stats = {"bins_without_si": 0, "lags_missing": 0}
    tolerance_seconds = lag_tolerance_minutes * 60.0
    rolling_window = timedelta(minutes=rolling_window_minutes)

    for camera_id in sorted(binned):
        camera_observations = sorted(by_camera[camera_id], key=lambda item: item.observed_at)
        timestamps = [item.observed_at.timestamp() for item in camera_observations]
        for bin_start in sorted(binned[camera_id]):
            group = binned[camera_id][bin_start]
            si_values = [
                item.saturation_index
                for item in group
                if item.saturation_index is not None
            ]
            if not si_values:
                stats["bins_without_si"] += 1
                continue

            pcu_values = [item.pcu_total for item in group if item.pcu_total is not None]
            confidence_values = [
                item.confidence for item in group if item.confidence is not None
            ]
            sources = sorted({item.source for item in group if item.source})
            local_start = bin_start.astimezone(local_tz)

            values: dict[str, Any] = {
                "camera_id": camera_id,
                "bin_start_utc": bin_start.isoformat().replace("+00:00", "Z"),
                "bin_start_local": local_start.isoformat(),
                "bin_minutes": bin_minutes,
                "observation_count": len(group),
                "saturation_sample_count": len(si_values),
                "pcu_total": _mean(pcu_values),
                "saturation_index": _mean(si_values),
                "severity": _mode_severity(group),
                "confidence": _mean(confidence_values),
                "source": "|".join(sources),
                "hour_of_day": local_start.hour,
                "day_of_week": local_start.weekday(),
                "is_weekend": 1 if local_start.weekday() >= 5 else 0,
            }

            for minutes in lag_minutes:
                target = bin_start - timedelta(minutes=minutes)
                lag_value, age_seconds = _as_of_saturation(
                    camera_observations, timestamps, target, tolerance_seconds
                )
                if lag_value is None:
                    stats["lags_missing"] += 1
                values[f"lag_{minutes}m"] = lag_value
                values[f"lag_{minutes}m_age_seconds"] = age_seconds

            rolling = _rolling_values(
                camera_observations,
                timestamps,
                bin_start - rolling_window,
                bin_start,
            )
            values[f"rolling_mean_{rolling_window_minutes}m"] = _mean(rolling)
            values[f"rolling_std_{rolling_window_minutes}m"] = _population_std(rolling)
            values[f"rolling_count_{rolling_window_minutes}m"] = len(rolling)

            rows.append(FeatureRow(values=values))

    return rows, fieldnames, stats


def _format_csv_value(value: Any) -> Any:
    if value is None:
        return ""
    if isinstance(value, float):
        if not math.isfinite(value):
            return ""
        return f"{value:.6f}"
    return value


def write_csv(rows: Sequence[FeatureRow], fieldnames: Sequence[str], destination: Path) -> None:
    destination.parent.mkdir(parents=True, exist_ok=True)
    temporary = destination.with_suffix(destination.suffix + ".tmp")
    with temporary.open("w", encoding="utf-8", newline="") as handle:
        writer = csv.DictWriter(handle, fieldnames=fieldnames, extrasaction="raise")
        writer.writeheader()
        for row in rows:
            writer.writerow(
                {key: _format_csv_value(row.values.get(key)) for key in fieldnames}
            )
    temporary.replace(destination)


def write_metadata(
    destination: Path,
    fieldnames: Sequence[str],
    rows: Sequence[FeatureRow],
    args: argparse.Namespace,
    sqlite_stats: dict[str, int],
    label_stats: dict[str, int],
    feature_stats: dict[str, int],
    duplicate_labels: int,
    selected_observations: Sequence[Observation],
) -> None:
    cameras = sorted({row.values["camera_id"] for row in rows})
    bin_starts = [row.values["bin_start_utc"] for row in rows]
    metadata = {
        "schema_version": FEATURE_SCHEMA_VERSION,
        "script_version": SCRIPT_VERSION,
        "generated_at_utc": datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
        "bin_minutes": args.bin_minutes,
        "timezone": args.timezone,
        "lag_minutes": list(args.lag_minutes),
        "lag_tolerance_minutes": args.lag_tolerance_minutes,
        "rolling_window_minutes": args.rolling_window_minutes,
        "feature_columns": list(fieldnames),
        "row_count": len(rows),
        "camera_count": len(cameras),
        "cameras": cameras,
        "bin_start_min": min(bin_starts) if bin_starts else None,
        "bin_start_max": max(bin_starts) if bin_starts else None,
        "observation_counts": {
            "selected": len(selected_observations),
            "sqlite": sqlite_stats.get("accepted", 0),
            "label": label_stats.get("accepted", 0),
            "duplicate_labels_dropped": duplicate_labels,
        },
        "source_stats": {
            "sqlite": sqlite_stats,
            "label": label_stats,
            "features": feature_stats,
        },
    }
    destination.parent.mkdir(parents=True, exist_ok=True)
    temporary = destination.with_suffix(destination.suffix + ".tmp")
    with temporary.open("w", encoding="utf-8") as handle:
        json.dump(metadata, handle, indent=2, sort_keys=True)
        handle.write("\n")
    temporary.replace(destination)


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description=(
            "Extract regular-bin PCU/saturation-index features for the "
            "short-horizon congestion forecast prototype (Issue #302)."
        )
    )
    parser.add_argument(
        "--db",
        type=Path,
        default=DEFAULT_DB_PATH,
        help=f"SQLite database path (default: {DEFAULT_DB_PATH})",
    )
    parser.add_argument(
        "--labels-root",
        type=Path,
        action="append",
        default=None,
        help=(
            "Label manifest root to backfill from; repeatable. "
            "Defaults to data/harvested/labels and data/proofs."
        ),
    )
    parser.add_argument(
        "--no-labels",
        action="store_true",
        help="Read SQLite only and skip label backfill",
    )
    parser.add_argument(
        "--bin-minutes",
        type=int,
        choices=(5, 10),
        default=DEFAULT_BIN_MINUTES,
        help="Regular bin width in minutes (default: 5)",
    )
    parser.add_argument(
        "--timezone",
        default=DEFAULT_TIMEZONE,
        help=f"IANA timezone for calendar features (default: {DEFAULT_TIMEZONE})",
    )
    parser.add_argument(
        "--since",
        default=None,
        help="Inclusive lower bound (ISO 8601 or YYYY-MM-DD)",
    )
    parser.add_argument(
        "--until",
        default=None,
        help="Exclusive upper bound (ISO 8601 or YYYY-MM-DD)",
    )
    parser.add_argument(
        "--lag-minutes",
        default=",".join(str(value) for value in DEFAULT_LAG_MINUTES),
        help="Comma-separated lag offsets in minutes (default: 5,10,15)",
    )
    parser.add_argument(
        "--lag-tolerance-minutes",
        type=float,
        default=DEFAULT_LAG_TOLERANCE_MINUTES,
        help=(
            "Maximum age of the sample used for a lag feature "
            f"(default: {DEFAULT_LAG_TOLERANCE_MINUTES})"
        ),
    )
    parser.add_argument(
        "--rolling-window-minutes",
        type=int,
        default=DEFAULT_ROLLING_WINDOW_MINUTES,
        help=(
            "Trailing window for rolling statistics "
            f"(default: {DEFAULT_ROLLING_WINDOW_MINUTES})"
        ),
    )
    parser.add_argument(
        "--out",
        type=Path,
        default=None,
        help="CSV output path (default: data/forecast/features_<bin>m.csv)",
    )
    parser.add_argument(
        "--metadata-out",
        type=Path,
        default=None,
        help="JSON metadata sidecar path (default: <out>.meta.json)",
    )
    parser.add_argument(
        "--log-level",
        choices=("DEBUG", "INFO", "WARNING", "ERROR"),
        default="INFO",
        help="Log verbosity (default: INFO)",
    )
    return parser


def _parse_lag_minutes(raw: str) -> list[int]:
    values: list[int] = []
    for part in raw.split(","):
        part = part.strip()
        if not part:
            continue
        try:
            minutes = int(part)
        except ValueError as exc:
            raise argparse.ArgumentTypeError(f"invalid lag offset: {part!r}") from exc
        if minutes <= 0:
            raise argparse.ArgumentTypeError("lag offsets must be positive")
        values.append(minutes)
    if not values:
        raise argparse.ArgumentTypeError("at least one lag offset is required")
    return sorted(set(values))


def run(argv: Optional[Sequence[str]] = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)

    logging.basicConfig(
        level=getattr(logging, args.log_level),
        format="%(levelname)s %(message)s",
        stream=sys.stderr,
    )

    try:
        local_tz = ZoneInfo(args.timezone)
    except Exception as exc:  # ZoneInfoNotFoundError varies with tzdata packaging
        LOG.error("invalid timezone %s: %s", args.timezone, exc)
        return 1

    try:
        args.lag_minutes = _parse_lag_minutes(args.lag_minutes)
        since = parse_bound(args.since, local_tz, upper=False)
        until = parse_bound(args.until, local_tz, upper=True)
    except (ValueError, argparse.ArgumentTypeError) as exc:
        LOG.error("%s", exc)
        return 1
    if since is not None and until is not None and since >= until:
        LOG.error("--since must be strictly earlier than --until")
        return 1

    out_path = args.out
    if out_path is None:
        out_path = REPO_ROOT / "data" / "forecast" / f"features_{args.bin_minutes}m.csv"
    metadata_path = args.metadata_out
    if metadata_path is None:
        metadata_path = out_path.with_name(out_path.stem + ".meta.json")

    sqlite_observations, sqlite_stats = load_sqlite_observations(args.db, local_tz)
    LOG.info("SQLite observations: %d accepted from %d rows", sqlite_stats["accepted"], sqlite_stats["rows"])

    label_observations: list[Observation] = []
    label_stats = {"files": 0, "records": 0, "accepted": 0, "invalid": 0, "without_si": 0}
    if not args.no_labels:
        roots = args.labels_root or list(DEFAULT_LABEL_ROOTS)
        label_observations, label_stats = load_label_observations(roots, local_tz)
        LOG.info("label observations: %d accepted from %d files", label_stats["accepted"], label_stats["files"])

    merged, duplicate_labels = deduplicate_observations(
        sqlite_observations, label_observations
    )
    selected = filter_observations(merged, since, until)
    if duplicate_labels:
        LOG.info("dropped %d label rows duplicating SQLite observations", duplicate_labels)
    LOG.info("selected observations: %d", len(selected))

    rows, fieldnames, feature_stats = build_feature_rows(
        selected,
        bin_minutes=args.bin_minutes,
        local_tz=local_tz,
        lag_minutes=args.lag_minutes,
        rolling_window_minutes=args.rolling_window_minutes,
        lag_tolerance_minutes=args.lag_tolerance_minutes,
    )
    if not rows:
        LOG.error(
            "no feature rows produced; ensure migration 0007 has run and label "
            "manifests contain pcu/SI notes"
        )
        return 2

    write_csv(rows, fieldnames, out_path)
    write_metadata(
        metadata_path,
        fieldnames,
        rows,
        args,
        sqlite_stats,
        label_stats,
        feature_stats,
        duplicate_labels,
        selected,
    )
    LOG.info(
        "wrote %d rows across %d cameras to %s (metadata: %s)",
        len(rows),
        len({row.values["camera_id"] for row in rows}),
        out_path,
        metadata_path,
    )
    return 0


def main() -> None:
    sys.exit(run())


if __name__ == "__main__":
    main()
