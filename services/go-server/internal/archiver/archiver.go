package archiver

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/529-studio/projectKV/services/go-server/internal/hotset"
	"github.com/529-studio/projectKV/services/go-server/internal/storage"
)

// MockJPEGData is a minimal valid 1x1 pixel JPEG file used for mock/dev frame archiving.
var MockJPEGData = []byte{
	0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 0x4a, 0x46, 0x49, 0x46, 0x00, 0x01,
	0x01, 0x01, 0x00, 0x48, 0x00, 0x48, 0x00, 0x00, 0xff, 0xdb, 0x00, 0x43,
	0x00, 0x08, 0x06, 0x06, 0x07, 0x06, 0x05, 0x08, 0x07, 0x07, 0x07, 0x09,
	0x09, 0x08, 0x0a, 0x0c, 0x14, 0x0d, 0x0c, 0x0b, 0x0b, 0x0c, 0x19, 0x12,
	0x13, 0x0f, 0x14, 0x1d, 0x1a, 0x1f, 0x1e, 0x1d, 0x1a, 0x1c, 0x1c, 0x20,
	0x24, 0x2e, 0x27, 0x20, 0x22, 0x2c, 0x23, 0x1c, 0x1c, 0x28, 0x37, 0x29,
	0x2c, 0x30, 0x31, 0x34, 0x34, 0x34, 0x1f, 0x27, 0x39, 0x3d, 0x38, 0x32,
	0x3c, 0x2e, 0x33, 0x34, 0x32, 0xff, 0xc0, 0x00, 0x0b, 0x08, 0x00, 0x01,
	0x00, 0x01, 0x01, 0x01, 0x11, 0x00, 0xff, 0xc4, 0x00, 0x1f, 0x00, 0x00,
	0x01, 0x05, 0x01, 0x01, 0x01, 0x01, 0x01, 0x01, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
	0x09, 0x0a, 0x0b, 0xff, 0xda, 0x00, 0x08, 0x01, 0x01, 0x00, 0x00, 0x3f,
	0x00, 0xbf, 0x00, 0xff, 0xd9,
}

// Config specifies operating parameters for snapshot archiving and retention.
type Config struct {
	SampleInterval time.Duration
	RetentionDays  int
	MaxDiskMB      int64
	CheckInterval  time.Duration
}

// Archiver coordinates event-driven snapshot archiving, policy decisions, and retention.
type Archiver struct {
	store     storage.Storage
	policy    *PolicyEngine
	retention *RetentionJob
	log       *slog.Logger
}

// New creates a new Archiver with the given storage driver, root directory, and config.
func New(store storage.Storage, rootDir string, cfg Config, log *slog.Logger) *Archiver {
	if log == nil {
		log = slog.Default()
	}
	if cfg.SampleInterval <= 0 {
		cfg.SampleInterval = 300 * time.Second
	}
	if cfg.RetentionDays <= 0 {
		cfg.RetentionDays = 14
	}
	if cfg.MaxDiskMB <= 0 {
		cfg.MaxDiskMB = 20000
	}
	if cfg.CheckInterval <= 0 {
		cfg.CheckInterval = 1 * time.Hour
	}

	policy := NewPolicyEngine(cfg.SampleInterval)
	retention := NewRetentionJob(store, rootDir, cfg.RetentionDays, cfg.MaxDiskMB, cfg.CheckInterval, log)

	return &Archiver{
		store:     store,
		policy:    policy,
		retention: retention,
		log:       log,
	}
}

// Policy returns the underlying policy engine (e.g. for testing or eval tagging).
func (a *Archiver) Policy() *PolicyEngine {
	return a.policy
}

// Retention returns the underlying retention job.
func (a *Archiver) Retention() *RetentionJob {
	return a.retention
}

// OnVerdict evaluates the incoming verdict against policy and archives the frame if eligible.
func (a *Archiver) OnVerdict(v hotset.Verdict, frameReader io.Reader) error {
	now := time.Now().UTC()
	shouldArchive, reason := a.policy.ShouldArchive(v, now)
	if !shouldArchive {
		return nil
	}

	if frameReader == nil {
		a.log.Warn("archiver skipped: frame reader is nil",
			"camera", v.CameraID,
			"severity", v.Severity,
			"reason", reason,
		)
		return nil
	}

	ts := v.Timestamp
	if ts.IsZero() {
		ts = now
	}
	path := StoragePath(ts, v.CameraID, ts.Unix(), reason)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := a.store.Put(ctx, path, frameReader); err != nil {
		a.log.Error("failed to archive frame",
			"camera", v.CameraID,
			"path", path,
			"reason", reason,
			"error", err,
		)
		return fmt.Errorf("archive frame %s: %w", path, err)
	}

	a.log.Info("snapshot archived successfully",
		"camera", v.CameraID,
		"path", path,
		"reason", reason,
	)
	return nil
}

// StartRetention begins the periodic retention background goroutine.
func (a *Archiver) StartRetention(ctx context.Context) {
	a.retention.Start(ctx)
}
