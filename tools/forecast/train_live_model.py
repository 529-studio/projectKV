#!/usr/bin/env python3
"""
train_live_model.py - Automated Forecast Refit Pipeline & Live History Model Deployment.

Sprint 11 Wave 2 Task 4 (Issue #398):
  * Extracts authentic 5-minute binned traffic features from SQLite
    (`hotset_verdict_history` joined as-of with `weather_observations`).
  * Enforces train/serve series parity (frame-wide observations matching Go query).
  * Executes chronological train/test split (default 80/20) without shuffling.
  * Fits ridge regression on delta targets with standardized features and
    unpenalized intercept, alongside persistence and historical median baselines.
  * Evaluates multi-horizon forecasts (10m, 20m, 30m) and enforces the verification
    gate (candidate MAE must beat persistence MAE on at least one horizon).
  * Validates candidate artifact schema and parameters before atomic promotion.
  * Promotes candidate model weights and metadata atomically to the Go embedded
    artifact JSON (`services/go-server/internal/forecast/embedded/prototype_v1.json`).
  * Emits updated empirical backtest evaluation JSON and markdown reports.

Sprint 12 Task 1 (Issue #413):
  * Adds single-flight refit locking (fcntl.flock), durable scheduler state,
    row-delta gating against hotset_verdict_history, wall-clock or fixed-interval
    scheduling, and a supervised in-process daemon (--daemon).

Adheres strictly to AGENTS.md:
  * Rule 1: Zero emojis, 100% English logs, comments, and docstrings.
  * Rule 2: Root-cause sound solution, never fear effort.
  * Rule 8: Zero hardcoded credentials or mock strings. Opens DB read-only.
  * Rule 10: Strict evidence integrity. Derives and verifies authentic live history provenance.
"""

from __future__ import annotations

import argparse
import collections
import hashlib
import json
import logging
import math
import os
import re
import signal
import sqlite3
import sys
import tempfile
import threading
import time
from dataclasses import dataclass
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Any, Optional, Sequence
from urllib.parse import quote
from zoneinfo import ZoneInfo

try:
    import fcntl
except ImportError:  # pragma: no cover
    fcntl = None  # type: ignore[assignment]

# Ensure tools/forecast directory is in sys.path for direct script execution
CURRENT_DIR = Path(__file__).resolve().parent
REPO_ROOT = CURRENT_DIR.parent.parent
if str(CURRENT_DIR) not in sys.path:
    sys.path.insert(0, str(CURRENT_DIR))
if str(REPO_ROOT) not in sys.path:
    sys.path.insert(0, str(REPO_ROOT))

import extract_features
import backtest

LOG = logging.getLogger("forecast.train_live_model")

ARTIFACT_SCHEMA_VERSION = 1
SUPPORTED_HORIZONS = (10, 20, 30)
PROHIBITED_SOURCE_KEYWORDS = ("synthetic", "mock", "fixture", "dummy", "fake")
AUTHENTIC_OPERATIONAL_SOURCES = {"proof_archive", "harvester_daemon", "edge_worker", "inference_worker"}

# Exit codes
EXIT_OK = 0
EXIT_CONFIG_ERROR = 1
EXIT_DATA_ERROR = 2
EXIT_GATE_FAILED = 3
EXIT_FAIL_ON_LOCKED = 4
EXIT_INTERRUPTED = 130
EXIT_TERMINATED = 143

# Default paths
DEFAULT_DB_PATH = REPO_ROOT / "services" / "go-server" / "data" / "camera-view.db"
FALLBACK_DB_PATH = REPO_ROOT / "data" / "camera-view.db"
DEFAULT_FEATURES_OUT = REPO_ROOT / "data" / "forecast" / "features_live_5m.csv"
DEFAULT_ARTIFACT_OUT = (
    REPO_ROOT
    / "services"
    / "go-server"
    / "internal"
    / "forecast"
    / "embedded"
    / "prototype_v1.json"
)
DEFAULT_REPORT_OUT = REPO_ROOT / "docs" / "reports" / "forecast_prototype_v1.md"
DEFAULT_EVAL_JSON_OUT = REPO_ROOT / "docs" / "reports" / "forecast_prototype_v1.json"

DEFAULT_TRAIN_RATIO = 0.80
DEFAULT_BIN_MINUTES = 5
DEFAULT_RIDGE_LAMBDA = 1.0
DEFAULT_TARGET_MODE = "delta"
DEFAULT_MODEL_VERSION = "2026.11.10-live"
DEFAULT_TIMEZONE = "Asia/Ho_Chi_Minh"

# Scheduler defaults (S12-T1, Issue #413)
DEFAULT_INTERVAL_SECONDS = 21600  # 6 hours
DEFAULT_LOCK_FILE = REPO_ROOT / "var" / "forecast" / "refit.lock"
DEFAULT_STATE_FILE = REPO_ROOT / "var" / "forecast" / "refit_state.json"
DAEMON_SLEEP_GRANULARITY_SECONDS = 1.0
SCHEDULE_LOOKAHEAD_DAYS = 8
SCHEDULER_STATUSES = (
    "success",
    "promoted",
    "unchanged",
    "skipped_locked",
    "skipped_no_new_data",
    "gate_failed",
    "error",
)


def resolve_db_path(db_arg: Optional[Any]) -> Path:
    """Resolve database path, falling back to repository alternative if unspecified."""
    if db_arg is not None:
        return Path(db_arg).resolve()
    if DEFAULT_DB_PATH.exists():
        return DEFAULT_DB_PATH.resolve()
    if FALLBACK_DB_PATH.exists():
        return FALLBACK_DB_PATH.resolve()
    return DEFAULT_DB_PATH.resolve()


def make_portable_path(path: Path) -> str:
    """Return repository-relative path string if under REPO_ROOT, otherwise resolved path."""
    try:
        return str(path.resolve().relative_to(REPO_ROOT.resolve()))
    except (ValueError, RuntimeError):
        return str(path)


def compute_sha256(path_or_bytes: Path | bytes) -> str:
    """Compute cryptographic SHA-256 hex digest of file or byte string."""
    hasher = hashlib.sha256()
    if isinstance(path_or_bytes, Path):
        with path_or_bytes.open("rb") as handle:
            while chunk := handle.read(65536):
                hasher.update(chunk)
    else:
        hasher.update(path_or_bytes)
    return hasher.hexdigest()


def atomic_write(path: Path, content: str, mode: int = 0o644) -> None:
    """Atomically write text content to a destination file using os.replace."""
    path.parent.mkdir(parents=True, exist_ok=True)
    temp_file = tempfile.NamedTemporaryFile(
        "w",
        dir=path.parent,
        prefix=f".{path.name}.",
        suffix=".tmp",
        delete=False,
        encoding="utf-8",
    )
    try:
        temp_file.write(content)
        temp_file.flush()
        os.fsync(temp_file.fileno())
        # Set explicit permissions before closing and replacing (M7)
        try:
            os.chmod(temp_file.name, mode)
        except OSError:
            pass
        temp_file.close()
        os.replace(temp_file.name, path)
        # Flush parent directory metadata for crash durability (N15)
        try:
            dir_fd = os.open(str(path.parent), os.O_RDONLY)
            try:
                os.fsync(dir_fd)
            finally:
                os.close(dir_fd)
        except (OSError, AttributeError):
            pass
    except Exception:
        if os.path.exists(temp_file.name):
            try:
                os.remove(temp_file.name)
            except OSError:
                pass
        raise


def extract_live_features(
    db_path: Path,
    weather_db_path: Path,
    bin_minutes: int,
    local_tz: ZoneInfo,
    include_directional: bool = False,
) -> tuple[list[extract_features.FeatureRow], list[str], dict[str, Any], dict[str, int]]:
    """Extract authentic live traffic features from SQLite with strict provenance verification."""
    if not db_path.exists():
        raise FileNotFoundError(f"SQLite database not found: {db_path}")

    LOG.info("Reading live traffic observations from %s", db_path)
    obs, obs_stats = extract_features.load_sqlite_observations(
        db_path, local_tz, include_directional=include_directional
    )
    if not obs:
        raise ValueError(
            f"No observations found in hotset_verdict_history in {db_path}"
        )

    # Provenance guard (Rule 10 & M1): verify that observations do not derive from synthetic sources
    # and strictly originate from authentic operational sources.
    source_counts = dict(collections.Counter(o.source or "" for o in obs))
    for src in source_counts:
        src_lower = src.lower()
        if any(kw in src_lower for kw in PROHIBITED_SOURCE_KEYWORDS):
            raise ValueError(
                f"Prohibited synthetic source detected in observations: {src!r} (Rule 10 violation)"
            )

    has_operational = any(src in AUTHENTIC_OPERATIONAL_SOURCES for src in source_counts)
    if not has_operational:
        raise ValueError(
            f"No authentic operational sources detected in observations. Found sources: {source_counts}. "
            f"Expected at least one authentic operational source from {sorted(AUTHENTIC_OPERATIONAL_SOURCES)} (Rule 10 / M1 violation)"
        )

    LOG.info("Loaded %d live traffic observations (stats: %s, sources: %s)", len(obs), obs_stats, source_counts)

    weather_obs = []
    if weather_db_path.exists():
        LOG.info("Reading weather observations from %s", weather_db_path)
        weather_obs, weather_stats = extract_features.load_sqlite_weather_observations(
            weather_db_path, local_tz
        )
        LOG.info(
            "Loaded %d weather observations (stats: %s)",
            len(weather_obs),
            weather_stats,
        )
    else:
        LOG.warning("Weather database %s does not exist, proceeding without weather", weather_db_path)

    LOG.info("Resampling observations into %d-minute regular bins", bin_minutes)
    rows, fieldnames, stats = extract_features.build_feature_rows(
        obs,
        bin_minutes=bin_minutes,
        local_tz=local_tz,
        lag_minutes=(5, 10, 15),
        rolling_window_minutes=15,
        lag_tolerance_minutes=5.0,
        weather_observations=weather_obs,
    )
    LOG.info("Extracted %d feature rows (stats: %s)", len(rows), stats)
    return rows, fieldnames, stats, source_counts


def build_model_artifact(
    backtest_result: dict[str, Any],
    records: Sequence[backtest.FeatureRecord],
    split: backtest.ChronologicalSplit,
    features_sha256: str,
    model_version: str,
    target_mode: str,
    horizons_minutes: Sequence[int],
    db_sha256: Optional[str] = None,
    source_counts: Optional[dict[str, int]] = None,
) -> dict[str, Any]:
    """Construct model artifact matching Go services/go-server/internal/forecast/artifact.go schema."""
    train_records = [r for r in records if r.bin_start < split.cutoff]
    if not train_records:
        raise ValueError("Cannot build model artifact: training split has zero records")

    # fit_rows semantic: total eligible time-series training bins across all cameras (Rule N1)
    fit_rows = len(train_records)
    distinct_cameras = sorted({r.camera_id for r in train_records})
    fit_cameras = len(distinct_cameras)

    min_start_utc = min(r.bin_start for r in train_records).isoformat().replace("+00:00", "Z")
    max_start_utc = max(r.bin_start for r in train_records).isoformat().replace("+00:00", "Z")
    fit_window = {
        "start": min_start_utc,
        "end": max_start_utc,
    }

    results = backtest_result["results"]
    h10_key = str(horizons_minutes[0])
    candidate_key = f"ridge_{target_mode}"
    if candidate_key not in results[h10_key]["models"]:
        raise ValueError(f"Candidate model {candidate_key!r} not found in backtest results")

    candidate_h10 = results[h10_key]["models"][candidate_key]
    persistence_h10 = results[h10_key]["models"]["persistence"]
    median_h10 = results[h10_key]["models"]["historical_median"]

    candidate_horizons: list[dict[str, Any]] = []
    persistence_horizons: list[dict[str, Any]] = []
    median_horizons: list[dict[str, Any]] = []

    for h in horizons_minutes:
        h_str = str(h)
        h_res = results[h_str]
        test_samples = h_res["evaluation"]["test_samples"]

        # 1. Candidate ridge horizon
        r_model = h_res["models"][candidate_key]
        r_params = r_model["parameters"]
        r_metrics = r_model["metrics"]
        med_model = h_res["models"]["historical_median"]
        global_median = med_model["parameters"]["global_median"]

        candidate_horizons.append({
            "horizon_minutes": h,
            "mae": round(r_metrics["mae"], 8),
            "rmse": round(r_metrics["rmse"], 8),
            "sample_count": test_samples,
            "params": {
                "horizon_minutes": h,
                "intercept": round(r_params["intercept"], 8),
                "means": [round(x, 8) for x in r_params["means"]],
                "stds": [round(x, 8) for x in r_params["stds"]],
                "coeffs": [round(x, 8) for x in r_params["coeffs"]],
                "mae": round(r_metrics["mae"], 8),
                "rmse": round(r_metrics["rmse"], 8),
                "global_median": round(global_median, 8),
            },
        })

        # 2. Persistence horizon
        p_model = h_res["models"]["persistence"]
        p_metrics = p_model["metrics"]
        persistence_horizons.append({
            "horizon_minutes": h,
            "mae": round(p_metrics["mae"], 8),
            "rmse": round(p_metrics["rmse"], 8),
            "sample_count": test_samples,
        })

        # 3. Historical median horizon
        m_model = h_res["models"]["historical_median"]
        m_metrics = m_model["metrics"]
        median_horizons.append({
            "horizon_minutes": h,
            "mae": round(m_metrics["mae"], 8),
            "rmse": round(m_metrics["rmse"], 8),
            "sample_count": test_samples,
            "params": {
                "horizon_minutes": h,
                "global_median": round(m_model["parameters"]["global_median"], 8),
            },
        })

    model_entries = [
        {
            "name": candidate_key,
            "version": model_version,
            "target_mode": target_mode,
            "fit_source": "live_history",
            "fit_rows": fit_rows,
            "fit_cameras": fit_cameras,
            "fit_window": fit_window,
            "fit_data_sha256": features_sha256,
            "source_counts": source_counts or {},
            "mae": round(candidate_h10["metrics"]["mae"], 8),
            "horizons": candidate_horizons,
        },
        {
            "name": "persistence",
            "version": model_version,
            "target_mode": "persistence",
            "fit_source": "live_history",
            "fit_rows": fit_rows,
            "fit_cameras": fit_cameras,
            "fit_window": fit_window,
            "fit_data_sha256": features_sha256,
            "source_counts": source_counts or {},
            "mae": round(persistence_h10["metrics"]["mae"], 8),
            "horizons": persistence_horizons,
        },
        {
            "name": "historical_median",
            "version": model_version,
            "target_mode": "historical_median",
            "fit_source": "live_history",
            "fit_rows": fit_rows,
            "fit_cameras": fit_cameras,
            "fit_window": fit_window,
            "fit_data_sha256": features_sha256,
            "source_counts": source_counts or {},
            "mae": round(median_h10["metrics"]["mae"], 8),
            "horizons": median_horizons,
        },
    ]

    artifact: dict[str, Any] = {
        "schema_version": ARTIFACT_SCHEMA_VERSION,
        "generated_at_utc": backtest_result["generated_at_utc"],
        "fit_source": "live_history",
        "models": model_entries,
    }
    if db_sha256:
        artifact["db_sha256"] = db_sha256

    return artifact


def verify_artifact_data(data: Any, candidate_key: str = "ridge_delta") -> tuple[bool, str]:
    """Validate in-memory candidate model artifact against schema and Go server invariants."""
    if not isinstance(data, dict):
        return False, "Artifact root must be a JSON object"

    if data.get("schema_version") != ARTIFACT_SCHEMA_VERSION:
        return (
            False,
            f"schema_version must be {ARTIFACT_SCHEMA_VERSION}, got {data.get('schema_version')}",
        )

    models = data.get("models")
    if not isinstance(models, list) or len(models) == 0:
        return False, "Artifact must contain a non-empty 'models' list"

    model_map: dict[str, dict[str, Any]] = {}
    for m in models:
        if not isinstance(m, dict) or "name" not in m:
            return False, "Every model entry must be a dictionary with a 'name' field"
        model_map[m["name"]] = m

    for required in (candidate_key, "persistence", "historical_median"):
        if required not in model_map:
            return False, f"Missing required model '{required}' in artifact"

    for name, m in model_map.items():
        if m.get("fit_source") != "live_history":
            return (
                False,
                f"Model '{name}' fit_source must be 'live_history', found {m.get('fit_source')!r}",
            )
        if not m.get("fit_rows") or m.get("fit_rows") <= 0:
            return False, f"Model '{name}' fit_rows must be positive"
        if not m.get("fit_cameras") or m.get("fit_cameras") <= 0:
            return False, f"Model '{name}' fit_cameras must be positive"
        window = m.get("fit_window")
        if not isinstance(window, dict) or "start" not in window or "end" not in window:
            return False, f"Model '{name}' fit_window must specify 'start' and 'end'"
        if not m.get("fit_data_sha256") or len(m.get("fit_data_sha256")) != 64:
            return False, f"Model '{name}' fit_data_sha256 must be a 64-character SHA-256 hex string"

        horizons = m.get("horizons")
        if not isinstance(horizons, list) or len(horizons) == 0:
            return False, f"Model '{name}' horizons must be non-empty"

        for h_entry in horizons:
            if not isinstance(h_entry, dict):
                return False, f"Model '{name}' horizon entry must be a dictionary (A1)"

        covered_horizons = {h_entry.get("horizon_minutes") for h_entry in horizons}
        if name == candidate_key:
            missing = set(SUPPORTED_HORIZONS) - covered_horizons
            if missing:
                return (
                    False,
                    f"Candidate model '{name}' missing required horizons {sorted(missing)}; "
                    f"must cover all {SUPPORTED_HORIZONS} (RB1)",
                )

        for h_entry in horizons:
            h_min = h_entry.get("horizon_minutes")
            if h_min not in SUPPORTED_HORIZONS:
                return False, f"Model '{name}' has unsupported horizon {h_min}"
            if h_entry.get("mae", -1.0) <= 0.0:
                return False, f"Model '{name}' horizon {h_min} has non-positive MAE"
            if h_entry.get("rmse", -1.0) <= 0.0:
                return False, f"Model '{name}' horizon {h_min} has non-positive RMSE"

            if name == candidate_key:
                params = h_entry.get("params")
                if not isinstance(params, dict):
                    return False, f"{candidate_key} horizon {h_min} is missing 'params'"
                for key in ("means", "stds", "coeffs"):
                    arr = params.get(key)
                    if not isinstance(arr, list) or len(arr) != 6:
                        return False, f"{candidate_key} horizon {h_min} param '{key}' must have exactly 6 floats"
                    if any(not math.isfinite(x) for x in arr):
                        return False, f"{candidate_key} horizon {h_min} param '{key}' contains non-finite numbers"
                if "intercept" not in params or not math.isfinite(params["intercept"]):
                    return False, f"{candidate_key} horizon {h_min} intercept must be finite"

    # Enforce verification gate: candidate MAE must beat persistence on at least 1 horizon
    candidate_horizons = {h["horizon_minutes"]: h["mae"] for h in model_map[candidate_key]["horizons"]}
    persist_horizons = {h["horizon_minutes"]: h["mae"] for h in model_map["persistence"]["horizons"]}
    beats = [h for h in candidate_horizons if candidate_horizons[h] < persist_horizons.get(h, 0.0)]
    if not beats:
        return False, f"Gate failed: candidate {candidate_key} does not beat persistence on any horizon"

    return True, f"Artifact verification passed (beats persistence on horizons: {beats})"


def verify_artifact(artifact_path: Path) -> tuple[bool, str]:
    """Verify an existing model artifact from disk without refitting."""
    if not artifact_path.exists():
        return False, f"Artifact file does not exist: {artifact_path}"

    try:
        with artifact_path.open("r", encoding="utf-8") as f:
            data = json.load(f)
    except Exception as exc:
        return False, f"Failed to parse JSON in {artifact_path}: {exc}"

    return verify_artifact_data(data)


def build_parser() -> argparse.ArgumentParser:
    """Build command-line parser for train_live_model.py."""
    parser = argparse.ArgumentParser(
        description="Automated forecast refit pipeline & live history model deployment.",
        formatter_class=argparse.ArgumentDefaultsHelpFormatter,
    )
    parser.add_argument(
        "--db",
        type=Path,
        default=None,
        help="Path to SQLite database (default: services/go-server/data/camera-view.db, fallback: data/camera-view.db)",
    )
    parser.add_argument(
        "--weather-db",
        type=Path,
        default=None,
        help="Path to SQLite database for weather_observations (default: same as --db)",
    )
    parser.add_argument(
        "--features-out",
        type=Path,
        default=DEFAULT_FEATURES_OUT,
        help="Path to write the extracted features CSV",
    )
    parser.add_argument(
        "--artifact-out",
        type=Path,
        default=DEFAULT_ARTIFACT_OUT,
        help="Path to write the promoted model artifact JSON",
    )
    parser.add_argument(
        "--report-out",
        type=Path,
        default=DEFAULT_REPORT_OUT,
        help="Path to write markdown backtest report",
    )
    parser.add_argument(
        "--eval-json-out",
        type=Path,
        default=DEFAULT_EVAL_JSON_OUT,
        help="Path to write backtest eval output JSON",
    )
    parser.add_argument(
        "--train-ratio",
        type=float,
        default=DEFAULT_TRAIN_RATIO,
        help="Chronological train/test split ratio (0.1 to 0.95)",
    )
    parser.add_argument(
        "--bin-minutes",
        type=int,
        default=DEFAULT_BIN_MINUTES,
        choices=(5, 10),
        help="Regular bin width in minutes",
    )
    parser.add_argument(
        "--horizons",
        type=str,
        default="10,20,30",
        help="Comma-separated forecast horizons in minutes (subset of 10,20,30)",
    )
    parser.add_argument(
        "--ridge-lambda",
        type=float,
        default=DEFAULT_RIDGE_LAMBDA,
        help="Ridge regression L2 regularization parameter",
    )
    parser.add_argument(
        "--target-mode",
        type=str,
        default=DEFAULT_TARGET_MODE,
        choices=("delta",),
        help="Target prediction mode (Go server serves 'delta' mode)",
    )
    parser.add_argument(
        "--include-directional",
        action="store_true",
        default=False,
        help="Include directional sub-lane rows (default False for train/serve parity with direction='')",
    )
    parser.add_argument(
        "--version",
        type=str,
        default=DEFAULT_MODEL_VERSION,
        help="Candidate model version string",
    )
    parser.add_argument(
        "--timezone",
        type=str,
        default=DEFAULT_TIMEZONE,
        help="Local timezone for calendar feature alignment",
    )
    parser.add_argument(
        "--promote",
        action="store_true",
        help="Atomically promote candidate artifact to --artifact-out on gate pass",
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Run pipeline and evaluate gate without writing or updating files on disk",
    )
    parser.add_argument(
        "--verify",
        action="store_true",
        help="Verify existing artifact without refitting",
    )
    parser.add_argument(
        "--log-level",
        type=str,
        default="INFO",
        choices=("DEBUG", "INFO", "WARNING", "ERROR"),
        help="Logging level",
    )
    # Scheduler flags (S12-T1, Issue #413)
    parser.add_argument(
        "--daemon",
        action="store_true",
        help="Run as an in-process refit daemon (loop until SIGINT/SIGTERM or --max-runs)",
    )
    parser.add_argument(
        "--interval",
        type=int,
        default=DEFAULT_INTERVAL_SECONDS,
        help="Seconds between refit cycles when --schedule-hours is empty",
    )
    parser.add_argument(
        "--schedule-hours",
        type=str,
        default=None,
        help="Comma-separated local HH:MM slots (e.g. 03:00,15:00); overrides --interval",
    )
    parser.add_argument(
        "--lock-file",
        "--lock-path",
        type=Path,
        dest="lock_file",
        default=DEFAULT_LOCK_FILE,
        help="Advisory lock file that admits one refit at a time",
    )
    parser.add_argument(
        "--state-file",
        "--state-path",
        type=Path,
        dest="state_file",
        default=DEFAULT_STATE_FILE,
        help="JSON file recording scheduler run history and the last successful row count",
    )
    parser.add_argument(
        "--telemetry-out",
        type=Path,
        default=None,
        help="Path to write operator telemetry / status JSON snapshot",
    )
    parser.add_argument(
        "--status",
        action="store_true",
        help="Print current scheduler state JSON and exit",
    )
    parser.add_argument(
        "--health",
        action="store_true",
        help="Check scheduler health and exit (0=healthy, 1=unhealthy)",
    )
    parser.add_argument(
        "--min-new-rows",
        type=int,
        default=0,
        help="Skip a cycle unless at least this many new hotset_verdict_history rows arrived (0 disables)",
    )
    parser.add_argument(
        "--max-runs",
        type=int,
        default=0,
        help="Stop after this many cycles (0 runs until signalled)",
    )
    parser.add_argument(
        "--fail-on-locked",
        action="store_true",
        help="Exit with code 4 when another refit holds the lock instead of skipping the cycle",
    )
    return parser


def parse_horizons(raw: str) -> tuple[int, ...]:
    """Parse and validate comma-separated horizons string against Go-supported horizons (B2)."""
    tokens = [t.strip() for t in raw.split(",") if t.strip()]
    if not tokens:
        raise ValueError("Horizons cannot be empty")
    horizons = []
    for token in tokens:
        try:
            val = int(token)
            if val <= 0:
                raise ValueError()
        except ValueError:
            raise ValueError(f"Invalid forecast horizon: {token!r}")

        if val not in SUPPORTED_HORIZONS:
            raise ValueError(
                f"Unsupported forecast horizon {val}. Go server only supports horizons in {SUPPORTED_HORIZONS}"
            )
        horizons.append(val)
    return tuple(sorted(set(horizons)))


# --------------------------------------------------------------------------- #
# Scheduler support: single-flight lock, durable state, row-delta gating,
# wall-clock scheduling, and the in-process refit daemon (S12-T1 / Issue #413).
# --------------------------------------------------------------------------- #


class ForecastLockError(RuntimeError):
    """Raised when the refit lock cannot be created, inspected, or unlocked."""


class LockHeldError(ForecastLockError):
    """Raised when another process already holds the advisory refit lock."""

    def __init__(self, lock_path: Path, holder_pid: Optional[int] = None) -> None:
        self.lock_path = Path(lock_path)
        self.holder_pid = holder_pid
        holder = f" (holder pid: {holder_pid})" if holder_pid is not None else ""
        super().__init__(f"refit lock already held: {self.lock_path}{holder}")


class ForecastRefitLock:
    """Advisory exclusive lock that admits one forecast refit at a time."""

    def __init__(self, lock_path: Path) -> None:
        self.lock_path = Path(lock_path)
        self._fd: Optional[int] = None

    @property
    def is_held(self) -> bool:
        return self._fd is not None

    @property
    def acquired(self) -> bool:
        return self._fd is not None

    def acquire(self, blocking: bool = False) -> bool:
        if self._fd is not None:
            raise LockHeldError(self.lock_path, os.getpid())
        if fcntl is None:
            raise ForecastLockError("fcntl is unavailable; requires POSIX platform")
        try:
            self.lock_path.parent.mkdir(parents=True, exist_ok=True)
            fd = os.open(str(self.lock_path), os.O_RDWR | os.O_CREAT, 0o644)
        except OSError as exc:
            raise ForecastLockError(f"cannot open refit lock file {self.lock_path}: {exc}") from exc

        operation = fcntl.LOCK_EX if blocking else (fcntl.LOCK_EX | fcntl.LOCK_NB)
        try:
            fcntl.flock(fd, operation)
        except (BlockingIOError, OSError) as exc:
            os.close(fd)
            if isinstance(exc, BlockingIOError) or (hasattr(exc, "errno") and exc.errno in (11, 35)):
                if not blocking:
                    raise LockHeldError(self.lock_path, self.holder_pid())
                return False
            raise ForecastLockError(f"cannot flock refit lock file {self.lock_path}: {exc}") from exc

        try:
            payload = json.dumps({
                "pid": os.getpid(),
                "started_at": _utc_iso(datetime.now(timezone.utc)),
            })
            os.ftruncate(fd, 0)
            os.lseek(fd, 0, os.SEEK_SET)
            os.write(fd, payload.encode("utf-8"))
            os.fsync(fd)
        except OSError:
            pass

        self._fd = fd
        return True

    def release(self) -> None:
        fd, self._fd = self._fd, None
        if fd is None:
            return
        try:
            fcntl.flock(fd, fcntl.LOCK_UN)
        except OSError:
            pass
        finally:
            try:
                os.close(fd)
            except OSError:
                pass

    def holder_pid(self) -> Optional[int]:
        try:
            raw = self.lock_path.read_text(encoding="utf-8").strip()
            data = json.loads(raw)
            return data.get("pid")
        except Exception:
            return None

    def __enter__(self) -> "ForecastRefitLock":
        self.acquire(blocking=False)
        return self

    def __exit__(self, exc_type: object, exc: object, tb: object) -> bool:
        self.release()
        return False


def _utc_iso(moment: datetime) -> str:
    """Render a timestamp as ISO-8601 UTC with second precision."""
    if moment.tzinfo is None:
        moment = moment.replace(tzinfo=timezone.utc)
    return moment.astimezone(timezone.utc).isoformat(timespec="seconds")


def _coerce_optional_str(value: Any) -> Optional[str]:
    return value if isinstance(value, str) and value else None


def _coerce_int(value: Any, default: int) -> int:
    if value is None or isinstance(value, bool):
        return default
    if isinstance(value, int):
        return value
    if isinstance(value, float) and value.is_integer():
        return int(value)
    if isinstance(value, str) and value.strip().lstrip("-").isdigit():
        return int(value.strip())
    return default


def _coerce_optional_int(value: Any) -> Optional[int]:
    if value is None or isinstance(value, bool):
        return None
    if isinstance(value, int):
        return value
    if isinstance(value, float) and value.is_integer():
        return int(value)
    if isinstance(value, str) and value.strip().lstrip("-").isdigit():
        return int(value.strip())
    return None


@dataclass
class CycleOutcome:
    status: str
    exit_code: int
    rows_available: int = 0
    rows_new: int = 0
    artifact_sha256: str = ""


class SchedulerState:
    """Durable record of refit scheduling history."""

    SCHEMA_VERSION = 1

    def __init__(self, path: Optional[Path] = None) -> None:
        self.path = Path(path) if path is not None else None
        self.schema_version: int = self.SCHEMA_VERSION
        self.mode: str = "daemon"
        self.pid: Optional[int] = None
        self.started_at: Optional[str] = None
        self.cadence: dict[str, Any] = {}
        self.last_cycle: dict[str, Any] = {}
        self.counters: dict[str, int] = {
            "cycles_total": 0,
            "cycles_promoted": 0,
            "cycles_unchanged": 0,
            "cycles_gate_failed": 0,
            "cycles_error": 0,
            "cycles_skipped_locked": 0,
            "cycles_skipped_no_new_data": 0,
            "consecutive_failures": 0,
            "rows_last_seen": 0,
        }
        self.last_promotion: Optional[dict[str, Any]] = None
        self.last_slot_fired: Optional[str] = None
        self.next_slot_at: Optional[str] = None
        self.clean_shutdown: bool = True
        self.updated_at: Optional[str] = None

    @property
    def total_runs(self) -> int:
        return self.counters.get("cycles_total", 0)

    @total_runs.setter
    def total_runs(self, val: int) -> None:
        self.counters["cycles_total"] = val

    @property
    def consecutive_failures(self) -> int:
        return self.counters.get("consecutive_failures", 0)

    @consecutive_failures.setter
    def consecutive_failures(self, val: int) -> None:
        self.counters["consecutive_failures"] = val

    def to_dict(self) -> dict[str, Any]:
        return {
            "schema_version": self.schema_version,
            "mode": self.mode,
            "pid": self.pid,
            "started_at": self.started_at,
            "cadence": self.cadence,
            "last_cycle": self.last_cycle,
            "counters": self.counters,
            "last_promotion": self.last_promotion,
            "last_slot_fired": self.last_slot_fired,
            "next_slot_at": self.next_slot_at,
            "clean_shutdown": self.clean_shutdown,
            "updated_at": self.updated_at,
        }

    as_dict = to_dict

    @classmethod
    def from_dict(cls, raw: dict[str, Any], path: Optional[Path] = None) -> "SchedulerState":
        instance = cls(path=path)
        instance.schema_version = _coerce_int(raw.get("schema_version"), cls.SCHEMA_VERSION)
        instance.mode = _coerce_optional_str(raw.get("mode")) or "daemon"
        instance.pid = _coerce_optional_int(raw.get("pid"))
        instance.started_at = _coerce_optional_str(raw.get("started_at"))
        if isinstance(raw.get("cadence"), dict):
            instance.cadence = dict(raw["cadence"])
        if isinstance(raw.get("last_cycle"), dict):
            instance.last_cycle = dict(raw["last_cycle"])
        if isinstance(raw.get("counters"), dict):
            for k, v in raw["counters"].items():
                instance.counters[k] = _coerce_int(v, 0)
        if isinstance(raw.get("last_promotion"), dict):
            instance.last_promotion = dict(raw["last_promotion"])
        instance.last_slot_fired = _coerce_optional_str(raw.get("last_slot_fired"))
        instance.next_slot_at = _coerce_optional_str(raw.get("next_slot_at"))
        instance.clean_shutdown = bool(raw.get("clean_shutdown", True))
        instance.updated_at = _coerce_optional_str(raw.get("updated_at"))
        return instance

    @classmethod
    def load(cls, path: Path) -> "SchedulerState":
        path = Path(path)
        if not path.exists():
            return cls(path=path)
        try:
            raw = json.loads(path.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError) as exc:
            stamp = datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ")
            quarantine = path.with_name(f"{path.name}.corrupt-{stamp}")
            LOG.warning("State file %s corrupt (%s); quarantining to %s", path, exc, quarantine)
            try:
                os.replace(path, quarantine)
            except OSError:
                pass
            return cls(path=path)

        if not isinstance(raw, dict) or raw.get("schema_version") != cls.SCHEMA_VERSION:
            stamp = datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ")
            quarantine = path.with_name(f"{path.name}.corrupt-{stamp}")
            LOG.warning("State file %s invalid schema; quarantining to %s", path, quarantine)
            try:
                os.replace(path, quarantine)
            except OSError:
                pass
            return cls(path=path)

        return cls.from_dict(raw, path=path)

    def save(self, path: Optional[Path] = None) -> None:
        target_path = Path(path) if path is not None else self.path
        if target_path is None:
            raise ValueError("No path specified for SchedulerState.save()")
        self.updated_at = _utc_iso(datetime.now(timezone.utc))
        atomic_write(target_path, json.dumps(self.to_dict(), indent=2, sort_keys=True) + "\n")

    def record_cycle_start(self, started_at: datetime) -> None:
        self.last_cycle = {
            "started_at": _utc_iso(started_at),
            "status": "running",
        }
        self.clean_shutdown = False
        self.pid = os.getpid()

    def record_cycle_result(
        self,
        status: str,
        exit_code: int,
        *,
        completed_at: Optional[datetime] = None,
        **extra: Any,
    ) -> None:
        if completed_at is None:
            completed_at = datetime.now(timezone.utc)
        self.last_cycle["status"] = status
        self.last_cycle["exit_code"] = int(exit_code)
        self.last_cycle["completed_at"] = _utc_iso(completed_at)
        for k, v in extra.items():
            self.last_cycle[k] = v

        counter_key = f"cycles_{status}"
        if counter_key in self.counters:
            self.counters[counter_key] += 1
        else:
            self.counters[counter_key] = 1

        if status not in ("skipped_locked", "skipped_no_new_data"):
            self.counters["cycles_total"] += 1
            if status in ("promoted", "unchanged", "success"):
                self.counters["consecutive_failures"] = 0
            elif status in ("gate_failed", "error"):
                self.counters["consecutive_failures"] += 1


_SCHEDULE_SLOT_RE = re.compile(r"^\s*(?P<hour>\d{1,2}):(?P<minute>\d{2})\s*$")


def parse_schedule_hours(raw: str) -> tuple[str, ...]:
    """Parse comma-separated HH:MM schedule slots into sorted unique tuple."""
    if not raw or not raw.strip():
        raise ValueError("schedule slots cannot be empty")
    tokens = raw.split(",")
    slots = set()
    for token in tokens:
        stripped = token.strip()
        if not stripped:
            raise ValueError("empty schedule slot token in schedule_hours")
        m = _SCHEDULE_SLOT_RE.match(stripped)
        if not m:
            raise ValueError(f"Invalid schedule slot format: {stripped!r}")
        hour = int(m.group("hour"))
        minute = int(m.group("minute"))
        if not (0 <= hour <= 23):
            raise ValueError(f"Schedule hour {hour} out of range in {token!r}")
        if not (0 <= minute <= 59):
            raise ValueError(f"Schedule minute {minute} out of range in {token!r}")
        slots.add(f"{hour:02d}:{minute:02d}")
    return tuple(sorted(slots))


def compute_next_delay(
    *,
    now: datetime,
    interval_seconds: Optional[float] = None,
    slots: Sequence[str] = (),
    timezone: ZoneInfo = ZoneInfo(DEFAULT_TIMEZONE),
    last_slot_fired: Optional[str] = None,
) -> tuple[float, Optional[dict[str, str]]]:
    """Compute delay in seconds and optional slot marker until next refit."""
    if now.tzinfo is None:
        raise ValueError("compute_next_delay requires timezone-aware datetime")
    if not slots:
        interval = float(interval_seconds) if interval_seconds is not None else float(DEFAULT_INTERVAL_SECONDS)
        if interval <= 0:
            raise ValueError(f"interval must be positive (got {interval})")
        return interval, None

    now_tz = now.astimezone(timezone)
    best_delay: Optional[float] = None
    best_marker: Optional[dict[str, str]] = None

    for day_offset in range(SCHEDULE_LOOKAHEAD_DAYS):
        day = (now_tz + timedelta(days=day_offset)).date()
        for slot in slots:
            hour, minute = [int(p) for p in slot.split(":")]
            slot_dt = datetime(day.year, day.month, day.day, hour, minute, tzinfo=timezone)
            marker_str = f"{day.isoformat()}T{slot}"
            if last_slot_fired and marker_str == last_slot_fired:
                continue
            delay = (slot_dt - now_tz).total_seconds()
            if delay <= 0:
                continue
            if best_delay is None or delay < best_delay:
                best_delay = delay
                best_marker = {"local_date": day.isoformat(), "slot": slot}

    if best_delay is None:
        raise ValueError("Could not resolve future schedule slot")
    return best_delay, best_marker


class HistoryCountError(RuntimeError):
    """Raised when hotset_verdict_history row count cannot be determined."""


def count_history_rows(db_path: Path) -> int:
    """Count rows in hotset_verdict_history using read-only mode."""
    path = Path(db_path)
    if not path.exists():
        raise HistoryCountError(f"Database file does not exist: {path}")
    uri = f"file:{quote(path.resolve().as_posix(), safe='/:')}?mode=ro"
    conn = sqlite3.connect(uri, uri=True, timeout=5.0)
    try:
        conn.execute("PRAGMA query_only = ON")
        cur = conn.cursor()
        cur.execute(
            "SELECT name FROM sqlite_master WHERE type='table' AND name='hotset_verdict_history'"
        )
        if cur.fetchone() is None:
            return 0
        cur.execute("SELECT count(*) FROM hotset_verdict_history")
        row = cur.fetchone()
        return int(row[0]) if row else 0
    finally:
        conn.close()


def save_telemetry_snapshot(
    state: SchedulerState,
    telemetry_path: Path,
    healthy: bool = True,
) -> None:
    """Write operator telemetry snapshot JSON."""
    data = state.as_dict()
    data["healthy"] = healthy
    data["telemetry_generated_at"] = _utc_iso(datetime.now(timezone.utc))
    atomic_write(telemetry_path, json.dumps(data, indent=2))


def run_daemon(
    args: Optional[argparse.Namespace] = None,
    *,
    db_path: Optional[Path] = None,
    artifact_out: Optional[Path] = None,
    lock_path: Optional[Path] = None,
    state_path: Optional[Path] = None,
    telemetry_out: Optional[Path] = None,
    interval_seconds: Optional[float] = None,
    schedule_hours: Optional[Any] = None,
    timezone: Optional[ZoneInfo] = None,
    max_runs: Optional[int] = None,
    dry_run: bool = False,
    promote: bool = False,
    fail_on_locked: bool = False,
    min_new_rows: int = 0,
    cycle_fn: Optional[Any] = None,
    sleep_fn: Any = time.sleep,
    now_fn: Optional[Any] = None,
) -> int:
    """Execute scheduled daemon loop."""
    if args is None:
        args = build_parser().parse_args([])
        effective_db_path = resolve_db_path(db_path)
        effective_artifact_out = Path(artifact_out) if artifact_out else DEFAULT_ARTIFACT_OUT
        effective_lock_path = Path(lock_path) if lock_path else DEFAULT_LOCK_FILE
        effective_state_path = Path(state_path) if state_path else DEFAULT_STATE_FILE
        try:
            effective_interval = float(interval_seconds) if interval_seconds is not None else float(DEFAULT_INTERVAL_SECONDS)
            if effective_interval <= 0:
                LOG.error("--interval must be positive, got %s", effective_interval)
                return EXIT_CONFIG_ERROR
        except (ValueError, TypeError) as exc:
            LOG.error("Invalid --interval: %s", exc)
            return EXIT_CONFIG_ERROR

        try:
            if schedule_hours is None:
                slots = ()
            elif isinstance(schedule_hours, (list, tuple)):
                slots = tuple(schedule_hours)
            elif isinstance(schedule_hours, str):
                slots = parse_schedule_hours(schedule_hours)
            else:
                slots = ()
        except ValueError as exc:
            LOG.error("Invalid --schedule-hours: %s", exc)
            return EXIT_CONFIG_ERROR

        try:
            effective_tz = timezone or ZoneInfo(DEFAULT_TIMEZONE)
        except Exception as exc:
            LOG.error("Invalid timezone: %s", exc)
            return EXIT_CONFIG_ERROR

        effective_max_runs = max_runs
        effective_dry_run = dry_run
        effective_promote = promote
        effective_fail_on_locked = fail_on_locked
        effective_min_new_rows = min_new_rows
    else:
        effective_db_path = resolve_db_path(args.db) if db_path is None else Path(db_path)
        effective_artifact_out = Path(args.artifact_out) if artifact_out is None else Path(artifact_out)
        effective_lock_path = Path(args.lock_file) if lock_path is None else Path(lock_path)
        effective_state_path = Path(args.state_file) if state_path is None else Path(state_path)
        try:
            effective_interval = float(args.interval) if interval_seconds is None else float(interval_seconds)
            if effective_interval <= 0:
                LOG.error("--interval must be positive, got %s", effective_interval)
                return EXIT_CONFIG_ERROR
        except (ValueError, TypeError) as exc:
            LOG.error("Invalid --interval: %s", exc)
            return EXIT_CONFIG_ERROR

        try:
            if schedule_hours is not None:
                if isinstance(schedule_hours, (list, tuple)):
                    slots = tuple(schedule_hours)
                else:
                    slots = parse_schedule_hours(str(schedule_hours))
            elif getattr(args, "schedule_hours", None) is not None:
                slots = parse_schedule_hours(args.schedule_hours)
            else:
                slots = ()
        except ValueError as exc:
            LOG.error("Invalid --schedule-hours: %s", exc)
            return EXIT_CONFIG_ERROR

        tz_name = getattr(args, "timezone", None) or DEFAULT_TIMEZONE
        try:
            effective_tz = timezone if timezone is not None else ZoneInfo(tz_name)
        except Exception as exc:
            LOG.error("Invalid --timezone %r: %s", tz_name, exc)
            return EXIT_CONFIG_ERROR

        effective_max_runs = (args.max_runs if args.max_runs > 0 else None) if max_runs is None else max_runs
        effective_dry_run = args.dry_run or dry_run
        effective_promote = getattr(args, "promote", False) or promote
        effective_fail_on_locked = args.fail_on_locked or fail_on_locked
        effective_min_new_rows = args.min_new_rows if min_new_rows == 0 else min_new_rows

    effective_telemetry_out = Path(getattr(args, "telemetry_out", None)) if getattr(args, "telemetry_out", None) else (Path(telemetry_out) if telemetry_out else None)

    get_now = now_fn or (lambda: datetime.now(effective_tz))
    stop_event = threading.Event()

    if args is not None:
        args.db = Path(effective_db_path)
        args.artifact_out = Path(effective_artifact_out)
        args.lock_file = Path(effective_lock_path)
        args.state_file = Path(effective_state_path)
        args.dry_run = effective_dry_run
        args.promote = effective_promote
        args.fail_on_locked = effective_fail_on_locked
        args.min_new_rows = effective_min_new_rows

    def _default_sleep(duration: float) -> None:
        stop_event.wait(max(0.0, duration))

    actual_sleep_fn = _default_sleep if sleep_fn is time.sleep else sleep_fn

    signal_count = [0]

    def _on_signal(signum: int, _frame: Any) -> None:
        signal_count[0] += 1
        if signal_count[0] > 1:
            LOG.warning(
                "Received second signal %d; terminating immediately with exit %d",
                signum,
                128 + signum,
            )
            # AC5 second-signal escalation: exit 143 (SIGTERM) or 130 (SIGINT)
            sys.exit(128 + signum)
        LOG.info("Received signal %d; shutting down refit daemon cleanly", signum)
        stop_event.set()

    orig_handlers = {}
    for sig in (signal.SIGINT, signal.SIGTERM):
        try:
            orig_handlers[sig] = signal.signal(sig, _on_signal)
        except (ValueError, OSError):
            pass

    state = SchedulerState.load(effective_state_path)
    state.mode = "daemon"
    state.cadence = {
        "kind": "slots" if slots else "interval",
        "seconds": int(effective_interval),
        "timezone": effective_tz.key,
        "slots": list(slots),
    }

    daemon_started_at = state.started_at
    skipped_locked_count = 0
    runs_completed = 0
    terminal_code = EXIT_OK
    owns_state = False
    active_marker = None
    normal_exit = False

    def _save_state(st: SchedulerState, healthy: bool = True) -> None:
        st.save(effective_state_path)
        if effective_telemetry_out is not None:
            try:
                save_telemetry_snapshot(st, effective_telemetry_out, healthy=healthy)
            except Exception as exc:
                LOG.warning("Failed to write telemetry snapshot to %s: %s", effective_telemetry_out, exc)

    # M2: In slot mode, sleep to the first scheduled slot before running any refit
    if slots:
        delay, next_marker = compute_next_delay(
            now=get_now(),
            interval_seconds=effective_interval,
            slots=slots,
            timezone=effective_tz,
            last_slot_fired=state.last_slot_fired,
        )
        active_marker = f"{next_marker['local_date']}T{next_marker['slot']}" if next_marker else None
        if next_marker:
            state.next_slot_at = _utc_iso(get_now() + timedelta(seconds=delay))
            if owns_state:
                _save_state(state)
        actual_sleep_fn(delay)

    try:
        while not stop_event.is_set():
            if effective_max_runs is not None and runs_completed >= effective_max_runs:
                normal_exit = True
                break

            cycle_start = get_now()
            lock = ForecastRefitLock(effective_lock_path)
            lock_acquired = False
            try:
                lock.acquire(blocking=False)
                lock_acquired = True
                owns_state = True
                # Reload fresh state under lock to prevent lost updates (MJ-4)
                state = SchedulerState.load(effective_state_path)
                state.mode = "daemon"
                state.cadence = {
                    "kind": "slots" if slots else "interval",
                    "seconds": int(effective_interval),
                    "timezone": effective_tz.key,
                    "slots": list(slots),
                }
                if daemon_started_at and not state.started_at:
                    state.started_at = daemon_started_at
                if skipped_locked_count > 0:
                    state.counters["cycles_skipped_locked"] = (
                        state.counters.get("cycles_skipped_locked", 0) + skipped_locked_count
                    )
                    skipped_locked_count = 0
            except LockHeldError:
                LOG.warning("Refit lock held elsewhere: %s", effective_lock_path)
                runs_completed += 1
                skipped_locked_count += 1
                state.record_cycle_result("skipped_locked", EXIT_FAIL_ON_LOCKED, error="Refit lock held elsewhere")
                if effective_fail_on_locked:
                    return EXIT_FAIL_ON_LOCKED
                if effective_max_runs is not None and runs_completed >= effective_max_runs:
                    normal_exit = True
                    return EXIT_OK
                delay, next_marker = compute_next_delay(now=get_now(), interval_seconds=effective_interval, slots=slots, timezone=effective_tz, last_slot_fired=state.last_slot_fired)
                active_marker = f"{next_marker['local_date']}T{next_marker['slot']}" if next_marker else None
                actual_sleep_fn(delay)
                continue
            except ForecastLockError as exc:
                LOG.error("Failed to acquire refit lock: %s", exc)
                return EXIT_CONFIG_ERROR

            try:
                state.pid = os.getpid()
                state.clean_shutdown = False
                state.record_cycle_start(cycle_start)
                _save_state(state)

                rows_seen = state.counters.get("rows_last_seen", 0)
                try:
                    current_rows = count_history_rows(effective_db_path)
                    prev_rows = state.counters.get("rows_last_seen", 0)

                    # Reset baseline if database rows decreased (e.g. table truncated / rotated)
                    if current_rows < prev_rows:
                        LOG.warning(
                            "History row count decreased (%d < %d); resetting baseline to current row count",
                            current_rows,
                            prev_rows,
                        )
                        prev_rows = current_rows
                        state.counters["rows_last_seen"] = current_rows

                    new_rows = current_rows - prev_rows

                    if effective_min_new_rows > 0 and new_rows < effective_min_new_rows:
                        LOG.info(
                            "Skipping refit: %d new rows < min %d (total=%d, baseline=%d)",
                            new_rows,
                            effective_min_new_rows,
                            current_rows,
                            prev_rows,
                        )
                        state.record_cycle_result("skipped_no_new_data", EXIT_OK)
                        _save_state(state)
                        runs_completed += 1
                        if effective_max_runs is not None and runs_completed >= effective_max_runs:
                            normal_exit = True
                            state.clean_shutdown = True
                            state.pid = None
                            _save_state(state)
                            break
                        delay, next_marker = compute_next_delay(now=get_now(), interval_seconds=effective_interval, slots=slots, timezone=effective_tz, last_slot_fired=state.last_slot_fired)
                        active_marker = f"{next_marker['local_date']}T{next_marker['slot']}" if next_marker else None
                        if next_marker:
                            state.next_slot_at = _utc_iso(get_now() + timedelta(seconds=delay))
                            _save_state(state)
                        actual_sleep_fn(delay)
                        continue

                    if cycle_fn is not None:
                        outcome = cycle_fn(effective_dry_run)
                        if isinstance(outcome, CycleOutcome):
                            c_status = outcome.status
                            c_exit = outcome.exit_code
                            rows_seen = outcome.rows_available or current_rows
                        else:
                            c_exit = int(outcome)
                            c_status = "promoted" if c_exit == 0 else "error"
                            rows_seen = current_rows
                    else:
                        old_sha = hashlib.sha256(effective_artifact_out.read_bytes()).hexdigest() if effective_artifact_out.exists() else None
                        c_exit = run_cycle(args)
                        new_sha = hashlib.sha256(effective_artifact_out.read_bytes()).hexdigest() if effective_artifact_out.exists() else None
                        if c_exit == EXIT_OK:
                            if args.promote and not effective_dry_run and new_sha != old_sha:
                                c_status = "promoted"
                            else:
                                c_status = "unchanged"
                        elif c_exit == EXIT_GATE_FAILED:
                            c_status = "gate_failed"
                        else:
                            c_status = "error"
                        rows_seen = current_rows

                    state.record_cycle_result(c_status, c_exit)
                    if c_status in ("promoted", "unchanged", "success"):
                        state.counters["rows_last_seen"] = rows_seen
                        if active_marker:
                            state.last_slot_fired = active_marker
                except Exception as exc:
                    LOG.error("Forecast daemon cycle raised unexpected exception: %s", exc, exc_info=True)
                    c_status = "error"
                    c_exit = EXIT_DATA_ERROR
                    state.record_cycle_result(c_status, c_exit, error=str(exc))

                runs_completed += 1
                if effective_max_runs is not None and runs_completed >= effective_max_runs:
                    normal_exit = True
                    state.clean_shutdown = True
                    state.pid = None
                _save_state(state, healthy=(c_exit in (EXIT_OK, EXIT_FAIL_ON_LOCKED)))

                if c_exit == EXIT_CONFIG_ERROR:
                    terminal_code = c_exit
                    break
            finally:
                if lock_acquired:
                    lock.release()
                    owns_state = False

            if effective_max_runs is not None and runs_completed >= effective_max_runs:
                normal_exit = True
                break

            if stop_event.is_set():
                normal_exit = True
                break

            cycle_duration = (get_now() - cycle_start).total_seconds()
            delay, next_marker = compute_next_delay(now=get_now(), interval_seconds=effective_interval, slots=slots, timezone=effective_tz, last_slot_fired=state.last_slot_fired)
            active_marker = f"{next_marker['local_date']}T{next_marker['slot']}" if next_marker else None
            if not slots:
                delay = max(0.0, effective_interval - cycle_duration)
            if next_marker:
                state.next_slot_at = _utc_iso(get_now() + timedelta(seconds=delay))
                _save_state(state)
            actual_sleep_fn(delay)

        if stop_event.is_set():
            normal_exit = True

    finally:
        if normal_exit:
            try:
                term_lock = ForecastRefitLock(effective_lock_path)
                try:
                    term_lock.acquire(blocking=False)
                    st = SchedulerState.load(effective_state_path)
                    st.clean_shutdown = True
                    st.pid = None
                    if skipped_locked_count > 0:
                        st.counters["cycles_skipped_locked"] = (
                            st.counters.get("cycles_skipped_locked", 0) + skipped_locked_count
                        )
                        skipped_locked_count = 0
                    _save_state(st, healthy=True)
                finally:
                    term_lock.release()
            except Exception:
                pass
        for sig, h in orig_handlers.items():
            try:
                signal.signal(sig, h)
            except (ValueError, OSError):
                pass

    return terminal_code


def run(argv: Optional[Sequence[str]] = None) -> int:
    """Execute training pipeline CLI: one-shot refit, verification, or daemon."""
    parser = build_parser()
    args = parser.parse_args(argv)

    logging.basicConfig(
        level=getattr(logging, args.log_level.upper(), logging.INFO),
        format="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
    )

    if getattr(args, "status", False):
        state_path = Path(args.state_file) if args.state_file else DEFAULT_STATE_FILE
        if not state_path.exists():
            LOG.error("Scheduler state file not found: %s", state_path)
            return EXIT_CONFIG_ERROR
        try:
            st = SchedulerState.load(state_path)
            print(json.dumps(st.as_dict(), indent=2))
            return EXIT_OK
        except Exception as exc:
            LOG.error("Failed to load scheduler state from %s: %s", state_path, exc)
            return EXIT_CONFIG_ERROR

    if getattr(args, "health", False):
        state_path = Path(args.state_file) if args.state_file else DEFAULT_STATE_FILE
        if not state_path.exists():
            LOG.error("Scheduler state file not found: %s", state_path)
            return EXIT_CONFIG_ERROR
        try:
            raw = json.loads(state_path.read_text(encoding="utf-8"))
            if not isinstance(raw, dict) or raw.get("schema_version") != 1:
                LOG.error("Unhealthy: invalid state schema in %s", state_path)
                return EXIT_CONFIG_ERROR
            st = SchedulerState.from_dict(raw, path=state_path)
            if st.counters.get("consecutive_failures", 0) >= 5:
                LOG.error(
                    "Unhealthy: excessive consecutive failures (%d >= 5)",
                    st.counters["consecutive_failures"],
                )
                return EXIT_CONFIG_ERROR
            if st.pid is not None and not st.clean_shutdown:
                try:
                    os.kill(st.pid, 0)
                except OSError:
                    LOG.error("Unhealthy: daemon PID %d is not running", st.pid)
                    return EXIT_CONFIG_ERROR
            print("OK - scheduler state healthy")
            return EXIT_OK
        except Exception as exc:
            LOG.error("Unhealthy: failed to verify state: %s", exc)
            return EXIT_CONFIG_ERROR

    if args.daemon:
        if args.verify:
            LOG.error("--daemon and --verify are mutually exclusive")
            return EXIT_CONFIG_ERROR
        return run_daemon(args)

    if args.verify:
        LOG.info("Verifying model artifact at %s", args.artifact_out)
        valid, msg = verify_artifact(args.artifact_out)
        if valid:
            LOG.info("Verification succeeded: %s", msg)
            return EXIT_OK
        LOG.error("Verification failed: %s", msg)
        return EXIT_GATE_FAILED

    # One-shot path under cooperative refit lock (B1 / AC4)
    lock_path = Path(args.lock_file) if args.lock_file else None
    if lock_path:
        lock = ForecastRefitLock(lock_path)
        try:
            lock.acquire(blocking=False)
        except LockHeldError:
            if args.fail_on_locked:
                LOG.error("Refit lock held elsewhere: %s (--fail-on-locked)", lock_path)
                return EXIT_FAIL_ON_LOCKED
            LOG.warning("Refit lock held elsewhere: %s (skipped)", lock_path)
            return EXIT_OK
        except ForecastLockError as exc:
            LOG.error("Failed to acquire refit lock: %s", exc)
            return EXIT_CONFIG_ERROR
        try:
            # One-shot row-delta gate (MJ-1)
            state_path = Path(args.state_file) if getattr(args, "state_file", None) else None
            min_rows = getattr(args, "min_new_rows", 0)
            if min_rows > 0 and state_path and state_path.exists():
                state = SchedulerState.load(state_path)
                try:
                    current_rows = count_history_rows(resolve_db_path(args.db))
                except HistoryCountError as exc:
                    LOG.error("History count error: %s", exc)
                    return EXIT_DATA_ERROR
                prev_rows = state.counters.get("rows_last_seen", 0)
                if current_rows < prev_rows:
                    prev_rows = current_rows
                    state.counters["rows_last_seen"] = current_rows
                new_rows = current_rows - prev_rows
                if new_rows < min_rows:
                    LOG.info(
                        "Skipping one-shot refit: %d new rows < min %d (total=%d, baseline=%d)",
                        new_rows, min_rows, current_rows, prev_rows
                    )
                    state.record_cycle_result("skipped_no_new_data", EXIT_OK)
                    state.save(state_path)
                    return EXIT_OK

            exit_code = run_cycle(args)
            if state_path and exit_code == EXIT_OK:
                state = SchedulerState.load(state_path)
                try:
                    current_rows = count_history_rows(resolve_db_path(args.db))
                    state.counters["rows_last_seen"] = current_rows
                except Exception:
                    pass
                state.record_cycle_result("promoted" if args.promote else "unchanged", EXIT_OK)
                state.save(state_path)
            return exit_code
        finally:
            lock.release()

    return run_cycle(args)


def run_cycle(args: argparse.Namespace) -> int:
    """Execute exactly one refit, evaluation gate, and optional promotion cycle."""
    if args.promote and not args.dry_run:
        artifact_out = Path(args.artifact_out)
        try:
            artifact_out.parent.mkdir(parents=True, exist_ok=True)
            if not os.access(artifact_out.parent, os.W_OK):
                LOG.error("Artifact directory is not writable: %s", artifact_out.parent)
                return EXIT_CONFIG_ERROR
            if artifact_out.exists() and not os.access(artifact_out, os.W_OK):
                LOG.error("Artifact file is not writable: %s", artifact_out)
                return EXIT_CONFIG_ERROR
        except Exception as exc:
            LOG.error("Cannot write artifact to %s: %s", artifact_out, exc)
            return EXIT_CONFIG_ERROR

    try:
        horizons = parse_horizons(args.horizons)
    except ValueError as exc:
        LOG.error("%s", exc)
        return EXIT_CONFIG_ERROR

    if args.promote and set(horizons) != set(SUPPORTED_HORIZONS):
        LOG.error(
            "Promotion halted: --promote requires full horizon coverage of all %s, but got %s (RB1)",
            SUPPORTED_HORIZONS,
            horizons,
        )
        return EXIT_CONFIG_ERROR

    try:
        local_tz = ZoneInfo(args.timezone)
    except Exception as exc:
        LOG.error("Invalid timezone %r: %s", args.timezone, exc)
        return EXIT_CONFIG_ERROR

    if not (0.1 <= args.train_ratio <= 0.95):
        LOG.error("train_ratio must be between 0.1 and 0.95 (got %s)", args.train_ratio)
        return EXIT_CONFIG_ERROR

    db_path = resolve_db_path(args.db)
    weather_db_path = args.weather_db.resolve() if args.weather_db else db_path

    if not db_path.exists():
        LOG.error("Database path does not exist: %s", db_path)
        return EXIT_CONFIG_ERROR

    db_sha256 = compute_sha256(db_path)

    # 1. Feature Extraction
    try:
        rows, fieldnames, stats, source_counts = extract_live_features(
            db_path=db_path,
            weather_db_path=weather_db_path,
            bin_minutes=args.bin_minutes,
            local_tz=local_tz,
            include_directional=args.include_directional,
        )
    except FileNotFoundError as exc:
        LOG.error("%s", exc)
        return EXIT_CONFIG_ERROR
    except ValueError as exc:
        LOG.error("Data validation failed: %s", exc)
        return EXIT_DATA_ERROR
    except Exception as exc:
        LOG.error("Feature extraction failed: %s", exc)
        return EXIT_DATA_ERROR

    if not rows:
        LOG.error("No valid feature rows extracted from %s", db_path)
        return EXIT_DATA_ERROR

    # 2. Write features CSV (or write to temporary path during dry-run)
    if args.dry_run:
        temp_csv = tempfile.NamedTemporaryFile("w", suffix=".csv", delete=False)
        temp_csv.close()
        features_csv_path = Path(temp_csv.name)
    else:
        features_csv_path = args.features_out
        features_csv_path.parent.mkdir(parents=True, exist_ok=True)

    try:
        extract_features.write_csv(rows, fieldnames, features_csv_path)
        features_sha256 = compute_sha256(features_csv_path)
        if not args.dry_run:
            LOG.info(
                "Wrote %d feature rows to %s (SHA-256: %s)",
                len(rows),
                features_csv_path,
                features_sha256,
            )
    except Exception as exc:
        LOG.error("Failed to write features CSV: %s", exc)
        return EXIT_DATA_ERROR

    # 3. Model Fitting & Backtest Evaluation
    try:
        records, summary = backtest.load_feature_records(
            features_csv_path,
            feature_columns=backtest.DEFAULT_FEATURE_COLUMNS,
            timezone_name=args.timezone,
        )
    except Exception as exc:
        LOG.error("Failed to load feature records for backtest: %s", exc)
        return EXIT_DATA_ERROR

    config = backtest.BacktestConfig(
        train_ratio=args.train_ratio,
        horizons_minutes=horizons,
        feature_columns=backtest.DEFAULT_FEATURE_COLUMNS,
        target_mode=args.target_mode,
        ridge_lambda=args.ridge_lambda,
        timezone=args.timezone,
    )

    try:
        input_display_path = make_portable_path(features_csv_path)
        summary["path"] = input_display_path

        portable_db_path = make_portable_path(db_path)
        command_repr = (
            f"python3 tools/forecast/train_live_model.py "
            f"--db {portable_db_path} --train-ratio {args.train_ratio} "
            f"--horizons {args.horizons} --target-mode {args.target_mode}"
        )
        backtest_result = backtest.run_backtest(
            records=records,
            input_summary=summary,
            config=config,
            data_note="Authentic live traffic history from SQLite hotset_verdict_history.",
            command=command_repr,
        )
        # Explicitly declare fit_source on backtest output to guarantee Gate 1 compliance
        backtest_result["fit_source"] = "live_history"
        if "data_provenance" in backtest_result:
            backtest_result["data_provenance"]["fit_source"] = "live_history"
            backtest_result["data_provenance"]["input"] = input_display_path
            backtest_result["data_provenance"]["db_sha256"] = db_sha256
    except Exception as exc:
        LOG.error("Backtest evaluation failed: %s", exc)
        return EXIT_DATA_ERROR

    verification = backtest_result.get("verification", {})
    gate_passed = bool(verification.get("passed"))
    beats_horizons = verification.get("beats_persistence_horizons", [])

    LOG.info(
        "Verification gate result: passed=%s (beats persistence at horizons: %s)",
        gate_passed,
        beats_horizons,
    )

    candidate_key = f"ridge_{args.target_mode}"
    split = backtest.chronological_split(records, args.train_ratio)
    candidate_artifact = build_model_artifact(
        backtest_result=backtest_result,
        records=records,
        split=split,
        features_sha256=features_sha256,
        model_version=args.version,
        target_mode=args.target_mode,
        horizons_minutes=horizons,
        db_sha256=db_sha256,
        source_counts=source_counts,
    )

    backtest_result["generator"] = (
        f"`tools/forecast/train_live_model.py` (via backtest core `{backtest.SCRIPT_VERSION}`)"
    )
    rendered_markdown = backtest.render_markdown(backtest_result)
    rendered_eval_json = json.dumps(backtest_result, indent=2, sort_keys=True) + "\n"
    rendered_artifact_json = json.dumps(candidate_artifact, indent=2, sort_keys=False) + "\n"

    # Clean up dry-run temporary CSV
    if args.dry_run and features_csv_path.exists():
        try:
            features_csv_path.unlink()
        except OSError:
            pass

    if not gate_passed:
        LOG.error(
            "Gate failed: candidate model does not beat persistence on any horizon. Deployment halted."
        )
        if not args.dry_run:
            try:
                atomic_write(args.eval_json_out, rendered_eval_json)
                atomic_write(args.report_out, rendered_markdown)
                LOG.info(
                    "Failure evidence recorded to %s and %s",
                    args.eval_json_out,
                    args.report_out,
                )
            except Exception as write_err:
                LOG.warning("Failed to record failure evidence: %s", write_err)
        return EXIT_GATE_FAILED

    # Gate passed
    if args.dry_run:
        LOG.info("Dry-run mode: gate passed, skipping all file writes on disk")
        return EXIT_OK

    # Pre-promotion validation (RB1 / B2 / N16: validate in-memory before writing outputs)
    if args.promote:
        valid, msg = verify_artifact_data(candidate_artifact, candidate_key=candidate_key)
        if not valid:
            LOG.error("Candidate artifact failed pre-promotion validation: %s", msg)
            return EXIT_GATE_FAILED

    # Update backtest reports
    try:
        atomic_write(args.eval_json_out, rendered_eval_json)
        atomic_write(args.report_out, rendered_markdown)
        LOG.info(
            "Updated evaluation outputs: %s and %s",
            args.eval_json_out,
            args.report_out,
        )
    except Exception as exc:
        LOG.error("Failed to write evaluation outputs: %s", exc)
        return EXIT_DATA_ERROR

    # Atomically promote artifact if requested
    if args.promote:
        artifact_out = Path(args.artifact_out)
        try:
            # AC8: Fail closed if artifact path or parent is unwritable
            artifact_out.parent.mkdir(parents=True, exist_ok=True)
            if not os.access(artifact_out.parent, os.W_OK):
                LOG.error("Artifact directory is not writable: %s", artifact_out.parent)
                return EXIT_CONFIG_ERROR
            if artifact_out.exists() and not os.access(artifact_out, os.W_OK):
                LOG.error("Artifact file is not writable: %s", artifact_out)
                return EXIT_CONFIG_ERROR

            # Check for byte-identical artifact (M6)
            if artifact_out.exists():
                existing_bytes = artifact_out.read_bytes()
                new_bytes = rendered_artifact_json.encode("utf-8")
                if existing_bytes == new_bytes:
                    LOG.info(
                        "Model artifact unchanged (byte-identical SHA-256); skipping rewrite"
                    )
                    return EXIT_OK
            atomic_write(artifact_out, rendered_artifact_json)
            LOG.info(
                "Model artifact successfully promoted to %s (version: %s)",
                artifact_out,
                args.version,
            )
        except Exception as exc:
            LOG.error("Failed to promote model artifact: %s", exc)
            return EXIT_CONFIG_ERROR
    else:
        LOG.info(
            "Gate passed! Candidate artifact was NOT deployed to %s (use --promote to deploy)",
            args.artifact_out,
        )

    return EXIT_OK


def main() -> None:
    sys.exit(run())


if __name__ == "__main__":
    main()
