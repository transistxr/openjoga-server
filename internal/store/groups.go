package store

import (
	"context"
	"database/sql"
	"errors"
)

const (
	VisibilityPublic   = "public"
	VisibilityPrivate  = "private"
	VisibilityUnlisted = "unlisted"

	GroupRoleOrganiser = "organiser"
	GroupRoleMember    = "member"

	MemberPending = "pending"
	MemberActive  = "active"
)

type Community struct {
	ID       int64
	Name     string
	Currency string
	Settings string
}

type Group struct {
	ID int64
	Slug            string
	Name            string
	Sport           *string
	Description     *string
	Visibility      string
	DefaultLocation *string

	DefaultCostType         string
	DefaultCostPerHeadPence *int64
	DefaultCostTotalPence   *int64

	MaxGuestsPerRSVP   int
	MaxSlotsPerSponsor int

	PaymentKind  *string
	PaymentValue *string
	PaymentLabel *string

	TrackKitty bool
	CreatedAt  string
}

func (g Group) HasPaymentDetails() bool {
	return g.PaymentKind != nil && g.PaymentValue != nil && *g.PaymentValue != ""
}

type Membership struct {
	ID           int64
	GroupID      int64
	UserID       int64
	Role         string
	Status       string
	InviteID     *int64
	JoinedAt     string
	UserName     string // joined from users
	UserPronouns *string
	UserEmail    *string
}

func (m Membership) IsActive() bool    { return m.Status == MemberActive }
func (m Membership) IsOrganiser() bool { return m.Role == GroupRoleOrganiser && m.IsActive() }

// --- Community ------------------------------------------------------------

func (s *Store) GetCommunity(ctx context.Context, defaultName, defaultCurrency string) (Community, error) {
	var c Community
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, currency, settings FROM community WHERE id = 1`,
	).Scan(&c.ID, &c.Name, &c.Currency, &c.Settings)

	if errors.Is(err, sql.ErrNoRows) {
		if _, err := s.db.ExecContext(ctx,
			`INSERT INTO community (id, name, currency, created_at) VALUES (1, ?, ?, ?)`,
			defaultName, defaultCurrency, Now()); err != nil {
			return c, err
		}
		return Community{ID: 1, Name: defaultName, Currency: defaultCurrency, Settings: "{}"}, nil
	}
	return c, err
}

func (s *Store) UpdateCommunity(ctx context.Context, name, currency string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE community SET name = ?, currency = ? WHERE id = 1`, name, currency)
	return err
}

// --- Groups ---------------------------------------------------------------

const groupColumns = `id, slug, name, sport, description, visibility, default_location,
	default_cost_type, default_cost_per_head_pence, default_cost_total_pence,
	max_guests_per_rsvp, max_slots_per_sponsor,
	payment_kind, payment_value, payment_label, track_kitty, created_at`

func scanGroup(row scanner) (Group, error) {
	var g Group
	err := row.Scan(&g.ID, &g.Slug, &g.Name, &g.Sport, &g.Description, &g.Visibility, &g.DefaultLocation,
		&g.DefaultCostType, &g.DefaultCostPerHeadPence, &g.DefaultCostTotalPence,
		&g.MaxGuestsPerRSVP, &g.MaxSlotsPerSponsor,
		&g.PaymentKind, &g.PaymentValue, &g.PaymentLabel, &g.TrackKitty, &g.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return g, ErrNotFound
	}
	return g, err
}

func (s *Store) CreateGroup(ctx context.Context, g Group) (Group, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO groups (slug, name, sport, description, visibility, default_location,
			default_cost_type, default_cost_per_head_pence, default_cost_total_pence,
			max_guests_per_rsvp, max_slots_per_sponsor,
			payment_kind, payment_value, payment_label, track_kitty, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		g.Slug, g.Name, g.Sport, g.Description, g.Visibility, g.DefaultLocation,
		g.DefaultCostType, g.DefaultCostPerHeadPence, g.DefaultCostTotalPence,
		g.MaxGuestsPerRSVP, g.MaxSlotsPerSponsor,
		g.PaymentKind, g.PaymentValue, g.PaymentLabel, g.TrackKitty, Now(), Now())
	if err != nil {
		return Group{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Group{}, err
	}
	return s.GetGroup(ctx, id)
}

func (s *Store) GetGroup(ctx context.Context, id int64) (Group, error) {
	return scanGroup(s.db.QueryRowContext(ctx,
		`SELECT `+groupColumns+` FROM groups WHERE id = ?`, id))
}

// GetGroupBySlug is how every request finds a group.
func (s *Store) GetGroupBySlug(ctx context.Context, slug string) (Group, error) {
	return scanGroup(s.db.QueryRowContext(ctx,
		`SELECT `+groupColumns+` FROM groups WHERE slug = ?`, slug))
}

// ListGroups returns what a caller may know exists.
func (s *Store) ListGroups(ctx context.Context, userID int64, isAdmin bool) ([]Group, error) {
	query := `SELECT ` + groupColumns + ` FROM groups`
	args := []any{}
	if !isAdmin {
		query += ` WHERE visibility = ?
		             OR id IN (SELECT group_id FROM group_members
		                        WHERE user_id = ? AND status = ?)`
		args = append(args, VisibilityPublic, userID, MemberActive)
	}
	query += ` ORDER BY name`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []Group
	for rows.Next() {
		g, err := scanGroup(rows)
		if err != nil {
			return nil, err
		}
		groups = append(groups, g)
	}
	return groups, rows.Err()
}

func (s *Store) UpdateGroup(ctx context.Context, g Group) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE groups SET name = ?, sport = ?, description = ?, visibility = ?,
			default_location = ?, default_cost_type = ?, default_cost_per_head_pence = ?,
			default_cost_total_pence = ?, max_guests_per_rsvp = ?, max_slots_per_sponsor = ?,
			payment_kind = ?, payment_value = ?, payment_label = ?, track_kitty = ?,
			updated_at = ?
		 WHERE id = ?`,
		g.Name, g.Sport, g.Description, g.Visibility, g.DefaultLocation,
		g.DefaultCostType, g.DefaultCostPerHeadPence, g.DefaultCostTotalPence,
		g.MaxGuestsPerRSVP, g.MaxSlotsPerSponsor,
		g.PaymentKind, g.PaymentValue, g.PaymentLabel, g.TrackKitty, Now(), g.ID)
	return err
}

// --- Membership -----------------------------------------------------------

const membershipColumns = `m.id, m.group_id, m.user_id, m.role, m.status, m.invite_id,
	m.joined_at, u.display_name, u.pronouns, u.email`

func scanMembership(row scanner) (Membership, error) {
	var m Membership
	err := row.Scan(&m.ID, &m.GroupID, &m.UserID, &m.Role, &m.Status, &m.InviteID,
		&m.JoinedAt, &m.UserName, &m.UserPronouns, &m.UserEmail)
	if errors.Is(err, sql.ErrNoRows) {
		return m, ErrNotFound
	}
	return m, err
}

// AddMember adds somebody to a group, or leaves an existing membership alone.
func (s *Store) AddMember(ctx context.Context, groupID, userID int64, role, status string, inviteID *int64) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO group_members (group_id, user_id, role, status, invite_id, joined_at)
		VALUES (?,?,?,?,?,?)
		ON CONFLICT (group_id, user_id) DO NOTHING`,
		groupID, userID, role, status, inviteID, Now())
	return err
}

func (s *Store) GetMembership(ctx context.Context, groupID, userID int64) (Membership, error) {
	return scanMembership(s.db.QueryRowContext(ctx,
		`SELECT `+membershipColumns+` FROM group_members m
		   JOIN users u ON u.id = m.user_id
		  WHERE m.group_id = ? AND m.user_id = ?`, groupID, userID))
}

// ListMembers returns members in a given status, active ones by default.
func (s *Store) ListMembers(ctx context.Context, groupID int64, status string) ([]Membership, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+membershipColumns+` FROM group_members m
		   JOIN users u ON u.id = m.user_id
		  WHERE m.group_id = ? AND m.status = ?
		  ORDER BY u.display_name`, groupID, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var members []Membership
	for rows.Next() {
		m, err := scanMembership(rows)
		if err != nil {
			return nil, err
		}
		members = append(members, m)
	}
	return members, rows.Err()
}

// ListGroupsForUser returns the groups somebody is an active member of.
func (s *Store) ListGroupsForUser(ctx context.Context, userID int64) ([]Group, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+groupColumns+` FROM groups
		  WHERE id IN (SELECT group_id FROM group_members
		                WHERE user_id = ? AND status = ?)
		  ORDER BY name`, userID, MemberActive)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []Group
	for rows.Next() {
		g, err := scanGroup(rows)
		if err != nil {
			return nil, err
		}
		groups = append(groups, g)
	}
	return groups, rows.Err()
}

func (s *Store) SetMemberStatus(ctx context.Context, groupID, userID int64, status string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE group_members SET status = ? WHERE group_id = ? AND user_id = ?`,
		status, groupID, userID)
	return err
}

func (s *Store) SetMemberRole(ctx context.Context, groupID, userID int64, role string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE group_members SET role = ? WHERE group_id = ? AND user_id = ?`,
		role, groupID, userID)
	return err
}

func (s *Store) RemoveMember(ctx context.Context, groupID, userID int64) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM group_members WHERE group_id = ? AND user_id = ?`, groupID, userID)
	return err
}

func (s *Store) IsActiveMember(ctx context.Context, groupID, userID int64) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM group_members
		  WHERE group_id = ? AND user_id = ? AND status = ?`,
		groupID, userID, MemberActive).Scan(&n)
	return n > 0, err
}
