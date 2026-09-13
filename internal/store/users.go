package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

const (
	VerifiedSelf  = "self"
	VerifiedAdmin = "admin"
)

const (
	RoleOwner  = "owner"
	RoleAdmin  = "admin"
	RoleMember = "member"
)

type User struct {
	ID          int64
	DisplayName string

	Pronouns *string

	Email            *string
	EmailVerifiedVia *string
	EmailVerifiedBy  *int64
	EmailVerifiedAt  *string

	Phone            *string
	PhoneVerifiedVia *string
	PhoneVerifiedBy  *int64
	PhoneVerifiedAt  *string

	IsDeviceOnly  bool
	CommunityRole string
	CreatedAt     string
}

func (u User) EmailProven() bool {
	return u.Email != nil && u.EmailVerifiedVia != nil && *u.EmailVerifiedVia == VerifiedSelf
}

func (u User) HasVerifiedContact() bool {
	return (u.Email != nil && u.EmailVerifiedVia != nil) ||
		(u.Phone != nil && u.PhoneVerifiedVia != nil)
}

func (u User) IsAdmin() bool {
	return u.CommunityRole == RoleOwner || u.CommunityRole == RoleAdmin
}

const userColumns = `id, display_name, pronouns,
	email, email_verified_via, email_verified_by, email_verified_at,
	phone, phone_verified_via, phone_verified_by, phone_verified_at,
	is_device_only, community_role, created_at`

type scanner interface{ Scan(dest ...any) error }

func scanUser(row scanner) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.DisplayName, &u.Pronouns,
		&u.Email, &u.EmailVerifiedVia, &u.EmailVerifiedBy, &u.EmailVerifiedAt,
		&u.Phone, &u.PhoneVerifiedVia, &u.PhoneVerifiedBy, &u.PhoneVerifiedAt,
		&u.IsDeviceOnly, &u.CommunityRole, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrNotFound
	}
	return u, err
}

func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// CreateDeviceUser records somebody who has typed a name and nothing else. They
// are reachable only by the cookie on the browser that created them; if that
// cookie goes, so does this identity.
func (s *Store) CreateDeviceUser(ctx context.Context, displayName string) (User, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO users (display_name, is_device_only, created_at, updated_at)
		 VALUES (?, 1, ?, ?)`,
		displayName, Now(), Now())
	if err != nil {
		return User{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return User{}, err
	}
	return s.GetUser(ctx, id)
}

func (s *Store) GetUser(ctx context.Context, id int64) (User, error) {
	return scanUser(s.db.QueryRowContext(ctx,
		`SELECT `+userColumns+` FROM users WHERE id = ?`, id))
}

// GetUserByProvenEmail finds the account an email may be merged into.
func (s *Store) GetUserByProvenEmail(ctx context.Context, email string) (User, error) {
	return scanUser(s.db.QueryRowContext(ctx,
		`SELECT `+userColumns+` FROM users
		 WHERE email = ? AND email_verified_via = ?`,
		NormalizeEmail(email), VerifiedSelf))
}

func (s *Store) SetDisplayName(ctx context.Context, userID int64, name string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET display_name = ?, updated_at = ? WHERE id = ?`,
		name, Now(), userID)
	return err
}

func (s *Store) SetPronouns(ctx context.Context, userID int64, pronouns *string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET pronouns = ?, updated_at = ? WHERE id = ?`,
		pronouns, Now(), userID)
	return err
}

func (s *Store) VerifyEmail(ctx context.Context, userID int64, email string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users
		    SET email = ?, email_verified_via = ?, email_verified_at = ?,
		        is_device_only = 0, updated_at = ?
		  WHERE id = ?`,
		NormalizeEmail(email), VerifiedSelf, Now(), Now(), userID)
	return err
}

// AttestContact records an admin vouching for somebody's contact details: they
// met them and the number works.
func (s *Store) AttestContact(ctx context.Context, userID int64, email, phone string, byAdmin int64) error {
	if email != "" {
		if _, err := s.db.ExecContext(ctx,
			`UPDATE users
			    SET email = ?, email_verified_via = ?, email_verified_by = ?,
			        email_verified_at = ?, is_device_only = 0, updated_at = ?
			  WHERE id = ?
			    AND (email_verified_via IS NULL OR email_verified_via = ?)`,
			NormalizeEmail(email), VerifiedAdmin, byAdmin, Now(), Now(), userID, VerifiedAdmin,
		); err != nil {
			return taken(fmt.Errorf("attest email: %w", err))
		}
	}
	if phone != "" {
		if _, err := s.db.ExecContext(ctx,
			`UPDATE users
			    SET phone = ?, phone_verified_via = ?, phone_verified_by = ?,
			        phone_verified_at = ?, is_device_only = 0, updated_at = ?
			  WHERE id = ?`,
			phone, VerifiedAdmin, byAdmin, Now(), Now(), userID,
		); err != nil {
			return taken(fmt.Errorf("attest phone: %w", err))
		}
	}
	return nil
}

// SetUnverifiedContact records a contact somebody supplied about themselves,
// with nobody vouching for it yet.
func (s *Store) SetUnverifiedContact(ctx context.Context, userID int64, email, phone string) error {
	if email != "" {
		_, err := s.db.ExecContext(ctx,
			`UPDATE users SET email = ?, updated_at = ?
			  WHERE id = ? AND email_verified_via IS NULL`,
			NormalizeEmail(email), Now(), userID)
		if err != nil {
			return taken(err)
		}
	}
	if phone != "" {
		_, err := s.db.ExecContext(ctx,
			`UPDATE users SET phone = ?, updated_at = ?
			  WHERE id = ? AND phone_verified_via IS NULL`,
			phone, Now(), userID)
		if err != nil {
			return taken(err)
		}
	}
	return nil
}

var ErrContactTaken = errors.New("that contact already belongs to somebody else")

func taken(err error) error {
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return ErrContactTaken
	}
	return err
}

func (s *Store) CountAdmins(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM users WHERE community_role IN (?, ?)`,
		RoleOwner, RoleAdmin).Scan(&n)
	return n, err
}

func (s *Store) SetCommunityRole(ctx context.Context, userID int64, role string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET community_role = ?, updated_at = ? WHERE id = ?`,
		role, Now(), userID)
	return err
}

func (s *Store) Reachable(ctx context.Context, userID int64) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
		SELECT (
		  SELECT COUNT(*) FROM users u
		   WHERE u.id = ?
		     AND ((u.email IS NOT NULL AND u.email_verified_via IS NOT NULL)
		       OR (u.phone IS NOT NULL AND u.phone_verified_via IS NOT NULL))
		) + (
		  SELECT COUNT(*) FROM push_subscriptions WHERE user_id = ?
		)`, userID, userID).Scan(&n)
	return n > 0, err
}

func (s *Store) ReachableSet(ctx context.Context, userIDs []int64) (map[int64]bool, error) {
	out := make(map[int64]bool, len(userIDs))
	if len(userIDs) == 0 {
		return out, nil
	}
	args := make([]any, len(userIDs))
	for i, id := range userIDs {
		args[i] = id
		out[id] = false
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(userIDs)), ",")

	rows, err := s.db.QueryContext(ctx, `
		SELECT u.id,
		       ((u.email IS NOT NULL AND u.email_verified_via IS NOT NULL)
		     OR (u.phone IS NOT NULL AND u.phone_verified_via IS NOT NULL)
		     OR EXISTS (SELECT 1 FROM push_subscriptions p WHERE p.user_id = u.id))
		  FROM users u WHERE u.id IN (`+placeholders+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var id int64
		var reachable bool
		if err := rows.Scan(&id, &reachable); err != nil {
			return nil, err
		}
		out[id] = reachable
	}
	return out, rows.Err()
}

func (s *Store) ListCommunityMembers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+userColumns+` FROM users
		  WHERE is_device_only = 0 ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// MergeUsers folds `from` into `to` and deletes `from`.
//
// Two callers: claiming an identity when an email turns out to belong to an
// existing account, and an admin manually merging the duplicate rows that appear
// whenever somebody types their name on a second browser.
func (s *Store) MergeUsers(ctx context.Context, from, to int64) error {
	if from == to {
		return errors.New("store: cannot merge a user into itself")
	}

	exec := func(desc, query string) error {
		if _, err := s.db.ExecContext(ctx, query,
			sql.Named("from", from), sql.Named("to", to)); err != nil {
			return fmt.Errorf("merge %s: %w", desc, err)
		}
		return nil
	}

	steps := []struct{ desc, query string }{
		{"guests onto surviving rsvps", `
			UPDATE guests SET rsvp_id = (
			    SELECT t.id FROM rsvps t
			     WHERE t.user_id = :to
			       AND t.event_id = (SELECT s.event_id FROM rsvps s WHERE s.id = guests.rsvp_id))
			 WHERE rsvp_id IN (
			    SELECT s.id FROM rsvps s
			     WHERE s.user_id = :from
			       AND EXISTS (SELECT 1 FROM rsvps t
			                    WHERE t.event_id = s.event_id AND t.user_id = :to))`},
		{"duplicate rsvps", `
			DELETE FROM rsvps
			 WHERE user_id = :from
			   AND EXISTS (SELECT 1 FROM rsvps t
			                WHERE t.event_id = rsvps.event_id AND t.user_id = :to)`},
		{"rsvps", `UPDATE rsvps SET user_id = :to WHERE user_id = :from`},

		{"paid status onto surviving payments", `
			UPDATE payments
			   SET status = 'paid',
			       marked_paid_at = (SELECT s.marked_paid_at FROM payments s
			                          WHERE s.event_id = payments.event_id AND s.user_id = :from),
			       marked_by      = (SELECT s.marked_by      FROM payments s
			                          WHERE s.event_id = payments.event_id AND s.user_id = :from)
			 WHERE user_id = :to AND status = 'owed'
			   AND EXISTS (SELECT 1 FROM payments s
			                WHERE s.event_id = payments.event_id
			                  AND s.user_id = :from AND s.status = 'paid')`},
		{"duplicate payments", `
			DELETE FROM payments
			 WHERE user_id = :from
			   AND EXISTS (SELECT 1 FROM payments t
			                WHERE t.event_id = payments.event_id AND t.user_id = :to)`},
		{"payments", `UPDATE payments SET user_id = :to WHERE user_id = :from`},

		{"membership role and status", `
			UPDATE group_members
			   SET role = CASE WHEN role = 'organiser' THEN 'organiser'
			                   WHEN (SELECT s.role FROM group_members s
			                          WHERE s.group_id = group_members.group_id
			                            AND s.user_id = :from) = 'organiser' THEN 'organiser'
			                   ELSE role END,
			       status = CASE WHEN status = 'active' THEN 'active'
			                     WHEN (SELECT s.status FROM group_members s
			                            WHERE s.group_id = group_members.group_id
			                              AND s.user_id = :from) = 'active' THEN 'active'
			                     ELSE status END
			 WHERE user_id = :to
			   AND EXISTS (SELECT 1 FROM group_members s
			                WHERE s.group_id = group_members.group_id AND s.user_id = :from)`},
		{"duplicate memberships", `
			DELETE FROM group_members
			 WHERE user_id = :from
			   AND EXISTS (SELECT 1 FROM group_members t
			                WHERE t.group_id = group_members.group_id AND t.user_id = :to)`},
		{"memberships", `UPDATE group_members SET user_id = :to WHERE user_id = :from`},

		// --- Notification preferences ------------------------------------
		{"duplicate notification preferences", `
			DELETE FROM notification_preferences
			 WHERE user_id = :from
			   AND EXISTS (SELECT 1 FROM notification_preferences t
			                WHERE t.user_id = :to
			                  AND t.channel = notification_preferences.channel
			                  AND t.event_type = notification_preferences.event_type)`},
		{"notification preferences", `
			UPDATE notification_preferences SET user_id = :to WHERE user_id = :from`},

		// --- Everything without a collision to resolve -------------------
		{"sponsorships", `UPDATE sponsorships SET sponsor_user_id = :to WHERE sponsor_user_id = :from`},
		{"sponsorship approvals", `UPDATE sponsorships SET approved_by = :to WHERE approved_by = :from`},
		{"kitty entries", `UPDATE kitty_entries SET user_id = :to WHERE user_id = :from`},
		{"kitty authorship", `UPDATE kitty_entries SET created_by = :to WHERE created_by = :from`},
		{"payment attribution", `UPDATE payments SET marked_by = :to WHERE marked_by = :from`},
		{"notifications", `UPDATE notifications SET user_id = :to WHERE user_id = :from`},
		{"push subscriptions", `UPDATE push_subscriptions SET user_id = :to WHERE user_id = :from`},
		{"claim links", `UPDATE claim_links SET user_id = :to WHERE user_id = :from`},
		{"claim link authorship", `UPDATE claim_links SET created_by = :to WHERE created_by = :from`},
		{"pending verifications", `
			UPDATE verification_challenges SET claim_user_id = :to WHERE claim_user_id = :from`},
		{"events created", `UPDATE events SET created_by = :to WHERE created_by = :from`},
		{"invites created", `UPDATE invites SET created_by = :to WHERE created_by = :from`},
		{"attestations given", `
			UPDATE users SET email_verified_by = :to WHERE email_verified_by = :from`},
		{"phone attestations given", `
			UPDATE users SET phone_verified_by = :to WHERE phone_verified_by = :from`},
		{"audit trail", `UPDATE audit_log SET actor_id = :to WHERE actor_id = :from`},

		// --- Contacts ----------------------------------------------------
		{"email", `
			UPDATE users
			   SET email             = (SELECT s.email             FROM users s WHERE s.id = :from),
			       email_verified_via = (SELECT s.email_verified_via FROM users s WHERE s.id = :from),
			       email_verified_by  = (SELECT s.email_verified_by  FROM users s WHERE s.id = :from),
			       email_verified_at  = (SELECT s.email_verified_at  FROM users s WHERE s.id = :from),
			       updated_at = strftime('%Y-%m-%dT%H:%M:%SZ','now')
			 WHERE id = :to AND email IS NULL
			   AND (SELECT s.email FROM users s WHERE s.id = :from) IS NOT NULL`},
		{"phone", `
			UPDATE users
			   SET phone              = (SELECT s.phone              FROM users s WHERE s.id = :from),
			       phone_verified_via = (SELECT s.phone_verified_via FROM users s WHERE s.id = :from),
			       phone_verified_by  = (SELECT s.phone_verified_by  FROM users s WHERE s.id = :from),
			       phone_verified_at  = (SELECT s.phone_verified_at  FROM users s WHERE s.id = :from),
			       updated_at = strftime('%Y-%m-%dT%H:%M:%SZ','now')
			 WHERE id = :to AND phone IS NULL
			   AND (SELECT s.phone FROM users s WHERE s.id = :from) IS NOT NULL`},

		{"device-only flag", `
			UPDATE users SET is_device_only = 0
			 WHERE id = :to
			   AND (email IS NOT NULL OR phone IS NOT NULL)`},

		{"source user", `DELETE FROM users WHERE id = :from`},
	}

	for _, step := range steps {
		if err := exec(step.desc, step.query); err != nil {
			return err
		}
	}
	return nil
}
