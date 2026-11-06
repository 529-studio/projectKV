package main

import (
	"database/sql"
	"testing"

	"github.com/529-studio/projectKV/services/go-server/internal/camera"
	_ "modernc.org/sqlite"
)

func TestSubscriptionsAudit_Classification(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	schema := `
	CREATE TABLE flood_subscriptions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		channel_name TEXT NOT NULL,
		chat_id TEXT NOT NULL,
		target_type TEXT NOT NULL,
		target_id TEXT NOT NULL,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE flood_subscriptions_legacy_0010 (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		original_id INTEGER,
		channel_name TEXT NOT NULL,
		chat_id TEXT NOT NULL,
		target_type TEXT NOT NULL,
		target_id TEXT NOT NULL,
		quarantine_reason TEXT NOT NULL,
		migrated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	INSERT INTO flood_subscriptions (channel_name, chat_id, target_type, target_id) VALUES
		('telegram', 'user-1', 'all', ''),
		('telegram', 'user-2', 'zone', 'Quận 7 - Nhà Bè'),
		('telegram', 'user-3', 'zone', 'Khu vực Nội thành TP.HCM'),
		('telegram', 'user-4', 'zone', 'Invalid Zone XYZ'),
		('telegram', 'user-5', 'ward', 'Bến Nghé'),
		('telegram', 'user-6', 'ward', 'Unknown Ward 999'),
		('telegram', 'user-7', 'chokepoint', 'cam-vvk-01'),
		('telegram', 'user-8', 'chokepoint', 'non-existent-cam'),
		('telegram', 'user-9', 'invalid_type', 'xyz');

	INSERT INTO flood_subscriptions_legacy_0010 (original_id, channel_name, chat_id, target_type, target_id, quarantine_reason) VALUES
		(101, 'telegram', 'old-user-1', 'zone', 'Old District', 'unresolvable_zone_target'),
		(102, 'telegram', 'old-user-2', 'bad', 'xyz', 'invalid_target_type');
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("create test schema: %v", err)
	}

	// Mock or load minimal catalog
	cams := []camera.Camera{
		{ID: "cam-vvk-01", DisplayName: "Võ Văn Kiệt", PhuongName: "Bến Nghé", Zone: "Khu vực Nội thành TP.HCM"},
	}
	cat := camera.NewCatalog(cams)

	stats, err := RunAudit(db, cat)
	if err != nil {
		t.Fatalf("RunAudit failed: %v", err)
	}

	if stats.TotalActive != 9 {
		t.Errorf("expected 9 total active, got %d", stats.TotalActive)
	}

	// user-1 (all), user-2 (zone), user-3 (zone), user-5 (ward), user-7 (chokepoint) -> 5 valid
	if stats.ValidActive != 5 {
		t.Errorf("expected 5 valid active, got %d", stats.ValidActive)
	}

	// user-4 (bad zone), user-6 (bad ward), user-8 (bad cam), user-9 (bad type) -> 4 invalid
	if stats.InvalidActive != 4 {
		t.Errorf("expected 4 invalid active, got %d", stats.InvalidActive)
	}

	if stats.Quarantined != 2 {
		t.Errorf("expected 2 quarantined rows, got %d", stats.Quarantined)
	}

	if len(stats.InvalidRows) != 4 {
		t.Errorf("expected 4 invalid rows in breakdown, got %d", len(stats.InvalidRows))
	}
}
