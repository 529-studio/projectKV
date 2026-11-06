package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestWeatherRepo_LifecycleAndAsOf(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "weather_test.db")

	db, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	if err := AssertSchemaReady(db); err != nil {
		t.Fatalf("AssertSchemaReady failed: %v", err)
	}

	repo := NewWeatherRepo(db)

	// Initial empty state
	latest, err := repo.GetLatest(ctx)
	if err != ErrWeatherNotFound {
		t.Fatalf("expected ErrWeatherNotFound on empty DB, got: %v (latest=%v)", err, latest)
	}

	count, err := repo.Count(ctx)
	if err != nil {
		t.Fatalf("Count failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected count 0, got %d", count)
	}

	baseTime := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)

	// Record observation 1 at 10:00
	obs1 := &WeatherObservation{
		PrecipitationMm: 0.0,
		WeatherCode:     1,
		TemperatureC:    28.5,
		HumidityPct:     75.0,
		RadarActive:     0,
		RadarFrameAgeS:  0,
		Source:          "open-meteo+rainviewer",
		PayloadHash:     "hash_obs1",
		ObservedAt:      baseTime,
	}

	if err := repo.Record(ctx, obs1); err != nil {
		t.Fatalf("Record obs1 failed: %v", err)
	}
	if obs1.ID <= 0 {
		t.Errorf("expected positive ID, got %d", obs1.ID)
	}

	// Record identical observation again: must be ignored idempotently
	obs1Dup := &WeatherObservation{
		PrecipitationMm: 0.0,
		WeatherCode:     1,
		TemperatureC:    28.5,
		HumidityPct:     75.0,
		RadarActive:     0,
		RadarFrameAgeS:  0,
		Source:          "open-meteo+rainviewer",
		PayloadHash:     "hash_obs1",
		ObservedAt:      baseTime,
	}
	if err := repo.Record(ctx, obs1Dup); err != nil {
		t.Fatalf("Record duplicate obs1 failed: %v", err)
	}

	count, err = repo.Count(ctx)
	if err != nil {
		t.Fatalf("Count failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected count 1 after duplicate record, got %d", count)
	}

	// Record observation 2 at 10:10 with rain
	obs2Time := baseTime.Add(10 * time.Minute)
	obs2 := &WeatherObservation{
		PrecipitationMm: 4.2,
		WeatherCode:     63,
		TemperatureC:    26.0,
		HumidityPct:     90.0,
		RadarActive:     1,
		RadarFrameAgeS:  120,
		Source:          "open-meteo+rainviewer",
		PayloadHash:     "hash_obs2",
		ObservedAt:      obs2Time,
	}
	if err := repo.Record(ctx, obs2); err != nil {
		t.Fatalf("Record obs2 failed: %v", err)
	}

	// Verify GetLatest returns obs2
	latest, err = repo.GetLatest(ctx)
	if err != nil {
		t.Fatalf("GetLatest failed: %v", err)
	}
	if latest.WeatherCode != 63 || latest.PrecipitationMm != 4.2 {
		t.Errorf("unexpected latest observation: %+v", latest)
	}

	// Test GetAsOf semantics (ADR-012 D3, Lesson 67: max age 30m, no forward fill):
	maxAge := 30 * time.Minute

	// Case 1: Exactly at 10:10 -> returns obs2
	asOf1010, err := repo.GetAsOf(ctx, obs2Time, maxAge)
	if err != nil {
		t.Fatalf("GetAsOf 10:10 failed: %v", err)
	}
	if asOf1010.WeatherCode != 63 {
		t.Errorf("expected obs2 (code 63), got %d", asOf1010.WeatherCode)
	}

	// Case 2: At 10:05 -> returns obs1 (obs2 is in the future relative to 10:05)
	asOf1005, err := repo.GetAsOf(ctx, baseTime.Add(5*time.Minute), maxAge)
	if err != nil {
		t.Fatalf("GetAsOf 10:05 failed: %v", err)
	}
	if asOf1005.WeatherCode != 1 {
		t.Errorf("expected obs1 (code 1), got %d", asOf1005.WeatherCode)
	}

	// Case 3: At 10:35 -> 25 minutes after obs2 (within 30m bound) -> returns obs2
	asOf1035, err := repo.GetAsOf(ctx, obs2Time.Add(25*time.Minute), maxAge)
	if err != nil {
		t.Fatalf("GetAsOf 10:35 failed: %v", err)
	}
	if asOf1035.WeatherCode != 63 {
		t.Errorf("expected obs2, got %d", asOf1035.WeatherCode)
	}

	// Case 4: At 10:40 -> exactly 30 minutes after obs2 -> returns obs2 (boundary)
	asOf1040, err := repo.GetAsOf(ctx, obs2Time.Add(30*time.Minute), maxAge)
	if err != nil {
		t.Fatalf("GetAsOf 10:40 failed: %v", err)
	}
	if asOf1040.WeatherCode != 63 {
		t.Errorf("expected obs2, got %d", asOf1040.WeatherCode)
	}

	// Case 5: At 10:41 -> 31 minutes after obs2 (exceeds 30m cutoff) -> ErrWeatherNotFound (no forward fill)
	asOf1041, err := repo.GetAsOf(ctx, obs2Time.Add(31*time.Minute), maxAge)
	if err != ErrWeatherNotFound {
		t.Fatalf("expected ErrWeatherNotFound at 31m past observation, got: %v (obs=%v)", err, asOf1041)
	}

	// Case 6: Target before any observation (e.g. 09:59) -> ErrWeatherNotFound
	asOf0959, err := repo.GetAsOf(ctx, baseTime.Add(-1*time.Minute), maxAge)
	if err != ErrWeatherNotFound {
		t.Fatalf("expected ErrWeatherNotFound at 09:59, got: %v (obs=%v)", err, asOf0959)
	}

	// Test GetHistory
	history, err := repo.GetHistory(ctx, baseTime, obs2Time, 10)
	if err != nil {
		t.Fatalf("GetHistory failed: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("expected 2 history entries, got %d", len(history))
	}
	if history[0].WeatherCode != 1 || history[1].WeatherCode != 63 {
		t.Errorf("unexpected history items: %+v", history)
	}
}
