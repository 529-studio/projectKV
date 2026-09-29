package archiver_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/529-studio/projectKV/services/go-server/internal/archiver"
	"github.com/529-studio/projectKV/services/go-server/internal/hotset"
	"github.com/529-studio/projectKV/services/go-server/internal/storage"
)

func TestStoragePath(t *testing.T) {
	ts := time.Date(2026, 9, 29, 15, 0, 0, 0, time.UTC)
	path := archiver.StoragePath(ts, "cam123", ts.Unix(), archiver.ReasonStateChange)
	expected := "2026-09-29/cam123/1790694000_state_change.jpg"
	if path != expected {
		t.Errorf("expected %q, got %q", expected, path)
	}
}

func TestPolicyEngine_StateChange(t *testing.T) {
	policy := archiver.NewPolicyEngine(300 * time.Second)
	t0 := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)

	// 1. Initial CLEAR -> Sampled (as first frame)
	v1 := hotset.Verdict{
		CameraID:  "cam1",
		Severity:  hotset.SeverityClear,
		Timestamp: t0,
	}
	shouldSave, reason := policy.ShouldArchive(v1, t0)
	if !shouldSave || reason != archiver.ReasonSample {
		t.Errorf("expected initial frame to be sampled, got shouldSave=%v reason=%s", shouldSave, reason)
	}

	// 2. CLEAR -> CLEAR 10s later: no state change, within sample window -> skip
	t1 := t0.Add(10 * time.Second)
	v2 := hotset.Verdict{
		CameraID:  "cam1",
		Severity:  hotset.SeverityClear,
		Timestamp: t1,
	}
	shouldSave, reason = policy.ShouldArchive(v2, t1)
	if shouldSave {
		t.Errorf("expected steady CLEAR to be skipped, got shouldSave=%v reason=%s", shouldSave, reason)
	}

	// 3. CLEAR -> CONGESTED: state change to jam!
	t2 := t0.Add(30 * time.Second)
	v3 := hotset.Verdict{
		CameraID:  "cam1",
		Severity:  hotset.SeverityCongested,
		Timestamp: t2,
	}
	shouldSave, reason = policy.ShouldArchive(v3, t2)
	if !shouldSave || reason != archiver.ReasonStateChange {
		t.Errorf("expected state change to CONGESTED, got shouldSave=%v reason=%s", shouldSave, reason)
	}

	// 4. CONGESTED -> CONGESTED 20s later: same severity, within sample window -> skip
	t3 := t2.Add(20 * time.Second)
	v4 := hotset.Verdict{
		CameraID:  "cam1",
		Severity:  hotset.SeverityCongested,
		Timestamp: t3,
	}
	shouldSave, reason = policy.ShouldArchive(v4, t3)
	if shouldSave {
		t.Errorf("expected consecutive CONGESTED within sample interval to be skipped, got reason=%s", reason)
	}

	// 5. CONGESTED -> FLOODED_DEEP: state change to deep flood!
	t4 := t3.Add(10 * time.Second)
	v5 := hotset.Verdict{
		CameraID:  "cam1",
		Severity:  hotset.SeverityFloodedDeep,
		Timestamp: t4,
	}
	shouldSave, reason = policy.ShouldArchive(v5, t4)
	if !shouldSave || reason != archiver.ReasonStateChange {
		t.Errorf("expected state change to FLOODED_DEEP, got shouldSave=%v reason=%s", shouldSave, reason)
	}

	// 6. FLOODED_DEEP -> CLEAR: transition back to clear is not jam/deep -> skip if within sample interval
	t5 := t4.Add(15 * time.Second)
	v6 := hotset.Verdict{
		CameraID:  "cam1",
		Severity:  hotset.SeverityClear,
		Timestamp: t5,
	}
	shouldSave, _ = policy.ShouldArchive(v6, t5)
	if shouldSave {
		t.Errorf("transition to CLEAR within sample window should not be archived")
	}
}

func TestPolicyEngine_SamplingInterval(t *testing.T) {
	interval := 300 * time.Second
	policy := archiver.NewPolicyEngine(interval)
	t0 := time.Date(2026, 9, 29, 8, 0, 0, 0, time.UTC)

	// First verdict: sampled
	v := hotset.Verdict{CameraID: "camA", Severity: hotset.SeverityClear, Timestamp: t0}
	shouldSave, reason := policy.ShouldArchive(v, t0)
	if !shouldSave || reason != archiver.ReasonSample {
		t.Fatalf("first frame should be sampled: %v, %s", shouldSave, reason)
	}

	// 299s later: still within interval -> skip
	t299 := t0.Add(299 * time.Second)
	v.Timestamp = t299
	shouldSave, _ = policy.ShouldArchive(v, t299)
	if shouldSave {
		t.Errorf("verdict before sample interval expired should not be archived")
	}

	// 301s later: interval elapsed -> sample again
	t301 := t0.Add(301 * time.Second)
	v.Timestamp = t301
	shouldSave, reason = policy.ShouldArchive(v, t301)
	if !shouldSave || reason != archiver.ReasonSample {
		t.Errorf("expected sample after interval, got shouldSave=%v reason=%s", shouldSave, reason)
	}
}

func TestPolicyEngine_EvalSet(t *testing.T) {
	policy := archiver.NewPolicyEngine(300 * time.Second)
	t0 := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)

	// Explicit eval source
	v1 := hotset.Verdict{
		CameraID:  "camE1",
		Severity:  hotset.SeverityClear,
		Source:    "eval",
		Timestamp: t0,
	}
	shouldSave, reason := policy.ShouldArchive(v1, t0)
	if !shouldSave || reason != archiver.ReasonEval {
		t.Errorf("expected eval verdict to be archived with ReasonEval, got %v, %s", shouldSave, reason)
	}

	// Explicitly tagged camera
	policy.TagForEval("camE2", true)
	v2 := hotset.Verdict{
		CameraID:  "camE2",
		Severity:  hotset.SeverityClear,
		Source:    "vlm",
		Timestamp: t0,
	}
	shouldSave, reason = policy.ShouldArchive(v2, t0)
	if !shouldSave || reason != archiver.ReasonEval {
		t.Errorf("expected tagged camera to be archived with ReasonEval, got %v, %s", shouldSave, reason)
	}
}

func TestArchiver_OnVerdict(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := storage.NewLocalStorage(tmpDir)
	if err != nil {
		t.Fatalf("failed to create local storage: %v", err)
	}

	cfg := archiver.Config{
		SampleInterval: 300 * time.Second,
		RetentionDays:  14,
		MaxDiskMB:      100,
	}
	arch := archiver.New(store, tmpDir, cfg, nil)

	t0 := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	v := hotset.Verdict{
		CameraID:  "cam_test",
		Severity:  hotset.SeverityCongested,
		Timestamp: t0,
	}

	data := []byte("fake jpeg payload")
	err = arch.OnVerdict(v, bytes.NewReader(data))
	if err != nil {
		t.Fatalf("OnVerdict failed: %v", err)
	}

	expectedPath := archiver.StoragePath(t0, "cam_test", t0.Unix(), archiver.ReasonStateChange)
	exists, err := store.Exists(context.Background(), expectedPath)
	if err != nil {
		t.Fatalf("Exists failed: %v", err)
	}
	if !exists {
		t.Errorf("expected archived file at %s, but does not exist", expectedPath)
	}

	// Nil frame reader should be handled gracefully without error or crash
	err = arch.OnVerdict(v, nil)
	if err != nil {
		t.Errorf("OnVerdict with nil frameReader returned error: %v", err)
	}
}

func TestRetention_AgeBasedDeletion(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := storage.NewLocalStorage(tmpDir)
	if err != nil {
		t.Fatalf("failed to create local storage: %v", err)
	}

	retentionDays := 14
	retention := archiver.NewRetentionJob(store, tmpDir, retentionDays, 20000, time.Hour, nil)

	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)

	// Old file: 20 days ago (older than 14 days -> should be deleted)
	oldTime := now.Add(-20 * 24 * time.Hour)
	oldPath := archiver.StoragePath(oldTime, "camOld", oldTime.Unix(), archiver.ReasonStateChange)
	if err := store.Put(context.Background(), oldPath, bytes.NewReader([]byte("old data"))); err != nil {
		t.Fatalf("failed to put old file: %v", err)
	}

	// Fresh file: 2 days ago (within 14 days -> should be kept)
	newTime := now.Add(-2 * 24 * time.Hour)
	newPath := archiver.StoragePath(newTime, "camNew", newTime.Unix(), archiver.ReasonSample)
	if err := store.Put(context.Background(), newPath, bytes.NewReader([]byte("new data"))); err != nil {
		t.Fatalf("failed to put new file: %v", err)
	}

	deleted, _, err := retention.RunOnce(context.Background(), now)
	if err != nil {
		t.Fatalf("RunOnce failed: %v", err)
	}
	if deleted != 1 {
		t.Errorf("expected 1 file deleted, got %d", deleted)
	}

	// Verify old file is gone
	oldExists, _ := store.Exists(context.Background(), oldPath)
	if oldExists {
		t.Errorf("old file should have been deleted: %s", oldPath)
	}

	// Verify new file still exists
	newExists, _ := store.Exists(context.Background(), newPath)
	if !newExists {
		t.Errorf("new file should be preserved: %s", newPath)
	}
}

func TestRetention_DiskCapEnforcement(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := storage.NewLocalStorage(tmpDir)
	if err != nil {
		t.Fatalf("failed to create local storage: %v", err)
	}

	// Set disk cap to 1 MB
	retentionDays := 30
	retention := archiver.NewRetentionJob(store, tmpDir, retentionDays, 1, time.Hour, nil)

	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)

	// Create 3 files of 600 KB each (total 1.8 MB > 1 MB cap)
	// File 1: 3 hours ago (oldest)
	// File 2: 2 hours ago (middle)
	// File 3: 1 hour ago (newest)
	fileSize := 600 * 1024
	payload := make([]byte, fileSize)

	t1 := now.Add(-3 * time.Hour)
	p1 := archiver.StoragePath(t1, "cam1", t1.Unix(), archiver.ReasonSample)
	if err := store.Put(context.Background(), p1, bytes.NewReader(payload)); err != nil {
		t.Fatalf("failed to put p1: %v", err)
	}

	t2 := now.Add(-2 * time.Hour)
	p2 := archiver.StoragePath(t2, "cam2", t2.Unix(), archiver.ReasonSample)
	if err := store.Put(context.Background(), p2, bytes.NewReader(payload)); err != nil {
		t.Fatalf("failed to put p2: %v", err)
	}

	t3 := now.Add(-1 * time.Hour)
	p3 := archiver.StoragePath(t3, "cam3", t3.Unix(), archiver.ReasonSample)
	if err := store.Put(context.Background(), p3, bytes.NewReader(payload)); err != nil {
		t.Fatalf("failed to put p3: %v", err)
	}

	deleted, _, err := retention.RunOnce(context.Background(), now)
	if err != nil {
		t.Fatalf("RunOnce failed: %v", err)
	}

	// 1.8 MB total, cap is 1 MB. Deleting oldest (p1 = 600KB) leaves 1.2 MB > 1 MB.
	// Deleting middle (p2 = 600KB) leaves 600 KB <= 1 MB.
	// So 2 files should be deleted.
	if deleted != 2 {
		t.Errorf("expected 2 files deleted for disk cap, got %d", deleted)
	}

	p1Exists, _ := store.Exists(context.Background(), p1)
	if p1Exists {
		t.Errorf("oldest file p1 should have been deleted")
	}

	p2Exists, _ := store.Exists(context.Background(), p2)
	if p2Exists {
		t.Errorf("middle file p2 should have been deleted")
	}

	p3Exists, _ := store.Exists(context.Background(), p3)
	if !p3Exists {
		t.Errorf("newest file p3 should have been kept")
	}
}

func TestRetention_CleansEmptyDirectories(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := storage.NewLocalStorage(tmpDir)
	if err != nil {
		t.Fatalf("failed to create local storage: %v", err)
	}

	retention := archiver.NewRetentionJob(store, tmpDir, 1, 100, time.Hour, nil)
	now := time.Now().UTC()
	oldTime := now.Add(-5 * 24 * time.Hour)

	oldPath := archiver.StoragePath(oldTime, "camDirTest", oldTime.Unix(), archiver.ReasonSample)
	if err := store.Put(context.Background(), oldPath, bytes.NewReader([]byte("test"))); err != nil {
		t.Fatalf("failed to put file: %v", err)
	}

	parentDir := filepath.Join(tmpDir, filepath.Dir(oldPath))
	if _, err := os.Stat(parentDir); err != nil {
		t.Fatalf("directory should exist before retention: %v", err)
	}

	_, _, err = retention.RunOnce(context.Background(), now)
	if err != nil {
		t.Fatalf("RunOnce failed: %v", err)
	}

	// Directory should be cleaned up after deleting the only file in it
	if _, err := os.Stat(parentDir); !os.IsNotExist(err) {
		t.Errorf("empty parent directory should have been removed: %s", parentDir)
	}
}
