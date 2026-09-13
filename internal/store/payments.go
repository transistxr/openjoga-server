package store

import (
	"context"
	"database/sql"
	"errors"
)

const (
	PaymentOwed   = "owed"
	PaymentPaid   = "paid"
	PaymentWaived = "waived"
)

const (
	SponsorshipPending = "pending"
	SponsorshipActive  = "active"
	SponsorshipRevoked = "revoked"
)

const (
	KittyPayment    = "payment"
	KittyRefund     = "refund"
	KittyAdjustment = "adjustment"
	KittySurplus    = "surplus"
)

// Tracking only.
type Payment struct {
	ID             int64
	EventID        int64
	UserID         int64
	AmountDuePence int64
	Status         string

	SponsorshipID *int64

	MarkedPaidAt *string
	MarkedBy     *int64
	Reference *string

	UserName string
}

func (p Payment) Covered() bool { return p.SponsorshipID != nil }

type Sponsorship struct {
	ID            int64
	EventID       int64
	SponsorUserID int64
	Slots         int
	Status        string
	Note          *string
	ApprovedBy    *int64
	ApprovedAt    *string
	CreatedAt     string

	SponsorName string
	Claimed     int
}

func (s Sponsorship) Remaining() int {
	left := s.Slots - s.Claimed
	if left < 0 {
		return 0
	}
	return left
}

type KittyEntry struct {
	ID          int64
	GroupID     int64
	Type        string
	AmountPence int64
	UserID      *int64
	EventID     *int64
	Note        *string
	CreatedBy   *int64
	CreatedAt   string
}

type Debt struct {
	UserID     int64
	UserName   string
	Games      int
	TotalPence int64
	Reachable  bool
}

// --- Payments -------------------------------------------------------------

func (s *Store) UpsertPayment(ctx context.Context, p Payment) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO payments (event_id, user_id, amount_due_pence, status, sponsorship_id)
		VALUES (?,?,?,?,?)
		ON CONFLICT (event_id, user_id) DO UPDATE SET
			amount_due_pence = excluded.amount_due_pence,
			sponsorship_id   = excluded.sponsorship_id`,
		p.EventID, p.UserID, p.AmountDuePence, PaymentOwed, p.SponsorshipID)
	return err
}

func (s *Store) GetPayment(ctx context.Context, id int64) (Payment, error) {
	var p Payment
	err := s.db.QueryRowContext(ctx, `
		SELECT p.id, p.event_id, p.user_id, p.amount_due_pence, p.status,
		       p.sponsorship_id, p.marked_paid_at, p.marked_by, p.reference, u.display_name
		  FROM payments p JOIN users u ON u.id = p.user_id
		 WHERE p.id = ?`, id,
	).Scan(&p.ID, &p.EventID, &p.UserID, &p.AmountDuePence, &p.Status,
		&p.SponsorshipID, &p.MarkedPaidAt, &p.MarkedBy, &p.Reference, &p.UserName)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

func (s *Store) ListPayments(ctx context.Context, eventID int64) ([]Payment, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.id, p.event_id, p.user_id, p.amount_due_pence, p.status,
		       p.sponsorship_id, p.marked_paid_at, p.marked_by, p.reference, u.display_name
		  FROM payments p JOIN users u ON u.id = p.user_id
		 WHERE p.event_id = ? ORDER BY u.display_name`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var payments []Payment
	for rows.Next() {
		var p Payment
		if err := rows.Scan(&p.ID, &p.EventID, &p.UserID, &p.AmountDuePence, &p.Status,
			&p.SponsorshipID, &p.MarkedPaidAt, &p.MarkedBy, &p.Reference,
			&p.UserName); err != nil {
			return nil, err
		}
		payments = append(payments, p)
	}
	return payments, rows.Err()
}

func (s *Store) SetPaymentStatus(ctx context.Context, id int64, status string, by int64, reference *string) error {
	var paidAt *string
	if status == PaymentPaid {
		now := Now()
		paidAt = &now
	} else {
		reference = nil
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE payments SET status = ?, marked_paid_at = ?, marked_by = ?, reference = ?
		  WHERE id = ?`,
		status, paidAt, by, reference, id)
	return err
}

func (s *Store) GetPaymentForUser(ctx context.Context, eventID, userID int64) (Payment, error) {
	var p Payment
	err := s.db.QueryRowContext(ctx, `
		SELECT p.id, p.event_id, p.user_id, p.amount_due_pence, p.status,
		       p.sponsorship_id, p.marked_paid_at, p.marked_by, p.reference, u.display_name
		  FROM payments p JOIN users u ON u.id = p.user_id
		 WHERE p.event_id = ? AND p.user_id = ?`, eventID, userID,
	).Scan(&p.ID, &p.EventID, &p.UserID, &p.AmountDuePence, &p.Status,
		&p.SponsorshipID, &p.MarkedPaidAt, &p.MarkedBy, &p.Reference, &p.UserName)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

func (s *Store) ListDebts(ctx context.Context, groupID *int64) ([]Debt, error) {
	query := `
		SELECT p.user_id, u.display_name, COUNT(*), SUM(p.amount_due_pence)
		  FROM payments p
		  JOIN users  u ON u.id = p.user_id
		  JOIN events e ON e.id = p.event_id
		 WHERE p.status = ? AND p.amount_due_pence > 0`
	args := []any{PaymentOwed}
	if groupID != nil {
		query += ` AND e.group_id = ?`
		args = append(args, *groupID)
	}
	query += ` GROUP BY p.user_id, u.display_name ORDER BY SUM(p.amount_due_pence) DESC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var debts []Debt
	for rows.Next() {
		var d Debt
		if err := rows.Scan(&d.UserID, &d.UserName, &d.Games, &d.TotalPence); err != nil {
			return nil, err
		}
		debts = append(debts, d)
	}
	return debts, rows.Err()
}

func (s *Store) CountUnpaidGames(ctx context.Context, userID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM payments
		  WHERE user_id = ? AND status = ? AND amount_due_pence > 0`,
		userID, PaymentOwed).Scan(&n)
	return n, err
}

// --- Sponsorships ---------------------------------------------------------

const sponsorshipColumns = `s.id, s.event_id, s.sponsor_user_id, s.slots, s.status,
	s.note, s.approved_by, s.approved_at, s.created_at, u.display_name,
	(SELECT COUNT(*) FROM rsvps r
	  WHERE r.sponsorship_id = s.id AND r.status = 'going')`

func scanSponsorship(row scanner) (Sponsorship, error) {
	var sp Sponsorship
	err := row.Scan(&sp.ID, &sp.EventID, &sp.SponsorUserID, &sp.Slots, &sp.Status,
		&sp.Note, &sp.ApprovedBy, &sp.ApprovedAt, &sp.CreatedAt, &sp.SponsorName, &sp.Claimed)
	if errors.Is(err, sql.ErrNoRows) {
		return sp, ErrNotFound
	}
	return sp, err
}

func (s *Store) CreateSponsorship(ctx context.Context, sp Sponsorship) (Sponsorship, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO sponsorships (event_id, sponsor_user_id, slots, status, note,
			approved_by, approved_at, created_at)
		VALUES (?,?,?,?,?,?,?,?)`,
		sp.EventID, sp.SponsorUserID, sp.Slots, sp.Status, sp.Note,
		sp.ApprovedBy, sp.ApprovedAt, Now())
	if err != nil {
		return Sponsorship{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Sponsorship{}, err
	}
	return s.GetSponsorship(ctx, id)
}

func (s *Store) GetSponsorship(ctx context.Context, id int64) (Sponsorship, error) {
	return scanSponsorship(s.db.QueryRowContext(ctx,
		`SELECT `+sponsorshipColumns+` FROM sponsorships s
		   JOIN users u ON u.id = s.sponsor_user_id
		  WHERE s.id = ?`, id))
}

func (s *Store) ListSponsorships(ctx context.Context, eventID int64, status string) ([]Sponsorship, error) {
	query := `SELECT ` + sponsorshipColumns + ` FROM sponsorships s
	            JOIN users u ON u.id = s.sponsor_user_id
	           WHERE s.event_id = ?`
	args := []any{eventID}
	if status != "" {
		query += ` AND s.status = ?`
		args = append(args, status)
	}
	query += ` ORDER BY s.created_at`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sponsorships []Sponsorship
	for rows.Next() {
		sp, err := scanSponsorship(rows)
		if err != nil {
			return nil, err
		}
		sponsorships = append(sponsorships, sp)
	}
	return sponsorships, rows.Err()
}

func (s *Store) NextAvailableSponsorship(ctx context.Context, eventID int64) (Sponsorship, error) {
	return scanSponsorship(s.db.QueryRowContext(ctx,
		`SELECT `+sponsorshipColumns+` FROM sponsorships s
		   JOIN users u ON u.id = s.sponsor_user_id
		  WHERE s.event_id = ? AND s.status = ?
		    AND s.slots > (SELECT COUNT(*) FROM rsvps r
		                    WHERE r.sponsorship_id = s.id AND r.status = 'going')
		  ORDER BY s.created_at LIMIT 1`, eventID, SponsorshipActive))
}

// SetSponsorshipStatus approves or revokes an offer.
func (s *Store) SetSponsorshipStatus(ctx context.Context, id int64, status string, by int64) error {
	var approvedBy *int64
	var approvedAt *string
	if status == SponsorshipActive {
		now := Now()
		approvedBy, approvedAt = &by, &now
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE sponsorships SET status = ?, approved_by = ?, approved_at = ? WHERE id = ?`,
		status, approvedBy, approvedAt, id)
	return err
}

func (s *Store) CountSponsoredSlots(ctx context.Context, eventID, userID int64) (int, error) {
	var n sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT SUM(slots) FROM sponsorships
		  WHERE event_id = ? AND sponsor_user_id = ? AND status <> ?`,
		eventID, userID, SponsorshipRevoked).Scan(&n)
	return int(n.Int64), err
}

// --- Kitty ----------------------------------------------------------------

func (s *Store) AddKittyEntry(ctx context.Context, e KittyEntry) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO kitty_entries (group_id, type, amount_pence, user_id, event_id,
			note, created_by, created_at)
		VALUES (?,?,?,?,?,?,?,?)`,
		e.GroupID, e.Type, e.AmountPence, e.UserID, e.EventID, e.Note, e.CreatedBy, Now())
	return err
}

func (s *Store) ListKittyEntries(ctx context.Context, groupID int64) ([]KittyEntry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, group_id, type, amount_pence, user_id, event_id, note,
		       created_by, created_at
		  FROM kitty_entries WHERE group_id = ? ORDER BY created_at DESC
		 LIMIT `+itoa(MaxListRows), groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []KittyEntry
	for rows.Next() {
		var e KittyEntry
		if err := rows.Scan(&e.ID, &e.GroupID, &e.Type, &e.AmountPence, &e.UserID,
			&e.EventID, &e.Note, &e.CreatedBy, &e.CreatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func (s *Store) KittyBalance(ctx context.Context, groupID int64) (int64, error) {
	var balance sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT SUM(amount_pence) FROM kitty_entries WHERE group_id = ?`,
		groupID).Scan(&balance)
	return balance.Int64, err
}
