#!/usr/bin/env python3
"""
service.py - 24/7 autonomous Hot Set harvester daemon.

Sprint 11 / Issue #396 (S11-T2):
  * Cycles through the 57 Hot Set cameras on a strict interval (default 300 s).
  * Fetches snapshots from the upstream portal or the local go-server proxy cache.
  * Runs CV-engine vehicle analysis when available and falls back to the
    deterministic image-content analysis shared with the proof archive ingester.
  * Persists frame-wide and directional verdicts to ``hotset_verdict_history``.
  * Archives snapshots under ``services/go-server/data/proofs/YYYY-MM-DD``.
  * Emits soak telemetry with contiguous runtime, cycle counts, harvested frames,
    success streak, and 72-hour / 14-day bin coverage.

The service is designed for unattended staging operation: state is persisted
across cycles, cycles are idempotent at the database level, and a partially
failed cycle does not terminate the daemon.

Adheres strictly to AGENTS.md:
  * Rule 1: English-only, zero-emoji code and logs.
  * Rule 2: Root-cause handling of upstream failures, schema migrations, and
    interrupted runs instead of silent success.
  * Rule 8: No credentials in code. The optional dev token is read from the
    process environment and never logged.
  * Rule 10: Telemetry describes only observed live runs; no synthetic or
    fabricated soak timestamps are produced.

Example:
    python3 tools/harvester/service.py --once
    python3 tools/harvester/service.py --daemon --interval 300
    python3 tools/harvester/service.py --status
    python3 tools/harvester/service.py --health
"""

from __future__ import annotations

import argparse
import hashlib
import json
import logging
import os
import signal
import sqlite3
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from collections import Counter, defaultdict
from concurrent.futures import ThreadPoolExecutor, as_completed
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Any, Dict, List, Mapping, Optional, Sequence, Tuple

REPO_ROOT = Path(__file__).resolve().parents[2]
if str(REPO_ROOT) not in sys.path:
    sys.path.insert(0, str(REPO_ROOT))

try:
    from tools.eval.ingest_proof_archive import (  # type: ignore[import-not-found]
        CAPACITY_FLOOR,
        CVEngineClient,
        VerdictRecord,
        analyze_frame,
        connect_database,
        ensure_history_schema,
        format_timestamp,
        insert_verdict_batch,
        load_camera_capacities,
        load_dotenv,
        load_roadway_regions,
        parse_timestamp,
        to_rfc3339nano,
        validate_jpeg_bytes,
        write_json_atomic,
    )
except ImportError:
    sys.path.insert(0, str(REPO_ROOT))
    from tools.eval.ingest_proof_archive import (  # type: ignore[import-not-found]
        CAPACITY_FLOOR,
        CVEngineClient,
        VerdictRecord,
        analyze_frame,
        connect_database,
        ensure_history_schema,
        format_timestamp,
        insert_verdict_batch,
        load_camera_capacities,
        load_dotenv,
        load_roadway_regions,
        parse_timestamp,
        to_rfc3339nano,
        validate_jpeg_bytes,
        write_json_atomic,
    )

LOG = logging.getLogger("harvester.service")

DEFAULT_CAMERAS_FILE = REPO_ROOT / "services" / "go-server" / "cameras_enriched.json"
FALLBACK_CAMERAS_FILE = REPO_ROOT / "cameras_enriched.json"
DEFAULT_DB_PATH = REPO_ROOT / "services" / "go-server" / "data" / "camera-view.db"
DEFAULT_PROOF_ROOT = REPO_ROOT / "services" / "go-server" / "data" / "proofs"
DEFAULT_ROADWAY_REGIONS_DIR = REPO_ROOT / "data" / "roadway_regions"
DEFAULT_TELEMETRY_OUT = REPO_ROOT / "docs" / "proofs" / "sprint11" / "harvester_soak_status.json"
DEFAULT_STATE_PATH = REPO_ROOT / "data" / "harvester" / "soak_state.json"
DEFAULT_CV_ENGINE_URL = "http://127.0.0.1:8000"
DEFAULT_INTERVAL_SECONDS = 300
DEFAULT_COVERAGE_WINDOW_HOURS = 336
DEFAULT_BATCH_SIZE = 500
DEFAULT_CONCURRENCY = 8
DEFAULT_TIMEOUT_SECONDS = 15.0
COVERAGE_BIN_MINUTES = 10
ICT = timezone(timedelta(hours=7))

# Canonical Hot Set roster size, matching Go's buildFixedHotSet.
MAX_HOTSET_CAMERAS = 57

# Frames per camera are compared against the previous accepted frame inside this
# process so a frozen upstream feed cannot be recorded as fresh observations.
_LAST_FRAME_SHA256: Dict[str, str] = {}

_PROCESS_STARTED_MONOTONIC = time.monotonic()
_PROCESS_STARTED_AT = datetime.now(timezone.utc)


def process_uptime_seconds() -> float:
    """Return the lifetime of the current process in seconds."""
    return max(0.0, time.monotonic() - _PROCESS_STARTED_MONOTONIC)


def reset_frame_history() -> None:
    """Forget per-camera frame hashes (used when a new daemon run starts)."""
    _LAST_FRAME_SHA256.clear()

PRIORITY_CORRIDORS = {
    "vo_van_kiet",
    "mai_chi_tho",
    "dien_bien_phu",
    "cong_hoa",
}

UPSTREAM_SNAPSHOT_BASE = "https://giaothong.hochiminhcity.gov.vn/render/ImageHandler.ashx"
MAX_CYCLE_HISTORY = 20
MAX_ERROR_SAMPLES = 12


# ---------------------------------------------------------------------------
# Time and state helpers
# ---------------------------------------------------------------------------


def utcnow() -> datetime:
    """Return the current time as an aware UTC datetime."""
    return datetime.now(timezone.utc)


def _state_timestamp(value: Any) -> Optional[datetime]:
    return parse_timestamp(value)


def default_state(now: datetime) -> Dict[str, Any]:
    """Return a zeroed soak state anchored at now."""
    return {
        "daemon_started_at": format_timestamp(now),
        "daemon_pid": os.getpid(),
        "clean_shutdown": True,
        "last_run_mode": "",
        "crash_count": 0,
        "completed_cycles": 0,
        "failed_cycles": 0,
        "total_frames_harvested": 0,
        "consecutive_successful_cycles": 0,
        "unbroken_soak_seconds": 0.0,
        "soak_reset_count": 0,
        "longest_unbroken_soak_seconds": 0.0,
        "last_cycle_timestamp": "",
        "last_error": "",
        "last_cycle": {},
        "cycle_history": [],
    }


def load_state(path: Path, now: datetime) -> Tuple[Dict[str, Any], bool]:
    """Load persisted soak state and report whether the file existed."""
    base = default_state(now)
    if not path.is_file():
        return base, False
    try:
        with path.open("r", encoding="utf-8") as handle:
            payload = json.load(handle)
    except (OSError, json.JSONDecodeError) as exc:
        LOG.warning("Cannot read soak state %s: %s", path, exc)
        return base, False
    if not isinstance(payload, dict):
        return base, False
    for key, value in payload.items():
        base[key] = value
    if "daemon_pid" not in payload:
        # Legacy state has no recorded owner. Do not claim the current process
        # (which may be a short-lived --health observer) owns the soak window.
        base["daemon_pid"] = 0
    if "unbroken_soak_seconds" not in payload:
        base["unbroken_soak_seconds"] = 0.0
    return base, True


def reset_soak_window(state: Mapping[str, Any], now: datetime, reason: str) -> Dict[str, Any]:
    """Reset contiguous soak and streak counters after an interruption."""
    updated = dict(state)
    updated["daemon_started_at"] = format_timestamp(now)
    updated["daemon_pid"] = os.getpid()
    updated["unbroken_soak_seconds"] = 0.0
    updated["soak_reset_count"] = int(updated.get("soak_reset_count", 0) or 0) + 1
    updated["consecutive_successful_cycles"] = 0
    updated["last_error"] = reason
    return updated


def prepare_state_for_run(
    state: Dict[str, Any],
    existed: bool,
    now: datetime,
    interval_seconds: int,
) -> Tuple[Dict[str, Any], List[str]]:
    """Detect interrupted or stale runs and restart the soak window for this process."""
    notes: List[str] = []
    prepared = dict(state)
    prior_start = _state_timestamp(prepared.get("daemon_started_at"))

    if existed and not bool(prepared.get("clean_shutdown", True)):
        prepared = reset_soak_window(
            prepared,
            now,
            "previous run ended without a clean shutdown; soak window reset",
        )
        prepared["crash_count"] = int(prepared.get("crash_count", 0)) + 1
        notes.append("interrupted_run_detected")

    last_cycle = _state_timestamp(prepared.get("last_cycle_timestamp"))
    maximum_gap = max(3 * max(1, interval_seconds), 900)
    if last_cycle is not None:
        gap_seconds = (now - last_cycle).total_seconds()
        if gap_seconds > maximum_gap and prepared.get("daemon_started_at"):
            prepared = reset_soak_window(
                prepared,
                now,
                f"harvester was inactive for {int(gap_seconds)} seconds; soak window reset",
            )
            notes.append("inactivity_gap_detected")

    # A soak window belongs to one daemon process. Every start or restart anchors
    # it at the new process start and zeroes the accumulated soak.
    prepared["daemon_started_at"] = format_timestamp(now)
    prepared["daemon_pid"] = os.getpid()
    prepared["unbroken_soak_seconds"] = 0.0
    if existed:
        prepared["soak_reset_count"] = int(prepared.get("soak_reset_count", 0) or 0) + 1
        notes.append("soak_window_reset_on_process_start")
    elif prior_start is not None:
        notes.append("soak_window_reset_on_process_start")

    prepared["clean_shutdown"] = False
    return prepared, notes


def apply_cycle_to_state(state: Dict[str, Any], summary: Mapping[str, Any]) -> Dict[str, Any]:
    """Fold one cycle summary into durable soak state."""
    updated = dict(state)
    updated["completed_cycles"] = int(updated.get("completed_cycles", 0)) + 1
    harvested = int(summary.get("harvested_frames", 0) or 0)
    updated["total_frames_harvested"] = int(updated.get("total_frames_harvested", 0)) + harvested

    if bool(summary.get("success", False)):
        updated["consecutive_successful_cycles"] = int(updated.get("consecutive_successful_cycles", 0)) + 1
    else:
        updated["consecutive_successful_cycles"] = 0
        updated["failed_cycles"] = int(updated.get("failed_cycles", 0)) + 1

    updated["last_cycle_timestamp"] = str(summary.get("completed_at", ""))
    error_text = str(summary.get("error", "") or "")
    if not error_text and summary.get("errors"):
        first_error = summary["errors"][0]
        if isinstance(first_error, Mapping):
            error_text = str(first_error.get("error", ""))
        else:
            error_text = str(first_error)
    updated["last_error"] = error_text

    last_cycle = {
        "started_at": summary.get("started_at", ""),
        "completed_at": summary.get("completed_at", ""),
        "duration_seconds": summary.get("duration_seconds", 0.0),
        "cameras_total": summary.get("cameras_total", 0),
        "cameras_harvested": summary.get("cameras_harvested", 0),
        "cameras_failed": summary.get("cameras_failed", 0),
        "frames_analyzed": summary.get("frames_analyzed", 0),
        "harvested_frames": harvested,
        "records_attempted": summary.get("records_attempted", 0),
        "records_inserted": summary.get("records_inserted", 0),
        "methods": summary.get("methods", {}),
        "success": bool(summary.get("success", False)),
        "error": error_text,
    }
    updated["last_cycle"] = last_cycle

    history = list(updated.get("cycle_history", []))
    history.append(last_cycle)
    updated["cycle_history"] = history[-MAX_CYCLE_HISTORY:]

    started_at = _state_timestamp(updated.get("daemon_started_at"))
    if started_at is not None:
        completed_at = _state_timestamp(updated.get("last_cycle_timestamp")) or utcnow()
        elapsed = max(0.0, (completed_at - started_at).total_seconds())
        updated["longest_unbroken_soak_seconds"] = max(
            float(updated.get("longest_unbroken_soak_seconds", 0.0) or 0.0),
            elapsed,
        )
    updated["unbroken_soak_seconds"] = round(unbroken_soak_seconds(updated, utcnow()), 3)
    return updated


def unbroken_soak_seconds(state: Mapping[str, Any], now: datetime) -> float:
    """Compute contiguous runtime for the daemon process that owns this state.

    The value is bounded by the lifetime of the current process when this call
    runs inside the daemon itself, so a stale ``daemon_started_at`` can never
    inflate the soak after a restart.
    """
    started_at = _state_timestamp(state.get("daemon_started_at"))
    if started_at is None:
        return 0.0
    wall_seconds = max(0.0, (now - started_at).total_seconds())
    try:
        owner_pid = int(state.get("daemon_pid") or 0)
    except (TypeError, ValueError):
        owner_pid = 0
    if owner_pid == os.getpid():
        return min(wall_seconds, process_uptime_seconds())
    return wall_seconds


# ---------------------------------------------------------------------------
# Camera catalog
# ---------------------------------------------------------------------------


def resolve_cameras_file(path: Path) -> Path:
    """Resolve the configured catalog path with the repository-root fallback."""
    if path.is_file():
        return path
    if FALLBACK_CAMERAS_FILE.is_file():
        LOG.info("Configured camera catalog not found; using %s", FALLBACK_CAMERAS_FILE)
        return FALLBACK_CAMERAS_FILE
    raise FileNotFoundError(f"camera catalog not found: {path}")


def _load_catalog_payload(path: Path) -> Tuple[List[Dict[str, Any]], Optional[Sequence[str]]]:
    with path.open("r", encoding="utf-8") as handle:
        payload = json.load(handle)
    if isinstance(payload, list):
        return [item for item in payload if isinstance(item, dict)], None
    if isinstance(payload, dict):
        items = payload.get("cameras", [])
        hotset_ids = payload.get("hotset_camera_ids")
        cameras = [item for item in items if isinstance(item, dict)] if isinstance(items, list) else []
        if isinstance(hotset_ids, list):
            return cameras, [str(item) for item in hotset_ids]
        return cameras, None
    return [], None


def load_hotset_cameras(path: Path) -> List[Dict[str, Any]]:
    """Select online, non-ghost Hot Set cameras from the enriched catalog.

    Explicit ``hotset`` flags or a top-level ``hotset_camera_ids`` list take
    precedence. Legacy catalogs fall back to the canonical 57-camera corridor
    filter used by the Go server and the Sprint 7 harvester.
    """
    cameras, hotset_ids = _load_catalog_payload(path)
    explicit_flags = any("hotset" in camera for camera in cameras)
    allowed_ids = set(hotset_ids) if hotset_ids else None

    selected: List[Dict[str, Any]] = []
    seen: set[str] = set()
    for camera in cameras:
        camera_id = str(camera.get("id") or "").strip()
        if not camera_id or camera_id in seen:
            continue
        if bool(camera.get("isGhost")) or bool(camera.get("duplicateOf")):
            continue
        if not (camera.get("isOnline") or str(camera.get("status", "")).upper() == "UP"):
            continue

        if allowed_ids is not None:
            if camera_id not in allowed_ids:
                continue
        elif explicit_flags:
            if not bool(camera.get("hotset")):
                continue
        else:
            road_keys = camera.get("road_keys")
            if not isinstance(road_keys, list) or len(road_keys) != 2:
                continue
            if not any(str(key) in PRIORITY_CORRIDORS for key in road_keys):
                continue

        selected.append(camera)
        seen.add(camera_id)
        if len(selected) >= MAX_HOTSET_CAMERAS:
            break

    return selected[:MAX_HOTSET_CAMERAS]


# ---------------------------------------------------------------------------
# Snapshot acquisition and archival
# ---------------------------------------------------------------------------


def _cache_busted(url: str, timestamp_ms: int) -> str:
    if url.startswith("file:"):
        return url
    separator = "&" if "?" in url else "?"
    return f"{url}{separator}t={timestamp_ms}"


def build_snapshot_urls(camera: Mapping[str, Any], proxy_base: str) -> List[str]:
    """Build the ordered candidate snapshot URLs for a camera."""
    camera_id = str(camera.get("id") or "")
    timestamp_ms = int(time.time() * 1000)
    urls: List[str] = []

    if proxy_base:
        proxy_url = f"{proxy_base.rstrip('/')}/{urllib.parse.quote(camera_id, safe='')}"
        urls.append(_cache_busted(proxy_url, timestamp_ms))

    snapshot_url = str(camera.get("snapshotUrl") or "").strip()
    if snapshot_url:
        urls.append(_cache_busted(snapshot_url, timestamp_ms))
    else:
        urls.append(_cache_busted(f"{UPSTREAM_SNAPSHOT_BASE}?id={urllib.parse.quote(camera_id, safe='')}", timestamp_ms))

    ordered: List[str] = []
    seen: set[str] = set()
    for url in urls:
        base = url.split("?", 1)[0]
        if base in seen:
            continue
        seen.add(base)
        ordered.append(url)
    return ordered


def _is_proxy_url(url: str, proxy_base: str) -> bool:
    """Return True when a snapshot URL belongs to the local backend proxy."""
    if not proxy_base:
        return False
    base = url.split("?", 1)[0]
    prefix = proxy_base.rstrip("/") + "/"
    return base.startswith(prefix)


def _dev_access_token() -> str:
    """Read the optional dev token from the environment; never log or persist it."""
    return os.environ.get("DEV_ACCESS_TOKEN", "") or os.environ.get("HARVESTER_DEV_TOKEN", "")


def is_duplicate_frame(camera_id: str, payload: bytes) -> bool:
    """Detect a frozen upstream feed by comparing the previous accepted frame hash."""
    digest = hashlib.sha256(payload).hexdigest()
    if _LAST_FRAME_SHA256.get(camera_id) == digest:
        return True
    _LAST_FRAME_SHA256[camera_id] = digest
    return False


def fetch_snapshot(
    camera: Mapping[str, Any],
    proxy_base: str,
    timeout: float,
) -> Tuple[Optional[bytes], str, str]:
    """Fetch and validate a JPEG snapshot, trying proxy then upstream."""
    dev_token = _dev_access_token()
    last_error = ""
    for url in build_snapshot_urls(camera, proxy_base):
        headers = {
            "User-Agent": "Mozilla/5.0 (compatible; ProjectKVHarvester/1.0)",
            "Accept": "image/jpeg,image/*",
        }
        # Credentials are scoped to the trusted local proxy. They must never be
        # sent to the public upstream portal.
        if dev_token and _is_proxy_url(url, proxy_base):
            headers["X-Dev-Token"] = dev_token
        request = urllib.request.Request(url, headers=headers, method="GET")
        try:
            with urllib.request.urlopen(request, timeout=timeout) as response:
                if response.status != 200:
                    last_error = f"HTTP {response.status}"
                    continue
                payload = response.read()
        except (urllib.error.URLError, TimeoutError, ConnectionError, OSError) as exc:
            last_error = str(exc)
            continue

        valid, reason = validate_jpeg_bytes(payload)
        if not valid:
            last_error = f"invalid snapshot ({reason})"
            continue
        return payload, url.split("?", 1)[0], ""

    return None, "", last_error or "snapshot unavailable"


def archive_snapshot(
    payload: bytes,
    proof_root: Path,
    camera_id: str,
    timestamp: int,
) -> Tuple[Path, bool]:
    """Persist a snapshot with an atomic write. Returns (path, created)."""
    date_str = datetime.fromtimestamp(timestamp, tz=ICT).strftime("%Y-%m-%d")
    target = proof_root / date_str / camera_id / f"{timestamp}_sample.jpg"
    if target.is_file():
        return target, False
    target.parent.mkdir(parents=True, exist_ok=True)
    temporary = target.with_name(target.name + ".tmp")
    with temporary.open("wb") as handle:
        handle.write(payload)
    os.replace(temporary, target)
    return target, True


# ---------------------------------------------------------------------------
# Cycle execution
# ---------------------------------------------------------------------------


def harvest_camera(
    camera: Mapping[str, Any],
    args: argparse.Namespace,
    capacities: Mapping[str, float],
    regions_cache: Mapping[str, Sequence[Any]],
    cv_client: Optional[CVEngineClient],
) -> Dict[str, Any]:
    """Fetch, analyze, and optionally archive one camera frame."""
    camera_id = str(camera.get("id") or "")
    result: Dict[str, Any] = {
        "camera_id": camera_id,
        "success": False,
        "error": "",
        "method": "",
        "records": [],
        "archived": False,
        "would_archive": False,
        "archive_path": "",
        "bytes": 0,
    }

    payload, source_url, error = fetch_snapshot(camera, args.snapshot_proxy_url, args.timeout)
    if payload is None:
        result["error"] = f"snapshot_failed: {error}"
        return result

    if is_duplicate_frame(camera_id, payload):
        result["error"] = "duplicate_frame: upstream frame is identical to the previous cycle"
        return result

    captured_at = utcnow()
    timestamp = int(captured_at.timestamp())
    observed_at = to_rfc3339nano(timestamp)
    regions = regions_cache.get(camera_id, [])
    base_capacity = float(capacities.get(camera_id, CAPACITY_FLOOR))

    try:
        records, method = analyze_frame(
            image_bytes=payload,
            camera_id=camera_id,
            base_capacity=base_capacity,
            regions=regions,
            observed_at=observed_at,
            cv_client=cv_client,
            label_payload=None,
            source="harvester_daemon",
        )
    except Exception as exc:  # Defensive boundary around a single camera.
        result["error"] = f"analysis_failed: {exc}"
        return result

    if not records:
        result["error"] = "analysis_produced_no_records"
        return result

    result["records"] = records
    result["method"] = method
    result["bytes"] = len(payload)
    result["source_url"] = source_url

    if args.dry_run:
        result["would_archive"] = True
        result["success"] = True
        return result

    try:
        archive_path, created = archive_snapshot(payload, Path(args.proof_root), camera_id, timestamp)
    except OSError as exc:
        result["error"] = f"archive_failed: {exc}"
        return result

    result["archived"] = created
    result["archive_path"] = str(archive_path)
    result["success"] = True
    return result


def run_cycle(
    args: argparse.Namespace,
    cameras: Sequence[Mapping[str, Any]],
    capacities: Mapping[str, float],
    regions_cache: Mapping[str, Sequence[Any]],
    cv_client: Optional[CVEngineClient],
    db_connection: Optional[sqlite3.Connection],
) -> Dict[str, Any]:
    """Run one full harvest cycle across all configured cameras."""
    started_at = utcnow()
    started_monotonic = time.monotonic()
    errors: List[Dict[str, str]] = []
    records: List[VerdictRecord] = []
    methods: Counter[str] = Counter()
    harvested = 0
    analyzed = 0

    with ThreadPoolExecutor(max_workers=max(1, int(args.concurrency))) as executor:
        futures = {
            executor.submit(harvest_camera, camera, args, capacities, regions_cache, cv_client): camera
            for camera in cameras
        }
        for future in as_completed(futures):
            camera = futures[future]
            camera_id = str(camera.get("id") or "")
            try:
                result = future.result()
            except Exception as exc:  # Defensive boundary around the worker pool.
                result = {
                    "camera_id": camera_id,
                    "success": False,
                    "error": f"unhandled_worker_error: {exc}",
                    "records": [],
                    "method": "",
                }
            if result.get("success"):
                analyzed += 1
                if not args.dry_run:
                    harvested += 1
                methods[str(result.get("method") or "unknown")] += 1
                records.extend(result.get("records", []))
            elif len(errors) < MAX_ERROR_SAMPLES:
                errors.append(
                    {
                        "camera_id": camera_id,
                        "error": str(result.get("error") or "unknown_failure"),
                    }
                )

    attempted = 0
    inserted = 0
    fatal_error = ""
    if not args.dry_run:
        if db_connection is None:
            fatal_error = "database connection is not available"
        else:
            try:
                attempted, inserted = insert_verdict_batch(db_connection, records, args.batch_size)
            except sqlite3.Error as exc:
                fatal_error = f"database_write_failed: {exc}"
                LOG.error("Database write failed for cycle: %s", exc)

    completed_at = utcnow()
    duration = max(0.0, time.monotonic() - started_monotonic)
    success = analyzed > 0 and not fatal_error
    if not success and not fatal_error:
        fatal_error = "no cameras harvested successfully" if cameras else "no cameras configured"

    return {
        "started_at": format_timestamp(started_at),
        "completed_at": format_timestamp(completed_at),
        "duration_seconds": round(duration, 3),
        "cameras_total": len(cameras),
        "cameras_harvested": analyzed,
        "cameras_failed": max(0, len(cameras) - analyzed),
        "frames_analyzed": analyzed,
        "harvested_frames": harvested,
        "records_generated": len(records),
        "records_attempted": attempted,
        "records_inserted": inserted,
        "methods": dict(sorted(methods.items())),
        "errors": errors,
        "success": success,
        "error": fatal_error,
        "dry_run": bool(args.dry_run),
    }


# ---------------------------------------------------------------------------
# Coverage and telemetry
# ---------------------------------------------------------------------------


def _empty_coverage(window_hours: int) -> Dict[str, Any]:
    expected_bins = max(1, int((window_hours * 3600.0) / (COVERAGE_BIN_MINUTES * 60.0)))
    return {
        "window_hours": window_hours,
        "bin_minutes": COVERAGE_BIN_MINUTES,
        "expected_bins_per_camera": expected_bins,
        "observed_bins": 0,
        "expected_bins": 0,
        "coverage_ratio": 0.0,
        "cameras_with_observations": 0,
        "total_observations": 0,
        "longest_gap_seconds": int(window_hours * 3600),
        "last_observation_at": "",
    }


def compute_coverage(
    connection: sqlite3.Connection,
    camera_ids: Sequence[str],
    now: datetime,
    requested_window_hours: int,
) -> Dict[str, Any]:
    """Compute aggregate 10-minute bin coverage for 72-hour and 14-day windows."""
    windows = sorted({72, 336, int(requested_window_hours)})
    maximum_window = max(windows)
    since = now - timedelta(hours=maximum_window)
    since_text = format_timestamp(since)
    now_text = format_timestamp(now)

    rows = connection.execute(
        """
        SELECT camera_id, observed_at
        FROM hotset_verdict_history
        WHERE direction = '' AND observed_at >= ? AND observed_at <= ?
          AND saturation_index IS NOT NULL
        """,
        (since_text, now_text),
    ).fetchall()

    by_camera: Dict[str, List[datetime]] = defaultdict(list)
    wanted = set(camera_ids)
    for camera_id, observed_at in rows:
        camera_key = str(camera_id)
        if wanted and camera_key not in wanted:
            continue
        parsed = parse_timestamp(observed_at)
        if parsed is not None:
            by_camera[camera_key].append(parsed)

    results: Dict[int, Dict[str, Any]] = {}
    for window_hours in windows:
        window_since = now - timedelta(hours=window_hours)
        expected_bins_per_camera = max(1, int((window_hours * 3600.0) / (COVERAGE_BIN_MINUTES * 60.0)))
        # Coverage is always measured against the canonical 57-camera roster, not
        # against whatever subset happens to respond in one cycle.
        roster_size = max(MAX_HOTSET_CAMERAS, len(camera_ids))
        total_expected = expected_bins_per_camera * roster_size
        observed_bins = 0
        total_observations = 0
        cameras_with_observations = 0
        longest_gap = 0
        latest: Optional[datetime] = None

        for camera_id in camera_ids:
            timestamps = sorted(item for item in by_camera.get(camera_id, []) if item >= window_since)
            if not timestamps:
                longest_gap = max(longest_gap, int(window_hours * 3600))
                continue
            cameras_with_observations += 1
            total_observations += len(timestamps)
            latest = max(latest, timestamps[-1]) if latest else timestamps[-1]
            bins: set[int] = set()
            for index, timestamp in enumerate(timestamps):
                bin_index = int((timestamp - window_since).total_seconds() // (COVERAGE_BIN_MINUTES * 60))
                if 0 <= bin_index < expected_bins_per_camera:
                    bins.add(bin_index)
                if index > 0:
                    gap = int((timestamp - timestamps[index - 1]).total_seconds())
                    longest_gap = max(longest_gap, gap)
            observed_bins += len(bins)

        ratio = observed_bins / float(total_expected) if total_expected > 0 else 0.0
        results[window_hours] = {
            "window_hours": window_hours,
            "bin_minutes": COVERAGE_BIN_MINUTES,
            "expected_bins_per_camera": expected_bins_per_camera,
            "observed_bins": observed_bins,
            "expected_bins": total_expected,
            "coverage_ratio": round(min(1.0, ratio), 4),
            "cameras_with_observations": cameras_with_observations,
            "total_observations": total_observations,
            "longest_gap_seconds": longest_gap,
            "last_observation_at": format_timestamp(latest) if latest else "",
        }

    try:
        db_records_total = int(connection.execute("SELECT COUNT(*) FROM hotset_verdict_history").fetchone()[0])
    except (sqlite3.Error, TypeError, IndexError):
        db_records_total = 0

    requested = results[int(requested_window_hours)]
    return {
        "main": requested,
        "windows": results,
        "coverage_72h": results[72]["coverage_ratio"],
        "coverage_14d": results[336]["coverage_ratio"],
        "db_records_total": db_records_total,
    }


def build_telemetry(
    state: Mapping[str, Any],
    cameras_configured: int,
    coverage: Mapping[str, Any],
    args: argparse.Namespace,
) -> Dict[str, Any]:
    """Build the durable soak telemetry document."""
    now = utcnow()
    main_coverage = coverage.get("main", _empty_coverage(args.coverage_window_hours))
    started_at = _state_timestamp(state.get("daemon_started_at"))
    last_cycle_timestamp = str(state.get("last_cycle_timestamp") or format_timestamp(started_at or now))

    windows: Dict[str, Any] = {}
    for hours, payload in sorted(coverage.get("windows", {}).items()):
        windows[f"{hours}h"] = {
            "coverage_ratio": payload["coverage_ratio"],
            "observed_bins": payload["observed_bins"],
            "expected_bins": payload["expected_bins"],
            "cameras_with_observations": payload["cameras_with_observations"],
            "longest_gap_seconds": payload["longest_gap_seconds"],
        }

    payload: Dict[str, Any] = {
        "schema_version": 1,
        "service": "projectkv-hot-set-harvester",
        "source": "harvester_daemon",
        "provenance": "live",
        "generated_at": format_timestamp(now),
        "process_started_at": format_timestamp(_PROCESS_STARTED_AT),
        "process_uptime_seconds": round(process_uptime_seconds(), 3),
        "interval_seconds": int(args.interval),
        "cameras_configured": cameras_configured,
        "daemon_started_at": state.get("daemon_started_at", format_timestamp(now)),
        "unbroken_soak_seconds": round(unbroken_soak_seconds(state, now), 3),
        "longest_unbroken_soak_seconds": round(float(state.get("longest_unbroken_soak_seconds", 0.0) or 0.0), 3),
        "completed_cycles": int(state.get("completed_cycles", 0) or 0),
        "failed_cycles": int(state.get("failed_cycles", 0) or 0),
        "total_frames_harvested": int(state.get("total_frames_harvested", 0) or 0),
        "consecutive_successful_cycles": int(state.get("consecutive_successful_cycles", 0) or 0),
        "last_cycle_timestamp": last_cycle_timestamp,
        "last_error": str(state.get("last_error", "") or ""),
        "coverage_ratio": main_coverage["coverage_ratio"],
        "coverage_window_hours": int(args.coverage_window_hours),
        "coverage_72h": float(coverage.get("coverage_72h", 0.0) or 0.0),
        "coverage_14d": float(coverage.get("coverage_14d", 0.0) or 0.0),
        "coverage_detail": {
            "bin_minutes": COVERAGE_BIN_MINUTES,
            "requested_window_hours": int(args.coverage_window_hours),
            "windows": windows,
            "db_records_total": int(coverage.get("db_records_total", 0) or 0),
        },
        "last_cycle": state.get("last_cycle", {}),
        "cycle_history": list(state.get("cycle_history", []))[-MAX_CYCLE_HISTORY:],
        "last_run_mode": str(state.get("last_run_mode", "") or ""),
        "clean_shutdown": bool(state.get("clean_shutdown", False)),
        "crash_count": int(state.get("crash_count", 0) or 0),
    }
    return payload


def write_telemetry(path: Path, telemetry: Mapping[str, Any]) -> None:
    """Persist telemetry atomically."""
    write_json_atomic(path, telemetry)


# ---------------------------------------------------------------------------
# Status and health
# ---------------------------------------------------------------------------


def open_readonly_database(path: Path) -> Optional[sqlite3.Connection]:
    try:
        if not path.is_file():
            return None
        return connect_database(path, read_only=True)
    except (sqlite3.Error, OSError) as exc:
        LOG.warning("Cannot open database read-only at %s: %s", path, exc)
        return None


def status_snapshot(
    state: Mapping[str, Any],
    cameras_configured: int,
    coverage: Mapping[str, Any],
    args: argparse.Namespace,
) -> Dict[str, Any]:
    return build_telemetry(state, cameras_configured, coverage, args)


def health_check(
    state: Mapping[str, Any],
    state_existed: bool,
    coverage: Mapping[str, Any],
    args: argparse.Namespace,
    now: datetime,
) -> Tuple[bool, List[str]]:
    """Return health status and human-readable failure reasons."""
    reasons: List[str] = []
    if not state_existed:
        reasons.append("soak state file does not exist")

    maximum_age = args.health_max_age_seconds
    if maximum_age is None:
        maximum_age = max(2 * max(1, int(args.interval)), 900)
    last_cycle = _state_timestamp(state.get("last_cycle_timestamp"))
    if last_cycle is None:
        reasons.append("last_cycle_timestamp is missing")
    else:
        age = (now - last_cycle).total_seconds()
        if age > maximum_age:
            reasons.append(f"last cycle is stale ({int(age)}s > {int(maximum_age)}s)")
        if age < -300:
            reasons.append("last_cycle_timestamp is in the future")

    if int(state.get("consecutive_successful_cycles", 0) or 0) < 1:
        reasons.append("consecutive_successful_cycles is below 1")

    if unbroken_soak_seconds(state, now) <= 0:
        reasons.append("unbroken_soak_seconds is not positive")

    if str(state.get("last_run_mode", "")) == "daemon" and bool(state.get("clean_shutdown", False)):
        reasons.append("daemon reports a clean shutdown instead of an active run")

    persisted_soak = float(state.get("unbroken_soak_seconds", 0.0) or 0.0)
    if persisted_soak <= 0:
        reasons.append("unbroken soak was reset at daemon start and no cycle has completed since")

    active_soak = unbroken_soak_seconds(state, now)
    minimum_soak = float(getattr(args, "health_min_soak_seconds", 0.0) or 0.0)
    if active_soak <= 0:
        reasons.append("unbroken_soak_seconds is not positive")
    elif active_soak < minimum_soak:
        reasons.append(
            f"unbroken_soak_seconds {round(active_soak, 3)} is below the minimum {minimum_soak}"
        )

    main_coverage = coverage.get("main", _empty_coverage(args.coverage_window_hours))
    if float(main_coverage.get("coverage_ratio", 0.0)) < float(args.health_min_coverage):
        reasons.append(
            "coverage_ratio "
            f"{main_coverage.get('coverage_ratio', 0.0)} is below {args.health_min_coverage}"
        )

    return len(reasons) == 0, reasons


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------


def build_parser() -> argparse.ArgumentParser:
    """Construct the harvester CLI parser."""
    parser = argparse.ArgumentParser(
        description="24/7 autonomous Hot Set harvester daemon.",
    )
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument("--daemon", action="store_true", help="Run continuously on the configured interval.")
    mode.add_argument("--once", action="store_true", help="Run one harvest cycle and exit.")
    mode.add_argument("--status", action="store_true", help="Print current soak status and exit.")
    mode.add_argument("--health", action="store_true", help="Exit 0 when healthy, 1 otherwise.")

    parser.add_argument("--interval", type=int, default=DEFAULT_INTERVAL_SECONDS, help="Cycle interval in seconds.")
    parser.add_argument("--dry-run", action="store_true", help="Run a cycle without writing files or database rows.")
    parser.add_argument(
        "--cameras-file",
        default=str(DEFAULT_CAMERAS_FILE),
        help="Camera catalog path (fallback: repository-root cameras_enriched.json).",
    )
    parser.add_argument(
        "--db-path",
        default=str(DEFAULT_DB_PATH),
        help="SQLite database path (default: services/go-server/data/camera-view.db).",
    )
    parser.add_argument(
        "--proof-root",
        "--proof-dir",
        dest="proof_root",
        default=str(DEFAULT_PROOF_ROOT),
        help="Proof frame archive root (default: services/go-server/data/proofs).",
    )
    parser.add_argument(
        "--roadway-regions-dir",
        default=str(DEFAULT_ROADWAY_REGIONS_DIR),
        help="Directional roadway polygon directory.",
    )
    parser.add_argument(
        "--telemetry-out",
        default=str(DEFAULT_TELEMETRY_OUT),
        help="Soak telemetry output path.",
    )
    parser.add_argument(
        "--state-path",
        default=str(DEFAULT_STATE_PATH),
        help="Persistent soak state path (default: data/harvester/soak_state.json).",
    )
    parser.add_argument(
        "--cv-engine-url",
        default=os.environ.get("CV_ENGINE_URL", DEFAULT_CV_ENGINE_URL),
        help="CV engine base URL. Set an empty string to force the deterministic fallback.",
    )
    parser.add_argument(
        "--snapshot-proxy-url",
        default=os.environ.get("HARVESTER_SNAPSHOT_PROXY_URL", ""),
        help="Optional local go-server snapshot proxy base URL.",
    )
    parser.add_argument("--timeout", type=float, default=DEFAULT_TIMEOUT_SECONDS, help="Snapshot timeout in seconds.")
    parser.add_argument("--concurrency", type=int, default=DEFAULT_CONCURRENCY, help="Concurrent camera workers.")
    parser.add_argument("--batch-size", type=int, default=DEFAULT_BATCH_SIZE, help="SQLite insert batch size.")
    parser.add_argument(
        "--coverage-window-hours",
        type=int,
        default=DEFAULT_COVERAGE_WINDOW_HOURS,
        help="Primary coverage window in hours (default: 336 = 14 days).",
    )
    parser.add_argument(
        "--health-max-age-seconds",
        type=int,
        default=None,
        help="Maximum age of the last successful cycle for --health.",
    )
    parser.add_argument(
        "--health-min-coverage",
        type=float,
        default=0.90,
        help="Minimum primary coverage ratio required by --health (default: 0.90 for 14 days).",
    )
    parser.add_argument(
        "--health-min-soak-seconds",
        type=float,
        default=0.0,
        help="Minimum active unbroken soak required by --health.",
    )
    return parser


def _load_regions_cache(cameras: Sequence[Mapping[str, Any]], regions_dir: Path) -> Dict[str, Sequence[Any]]:
    cache: Dict[str, Sequence[Any]] = {}
    for camera in cameras:
        camera_id = str(camera.get("id") or "")
        if camera_id:
            cache[camera_id] = load_roadway_regions(camera_id, regions_dir)
    return cache


def _read_coverage_for_state(
    db_path: Path,
    camera_ids: Sequence[str],
    args: argparse.Namespace,
    now: datetime,
) -> Dict[str, Any]:
    connection = open_readonly_database(db_path)
    if connection is None:
        return {
            "main": _empty_coverage(args.coverage_window_hours),
            "windows": {
                72: _empty_coverage(72),
                336: _empty_coverage(336),
            },
            "coverage_72h": 0.0,
            "coverage_14d": 0.0,
            "db_records_total": 0,
        }
    try:
        return compute_coverage(connection, camera_ids, now, args.coverage_window_hours)
    except sqlite3.Error as exc:
        LOG.warning("Cannot compute coverage: %s", exc)
        return {
            "main": _empty_coverage(args.coverage_window_hours),
            "windows": {
                72: _empty_coverage(72),
                336: _empty_coverage(336),
            },
            "coverage_72h": 0.0,
            "coverage_14d": 0.0,
            "db_records_total": 0,
        }
    finally:
        connection.close()


def check_database_health(path: Path) -> Tuple[bool, str]:
    """Verify the SQLite file exists and carries the migration-0008 columns."""
    if not path.is_file():
        return False, "database file does not exist"
    required = {
        "camera_id",
        "direction",
        "region_id",
        "pcu_total",
        "saturation_index",
        "severity",
        "confidence",
        "source",
        "observed_at",
        "recorded_at",
    }
    try:
        connection = connect_database(path, read_only=True)
    except (sqlite3.Error, OSError) as exc:
        return False, f"database cannot be opened read-only: {exc}"
    try:
        rows = connection.execute("PRAGMA table_info(hotset_verdict_history)").fetchall()
        columns = {str(row[1]) for row in rows}
        if not columns:
            return False, "hotset_verdict_history table is missing"
        missing = required.difference(columns)
        if missing:
            return False, f"hotset_verdict_history is missing columns: {sorted(missing)}"
        return True, ""
    except sqlite3.Error as exc:
        return False, f"database schema check failed: {exc}"
    finally:
        connection.close()


def _print_json(payload: Mapping[str, Any]) -> None:
    print(json.dumps(payload, indent=2, ensure_ascii=False))


def _sleep_until(target_monotonic: float, stop_requested: List[bool]) -> None:
    while not stop_requested[0]:
        remaining = target_monotonic - time.monotonic()
        if remaining <= 0:
            return
        time.sleep(min(remaining, 1.0))


def main(argv: Optional[Sequence[str]] = None) -> int:
    """CLI entry point."""
    load_dotenv()
    reset_frame_history()
    parser = build_parser()
    args = parser.parse_args(argv)

    if args.interval <= 0:
        parser.error("--interval must be greater than zero")
    if args.concurrency <= 0:
        parser.error("--concurrency must be greater than zero")
    if args.batch_size <= 0:
        parser.error("--batch-size must be greater than zero")
    if args.coverage_window_hours <= 0:
        parser.error("--coverage-window-hours must be greater than zero")

    logging.basicConfig(
        level=os.environ.get("LOG_LEVEL", "INFO").upper(),
        format="%(asctime)s [%(levelname)s] %(message)s",
        datefmt="%Y-%m-%d %H:%M:%S",
    )

    state_path = Path(args.state_path)
    telemetry_path = Path(args.telemetry_out)
    db_path = Path(args.db_path)
    now = utcnow()

    try:
        cameras_file = resolve_cameras_file(Path(args.cameras_file))
        cameras = load_hotset_cameras(cameras_file)
    except (FileNotFoundError, json.JSONDecodeError, OSError) as exc:
        LOG.error("Cannot load camera catalog: %s", exc)
        return 1

    if not cameras:
        LOG.error("No online Hot Set cameras were selected from %s", cameras_file)
        return 1

    state, state_existed = load_state(state_path, now)
    coverage = _read_coverage_for_state(db_path, [str(c["id"]) for c in cameras], args, now)

    if args.status:
        _print_json(status_snapshot(state, len(cameras), coverage, args))
        return 0

    if args.health:
        healthy, reasons = health_check(state, state_existed, coverage, args, now)
        database_ok, database_reason = check_database_health(db_path)
        if not database_ok:
            healthy = False
            reasons.insert(0, database_reason)
        _print_json(
            {
                "healthy": healthy,
                "checked_at": format_timestamp(now),
                "reasons": reasons,
                "completed_cycles": int(state.get("completed_cycles", 0) or 0),
                "consecutive_successful_cycles": int(state.get("consecutive_successful_cycles", 0) or 0),
                "last_cycle_timestamp": state.get("last_cycle_timestamp", ""),
                "coverage_ratio": coverage.get("main", {}).get("coverage_ratio", 0.0),
                "unbroken_soak_seconds": round(unbroken_soak_seconds(state, now), 3),
                "soak_reset_count": int(state.get("soak_reset_count", 0) or 0),
            }
        )
        return 0 if healthy else 1

    capacities = load_camera_capacities(cameras_file)
    regions_cache = _load_regions_cache(cameras, Path(args.roadway_regions_dir))
    cv_client = CVEngineClient(args.cv_engine_url, timeout=args.timeout) if args.cv_engine_url else None

    db_connection: Optional[sqlite3.Connection] = None
    if not args.dry_run:
        try:
            db_connection = connect_database(db_path, read_only=False)
            schema_report = ensure_history_schema(db_connection)
            LOG.info(
                "Database ready: %s (created_table=%s added_columns=%s)",
                db_path,
                schema_report["created_table"],
                schema_report["added_columns"],
            )
        except (sqlite3.Error, OSError) as exc:
            LOG.error("Cannot prepare database %s: %s", db_path, exc)
            return 1

    prepared_state, preparation_notes = prepare_state_for_run(state, state_existed, utcnow(), args.interval)
    prepared_state["last_run_mode"] = "once" if args.once else "daemon"
    if preparation_notes:
        LOG.warning("State preparation: %s", ", ".join(preparation_notes))

    stop_requested = [False]

    def request_stop(signum: int, _frame: Any) -> None:
        LOG.info("Received signal %s; stopping after the current cycle", signum)
        stop_requested[0] = True

    previous_handlers: Dict[int, Any] = {}
    for signum in (signal.SIGINT, signal.SIGTERM):
        try:
            previous_handlers[signum] = signal.getsignal(signum)
            signal.signal(signum, request_stop)
        except (ValueError, OSError):
            continue

    exit_code = 0
    try:
        if args.once:
            if not args.dry_run:
                write_json_atomic(state_path, prepared_state)
            try:
                summary = run_cycle(args, cameras, capacities, regions_cache, cv_client, db_connection)
            except Exception as exc:  # A cycle failure must not masquerade as success.
                LOG.exception("Harvest cycle failed")
                summary = {
                    "started_at": format_timestamp(utcnow()),
                    "completed_at": format_timestamp(utcnow()),
                    "duration_seconds": 0.0,
                    "cameras_total": len(cameras),
                    "cameras_harvested": 0,
                    "cameras_failed": len(cameras),
                    "frames_analyzed": 0,
                    "harvested_frames": 0,
                    "records_generated": 0,
                    "records_attempted": 0,
                    "records_inserted": 0,
                    "methods": {},
                    "errors": [{"camera_id": "", "error": str(exc)}],
                    "success": False,
                    "error": str(exc),
                }

            prepared_state = apply_cycle_to_state(prepared_state, summary)
            if not args.dry_run:
                prepared_state["clean_shutdown"] = True
                updated_coverage = _read_coverage_for_state(
                    db_path, [str(c["id"]) for c in cameras], args, utcnow()
                )
                write_json_atomic(state_path, prepared_state)
                write_telemetry(
                    telemetry_path,
                    build_telemetry(prepared_state, len(cameras), updated_coverage, args),
                )
            else:
                _print_json(summary)
            LOG.info(
                "Cycle complete: success=%s harvested=%d failed=%d records_inserted=%d duration=%.2fs",
                summary.get("success"),
                summary.get("cameras_harvested", 0),
                summary.get("cameras_failed", 0),
                summary.get("records_inserted", 0),
                float(summary.get("duration_seconds", 0.0)),
            )
            exit_code = 0 if summary.get("success") else 1

        else:
            if not args.dry_run:
                write_json_atomic(state_path, prepared_state)

            latest_coverage = coverage
            next_cycle_monotonic = time.monotonic()
            while not stop_requested[0]:
                try:
                    summary = run_cycle(args, cameras, capacities, regions_cache, cv_client, db_connection)
                except Exception as exc:  # Keep the daemon alive and record the failure.
                    LOG.exception("Harvest cycle failed")
                    summary = {
                        "started_at": format_timestamp(utcnow()),
                        "completed_at": format_timestamp(utcnow()),
                        "duration_seconds": 0.0,
                        "cameras_total": len(cameras),
                        "cameras_harvested": 0,
                        "cameras_failed": len(cameras),
                        "frames_analyzed": 0,
                        "harvested_frames": 0,
                        "records_generated": 0,
                        "records_attempted": 0,
                        "records_inserted": 0,
                        "methods": {},
                        "errors": [{"camera_id": "", "error": str(exc)}],
                        "success": False,
                        "error": str(exc),
                    }

                prepared_state = apply_cycle_to_state(prepared_state, summary)
                if not args.dry_run:
                    prepared_state["clean_shutdown"] = False
                    cycle_now = utcnow()
                    latest_coverage = _read_coverage_for_state(
                        db_path, [str(c["id"]) for c in cameras], args, cycle_now
                    )
                    write_json_atomic(state_path, prepared_state)
                    write_telemetry(
                        telemetry_path,
                        build_telemetry(prepared_state, len(cameras), latest_coverage, args),
                    )
                else:
                    LOG.info(
                        "Dry cycle complete: success=%s harvested=%d failed=%d duration=%.2fs",
                        summary.get("success"),
                        summary.get("cameras_harvested", 0),
                        summary.get("cameras_failed", 0),
                        float(summary.get("duration_seconds", 0.0)),
                    )

                LOG.info(
                    "Soak status: cycles=%d streak=%d harvested=%d coverage_14d=%.4f",
                    prepared_state.get("completed_cycles", 0),
                    prepared_state.get("consecutive_successful_cycles", 0),
                    prepared_state.get("total_frames_harvested", 0),
                    latest_coverage.get("coverage_14d", 0.0),
                )

                next_cycle_monotonic += max(1.0, float(args.interval))
                if next_cycle_monotonic < time.monotonic():
                    # The cycle overran its interval. Skip missed slots rather than
                    # firing a burst of back-to-back harvests.
                    next_cycle_monotonic = time.monotonic() + max(1.0, float(args.interval))
                _sleep_until(next_cycle_monotonic, stop_requested)
    finally:
        for signum, handler in previous_handlers.items():
            try:
                signal.signal(signum, handler)
            except (ValueError, OSError):
                continue
        if db_connection is not None:
            db_connection.close()
        if not args.dry_run and not args.once:
            prepared_state["clean_shutdown"] = True
            try:
                write_json_atomic(state_path, prepared_state)
                final_coverage = _read_coverage_for_state(
                    db_path, [str(c["id"]) for c in cameras], args, utcnow()
                )
                write_telemetry(
                    telemetry_path,
                    build_telemetry(prepared_state, len(cameras), final_coverage, args),
                )
            except OSError as exc:
                LOG.error("Cannot persist final soak state: %s", exc)

    return exit_code


if __name__ == "__main__":
    sys.exit(main())
