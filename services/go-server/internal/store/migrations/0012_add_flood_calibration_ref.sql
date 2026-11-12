-- Migration: 0012_add_flood_calibration_ref.sql
-- Persist the §2.1 reference ladder string for Issue #401 / Sprint 12 Task 3.
--
-- Additive only, and deliberately a separate version rather than an extra statement
-- inside 0011: `runMigrations` skips a file whose version is already recorded, so
-- editing an applied migration would leave every database that ran the earlier 0011
-- without the column while the boot-time schema assertion demands it.
--
-- The value is one of MOTORCYCLE_110CM | CURB_15CM | CAMERA_OVERRIDE | NONE. It is
-- stored verbatim with the level so a restart rehydrates the same claim the engine
-- published; '' means the row predates the reference ladder (NULL-equivalent for the
-- API, which omits the key).

ALTER TABLE camera_verdicts ADD COLUMN flood_calibration_ref TEXT NOT NULL DEFAULT '';
