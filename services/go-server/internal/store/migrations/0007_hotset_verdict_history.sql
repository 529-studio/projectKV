-- Migration: 0007_hotset_verdict_history.sql
-- Append-only time-series of Hot Set verdicts for short-horizon congestion
-- forecasting (Issue #302 / S9-T6).
--
-- camera_verdicts keeps only the latest row per camera. Forecasting needs the
-- full observation history, including the PCU total and saturation index that
-- are computed by cvclient.PCUCalculator but were previously discarded after
-- the in-memory fan-out.
--
-- Design notes:
--   * observed_at is the upstream frame observation time. It is written as an
--     RFC3339Nano UTC string by VerdictHistoryRepo so lexical ordering matches
--     chronological ordering.
--   * recorded_at is the local persistence time. It defaults to the SQLite UTC
--     timestamp and is intentionally separate from observed_at so backfills and
--     late analyses remain auditable.
--   * UNIQUE(camera_id, observed_at) makes the writer idempotent across process
--     restarts and repeated fan-outs of the same verdict.
--   * pcu_total and saturation_index are nullable to tolerate older sources
--     that only emit a severity label. Feature extraction discards rows that
--     do not carry a saturation index.

CREATE TABLE IF NOT EXISTS hotset_verdict_history (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    camera_id         TEXT NOT NULL,
    pcu_total         REAL,
    saturation_index  REAL,
    severity          TEXT NOT NULL,
    confidence        REAL NOT NULL DEFAULT 0.0,
    source            TEXT NOT NULL DEFAULT '',
    observed_at       TIMESTAMP NOT NULL,
    recorded_at       TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (pcu_total IS NULL OR pcu_total >= 0.0),
    CHECK (saturation_index IS NULL OR saturation_index >= 0.0)
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_hotset_verdict_history_camera_observed
    ON hotset_verdict_history(camera_id, observed_at);

CREATE INDEX IF NOT EXISTS idx_hotset_verdict_history_observed
    ON hotset_verdict_history(observed_at);

CREATE INDEX IF NOT EXISTS idx_hotset_verdict_history_camera_recorded
    ON hotset_verdict_history(camera_id, recorded_at);
