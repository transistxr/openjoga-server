package service

import (
	"context"
	"errors"

	"openjoga-server/internal/store"
)

func (s *Service) OfferSponsorship(ctx context.Context, e store.Event, g store.Group, sponsorUserID int64, slots int, note *string) (store.Sponsorship, error) {
	if !AcceptsChanges(e) {
		return store.Sponsorship{}, ErrClosed
	}
	if slots <= 0 {
		return store.Sponsorship{}, ErrInvalid
	}

	var sponsorship store.Sponsorship
	err := s.Store.Tx(ctx, func(tx *store.Store) error {
		already, err := tx.CountSponsoredSlots(ctx, e.ID, sponsorUserID)
		if err != nil {
			return err
		}
		if already+slots > g.MaxSlotsPerSponsor {
			return ErrSponsorCap
		}

		vetted, err := tx.IsActiveMember(ctx, e.GroupID, sponsorUserID)
		if err != nil {
			return err
		}
		status := store.SponsorshipPending
		if vetted {
			status = store.SponsorshipActive
		}

		sponsorship, err = tx.CreateSponsorship(ctx, store.Sponsorship{
			EventID:       e.ID,
			SponsorUserID: sponsorUserID,
			Slots:         slots,
			Status:        status,
			Note:          note,
		})
		return err
	})
	return sponsorship, err
}

func (s *Service) ApproveSponsorship(ctx context.Context, id, by int64) error {
	if err := s.Store.SetSponsorshipStatus(ctx, id, store.SponsorshipActive, by); err != nil {
		return err
	}
	return s.Store.Record(ctx, &by, store.ActionApproveSponsor, "sponsorship", &id, "")
}

func (s *Service) RevokeSponsorship(ctx context.Context, id, by int64) error {
	err := s.Store.Tx(ctx, func(tx *store.Store) error {
		sponsorship, err := tx.GetSponsorship(ctx, id)
		if err != nil {
			return err
		}
		if sponsorship.Claimed > 0 {
			return ErrNotAllowed
		}
		return tx.SetSponsorshipStatus(ctx, id, store.SponsorshipRevoked, by)
	})
	if err != nil {
		return err
	}
	return s.Store.Record(ctx, &by, store.ActionRevokeSponsor, "sponsorship", &id, "")
}

func (s *Service) AvailableSponsoredSlots(ctx context.Context, eventID int64) (int, []store.Sponsorship, error) {
	active, err := s.Store.ListSponsorships(ctx, eventID, store.SponsorshipActive)
	if err != nil {
		return 0, nil, err
	}
	total := 0
	for _, sponsorship := range active {
		total += sponsorship.Remaining()
	}
	return total, active, nil
}

func (s *Service) SponsorshipFor(ctx context.Context, rsvp store.RSVP) (*store.Sponsorship, error) {
	if rsvp.SponsorshipID == nil {
		return nil, nil
	}
	sponsorship, err := s.Store.GetSponsorship(ctx, *rsvp.SponsorshipID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &sponsorship, nil
}
