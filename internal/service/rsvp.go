package service

import (
	"context"
	"errors"

	"openjoga-server/internal/store"
)

type RSVPResult struct {
	RSVP     store.RSVP
	Capacity store.Capacity

	Sponsorship *store.Sponsorship
	Promoted *store.RSVP
}

func AcceptsChanges(e store.Event) bool {
	return e.Status == store.EventScheduled || e.Status == store.EventConfirmed
}

func (s *Service) SetRSVP(ctx context.Context, e store.Event, userID int64, displayName, status string, claimSponsored bool) (RSVPResult, error) {
	if !AcceptsChanges(e) {
		return RSVPResult{}, ErrClosed
	}

	var result RSVPResult
	err := s.Store.Tx(ctx, func(tx *store.Store) error {
		existing, err := tx.GetRSVP(ctx, e.ID, userID)
		hadSlot := err == nil && existing.Status == store.RSVPGoing
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}

		if displayName != "" {
			user, err := tx.GetUser(ctx, userID)
			if err != nil {
				return err
			}
			if user.DisplayName != displayName {
				if err := tx.SetDisplayName(ctx, userID, displayName); err != nil {
					return err
				}
			}
		}

		next := store.RSVP{
			EventID: e.ID,
			UserID:  userID,
			Status:  status,
		}

		switch status {
		case store.RSVPGoing:
			if !hadSlot {
				capacity, err := tx.Capacity(ctx, e.ID)
				if err != nil {
					return err
				}
				if capacity.Full() {
					next.Status = store.RSVPWaitlisted
					next.WaitlistedAt = ptr(store.Now())
				}
			} else {
				next.WaitlistedAt = existing.WaitlistedAt
				next.SponsorshipID = existing.SponsorshipID
			}

			if next.Status == store.RSVPGoing && claimSponsored && next.SponsorshipID == nil {
				sponsorship, err := tx.NextAvailableSponsorship(ctx, e.ID)
				if errors.Is(err, store.ErrNotFound) {
					return ErrNoSponsored
				}
				if err != nil {
					return err
				}
				next.SponsorshipID = &sponsorship.ID
				result.Sponsorship = &sponsorship
			}

		case store.RSVPWaitlisted:
			next.WaitlistedAt = ptr(store.Now())

		default:
			next.SponsorshipID = nil
		}

		saved, err := tx.UpsertRSVP(ctx, next)
		if err != nil {
			return err
		}
		result.RSVP = saved

		if hadSlot && saved.Status != store.RSVPGoing {
			promoted, err := promoteFromWaitlist(ctx, tx, e)
			if err != nil {
				return err
			}
			result.Promoted = promoted
		}

		result.Capacity, err = tx.Capacity(ctx, e.ID)
		return err
	})
	return result, err
}

func (s *Service) AddGuest(ctx context.Context, e store.Event, g store.Group, userID int64, name *string) (store.Guest, error) {
	if !AcceptsChanges(e) {
		return store.Guest{}, ErrClosed
	}

	var guest store.Guest
	err := s.Store.Tx(ctx, func(tx *store.Store) error {
		rsvp, err := tx.GetRSVP(ctx, e.ID, userID)
		if err != nil {
			return err
		}
		if rsvp.Status != store.RSVPGoing {
			return ErrNotAllowed
		}

		count, err := tx.CountGuests(ctx, rsvp.ID)
		if err != nil {
			return err
		}
		if count >= g.MaxGuestsPerRSVP {
			return ErrGuestLimit
		}

		capacity, err := tx.Capacity(ctx, e.ID)
		if err != nil {
			return err
		}
		if capacity.Full() {
			return ErrFull
		}

		guest, err = tx.AddGuest(ctx, rsvp.ID, name)
		return err
	})
	return guest, err
}

func (s *Service) RemoveGuest(ctx context.Context, e store.Event, guestID int64) (*store.RSVP, error) {
	var promoted *store.RSVP
	err := s.Store.Tx(ctx, func(tx *store.Store) error {
		if err := tx.DeleteGuest(ctx, guestID); err != nil {
			return err
		}
		var err error
		promoted, err = promoteFromWaitlist(ctx, tx, e)
		return err
	})
	return promoted, err
}

func (s *Service) RemoveRSVP(ctx context.Context, e store.Event, rsvpID int64) (*store.RSVP, error) {
	var promoted *store.RSVP
	err := s.Store.Tx(ctx, func(tx *store.Store) error {
		rsvp, err := tx.GetRSVPByID(ctx, rsvpID)
		if err != nil {
			return err
		}
		if rsvp.EventID != e.ID {
			return ErrNotAllowed
		}
		if err := tx.DeleteRSVP(ctx, rsvpID); err != nil {
			return err
		}
		if rsvp.Status == store.RSVPGoing {
			promoted, err = promoteFromWaitlist(ctx, tx, e)
		}
		return err
	})
	return promoted, err
}

// promoteFromWaitlist moves the longest-waiting person into a freed slot.
func promoteFromWaitlist(ctx context.Context, tx *store.Store, e store.Event) (*store.RSVP, error) {
	capacity, err := tx.Capacity(ctx, e.ID)
	if err != nil {
		return nil, err
	}
	if capacity.Max == nil || capacity.Full() {
		return nil, nil
	}

	next, err := tx.NextWaitlisted(ctx, e.ID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	guests, err := tx.CountGuests(ctx, next.ID)
	if err != nil {
		return nil, err
	}
	if capacity.Total()+1+guests > *capacity.Max {
		return nil, nil
	}

	if err := tx.SetRSVPStatus(ctx, next.ID, store.RSVPGoing, nil); err != nil {
		return nil, err
	}
	next.Status = store.RSVPGoing

	if e.Status == store.EventConfirmed && e.CostPerHeadPence != nil {
		if err := tx.UpsertPayment(ctx, store.Payment{
			EventID:        e.ID,
			UserID:         next.UserID,
			AmountDuePence: AmountDue(*e.CostPerHeadPence, guests, next.SponsorshipID != nil),
			SponsorshipID:  next.SponsorshipID,
		}); err != nil {
			return nil, err
		}
	}
	return &next, nil
}
