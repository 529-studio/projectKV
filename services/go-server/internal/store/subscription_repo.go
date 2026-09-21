package store

import (
	"context"
	"database/sql"
	"time"
)

type Subscription struct {
	ChannelName string
	RecipientID string
	Zone        string
}

type SubscriptionRepo struct {
	db                 *sql.DB
	dedupeCooldownMins int
}

func NewSubscriptionRepo(db *sql.DB, cooldownMins int) *SubscriptionRepo {
	return &SubscriptionRepo{db: db, dedupeCooldownMins: cooldownMins}
}

func (r *SubscriptionRepo) SubscribersForZone(ctx context.Context, zone string) ([]Subscription, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT channel_name, recipient_id, zone FROM subscriptions WHERE zone = ?", zone)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var subs []Subscription
	for rows.Next() {
		var s Subscription
		if err := rows.Scan(&s.ChannelName, &s.RecipientID, &s.Zone); err != nil {
			return nil, err
		}
		subs = append(subs, s)
	}
	return subs, rows.Err()
}

func (r *SubscriptionRepo) HasRecentAlert(ctx context.Context, sub Subscription, severity string) (bool, error) {
	cutoff := time.Now().Add(-time.Duration(r.dedupeCooldownMins) * time.Minute)
	var count int
	err := r.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM alert_history WHERE channel_name = ? AND recipient_id = ? AND zone = ? AND severity = ? AND sent_at >= ?",
		sub.ChannelName, sub.RecipientID, sub.Zone, severity, cutoff,
	).Scan(&count)
	return count > 0, err
}

func (r *SubscriptionRepo) RecordAlert(ctx context.Context, sub Subscription, severity string) error {
	_, err := r.db.ExecContext(ctx,
		"INSERT INTO alert_history (channel_name, recipient_id, zone, severity, sent_at) VALUES (?, ?, ?, ?, ?)",
		sub.ChannelName, sub.RecipientID, sub.Zone, severity, time.Now(),
	)
	return err
}
