package store

import (
	"context"
	"database/sql"
	"errors"
)

// A pending proof that somebody controls an email address.
type Challenge struct {
	ID         int64
	Identifier string

	LinkTokenHash string
	CodeHash      string

	// The device-user to fold into
	ClaimUserID *int64

	Attempts    int
	MaxAttempts int
	ExpiresAt   string
	ConsumedAt  *string
	CreatedAt   string
}

func (c Challenge) Expired() bool  { return c.ExpiresAt <= Now() }
func (c Challenge) Consumed() bool { return c.ConsumedAt != nil }
func (c Challenge) Exhausted() bool {
	return c.Attempts >= c.MaxAttempts
}

const challengeColumns = `id, identifier, link_token_hash, code_hash, claim_user_id,
	attempts, max_attempts, expires_at, consumed_at, created_at`

func scanChallenge(row scanner) (Challenge, error) {
	var c Challenge
	err := row.Scan(&c.ID, &c.Identifier, &c.LinkTokenHash, &c.CodeHash, &c.ClaimUserID,
		&c.Attempts, &c.MaxAttempts, &c.ExpiresAt, &c.ConsumedAt, &c.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

func (s *Store) CreateChallenge(ctx context.Context, c Challenge) (Challenge, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO verification_challenges (channel, identifier, link_token_hash,
			code_hash, claim_user_id, max_attempts, expires_at, created_at)
		VALUES ('email',?,?,?,?,?,?,?)`,
		c.Identifier, c.LinkTokenHash, c.CodeHash, c.ClaimUserID,
		c.MaxAttempts, c.ExpiresAt, Now())
	if err != nil {
		return Challenge{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Challenge{}, err
	}
	return scanChallenge(s.db.QueryRowContext(ctx,
		`SELECT `+challengeColumns+` FROM verification_challenges WHERE id = ?`, id))
}

func (s *Store) GetChallengeByLinkToken(ctx context.Context, tokenHash string) (Challenge, error) {
	return scanChallenge(s.db.QueryRowContext(ctx,
		`SELECT `+challengeColumns+` FROM verification_challenges WHERE link_token_hash = ?`,
		tokenHash))
}

// GetActiveChallenge finds the live challenge for an address, newest first.
func (s *Store) GetActiveChallenge(ctx context.Context, identifier string) (Challenge, error) {
	return scanChallenge(s.db.QueryRowContext(ctx,
		`SELECT `+challengeColumns+` FROM verification_challenges
		  WHERE identifier = ? AND consumed_at IS NULL AND expires_at > ?
		  ORDER BY created_at DESC LIMIT 1`, NormalizeEmail(identifier), Now()))
}

func (s *Store) IncrementChallengeAttempts(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE verification_challenges SET attempts = attempts + 1 WHERE id = ?`, id)
	return err
}

func (s *Store) ConsumeChallenge(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE verification_challenges SET consumed_at = ? WHERE id = ?`, Now(), id)
	return err
}

func (s *Store) InvalidateChallenges(ctx context.Context, identifier string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE verification_challenges SET consumed_at = ?
		  WHERE identifier = ? AND consumed_at IS NULL`,
		Now(), NormalizeEmail(identifier))
	return err
}

func (s *Store) CountChallengesSince(ctx context.Context, identifier, since string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM verification_challenges
		  WHERE identifier = ? AND created_at >= ?`,
		NormalizeEmail(identifier), since).Scan(&n)
	return n, err
}

func (s *Store) DeleteExpiredChallenges(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM verification_challenges WHERE expires_at < ?`, Now())
	return err
}
