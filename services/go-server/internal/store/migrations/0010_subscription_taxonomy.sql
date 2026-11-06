-- Migration: 0010_subscription_taxonomy.sql
-- Reconciles flood subscription taxonomy into canonical operational zone and administrative ward tiers.
-- Maps legacy district strings to canonical zones, and quarantines invalid or orphan targets into
-- flood_subscriptions_legacy_0010 without dropping rows (ADR-012 D4).

CREATE TABLE IF NOT EXISTS flood_subscriptions_legacy_0010 (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    original_id       INTEGER,
    channel_name      TEXT NOT NULL,
    chat_id           TEXT NOT NULL,
    target_type       TEXT NOT NULL,
    target_id         TEXT NOT NULL,
    quarantine_reason TEXT NOT NULL,
    migrated_at       TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- 1. Map legacy South Saigon districts (Quận 7, Nhà Bè)
UPDATE flood_subscriptions
SET target_id = 'Quận 7 - Nhà Bè'
WHERE target_type = 'zone'
  AND LOWER(TRIM(target_id)) IN ('quận 7', 'q7', 'q.7', 'nhà bè', 'huyện nhà bè', 'quận 7 - nhà bè');

-- 2. Map legacy East Saigon / Thu Duc districts (Thủ Đức, Quận 2, Quận 9)
UPDATE flood_subscriptions
SET target_id = 'TP. Thủ Đức'
WHERE target_type = 'zone'
  AND LOWER(TRIM(target_id)) IN ('thủ đức', 'tp. thủ đức', 'tp thủ đức', 'thành phố thủ đức', 'quận 2', 'q2', 'q.2', 'quận 9', 'q9', 'q.9');

-- 3. Map legacy West Saigon districts (Bình Tân, Bình Chánh)
UPDATE flood_subscriptions
SET target_id = 'Bình Tân - Bình Chánh'
WHERE target_type = 'zone'
  AND LOWER(TRIM(target_id)) IN ('bình tân', 'quận bình tân', 'bình chánh', 'huyện bình chánh', 'bình tân - bình chánh');

-- 4. Map legacy Northwest Gateway districts (Quận 12, Hóc Môn, Củ Chi)
UPDATE flood_subscriptions
SET target_id = 'Q.12 - Hóc Môn - Củ Chi'
WHERE target_type = 'zone'
  AND LOWER(TRIM(target_id)) IN ('quận 12', 'q12', 'q.12', 'hóc môn', 'huyện hóc môn', 'củ chi', 'huyện củ chi', 'q.12 - hóc môn - củ chi', 'quận 12 - hóc môn - củ chi');

-- 5. Map Cat Lai Port cluster
UPDATE flood_subscriptions
SET target_id = 'Cụm Cảng Cát Lái'
WHERE target_type = 'zone'
  AND LOWER(TRIM(target_id)) IN ('cát lái', 'cảng cát lái', 'cụm cảng cát lái');

-- 6. Map Binh Duong
UPDATE flood_subscriptions
SET target_id = 'Bình Dương'
WHERE target_type = 'zone'
  AND LOWER(TRIM(target_id)) IN ('bình dương', 'tỉnh bình dương');

-- 7. Map Ba Ria - Vung Tau
UPDATE flood_subscriptions
SET target_id = 'Bà Rịa - Vũng Tàu'
WHERE target_type = 'zone'
  AND LOWER(TRIM(target_id)) IN ('bà rịa - vũng tàu', 'vũng tàu', 'bà rịa', 'tỉnh bà rịa - vũng tàu');

-- 8. Map Inner City districts (D1, D3, D4, D5, D6, D8, D10, D11, Bình Thạnh, Phú Nhuận, Tân Bình, Tân Phú, Gò Vấp)
UPDATE flood_subscriptions
SET target_id = 'Khu vực Nội thành TP.HCM'
WHERE target_type = 'zone'
  AND LOWER(TRIM(target_id)) IN (
      'quận 1', 'q1', 'q.1',
      'quận 3', 'q3', 'q.3',
      'quận 4', 'q4', 'q.4',
      'quận 5', 'q5', 'q.5',
      'quận 6', 'q6', 'q.6',
      'quận 8', 'q8', 'q.8',
      'quận 10', 'q10', 'q.10',
      'quận 11', 'q11', 'q.11',
      'bình thạnh', 'quận bình thạnh',
      'phú nhuận', 'quận phú nhuận',
      'tân bình', 'quận tân bình',
      'tân phú', 'quận tân phú',
      'gò vấp', 'quận gò vấp',
      'nội thành', 'khu vực nội thành',
      'khu vực nội thành tp.hcm', 'nội thành tp.hcm'
  );

-- 9. Normalize target_type = 'all' to have empty target_id
UPDATE flood_subscriptions
SET target_id = ''
WHERE target_type = 'all' AND target_id != '';

-- 10. Quarantine invalid target_type
INSERT INTO flood_subscriptions_legacy_0010 (original_id, channel_name, chat_id, target_type, target_id, quarantine_reason)
SELECT id, channel_name, chat_id, target_type, target_id, 'invalid_target_type'
FROM flood_subscriptions
WHERE target_type NOT IN ('all', 'chokepoint', 'zone', 'ward');

DELETE FROM flood_subscriptions
WHERE target_type NOT IN ('all', 'chokepoint', 'zone', 'ward');

-- 11. Quarantine empty target_id on chokepoint, zone, or ward
INSERT INTO flood_subscriptions_legacy_0010 (original_id, channel_name, chat_id, target_type, target_id, quarantine_reason)
SELECT id, channel_name, chat_id, target_type, target_id, 'empty_target_id'
FROM flood_subscriptions
WHERE target_type IN ('chokepoint', 'zone', 'ward') AND TRIM(target_id) = '';

DELETE FROM flood_subscriptions
WHERE target_type IN ('chokepoint', 'zone', 'ward') AND TRIM(target_id) = '';

-- 12. Quarantine unresolvable zones (not matching any canonical zone)
INSERT INTO flood_subscriptions_legacy_0010 (original_id, channel_name, chat_id, target_type, target_id, quarantine_reason)
SELECT id, channel_name, chat_id, target_type, target_id, 'unresolvable_zone_target'
FROM flood_subscriptions
WHERE target_type = 'zone'
  AND target_id NOT IN (
      'Bà Rịa - Vũng Tàu',
      'Bình Dương',
      'Cụm Cảng Cát Lái',
      'Quận 7 - Nhà Bè',
      'TP. Thủ Đức',
      'Bình Tân - Bình Chánh',
      'Q.12 - Hóc Môn - Củ Chi',
      'Khu vực Nội thành TP.HCM'
  );

DELETE FROM flood_subscriptions
WHERE target_type = 'zone'
  AND target_id NOT IN (
      'Bà Rịa - Vũng Tàu',
      'Bình Dương',
      'Cụm Cảng Cát Lái',
      'Quận 7 - Nhà Bè',
      'TP. Thủ Đức',
      'Bình Tân - Bình Chánh',
      'Q.12 - Hóc Môn - Củ Chi',
      'Khu vực Nội thành TP.HCM'
  );

-- 13. Quarantine duplicates created by normalization (preserve the earliest record)
INSERT INTO flood_subscriptions_legacy_0010 (original_id, channel_name, chat_id, target_type, target_id, quarantine_reason)
SELECT f1.id, f1.channel_name, f1.chat_id, f1.target_type, f1.target_id, 'duplicate_after_normalization'
FROM flood_subscriptions f1
WHERE f1.id > (
    SELECT MIN(f2.id)
    FROM flood_subscriptions f2
    WHERE f2.channel_name = f1.channel_name
      AND f2.chat_id = f1.chat_id
      AND f2.target_type = f1.target_type
      AND f2.target_id = f1.target_id
);

DELETE FROM flood_subscriptions
WHERE id IN (
    SELECT original_id FROM flood_subscriptions_legacy_0010 WHERE quarantine_reason = 'duplicate_after_normalization'
);

-- 14. Recreate table with strict CHECK constraint on target_type
CREATE TABLE flood_subscriptions_new (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    channel_name  TEXT NOT NULL,
    chat_id       TEXT NOT NULL,
    target_type   TEXT NOT NULL CHECK (target_type IN ('all', 'chokepoint', 'zone', 'ward')),
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
CREATE INDEX IF NOT EXISTS idx_flood_sub_legacy_chat ON flood_subscriptions_legacy_0010(channel_name, chat_id);
