CREATE TABLE IF NOT EXISTS camera_capacities (
    camera_id   TEXT PRIMARY KEY,
    learned_max REAL NOT NULL,
    updated_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
