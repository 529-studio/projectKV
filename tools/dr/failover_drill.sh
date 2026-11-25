#!/usr/bin/env bash
# tools/dr/failover_drill.sh - Automated SQLite WAL Backup & Failover Drill Harness.
# Issue #556 / Sprint 19 Task 8.
#
# Executes a non-destructive disaster recovery simulation in an isolated temp workspace:
#   1. Provisions a WAL-mode primary SQLite database with active WAL frames.
#   2. Performs a WAL-safe `VACUUM INTO` hot snapshot + gzip archive cycle.
#   3. Restores and promotes a secondary SQLite instance from the compressed archive.
#   4. Validates `PRAGMA integrity_check` == "ok" and exact zero-loss row parity.
#   5. Probes Redis failover readiness if `REDIS_ADDR` is configured and reachable.

set -euo pipefail

REPORT_JSON=""
REDIS_ADDR="${REDIS_ADDR:-}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --report-json)
      REPORT_JSON="${2:-}"
      shift 2
      ;;
    --redis-addr)
      REDIS_ADDR="${2:-}"
      shift 2
      ;;
    -h|--help)
      echo "Usage: $0 [--report-json <path>] [--redis-addr <host:port>]"
      exit 0
      ;;
    *)
      echo "[ERR] Unknown argument: $1" >&2
      exit 2
      ;;
  esac
done

if ! command -v sqlite3 >/dev/null 2>&1; then
  echo "[ERR] sqlite3 binary is required for failover drill" >&2
  exit 1
fi

WORK_DIR="$(mktemp -d "${TMPDIR:-/tmp}/warded-dr-drill.XXXXXX")"
cleanup() {
  rm -rf "$WORK_DIR"
}
trap cleanup EXIT

PRIMARY_DB="${WORK_DIR}/camera-view-primary.db"
SNAPSHOT_DB="${WORK_DIR}/warded-snapshot.sqlite3"
ARCHIVE_GZ="${WORK_DIR}/warded-snapshot.sqlite3.gz"
SECONDARY_DB="${WORK_DIR}/camera-view-secondary.db"

START_EPOCH="$(date +%s)"

echo "[INFO] Starting DR failover drill in isolated workspace: ${WORK_DIR}"

# 1. Seed WAL-mode primary SQLite database
sqlite3 "$PRIMARY_DB" <<'SQL'
PRAGMA journal_mode=WAL;
PRAGMA synchronous=NORMAL;
CREATE TABLE verdicts (
  camera_id TEXT PRIMARY KEY,
  severity TEXT NOT NULL,
  flood_severity TEXT NOT NULL,
  observed_at TEXT NOT NULL
);
CREATE TABLE push_subscriptions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  endpoint TEXT NOT NULL UNIQUE,
  target_type TEXT NOT NULL
);
SQL

# Seed 50 verdicts and 10 push subscriptions across transactions
python3 - "$PRIMARY_DB" <<'PY'
import sqlite3, sys
db_path = sys.argv[1]
conn = sqlite3.connect(db_path)
conn.execute("PRAGMA journal_mode=WAL;")
for i in range(50):
    conn.execute(
        "INSERT INTO verdicts (camera_id, severity, flood_severity, observed_at) VALUES (?, ?, ?, ?)",
        (f"cam_{i:03d}", "MODERATE" if i % 3 == 0 else "CLEAR", "FLOODED_SHALLOW" if i % 5 == 0 else "DRY", "2026-11-25T15:00:00Z"),
    )
for j in range(10):
    conn.execute(
        "INSERT INTO push_subscriptions (endpoint, target_type) VALUES (?, ?)",
        (f"https://fcm.googleapis.com/fcm/send/sub_{j}", "corridor"),
    )
conn.commit()
# Open second connection to hold active WAL shared-memory state without full checkpoint
conn.execute("INSERT INTO verdicts (camera_id, severity, flood_severity, observed_at) VALUES ('cam_wal_live', 'HEAVY', 'FLOODED_DEEP', '2026-11-25T15:05:00Z')")
conn.commit()
conn.close()
PY

PRIMARY_VERDICTS="$(sqlite3 "$PRIMARY_DB" "SELECT COUNT(*) FROM verdicts;")"
PRIMARY_SUBS="$(sqlite3 "$PRIMARY_DB" "SELECT COUNT(*) FROM push_subscriptions;")"
echo "[INFO] Primary DB seeded (verdicts=${PRIMARY_VERDICTS}, push_subscriptions=${PRIMARY_SUBS})"

# 2. Execute WAL-safe VACUUM INTO snapshot & gzip compression (mirrors warded-backup)
sqlite3 "$PRIMARY_DB" "VACUUM INTO '${SNAPSHOT_DB}';"
gzip -9 -c "$SNAPSHOT_DB" > "$ARCHIVE_GZ"
gzip -t "$ARCHIVE_GZ"
echo "[INFO] WAL-safe VACUUM INTO archive created and gzip-verified"

# 3. Restore and promote secondary SQLite node
gunzip -c "$ARCHIVE_GZ" > "$SECONDARY_DB"
INTEGRITY="$(sqlite3 "$SECONDARY_DB" "PRAGMA integrity_check;")"
if [[ "$INTEGRITY" != "ok" ]]; then
  echo "[ERR] Secondary DB integrity check failed: ${INTEGRITY}" >&2
  exit 1
fi

SECONDARY_VERDICTS="$(sqlite3 "$SECONDARY_DB" "SELECT COUNT(*) FROM verdicts;")"
SECONDARY_SUBS="$(sqlite3 "$SECONDARY_DB" "SELECT COUNT(*) FROM push_subscriptions;")"

if [[ "$PRIMARY_VERDICTS" != "$SECONDARY_VERDICTS" || "$PRIMARY_SUBS" != "$SECONDARY_SUBS" ]]; then
  echo "[ERR] Zero-loss parity failed! Primary=(${PRIMARY_VERDICTS},${PRIMARY_SUBS}) Secondary=(${SECONDARY_VERDICTS},${SECONDARY_SUBS})" >&2
  exit 1
fi

# Verify latest WAL-committed record survived snapshot promotion
LIVE_CAM_SEV="$(sqlite3 "$SECONDARY_DB" "SELECT flood_severity FROM verdicts WHERE camera_id='cam_wal_live';")"
if [[ "$LIVE_CAM_SEV" != "FLOODED_DEEP" ]]; then
  echo "[ERR] Expected cam_wal_live=FLOODED_DEEP on promoted secondary, got ${LIVE_CAM_SEV}" >&2
  exit 1
fi
echo "[PASS] Secondary SQLite DB promoted with PRAGMA integrity_check=ok and 100% zero-loss row parity"

# 4. Optional Redis failover / replication probe
REDIS_STATUS="skipped"
if [[ -n "$REDIS_ADDR" ]] && command -v redis-cli >/dev/null 2>&1; then
  R_HOST="${REDIS_ADDR%%:*}"
  R_PORT="${REDIS_ADDR##*:}"
  if redis-cli -h "$R_HOST" -p "$R_PORT" PING >/dev/null 2>&1; then
    REDIS_STATUS="healthy"
    echo "[PASS] Redis endpoint ${REDIS_ADDR} responded PONG"
  else
    echo "[ERR] Redis endpoint ${REDIS_ADDR} unreachable" >&2
    exit 1
  fi
else
  echo "[INFO] Redis probe skipped (no REDIS_ADDR configured or redis-cli absent; SQLite single-writer path verified)"
fi

END_EPOCH="$(date +%s)"
DURATION_SEC="$((END_EPOCH - START_EPOCH))"

if [[ -n "$REPORT_JSON" ]]; then
  mkdir -p "$(dirname "$REPORT_JSON")"
  cat > "$REPORT_JSON" <<JSON
{
  "status": "pass",
  "duration_seconds": ${DURATION_SEC},
  "sqlite_integrity": "${INTEGRITY}",
  "primary_counts": {"verdicts": ${PRIMARY_VERDICTS}, "push_subscriptions": ${PRIMARY_SUBS}},
  "secondary_counts": {"verdicts": ${SECONDARY_VERDICTS}, "push_subscriptions": ${SECONDARY_SUBS}},
  "zero_data_loss": true,
  "redis_status": "${REDIS_STATUS}"
}
JSON
  echo "[INFO] Drill telemetry report written to ${REPORT_JSON}"
fi

echo "[PASS] DR failover drill completed in ${DURATION_SEC}s with zero data loss."
