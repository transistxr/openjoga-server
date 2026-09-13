package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"openjoga-server/internal/id"
	"openjoga-server/internal/store"
	"openjoga-server/internal/text"
)

func (s *Service) CreateEvent(ctx context.Context, g store.Group, e store.Event, by int64) (store.Event, error) {
	e.GroupID = g.ID
	e.Slug = id.New()
	e.CreatedBy = &by

	if e.Status == "" {
		e.Status = store.EventScheduled
	}
	if e.Timezone == "" {
		e.Timezone = "Europe/London"
	}
	if e.RSVPPolicy == "" {
		e.RSVPPolicy = DefaultRSVPPolicy(e.CostType)
	}
	if e.CostType == "" {
		e.CostType = store.CostFree
	}

	if e.ShowPaymentPreGame && e.RSVPPolicy == store.PolicyOpen {
		return store.Event{}, ErrPaymentNeedsGate
	}

	created, err := s.Store.CreateEvent(ctx, e)
	if err != nil {
		return store.Event{}, err
	}

	payload := fmt.Sprintf(`{"event_id":%d}`, created.ID)
	if created.RSVPCutoffAt != nil {
		if err := s.Store.Enqueue(ctx, store.JobEventCutoff, payload, *created.RSVPCutoffAt); err != nil {
			return created, err
		}
		if remindAt, ok := reminderTime(*created.RSVPCutoffAt); ok {
			if err := s.Store.Enqueue(ctx, store.JobReminder, payload, remindAt); err != nil {
				return created, err
			}
		}
	}
	if err := s.Store.Enqueue(ctx, store.JobEventComplete, payload, created.EndsAt); err != nil {
		return created, err
	}
	return created, nil
}

func reminderTime(cutoffAt string) (string, bool) {
	cutoff, err := store.ParseTime(cutoffAt)
	if err != nil {
		return "", false
	}
	remindAt := cutoff.Add(-24 * time.Hour)
	if remindAt.Before(time.Now().Add(1 * time.Hour)) {
		return "", false
	}
	return store.FormatTime(remindAt), true
}

func (s *Service) NotifyUser(ctx context.Context, userID int64, kind, title, body string, eventID *int64, url string) error {
	payload, err := json.Marshal(map[string]any{
		"user_id":  userID,
		"type":     kind,
		"title":    title,
		"body":     body,
		"url":      url,
		"event_id": eventID,
	})
	if err != nil {
		return err
	}
	return s.Store.Enqueue(ctx, store.JobSendNotification, string(payload), "")
}

func (s *Service) NotifyPromoted(ctx context.Context, e store.Event, rsvp store.RSVP) error {
	return s.NotifyUser(ctx, rsvp.UserID, store.NotifyPromoted,
		text.TitleYoureIn, text.BodyPromoted(), &e.ID, s.EventURL(e.Slug))
}

func (s *Service) RunCutoffIfDue(ctx context.Context, e store.Event) (string, error) {
	if e.RSVPCutoffAt != nil && *e.RSVPCutoffAt > store.Now() {
		return e.Status, nil
	}
	return s.RunCutoff(ctx, e)
}

func (s *Service) RunCutoff(ctx context.Context, e store.Event) (string, error) {
	if e.Status != store.EventScheduled {
		return e.Status, nil
	}

	capacity, err := s.Store.Capacity(ctx, e.ID)
	if err != nil {
		return "", err
	}
	if e.MinPlayers != nil && capacity.Total() < *e.MinPlayers {
		reason := text.ReasonBelowMinimum
		return store.EventCancelled, s.Cancel(ctx, e, &reason)
	}
	return store.EventConfirmed, s.Confirm(ctx, e)
}

// Confirm locks the game in and writes down what everybody owes.
func (s *Service) Confirm(ctx context.Context, e store.Event) error {
	return s.Store.Tx(ctx, func(tx *store.Store) error {
		going, err := tx.ListRSVPs(ctx, e.ID, store.RSVPGoing)
		if err != nil {
			return err
		}

		slots := 0
		for _, r := range going {
			slots += r.Slots()
		}

		perHead, err := resolvePrice(ctx, tx, e, slots)
		if err != nil {
			return err
		}

		owed := map[int64]int64{}
		coveredBy := map[int64]*int64{}
		claimsPerSponsor := map[int64]int{}

		for _, r := range going {
			sponsored := r.SponsorshipID != nil
			owed[r.UserID] += AmountDue(perHead, len(r.Guests), sponsored)

			if sponsored {
				coveredBy[r.UserID] = r.SponsorshipID
				sponsorship, err := tx.GetSponsorship(ctx, *r.SponsorshipID)
				if err != nil {
					return err
				}
				claimsPerSponsor[sponsorship.SponsorUserID]++
			}
		}
		for sponsorID, claimed := range claimsPerSponsor {
			owed[sponsorID] += perHead * int64(claimed)
		}

		for userID, amount := range owed {
			if err := tx.UpsertPayment(ctx, store.Payment{
				EventID:        e.ID,
				UserID:         userID,
				AmountDuePence: amount,
				SponsorshipID:  coveredBy[userID],
			}); err != nil {
				return err
			}
		}

		if err := recordSurplus(ctx, tx, e, perHead, slots); err != nil {
			return err
		}
		return tx.SetEventStatus(ctx, e.ID, store.EventConfirmed, nil)
	})
}

func resolvePrice(ctx context.Context, tx *store.Store, e store.Event, slots int) (int64, error) {
	switch e.CostType {
	case store.CostFixed:
		if e.CostPerHeadPence == nil {
			return 0, nil
		}
		return *e.CostPerHeadPence, nil

	case store.CostSplit:
		if e.CostPerHeadPence != nil {
			return *e.CostPerHeadPence, nil
		}
		if e.CostTotalPence == nil || slots == 0 {
			return 0, nil
		}
		perHead := ResolvePerHead(*e.CostTotalPence, slots)
		return perHead, tx.SetResolvedPerHead(ctx, e.ID, perHead)

	default:
		return 0, nil
	}
}

func recordSurplus(ctx context.Context, tx *store.Store, e store.Event, perHead int64, slots int) error {
	if e.CostType != store.CostSplit || e.CostTotalPence == nil {
		return nil
	}
	surplus := perHead*int64(slots) - *e.CostTotalPence
	if surplus <= 0 {
		return nil
	}

	g, err := tx.GetGroup(ctx, e.GroupID)
	if err != nil || !g.TrackKitty {
		return err
	}
	return tx.AddKittyEntry(ctx, store.KittyEntry{
		GroupID:     e.GroupID,
		Type:        store.KittySurplus,
		AmountPence: surplus,
		EventID:     &e.ID,
		Note:        ptr(text.NoteRoundingSurplus),
	})
}

func (s *Service) Cancel(ctx context.Context, e store.Event, reason *string) error {
	return s.Store.SetEventStatus(ctx, e.ID, store.EventCancelled, reason)
}

func (s *Service) Complete(ctx context.Context, e store.Event) error {
	if e.Status == store.EventCancelled || e.Status == store.EventCompleted {
		return nil
	}
	if e.Status == store.EventScheduled {
		if err := s.Confirm(ctx, e); err != nil {
			return err
		}
	}
	return s.Store.SetEventStatus(ctx, e.ID, store.EventCompleted, nil)
}

func (s *Service) CancellationMessage(e store.Event, _ string) string {
	var title string
	if e.Title != nil {
		title = *e.Title
	}
	return text.Cancellation(title, text.EventTime(e.StartsAt, e.Timezone), s.EventURL(e.Slug))
}
