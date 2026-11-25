# Lesson 556: Automated SQLite WAL Failover Drill Harness

## Navigation Aid & Quick Reference
- **Domain**: Disaster recovery automation, SQLite WAL `VACUUM INTO` snapshot capture, zero-data-loss failover verification.
- **Relevant Issues**: Issue #556 (S19-T8), Issue #442 (SQLite Daily Backup & Restore), PR #540 (`VACUUM INTO` hotfix).
- **Key Files**:
  - `tools/dr/failover_drill.sh` (Executable non-destructive DR drill simulating primary WAL writes, `VACUUM INTO` capture, gzip verification, and secondary promotion)
  - `tools/dr/test_failover_drill.py` (Automated integration test validating `PRAGMA integrity_check=ok` and exact row parity)
  - `docs/runbooks/sqlite_backup_restore.md` (Linked automated drill command and verification section)
- **Core Invariants**:
  - **Non-Destructive Workspace Isolation**: `failover_drill.sh` runs strictly inside `mktemp -d` cleaned via `trap cleanup EXIT`, ensuring zero impact on live databases.
  - **WAL-Safe Zero-Loss Parity**: Confirms `VACUUM INTO` captures committed WAL frames without requiring exclusive lock or manual checkpointing, guaranteeing identical row counts (`verdicts` and `push_subscriptions`) and `PRAGMA integrity_check=ok`.

---

## 1. Context & Operational Need

PR #540 replaced `sqlite3 .backup` with `VACUUM INTO` in `warded-backup` after live backups failed to converge under continuous writes. However, operators needed an automated, repeatable DR drill script to verify backup capture, gzip archiving, secondary promotion, and zero-data-loss parity across releases.

---

## 2. Verification & Empirical Proof

1. **Automated Drill Integration Test (`tools/dr/test_failover_drill.py`)**:
   - Executed `python3 -m unittest tools/dr/test_failover_drill.py`: Verified 51/51 `verdicts` (including `cam_wal_live` WAL commit) and 10/10 `push_subscriptions` survived snapshot -> gzip -> gunzip -> promotion with `PRAGMA integrity_check=ok`.
