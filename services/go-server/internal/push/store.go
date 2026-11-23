package push

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Subscription represents client push credentials registered per W3C Web Push specifications.
type Subscription struct {
	ID        string    `json:"id"`
	Endpoint  string    `json:"endpoint"`
	P256dh    string    `json:"p256dh"`
	Auth      string    `json:"auth"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// HashEndpoint computes a deterministic SHA-256 identifier for a push service endpoint.
func HashEndpoint(endpoint string) string {
	h := sha256.Sum256([]byte(strings.TrimSpace(endpoint)))
	return hex.EncodeToString(h[:])
}

// Store defines persistence operations for Web Push client credentials.
type Store interface {
	Save(ctx context.Context, sub Subscription) (string, error)
	GetByID(ctx context.Context, id string) (Subscription, bool, error)
	GetByEndpoint(ctx context.Context, endpoint string) (Subscription, bool, error)
	GetByIDs(ctx context.Context, ids []string) ([]Subscription, error)
	Delete(ctx context.Context, id string) error
	DeleteByEndpoint(ctx context.Context, endpoint string) error
}

// SQLiteStore implements Store using hand-written SQL on SQLite (no ORM per Rule 7).
type SQLiteStore struct {
	db *sql.DB
}

// NewStore initializes a new SQLiteStore instance.
func NewStore(db *sql.DB) *SQLiteStore {
	return &SQLiteStore{db: db}
}

// Save inserts or updates a Web Push subscription. Returns the computed ID.
func (s *SQLiteStore) Save(ctx context.Context, sub Subscription) (string, error) {
	endpoint := strings.TrimSpace(sub.Endpoint)
	if endpoint == "" {
		return "", errors.New("endpoint cannot be empty")
	}
	p256dh := strings.TrimSpace(sub.P256dh)
	if p256dh == "" {
		return "", errors.New("p256dh key cannot be empty")
	}
	auth := strings.TrimSpace(sub.Auth)
	if auth == "" {
		return "", errors.New("auth secret cannot be empty")
	}

	id := HashEndpoint(endpoint)
	now := time.Now().UTC()

	query := `
		INSERT INTO web_push_subscriptions (id, endpoint, p256dh, auth, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(endpoint) DO UPDATE SET
			p256dh = excluded.p256dh,
			auth = excluded.auth,
			updated_at = excluded.updated_at
	`

	_, err := s.db.ExecContext(ctx, query, id, endpoint, p256dh, auth, now, now)
	if err != nil {
		return "", fmt.Errorf("save web push subscription: %w", err)
	}

	return id, nil
}

// GetByID retrieves a subscription by its SHA-256 ID.
func (s *SQLiteStore) GetByID(ctx context.Context, id string) (Subscription, bool, error) {
	query := `
		SELECT id, endpoint, p256dh, auth, created_at, updated_at
		FROM web_push_subscriptions
		WHERE id = ?
	`
	var sub Subscription
	err := s.db.QueryRowContext(ctx, query, strings.TrimSpace(id)).Scan(
		&sub.ID,
		&sub.Endpoint,
		&sub.P256dh,
		&sub.Auth,
		&sub.CreatedAt,
		&sub.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Subscription{}, false, nil
		}
		return Subscription{}, false, fmt.Errorf("get web push subscription by id (%s): %w", id, err)
	}
	return sub, true, nil
}

// GetByEndpoint retrieves a subscription by its push service endpoint URL.
func (s *SQLiteStore) GetByEndpoint(ctx context.Context, endpoint string) (Subscription, bool, error) {
	query := `
		SELECT id, endpoint, p256dh, auth, created_at, updated_at
		FROM web_push_subscriptions
		WHERE endpoint = ?
	`
	var sub Subscription
	err := s.db.QueryRowContext(ctx, query, strings.TrimSpace(endpoint)).Scan(
		&sub.ID,
		&sub.Endpoint,
		&sub.P256dh,
		&sub.Auth,
		&sub.CreatedAt,
		&sub.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Subscription{}, false, nil
		}
		return Subscription{}, false, fmt.Errorf("get web push subscription by endpoint: %w", err)
	}
	return sub, true, nil
}

// GetByIDs returns all existing subscriptions matching the provided slice of IDs.
func (s *SQLiteStore) GetByIDs(ctx context.Context, ids []string) ([]Subscription, error) {
	if len(ids) == 0 {
		return []Subscription{}, nil
	}

	// Filter out empty or whitespace IDs
	validIDs := make([]string, 0, len(ids))
	for _, id := range ids {
		trimmed := strings.TrimSpace(id)
		if trimmed != "" {
			validIDs = append(validIDs, trimmed)
		}
	}
	if len(validIDs) == 0 {
		return []Subscription{}, nil
	}

	placeholders := make([]string, len(validIDs))
	args := make([]interface{}, len(validIDs))
	for i, id := range validIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf(`
		SELECT id, endpoint, p256dh, auth, created_at, updated_at
		FROM web_push_subscriptions
		WHERE id IN (%s)
		ORDER BY id ASC
	`, strings.Join(placeholders, ","))

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query subscriptions by ids: %w", err)
	}
	defer rows.Close()

	var list []Subscription
	for rows.Next() {
		var sub Subscription
		if err := rows.Scan(&sub.ID, &sub.Endpoint, &sub.P256dh, &sub.Auth, &sub.CreatedAt, &sub.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan subscription: %w", err)
		}
		list = append(list, sub)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}

	return list, nil
}

// Delete removes a subscription by ID.
func (s *SQLiteStore) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM web_push_subscriptions WHERE id = ?`
	_, err := s.db.ExecContext(ctx, query, strings.TrimSpace(id))
	if err != nil {
		return fmt.Errorf("delete web push subscription by id (%s): %w", id, err)
	}
	return nil
}

// DeleteByEndpoint removes a subscription by its endpoint URL.
func (s *SQLiteStore) DeleteByEndpoint(ctx context.Context, endpoint string) error {
	query := `DELETE FROM web_push_subscriptions WHERE endpoint = ?`
	_, err := s.db.ExecContext(ctx, query, strings.TrimSpace(endpoint))
	if err != nil {
		return fmt.Errorf("delete web push subscription by endpoint: %w", err)
	}
	return nil
}
