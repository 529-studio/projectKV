package subscription

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Store defines the persistent interface for flood alert subscriptions.
type Store interface {
	Subscribe(ctx context.Context, sub Subscription) error
	Unsubscribe(ctx context.Context, channelName, chatID string, targetType TargetType, targetID string) error
	GetSubscribersForChokepoint(ctx context.Context, channelName, chokepointID, zone, ward string) ([]string, error)
	ListSubscriptions(ctx context.Context, channelName, chatID string) ([]Subscription, error)
}

// SQLiteStore implements Store using hand-written SQL on modernc.org/sqlite (no ORM per Rule 7).
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore creates a new subscription store backed by SQLite.
func NewSQLiteStore(db *sql.DB) *SQLiteStore {
	return &SQLiteStore{db: db}
}

// Subscribe persists a new subscription. It is idempotent (no-op if duplicate exists).
func (s *SQLiteStore) Subscribe(ctx context.Context, sub Subscription) error {
	if err := sub.IsValid(); err != nil {
		return err
	}

	query := `
		INSERT INTO flood_subscriptions (channel_name, chat_id, target_type, target_id, created_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(channel_name, chat_id, target_type, target_id) DO NOTHING
	`

	createdAt := sub.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now()
	}

	_, err := s.db.ExecContext(ctx, query,
		strings.TrimSpace(sub.ChannelName),
		strings.TrimSpace(sub.ChatID),
		string(sub.TargetType),
		strings.TrimSpace(sub.TargetID),
		createdAt,
	)
	if err != nil {
		return fmt.Errorf("subscribe (%s, %s, %s): %w", sub.ChannelName, sub.ChatID, sub.TargetType, err)
	}

	return nil
}

// Unsubscribe removes an existing subscription.
func (s *SQLiteStore) Unsubscribe(ctx context.Context, channelName, chatID string, targetType TargetType, targetID string) error {
	query := `
		DELETE FROM flood_subscriptions
		WHERE channel_name = ? AND chat_id = ? AND target_type = ? AND target_id = ?
	`
	_, err := s.db.ExecContext(ctx, query,
		strings.TrimSpace(channelName),
		strings.TrimSpace(chatID),
		string(targetType),
		strings.TrimSpace(targetID),
	)
	if err != nil {
		return fmt.Errorf("unsubscribe: %w", err)
	}
	return nil
}

// GetSubscribersForChokepoint queries all distinct chat IDs subscribed to a given flood event.
// Matches subscriptions with target_type = 'all', or matching specific chokepointID, zone, or ward.
func (s *SQLiteStore) GetSubscribersForChokepoint(ctx context.Context, channelName, chokepointID, zone, ward string) ([]string, error) {
	chokepointID = strings.TrimSpace(chokepointID)
	zone = strings.TrimSpace(zone)
	ward = strings.TrimSpace(ward)

	query := `
		SELECT DISTINCT chat_id FROM flood_subscriptions
		WHERE channel_name = ? AND (
			(target_type = 'all') OR
			(target_type = 'chokepoint' AND target_id = ?) OR
			(target_type = 'zone' AND (? != '' AND LOWER(target_id) = LOWER(?))) OR
			(target_type = 'ward' AND (? != '' AND LOWER(target_id) = LOWER(?)))
		)
		ORDER BY chat_id ASC
	`

	rows, err := s.db.QueryContext(ctx, query,
		channelName,
		chokepointID,
		zone, zone,
		ward, ward,
	)
	if err != nil {
		return nil, fmt.Errorf("query subscribers for chokepoint (%s): %w", chokepointID, err)
	}
	defer rows.Close()

	var recipients []string
	for rows.Next() {
		var chatID string
		if err := rows.Scan(&chatID); err != nil {
			return nil, fmt.Errorf("scan subscriber chat_id: %w", err)
		}
		recipients = append(recipients, chatID)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	return recipients, nil
}

// ListSubscriptions returns all active subscriptions for a given channel and chat ID.
func (s *SQLiteStore) ListSubscriptions(ctx context.Context, channelName, chatID string) ([]Subscription, error) {
	query := `
		SELECT id, channel_name, chat_id, target_type, target_id, created_at
		FROM flood_subscriptions
		WHERE channel_name = ? AND chat_id = ?
		ORDER BY id ASC
	`

	rows, err := s.db.QueryContext(ctx, query, strings.TrimSpace(channelName), strings.TrimSpace(chatID))
	if err != nil {
		return nil, fmt.Errorf("list subscriptions: %w", err)
	}
	defer rows.Close()

	var list []Subscription
	for rows.Next() {
		var sub Subscription
		var targetTypeStr string
		if err := rows.Scan(&sub.ID, &sub.ChannelName, &sub.ChatID, &targetTypeStr, &sub.TargetID, &sub.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan subscription: %w", err)
		}
		sub.TargetType = TargetType(targetTypeStr)
		list = append(list, sub)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	return list, nil
}
