ALTER TABLE camera_verdicts ADD COLUMN observed_at TIMESTAMP;
UPDATE camera_verdicts SET observed_at = updated_at WHERE observed_at IS NULL;
