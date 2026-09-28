ALTER TABLE camera_verdicts ADD COLUMN source TEXT NOT NULL DEFAULT '';
DELETE FROM camera_verdicts;
