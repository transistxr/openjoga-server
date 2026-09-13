package store

import (
	"context"
	"database/sql"
	"errors"
)

const (
	ChannelInApp   = "inapp"
	ChannelEmail   = "email"
	ChannelWebPush = "webpush"
)

const (
	NotifyGameCreated = "game_created"
	NotifyReminder    = "reminder"
	NotifyConfirmed   = "confirmed"
	NotifyCancelled   = "cancelled"
	NotifyPromoted    = "promoted"
	NotifyWaitlisted  = "waitlisted"
	NotifySponsorship = "sponsorship_pending"
	NotifyJoinRequest = "join_request"
)

type Notification struct {
	ID        int64
	UserID    int64
	Type      string
	EventID   *int64
	Title     string
	Body      *string
	ReadAt    *string
	CreatedAt string
}

type PushSubscription struct {
	ID        int64
	UserID    int64
	Endpoint  string
	P256dh    string
	Auth      string
	CreatedAt string
}

type Preference struct {
	Channel   string
	EventType string
	Enabled   bool
}

func (s *Store) CreateNotification(ctx context.Context, n Notification) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO notifications (user_id, type, event_id, title, body, created_at)
		 VALUES (?,?,?,?,?,?)`,
		n.UserID, n.Type, n.EventID, n.Title, n.Body, Now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) ListNotifications(ctx context.Context, userID int64, limit int) ([]Notification, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, user_id, type, event_id, title, body, read_at, created_at
		   FROM notifications WHERE user_id = ?
		  ORDER BY created_at DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Notification
	for rows.Next() {
		var n Notification
		if err := rows.Scan(&n.ID, &n.UserID, &n.Type, &n.EventID, &n.Title,
			&n.Body, &n.ReadAt, &n.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s *Store) MarkNotificationsRead(ctx context.Context, userID int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE notifications SET read_at = ? WHERE user_id = ? AND read_at IS NULL`,
		Now(), userID)
	return err
}

// --- Preferences ----------------------------------------------------------

func (s *Store) SetPreference(ctx context.Context, userID int64, p Preference) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO notification_preferences (user_id, channel, event_type, enabled)
		VALUES (?,?,?,?)
		ON CONFLICT (user_id, channel, event_type) DO UPDATE SET enabled = excluded.enabled`,
		userID, p.Channel, p.EventType, p.Enabled)
	return err
}

func (s *Store) ListPreferences(ctx context.Context, userID int64) ([]Preference, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT channel, event_type, enabled FROM notification_preferences WHERE user_id = ?`,
		userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var prefs []Preference
	for rows.Next() {
		var p Preference
		if err := rows.Scan(&p.Channel, &p.EventType, &p.Enabled); err != nil {
			return nil, err
		}
		prefs = append(prefs, p)
	}
	return prefs, rows.Err()
}

func (s *Store) ChannelEnabled(ctx context.Context, userID int64, channel, eventType string) (bool, error) {
	var enabled bool
	err := s.db.QueryRowContext(ctx,
		`SELECT enabled FROM notification_preferences
		  WHERE user_id = ? AND channel = ? AND event_type IN (?, 'all')
		  ORDER BY CASE event_type WHEN 'all' THEN 1 ELSE 0 END LIMIT 1`,
		userID, channel, eventType).Scan(&enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	return enabled, err
}

// --- Push subscriptions ---------------------------------------------------

func (s *Store) AddPushSubscription(ctx context.Context, p PushSubscription) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO push_subscriptions (user_id, endpoint, p256dh, auth, created_at)
		VALUES (?,?,?,?,?)
		ON CONFLICT (endpoint) DO UPDATE SET
			user_id = excluded.user_id,
			p256dh  = excluded.p256dh,
			auth    = excluded.auth`,
		p.UserID, p.Endpoint, p.P256dh, p.Auth, Now())
	return err
}

func (s *Store) ListPushSubscriptions(ctx context.Context, userID int64) ([]PushSubscription, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, user_id, endpoint, p256dh, auth, created_at
		   FROM push_subscriptions WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var subs []PushSubscription
	for rows.Next() {
		var p PushSubscription
		if err := rows.Scan(&p.ID, &p.UserID, &p.Endpoint, &p.P256dh, &p.Auth, &p.CreatedAt); err != nil {
			return nil, err
		}
		subs = append(subs, p)
	}
	return subs, rows.Err()
}

func (s *Store) DeletePushSubscription(ctx context.Context, endpoint string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM push_subscriptions WHERE endpoint = ?`, endpoint)
	return err
}
