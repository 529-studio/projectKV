package subscription

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory sqlite: %v", err)
	}

	createTable := `
	CREATE TABLE flood_subscriptions (
		id            INTEGER PRIMARY KEY AUTOINCREMENT,
		channel_name  TEXT NOT NULL,
		chat_id       TEXT NOT NULL,
		target_type   TEXT NOT NULL,
		target_id     TEXT NOT NULL DEFAULT '',
		created_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(channel_name, chat_id, target_type, target_id)
	);
	CREATE INDEX idx_flood_sub_target ON flood_subscriptions(channel_name, target_type, target_id);
	CREATE INDEX idx_flood_sub_chat ON flood_subscriptions(channel_name, chat_id);
	`
	if _, err := db.Exec(createTable); err != nil {
		t.Fatalf("failed to create flood_subscriptions table: %v", err)
	}

	return db
}

func TestSQLiteStore_CRUD(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	store := NewSQLiteStore(db)
	ctx := context.Background()

	// 1. Subscribe to "all" (city-wide)
	subAll := Subscription{
		ChannelName: "telegram",
		ChatID:      "chat-all-01",
		TargetType:  TargetAll,
	}
	if err := store.Subscribe(ctx, subAll); err != nil {
		t.Fatalf("Subscribe(all) failed: %v", err)
	}

	// Idempotency: duplicate subscribe should not error
	if err := store.Subscribe(ctx, subAll); err != nil {
		t.Fatalf("duplicate Subscribe(all) failed: %v", err)
	}

	// 2. Subscribe to specific chokepoint
	subChokepoint := Subscription{
		ChannelName: "telegram",
		ChatID:      "chat-cp-02",
		TargetType:  TargetChokepoint,
		TargetID:    "cam-vvk-01",
	}
	if err := store.Subscribe(ctx, subChokepoint); err != nil {
		t.Fatalf("Subscribe(chokepoint) failed: %v", err)
	}

	// 3. Subscribe to zone
	subZone := Subscription{
		ChannelName: "telegram",
		ChatID:      "chat-zone-03",
		TargetType:  TargetZone,
		TargetID:    "Quận 1",
	}
	if err := store.Subscribe(ctx, subZone); err != nil {
		t.Fatalf("Subscribe(zone) failed: %v", err)
	}

	// 4. Subscribe to ward
	subWard := Subscription{
		ChannelName: "telegram",
		ChatID:      "chat-ward-04",
		TargetType:  TargetWard,
		TargetID:    "Phường Bến Nghé",
	}
	if err := store.Subscribe(ctx, subWard); err != nil {
		t.Fatalf("Subscribe(ward) failed: %v", err)
	}

	// Test Query 1: Alert for cam-vvk-01 in Quận 1, Phường Cầu Kho
	recipients1, err := store.GetSubscribersForChokepoint(ctx, "telegram", "cam-vvk-01", "Quận 1", "Phường Cầu Kho")
	if err != nil {
		t.Fatalf("GetSubscribersForChokepoint failed: %v", err)
	}
	// Expected: chat-all-01 (all), chat-cp-02 (cam-vvk-01), chat-zone-03 (Quận 1)
	expected1 := map[string]bool{"chat-all-01": true, "chat-cp-02": true, "chat-zone-03": true}
	if len(recipients1) != 3 {
		t.Fatalf("expected 3 recipients, got %d: %v", len(recipients1), recipients1)
	}
	for _, r := range recipients1 {
		if !expected1[r] {
			t.Errorf("unexpected recipient: %s", r)
		}
	}

	// Test Query 2: Alert for cam-other in Bình Thạnh, Phường 25
	recipients2, err := store.GetSubscribersForChokepoint(ctx, "telegram", "cam-other", "Bình Thạnh", "Phường 25")
	if err != nil {
		t.Fatalf("GetSubscribersForChokepoint failed: %v", err)
	}
	// Expected: only chat-all-01
	if len(recipients2) != 1 || recipients2[0] != "chat-all-01" {
		t.Fatalf("expected [chat-all-01], got %v", recipients2)
	}

	// Test Query 3: Alert in Phường Bến Nghé
	recipients3, err := store.GetSubscribersForChokepoint(ctx, "telegram", "cam-bn-01", "Quận 1", "Phường Bến Nghé")
	if err != nil {
		t.Fatalf("GetSubscribersForChokepoint failed: %v", err)
	}
	// Expected: chat-all-01, chat-zone-03 (Quận 1), chat-ward-04 (Bến Nghé)
	expected3 := map[string]bool{"chat-all-01": true, "chat-zone-03": true, "chat-ward-04": true}
	if len(recipients3) != 3 {
		t.Fatalf("expected 3 recipients for Ben Nghe, got %d: %v", len(recipients3), recipients3)
	}
	for _, r := range recipients3 {
		if !expected3[r] {
			t.Errorf("unexpected recipient: %s", r)
		}
	}

	// Test ListSubscriptions
	list, err := store.ListSubscriptions(ctx, "telegram", "chat-cp-02")
	if err != nil {
		t.Fatalf("ListSubscriptions failed: %v", err)
	}
	if len(list) != 1 || list[0].TargetID != "cam-vvk-01" {
		t.Fatalf("unexpected list result: %v", list)
	}

	// Test Unsubscribe
	if err := store.Unsubscribe(ctx, "telegram", "chat-cp-02", TargetChokepoint, "cam-vvk-01"); err != nil {
		t.Fatalf("Unsubscribe failed: %v", err)
	}

	// Query again: chat-cp-02 must no longer be returned
	recipientsAfterUnsub, err := store.GetSubscribersForChokepoint(ctx, "telegram", "cam-vvk-01", "Quận 1", "Phường Cầu Kho")
	if err != nil {
		t.Fatalf("GetSubscribersForChokepoint failed: %v", err)
	}
	for _, r := range recipientsAfterUnsub {
		if r == "chat-cp-02" {
			t.Fatalf("expected chat-cp-02 to be unsubscribed, but was returned")
		}
	}
}

func TestSubscription_Validation(t *testing.T) {
	s1 := Subscription{}
	if err := s1.IsValid(); err != ErrInvalidChatID {
		t.Fatalf("expected ErrInvalidChatID, got %v", err)
	}

	s2 := Subscription{ChatID: "123"}
	if err := s2.IsValid(); err != ErrInvalidChannel {
		t.Fatalf("expected ErrInvalidChannel, got %v", err)
	}

	s3 := Subscription{ChatID: "123", ChannelName: "telegram", TargetType: "invalid"}
	if err := s3.IsValid(); err != ErrInvalidTargetType {
		t.Fatalf("expected ErrInvalidTargetType, got %v", err)
	}
}
