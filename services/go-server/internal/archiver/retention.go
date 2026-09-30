package archiver

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/529-studio/projectKV/services/go-server/internal/storage"
)

// fileEntry stores metadata for an archived snapshot file.
type fileEntry struct {
	relPath   string
	absPath   string
	size      int64
	timestamp time.Time
}

// RetentionJob enforces file age retention and disk usage caps.
type RetentionJob struct {
	store         storage.Storage
	rootDir       string
	retentionDays int
	maxDiskMB     int64
	checkInterval time.Duration
	log           *slog.Logger
}

// NewRetentionJob creates a new retention job.
func NewRetentionJob(store storage.Storage, rootDir string, retentionDays int, maxDiskMB int64, checkInterval time.Duration, log *slog.Logger) *RetentionJob {
	if log == nil {
		log = slog.Default()
	}
	if checkInterval <= 0 {
		checkInterval = 1 * time.Hour
	}
	return &RetentionJob{
		store:         store,
		rootDir:       rootDir,
		retentionDays: retentionDays,
		maxDiskMB:     maxDiskMB,
		checkInterval: checkInterval,
		log:           log,
	}
}

// parseTimestampFromFilename extracts unix timestamp from "{timestamp}_{reason}.jpg"
// and falls back to file modification time if parsing fails.
func parseTimestampFromFilename(baseName string, fallbackModTime time.Time) time.Time {
	parts := strings.SplitN(baseName, "_", 2)
	if len(parts) >= 1 {
		if ts, err := strconv.ParseInt(parts[0], 10, 64); err == nil {
			return time.Unix(ts, 0).UTC()
		}
	}
	return fallbackModTime.UTC()
}

// deleteFile removes the file via storage.Storage or os.Remove.
func (r *RetentionJob) deleteFile(ctx context.Context, relPath, absPath string) error {
	if r.store != nil && relPath != "" {
		err := r.store.Delete(ctx, relPath)
		if err == nil || errors.Is(err, storage.ErrNotFound) {
			return nil
		}
	}
	return os.Remove(absPath)
}

// RunOnce executes a single retention pass:
// 1. Deletes files older than retentionDays.
// 2. Enforces maxDiskMB cap (deleting oldest files first).
// 3. Cleans up empty directories.
func (r *RetentionJob) RunOnce(ctx context.Context, now time.Time) (int, int64, error) {
	if r.rootDir == "" {
		return 0, 0, nil
	}
	absRoot, err := filepath.Abs(r.rootDir)
	if err != nil {
		return 0, 0, err
	}
	if _, err := os.Stat(absRoot); os.IsNotExist(err) {
		return 0, 0, nil
	}

	if now.IsZero() {
		now = time.Now().UTC()
	}

	maxAge := time.Duration(r.retentionDays) * 24 * time.Hour
	var activeFiles []fileEntry
	var dirs []string
	var totalBytes int64
	var deletedCount int
	var freedBytes int64

	// Walk archive root
	err = filepath.WalkDir(absRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if d.IsDir() {
			if path != absRoot {
				dirs = append(dirs, path)
			}
			return nil
		}

		name := d.Name()
		// Skip temporary files
		if strings.HasPrefix(name, ".tmp.") || strings.HasPrefix(name, ".") {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return nil
		}

		rel, err := filepath.Rel(absRoot, path)
		if err != nil {
			return nil
		}

		ts := parseTimestampFromFilename(name, info.ModTime())
		fileAge := now.Sub(ts)

		// 1. Delete if older than retentionDays
		if r.retentionDays > 0 && fileAge > maxAge {
			if delErr := r.deleteFile(ctx, rel, path); delErr == nil {
				deletedCount++
				freedBytes += info.Size()
			}
			return nil
		}

		// Keep tracking for disk cap enforcement
		activeFiles = append(activeFiles, fileEntry{
			relPath:   rel,
			absPath:   path,
			size:      info.Size(),
			timestamp: ts,
		})
		totalBytes += info.Size()
		return nil
	})

	if err != nil && !errors.Is(err, context.Canceled) {
		return deletedCount, freedBytes, err
	}

	// 2. Enforce disk cap: delete oldest files first
	maxDiskBytes := r.maxDiskMB * 1024 * 1024
	if r.maxDiskMB > 0 && totalBytes > maxDiskBytes {
		// Sort oldest first
		sort.Slice(activeFiles, func(i, j int) bool {
			return activeFiles[i].timestamp.Before(activeFiles[j].timestamp)
		})

		for _, f := range activeFiles {
			if totalBytes <= maxDiskBytes || ctx.Err() != nil {
				break
			}
			if delErr := r.deleteFile(ctx, f.relPath, f.absPath); delErr == nil {
				deletedCount++
				freedBytes += f.size
				totalBytes -= f.size
			}
		}
	}

	// 3. Clean up empty directories (bottom-up by sorting length descending)
	sort.Slice(dirs, func(i, j int) bool {
		return len(dirs[i]) > len(dirs[j])
	})
	for _, d := range dirs {
		_ = os.Remove(d) // Fails silently if directory is not empty
	}

	if deletedCount > 0 {
		r.log.Info("retention pass completed",
			"deleted_files", deletedCount,
			"freed_mb", float64(freedBytes)/(1024*1024),
			"remaining_mb", float64(totalBytes)/(1024*1024),
		)
	}

	return deletedCount, freedBytes, nil
}

// Start runs the background retention loop until ctx is cancelled.
func (r *RetentionJob) Start(ctx context.Context) {
	ticker := time.NewTicker(r.checkInterval)
	defer ticker.Stop()

	// Initial run on startup
	if _, _, err := r.RunOnce(ctx, time.Now()); err != nil {
		r.log.Warn("initial retention run failed", "error", err)
	}

	for {
		select {
		case <-ctx.Done():
			r.log.Info("retention worker stopping")
			return
		case <-ticker.C:
			if _, _, err := r.RunOnce(ctx, time.Now()); err != nil {
				r.log.Warn("periodic retention run failed", "error", err)
			}
		}
	}
}
