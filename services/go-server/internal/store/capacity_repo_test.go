package store_test

import (
	"context"
	"database/sql"
	"math"
	"path/filepath"
	"sync"
	"testing"

	"github.com/529-studio/projectKV/services/go-server/internal/store"
	_ "modernc.org/sqlite"
)

func openTestCapacityDB(t *testing.T) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test_capacity.db")
	db, err := store.NewDB(dbPath)
	if err != nil {
		t.Fatalf("open sqlite with migrations: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestCapacityRepo_SaveAndGet(t *testing.T) {
	db := openTestCapacityDB(t)
	repo := store.NewCapacityRepo(db)
	ctx := context.Background()

	camID := "cam-vo-van-kiet"
	peak := 45.5

	if err := repo.Save(ctx, camID, peak); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	got, err := repo.Get(ctx, camID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if math.Abs(got-peak) > 1e-6 {
		t.Errorf("expected learned_max = %f, got %f", peak, got)
	}
}

func TestCapacityRepo_UpsertUpdatesValue(t *testing.T) {
	db := openTestCapacityDB(t)
	repo := store.NewCapacityRepo(db)
	ctx := context.Background()

	camID := "cam-1"
	if err := repo.Save(ctx, camID, 20.0); err != nil {
		t.Fatalf("initial Save failed: %v", err)
	}

	// Update to higher peak
	if err := repo.Save(ctx, camID, 35.0); err != nil {
		t.Fatalf("upsert Save failed: %v", err)
	}

	got, err := repo.Get(ctx, camID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if math.Abs(got-35.0) > 1e-6 {
		t.Errorf("expected updated learned_max = 35.0, got %f", got)
	}
}

func TestCapacityRepo_GetAll(t *testing.T) {
	db := openTestCapacityDB(t)
	repo := store.NewCapacityRepo(db)
	ctx := context.Background()

	expected := map[string]float64{
		"cam-1": 45.0,
		"cam-2": 30.0,
		"cam-3": 15.0,
	}

	for id, val := range expected {
		if err := repo.Save(ctx, id, val); err != nil {
			t.Fatalf("Save %s failed: %v", id, err)
		}
	}

	all, err := repo.GetAll(ctx)
	if err != nil {
		t.Fatalf("GetAll failed: %v", err)
	}

	if len(all) != len(expected) {
		t.Fatalf("expected %d entries, got %d", len(expected), len(all))
	}

	for id, expVal := range expected {
		gotVal, ok := all[id]
		if !ok {
			t.Errorf("missing camera %s in GetAll", id)
		} else if math.Abs(gotVal-expVal) > 1e-6 {
			t.Errorf("camera %s: expected %f, got %f", id, expVal, gotVal)
		}
	}
}

func TestCapacityRepo_ConcurrentAccess(t *testing.T) {
	db := openTestCapacityDB(t)
	repo := store.NewCapacityRepo(db)
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			camID := "cam-concurrent"
			_ = repo.Save(ctx, camID, float64(idx+10))
			_, _ = repo.Get(ctx, camID)
		}(i)
	}
	wg.Wait()

	val, err := repo.Get(ctx, "cam-concurrent")
	if err != nil {
		t.Fatalf("Get after concurrent writes failed: %v", err)
	}
	if val < 10.0 {
		t.Errorf("expected positive learned_max, got %f", val)
	}
}
