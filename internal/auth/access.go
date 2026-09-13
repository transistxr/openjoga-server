package auth

import (
	"context"
	"errors"

	"openjoga-server/internal/store"
	"openjoga-server/internal/text"
)

//	TierAnonymous  possession of an unguessable link, no identity at all
//	TierDevice     a cookie, created on first RSVP. Proves continuity in this
//	               browser and nothing about a person
//	TierVerified   a trusted contact exists - admin vouched for one
type Tier int

const (
	TierAnonymous Tier = iota
	TierDevice
	TierVerified
)

var (
	ErrForbidden   = errors.New(text.ErrForbidden)
	ErrNeedAccount = errors.New(text.ErrNeedAccount)
	ErrNeedContact = errors.New(text.ErrNeedContact)
)

type Identity struct {
	User *store.User
	Tier Tier
}

func Resolve(ctx context.Context, s *store.Store, userID int64) (Identity, error) {
	if userID == 0 {
		return Identity{Tier: TierAnonymous}, nil
	}
	u, err := s.GetUser(ctx, userID)
	if errors.Is(err, store.ErrNotFound) {
		return Identity{Tier: TierAnonymous}, nil
	}
	if err != nil {
		return Identity{}, err
	}

	tier := TierDevice
	if u.HasVerifiedContact() {
		tier = TierVerified
	}
	return Identity{User: &u, Tier: tier}, nil
}

func (i Identity) IsAnonymous() bool { return i.Tier == TierAnonymous }
func (i Identity) IsVerified() bool  { return i.Tier >= TierVerified }

func (i Identity) UserID() int64 {
	if i.User == nil {
		return 0
	}
	return i.User.ID
}

func (i Identity) DisplayName() string {
	if i.User == nil {
		return ""
	}
	return i.User.DisplayName
}

func (i Identity) IsCommunityAdmin() bool {
	return i.User != nil && i.User.IsAdmin() && i.IsVerified()
}

func (i Identity) RequireAccount() error {
	if i.User == nil {
		return ErrNeedAccount
	}
	return nil
}

func (i Identity) RequireVerified() error {
	if err := i.RequireAccount(); err != nil {
		return err
	}
	if !i.IsVerified() {
		return ErrNeedContact
	}
	return nil
}

func (i Identity) RequireCommunityAdmin() error {
	if err := i.RequireVerified(); err != nil {
		return err
	}
	if !i.User.IsAdmin() {
		return ErrForbidden
	}
	return nil
}

func (i Identity) RequireOrganiser(ctx context.Context, s *store.Store, groupID int64) error {
	if err := i.RequireVerified(); err != nil {
		return err
	}
	if i.User.IsAdmin() {
		return nil
	}
	m, err := s.GetMembership(ctx, groupID, i.User.ID)
	if errors.Is(err, store.ErrNotFound) {
		return ErrForbidden
	}
	if err != nil {
		return err
	}
	if !m.IsOrganiser() {
		return ErrForbidden
	}
	return nil
}

func CanRSVP(ctx context.Context, s *store.Store, i Identity, e store.Event) error {
	switch e.RSVPPolicy {
	case store.PolicyOpen:
		return nil

	case store.PolicyContactable:
		return i.RequireVerified()

	case store.PolicyMembersOnly:
		if err := i.RequireVerified(); err != nil {
			return err
		}
		member, err := s.IsActiveMember(ctx, e.GroupID, i.User.ID)
		if err != nil {
			return err
		}
		if !member {
			return ErrForbidden
		}
		return nil
	}
	return ErrForbidden
}
