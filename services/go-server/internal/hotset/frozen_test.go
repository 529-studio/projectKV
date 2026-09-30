package hotset_test

import (
	"testing"

	"github.com/529-studio/projectKV/services/go-server/internal/hotset"
)

func TestFrozenDetector_ThresholdThree_IdenticalMarksStale(t *testing.T) {
	d := hotset.NewFrozenDetector(3)
	camID := "cam-101"
	hash := "sha256_hash_aaa"

	// 1st cycle: first frame seen -> not stale
	if d.Check(camID, hash) {
		t.Errorf("expected 1st frame to not be stale")
	}

	// 2nd cycle: identical frame -> not stale yet (threshold 3)
	if d.Check(camID, hash) {
		t.Errorf("expected 2nd identical frame to not be stale")
	}

	// 3rd cycle: 3rd consecutive identical frame -> STALE!
	if !d.Check(camID, hash) {
		t.Errorf("expected 3rd identical frame to be marked stale")
	}

	// 4th cycle: continues identical -> remains stale
	if !d.Check(camID, hash) {
		t.Errorf("expected 4th identical frame to remain stale")
	}
}

func TestFrozenDetector_DifferentHashResetsCounter(t *testing.T) {
	d := hotset.NewFrozenDetector(3)
	camID := "cam-102"

	// 1st cycle: hashA -> not stale (count 1)
	if d.Check(camID, "hashA") {
		t.Errorf("expected 1st frame to not be stale")
	}

	// 2nd cycle: hashA -> not stale (count 2)
	if d.Check(camID, "hashA") {
		t.Errorf("expected 2nd frame to not be stale")
	}

	// 3rd cycle: hashB (camera moved / live traffic update) -> resets count to 1, not stale!
	if d.Check(camID, "hashB") {
		t.Errorf("expected changed frame to reset counter and not be stale")
	}

	// 4th cycle: hashB -> count 2, not stale
	if d.Check(camID, "hashB") {
		t.Errorf("expected 2nd hashB to not be stale")
	}

	// 5th cycle: hashB -> count 3, STALE!
	if !d.Check(camID, "hashB") {
		t.Errorf("expected 3rd hashB to be marked stale")
	}
}

func TestFrozenDetector_MultipleCamerasIndependent(t *testing.T) {
	d := hotset.NewFrozenDetector(3)

	// Cam A freezes
	d.Check("camA", "hash1")
	d.Check("camA", "hash1")
	if !d.Check("camA", "hash1") {
		t.Errorf("expected camA to be stale on 3rd identical frame")
	}

	// Cam B is healthy and live
	d.Check("camB", "hashX")
	d.Check("camB", "hashY")
	if d.Check("camB", "hashZ") {
		t.Errorf("expected camB with changing frames to never be stale")
	}
}

func TestFrozenDetector_CheckBytes(t *testing.T) {
	d := hotset.NewFrozenDetector(3)
	camID := "cam-bytes"
	imgA := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46}
	imgB := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x47}

	if d.CheckBytes(camID, imgA) {
		t.Errorf("cycle 1: should not be stale")
	}
	if d.CheckBytes(camID, imgA) {
		t.Errorf("cycle 2: should not be stale")
	}
	if !d.CheckBytes(camID, imgA) {
		t.Errorf("cycle 3: should be stale")
	}

	// New frame byte payload resets
	if d.CheckBytes(camID, imgB) {
		t.Errorf("cycle 4 (new image): should not be stale")
	}
}

func TestFrozenDetector_EmptyAndReset(t *testing.T) {
	d := hotset.NewFrozenDetector(3)

	if d.Check("", "hash") {
		t.Errorf("empty cameraID must return false")
	}
	if d.Check("cam", "") {
		t.Errorf("empty hash must return false")
	}
	if d.CheckBytes("cam", nil) {
		t.Errorf("empty bytes must return false")
	}

	d.Check("cam-reset", "hash")
	d.Check("cam-reset", "hash")
	d.Check("cam-reset", "hash") // now stale
	if !d.Check("cam-reset", "hash") {
		t.Errorf("should be stale before reset")
	}

	d.Reset("cam-reset")
	if d.Check("cam-reset", "hash") {
		t.Errorf("should not be stale immediately after reset")
	}
}
