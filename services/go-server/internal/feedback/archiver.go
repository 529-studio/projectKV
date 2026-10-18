package feedback

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"
)

// Archiver asynchronously captures snapshots and stores feedback manifests to object storage.
type Archiver struct {
	fetcher SnapshotFetcher
	storage StorageUploader
	loc     *time.Location
}

// NewArchiver constructs an Archiver with injected dependencies.
func NewArchiver(fetcher SnapshotFetcher, storage StorageUploader, loc *time.Location) *Archiver {
	if loc == nil {
		loc = time.FixedZone("ICT", 7*3600)
	}
	return &Archiver{
		fetcher: fetcher,
		storage: storage,
		loc:     loc,
	}
}

// HashClientIP computes a secure SHA-256 hex digest of the client IP to preserve user privacy.
func HashClientIP(clientIP string) string {
	if clientIP == "" {
		return ""
	}
	hash := sha256.Sum256([]byte(clientIP))
	return fmt.Sprintf("%x", hash)
}

// BuildStoragePaths formats the R2 object keys for the image snapshot and metadata manifest.
func (a *Archiver) BuildStoragePaths(cameraID string, ts time.Time) (imagePath, metaPath string) {
	datePath := ts.In(a.loc).Format("2006-01-02")
	base := fmt.Sprintf("feedback/%s/%s_%d", datePath, cameraID, ts.Unix())
	return base + ".jpg", base + ".json"
}

// Archive synchronously processes and persists a user flood report.
func (a *Archiver) Archive(ctx context.Context, cameraID, clientIP, userAgent string, ts time.Time) error {
	var frame []byte
	var fetchErr error

	if a.fetcher != nil {
		frame, fetchErr = a.fetcher.FetchSnapshot(ctx, cameraID)
		if fetchErr != nil {
			slog.Warn("feedback: failed to fetch live snapshot", "camera_id", cameraID, "err", fetchErr)
		}
	}

	hasSnapshot := len(frame) > 0
	imagePath, metaPath := a.BuildStoragePaths(cameraID, ts)

	// 1. Upload snapshot JPEG if available and storage driver configured
	if hasSnapshot && a.storage != nil {
		if err := a.storage.Put(ctx, imagePath, bytes.NewReader(frame)); err != nil {
			slog.Warn("feedback: failed to upload snapshot to storage", "path", imagePath, "err", err)
		}
	}

	// 2. Build metadata manifest
	meta := ReportMetadata{
		Source:       "user_reported",
		Trust:        "unverified",
		CameraID:     cameraID,
		Timestamp:    ts.In(a.loc).Format(time.RFC3339),
		ClientIPHash: HashClientIP(clientIP),
		UserAgent:    userAgent,
		HasSnapshot:  hasSnapshot,
	}

	metaBytes, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("feedback: failed to marshal metadata: %w", err)
	}

	// 3. Upload metadata JSON to storage
	if a.storage != nil {
		if err := a.storage.Put(ctx, metaPath, bytes.NewReader(metaBytes)); err != nil {
			return fmt.Errorf("feedback: failed to upload metadata manifest: %w", err)
		}
	}

	slog.Info("feedback: successfully archived user flood report",
		"camera_id", cameraID,
		"has_snapshot", hasSnapshot,
		"meta_path", metaPath)

	return nil
}

// ArchiveAsync dispatches the archiving process in a background goroutine with a 20-second timeout.
func (a *Archiver) ArchiveAsync(cameraID, clientIP, userAgent string, ts time.Time) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()

		if err := a.Archive(ctx, cameraID, clientIP, userAgent, ts); err != nil {
			slog.Warn("feedback: background archive failed", "camera_id", cameraID, "err", err)
		}
	}()
}
