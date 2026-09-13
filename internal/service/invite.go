package service

import (
	"context"
	"errors"
	"time"

	"openjoga-server/internal/id"
	"openjoga-server/internal/store"
	"openjoga-server/internal/text"
)

const RotatingInviteTTL = 10 * time.Minute

const DurableInviteTTL = 90 * 24 * time.Hour

var ErrInviteUnusable = errors.New(text.ErrInviteUsed)

func (s *Service) CreateInvite(ctx context.Context, inv store.Invite, creatorID int64, creatorIsAdmin bool) (store.Invite, string, error) {
	token := id.New()
	inv.TokenHash = id.Hash(token)
	inv.CreatedBy = &creatorID

	if inv.Kind == "" {
		inv.Kind = store.InviteDurable
	}
	if inv.Role == "" {
		inv.Role = store.GroupRoleMember
	}

	if !creatorIsAdmin {
		inv.RequiresApproval = true
		inv.Role = store.GroupRoleMember
		inv.AttestsContact = false
	}

	switch inv.Kind {
	case store.InviteRotating:
		inv.ExpiresAt = ptr(store.FormatTime(time.Now().Add(RotatingInviteTTL)))
		if creatorIsAdmin {
			inv.RequiresApproval = false
			inv.AttestsContact = true
		}
	default:
		inv.RequiresApproval = true
		if inv.ExpiresAt == nil {
			inv.ExpiresAt = ptr(store.FormatTime(time.Now().Add(DurableInviteTTL)))
		}
	}

	created, err := s.Store.CreateInvite(ctx, inv)
	if err != nil {
		return store.Invite{}, "", err
	}
	_ = s.Store.Record(ctx, &creatorID, store.ActionCreateInvite, "invite", &created.ID, "")
	return created, token, nil
}

func (s *Service) InviteURL(token string) string {
	return s.BaseURL + "/join/" + token
}

type RedeemOutcome struct {
	GroupID  *int64
	Status   string
	Role     string
	Attested bool
}

type JoinDetails struct {
	Email string
	Phone string
}

func (j JoinDetails) HasContact() bool { return j.Email != "" || j.Phone != "" }

func (s *Service) RedeemInvite(ctx context.Context, token string, userID int64, details JoinDetails) (RedeemOutcome, error) {
	var out RedeemOutcome

	err := s.Store.Tx(ctx, func(tx *store.Store) error {
		invite, err := tx.GetInviteByTokenHash(ctx, id.Hash(token))
		if errors.Is(err, store.ErrNotFound) {
			return ErrInviteUnusable
		}
		if err != nil {
			return err
		}
		if !invite.Usable() {
			return ErrInviteUnusable
		}

		out.Status = store.MemberActive
		if invite.RequiresApproval {
			out.Status = store.MemberPending
		}
		out.Role = invite.Role
		out.GroupID = invite.GroupID

		if invite.Scope == store.ScopeGroup && invite.GroupID != nil {
			if err := tx.AddMember(ctx, *invite.GroupID, userID, invite.Role, out.Status, &invite.ID); err != nil {
				return err
			}
		}

		if details.HasContact() {
			if invite.AttestsContact && invite.CreatedBy != nil {
				if err := tx.AttestContact(ctx, userID, details.Email, details.Phone,
					*invite.CreatedBy); err != nil {
					return err
				}
				out.Attested = true
			} else if err := tx.SetUnverifiedContact(ctx, userID, details.Email, details.Phone); err != nil {
				return err
			}
		}

		return tx.IncrementInviteUses(ctx, invite.ID)
	})
	return out, err
}

func (s *Service) RevokeInvite(ctx context.Context, inviteID, by int64) error {
	if err := s.Store.RevokeInvite(ctx, inviteID); err != nil {
		return err
	}
	return s.Store.Record(ctx, &by, store.ActionRevokeInvite, "invite", &inviteID, "")
}
