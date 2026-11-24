-- Migration: 0015_flood_verdict_history.sql
-- Time-series flood depth observations for corridor historical trends (Issue #526).
--
-- Design notes:
--   * observed_at is the upstream observation time formatted as RFC3339Nano UTC string.
--   * recorded_at is the local persistence timestamp.
--   * UNIQUE(camera_id, observed_at) guarantees idempotent ingestion across restarts and retries.
--   * flood_depth_cm is nullable: NULL means unmeasured, 0.0 means measured dry.

CREATE TABLE IF NOT EXISTS flood_verdict_history (
    id                     INTEGER PRIMARY KEY AUTOINCREMENT,
    camera_id              TEXT NOT NULL,
    flood_depth_cm         REAL,
    flood_hazard_level     INTEGER,
    flood_severity         TEXT NOT NULL DEFAULT '',
    flood_depth_confidence TEXT NOT NULL DEFAULT '',
    flood_calibration_ref  TEXT NOT NULL DEFAULT '',
    observed_at            TIMESTAMP NOT NULL,
    recorded_at            TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_flood_verdict_history_camera_observed
    ON flood_verdict_history(camera_id, observed_at);

CREATE INDEX IF NOT EXISTS idx_flood_verdict_history_observed
    ON flood_verdict_history(observed_at);

CREATE INDEX IF NOT EXISTS idx_flood_verdict_history_camera_observed
    ON flood_verdict_history(camera_id, observed_at);
