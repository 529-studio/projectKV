-- Migration: 0006_flood_subscriptions.sql
-- Subscriptions for verified real-time flood alerts across multiple targeting scopes:
-- - "all": city-wide alerts
-- - "chokepoint": specific camera or flood chokepoint ID
-- - "zone": district / quận / huyện
-- - "ward": ward / phường / xã

CREATE TABLE IF NOT EXISTS flood_subscriptions (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    channel_name  TEXT NOT NULL,              -- "telegram"
    chat_id       TEXT NOT NULL,              -- recipient identifier (e.g. Telegram chat_id)
    target_type   TEXT NOT NULL,              -- "all" | "chokepoint" | "zone" | "ward"
    target_id     TEXT NOT NULL DEFAULT '',   -- camera_id, district, ward, or empty
    created_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(channel_name, chat_id, target_type, target_id)
);

CREATE INDEX IF NOT EXISTS idx_flood_sub_target ON flood_subscriptions(channel_name, target_type, target_id);
CREATE INDEX IF NOT EXISTS idx_flood_sub_chat ON flood_subscriptions(channel_name, chat_id);
