#!/usr/bin/env python3
"""Restore drill for the daily SQLite ``VACUUM INTO`` archives.

Production keeps one archive per day in ``r2:<bucket>/warded/``: the
``warded-backup`` sidecar runs ``sqlite3 ... "VACUUM INTO ..."`` (one
consistent pass over a live database, so WAL frames are included), gzips the
snapshot to ``warded-YYYYMMDD.sqlite3.gz`` and uploads it with ``rclone``.

This tool proves that artifact can be restored:

``hermetic`` (no network, no credentials)
    Builds a WAL-mode database, snapshots it with ``VACUUM INTO`` exactly like
    the sidecar, gzips the snapshot, restores it into a sandbox and verifies
    integrity, row count, a
    stable table checksum, and that every row committed before the snapshot is
    present while rows written afterwards are not.  Repeats ``--cycles`` times
    and measures restore wall time against the RTO budget.

``live``
    Lists ``r2:<bucket>/warded/`` with ``rclone``, downloads the newest
    ``warded-*.sqlite3.gz``, restores it and verifies it.  The archive age in
    hours is checked against the RPO budget.

``auto``
    Runs ``live`` when ``rclone`` and its R2 environment are configured,
    otherwise prints ``[INFO] live drill skipped: <reason>`` and falls back to
    the hermetic drill, so CI still exercises the snapshot/restore path.

Standard library only.
"""

from __future__ import annotations

import argparse
import gzip
import hashlib
import json
import os
import re
import shutil
import sqlite3
import subprocess
import sys
import tempfile
import time
from dataclasses import dataclass, field
from datetime import datetime, timezone
from pathlib import Path
from typing import Iterable, Sequence

SAMPLE_TABLE = "camera_verdicts"
ARCHIVE_NAME_PATTERN = re.compile(r"^warded-\d{8}\.sqlite3\.gz$")
ARCHIVE_DATE_FORMAT = "%Y%m%d"
STORAGE_PREFIX = "warded/"
REMOTE_NAME = "r2"

COMMITTED_ROWS = 200
ROWS_AFTER_SNAPSHOT = 50
VERDICTS = ("clear", "moderate", "heavy")

DEFAULT_BUCKET = os.environ.get("BACKUP_BUCKET_NAME") or "homelab-backup"
DEFAULT_OUT = "docs/reports/sqlite_backup_drill_summary.json"
DEFAULT_RTO_BUDGET_SECONDS = 60.0
DEFAULT_MIN_AGE_HOURS = 24.0

RCLONE_ENV_VARS = (
    "RCLONE_CONFIG_R2_TYPE",
    "RCLONE_CONFIG_R2_ACCESS_KEY_ID",
    "RCLONE_CONFIG_R2_SECRET_ACCESS_KEY",
)
RCLONE_TIMEOUT_SECONDS = 300

SCHEMA_SQL = """
CREATE TABLE camera_verdicts (
    id INTEGER PRIMARY KEY,
    camera_id TEXT NOT NULL,
    observed_at TEXT NOT NULL,
    verdict TEXT NOT NULL,
    confidence REAL NOT NULL
);
CREATE INDEX idx_camera_verdicts_camera ON camera_verdicts (camera_id);
"""


class DrillError(RuntimeError):
    """A drill step failed and the failure belongs in the verdict."""


class DrillSkip(RuntimeError):
    """The live drill cannot run here; ``auto`` reports it as skipped."""


def utc_now() -> datetime:
    return datetime.now(timezone.utc)


def iso_utc(moment: datetime) -> str:
    return moment.astimezone(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


@dataclass(frozen=True)
class DrillOptions:
    mode: str = "auto"
    out: Path = Path(DEFAULT_OUT)
    cycles: int = 1
    rto_budget_seconds: float = DEFAULT_RTO_BUDGET_SECONDS
    archive: Path | None = None
    bucket: str = DEFAULT_BUCKET
    min_age_hours: float = DEFAULT_MIN_AGE_HOURS


@dataclass
class Verification:
    integrity: str
    rows: int | None
    checksum: str | None
    rto_seconds: float
    errors: list[str] = field(default_factory=list)

    @property
    def ok(self) -> bool:
        return self.integrity == "ok" and not self.errors


@dataclass
class DrillResult:
    mode: str
    started_at: str
    cycles: int
    rto_seconds: float | None
    rto_budget_seconds: float
    integrity: str
    rows: int | None
    checksum: str | None
    rpo_seconds: float | None
    archive: str | None
    verdict: str
    errors: list[str] = field(default_factory=list)
    notes: list[str] = field(default_factory=list)

    def as_dict(self) -> dict[str, object]:
        return {
            "mode": self.mode,
            "started_at": self.started_at,
            "cycles": self.cycles,
            "rto_seconds": self.rto_seconds,
            "rto_budget_seconds": self.rto_budget_seconds,
            "integrity": self.integrity,
            "rows": self.rows,
            "checksum": self.checksum,
            "rpo_seconds": self.rpo_seconds,
            "archive": self.archive,
            "verdict": self.verdict,
            "errors": list(self.errors),
            "notes": list(self.notes),
        }


@dataclass
class DrillFixture:
    """A hermetic archive plus the exact content the restore must reproduce."""

    archive: Path
    rows: int
    checksum: str
    ids: tuple[int, ...]


# ---------------------------------------------------------------------------
# SQLite helpers
# ---------------------------------------------------------------------------


def _row_bytes(row: Iterable[object]) -> bytes:
    """Canonical, type-stable serialization of one row."""
    values = []
    for value in row:
        if isinstance(value, bytes):
            values.append({"blob": value.hex()})
        elif isinstance(value, float):
            values.append({"float": repr(value)})
        else:
            values.append(value)
    return (
        json.dumps(values, ensure_ascii=True, separators=(",", ":"), sort_keys=True)
        + "\n"
    ).encode("ascii")


def user_tables(connection: sqlite3.Connection) -> list[str]:
    rows = connection.execute(
        "SELECT name FROM sqlite_master "
        "WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name"
    ).fetchall()
    return [str(row[0]) for row in rows]


def select_sample_table(connection: sqlite3.Connection) -> str | None:
    """Prefer the drill's own table, else the first user table."""
    tables = user_tables(connection)
    if SAMPLE_TABLE in tables:
        return SAMPLE_TABLE
    return tables[0] if tables else None


def table_measurement(connection: sqlite3.Connection, table: str) -> tuple[int, str]:
    """Return ``(row_count, sha256)`` over a deterministic row order."""
    try:
        fetched = connection.execute(
            f'SELECT * FROM "{table}" ORDER BY rowid'
        ).fetchall()
    except sqlite3.OperationalError:
        fetched = connection.execute(f'SELECT * FROM "{table}"').fetchall()
    digest = hashlib.sha256()
    for row in fetched:
        digest.update(_row_bytes(row))
    return len(fetched), digest.hexdigest()


def integrity_check(connection: sqlite3.Connection) -> str:
    rows = connection.execute("PRAGMA integrity_check").fetchall()
    return "; ".join(str(row[0]) for row in rows)


def _insert_rows(connection: sqlite3.Connection, start: int, end: int) -> None:
    payload = []
    for index in range(start, end + 1):
        payload.append(
            (
                index,
                f"cam-{index:05d}",
                f"2026-01-01T{(index // 3600) % 24:02d}:{(index // 60) % 60:02d}:{index % 60:02d}Z",
                VERDICTS[index % len(VERDICTS)],
                round(0.5 + (index % 500) / 1000.0, 3),
            )
        )
    connection.executemany(
        f"INSERT INTO {SAMPLE_TABLE} "
        "(id, camera_id, observed_at, verdict, confidence) VALUES (?, ?, ?, ?, ?)",
        payload,
    )


# ---------------------------------------------------------------------------
# Archive helpers
# ---------------------------------------------------------------------------


def decompress_archive(archive: Path, destination: Path) -> Path:
    """Gunzip ``archive`` into ``destination``; raise ``DrillError`` on damage."""
    if not archive.is_file():
        raise DrillError(f"archive not found: {archive}")
    destination.parent.mkdir(parents=True, exist_ok=True)
    try:
        with gzip.open(archive, "rb") as source, open(destination, "wb") as target:
            shutil.copyfileobj(source, target, 1024 * 1024)
    except (OSError, EOFError) as exc:
        destination.unlink(missing_ok=True)
        raise DrillError(f"gzip decode failed for {archive.name}: {exc}") from exc
    if destination.stat().st_size == 0:
        destination.unlink(missing_ok=True)
        raise DrillError(f"gzip decode produced an empty database for {archive.name}")
    return destination


def compress_snapshot(snapshot: Path, archive: Path) -> Path:
    archive.parent.mkdir(parents=True, exist_ok=True)
    with open(snapshot, "rb") as source, gzip.open(archive, "wb") as target:
        shutil.copyfileobj(source, target, 1024 * 1024)
    return archive


def build_hermetic_archive(
    workdir: Path,
    *,
    rows: int = COMMITTED_ROWS,
    rows_after_snapshot: int = ROWS_AFTER_SNAPSHOT,
    stamp: str | None = None,
) -> DrillFixture:
    """Create the drill archive exactly the way ``warded-backup`` does.

    The source database runs in WAL mode with autocheckpoint disabled, so the
    committed rows live only in the ``-wal`` file when the snapshot is taken.
    ``VACUUM INTO`` reads one consistent snapshot and captures them; a raw
    ``cp`` would not.
    """
    workdir.mkdir(parents=True, exist_ok=True)
    day = stamp or utc_now().strftime(ARCHIVE_DATE_FORMAT)
    source_path = workdir / "camera-view.db"
    snapshot_path = workdir / f"warded-{day}.sqlite3"
    archive_path = workdir / f"warded-{day}.sqlite3.gz"

    connection = sqlite3.connect(source_path)
    try:
        connection.execute("PRAGMA journal_mode=WAL")
        connection.execute("PRAGMA synchronous=NORMAL")
        connection.execute("PRAGMA wal_autocheckpoint=0")
        connection.executescript(SCHEMA_SQL)
        _insert_rows(connection, 1, rows)
        connection.commit()

        if not Path(f"{source_path}-wal").is_file():
            raise DrillError("source database did not enter WAL mode")

        connection.execute("VACUUM INTO ?", (str(snapshot_path),))

        # Written after the snapshot: these rows must not appear in the restore.
        _insert_rows(connection, rows + 1, rows + rows_after_snapshot)
        connection.commit()
        live_rows = connection.execute(
            f"SELECT COUNT(*) FROM {SAMPLE_TABLE}"
        ).fetchone()[0]
        if live_rows != rows + rows_after_snapshot:
            raise DrillError(
                f"source database lost rows: {live_rows} != {rows + rows_after_snapshot}"
            )
    finally:
        connection.close()

    measurement = sqlite3.connect(f"file:{snapshot_path}?mode=ro", uri=True)
    try:
        detail = integrity_check(measurement)
        if detail != "ok":
            raise DrillError(f"snapshot integrity_check returned {detail}")
        snapshot_rows, checksum = table_measurement(measurement, SAMPLE_TABLE)
        ids = tuple(
            int(row[0])
            for row in measurement.execute(
                f"SELECT id FROM {SAMPLE_TABLE} ORDER BY id"
            )
        )
    finally:
        measurement.close()

    if snapshot_rows != rows:
        raise DrillError(
            f"snapshot holds {snapshot_rows} rows, expected the {rows} committed "
            "before it"
        )
    if ids != tuple(range(1, rows + 1)):
        raise DrillError("snapshot is missing rows committed before the backup")

    compress_snapshot(snapshot_path, archive_path)
    return DrillFixture(archive=archive_path, rows=rows, checksum=checksum, ids=ids)


def verify_archive(
    archive: Path,
    sandbox: Path,
    *,
    expected_rows: int | None = None,
    expected_checksum: str | None = None,
    expected_ids: Sequence[int] | None = None,
) -> Verification:
    """Restore ``archive`` into ``sandbox`` and measure it against expectations."""
    started = time.perf_counter()
    sandbox.mkdir(parents=True, exist_ok=True)
    restored = sandbox / "restored.sqlite3"
    try:
        decompress_archive(archive, restored)
    except DrillError as exc:
        return Verification(
            integrity="fail",
            rows=None,
            checksum=None,
            rto_seconds=time.perf_counter() - started,
            errors=[str(exc)],
        )

    errors: list[str] = []
    integrity = "fail"
    rows: int | None = None
    checksum: str | None = None
    connection: sqlite3.Connection | None = None
    try:
        connection = sqlite3.connect(f"file:{restored}?mode=ro", uri=True)
        detail = integrity_check(connection)
        integrity = "ok" if detail == "ok" else "fail"
        if integrity != "ok":
            errors.append(f"integrity_check failed: {detail}")

        table = select_sample_table(connection)
        if table is None:
            errors.append("restored database has no user tables")
        else:
            rows, checksum = table_measurement(connection, table)
            if expected_rows is not None and rows != expected_rows:
                errors.append(
                    f"row count mismatch in {table}: archive has {rows}, "
                    f"expected {expected_rows}"
                )
            if expected_checksum is not None and checksum != expected_checksum:
                errors.append(
                    f"checksum mismatch in {table}: archive has {checksum[:16]}..., "
                    f"expected {expected_checksum[:16]}..."
                )
            if expected_ids is not None:
                present = {
                    int(row[0])
                    for row in connection.execute(f'SELECT id FROM "{table}"')
                }
                missing = [value for value in expected_ids if value not in present]
                if missing:
                    errors.append(
                        f"{len(missing)} row(s) committed before the snapshot are "
                        f"missing from the restore (first: id {missing[0]})"
                    )
    except sqlite3.DatabaseError as exc:
        integrity = "fail"
        errors.append(f"sqlite error while verifying the restore: {exc}")
    finally:
        if connection is not None:
            connection.close()

    return Verification(
        integrity=integrity,
        rows=rows,
        checksum=checksum,
        rto_seconds=time.perf_counter() - started,
        errors=errors,
    )


# ---------------------------------------------------------------------------
# Hermetic drill
# ---------------------------------------------------------------------------


def run_hermetic(options: DrillOptions) -> DrillResult:
    started_at = iso_utc(utc_now())
    print(
        f"[INFO] hermetic drill: {options.cycles} cycle(s), "
        f"RTO budget {options.rto_budget_seconds:.0f}s"
    )
    errors: list[str] = []
    integrity = "ok"
    rows: int | None = None
    checksum: str | None = None
    archive_label: str | None = None
    worst_rto = 0.0

    for cycle in range(1, options.cycles + 1):
        with tempfile.TemporaryDirectory(prefix="restore-drill-hermetic-") as tmp:
            root = Path(tmp)
            try:
                fixture = build_hermetic_archive(root)
            except DrillError as exc:
                errors.append(f"cycle {cycle}: {exc}")
                integrity = "fail"
                print(f"[ERR] cycle {cycle}: {exc}")
                continue

            verification = verify_archive(
                fixture.archive,
                root / "sandbox",
                expected_rows=fixture.rows,
                expected_checksum=fixture.checksum,
                expected_ids=fixture.ids,
            )
            worst_rto = max(worst_rto, verification.rto_seconds)
            archive_label = fixture.archive.name
            if verification.rows is not None:
                rows = verification.rows
            if verification.checksum is not None:
                checksum = verification.checksum
            if verification.integrity != "ok":
                integrity = "fail"
            for error in verification.errors:
                message = f"cycle {cycle}: {error}"
                if message not in errors:
                    errors.append(message)
                print(f"[ERR] cycle {cycle}: {error}")
            if verification.rto_seconds > options.rto_budget_seconds:
                message = (
                    f"cycle {cycle}: restore took {verification.rto_seconds:.3f}s, "
                    f"over the {options.rto_budget_seconds:.0f}s RTO budget"
                )
                errors.append(message)
                print(f"[ERR] {message}")
            print(
                f"[INFO] cycle {cycle}/{options.cycles}: integrity={verification.integrity} "
                f"rows={verification.rows} rto={verification.rto_seconds:.3f}s"
            )

    verdict = "pass" if integrity == "ok" and not errors else "fail"
    return DrillResult(
        mode=options.mode,
        started_at=started_at,
        cycles=options.cycles,
        rto_seconds=round(worst_rto, 6),
        rto_budget_seconds=options.rto_budget_seconds,
        integrity=integrity,
        rows=rows,
        checksum=checksum,
        rpo_seconds=None,
        archive=archive_label,
        verdict=verdict,
        errors=errors,
    )


# ---------------------------------------------------------------------------
# Live drill
# ---------------------------------------------------------------------------


def rclone_binary() -> str | None:
    return shutil.which("rclone")


def missing_rclone_env(environ: dict[str, str] | None = None) -> list[str]:
    env = os.environ if environ is None else environ
    return [name for name in RCLONE_ENV_VARS if not env.get(name)]


def live_skip_reason(environ: dict[str, str] | None = None) -> str | None:
    if rclone_binary() is None:
        return "rclone is not on PATH"
    missing = missing_rclone_env(environ)
    if missing:
        return "rclone R2 remote is not configured: missing " + ", ".join(missing)
    return None


def list_remote_archives(rclone: str, bucket: str) -> list[str]:
    command = [rclone, "lsf", "--files-only", f"{REMOTE_NAME}:{bucket}/{STORAGE_PREFIX}"]
    try:
        completed = subprocess.run(
            command,
            capture_output=True,
            text=True,
            timeout=RCLONE_TIMEOUT_SECONDS,
            check=False,
        )
    except (OSError, subprocess.SubprocessError) as exc:
        raise DrillSkip(f"cannot list r2:{bucket}/{STORAGE_PREFIX}: {exc}") from exc
    if completed.returncode != 0:
        detail = (completed.stderr or completed.stdout).strip().splitlines()
        first = detail[0] if detail else f"exit code {completed.returncode}"
        raise DrillSkip(f"cannot list r2:{bucket}/{STORAGE_PREFIX}: {first}")
    return [
        line.strip()
        for line in completed.stdout.splitlines()
        if ARCHIVE_NAME_PATTERN.match(line.strip())
    ]


def newest_archive(names: Iterable[str]) -> str | None:
    dated: list[tuple[datetime, str]] = []
    for name in names:
        if not ARCHIVE_NAME_PATTERN.match(name):
            continue
        try:
            moment = datetime.strptime(name[len("warded-") : -len(".sqlite3.gz")], ARCHIVE_DATE_FORMAT)
        except ValueError:
            continue
        dated.append((moment, name))
    if not dated:
        return None
    dated.sort()
    return dated[-1][1]


def download_archive(rclone: str, bucket: str, name: str, destination: Path) -> Path:
    destination.parent.mkdir(parents=True, exist_ok=True)
    command = [
        rclone,
        "copyto",
        f"{REMOTE_NAME}:{bucket}/{STORAGE_PREFIX}{name}",
        str(destination),
    ]
    try:
        completed = subprocess.run(
            command,
            capture_output=True,
            text=True,
            timeout=RCLONE_TIMEOUT_SECONDS,
            check=False,
        )
    except (OSError, subprocess.SubprocessError) as exc:
        raise DrillError(f"rclone copyto failed for {name}: {exc}") from exc
    if completed.returncode != 0 or not destination.is_file():
        detail = (completed.stderr or completed.stdout).strip().splitlines()
        first = detail[0] if detail else f"exit code {completed.returncode}"
        raise DrillError(f"rclone copyto failed for {name}: {first}")
    return destination


def _verify_live_archive(
    archive: Path,
    label: str,
    sandbox: Path,
    options: DrillOptions,
    started_at: str,
) -> DrillResult:
    verification = verify_archive(archive, sandbox)
    age_seconds = max(0.0, time.time() - archive.stat().st_mtime)
    budget_seconds = options.min_age_hours * 3600.0
    errors = list(verification.errors)
    if age_seconds > budget_seconds:
        errors.append(
            f"archive is {age_seconds / 3600.0:.1f}h older than the "
            f"{options.min_age_hours:.0f}h RPO budget"
        )
    print(
        f"[INFO] live archive {label}: integrity={verification.integrity} "
        f"rows={verification.rows} age={age_seconds / 3600.0:.1f}h"
    )
    for error in errors:
        print(f"[ERR] {error}")
    verdict = "pass" if verification.integrity == "ok" and not errors else "fail"
    return DrillResult(
        mode=options.mode,
        started_at=started_at,
        cycles=options.cycles,
        rto_seconds=round(verification.rto_seconds, 6),
        rto_budget_seconds=options.rto_budget_seconds,
        integrity=verification.integrity,
        rows=verification.rows,
        checksum=verification.checksum,
        rpo_seconds=round(age_seconds, 3),
        archive=label,
        verdict=verdict,
        errors=errors,
    )


def run_live(options: DrillOptions) -> DrillResult:
    started_at = iso_utc(utc_now())
    if options.archive is not None:
        archive_path = options.archive
        if not archive_path.is_file():
            raise DrillError(f"archive not found: {archive_path}")
        with tempfile.TemporaryDirectory(prefix="restore-drill-live-") as tmp:
            return _verify_live_archive(
                archive_path, str(archive_path), Path(tmp) / "sandbox", options, started_at
            )

    reason = live_skip_reason()
    if reason is not None:
        raise DrillSkip(reason)
    rclone = rclone_binary()
    assert rclone is not None  # narrowed by live_skip_reason

    names = list_remote_archives(rclone, options.bucket)
    name = newest_archive(names)
    if name is None:
        raise DrillError(
            f"no warded-YYYYMMDD.sqlite3.gz object under "
            f"{REMOTE_NAME}:{options.bucket}/{STORAGE_PREFIX}"
        )

    with tempfile.TemporaryDirectory(prefix="restore-drill-live-") as tmp:
        root = Path(tmp)
        downloaded = download_archive(rclone, options.bucket, name, root / name)
        return _verify_live_archive(
            downloaded,
            f"{REMOTE_NAME}:{options.bucket}/{STORAGE_PREFIX}{name}",
            root / "sandbox",
            options,
            started_at,
        )


# ---------------------------------------------------------------------------
# Orchestration and output
# ---------------------------------------------------------------------------


def _undetermined(options: DrillOptions, verdict: str, reason: str) -> DrillResult:
    return DrillResult(
        mode=options.mode,
        started_at=iso_utc(utc_now()),
        cycles=options.cycles,
        rto_seconds=None,
        rto_budget_seconds=options.rto_budget_seconds,
        integrity="skipped" if verdict == "skipped" else "fail",
        rows=None,
        checksum=None,
        rpo_seconds=None,
        archive=None,
        verdict=verdict,
        errors=[reason],
    )


def run_drill(options: DrillOptions) -> DrillResult:
    if options.mode == "hermetic":
        if options.archive is not None:
            print("[INFO] --archive is ignored in hermetic mode; the drill builds its own")
        return run_hermetic(options)

    if options.mode == "live":
        try:
            return run_live(options)
        except DrillSkip as exc:
            print(f"[ERR] live drill failed: {exc}")
            return _undetermined(options, "fail", str(exc))
        except DrillError as exc:
            print(f"[ERR] live drill failed: {exc}")
            return _undetermined(options, "fail", str(exc))

    # auto: prefer the real archive, fall back to the hermetic drill so the
    # snapshot/restore path is still exercised where the bucket is unreachable.
    try:
        return run_live(options)
    except DrillSkip as exc:
        reason = str(exc)
        print(f"[INFO] live drill skipped: {reason}")
    except DrillError as exc:
        print(f"[ERR] live drill failed: {exc}")
        return _undetermined(options, "fail", str(exc))

    print("[INFO] running the hermetic drill instead")
    result = run_hermetic(options)
    result.notes.append(f"live drill skipped: {reason}")
    return result


def format_human(result: DrillResult) -> str:
    lines = [
        f"restore drill: mode={result.mode} verdict={result.verdict}",
        f"  cycles    : {result.cycles}",
        f"  integrity : {result.integrity}",
        f"  rows      : {result.rows if result.rows is not None else 'n/a'}",
        f"  checksum  : {result.checksum or 'n/a'}",
        f"  rto       : "
        f"{f'{result.rto_seconds:.3f}s' if result.rto_seconds is not None else 'n/a'} "
        f"(budget {result.rto_budget_seconds:.0f}s)",
        f"  rpo       : "
        f"{f'{result.rpo_seconds / 3600.0:.1f}h' if result.rpo_seconds is not None else 'n/a'}",
        f"  archive   : {result.archive or 'n/a'}",
    ]
    for note in result.notes:
        lines.append(f"  note      : {note}")
    if result.verdict == "fail":
        lines.append("[ERR] verdict: fail")
    else:
        lines.append(f"[INFO] verdict: {result.verdict}")
    return "\n".join(lines)


def write_summary(result: DrillResult, destination: Path) -> Path:
    destination.parent.mkdir(parents=True, exist_ok=True)
    destination.write_text(
        json.dumps(result.as_dict(), indent=2, sort_keys=False) + "\n",
        encoding="utf-8",
    )
    return destination


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description=(
            "Restore a daily warded-YYYYMMDD.sqlite3.gz archive and verify "
            "integrity, rows, checksum and the RTO/RPO budgets."
        )
    )
    parser.add_argument(
        "--mode",
        choices=("hermetic", "live", "auto"),
        default="auto",
        help="hermetic builds its own archive; live reads the bucket; auto tries live then falls back to hermetic",
    )
    parser.add_argument(
        "--out",
        default=DEFAULT_OUT,
        help=f"JSON summary destination (default: {DEFAULT_OUT})",
    )
    parser.add_argument(
        "--cycles",
        type=int,
        default=1,
        help="hermetic repetitions (default: 1)",
    )
    parser.add_argument(
        "--rto-budget-seconds",
        type=float,
        default=DEFAULT_RTO_BUDGET_SECONDS,
        help=f"restore wall-time budget (default: {DEFAULT_RTO_BUDGET_SECONDS:.0f}s)",
    )
    parser.add_argument(
        "--archive",
        default=None,
        help="live mode: verify this local warded-*.sqlite3.gz instead of downloading one",
    )
    parser.add_argument(
        "--bucket",
        default=DEFAULT_BUCKET,
        help=f"R2 bucket holding the warded/ prefix (default: {DEFAULT_BUCKET})",
    )
    parser.add_argument(
        "--min-age-hours",
        type=float,
        default=DEFAULT_MIN_AGE_HOURS,
        help=(
            "RPO freshness budget in hours: the newest archive older than this "
            f"fails the drill (default: {DEFAULT_MIN_AGE_HOURS:.0f}h)"
        ),
    )
    return parser


def options_from_args(args: argparse.Namespace) -> DrillOptions:
    return DrillOptions(
        mode=args.mode,
        out=Path(args.out),
        cycles=max(1, int(args.cycles)),
        rto_budget_seconds=float(args.rto_budget_seconds),
        archive=Path(args.archive) if args.archive else None,
        bucket=args.bucket,
        min_age_hours=float(args.min_age_hours),
    )


def main(argv: Sequence[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    options = options_from_args(args)
    result = run_drill(options)
    write_summary(result, options.out)
    print(format_human(result))
    print(f"[INFO] summary written to {options.out}")
    return 0 if result.verdict in ("pass", "skipped") else 1


if __name__ == "__main__":
    sys.exit(main())
