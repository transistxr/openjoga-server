package worker

import (
	"context"
	"encoding/json"

	"openjoga-server/internal/notify"
	"openjoga-server/internal/store"
	"openjoga-server/internal/text"
)

type eventPayload struct {
	EventID int64 `json:"event_id"`
}

type notificationPayload struct {
	UserID  int64  `json:"user_id"`
	Type    string `json:"type"`
	Title   string `json:"title"`
	Body    string `json:"body"`
	URL     string `json:"url"`
	EventID *int64 `json:"event_id,omitempty"`
}

func (w *Worker) loadEvent(ctx context.Context, job store.Job) (store.Event, bool, error) {
	var p eventPayload
	if err := json.Unmarshal([]byte(job.Payload), &p); err != nil {
		return store.Event{}, false, err
	}
	e, err := w.store.GetEvent(ctx, p.EventID)
	if err == store.ErrNotFound {
		return store.Event{}, false, nil
	}
	return e, err == nil, err
}

func (w *Worker) handleCutoff(ctx context.Context, job store.Job) error {
	e, ok, err := w.loadEvent(ctx, job)
	if err != nil || !ok {
		return err
	}
	return w.runCutoff(ctx, e)
}

func (w *Worker) runCutoff(ctx context.Context, e store.Event) error {
	status, err := w.svc.RunCutoffIfDue(ctx, e)
	if err != nil {
		return err
	}

	when := text.EventTime(e.StartsAt, e.Timezone)
	switch status {
	case store.EventConfirmed:
		return w.tellPlayers(ctx, e, notify.Message{
			Type:    store.NotifyConfirmed,
			Title:   text.TitleGameOn,
			Body:    text.BodyConfirmed(when),
			EventID: &e.ID,
			URL:     w.svc.EventURL(e.Slug),
		}, store.RSVPGoing)

	case store.EventCancelled:
		return w.tellPlayers(ctx, e, notify.Message{
			Type:    store.NotifyCancelled,
			Title:   text.TitleGameOff,
			Body:    text.BodyCancelledAtCutoff(when),
			EventID: &e.ID,
			URL:     w.svc.EventURL(e.Slug),
		}, store.RSVPGoing, store.RSVPWaitlisted, store.RSVPMaybe)
	}
	return nil
}

func (w *Worker) handleComplete(ctx context.Context, job store.Job) error {
	e, ok, err := w.loadEvent(ctx, job)
	if err != nil || !ok {
		return err
	}
	return w.svc.Complete(ctx, e)
}

func (w *Worker) handleReminder(ctx context.Context, job store.Job) error {
	e, ok, err := w.loadEvent(ctx, job)
	if err != nil || !ok {
		return err
	}
	if e.Status != store.EventScheduled {
		return nil
	}

	capacity, err := w.store.Capacity(ctx, e.ID)
	if err != nil {
		return err
	}
	return w.tellPlayers(ctx, e, notify.Message{
		Type:  store.NotifyReminder,
		Title: text.TitleReminder,
		Body: text.BodyReminder(text.EventTime(e.StartsAt, e.Timezone),
			capacity.Total(), e.MinPlayers),
		EventID: &e.ID,
		URL:     w.svc.EventURL(e.Slug),
	}, store.RSVPGoing, store.RSVPMaybe)
}

func (w *Worker) handleNotification(ctx context.Context, job store.Job) error {
	var p notificationPayload
	if err := json.Unmarshal([]byte(job.Payload), &p); err != nil {
		return err
	}
	return w.notifier.Send(ctx, p.UserID, notify.Message{
		Type:    p.Type,
		Title:   p.Title,
		Body:    p.Body,
		EventID: p.EventID,
		URL:     p.URL,
	})
}

func (w *Worker) tellPlayers(ctx context.Context, e store.Event, m notify.Message, statuses ...string) error {
	rsvps, err := w.store.ListRSVPs(ctx, e.ID, statuses...)
	if err != nil {
		return err
	}
	for _, r := range rsvps {
		if err := w.notifier.Send(ctx, r.UserID, m); err != nil {
			w.log.Warn("notifying player", "user", r.UserID, "event", e.ID, "error", err)
		}
	}

	return nil
}
