-- Migration: 0011_add_flood_hazard.sql
-- Physical flood hazard layer for Issue #401 / Sprint 12 Task 3 (spec §5.4).
--
-- Additive only: every pre-existing column keeps its name, type and meaning, and no
-- row is backfilled. A verdict persisted before this migration reads back with NULL
-- hazard fields, which the API serialises as omitted/null and the UI renders with the
-- legacy flood badge (AC14, E20). Fabricating a level for an old row would be the
-- defect this issue removes.
--
-- `flood_depth_cm` is nullable on purpose: NULL means "never measured", 0.0 means
-- "measured dry". The text columns use NOT NULL DEFAULT '' so `COALESCE` is not
-- strictly required for reads, but keeps older SQLite files consistent.

ALTER TABLE camera_verdicts ADD COLUMN flood_depth_cm REAL;
ALTER TABLE camera_verdicts ADD COLUMN flood_hazard_level INTEGER;
ALTER TABLE camera_verdicts ADD COLUMN flood_hazard_basis TEXT NOT NULL DEFAULT '';
ALTER TABLE camera_verdicts ADD COLUMN flood_depth_confidence TEXT NOT NULL DEFAULT '';
