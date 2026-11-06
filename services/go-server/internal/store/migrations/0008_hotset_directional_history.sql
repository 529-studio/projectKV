-- Migration: 0008_hotset_directional_history.sql
-- Directional Hot Set verdict history schema expansion (ADR-012 D1 / Issue #374).
--
-- Enables recording per-direction observations (direction='a', direction='b')
-- while preserving backward compatibility for frame-wide rows (direction='').
--
-- Design notes:
--   * direction defaults to '' for frame-wide observations.
--   * region_id records the runtime geometry polygon ID.
--   * Replaces UNIQUE(camera_id, observed_at) with UNIQUE(camera_id, direction, observed_at)
--     allowing simultaneous recording of frame-wide and directional observations for the same timestamp.

ALTER TABLE hotset_verdict_history ADD COLUMN direction TEXT NOT NULL DEFAULT '';
ALTER TABLE hotset_verdict_history ADD COLUMN region_id TEXT NOT NULL DEFAULT '';

DROP INDEX IF EXISTS uq_hotset_verdict_history_camera_observed;

CREATE UNIQUE INDEX IF NOT EXISTS uq_hotset_verdict_history_cam_dir_observed
    ON hotset_verdict_history(camera_id, direction, observed_at);

CREATE INDEX IF NOT EXISTS idx_hotset_verdict_history_dir_observed
    ON hotset_verdict_history(direction, observed_at);
