package push_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/529-studio/projectKV/services/go-server/internal/push"
	"github.com/529-studio/projectKV/services/go-server/internal/store"
	_ "modernc.org/sqlite"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test_webpush.db")
	db, err := store.NewDB(dbPath)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestSQLiteStore_SaveAndGet(t *testing.T) {
	db := openTestDB(t)
	s := push.NewStore(db)
	ctx := context.Background()

	sub := push.Subscription{
		Endpoint: "https://fcm.googleapis.com/fcm/send/sample-token-123",
		P256dh:   "BEl62iUYgUivxIkv69yViEuiBIa-Ib9-SkvMeAtA3LFg",
		Auth:     "tBHItJI5svb_sample_auth",
	}

	// 1. Save subscription
	id, err := s.Save(ctx, sub)
	if err != nil {
		t.Fatalf("expected Save to succeed, got %v", err)
	}
	if id == "" {
		t.Fatalf("expected non-empty id, got empty")
	}

	// 2. Retrieve by ID
	retrieved, found, err := s.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("expected GetByID to succeed, got %v", err)
	}
	if !found {
		t.Fatalf("expected subscription to be found by id %s", id)
	}
	if retrieved.Endpoint != sub.Endpoint || retrieved.P256dh != sub.P256dh || retrieved.Auth != sub.Auth {
		t.Fatalf("retrieved mismatch: got %+v, want %+v", retrieved, sub)
	}

	// 3. Retrieve by Endpoint
	retrievedByEP, foundEP, err := s.GetByEndpoint(ctx, sub.Endpoint)
	if err != nil {
		t.Fatalf("expected GetByEndpoint to succeed, got %v", err)
	}
	if !foundEP {
		t.Fatalf("expected subscription to be found by endpoint")
	}
	if retrievedByEP.ID != id {
		t.Fatalf("id mismatch: got %s, want %s", retrievedByEP.ID, id)
	}

	// 4. Save again (idempotent update)
	subUpdated := sub
	subUpdated.P256dh = "updated_p256dh_key"
	id2, err := s.Save(ctx, subUpdated)
	if err != nil {
		t.Fatalf("expected idempotent Save to succeed, got %v", err)
	}
	if id2 != id {
		t.Fatalf("expected same id on duplicate endpoint, got %s != %s", id2, id)
	}

	retrievedUpdated, _, _ := s.GetByID(ctx, id)
	if retrievedUpdated.P256dh != "updated_p256dh_key" {
		t.Fatalf("expected updated p256dh, got %s", retrievedUpdated.P256dh)
	}
}

func TestSQLiteStore_GetByIDs(t *testing.T) {
	db := openTestDB(t)
	s := push.NewStore(db)
	ctx := context.Background()

	sub1 := push.Subscription{
		Endpoint: "https://updates.push.services.mozilla.com/wpush/v1/sub-1",
		P256dh:   "key1",
		Auth:     "auth1",
	}
	sub2 := push.Subscription{
		Endpoint: "https://updates.push.services.mozilla.com/wpush/v1/sub-2",
		P256dh:   "key2",
		Auth:     "auth2",
	}

	id1, _ := s.Save(ctx, sub1)
	id2, _ := s.Save(ctx, sub2)

	// Fetch multiple
	list, err := s.GetByIDs(ctx, []string{id1, id2, "non-existent-id"})
	if err != nil {
		t.Fatalf("expected GetByIDs to succeed, got %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 items, got %d", len(list))
	}
}

func TestSQLiteStore_Delete(t *testing.T) {
	db := openTestDB(t)
	s := push.NewStore(db)
	ctx := context.Background()

	sub := push.Subscription{
		Endpoint: "https://example.com/push/delete-me",
		P256dh:   "key",
		Auth:     "auth",
	}
	id, _ := s.Save(ctx, sub)

	if err := s.Delete(ctx, id); err != nil {
		t.Fatalf("expected Delete to succeed, got %v", err)
	}

	_, found, _ := s.GetByID(ctx, id)
	if found {
		t.Fatalf("expected deleted subscription to not be found")
	}
}
