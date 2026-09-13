package store

import "context"

const (
	ActionAttestContact     = "attest_contact"
	ActionApproveMember     = "approve_member"
	ActionRemoveMember      = "remove_member"
	ActionSetMemberRole     = "set_member_role"
	ActionSetCommunityRole  = "set_community_role"
	ActionIssueClaimLink    = "issue_claim_link"
	ActionMergeUsers        = "merge_users"
	ActionCreateInvite      = "create_invite"
	ActionRevokeInvite      = "revoke_invite"
	ActionApproveSponsor    = "approve_sponsorship"
	ActionRevokeSponsor     = "revoke_sponsorship"
	ActionRemoveRSVP        = "remove_rsvp"
	ActionMarkPaid          = "mark_paid"
	ActionKittyEntry        = "kitty_entry"
	ActionCancelEvent       = "cancel_event"
	ActionUpdatePaymentInfo = "update_payment_info"

	ActionRejectClaim = "reject_claim"
)

type AuditEntry struct {
	ID         int64
	ActorID    *int64
	Action     string
	TargetType *string
	TargetID   *int64
	Detail     string // JSON
	CreatedAt  string

	ActorName *string
}

func (s *Store) Record(ctx context.Context, actorID *int64, action, targetType string, targetID *int64, detail string) error {
	if detail == "" {
		detail = "{}"
	}
	var tt *string
	if targetType != "" {
		tt = &targetType
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO audit_log (actor_id, action, target_type, target_id, detail, created_at)
		 VALUES (?,?,?,?,?,?)`,
		actorID, action, tt, targetID, detail, Now())
	return err
}

func (s *Store) CountActionsAgainst(ctx context.Context, action, targetType string, targetID int64, since string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM audit_log
		  WHERE action = ? AND target_type = ? AND target_id = ? AND created_at >= ?`,
		action, targetType, targetID, since).Scan(&n)
	return n, err
}

func (s *Store) ListAudit(ctx context.Context, limit int) ([]AuditEntry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT a.id, a.actor_id, a.action, a.target_type, a.target_id, a.detail,
		       a.created_at, u.display_name
		  FROM audit_log a LEFT JOIN users u ON u.id = a.actor_id
		 ORDER BY a.created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []AuditEntry
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.ID, &e.ActorID, &e.Action, &e.TargetType, &e.TargetID,
			&e.Detail, &e.CreatedAt, &e.ActorName); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
