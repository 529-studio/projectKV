-- Migration: 0014_web_push_subscriptions.sql
-- Stores W3C Web Push API credentials for browser clients (RFC 8291 / RFC 8292).

CREATE TABLE IF NOT EXISTS web_push_subscriptions (
    id            TEXT PRIMARY KEY,           -- SHA-256 hex hash of the endpoint
    endpoint      TEXT NOT NULL UNIQUE,       -- Push service URL
    p256dh        TEXT NOT NULL,              -- Client P-256 public key (base64url)
    auth          TEXT NOT NULL,              -- 16-byte authentication secret (base64url)
    created_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_web_push_endpoint ON web_push_subscriptions(endpoint);
