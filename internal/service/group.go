package service

import (
	"context"

	"openjoga-server/internal/id"
	"openjoga-server/internal/store"
)

func (s *Service) CreateGroup(ctx context.Context, g store.Group, createdBy int64) (store.Group, error) {
	g.Slug = id.New()
	if g.Visibility == "" {
		g.Visibility = store.VisibilityUnlisted
	}
	if g.DefaultCostType == "" {
		g.DefaultCostType = store.CostFree
	}

	var created store.Group
	err := s.Store.Tx(ctx, func(tx *store.Store) error {
		var err error
		created, err = tx.CreateGroup(ctx, g)
		if err != nil {
			return err
		}
		return tx.AddMember(ctx, created.ID, createdBy,
			store.GroupRoleOrganiser, store.MemberActive, nil)
	})
	return created, err
}

//	public    anyone
//	unlisted  anyone holding the slug -- that is what the slug is for
//	private   active members and community admins only
func (s *Service) CanSeeGroup(ctx context.Context, g store.Group, userID int64, isAdmin bool) (bool, error) {
	if g.Visibility != store.VisibilityPrivate || isAdmin {
		return true, nil
	}
	if userID == 0 {
		return false, nil
	}
	return s.Store.IsActiveMember(ctx, g.ID, userID)
}

func (s *Service) CanListGroupEvents(ctx context.Context, g store.Group, userID int64, isAdmin bool) (bool, error) {
	if g.Visibility == store.VisibilityPublic || isAdmin {
		return true, nil
	}
	if userID == 0 {
		return false, nil
	}
	return s.Store.IsActiveMember(ctx, g.ID, userID)
}
