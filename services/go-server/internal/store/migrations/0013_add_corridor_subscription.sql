-- Migration: 0013_add_corridor_subscription.sql
-- Expands flood_subscriptions CHECK constraint on target_type to include 'corridor' (S15-T4).

CREATE TABLE IF NOT EXISTS flood_subscriptions (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    channel_name  TEXT NOT NULL,
    chat_id       TEXT NOT NULL,
    target_type   TEXT NOT NULL CHECK (target_type IN ('all', 'chokepoint', 'zone', 'ward', 'corridor')),
    target_id     TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(channel_name, chat_id, target_type, target_id)
);

CREATE TABLE flood_subscriptions_new (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    channel_name  TEXT NOT NULL,
    chat_id       TEXT NOT NULL,
    target_type   TEXT NOT NULL CHECK (target_type IN ('all', 'chokepoint', 'zone', 'ward', 'corridor')),
    target_id     TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(channel_name, chat_id, target_type, target_id)
);

INSERT INTO flood_subscriptions_new (id, channel_name, chat_id, target_type, target_id, created_at)
SELECT id, channel_name, chat_id, target_type, target_id, created_at FROM flood_subscriptions;

DROP TABLE flood_subscriptions;
ALTER TABLE flood_subscriptions_new RENAME TO flood_subscriptions;

CREATE INDEX IF NOT EXISTS idx_flood_sub_target ON flood_subscriptions(channel_name, target_type, target_id);
CREATE INDEX IF NOT EXISTS idx_flood_sub_chat ON flood_subscriptions(channel_name, chat_id);
