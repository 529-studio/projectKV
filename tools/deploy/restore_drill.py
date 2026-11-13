#!/usr/bin/env python3
"""restore_drill.py - automated SQLite disaster-recovery drill (Issue #442 / S14-T2).

The staging volume holds the only copy of the traffic database until Litestream
replicates it. A backup nobody has restored is a hypothesis, not a recovery
plan, so this tool restores the database into an isolated sandbox and proves the
restored copy is usable:

  1. Restore ``camera-view.db`` from a Litestream replica, or unpack a local
     replica built by the hermetic simulator when Litestream or R2 credentials
     are unavailable.
  2. Run ``PRAGMA integrity_check``, ``PRAGMA quick_check`` and
     ``PRAGMA foreign_key_check`` against the restored file.
  3. Compare record counts (verdicts, verdict history, subscriptions) and record
     freshness against the source database, and measure the recovery point
     objective.
  4. Measure the recovery time objective (restore wall clock) and enforce both
     budgets (RTO 60 s, RPO 10 s).
  5. Emit a structured JSON summary and exit non-zero on any failed check.

Modes
-----
``simulator``  Hermetic. Builds a WAL-mode SQLite database, writes a base
               snapshot plus periodic WAL captures into a local replica
               directory, then restores that replica into the sandbox. Exercises
               the same restore-and-validate code path without network access,
               so CI can run it on every change.
``live``       Restores an existing Litestream replica (``--replica-url``, or
               the ``s3://<bucket>/db-replicas/camera-view.db`` target declared
               in ``deploy/litestream/litestream.yml``) with the real
               ``litestream`` binary. ``--start-replication`` additionally runs
               ``litestream replicate`` against a sandbox copy of
               ``--source-db`` while heartbeats are written, which turns the
               drill into an end-to-end replication test rather than a restore
               of a replica somebody else produced.
``auto``       ``live`` when an explicit ``--replica-url`` (or
               ``RESTORE_DRILL_REPLICA_URL``) and the Litestream binary are both
               available, otherwise ``simulator``. A bucket name alone is not
               enough: auto mode must never reach a production bucket by
               accident. The chosen mode is always recorded in the summary,
               never implied.

Everything in the summary comes from the run that produced it: timestamps are
the restored database's own records, RTO is a measured wall clock, and no
number is estimated or carried over from a previous run.

Usage::

    python3 tools/deploy/restore_drill.py
    python3 tools/deploy/restore_drill.py --mode simulator --json-stdout
    python3 tools/deploy/restore_drill.py --mode live \
        --replica-url s3://warded-proofs/db-replicas/camera-view.db
    python3 tools/deploy/restore_drill.py --mode live --start-replication \
        --source-db /tmp/drill/camera-view.db --replica-url file:///tmp/drill/replica

Exit status: 0 when every check passes, 1 otherwise.
"""

from __future__ import annotations

import argparse
import json
import os
import re
import shutil
import sqlite3
import subprocess
import sys
import tempfile
import threading
import time
from dataclasses import dataclass, field
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Any, Iterable, Sequence

REPO_ROOT = Path(__file__).resolve().parents[2]
DEFAULT_CONFIG = REPO_ROOT / "deploy" / "litestream" / "litestream.yml"
DEFAULT_OUT = REPO_ROOT / "docs" / "reports" / "s14_t2_restore_drill_summary.json"
LITESTREAM_IMAGE = "litestream/litestream:0.3.13"

DEFAULT_RTO_BUDGET_SECONDS = 60.0
DEFAULT_RPO_BUDGET_SECONDS = 10.0
DEFAULT_SYNC_INTERVAL_SECONDS = 1.0
DEFAULT_CYCLES = 6
DEFAULT_REPLICATE_SECONDS = 12.0

EXIT_PASS = 0
EXIT_FAIL = 1


# ---------------------------------------------------------------------------
# Time helpers
# ---------------------------------------------------------------------------

_TS_RE = re.compile(
    r"^\s*(\d{4}-\d{2}-\d{2})[T ](\d{2}:\d{2}:\d{2})"
    r"(?:\.(\d{1,9}))?\s*(Z|[+-]\d{2}:?\d{2})?\s*$"
)


def utc_now() -> datetime:
    return datetime.now(timezone.utc)


def iso_utc(moment: datetime) -> str:
    """Microsecond ISO-8601 with an explicit UTC offset (never a naive stamp)."""
    return moment.astimezone(timezone.utc).isoformat(timespec="microseconds").replace("+00:00", "Z")


def parse_timestamp(value: Any) -> datetime | None:
    """Parse the timestamp shapes SQLite stores: ISO-8601, Z, 9-digit fractions."""
    if value is None:
        return None
    if isinstance(value, datetime):
        return value if value.tzinfo else value.replace(tzinfo=timezone.utc)
    text = str(value).strip()
    if not text:
        return None
    match = _TS_RE.match(text)
    if not match:
        return None
    date_part, time_part, fraction, offset = match.groups()
    micros = int((fraction or "0").ljust(6, "0")[:6])
    base = datetime.fromisoformat(f"{date_part}T{time_part}")
    if offset and offset not in ("Z", "z"):
        sign = 1 if offset[0] == "+" else -1
        digits = offset[1:].replace(":", "")
        base = base.replace(tzinfo=timezone(sign * timedelta(hours=int(digits[:2]), minutes=int(digits[2:]))))
    else:
        base = base.replace(tzinfo=timezone.utc)
    return (base + timedelta(microseconds=micros)).astimezone(timezone.utc)


def parse_duration_seconds(value: str | int | float | None) -> float | None:
    """Parse a Litestream duration (``1s``, ``500ms``, ``24h``, ``7d``)."""
    if value is None:
        return None
    if isinstance(value, (int, float)):
        return float(value)
    match = re.fullmatch(r"\s*(\d+(?:\.\d+)?)\s*(ms|s|m|h|d)?\s*", str(value))
    if not match:
        return None
    amount = float(match.group(1))
    unit = match.group(2) or "s"
    return amount * {"ms": 0.001, "s": 1.0, "m": 60.0, "h": 3600.0, "d": 86400.0}[unit]


# ---------------------------------------------------------------------------
# Litestream configuration
# ---------------------------------------------------------------------------

ENV_REF_RE = re.compile(r"\$\{([A-Za-z_][A-Za-z0-9_]*)\}")


@dataclass(frozen=True)
class ReplicaTarget:
    """The parts of ``deploy/litestream/litestream.yml`` the drill depends on."""

    db_path: str
    replica_type: str
    replica_name: str
    bucket: str
    replica_path: str
    endpoint: str
    region: str
    access_key_id: str
    secret_access_key: str
    sync_interval: str
    config_path: Path

    @property
    def sync_interval_seconds(self) -> float | None:
        return parse_duration_seconds(self.sync_interval)

    def env_references(self) -> list[str]:
        references: list[str] = []
        for value in (self.bucket, self.endpoint, self.region, self.access_key_id, self.secret_access_key):
            references.extend(ENV_REF_RE.findall(value))
        return references

    def resolved_bucket(self, env: dict[str, str] | None = None) -> str:
        return _expand_env(self.bucket, env)

    def resolved_replica_url(self, env: dict[str, str] | None = None) -> str | None:
        if self.replica_type != "s3":
            return None
        bucket = self.resolved_bucket(env)
        if not bucket:
            return None
        return f"s3://{bucket}/{self.replica_path.strip('/')}"


def _expand_env(value: str, env: dict[str, str] | None = None) -> str:
    env = os.environ if env is None else env
    return ENV_REF_RE.sub(lambda m: env.get(m.group(1), ""), value.strip().strip("'\""))


def load_replica_target(config_path: Path) -> ReplicaTarget:
    """Parse the single-database Litestream config without a YAML dependency.

    Litestream's configuration is YAML, but this project pins one database with
    one replica, so a line-oriented reader keeps the drill runnable on a stock
    Python install. The parser is strict about what it consumes: unknown
    structure is ignored, and a config without a database path or replica
    raises instead of guessing.
    """
    if not config_path.exists():
        raise FileNotFoundError(f"Litestream config not found: {config_path}")

    text = config_path.read_text(encoding="utf-8")
    db_path: str | None = None
    section = ""
    replica: dict[str, str] = {}

    for raw_line in text.splitlines():
        if not raw_line.strip() or raw_line.lstrip().startswith("#"):
            continue
        stripped = raw_line.strip()
        indent = len(raw_line) - len(raw_line.lstrip())

        if indent == 0:
            section = stripped.rstrip(":").strip() if stripped.endswith(":") else ""
            continue
        if stripped == "replicas:":
            section = "replica"
            continue
        if stripped.startswith("- "):
            stripped = stripped[2:].strip()
            if section == "dbs" and stripped.startswith("path:") and db_path is None:
                db_path = stripped.split(":", 1)[1].strip().strip("'\"")
                continue
            if section == "replica" and stripped.startswith("type:"):
                replica["type"] = stripped.split(":", 1)[1].strip().strip("'\"")
                continue
            if section == "replica" and stripped.startswith("name:"):
                replica["name"] = stripped.split(":", 1)[1].strip().strip("'\"")
                continue
            if section == "replica" and stripped.startswith("path:") and "path" not in replica:
                replica["path"] = stripped.split(":", 1)[1].strip().strip("'\"")
                continue
            continue
        if ":" not in stripped:
            continue
        key, _, value = stripped.partition(":")
        key = key.strip()
        value = value.strip().strip("'\"")
        if section == "replica" and key in {
            "type",
            "name",
            "bucket",
            "endpoint",
            "region",
            "access-key-id",
            "secret-access-key",
            "sync-interval",
            "path",
        }:
            replica.setdefault(key, value)

    if not db_path:
        raise ValueError(f"{config_path}: no database path declared under 'dbs:'")
    if not replica:
        raise ValueError(f"{config_path}: no replica declared for {db_path}")

    return ReplicaTarget(
        db_path=db_path,
        replica_type=replica.get("type", ""),
        replica_name=replica.get("name", ""),
        bucket=replica.get("bucket", ""),
        replica_path=replica.get("path", ""),
        endpoint=replica.get("endpoint", ""),
        region=replica.get("region", ""),
        access_key_id=replica.get("access-key-id", ""),
        secret_access_key=replica.get("secret-access-key", ""),
        sync_interval=replica.get("sync-interval", ""),
        config_path=config_path,
    )


# ---------------------------------------------------------------------------
# Table contract
# ---------------------------------------------------------------------------

@dataclass(frozen=True)
class TableSpec:
    """A business-critical table and the column that proves it is current."""

    logical_name: str
    table: str
    freshness_column: str


TABLE_SPECS: tuple[TableSpec, ...] = (
    TableSpec("verdicts", "camera_verdicts", "updated_at"),
    TableSpec("verdict_history", "hotset_verdict_history", "observed_at"),
    TableSpec("subscriptions", "flood_subscriptions", "created_at"),
)

# Mirrors services/go-server/internal/store/migrations for the tables above. The
# simulator must reproduce the real column contract, otherwise a restore drill
# could pass against a schema the backend cannot use.
SIMULATOR_SCHEMA = """
CREATE TABLE IF NOT EXISTS schema_migrations (
    version     INTEGER PRIMARY KEY,
    applied_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS camera_verdicts (
    camera_id   TEXT PRIMARY KEY,
    severity    TEXT NOT NULL,
    confidence  REAL NOT NULL,
    updated_at  TIMESTAMP NOT NULL
);
CREATE TABLE IF NOT EXISTS hotset_verdict_history (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    camera_id         TEXT NOT NULL,
    pcu_total         REAL,
    saturation_index  REAL,
    severity          TEXT NOT NULL,
    confidence        REAL NOT NULL DEFAULT 0.0,
    source            TEXT NOT NULL DEFAULT '',
    observed_at       TIMESTAMP NOT NULL,
    recorded_at       TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_hotset_verdict_history_camera_observed
    ON hotset_verdict_history(camera_id, observed_at);
CREATE TABLE IF NOT EXISTS flood_subscriptions (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    channel_name  TEXT NOT NULL,
    chat_id       TEXT NOT NULL,
    target_type   TEXT NOT NULL,
    target_id     TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(channel_name, chat_id, target_type, target_id)
);
"""


# ---------------------------------------------------------------------------
# Probes
# ---------------------------------------------------------------------------

@dataclass
class TableMeasurement:
    logical_name: str
    table: str
    exists: bool
    count: int | None
    newest: str | None
    newest_at: datetime | None
    error: str | None = None

    def to_dict(self) -> dict[str, Any]:
        return {
            "table": self.table,
            "exists": self.exists,
            "count": self.count,
            "newest_record_at": self.newest,
            "error": self.error,
        }


def _connect_read_only(db_path: Path) -> sqlite3.Connection:
    uri = f"{db_path.resolve().as_uri()}?mode=ro"
    return sqlite3.connect(uri, uri=True, timeout=30.0)


def measure_database(db_path: Path, specs: Sequence[TableSpec] = TABLE_SPECS) -> list[TableMeasurement]:
    """Read counts and newest record timestamps without mutating the database."""
    measurements: list[TableMeasurement] = []
    if not db_path.exists():
        return [
            TableMeasurement(spec.logical_name, spec.table, False, None, None, None, "database file missing")
            for spec in specs
        ]

    connection = _connect_read_only(db_path)
    try:
        for spec in specs:
            try:
                count = int(connection.execute(f"SELECT COUNT(*) FROM {spec.table}").fetchone()[0])
            except sqlite3.Error as exc:
                measurements.append(
                    TableMeasurement(spec.logical_name, spec.table, False, None, None, None, str(exc))
                )
                continue
            try:
                newest = connection.execute(
                    f"SELECT MAX({spec.freshness_column}) FROM {spec.table}"
                ).fetchone()[0]
            except sqlite3.Error as exc:
                measurements.append(
                    TableMeasurement(spec.logical_name, spec.table, True, count, None, None, str(exc))
                )
                continue
            measurements.append(
                TableMeasurement(
                    spec.logical_name,
                    spec.table,
                    True,
                    count,
                    str(newest) if newest is not None else None,
                    parse_timestamp(newest),
                )
            )
    finally:
        connection.close()
    return measurements


def newest_record(moment: Iterable[TableMeasurement]) -> datetime | None:
    stamps = [m.newest_at for m in moment if m.newest_at is not None]
    return max(stamps) if stamps else None


def count_newer_than(db_path: Path, spec: TableSpec, moment: datetime) -> int | None:
    """Count source rows committed after a recovery point.

    These rows are inside the RPO window: the replica is allowed to be missing
    them without that being data loss. Counting them lets the count check stay
    exact for a quiesced source and still be honest while a writer is live.
    """
    if not db_path.exists():
        return None
    connection = _connect_read_only(db_path)
    try:
        row = connection.execute(
            f"SELECT COUNT(*) FROM {spec.table} WHERE julianday({spec.freshness_column}) > julianday(?)",
            (iso_utc(moment),),
        ).fetchone()
        return int(row[0]) if row else None
    except sqlite3.Error:
        return None
    finally:
        connection.close()


def existing_tables(db_path: Path) -> set[str]:
    if not db_path.exists():
        return set()
    try:
        connection = _connect_read_only(db_path)
    except sqlite3.Error:
        return set()
    try:
        rows = connection.execute("SELECT name FROM sqlite_master WHERE type='table'").fetchall()
        return {str(row[0]) for row in rows}
    except sqlite3.Error:
        return set()
    finally:
        connection.close()


def pragma_rows(db_path: Path, statement: str) -> tuple[list[list[Any]], str | None]:
    try:
        connection = _connect_read_only(db_path)
    except sqlite3.Error as exc:
        return [], str(exc)
    try:
        rows = [list(row) for row in connection.execute(statement).fetchall()]
        return rows, None
    except sqlite3.Error as exc:
        return [], str(exc)
    finally:
        connection.close()


@dataclass
class Check:
    name: str
    status: str
    detail: str

    def to_dict(self) -> dict[str, str]:
        return {"name": self.name, "status": self.status, "detail": self.detail}


@dataclass
class RestoreOutcome:
    """How the restore was produced, for the evidence trail."""

    transport: str
    url: str
    duration_seconds: float
    detail: dict[str, Any] = field(default_factory=dict)


@dataclass
class DrillOutcome:
    mode: str
    restore: RestoreOutcome
    restored_db: Path
    source_db: Path | None
    rto_seconds: float
    rpo_seconds: float
    rpo_method: str
    checks: list[Check]
    measurements: list[TableMeasurement]
    reference_measurements: list[TableMeasurement]
    ceiling_measurements: list[TableMeasurement]
    restored_newest: datetime | None
    reference_newest: datetime | None
    started_at: datetime
    finished_at: datetime
    litestream: dict[str, Any] = field(default_factory=dict)
    notes: list[str] = field(default_factory=list)

    @property
    def passed(self) -> bool:
        return all(check.status == "pass" for check in self.checks)

    @property
    def failures(self) -> list[Check]:
        return [check for check in self.checks if check.status != "pass"]


def evaluate(
    *,
    restored_db: Path,
    rto_seconds: float,
    rpo_seconds: float,
    rpo_budget_seconds: float,
    rto_budget_seconds: float,
    reference: Sequence[TableMeasurement] = (),
    ceiling: Sequence[TableMeasurement] = (),
    source_db: Path | None = None,
    expected_newer_than: datetime | None = None,
) -> tuple[list[Check], list[TableMeasurement], datetime | None]:
    """Run every integrity, count and freshness check on a restored database.

    ``reference`` is measured on the live source *before* the restore starts, so
    a restored copy must contain at least those rows: anything committed before
    the drill is data the replica already had. ``ceiling`` is measured after the
    restore, so the restored copy cannot hold rows the source never committed.
    With a quiesced source the two are identical and the check is exact.
    """
    checks: list[Check] = []
    measurements = measure_database(restored_db)

    # 1. Integrity pragmas. A single non-"ok" row is a corrupt restore.
    for name, statement in (
        ("integrity_check", "PRAGMA integrity_check"),
        ("quick_check", "PRAGMA quick_check"),
    ):
        rows, error = pragma_rows(restored_db, statement)
        flat = [" ".join(str(cell) for cell in row) for row in rows]
        if error:
            checks.append(Check(name, "fail", error))
        elif flat == ["ok"]:
            checks.append(Check(name, "pass", "ok"))
        else:
            checks.append(Check(name, "fail", "; ".join(flat)[:500] or "no rows returned"))

    # 2. Foreign keys must be satisfied.
    rows, error = pragma_rows(restored_db, "PRAGMA foreign_key_check")
    if error:
        checks.append(Check("foreign_key_check", "fail", error))
    elif rows:
        checks.append(
            Check("foreign_key_check", "fail", f"{len(rows)} violating row(s): {rows[:5]}")
        )
    else:
        checks.append(Check("foreign_key_check", "pass", "no foreign key violations"))

    # 3. Schema contract: every business table must exist in the restore.
    present = existing_tables(restored_db)
    missing = [measurement.table for measurement in measurements if not measurement.exists]
    if missing:
        checks.append(Check("schema", "fail", f"missing table(s): {', '.join(missing)}"))
    else:
        checks.append(
            Check("schema", "pass", f"{len(TABLE_SPECS)} business tables present ({len(present)} total)")
        )

    # 4. Record counts: non-empty and consistent with the source window.
    count_problems: list[str] = []
    count_details: list[str] = []
    reference_by_name = {m.logical_name: m for m in reference}
    ceiling_by_name = {m.logical_name: m for m in ceiling}
    specs_by_name = {spec.logical_name: spec for spec in TABLE_SPECS}
    restored_newest_candidate = newest_record(measurements)
    for measurement in measurements:
        if measurement.count is None:
            count_problems.append(f"{measurement.logical_name}: not readable")
            continue
        count_details.append(f"{measurement.logical_name}={measurement.count}")
        if measurement.logical_name == "subscriptions":
            # Subscriptions are legitimately empty on a fresh deployment; the
            # table must exist and be readable, but zero rows is not data loss.
            continue
        if measurement.count <= 0:
            count_problems.append(f"{measurement.logical_name}: empty")
        floor = reference_by_name.get(measurement.logical_name)
        allowed = 0
        if floor is not None and floor.count is not None and measurement.count < floor.count:
            deficit = floor.count - measurement.count
            spec = specs_by_name[measurement.logical_name]
            if source_db is not None and restored_newest_candidate is not None:
                allowed = count_newer_than(source_db, spec, restored_newest_candidate) or 0
            if deficit > allowed:
                count_problems.append(
                    f"{measurement.logical_name}: restored {measurement.count} < committed {floor.count} "
                    f"at restore start (only {allowed} row(s) fall inside the recovery-point window)"
                )
            else:
                count_details.append(
                    f"{measurement.logical_name}: {deficit} row(s) pending inside the recovery-point window"
                )
        top = ceiling_by_name.get(measurement.logical_name)
        if top is not None and top.count is not None and measurement.count > top.count:
            count_problems.append(
                f"{measurement.logical_name}: restored {measurement.count} > source total {top.count}"
            )
    if count_problems:
        checks.append(Check("record_counts", "fail", "; ".join(count_problems)))
    else:
        checks.append(Check("record_counts", "pass", ", ".join(count_details)))

    restored_newest = restored_newest_candidate

    # 5. Freshness: the restored copy must contain records, and must not be
    #    older than the source's newest record by more than the RPO budget.
    if restored_newest is None:
        checks.append(Check("record_freshness", "fail", "no timestamps found in restored tables"))
    else:
        age = max(0.0, (utc_now() - restored_newest).total_seconds())
        if expected_newer_than is not None and restored_newest < expected_newer_than:
            checks.append(
                Check(
                    "record_freshness",
                    "fail",
                    f"newest restored record {iso_utc(restored_newest)} predates "
                    f"the drill's first write {iso_utc(expected_newer_than)}",
                )
            )
        elif age > rpo_budget_seconds:
            checks.append(
                Check(
                    "record_freshness",
                    "fail",
                    f"newest restored record is {age:.1f}s old (budget {rpo_budget_seconds:.0f}s)",
                )
            )
        else:
            checks.append(
                Check(
                    "record_freshness",
                    "pass",
                    f"newest record {iso_utc(restored_newest)} is {age:.1f}s old",
                )
            )

    # 6. RTO and RPO budgets.
    if rto_seconds > rto_budget_seconds:
        checks.append(
            Check("rto", "fail", f"restore took {rto_seconds:.2f}s > {rto_budget_seconds:.0f}s budget")
        )
    else:
        checks.append(
            Check("rto", "pass", f"restore took {rto_seconds:.2f}s <= {rto_budget_seconds:.0f}s budget")
        )
    if rpo_seconds > rpo_budget_seconds:
        checks.append(
            Check("rpo", "fail", f"recovery point lag {rpo_seconds:.1f}s > {rpo_budget_seconds:.0f}s budget")
        )
    else:
        checks.append(
            Check("rpo", "pass", f"recovery point lag {rpo_seconds:.1f}s <= {rpo_budget_seconds:.0f}s budget")
        )

    return checks, measurements, restored_newest


# ---------------------------------------------------------------------------
# Hermetic simulator
# ---------------------------------------------------------------------------

@dataclass
class SimulatedReplica:
    root: Path
    source_db: Path
    snapshot: Path
    wal_segments: list[Path]
    manifest: Path
    sync_interval_seconds: float
    first_write_at: datetime | None
    last_write_at: datetime | None


def _seed_simulator_database(connection: sqlite3.Connection, written_at: str) -> None:
    connection.executescript(SIMULATOR_SCHEMA)
    connection.execute("INSERT OR REPLACE INTO schema_migrations(version) VALUES (12)")
    for index, camera in enumerate(("drill-cam-a", "drill-cam-b", "drill-cam-c")):
        connection.execute(
            "INSERT INTO camera_verdicts(camera_id, severity, confidence, updated_at) VALUES (?, ?, ?, ?)",
            (camera, "MODERATE", 0.42 + index * 0.05, written_at),
        )
        connection.execute(
            "INSERT INTO hotset_verdict_history(camera_id, severity, confidence, source, observed_at)"
            " VALUES (?, ?, ?, ?, ?)",
            (camera, "MODERATE", 0.42 + index * 0.05, "restore-drill", written_at),
        )
    connection.execute(
        "INSERT INTO flood_subscriptions(channel_name, chat_id, target_type, target_id, created_at)"
        " VALUES ('telegram', 'drill-chat', 'all', '', ?)",
        (written_at,),
    )


def build_simulated_replica(
    root: Path,
    *,
    sync_interval_seconds: float = DEFAULT_SYNC_INTERVAL_SECONDS,
    cycles: int = DEFAULT_CYCLES,
    seed_rows: int = 3,
    sleep: Any = time.sleep,
) -> SimulatedReplica:
    """Write a WAL-mode database and capture a local replica of it.

    The replica is deliberately simple to inspect: one base snapshot produced by
    SQLite's online backup API, then one full WAL capture per sync interval.

    ``wal_autocheckpoint`` is disabled for the duration of the drill. The point
    of the exercise is to replay a contiguous WAL; letting SQLite checkpoint
    frames away mid-run would mean measuring a restore path production never
    takes, and the captured WAL would stop being a valid log for the base
    snapshot.
    """
    source_dir = root / "source"
    replica_dir = root / "replica"
    snapshot_dir = replica_dir / "snapshots"
    wal_dir = replica_dir / "wal"
    for directory in (source_dir, snapshot_dir, wal_dir):
        directory.mkdir(parents=True, exist_ok=True)

    source_db = source_dir / "camera-view.db"
    connection = sqlite3.connect(source_db, isolation_level=None, timeout=30.0)
    try:
        connection.execute("PRAGMA journal_mode=WAL")
        connection.execute("PRAGMA wal_autocheckpoint=0")
        connection.execute("PRAGMA synchronous=FULL")

        first_write_at = utc_now()
        _seed_simulator_database(connection, iso_utc(first_write_at))

        # Fold the seed data into the main file, then take the base snapshot so
        # the captured WAL starts at frame 1 for exactly that snapshot.
        connection.execute("PRAGMA wal_checkpoint(TRUNCATE)")
        snapshot = snapshot_dir / "00000000000000000000.db"
        snapshot_connection = sqlite3.connect(snapshot)
        try:
            connection.backup(snapshot_connection)
        finally:
            snapshot_connection.close()

        wal_segments: list[Path] = []
        last_write_at = first_write_at
        for cycle in range(1, max(1, cycles) + 1):
            last_write_at = utc_now()
            stamp = iso_utc(last_write_at)
            camera = f"drill-cam-{cycle % 3}"
            connection.execute(
                "INSERT INTO hotset_verdict_history(camera_id, severity, confidence, source, observed_at)"
                " VALUES (?, 'CONGESTED', ?, 'restore-drill', ?)",
                (camera, min(0.99, 0.6 + cycle / 100.0), stamp),
            )
            connection.execute(
                "INSERT INTO camera_verdicts(camera_id, severity, confidence, updated_at)"
                " VALUES (?, 'CONGESTED', ?, ?)"
                " ON CONFLICT(camera_id) DO UPDATE SET severity=excluded.severity,"
                " confidence=excluded.confidence, updated_at=excluded.updated_at",
                (camera, min(0.99, 0.6 + cycle / 100.0), stamp),
            )
            wal_segments.append(_capture_wal(source_db, wal_dir, cycle))
            if cycle < cycles:
                sleep(sync_interval_seconds)
    finally:
        connection.close()

    manifest_path = replica_dir / "manifest.json"
    manifest = {
        "format": "litestream-simulation/v1",
        "database": "camera-view.db",
        "snapshot": str(snapshot.relative_to(replica_dir)),
        "wal_segments": [
            {
                "index": index,
                "file": str(segment.relative_to(replica_dir)),
                "captured_at": iso_utc(utc_now()),
            }
            for index, segment in enumerate(wal_segments)
        ],
        "sync_interval_seconds": sync_interval_seconds,
        "cycles": cycles,
    }
    manifest_path.write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
    return SimulatedReplica(
        root=replica_dir,
        source_db=source_db,
        snapshot=snapshot,
        wal_segments=wal_segments,
        manifest=manifest_path,
        sync_interval_seconds=sync_interval_seconds,
        first_write_at=first_write_at,
        last_write_at=last_write_at,
    )


def _capture_wal(source_db: Path, wal_dir: Path, cycle: int) -> Path:
    wal_path = Path(str(source_db) + "-wal")
    if not wal_path.exists():
        raise RuntimeError(f"expected a WAL file at {wal_path}, found none")
    destination = wal_dir / f"{cycle:06d}.wal"
    shutil.copy2(wal_path, destination)
    return destination


def unpack_simulated_replica(replica: SimulatedReplica, destination: Path) -> RestoreOutcome:
    """Restore the base snapshot and replay the newest captured WAL into it."""
    started = time.perf_counter()
    if not replica.wal_segments:
        raise RuntimeError("simulated replica contains no WAL segments")
    destination.parent.mkdir(parents=True, exist_ok=True)
    shutil.copy2(replica.snapshot, destination)
    wal_target = Path(str(destination) + "-wal")
    shutil.copy2(replica.wal_segments[-1], wal_target)
    shutil.copy2(replica.manifest, destination.parent / "replica_manifest.json")

    # Opening the database runs SQLite's WAL recovery, which validates the frame
    # checksums and applies them. The checkpoint then folds them into the main
    # file so later probes read the same bytes the backend would open.
    connection = sqlite3.connect(destination, timeout=30.0)
    try:
        connection.execute("PRAGMA journal_mode=WAL")
        connection.execute("PRAGMA wal_checkpoint(TRUNCATE)")
    finally:
        connection.close()
    duration = time.perf_counter() - started
    if wal_target.exists():
        wal_target.unlink()
    return RestoreOutcome(
        transport="local-simulation",
        url=str(replica.root),
        duration_seconds=duration,
        detail={
            "snapshot": replica.snapshot.name,
            "wal_segments_available": len(replica.wal_segments),
            "wal_segment_replayed": replica.wal_segments[-1].name,
            "sync_interval_seconds": replica.sync_interval_seconds,
        },
    )


# ---------------------------------------------------------------------------
# Litestream runner
# ---------------------------------------------------------------------------

def find_litestream(explicit: str | None = None) -> str | None:
    """Locate the Litestream binary: flag, env var, PATH, then the repo cache."""
    for candidate in (explicit, os.environ.get("LITESTREAM_BIN")):
        if candidate:
            resolved = shutil.which(candidate) or (candidate if Path(candidate).is_file() else None)
            if resolved:
                return str(resolved)
    found = shutil.which("litestream")
    if found:
        return found
    cached = REPO_ROOT / "var" / "bin" / "litestream"
    if cached.is_file() and os.access(cached, os.X_OK):
        return str(cached)
    return None


def litestream_version(binary: str | None) -> dict[str, Any]:
    if not binary:
        return {"available": False, "binary": None, "version": None}
    try:
        completed = subprocess.run(
            [binary, "version"], capture_output=True, text=True, timeout=30, check=False
        )
        version = (completed.stdout or completed.stderr).strip().splitlines()
        return {
            "available": True,
            "binary": binary,
            "version": version[0] if version else None,
        }
    except (OSError, subprocess.SubprocessError) as exc:
        return {"available": False, "binary": binary, "version": None, "error": str(exc)}


def restore_with_litestream(
    binary: str,
    destination: Path,
    replica_url: str,
    *,
    config_path: Path | None = None,
    timeout: float = 300.0,
) -> RestoreOutcome:
    destination.parent.mkdir(parents=True, exist_ok=True)
    if destination.exists():
        destination.unlink()
    command = [binary, "restore", "-o", str(destination)]
    if config_path is not None:
        # Litestream refuses "-config" together with a replica URL, and with a
        # config it still wants the database path named in that config. That is
        # exactly how the sidecar runs in production, so the drill restores the
        # same way the deployment would.
        command.extend(["-config", str(config_path), load_replica_target(config_path).db_path])
    else:
        command.append(replica_url)

    started = time.perf_counter()
    completed = subprocess.run(command, capture_output=True, text=True, timeout=timeout, check=False)
    duration = time.perf_counter() - started
    if completed.returncode != 0 or not destination.exists():
        raise RuntimeError(
            f"litestream restore failed (exit {completed.returncode}): "
            f"{(completed.stderr or completed.stdout).strip()[:800] or 'no output'}"
        )
    return RestoreOutcome(
        transport="litestream-file" if replica_url.startswith("file://") else "litestream-s3",
        url=replica_url,
        duration_seconds=duration,
        detail={
            "command": " ".join(command),
            "replica_url": replica_url,
            "stdout_tail": completed.stdout.strip().splitlines()[-3:],
        },
    )


class HeartbeatWriter:
    """Writes verdict rows on an interval while replication and restore run.

    The drill needs the replica to be *behind a live writer*, not behind a
    quiesced file. Heartbeats keep committing real transactions so the measured
    recovery point reflects the streaming path.
    """

    def __init__(self, db_path: Path, interval_seconds: float):
        self.db_path = db_path
        self.interval_seconds = interval_seconds
        self.writes = 0
        self.first_written_at: datetime | None = None
        self.last_written_at: datetime | None = None
        self.error: str | None = None
        self._stop = threading.Event()
        self._thread = threading.Thread(target=self._run, name="restore-drill-heartbeat", daemon=True)

    def start(self) -> "HeartbeatWriter":
        self._thread.start()
        return self

    def stop(self) -> None:
        self._stop.set()
        self._thread.join(timeout=30.0)

    def _run(self) -> None:
        try:
            connection = sqlite3.connect(self.db_path, isolation_level=None, timeout=30.0)
        except sqlite3.Error as exc:
            self.error = str(exc)
            return
        try:
            connection.execute("PRAGMA journal_mode=WAL")
            while not self._stop.is_set():
                now = utc_now()
                stamp = iso_utc(now)
                camera = f"drill-live-{self.writes % 3}"
                try:
                    connection.execute(
                        "INSERT INTO hotset_verdict_history(camera_id, severity, confidence, source, observed_at)"
                        " VALUES (?, 'CONGESTED', 0.75, 'restore-drill', ?)",
                        (camera, stamp),
                    )
                    connection.execute(
                        "INSERT INTO camera_verdicts(camera_id, severity, confidence, updated_at)"
                        " VALUES (?, 'CONGESTED', 0.75, ?)"
                        " ON CONFLICT(camera_id) DO UPDATE SET severity=excluded.severity,"
                        " confidence=excluded.confidence, updated_at=excluded.updated_at",
                        (camera, stamp),
                    )
                except sqlite3.Error as exc:
                    self.error = str(exc)
                    return
                self.writes += 1
                self.last_written_at = now
                if self.first_written_at is None:
                    self.first_written_at = now
                self._stop.wait(self.interval_seconds)
        finally:
            connection.close()


# ---------------------------------------------------------------------------
# Drill orchestration
# ---------------------------------------------------------------------------

@dataclass
class DrillOptions:
    mode: str = "auto"
    config_path: Path = DEFAULT_CONFIG
    out: Path | None = DEFAULT_OUT
    sandbox: Path | None = None
    keep_sandbox: bool = False
    replica_url: str | None = None
    source_db: Path | None = None
    start_replication: bool = False
    replicate_seconds: float = DEFAULT_REPLICATE_SECONDS
    replicate_warmup_seconds: float = 4.0
    sync_interval_seconds: float = DEFAULT_SYNC_INTERVAL_SECONDS
    cycles: int = DEFAULT_CYCLES
    rto_budget_seconds: float = DEFAULT_RTO_BUDGET_SECONDS
    rpo_budget_seconds: float = DEFAULT_RPO_BUDGET_SECONDS
    litestream_binary: str | None = None
    timeout_seconds: float = 300.0
    json_stdout: bool = False
    quiet: bool = False


def _explicit_replica_url(options: DrillOptions) -> str | None:
    """Only an explicit URL may select live mode automatically.

    Deriving a URL from ``R2_BUCKET_NAME`` is convenient but dangerous: a
    developer with production credentials exported would point an innocuous
    ``make restore-drill`` at the real bucket. Auto-detection therefore needs a
    deliberate ``--replica-url`` or ``RESTORE_DRILL_REPLICA_URL``.
    """
    return options.replica_url or os.environ.get("RESTORE_DRILL_REPLICA_URL") or None


def _resolve_mode(options: DrillOptions) -> tuple[str, str]:
    """Return (mode, reason). Never silently changes an explicitly requested mode."""
    binary = find_litestream(options.litestream_binary)
    target_url = _explicit_replica_url(options)

    if options.mode == "simulator":
        return "simulator", "explicitly requested"
    if options.mode == "live":
        if not binary:
            raise RuntimeError(
                "live mode requires the litestream binary; install it (v0.3.13) "
                "or set LITESTREAM_BIN, or use --mode simulator"
            )
        if not target_url and not options.config_path.exists():
            raise RuntimeError(
                "live mode requires --replica-url (or a config with a resolvable "
                "s3:// replica target) and credentials"
            )
        return "live", "explicitly requested"
    if binary and target_url:
        return "live", "litestream binary and replica target both available"
    missing = []
    if not binary:
        missing.append("litestream binary")
    if not target_url:
        missing.append("replica URL")
    return "simulator", f"live prerequisites unavailable ({', '.join(missing)})"


def _sandbox_root(options: DrillOptions) -> tuple[Path, bool]:
    if options.sandbox is not None:
        options.sandbox.mkdir(parents=True, exist_ok=True)
        return options.sandbox, False
    return Path(tempfile.mkdtemp(prefix="restore-drill-")), True


def run_drill(options: DrillOptions) -> DrillOutcome:
    """Execute one drill and return its measured outcome."""
    sandbox, ephemeral = _sandbox_root(options)
    started_at = utc_now()
    notes: list[str] = []
    try:
        mode, reason = _resolve_mode(options)
        if mode == "simulator":
            notes.append(f"mode auto-selected simulator: {reason}" if options.mode == "auto" else reason)
            restore, restored_db, source_db, first_write_at = _run_simulator(sandbox, options)
            reference: list[TableMeasurement] = (
                measure_database(source_db) if source_db is not None else []
            )
            ceiling: list[TableMeasurement] = []
        else:
            notes.append(reason)
            restore, restored_db, source_db, first_write_at, reference, ceiling = _run_live(
                sandbox, options, notes
            )

        reference_newest = newest_record(reference) if reference else None
        restored_newest = newest_record(measure_database(restored_db))
        if reference_newest is not None and restored_newest is not None:
            rpo_seconds = max(0.0, (reference_newest - restored_newest).total_seconds())
            rpo_method = "reference_newest_minus_restored_newest"
        elif restored_newest is not None:
            rpo_seconds = max(0.0, (utc_now() - restored_newest).total_seconds())
            rpo_method = "restored_newest_age"
        else:
            rpo_seconds = float("inf")
            rpo_method = "unavailable"

        checks, measurements, restored_newest = evaluate(
            restored_db=restored_db,
            rto_seconds=restore.duration_seconds,
            rpo_seconds=rpo_seconds,
            rpo_budget_seconds=options.rpo_budget_seconds,
            rto_budget_seconds=options.rto_budget_seconds,
            reference=reference,
            ceiling=ceiling,
            source_db=source_db,
            expected_newer_than=first_write_at,
        )

        return DrillOutcome(
            mode=mode,
            restore=restore,
            restored_db=restored_db,
            source_db=source_db,
            rto_seconds=restore.duration_seconds,
            rpo_seconds=rpo_seconds,
            rpo_method=rpo_method,
            checks=checks,
            measurements=measurements,
            reference_measurements=reference,
            ceiling_measurements=ceiling,
            restored_newest=restored_newest,
            reference_newest=reference_newest,
            started_at=started_at,
            finished_at=utc_now(),
            litestream=litestream_version(find_litestream(options.litestream_binary)),
            notes=notes,
        )
    finally:
        if ephemeral and not options.keep_sandbox:
            shutil.rmtree(sandbox, ignore_errors=True)


def _run_simulator(
    sandbox: Path, options: DrillOptions
) -> tuple[RestoreOutcome, Path, Path, datetime | None]:
    replica = build_simulated_replica(
        sandbox / "simulation",
        sync_interval_seconds=options.sync_interval_seconds,
        cycles=options.cycles,
    )
    restored_db = sandbox / "restore" / "camera-view.db"
    restore = unpack_simulated_replica(replica, restored_db)
    return restore, restored_db, replica.source_db, replica.first_write_at


def _run_live(
    sandbox: Path, options: DrillOptions, notes: list[str]
) -> tuple[
    RestoreOutcome,
    Path,
    Path | None,
    datetime | None,
    list[TableMeasurement],
    list[TableMeasurement],
]:
    binary = find_litestream(options.litestream_binary)
    if not binary:
        raise RuntimeError("litestream binary not found")

    replica_url = _explicit_replica_url(options)
    config_path: Path | None = None
    if replica_url is None:
        target = load_replica_target(options.config_path)
        replica_url = target.resolved_replica_url()
        if replica_url is None:
            raise RuntimeError(
                "cannot resolve an s3:// replica URL from the Litestream config; "
                "set R2_BUCKET_NAME or pass --replica-url"
            )
        config_path = target.config_path
    elif replica_url.startswith("s3://"):
        # Let Litestream read endpoint/credentials from the mounted config, but
        # never restore a different replica than the one the caller named.
        if options.config_path.exists():
            configured = load_replica_target(options.config_path).resolved_replica_url()
            if configured and configured != replica_url:
                raise RuntimeError(
                    f"--replica-url {replica_url} does not match the replica declared in "
                    f"{options.config_path} ({configured}); point the config at the replica "
                    "you intend to restore"
                )
            config_path = options.config_path if configured else None

    source_db = options.source_db
    first_write_at: datetime | None = None
    process: subprocess.Popen[str] | None = None
    writer: HeartbeatWriter | None = None
    log_handle: Any = None
    log_path = sandbox / "replicate.log"

    if options.start_replication and replica_url.startswith("s3://"):
        # The litestream CLI has no endpoint flag; a URL-only replica would go to
        # AWS S3 with R2 credentials instead of to R2. Fail loudly rather than
        # produce a confusing authentication error against the wrong cloud.
        raise RuntimeError(
            "--start-replication cannot supply an S3 endpoint to the litestream CLI; "
            "run 'litestream replicate -config' yourself and drill with --mode live "
            "without --start-replication, or use a file:// replica"
        )

    try:
        if options.start_replication:
            if source_db is None:
                raise RuntimeError("--start-replication requires --source-db")
            # Drain a copy so the drill never touches the caller's database.
            drained = sandbox / "source" / "camera-view.db"
            drained.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(source_db, drained)
            source_db = drained
            log_handle = log_path.open("w", encoding="utf-8")
            process = subprocess.Popen(
                [binary, "replicate", str(source_db), replica_url],
                stdout=log_handle,
                stderr=subprocess.STDOUT,
                text=True,
            )
            # Wait for the first snapshot before committing heartbeat rows, so
            # every heartbeat lands in a WAL segment rather than a snapshot.
            time.sleep(max(0.0, options.replicate_warmup_seconds))
            if process.poll() is not None:
                raise RuntimeError(
                    f"litestream replicate exited early (code {process.returncode}); see {log_path}"
                )
            writer = HeartbeatWriter(source_db, options.sync_interval_seconds).start()
            time.sleep(max(0.0, options.replicate_seconds))
            if writer.error:
                raise RuntimeError(f"heartbeat writer failed: {writer.error}")
            first_write_at = writer.first_written_at
            notes.append(
                f"litestream replicate ran for {options.replicate_seconds:.0f}s and committed "
                f"{writer.writes} heartbeat transaction(s) before restore"
            )

        # Counts committed before the restore is data the replica must already
        # hold; counts after it bound what the restored copy may contain.
        reference = measure_database(source_db) if source_db is not None else []
        restored_db = sandbox / "restore" / "camera-view.db"
        restore = restore_with_litestream(
            binary,
            restored_db,
            replica_url,
            config_path=config_path,
            timeout=options.timeout_seconds,
        )
        ceiling = measure_database(source_db) if source_db is not None else []
        return restore, restored_db, source_db, first_write_at, reference, ceiling
    finally:
        if writer is not None:
            writer.stop()
        if process is not None:
            process.terminate()
            try:
                process.wait(timeout=15)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait(timeout=15)
        if log_handle is not None:
            log_handle.close()


def _replica_metadata(outcome: DrillOutcome, options: DrillOptions) -> dict[str, Any]:
    """Describe the replica target without ever serialising a credential."""
    metadata: dict[str, Any] = {
        "mode": outcome.mode,
        "config": str(options.config_path) if options.config_path.exists() else None,
    }
    if outcome.mode == "simulator":
        metadata["sync_interval_seconds"] = options.sync_interval_seconds
        return metadata
    if outcome.restore.transport == "litestream-file":
        metadata["transport"] = "file"
        metadata["replica_url"] = outcome.restore.url
        return metadata
    try:
        target = load_replica_target(options.config_path)
    except (OSError, ValueError):
        return metadata
    metadata.update(
        {
            "transport": target.replica_type,
            "bucket": target.resolved_bucket(),
            "endpoint": _expand_env(target.endpoint),
            "object_prefix": target.replica_path,
            "sync_interval": target.sync_interval,
            "credentials": "environment",
        }
    )
    return metadata


def build_summary(outcome: DrillOutcome, options: DrillOptions) -> dict[str, Any]:
    measured_now = utc_now()
    counts: dict[str, dict[str, Any]] = {}
    reference_by_name = {m.logical_name: m for m in outcome.reference_measurements}
    ceiling_by_name = {m.logical_name: m for m in outcome.ceiling_measurements}
    for measurement in outcome.measurements:
        reference = reference_by_name.get(measurement.logical_name)
        ceiling = ceiling_by_name.get(measurement.logical_name)
        counts[measurement.logical_name] = {
            **measurement.to_dict(),
            "committed_before_restore": reference.count if reference else None,
            "total_after_restore": ceiling.count if ceiling else None,
        }

    return {
        "schema_version": 1,
        "task": "S14-T2",
        "issue": 442,
        "generated_at": iso_utc(measured_now),
        "mode": outcome.mode,
        "verdict": "pass" if outcome.passed else "fail",
        "duration_seconds": round((outcome.finished_at - outcome.started_at).total_seconds(), 3),
        "metrics": {
            "rto_seconds": round(outcome.rto_seconds, 3),
            "rto_budget_seconds": options.rto_budget_seconds,
            "rpo_seconds": round(outcome.rpo_seconds, 3)
            if outcome.rpo_seconds != float("inf")
            else None,
            "rpo_budget_seconds": options.rpo_budget_seconds,
            "rpo_method": outcome.rpo_method,
            "restored_newest_record_at": iso_utc(outcome.restored_newest)
            if outcome.restored_newest
            else None,
            "reference_newest_record_at": iso_utc(outcome.reference_newest)
            if outcome.reference_newest
            else None,
        },
        "restore": {
            "transport": outcome.restore.transport,
            "replica_url": outcome.restore.url,
            "duration_seconds": round(outcome.restore.duration_seconds, 3),
            "detail": outcome.restore.detail,
        },
        "replica": _replica_metadata(outcome, options),
        "counts": counts,
        "checks": [check.to_dict() for check in outcome.checks],
        "failures": [check.to_dict() for check in outcome.failures],
        "litestream": outcome.litestream,
        "notes": outcome.notes,
        "sandbox": {
            "path": str(outcome.restored_db.parent.parent),
            "retained": bool(options.keep_sandbox or options.sandbox is not None),
        },
    }


def format_human(summary: dict[str, Any]) -> str:
    metrics = summary["metrics"]
    lines = [
        f"restore drill ({summary['mode']}) - {summary['verdict'].upper()}",
        f"  transport          : {summary['restore']['transport']}",
        f"  replica            : {summary['restore']['replica_url']}",
        f"  RTO                : {metrics['rto_seconds']:.2f}s "
        f"(budget {metrics['rto_budget_seconds']:.0f}s)",
        f"  RPO                : "
        + (
            f"{metrics['rpo_seconds']:.2f}s (budget {metrics['rpo_budget_seconds']:.0f}s)"
            if metrics["rpo_seconds"] is not None
            else "unavailable"
        ),
    ]
    for check in summary["checks"]:
        lines.append(f"  [{check['status']}] {check['name']}: {check['detail']}")
    for failure in summary["failures"]:
        lines.append(f"  FAIL {failure['name']}: {failure['detail']}")
    return "\n".join(lines)


def emit(summary: dict[str, Any], options: DrillOptions) -> None:
    if not options.quiet:
        print(format_human(summary))
    payload = json.dumps(summary, indent=2, sort_keys=False) + "\n"
    destination = options.out
    if destination is not None and str(destination) == "-":
        if not options.quiet:
            print()
        sys.stdout.write(payload)
        return
    if destination is not None:
        destination.parent.mkdir(parents=True, exist_ok=True)
        destination.write_text(payload, encoding="utf-8")
        if not options.quiet:
            print(f"  summary            : {destination}")
    if options.json_stdout:
        sys.stdout.write(payload)


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="Automated SQLite restore drill for Litestream replicas (Issue #442).",
        formatter_class=argparse.ArgumentDefaultsHelpFormatter,
    )
    parser.add_argument("--mode", choices=("auto", "live", "simulator"), default="auto")
    parser.add_argument("--config", type=Path, default=DEFAULT_CONFIG, help="Litestream config")
    parser.add_argument("--out", type=Path, default=DEFAULT_OUT, help="JSON summary destination ('-' for stdout)")
    parser.add_argument("--sandbox", type=Path, default=None, help="Reuse this sandbox directory")
    parser.add_argument("--keep-sandbox", action="store_true", help="Do not delete the temporary sandbox")
    parser.add_argument("--replica-url", default=None, help="Litestream replica URL (s3://... or file://...)")
    parser.add_argument("--source-db", type=Path, default=None, help="Live source database for freshness comparison")
    parser.add_argument(
        "--start-replication",
        action="store_true",
        help="Run litestream replicate against --source-db while heartbeats are written",
    )
    parser.add_argument("--replicate-seconds", type=float, default=DEFAULT_REPLICATE_SECONDS)
    parser.add_argument("--replicate-warmup-seconds", type=float, default=4.0)
    parser.add_argument("--sync-interval", type=float, default=DEFAULT_SYNC_INTERVAL_SECONDS)
    parser.add_argument("--cycles", type=int, default=DEFAULT_CYCLES, help="Simulator write/capture cycles")
    parser.add_argument("--rto-budget", type=float, default=DEFAULT_RTO_BUDGET_SECONDS)
    parser.add_argument("--rpo-budget", type=float, default=DEFAULT_RPO_BUDGET_SECONDS)
    parser.add_argument("--litestream", dest="litestream_binary", default=None)
    parser.add_argument("--timeout", type=float, default=300.0, help="litestream restore timeout (seconds)")
    parser.add_argument("--json-stdout", action="store_true", help="Also print the JSON summary")
    parser.add_argument("--quiet", action="store_true", help="Suppress the human-readable summary")
    return parser


def options_from_args(args: argparse.Namespace) -> DrillOptions:
    return DrillOptions(
        mode=args.mode,
        config_path=args.config,
        out=args.out,
        sandbox=args.sandbox,
        keep_sandbox=args.keep_sandbox,
        replica_url=args.replica_url,
        source_db=args.source_db,
        start_replication=args.start_replication,
        replicate_seconds=args.replicate_seconds,
        replicate_warmup_seconds=args.replicate_warmup_seconds,
        sync_interval_seconds=args.sync_interval,
        cycles=args.cycles,
        rto_budget_seconds=args.rto_budget,
        rpo_budget_seconds=args.rpo_budget,
        litestream_binary=args.litestream_binary,
        timeout_seconds=args.timeout,
        json_stdout=args.json_stdout,
        quiet=args.quiet,
    )


def main(argv: Sequence[str] | None = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)
    options = options_from_args(args)
    try:
        outcome = run_drill(options)
    except Exception as exc:  # noqa: BLE001 - the drill must report, never crash silently
        failure = {
            "schema_version": 1,
            "task": "S14-T2",
            "issue": 442,
            "generated_at": iso_utc(utc_now()),
            "mode": args.mode,
            "verdict": "fail",
            "error": f"{type(exc).__name__}: {exc}",
            "checks": [],
            "failures": [{"name": "drill", "status": "fail", "detail": str(exc)}],
        }
        if not options.quiet:
            print(f"restore drill ({args.mode}) - FAIL", file=sys.stderr)
            print(f"  error: {type(exc).__name__}: {exc}", file=sys.stderr)
        payload = json.dumps(failure, indent=2) + "\n"
        if options.out is not None and str(options.out) != "-":
            options.out.parent.mkdir(parents=True, exist_ok=True)
            options.out.write_text(payload, encoding="utf-8")
        if options.json_stdout or (options.out is not None and str(options.out) == "-"):
            sys.stdout.write(payload)
        return EXIT_FAIL

    summary = build_summary(outcome, options)
    emit(summary, options)
    return EXIT_PASS if summary["verdict"] == "pass" else EXIT_FAIL


if __name__ == "__main__":
    sys.exit(main())
