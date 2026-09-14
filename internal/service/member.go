package service

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"openjoga-server/internal/id"
	"openjoga-server/internal/store"
	"openjoga-server/internal/text"
)

const (
	MemberClaimLinkTTL = 365 * 24 * time.Hour

	OrganiserClaimLinkTTL = 24 * time.Hour
)

var ErrClaimLinkUnusable = errors.New(text.ErrClaimUsed)

type AddedMember struct {
	User        store.User
	ClaimToken  string
	ClaimURL    string
	WhatsAppURL string
}

func (s *Service) AddMember(ctx context.Context, name, email, phone string, groupID *int64, byAdmin int64) (AddedMember, error) {
	var out AddedMember

	err := s.Store.Tx(ctx, func(tx *store.Store) error {
		user, err := tx.CreateDeviceUser(ctx, name)
		if err != nil {
			return err
		}
		if err := tx.AttestContact(ctx, user.ID, email, phone, byAdmin); err != nil {
			return err
		}
		if groupID != nil {
			if err := tx.AddMember(ctx, *groupID, user.ID, store.GroupRoleMember, store.MemberActive, nil); err != nil {
				return err
			}
		}
		out.User, err = tx.GetUser(ctx, user.ID)
		return err
	})
	if err != nil {
		return out, err
	}

	token, err := s.IssueClaimLink(ctx, out.User.ID, byAdmin, false)
	if err != nil {
		return out, err
	}
	out.ClaimToken = token
	out.ClaimURL = s.ClaimURL(token)
	out.WhatsAppURL = WhatsAppMessageURL(phone, text.Welcome(out.ClaimURL))

	_ = s.Store.Record(ctx, &byAdmin, store.ActionAttestContact, "user", &out.User.ID, "")
	return out, nil
}

const MaxDisplayNameLength = 60

func NormalizeDisplayName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" || utf8.RuneCountInString(name) > MaxDisplayNameLength {
		return "", ErrInvalid
	}
	return name, nil
}

const MaxPronounsLength = 40

type ProfileUpdate struct {
	DisplayName *string
	Pronouns    *string
}

func (s *Service) UpdateProfile(ctx context.Context, userID int64, up ProfileUpdate) error {
	if up.DisplayName != nil {
		name, err := NormalizeDisplayName(*up.DisplayName)
		if err != nil {
			return err
		}
		if err := s.Store.SetDisplayName(ctx, userID, name); err != nil {
			return err
		}
	}

	if up.Pronouns != nil {
		pronouns, err := NormalizePronouns(*up.Pronouns)
		if err != nil {
			return err
		}
		if err := s.Store.SetPronouns(ctx, userID, pronouns); err != nil {
			return err
		}
	}
	return nil
}

func NormalizePronouns(raw string) (*string, error) {
	pronouns := strings.TrimSpace(raw)
	if pronouns == "" {
		return nil, nil
	}
	if utf8.RuneCountInString(pronouns) > MaxPronounsLength {
		return nil, ErrInvalid
	}
	return &pronouns, nil
}

func (s *Service) ClaimInstanceIfUnowned(ctx context.Context, userID int64) (bool, error) {
	owners, err := s.Store.CountAdmins(ctx)
	if err != nil || owners > 0 {
		return false, err
	}
	if err := s.Store.SetCommunityRole(ctx, userID, store.RoleOwner); err != nil {
		return false, err
	}
	return true, s.Store.Record(ctx, &userID, store.ActionSetCommunityRole, "user", &userID,
		`{"role":"owner","reason":"first sign-in on an unowned instance"}`)
}

func (s *Service) IssueClaimLink(ctx context.Context, userID, byAdmin int64, organiser bool) (string, error) {
	token := id.New()
	ttl := MemberClaimLinkTTL
	if organiser {
		ttl = OrganiserClaimLinkTTL
	}

	if _, err := s.Store.CreateClaimLink(ctx, store.ClaimLink{
		UserID:    userID,
		TokenHash: id.Hash(token),
		SingleUse: organiser,
		ExpiresAt: ptr(store.FormatTime(time.Now().Add(ttl))),
		CreatedBy: &byAdmin,
	}); err != nil {
		return "", err
	}
	_ = s.Store.Record(ctx, &byAdmin, store.ActionIssueClaimLink, "user", &userID, "")
	return token, nil
}

func (s *Service) ClaimURL(token string) string {
	return s.BaseURL + "/claim/" + token
}

func (s *Service) RedeemClaimLink(ctx context.Context, token string) (int64, error) {
	link, err := s.Store.GetClaimLinkByTokenHash(ctx, id.Hash(token))
	if errors.Is(err, store.ErrNotFound) {
		return 0, ErrClaimLinkUnusable
	}
	if err != nil {
		return 0, err
	}
	if !link.Usable() {
		return 0, ErrClaimLinkUnusable
	}
	if err := s.Store.ConsumeClaimLink(ctx, link.ID); err != nil {
		return 0, err
	}
	return link.UserID, nil
}

func (s *Service) ApproveMember(ctx context.Context, groupID, userID, by int64) error {
	if err := s.Store.SetMemberStatus(ctx, groupID, userID, store.MemberActive); err != nil {
		return err
	}
	if u, err := s.Store.GetUser(ctx, userID); err == nil {
		email, phone := "", ""
		if u.Email != nil && u.EmailVerifiedVia == nil {
			email = *u.Email
		}
		if u.Phone != nil && u.PhoneVerifiedVia == nil {
			phone = *u.Phone
		}
		if email != "" || phone != "" {
			_ = s.Store.AttestContact(ctx, userID, email, phone, by)
		}
	}
	return s.Store.Record(ctx, &by, store.ActionApproveMember, "user", &userID,
		fmt.Sprintf(`{"group_id":%d}`, groupID))
}

func (s *Service) RemoveMember(ctx context.Context, groupID, userID, by int64) error {
	if err := s.Store.RemoveMember(ctx, groupID, userID); err != nil {
		return err
	}
	return s.Store.Record(ctx, &by, store.ActionRemoveMember, "user", &userID,
		fmt.Sprintf(`{"group_id":%d}`, groupID))
}

func (s *Service) AttestContact(ctx context.Context, userID int64, email, phone string, byAdmin int64) error {
	if err := s.Store.AttestContact(ctx, userID, email, phone, byAdmin); err != nil {
		return err
	}
	return s.Store.Record(ctx, &byAdmin, store.ActionAttestContact, "user", &userID, "")
}

func (s *Service) MergeUsers(ctx context.Context, from, to, byAdmin int64) error {
	if from == to {
		return ErrInvalid
	}
	err := s.Store.Tx(ctx, func(tx *store.Store) error {
		return tx.MergeUsers(ctx, from, to)
	})
	if err != nil {
		return err
	}
	return s.Store.Record(ctx, &byAdmin, store.ActionMergeUsers, "user", &to,
		fmt.Sprintf(`{"merged_from":%d}`, from))
}

func WhatsAppMessageURL(phone, text string) string {
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, phone)
	if digits == "" {
		return ""
	}
	return "https://wa.me/" + digits + "?text=" + url.QueryEscape(text)
}

func (s *Service) ShareMessage(e store.Event, capacity store.Capacity) string {
	return text.Share(capacity.Total(), capacity.Max, capacity.SpotsLeft(), s.EventURL(e.Slug))
}

var ErrLastOwner = errors.New(text.ErrLastOwner)

func (s *Service) SetCommunityRole(ctx context.Context, userID int64, role string, byOwner int64) error {
	if userID == byOwner {
		return ErrNotAllowed
	}
	switch role {
	case store.RoleOwner, store.RoleAdmin, store.RoleMember:
	default:
		return ErrInvalid
	}

	err := s.Store.Tx(ctx, func(tx *store.Store) error {
		target, err := tx.GetUser(ctx, userID)
		if err != nil {
			return err
		}
		if target.IsAdmin() && role == store.RoleMember {
			admins, err := tx.CountAdmins(ctx)
			if err != nil {
				return err
			}
			if admins <= 1 {
				return ErrLastOwner
			}
		}
		return tx.SetCommunityRole(ctx, userID, role)
	})
	if err != nil {
		return err
	}
	return s.Store.Record(ctx, &byOwner, store.ActionSetCommunityRole, "user", &userID,
		fmt.Sprintf(`{"role":%q}`, role))
}

func (s *Service) SetGroupRole(ctx context.Context, groupID, userID int64, role string, by int64, byIsAdmin bool) error {
	if userID == by {
		return ErrNotAllowed
	}
	if role != store.GroupRoleOrganiser && role != store.GroupRoleMember {
		return ErrInvalid
	}
	if role == store.GroupRoleMember && !byIsAdmin {
		return ErrNotAllowed
	}

	m, err := s.Store.GetMembership(ctx, groupID, userID)
	if err != nil {
		return err
	}
	if !m.IsActive() {
		return ErrNotAllowed
	}

	if err := s.Store.SetMemberRole(ctx, groupID, userID, role); err != nil {
		return err
	}
	return s.Store.Record(ctx, &by, store.ActionSetMemberRole, "user", &userID,
		fmt.Sprintf(`{"group_id":%d,"role":%q}`, groupID, role))
}
