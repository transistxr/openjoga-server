package store

import (
	"context"
	"database/sql"
	"errors"
)

const (
	RSVPGoing      = "going"
	RSVPMaybe      = "maybe"
	RSVPOut        = "out"
	RSVPWaitlisted = "waitlisted"
)

type RSVP struct {
	ID      int64
	EventID int64
	UserID  int64
	Status  string

	DisplayName string
	Pronouns    *string

	WaitlistedAt *string
	Attended *bool

	SponsorshipID *int64

	RespondedAt string

	Guests []Guest
}

func (r RSVP) Slots() int { return 1 + len(r.Guests) }

type Guest struct {
	ID        int64
	RSVPID    int64
	Name      *string
	Attended  *bool
	CreatedAt string
}

const rsvpColumns = `r.id, r.event_id, r.user_id, r.status, u.display_name, u.pronouns,
	r.waitlisted_at, r.attended, r.sponsorship_id, r.responded_at`

const rsvpFrom = ` FROM rsvps r JOIN users u ON u.id = r.user_id`

func scanRSVP(row scanner) (RSVP, error) {
	var r RSVP
	err := row.Scan(&r.ID, &r.EventID, &r.UserID, &r.Status, &r.DisplayName, &r.Pronouns,
		&r.WaitlistedAt, &r.Attended, &r.SponsorshipID, &r.RespondedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, err
}

func (s *Store) UpsertRSVP(ctx context.Context, r RSVP) (RSVP, error) {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO rsvps (event_id, user_id, status, waitlisted_at,
			sponsorship_id, responded_at)
		VALUES (?,?,?,?,?,?)
		ON CONFLICT (event_id, user_id) DO UPDATE SET
			status         = excluded.status,
			waitlisted_at  = excluded.waitlisted_at,
			sponsorship_id = excluded.sponsorship_id,
			responded_at   = excluded.responded_at`,
		r.EventID, r.UserID, r.Status, r.WaitlistedAt,
		r.SponsorshipID, Now())
	if err != nil {
		return RSVP{}, err
	}
	return s.GetRSVP(ctx, r.EventID, r.UserID)
}

func (s *Store) GetRSVP(ctx context.Context, eventID, userID int64) (RSVP, error) {
	return scanRSVP(s.db.QueryRowContext(ctx,
		`SELECT `+rsvpColumns+rsvpFrom+` WHERE r.event_id = ? AND r.user_id = ?`,
		eventID, userID))
}

func (s *Store) GetRSVPByID(ctx context.Context, id int64) (RSVP, error) {
	return scanRSVP(s.db.QueryRowContext(ctx,
		`SELECT `+rsvpColumns+rsvpFrom+` WHERE r.id = ?`, id))
}

func (s *Store) ListRSVPs(ctx context.Context, eventID int64, statuses ...string) ([]RSVP, error) {
	query := `SELECT ` + rsvpColumns + rsvpFrom + ` WHERE r.event_id = ?`
	args := []any{eventID}
	if len(statuses) > 0 {
		query += ` AND r.status IN (` + placeholders(len(statuses)) + `)`
		for _, st := range statuses {
			args = append(args, st)
		}
	}
	query += ` ORDER BY COALESCE(r.waitlisted_at, r.responded_at)`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rsvps []RSVP
	byID := map[int64]int{}
	for rows.Next() {
		r, err := scanRSVP(rows)
		if err != nil {
			return nil, err
		}
		byID[r.ID] = len(rsvps)
		rsvps = append(rsvps, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(rsvps) == 0 {
		return rsvps, nil
	}

	guestRows, err := s.db.QueryContext(ctx, `
		SELECT g.id, g.rsvp_id, g.name, g.attended, g.created_at
		  FROM guests g JOIN rsvps r ON r.id = g.rsvp_id
		 WHERE r.event_id = ? ORDER BY g.created_at`, eventID)
	if err != nil {
		return nil, err
	}
	defer guestRows.Close()

	for guestRows.Next() {
		var g Guest
		if err := guestRows.Scan(&g.ID, &g.RSVPID, &g.Name, &g.Attended, &g.CreatedAt); err != nil {
			return nil, err
		}
		if i, ok := byID[g.RSVPID]; ok {
			rsvps[i].Guests = append(rsvps[i].Guests, g)
		}
	}
	return rsvps, guestRows.Err()
}

func (s *Store) NextWaitlisted(ctx context.Context, eventID int64) (RSVP, error) {
	return scanRSVP(s.db.QueryRowContext(ctx,
		`SELECT `+rsvpColumns+rsvpFrom+`
		  WHERE r.event_id = ? AND r.status = ?
		  ORDER BY r.waitlisted_at LIMIT 1`, eventID, RSVPWaitlisted))
}

func (s *Store) SetRSVPStatus(ctx context.Context, id int64, status string, waitlistedAt *string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE rsvps SET status = ?, waitlisted_at = ?, responded_at = ? WHERE id = ?`,
		status, waitlistedAt, Now(), id)
	return err
}

func (s *Store) DeleteRSVP(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM rsvps WHERE id = ?`, id)
	return err
}

func (s *Store) CountRSVPsForUser(ctx context.Context, userID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM rsvps WHERE user_id = ?`, userID).Scan(&n)
	return n, err
}

func (s *Store) MarkAttendance(ctx context.Context, rsvpID int64, attended bool) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE rsvps SET attended = ? WHERE id = ?`, attended, rsvpID)
	return err
}

// --- Guests ---------------------------------------------------------------

func (s *Store) AddGuest(ctx context.Context, rsvpID int64, name *string) (Guest, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO guests (rsvp_id, name, created_at) VALUES (?,?,?)`,
		rsvpID, name, Now())
	if err != nil {
		return Guest{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Guest{}, err
	}
	var g Guest
	err = s.db.QueryRowContext(ctx,
		`SELECT id, rsvp_id, name, attended, created_at FROM guests WHERE id = ?`, id,
	).Scan(&g.ID, &g.RSVPID, &g.Name, &g.Attended, &g.CreatedAt)
	return g, err
}

func (s *Store) CountGuests(ctx context.Context, rsvpID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM guests WHERE rsvp_id = ?`, rsvpID).Scan(&n)
	return n, err
}

func (s *Store) GetGuest(ctx context.Context, id int64) (Guest, error) {
	var g Guest
	err := s.db.QueryRowContext(ctx,
		`SELECT id, rsvp_id, name, attended, created_at FROM guests WHERE id = ?`, id,
	).Scan(&g.ID, &g.RSVPID, &g.Name, &g.Attended, &g.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return g, ErrNotFound
	}
	return g, err
}

func (s *Store) DeleteGuest(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM guests WHERE id = ?`, id)
	return err
}

func (s *Store) MarkGuestAttendance(ctx context.Context, guestID int64, attended bool) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE guests SET attended = ? WHERE id = ?`, attended, guestID)
	return err
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	out := make([]byte, 0, n*2-1)
	for i := range n {
		if i > 0 {
			out = append(out, ',')
		}
		out = append(out, '?')
	}
	return string(out)
}
