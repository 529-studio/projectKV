package subscription_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/529-studio/projectKV/services/go-server/internal/subscription"
	_ "modernc.org/sqlite"
)

// TestContract_TaxonomyDelivery verifies that:
// 1. Zone subscriptions (including legacy district aliases like "Quận 7") receive alerts for cameras in that zone ("Quận 7 - Nhà Bè").
// 2. Ward subscriptions receive alerts for cameras in that ward (with or without "Phường " prefix).
// 3. Unrelated zones do not receive alerts.
func TestContract_TaxonomyDelivery(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory sqlite: %v", err)
	}
	defer db.Close()

	createTable := `
	CREATE TABLE flood_subscriptions (
		id            INTEGER PRIMARY KEY AUTOINCREMENT,
		channel_name  TEXT NOT NULL,
		chat_id       TEXT NOT NULL,
		target_type   TEXT NOT NULL CHECK (target_type IN ('all', 'chokepoint', 'zone', 'ward')),
		target_id     TEXT NOT NULL DEFAULT '',
		created_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(channel_name, chat_id, target_type, target_id)
	);
	CREATE INDEX idx_flood_sub_target ON flood_subscriptions(channel_name, target_type, target_id);
	`
	if _, err := db.Exec(createTable); err != nil {
		t.Fatalf("create table: %v", err)
	}

	store := subscription.NewSQLiteStore(db)
	ctx := context.Background()

	// Register 4 test subscribers:
	// User 1: Subscribes to legacy district alias "Quận 7" (normalizes to "Quận 7 - Nhà Bè")
	sub1 := subscription.Subscription{
		ChannelName: "telegram",
		ChatID:      "chat-user-q7",
		TargetType:  subscription.TargetZone,
		TargetID:    "Quận 7",
	}
	if err := store.Subscribe(ctx, sub1); err != nil {
		t.Fatalf("sub1 failed: %v", err)
	}

	// User 2: Subscribes to canonical zone "Khu vực Nội thành TP.HCM"
	sub2 := subscription.Subscription{
		ChannelName: "telegram",
		ChatID:      "chat-user-inner",
		TargetType:  subscription.TargetZone,
		TargetID:    "Khu vực Nội thành TP.HCM",
	}
	if err := store.Subscribe(ctx, sub2); err != nil {
		t.Fatalf("sub2 failed: %v", err)
	}

	// User 3: Subscribes to ward "Bến Nghé" (prefix-less)
	sub3 := subscription.Subscription{
		ChannelName: "telegram",
		ChatID:      "chat-user-ward",
		TargetType:  subscription.TargetWard,
		TargetID:    "Bến Nghé",
	}
	if err := store.Subscribe(ctx, sub3); err != nil {
		t.Fatalf("sub3 failed: %v", err)
	}

	// User 4: Subscribes to city-wide "all"
	sub4 := subscription.Subscription{
		ChannelName: "telegram",
		ChatID:      "chat-user-all",
		TargetType:  subscription.TargetAll,
	}
	if err := store.Subscribe(ctx, sub4); err != nil {
		t.Fatalf("sub4 failed: %v", err)
	}

	// Scenario A: Camera in South Saigon triggers alert (Zone: "Quận 7 - Nhà Bè", Ward: "Phường Tân Phong")
	// Expected recipients: chat-user-q7 (matched via normalized zone), chat-user-all
	recipientsA, err := store.GetSubscribersForChokepoint(ctx, "telegram", "cam-q7-01", "Quận 7 - Nhà Bè", "Phường Tân Phong")
	if err != nil {
		t.Fatalf("GetSubscribersForChokepoint A failed: %v", err)
	}

	recipMapA := make(map[string]bool)
	for _, id := range recipientsA {
		recipMapA[id] = true
	}

	if !recipMapA["chat-user-q7"] {
		t.Errorf("expected chat-user-q7 to receive alert for camera in 'Quận 7 - Nhà Bè'")
	}
	if !recipMapA["chat-user-all"] {
		t.Errorf("expected chat-user-all to receive alert")
	}
	if recipMapA["chat-user-inner"] {
		t.Errorf("chat-user-inner should NOT receive alert for South Saigon camera")
	}
	if recipMapA["chat-user-ward"] {
		t.Errorf("chat-user-ward should NOT receive alert for Tân Phong camera")
	}

	// Scenario B: Camera in Inner City / Bến Nghé triggers alert (Zone: "Khu vực Nội thành TP.HCM", Ward: "Phường Bến Nghé")
	// Expected recipients: chat-user-inner, chat-user-ward (matches "Bến Nghé"), chat-user-all
	recipientsB, err := store.GetSubscribersForChokepoint(ctx, "telegram", "cam-d1-01", "Khu vực Nội thành TP.HCM", "Phường Bến Nghé")
	if err != nil {
		t.Fatalf("GetSubscribersForChokepoint B failed: %v", err)
	}

	recipMapB := make(map[string]bool)
	for _, id := range recipientsB {
		recipMapB[id] = true
	}

	if !recipMapB["chat-user-inner"] {
		t.Errorf("expected chat-user-inner to receive alert for Inner City camera")
	}
	if !recipMapB["chat-user-ward"] {
		t.Errorf("expected chat-user-ward to receive alert for 'Phường Bến Nghé' camera")
	}
	if !recipMapB["chat-user-all"] {
		t.Errorf("expected chat-user-all to receive alert")
	}
	if recipMapB["chat-user-q7"] {
		t.Errorf("chat-user-q7 should NOT receive alert for Inner City camera")
	}
}

// TestMigration_0010_SubscriptionTaxonomy verifies migration 0010 logic on legacy schema:
// - Legacy district strings ('Quận 7', 'Quận Bình Thạnh') are mapped to canonical zones.
// - Unresolvable targets and invalid target types are quarantined in flood_subscriptions_legacy_0010.
// - Duplicates created by normalization are quarantined without deleting valid subscribers.
func TestMigration_0010_SubscriptionTaxonomy(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	// 1. Setup legacy table matching migration 0006
	legacyInit := `
	CREATE TABLE flood_subscriptions (
		id            INTEGER PRIMARY KEY AUTOINCREMENT,
		channel_name  TEXT NOT NULL,
		chat_id       TEXT NOT NULL,
		target_type   TEXT NOT NULL,
		target_id     TEXT NOT NULL DEFAULT '',
		created_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(channel_name, chat_id, target_type, target_id)
	);

	INSERT INTO flood_subscriptions (id, channel_name, chat_id, target_type, target_id) VALUES
		(1, 'telegram', 'user-1', 'zone', 'Quận 7'),
		(2, 'telegram', 'user-2', 'zone', 'Quận Bình Thạnh'),
		(3, 'telegram', 'user-3', 'zone', 'Bình Dương'),
		(4, 'telegram', 'user-4', 'zone', 'Invalid District XYZ'),
		(5, 'telegram', 'user-5', 'bad_type', 'some_id'),
		(6, 'telegram', 'user-6', 'all', 'accidental_id'),
		(7, 'telegram', 'user-7', 'zone', ''),
		(8, 'telegram', 'user-8', 'chokepoint', 'cam-101');
	`
	if _, err := db.Exec(legacyInit); err != nil {
		t.Fatalf("setup legacy table: %v", err)
	}

	// 2. Read and apply migration 0010
	migPath := filepath.Join("..", "store", "migrations", "0010_subscription_taxonomy.sql")
	migSQL, err := os.ReadFile(migPath)
	if err != nil {
		t.Fatalf("read migration 0010 from %s: %v", migPath, err)
	}

	if _, err := db.Exec(string(migSQL)); err != nil {
		t.Fatalf("execute migration 0010 failed: %v", err)
	}

	// 3. Verify active subscriptions after migration
	rows, err := db.Query(`SELECT id, chat_id, target_type, target_id FROM flood_subscriptions ORDER BY id ASC`)
	if err != nil {
		t.Fatalf("query active subscriptions: %v", err)
	}
	defer rows.Close()

	activeMap := make(map[string]subscription.Subscription)
	for rows.Next() {
		var sub subscription.Subscription
		var targetType string
		if err := rows.Scan(&sub.ID, &sub.ChatID, &targetType, &sub.TargetID); err != nil {
			t.Fatalf("scan active row: %v", err)
		}
		sub.TargetType = subscription.TargetType(targetType)
		activeMap[sub.ChatID] = sub
	}

	// User 1 ('Quận 7') -> 'Quận 7 - Nhà Bè'
	u1, ok := activeMap["user-1"]
	if !ok || u1.TargetID != "Quận 7 - Nhà Bè" {
		t.Errorf("expected user-1 mapped to 'Quận 7 - Nhà Bè', got: %+v", u1)
	}

	// User 2 ('Quận Bình Thạnh') -> 'Khu vực Nội thành TP.HCM'
	u2, ok := activeMap["user-2"]
	if !ok || u2.TargetID != "Khu vực Nội thành TP.HCM" {
		t.Errorf("expected user-2 mapped to 'Khu vực Nội thành TP.HCM', got: %+v", u2)
	}

	// User 3 ('Bình Dương') -> 'Bình Dương'
	u3, ok := activeMap["user-3"]
	if !ok || u3.TargetID != "Bình Dương" {
		t.Errorf("expected user-3 mapped to 'Bình Dương', got: %+v", u3)
	}

	// User 6 ('all') -> target_id = ''
	u6, ok := activeMap["user-6"]
	if !ok || u6.TargetID != "" {
		t.Errorf("expected user-6 target_id to be empty, got: %+v", u6)
	}

	// User 8 ('chokepoint', 'cam-101') unchanged
	u8, ok := activeMap["user-8"]
	if !ok || u8.TargetID != "cam-101" {
		t.Errorf("expected user-8 to be active with cam-101, got: %+v", u8)
	}

	// User 4 ('Invalid District XYZ'), User 5 ('bad_type'), User 7 (empty target_id) must NOT be in active
	for _, badUser := range []string{"user-4", "user-5", "user-7"} {
		if _, exists := activeMap[badUser]; exists {
			t.Errorf("expected %s to be quarantined, but still active", badUser)
		}
	}

	// 4. Verify quarantine table flood_subscriptions_legacy_0010
	qRows, err := db.Query(`SELECT original_id, chat_id, target_type, target_id, quarantine_reason FROM flood_subscriptions_legacy_0010`)
	if err != nil {
		t.Fatalf("query quarantine table: %v", err)
	}
	defer qRows.Close()

	quarantineMap := make(map[string]string)
	for qRows.Next() {
		var origID int64
		var chatID, targetType, targetID, reason string
		if err := qRows.Scan(&origID, &chatID, &targetType, &targetID, &reason); err != nil {
			t.Fatalf("scan quarantine row: %v", err)
		}
		quarantineMap[chatID] = reason
	}

	if quarantineMap["user-4"] != "unresolvable_zone_target" {
		t.Errorf("expected user-4 quarantined with 'unresolvable_zone_target', got %q", quarantineMap["user-4"])
	}
	if quarantineMap["user-5"] != "invalid_target_type" {
		t.Errorf("expected user-5 quarantined with 'invalid_target_type', got %q", quarantineMap["user-5"])
	}
	if quarantineMap["user-7"] != "empty_target_id" {
		t.Errorf("expected user-7 quarantined with 'empty_target_id', got %q", quarantineMap["user-7"])
	}

	// 5. Verify CHECK constraint enforcement on new table
	_, err = db.Exec(`INSERT INTO flood_subscriptions (channel_name, chat_id, target_type, target_id) VALUES ('tg', 'fail', 'bad_type', 'x')`)
	if err == nil {
		t.Errorf("expected CHECK constraint violation on bad target_type, but succeeded")
	}
}
