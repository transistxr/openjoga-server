package store

import (
	"context"
	"database/sql"
	"errors"
)

const (
	EventDraft     = "draft"
	EventScheduled = "scheduled"
	EventConfirmed = "confirmed"
	EventCancelled = "cancelled"
	EventCompleted = "completed"
)

const (
	CostFree  = "free"
	CostFixed = "fixed"
	CostSplit = "split"
)

const (
	PolicyOpen        = "open"
	PolicyContactable = "contactable"
	PolicyMembersOnly = "members_only"
)

type Event struct {
	ID      int64
	GroupID int64
	Slug    string
	Title   *string
	Status  string

	StartsAt     string
	EndsAt       string
	Timezone     string
	Location     *string
	MinPlayers   *int
	MaxPlayers   *int
	RSVPCutoffAt *string

	RSVPPolicy string

	CostType string
	CostPerHeadPence *int64
	CostTotalPence *int64

	ShowPaymentPreGame bool

	CancelledReason *string
	CreatedBy       *int64
	CreatedAt       string
}

type Capacity struct {
	Going  int
	Guests int
	Max    *int
}

func (c Capacity) Total() int { return c.Going + c.Guests }

func (c Capacity) SpotsLeft() *int {
	if c.Max == nil {
		return nil
	}
	left := *c.Max - c.Total()
	if left < 0 {
		left = 0
	}
	return &left
}

func (c Capacity) Full() bool {
	return c.Max != nil && c.Total() >= *c.Max
}

const eventColumns = `id, group_id, slug, title, status, starts_at, ends_at, timezone,
	location, min_players, max_players, rsvp_cutoff_at, rsvp_policy,
	cost_type, cost_per_head_pence, cost_total_pence, show_payment_pre_game,
	cancelled_reason, created_by, created_at`

func scanEvent(row scanner) (Event, error) {
	var e Event
	err := row.Scan(&e.ID, &e.GroupID, &e.Slug, &e.Title, &e.Status,
		&e.StartsAt, &e.EndsAt, &e.Timezone, &e.Location,
		&e.MinPlayers, &e.MaxPlayers, &e.RSVPCutoffAt, &e.RSVPPolicy,
		&e.CostType, &e.CostPerHeadPence, &e.CostTotalPence, &e.ShowPaymentPreGame,
		&e.CancelledReason, &e.CreatedBy, &e.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return e, ErrNotFound
	}
	return e, err
}

func (s *Store) CreateEvent(ctx context.Context, e Event) (Event, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO events (group_id, slug, title, status, starts_at, ends_at, timezone,
			location, min_players, max_players, rsvp_cutoff_at, rsvp_policy,
			cost_type, cost_per_head_pence, cost_total_pence, show_payment_pre_game,
			created_by, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		e.GroupID, e.Slug, e.Title, e.Status, e.StartsAt, e.EndsAt, e.Timezone,
		e.Location, e.MinPlayers, e.MaxPlayers, e.RSVPCutoffAt, e.RSVPPolicy,
		e.CostType, e.CostPerHeadPence, e.CostTotalPence, e.ShowPaymentPreGame,
		e.CreatedBy, Now(), Now())
	if err != nil {
		return Event{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Event{}, err
	}
	return s.GetEvent(ctx, id)
}

func (s *Store) GetEvent(ctx context.Context, id int64) (Event, error) {
	return scanEvent(s.db.QueryRowContext(ctx,
		`SELECT `+eventColumns+` FROM events WHERE id = ?`, id))
}

func (s *Store) GetEventBySlug(ctx context.Context, slug string) (Event, error) {
	return scanEvent(s.db.QueryRowContext(ctx,
		`SELECT `+eventColumns+` FROM events WHERE slug = ?`, slug))
}

func (s *Store) ListEvents(ctx context.Context, groupID int64, upcomingOnly bool) ([]Event, error) {
	query := `SELECT ` + eventColumns + ` FROM events WHERE group_id = ?`
	args := []any{groupID}
	if upcomingOnly {
		query += ` AND ends_at >= ?`
		args = append(args, Now())
	}
	query += ` ORDER BY starts_at LIMIT ` + itoa(MaxListRows)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

func (s *Store) UpdateEvent(ctx context.Context, e Event) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE events SET title = ?, starts_at = ?, ends_at = ?, timezone = ?,
			location = ?, min_players = ?, max_players = ?, rsvp_cutoff_at = ?,
			rsvp_policy = ?, cost_type = ?, cost_per_head_pence = ?,
			cost_total_pence = ?, show_payment_pre_game = ?, updated_at = ?
		 WHERE id = ?`,
		e.Title, e.StartsAt, e.EndsAt, e.Timezone, e.Location,
		e.MinPlayers, e.MaxPlayers, e.RSVPCutoffAt, e.RSVPPolicy,
		e.CostType, e.CostPerHeadPence, e.CostTotalPence, e.ShowPaymentPreGame,
		Now(), e.ID)
	return err
}

func (s *Store) SetEventStatus(ctx context.Context, id int64, status string, reason *string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE events SET status = ?, cancelled_reason = ?, updated_at = ? WHERE id = ?`,
		status, reason, Now(), id)
	return err
}

func (s *Store) SetResolvedPerHead(ctx context.Context, id int64, perHeadPence int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE events SET cost_per_head_pence = ?, updated_at = ? WHERE id = ?`,
		perHeadPence, Now(), id)
	return err
}

func (s *Store) Capacity(ctx context.Context, eventID int64) (Capacity, error) {
	var c Capacity
	err := s.db.QueryRowContext(ctx, `
		SELECT
		  (SELECT COUNT(*) FROM rsvps
		    WHERE event_id = ? AND status = 'going'),
		  (SELECT COUNT(*) FROM guests g
		     JOIN rsvps r ON r.id = g.rsvp_id
		    WHERE r.event_id = ? AND r.status = 'going'),
		  (SELECT max_players FROM events WHERE id = ?)`,
		eventID, eventID, eventID).Scan(&c.Going, &c.Guests, &c.Max)
	return c, err
}

func (s *Store) EventsDueForCutoff(ctx context.Context) ([]Event, error) {
	return s.eventQuery(ctx,
		`SELECT `+eventColumns+` FROM events
		  WHERE status = ? AND rsvp_cutoff_at IS NOT NULL AND rsvp_cutoff_at <= ?`,
		EventScheduled, Now())
}

func (s *Store) EventsDueForCompletion(ctx context.Context) ([]Event, error) {
	return s.eventQuery(ctx,
		`SELECT `+eventColumns+` FROM events
		  WHERE status IN (?, ?) AND ends_at <= ?`,
		EventScheduled, EventConfirmed, Now())
}

func (s *Store) eventQuery(ctx context.Context, query string, args ...any) ([]Event, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// ListEventsForUser returns everything on somebody's horizon.
func (s *Store) ListEventsForUser(ctx context.Context, userID int64, includePast bool) ([]Event, error) {
	query := `
		SELECT e.id, e.group_id, e.slug, e.title, e.status, e.starts_at, e.ends_at,
		       e.timezone, e.location, e.min_players, e.max_players, e.rsvp_cutoff_at,
		       e.rsvp_policy, e.cost_type, e.cost_per_head_pence, e.cost_total_pence,
		       e.show_payment_pre_game, e.cancelled_reason, e.created_by, e.created_at
		  FROM events e
		 WHERE (EXISTS (SELECT 1 FROM rsvps r
		                 WHERE r.event_id = e.id AND r.user_id = ?)
		    OR  EXISTS (SELECT 1 FROM group_members m
		                 WHERE m.group_id = e.group_id
		                   AND m.user_id = ? AND m.status = ?))`
	args := []any{userID, userID, MemberActive}

	if !includePast {
		query += ` AND e.ends_at >= ?`
		args = append(args, Now())
	}
	query += ` ORDER BY e.starts_at LIMIT ` + itoa(MaxListRows)

	return s.eventQuery(ctx, query, args...)
}
