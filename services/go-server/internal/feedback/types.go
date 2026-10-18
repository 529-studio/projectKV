package feedback

import (
	"context"
	"io"
	"time"
)

// DefaultRateLimitCooldown defines the sliding window cooldown per IP per camera.
const DefaultRateLimitCooldown = 5 * time.Minute

// SnapshotFetcher defines the consumer interface for retrieving camera frame bytes.
type SnapshotFetcher interface {
	FetchSnapshot(ctx context.Context, cameraID string) ([]byte, error)
}

// CameraChecker defines the consumer interface for verifying camera existence in catalog.
type CameraChecker interface {
	CameraExists(cameraID string) bool
}

// StorageUploader defines the consumer interface for persisting files to object storage.
type StorageUploader interface {
	Put(ctx context.Context, path string, data io.Reader) error
}

// ReportResponse represents the JSON response returned to the client upon successful submission.
type ReportResponse struct {
	Status    string    `json:"status" example:"success"`
	Message   string    `json:"message" example:"Ghi nhận phản ánh ngập thành công"`
	CameraID  string    `json:"camera_id" example:"586e2995f9fab7001111b0b5"`
	Timestamp time.Time `json:"timestamp"`
}

// ErrorResponse represents an error response payload.
type ErrorResponse struct {
	Error             string `json:"error" example:"rate_limited"`
	Message           string `json:"message" example:"Too many reports for this camera. Please try again later."`
	RetryAfterSeconds int    `json:"retry_after_seconds,omitempty" example:"300"`
}

// ReportMetadata represents the manifest persisted to Cloudflare R2 alongside the snapshot.
type ReportMetadata struct {
	Source       string `json:"source"`
	Trust        string `json:"trust"`
	CameraID     string `json:"camera_id"`
	Timestamp    string `json:"timestamp"`
	ClientIPHash string `json:"client_ip_hash"`
	UserAgent    string `json:"user_agent"`
	HasSnapshot  bool   `json:"has_snapshot"`
}
