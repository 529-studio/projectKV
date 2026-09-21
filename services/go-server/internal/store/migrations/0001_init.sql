CREATE TABLE IF NOT EXISTS schema_migrations (
    version     INTEGER PRIMARY KEY,
    applied_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS subscriptions (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    channel_name  TEXT NOT NULL,       -- "telegram" | "zalo" | "webpush" | "sms"
    recipient_id  TEXT NOT NULL,       -- platform-specific identifier
    zone          TEXT NOT NULL,
    created_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(channel_name, recipient_id, zone)
);
CREATE INDEX IF NOT EXISTS idx_subscriptions_zone ON subscriptions(zone);

CREATE TABLE IF NOT EXISTS alert_history (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    channel_name  TEXT NOT NULL,
    recipient_id  TEXT NOT NULL,
    zone          TEXT NOT NULL,
    severity      TEXT NOT NULL,
    sent_at       TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_alert_history_lookup ON alert_history(channel_name, recipient_id, zone, sent_at);

CREATE TABLE IF NOT EXISTS camera_verdicts (
    camera_id   TEXT PRIMARY KEY,
    severity    TEXT NOT NULL,
    confidence  REAL NOT NULL,
    updated_at  TIMESTAMP NOT NULL
);
