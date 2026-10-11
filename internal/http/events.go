package http

import (
	"context"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"openjoga-server/internal/auth"
	"openjoga-server/internal/service"
	"openjoga-server/internal/store"
	"openjoga-server/internal/text"
)

func chiParam(r *http.Request, name string) string { return chi.URLParam(r, name) }

func chiID(r *http.Request, name string) (int64, error) {
	id, err := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
	if err != nil {
		return 0, service.ErrInvalid
	}
	return id, nil
}

func (s *Server) eventContext(r *http.Request) (store.Event, store.Group, bool, error) {
	ctx := r.Context()

	e, err := s.store.GetEventBySlug(ctx, chiParam(r, "slug"))
	if err != nil {
		return store.Event{}, store.Group{}, false, err
	}
	g, err := s.store.GetGroup(ctx, e.GroupID)
	if err != nil {
		return store.Event{}, store.Group{}, false, err
	}
	organiser := identityFrom(ctx).RequireOrganiser(ctx, s.store, e.GroupID) == nil
	return e, g, organiser, nil
}

func (s *Server) guestInEvent(ctx context.Context, guestID, eventID int64) (store.Guest, store.RSVP, error) {
	guest, err := s.store.GetGuest(ctx, guestID)
	if err != nil {
		return store.Guest{}, store.RSVP{}, err
	}
	host, err := s.store.GetRSVPByID(ctx, guest.RSVPID)
	if err != nil {
		return store.Guest{}, store.RSVP{}, err
	}
	if host.EventID != eventID {
		return store.Guest{}, store.RSVP{}, store.ErrNotFound
	}
	return guest, host, nil
}

func (s *Server) getEvent(w http.ResponseWriter, r *http.Request) {
	e, g, organiser, err := s.eventContext(r)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	view, err := s.buildEventView(r.Context(), e, g, identityFrom(r.Context()), organiser)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) createEvent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	g, identity, err := s.groupContext(r)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	groupID := g.ID
	if err := identity.RequireOrganiser(ctx, s.store, groupID); err != nil {
		s.writeErr(w, r, err)
		return
	}

	var body struct {
		Title              *string `json:"title"`
		StartsAt           string  `json:"starts_at"`
		EndsAt             string  `json:"ends_at"`
		Timezone           string  `json:"timezone"`
		Location           *string `json:"location"`
		MinPlayers         *int    `json:"min_players"`
		MaxPlayers         *int    `json:"max_players"`
		RSVPCutoffAt       *string `json:"rsvp_cutoff_at"`
		RSVPPolicy         string  `json:"rsvp_policy"`
		CostType           string  `json:"cost_type"`
		CostPerHeadPence   *int64  `json:"cost_per_head_pence"`
		CostTotalPence     *int64  `json:"cost_total_pence"`
		ShowPaymentPreGame bool    `json:"show_payment_pre_game"`
	}
	if err := decode(r, &body); err != nil {
		s.writeErr(w, r, err)
		return
	}

	created, err := s.svc.CreateEvent(ctx, g, store.Event{
		Title: body.Title, StartsAt: body.StartsAt, EndsAt: body.EndsAt,
		Timezone: body.Timezone, Location: body.Location,
		MinPlayers: body.MinPlayers, MaxPlayers: body.MaxPlayers,
		RSVPCutoffAt: body.RSVPCutoffAt, RSVPPolicy: body.RSVPPolicy,
		CostType: body.CostType, CostPerHeadPence: body.CostPerHeadPence,
		CostTotalPence: body.CostTotalPence, ShowPaymentPreGame: body.ShowPaymentPreGame,
	}, identity.UserID())
	if err != nil {
		s.writeErr(w, r, err)
		return
	}

	view, err := s.buildEventView(ctx, created, g, identity, true)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, view)
}

func (s *Server) listEvents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	g, identity, err := s.groupContext(r)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	groupID := g.ID

	allowed, err := s.svc.CanListGroupEvents(ctx, g, identity.UserID(), identity.IsCommunityAdmin())
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	if !allowed {
		s.writeErr(w, r, auth.ErrForbidden)
		return
	}

	events, err := s.store.ListEvents(ctx, groupID, r.URL.Query().Get("all") == "")
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	organiser := identity.RequireOrganiser(ctx, s.store, groupID) == nil

	views := make([]eventView, 0, len(events))
	for _, e := range events {
		view, err := s.buildEventView(ctx, e, g, identity, organiser)
		if err != nil {
			s.writeErr(w, r, err)
			return
		}
		views = append(views, view)
	}
	writeJSON(w, http.StatusOK, views)
}

func (s *Server) updateEvent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	e, g, organiser, err := s.eventContext(r)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	if !organiser {
		s.writeErr(w, r, auth.ErrForbidden)
		return
	}

	var body struct {
		Title              *string `json:"title"`
		StartsAt           *string `json:"starts_at"`
		EndsAt             *string `json:"ends_at"`
		Location           *string `json:"location"`
		MinPlayers         *int    `json:"min_players"`
		MaxPlayers         *int    `json:"max_players"`
		RSVPCutoffAt       *string `json:"rsvp_cutoff_at"`
		RSVPPolicy         *string `json:"rsvp_policy"`
		ShowPaymentPreGame *bool   `json:"show_payment_pre_game"`
	}
	if err := decode(r, &body); err != nil {
		s.writeErr(w, r, err)
		return
	}

	if body.Title != nil {
		e.Title = body.Title
	}
	if body.StartsAt != nil {
		e.StartsAt = *body.StartsAt
	}
	if body.EndsAt != nil {
		e.EndsAt = *body.EndsAt
	}
	if body.Location != nil {
		e.Location = body.Location
	}
	if body.MinPlayers != nil {
		e.MinPlayers = body.MinPlayers
	}
	if body.MaxPlayers != nil {
		e.MaxPlayers = body.MaxPlayers
	}
	if body.RSVPCutoffAt != nil {
		e.RSVPCutoffAt = body.RSVPCutoffAt
	}
	if body.RSVPPolicy != nil {
		e.RSVPPolicy = *body.RSVPPolicy
	}
	if body.ShowPaymentPreGame != nil {
		e.ShowPaymentPreGame = *body.ShowPaymentPreGame
	}

	if err := s.store.UpdateEvent(ctx, e); err != nil {
		s.writeErr(w, r, err)
		return
	}
	updated, err := s.store.GetEvent(ctx, e.ID)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	view, err := s.buildEventView(ctx, updated, g, identityFrom(ctx), true)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) cancelEvent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	e, _, organiser, err := s.eventContext(r)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	if !organiser {
		s.writeErr(w, r, auth.ErrForbidden)
		return
	}

	var body struct {
		Reason *string `json:"reason"`
	}
	if err := decode(r, &body); err != nil {
		s.writeErr(w, r, err)
		return
	}
	if err := s.svc.Cancel(ctx, e, body.Reason); err != nil {
		s.writeErr(w, r, err)
		return
	}

	identity := identityFrom(ctx)
	_ = s.store.Record(ctx, nilIfZero(identity.UserID()), store.ActionCancelEvent, "event", &e.ID, "")

	for _, status := range []string{store.RSVPGoing, store.RSVPWaitlisted, store.RSVPMaybe} {
		rsvps, err := s.store.ListRSVPs(ctx, e.ID, status)
		if err != nil {
			continue
		}
		for _, rsvp := range rsvps {
			_ = s.svc.NotifyUser(ctx, rsvp.UserID, store.NotifyCancelled, text.TitleGameOff,
				text.BodyCancelledByOrganiser(text.EventTime(e.StartsAt, e.Timezone)),
				&e.ID, s.svc.EventURL(e.Slug))
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"cancellation_message": s.svc.CancellationMessage(e, e.StartsAt),
	})
}

func (s *Server) setRSVP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	e, g, organiser, err := s.eventContext(r)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}

	var body struct {
		Status         string `json:"status"`
		DisplayName    string `json:"display_name"`
		ClaimSponsored bool   `json:"claim_sponsored"`
	}
	if err := decode(r, &body); err != nil {
		s.writeErr(w, r, err)
		return
	}

	identity := identityFrom(ctx)
	if err := auth.CanRSVP(ctx, s.store, identity, e); err != nil {
		s.writeErr(w, r, err)
		return
	}

	if body.DisplayName != "" {
		name, err := service.NormalizeDisplayName(body.DisplayName)
		if err != nil {
			s.writeErr(w, r, err)
			return
		}
		body.DisplayName = name
	}

	userID := identity.UserID()
	if userID == 0 {
		if body.DisplayName == "" {
			s.writeErr(w, r, service.ErrInvalid)
			return
		}
		created, err := s.store.CreateDeviceUser(ctx, body.DisplayName)
		if err != nil {
			s.writeErr(w, r, err)
			return
		}
		userID = created.ID
		if err := auth.SetUser(ctx, s.sessions, userID, false); err != nil {
			s.writeErr(w, r, err)
			return
		}
	}
	if body.DisplayName == "" {
		body.DisplayName = identity.DisplayName()
	}

	result, err := s.svc.SetRSVP(ctx, e, userID, body.DisplayName, body.Status, body.ClaimSponsored)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	if result.Promoted != nil {
		_ = s.svc.NotifyPromoted(ctx, e, *result.Promoted)
	}

	refreshed, err := auth.Resolve(ctx, s.store, userID)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	view, err := s.buildEventView(ctx, e, g, refreshed, organiser)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) removeRSVP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	e, g, organiser, err := s.eventContext(r)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	if !organiser {
		s.writeErr(w, r, auth.ErrForbidden)
		return
	}
	rsvpID, err := chiID(r, "id")
	if err != nil {
		s.writeErr(w, r, err)
		return
	}

	promoted, err := s.svc.RemoveRSVP(ctx, e, rsvpID)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	identity := identityFrom(ctx)
	_ = s.store.Record(ctx, nilIfZero(identity.UserID()), store.ActionRemoveRSVP, "rsvp", &rsvpID, "")
	if promoted != nil {
		_ = s.svc.NotifyPromoted(ctx, e, *promoted)
	}

	view, err := s.buildEventView(ctx, e, g, identity, true)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) addGuest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	e, g, organiser, err := s.eventContext(r)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	identity := identityFrom(ctx)
	if err := identity.RequireAccount(); err != nil {
		s.writeErr(w, r, err)
		return
	}

	var body struct {
		Name *string `json:"name"`
	}
	if err := decode(r, &body); err != nil {
		s.writeErr(w, r, err)
		return
	}

	if _, err := s.svc.AddGuest(ctx, e, g, identity.UserID(), body.Name); err != nil {
		s.writeErr(w, r, err)
		return
	}
	view, err := s.buildEventView(ctx, e, g, identity, organiser)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) removeGuest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	e, g, organiser, err := s.eventContext(r)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	guestID, err := chiID(r, "id")
	if err != nil {
		s.writeErr(w, r, err)
		return
	}

	identity := identityFrom(ctx)
	_, host, err := s.guestInEvent(ctx, guestID, e.ID)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	if !organiser && host.UserID != identity.UserID() {
		s.writeErr(w, r, auth.ErrForbidden)
		return
	}

	promoted, err := s.svc.RemoveGuest(ctx, e, guestID)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	if promoted != nil {
		_ = s.svc.NotifyPromoted(ctx, e, *promoted)
	}

	view, err := s.buildEventView(ctx, e, g, identity, organiser)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) markAttendance(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	e, _, organiser, err := s.eventContext(r)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	if !organiser {
		s.writeErr(w, r, auth.ErrForbidden)
		return
	}

	var body struct {
		RSVPID   *int64 `json:"rsvp_id"`
		GuestID  *int64 `json:"guest_id"`
		Attended bool   `json:"attended"`
	}
	if err := decode(r, &body); err != nil {
		s.writeErr(w, r, err)
		return
	}

	switch {
	case body.RSVPID != nil:
		rsvp, err := s.store.GetRSVPByID(ctx, *body.RSVPID)
		if err != nil {
			s.writeErr(w, r, err)
			return
		}
		if rsvp.EventID != e.ID {
			s.writeErr(w, r, auth.ErrForbidden)
			return
		}
		err = s.store.MarkAttendance(ctx, *body.RSVPID, body.Attended)
		if err != nil {
			s.writeErr(w, r, err)
			return
		}
	case body.GuestID != nil:
		if _, _, err := s.guestInEvent(ctx, *body.GuestID, e.ID); err != nil {
			s.writeErr(w, r, err)
			return
		}
		if err := s.store.MarkGuestAttendance(ctx, *body.GuestID, body.Attended); err != nil {
			s.writeErr(w, r, err)
			return
		}
	default:
		s.writeErr(w, r, service.ErrInvalid)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func nilIfZero[T comparable](v T) *T {
	var zero T
	if v == zero {
		return nil
	}
	return &v
}

func (s *Server) listMyEvents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identity := identityFrom(ctx)
	if err := identity.RequireAccount(); err != nil {
		s.writeErr(w, r, err)
		return
	}

	events, err := s.store.ListEventsForUser(ctx, identity.UserID(),
		r.URL.Query().Get("past") != "")
	if err != nil {
		s.writeErr(w, r, err)
		return
	}

	groups := map[int64]store.Group{}
	views := make([]eventView, 0, len(events))
	for _, e := range events {
		g, ok := groups[e.GroupID]
		if !ok {
			if g, err = s.store.GetGroup(ctx, e.GroupID); err != nil {
				s.writeErr(w, r, err)
				return
			}
			groups[e.GroupID] = g
		}
		organiser := identity.RequireOrganiser(ctx, s.store, e.GroupID) == nil
		view, err := s.buildEventView(ctx, e, g, identity, organiser)
		if err != nil {
			s.writeErr(w, r, err)
			return
		}
		views = append(views, view)
	}
	writeJSON(w, http.StatusOK, views)
}
