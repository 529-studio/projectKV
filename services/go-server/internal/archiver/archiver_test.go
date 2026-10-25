package archiver_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/529-studio/projectKV/services/go-server/internal/archiver"
	"github.com/529-studio/projectKV/services/go-server/internal/hotset"
	"github.com/529-studio/projectKV/services/go-server/internal/storage"
)

func TestStoragePath(t *testing.T) {
	// Case 1: 15:00 UTC = 22:00 ICT (same date 2026-09-29)
	ts1 := time.Date(2026, 9, 29, 15, 0, 0, 0, time.UTC)
	path1 := archiver.StoragePath(ts1, "cam123", ts1.Unix(), archiver.ReasonStateChange)
	expected1 := "2026-09-29/cam123/1790694000_state_change.jpg"
	if path1 != expected1 {
		t.Errorf("expected %q, got %q", expected1, path1)
	}

	// Case 2: 18:00 UTC on 2026-09-29 = 01:00 ICT on 2026-09-30 (midnight crossover in Vietnam ICT)
	ts2 := time.Date(2026, 9, 29, 18, 0, 0, 0, time.UTC)
	path2 := archiver.StoragePath(ts2, "cam123", ts2.Unix(), archiver.ReasonSample)
	expected2 := "2026-09-30/cam123/1790704800_sample.jpg"
	if path2 != expected2 {
		t.Errorf("expected %q, got %q", expected2, path2)
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

// validArchiveJPEG builds a payload satisfying the archiver quality gate
// (SOI magic bytes + minimum size), keeping deterministic filler content.
func validArchiveJPEG(total int) []byte {
	if total < archiver.MinArchiveJPEGSizeBytes {
		total = archiver.MinArchiveJPEGSizeBytes
	}
	data := make([]byte, total)
	copy(data, []byte{0xFF, 0xD8, 0xFF, 0xE0})
	for i := 4; i < total; i++ {
		data[i] = byte(i % 251)
	}
	return data
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

	data := validArchiveJPEG(archiver.MinArchiveJPEGSizeBytes + 512)
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

func TestArchiver_OnVerdict_DirectRawFrame(t *testing.T) {
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
	rawPayload := validArchiveJPEG(0)

	v := hotset.Verdict{
		CameraID:  "cam_direct",
		Severity:  hotset.SeverityCongested,
		Timestamp: t0,
		RawFrame:  rawPayload,
	}

	// Pass nil frameReader: should automatically consume v.RawFrame
	if err := arch.OnVerdict(v, nil); err != nil {
		t.Fatalf("OnVerdict failed: %v", err)
	}

	expectedPath := archiver.StoragePath(t0, "cam_direct", t0.Unix(), archiver.ReasonStateChange)
	rc, err := store.Get(context.Background(), expectedPath)
	if err != nil {
		t.Fatalf("failed to read archived file: %v", err)
	}
	defer rc.Close()

	savedBytes, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("failed to read stream: %v", err)
	}
	if !bytes.Equal(savedBytes, rawPayload) {
		t.Errorf("expected archived content %q, got %q", rawPayload, savedBytes)
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

// TestArchiver_OnVerdict_RejectsPlaceholderFrames verifies the quality gate
// (Issue #305): undersized stubs (the empirically observed frozen 2638-byte
// upstream placeholder), non-SOI payloads, and empty frames are skipped with
// no storage write and no error — mirroring the harvester gate semantics.
func TestArchiver_OnVerdict_RejectsPlaceholderFrames(t *testing.T) {
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

	t0 := time.Date(2026, 10, 25, 7, 30, 0, 0, time.UTC)
	cases := []struct {
		name  string
		frame []byte
		raw   bool
	}{
		{"undersized upstream placeholder stub", append([]byte{0xFF, 0xD8}, make([]byte, 2636)...), false},
		{"non-SOI payload of sufficient size", make([]byte, archiver.MinArchiveJPEGSizeBytes+100), false},
		{"empty frame", nil, false},
		{"undersized stub via RawFrame path", append([]byte{0xFF, 0xD8}, make([]byte, 100)...), true},
	}

	for _, tc := range cases {
		v := hotset.Verdict{
			CameraID:  "cam_placeholder",
			Severity:  hotset.SeverityCongested,
			Timestamp: t0,
		}
		if tc.raw {
			v.RawFrame = tc.frame
			err = arch.OnVerdict(v, nil)
		} else {
			err = arch.OnVerdict(v, bytes.NewReader(tc.frame))
		}
		if err != nil {
			t.Errorf("%s: expected nil error on skipped frame, got %v", tc.name, err)
		}

		expectedPath := archiver.StoragePath(t0, "cam_placeholder", t0.Unix(), archiver.ReasonStateChange)
		exists, existsErr := store.Exists(context.Background(), expectedPath)
		if existsErr != nil {
			t.Fatalf("%s: Exists failed: %v", tc.name, existsErr)
		}
		if exists {
			t.Errorf("%s: expected NO archived file at %s, but it exists", tc.name, expectedPath)
		}
	}

	// Sanity: a valid frame still archives (fresh camera so policy hysteresis
	// does not suppress the state_change reason).
	valid := hotset.Verdict{
		CameraID:  "cam_valid",
		Severity:  hotset.SeverityCongested,
		Timestamp: t0.Add(time.Minute),
	}
	if err := arch.OnVerdict(valid, bytes.NewReader(validArchiveJPEG(0))); err != nil {
		t.Fatalf("valid frame should archive, got %v", err)
	}
	validPath := archiver.StoragePath(t0.Add(time.Minute), "cam_valid", t0.Add(time.Minute).Unix(), archiver.ReasonStateChange)
	exists, err := store.Exists(context.Background(), validPath)
	if err != nil || !exists {
		t.Errorf("expected valid frame archived at %s (exists=%v err=%v)", validPath, exists, err)
	}
}
