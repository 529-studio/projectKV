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
import sys
import tempfile
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Optional, Sequence
from zoneinfo import ZoneInfo

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


def resolve_db_path(db_arg: Optional[Path]) -> Path:
    """Resolve database path, falling back to repository alternative if unspecified."""
    if db_arg is not None:
        return db_arg.resolve()
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


def atomic_write(path: Path, content: str) -> None:
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


def run(argv: Optional[Sequence[str]] = None) -> int:
    """Execute training pipeline CLI."""
    parser = build_parser()
    args = parser.parse_args(argv)

    logging.basicConfig(
        level=getattr(logging, args.log_level.upper(), logging.INFO),
        format="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
    )

    if args.verify:
        LOG.info("Verifying model artifact at %s", args.artifact_out)
        valid, msg = verify_artifact(args.artifact_out)
        if valid:
            LOG.info("Verification succeeded: %s", msg)
            return EXIT_OK
        else:
            LOG.error("Verification failed: %s", msg)
            return EXIT_GATE_FAILED

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
        try:
            atomic_write(args.artifact_out, rendered_artifact_json)
            LOG.info(
                "Model artifact successfully promoted to %s (version: %s)",
                args.artifact_out,
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
