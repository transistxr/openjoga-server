package store

import (
	"context"
	"database/sql"
	"errors"
)

const (
	InviteRotating = "rotating"
	InviteDurable = "durable"
)

const (
	ScopeCommunity = "community"
	ScopeGroup     = "group"
)

type Invite struct {
	ID        int64
	TokenHash string
	Kind      string
	Scope     string
	GroupID   *int64
	Role      string

	MaxUses *int
	Uses    int

	RequiresApproval bool
	AttestsContact   bool

	Label     *string
	ExpiresAt *string
	RevokedAt *string
	CreatedBy *int64
	CreatedAt string
}

func (i Invite) Usable() bool {
	if i.RevokedAt != nil {
		return false
	}
	if i.ExpiresAt != nil && *i.ExpiresAt <= Now() {
		return false
	}
	if i.MaxUses != nil && i.Uses >= *i.MaxUses {
		return false
	}
	return true
}

type ClaimLink struct {
	ID         int64
	UserID     int64
	TokenHash  string
	SingleUse  bool
	ExpiresAt  *string
	ConsumedAt *string
	CreatedBy  *int64
	CreatedAt  string
}

func (c ClaimLink) Usable() bool {
	if c.SingleUse && c.ConsumedAt != nil {
		return false
	}
	if c.ExpiresAt != nil && *c.ExpiresAt <= Now() {
		return false
	}
	return true
}

// --- Invites --------------------------------------------------------------

const inviteColumns = `id, token_hash, kind, scope, group_id, role, max_uses, uses,
	requires_approval, attests_contact, label, expires_at, revoked_at, created_by, created_at`

func scanInvite(row scanner) (Invite, error) {
	var i Invite
	err := row.Scan(&i.ID, &i.TokenHash, &i.Kind, &i.Scope, &i.GroupID, &i.Role,
		&i.MaxUses, &i.Uses, &i.RequiresApproval, &i.AttestsContact,
		&i.Label, &i.ExpiresAt, &i.RevokedAt, &i.CreatedBy, &i.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return i, ErrNotFound
	}
	return i, err
}

func (s *Store) CreateInvite(ctx context.Context, i Invite) (Invite, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO invites (token_hash, kind, scope, group_id, role, max_uses,
			requires_approval, attests_contact, label, expires_at, created_by, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		i.TokenHash, i.Kind, i.Scope, i.GroupID, i.Role, i.MaxUses,
		i.RequiresApproval, i.AttestsContact, i.Label, i.ExpiresAt, i.CreatedBy, Now())
	if err != nil {
		return Invite{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Invite{}, err
	}
	return s.GetInvite(ctx, id)
}

func (s *Store) GetInvite(ctx context.Context, id int64) (Invite, error) {
	return scanInvite(s.db.QueryRowContext(ctx,
		`SELECT `+inviteColumns+` FROM invites WHERE id = ?`, id))
}

func (s *Store) GetInviteByTokenHash(ctx context.Context, tokenHash string) (Invite, error) {
	return scanInvite(s.db.QueryRowContext(ctx,
		`SELECT `+inviteColumns+` FROM invites WHERE token_hash = ?`, tokenHash))
}

func (s *Store) ListInvites(ctx context.Context, groupID *int64) ([]Invite, error) {
	query := `SELECT ` + inviteColumns + ` FROM invites WHERE revoked_at IS NULL`
	args := []any{}
	if groupID != nil {
		query += ` AND group_id = ?`
		args = append(args, *groupID)
	}
	query += ` ORDER BY created_at DESC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var invites []Invite
	for rows.Next() {
		i, err := scanInvite(rows)
		if err != nil {
			return nil, err
		}
		invites = append(invites, i)
	}
	return invites, rows.Err()
}

func (s *Store) IncrementInviteUses(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE invites SET uses = uses + 1 WHERE id = ?`, id)
	return err
}

func (s *Store) RevokeInvite(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE invites SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`, Now(), id)
	return err
}

// --- Claim links ----------------------------------------------------------

const claimLinkColumns = `id, user_id, token_hash, single_use, expires_at,
	consumed_at, created_by, created_at`

func scanClaimLink(row scanner) (ClaimLink, error) {
	var c ClaimLink
	err := row.Scan(&c.ID, &c.UserID, &c.TokenHash, &c.SingleUse, &c.ExpiresAt,
		&c.ConsumedAt, &c.CreatedBy, &c.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

func (s *Store) CreateClaimLink(ctx context.Context, c ClaimLink) (ClaimLink, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO claim_links (user_id, token_hash, single_use, expires_at, created_by, created_at)
		VALUES (?,?,?,?,?,?)`,
		c.UserID, c.TokenHash, c.SingleUse, c.ExpiresAt, c.CreatedBy, Now())
	if err != nil {
		return ClaimLink{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return ClaimLink{}, err
	}
	return scanClaimLink(s.db.QueryRowContext(ctx,
		`SELECT `+claimLinkColumns+` FROM claim_links WHERE id = ?`, id))
}

func (s *Store) GetClaimLinkByTokenHash(ctx context.Context, tokenHash string) (ClaimLink, error) {
	return scanClaimLink(s.db.QueryRowContext(ctx,
		`SELECT `+claimLinkColumns+` FROM claim_links WHERE token_hash = ?`, tokenHash))
}

func (s *Store) ConsumeClaimLink(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE claim_links SET consumed_at = ? WHERE id = ?`, Now(), id)
	return err
}

func (s *Store) RevokeClaimLinksFor(ctx context.Context, userID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM claim_links WHERE user_id = ?`, userID)
	return err
}
