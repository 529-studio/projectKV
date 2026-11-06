# Lesson 72: Fail-Fast Database Schema Assertions & Non-Blocking Time-Series Batch Retention

## Context
In Sprint 9 (F1), the server booted successfully with zero live rows in `hotset_verdict_history` because migrations were assumed rather than asserted. When downstream consumers (such as short-horizon time-series forecasting or retrainers) attempted to query trailing intervals, they received empty datasets and degraded silently or produced degenerated metrics. Furthermore, long-term continuous data accumulation requires strict retention policies without starving active read/write transactions.

## Root Cause
1. **Silent Fallback Anti-Pattern**: A database connection opening without asserting table existence and column completeness allows application logic to start up half-configured (Lesson 66).
2. **Monolithic Deletes Block SQLite WAL Writers**: Issuing a wholesale `DELETE FROM table WHERE observed_at < cutoff` across millions of accumulated records inflates the write transaction log and locks concurrent writers, triggering `SQLITE_BUSY` contention on live inference updates.

## Solution
1. **Boot Schema Assertion (ADR-012 D2)**: Immediately following `store.NewDB`, `store.AssertSchemaReady` executes a table and column audit against `sqlite_master` and `PRAGMA table_info`. Any missing table (`hotset_verdict_history`) or required column (`camera_id`, `direction`, `observed_at`) triggers an immediate fatal exit (`log.Fatalf`), preventing half-alive deployments.
2. **Coverage Observability**: Expose `GET /api/v1/hotset/history/coverage` calculating 10-minute bin ratios and longest gaps, explicitly gating model serving ($\ge 80\%$ trailing 60m coverage) and model refitting ($\ge 70\%$ 72h contiguous coverage).
3. **Non-Blocking Chunked Retention**: Implement `PurgeOlderThan` executing batched rowid deletions (`LIMIT 5000`) on a daily ticker. Individual batches commit in single-digit milliseconds, allowing live hot-set observations to interleave seamlessly without lock contention.
