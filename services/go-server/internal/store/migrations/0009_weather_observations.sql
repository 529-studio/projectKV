-- Migration 0009: Weather Observations Feature Store (ADR-012 D3)
-- Append-only weather observations for short-horizon congestion forecasting.
-- Records meteorological snapshots (Open-Meteo) and city-scale radar state (RainViewer).

CREATE TABLE IF NOT EXISTS weather_observations (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    precipitation_mm   REAL NOT NULL DEFAULT 0.0,
    weather_code       INTEGER NOT NULL DEFAULT 0,
    temperature_c      REAL NOT NULL DEFAULT 0.0,
    humidity_pct       REAL NOT NULL DEFAULT 0.0,
    radar_active       INTEGER NOT NULL DEFAULT 0,
    radar_frame_age_s  INTEGER NOT NULL DEFAULT 0,
    source             TEXT NOT NULL,
    payload_hash       TEXT NOT NULL DEFAULT '',
    observed_at        TEXT NOT NULL,
    recorded_at        TEXT NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_weather_obs_source_time
    ON weather_observations(source, observed_at);

CREATE INDEX IF NOT EXISTS idx_weather_obs_observed_at
    ON weather_observations(observed_at);
