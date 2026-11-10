# Lesson 78: Unattended Staging Soak & Historical Proof Archive Ingestion

## Navigation Aid & Quick Reference
- **Domain**: Staging Operations, Autonomous Harvester, Historical Replay Pipeline, SQLite Concurrency
- **Relevant Issues**: #396 (Sprint 11 Wave 1 / S11-T2)
- **Key Files**:
  - `tools/harvester/service.py` (24/7 background harvester daemon)
  - `tools/eval/ingest_proof_archive.py` (Historical proof frame replay & SQLite populator)
  - `docker-compose.stg.yml` (`warded-harvester` compose service)
  - `services/go-server/internal/store/history_repo.go` (Hot set verdict history store)
- **Core Invariants**:
  - Process-anchored soak time: `unbroken_soak_seconds` must strictly measure contiguous process uptime, resetting on unhandled crash or restart.
  - SQLite WAL & busy timeout: Multi-process writers (`go-server` backend, Python harvester, and batch replay ingester) must all set `PRAGMA journal_mode=WAL;` and `PRAGMA busy_timeout=5000;` to eliminate write locks.
  - Zero token leakage: `DEV_ACCESS_TOKEN` / `X-Dev-Token` must be routed exclusively to internal proxy caches, never leaked to public upstream endpoints.
  - Sub-resolution & duplicate frame gating: Frames with dimensions $< 320 \times 240$ or identical SHA-256 hashes must be rejected to prevent frozen feed poisoning.

---

## 1. Context & The Problem
In Sprint 10, the short-horizon forecast endpoint `/api/v1/forecast` was locked at `503 model_unvalidated` because staging `hotset_verdict_history` held 0 authentic observation rows. Sprint 10's 72-hour soak was simulated via fixtures rather than accumulated through continuous 24/7 observation. Furthermore, 11,905 authentic proof frames collected on 2026-11-03 were sitting unindexed in proof storage without being fed into the SQLite feature store or the replay dataset.

## 2. Root Cause Analysis
1. **Scheduled Batch vs. Continuous Soak Inversion**: The previous harvester was scheduled as a batch cron at discrete hours (`07:30, 12:00, 17:30 ICT`), producing vast gaps between harvests rather than a continuous 5-minute sampling cadence needed for short-horizon feature engineering (10m, 20m, 30m bins).
2. **Multi-Process SQLite Lock Contention**: When the Go backend and external Python scripts attempted simultaneous writes to `camera-view.db`, default SQLite settings (`journal_mode=delete`, zero busy timeout) caused `OperationalError: database is locked`, aborting batch writes.
3. **Synthetic Soak Illusion**: A soak script that reads past timestamps or estimates soak duration across restarts masks real crashes and operational downtime.

## 3. Architecture & Solution

### A. Autonomous 24/7 Harvester Daemon (`tools/harvester/service.py`)
- Cycles every 300 seconds (5 minutes) across the 57 canonical Hot Set cameras.
- Priority fetching: queries local backend proxy cache first (`http://warded-backend:3000`), falling back to upstream with proper User-Agent headers.
- Strict token isolation: attaches `X-Dev-Token` only to the proxy origin.
- Rejection filters: discards non-JPEG payloads, frames below $320 \times 240$, and frozen frames matching the previous SHA-256 hash.
- Process-anchored telemetry: records `unbroken_soak_seconds` bounded by process lifetime (`time.monotonic()`), logging soak reset events when restarted.
- Compose integration: deployed as a standalone container `warded-harvester` under `docker-compose.stg.yml` mounting shared SQLite volume `warded-stg-data`.

### B. Historical Proof Archive Ingester (`tools/eval/ingest_proof_archive.py`)
- Traverses 11,905 authentic proof frames in `services/go-server/data/proofs/2026-11-03`.
- Robust parsing: extracts camera ID, Unix epoch, and event type with overflow and corrupt-pattern defense.
- Schema synchronization: ensures tables and indexes exist (`uq_hotset_verdict_history_cam_dir_observed`) and synchronizes Go `schema_migrations` so future migrations don't conflict.
- Dual-direction generation: checks camera directional geometries in `data/roadway_regions/` to generate `dir_a` and `dir_b` records alongside frame-wide observations.
- Batch transactions: commits in 500-record chunks with `WAL` mode and 5000ms busy timeout, completing 11,905 frames in ~3 minutes without lock starvation.
- Auditable replay manifest: records every processed frame in `docs/proofs/sprint11/proof_archive_manifest.json` with provenance `replayed`.

## 4. Verification & Prevention Rules
1. **Never mock soak duration**: Soak duration must be derived from monotonic process clocks or verified contiguous database timestamps, never synthetic declarations.
2. **Mandatory WAL on shared SQLite**: Any tool or service connecting to the SQLite database alongside the Go backend must enforce `PRAGMA journal_mode=WAL;` and `PRAGMA busy_timeout=5000;`.
3. **Automated CI Coverage**: All evaluation and harvester tools must be wired to `Makefile` targets (`test-eval`, `test-harvester`) and executed in CI workflows.
