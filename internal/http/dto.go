package http

import (
	"context"

	"openjoga-server/internal/auth"
	"openjoga-server/internal/service"
	"openjoga-server/internal/store"
	"openjoga-server/internal/text"
)


type costView struct {
	Type     string `json:"type"`
	Currency string `json:"currency"`
	Resolved bool   `json:"resolved"`

	PerHeadPence *int64 `json:"per_head_pence,omitempty"`
	LowPence   *int64 `json:"low_pence,omitempty"`
	HighPence  *int64 `json:"high_pence,omitempty"`
	TotalPence *int64 `json:"total_pence,omitempty"`
}

type guestView struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type attendeeView struct {
	Name string `json:"name"`
	Pronouns *string     `json:"pronouns,omitempty"`
	Guests   []guestView `json:"guests,omitempty"`
	Covered  bool        `json:"covered"`

	RSVPID         *int64 `json:"rsvp_id,omitempty"`
	UserID         *int64 `json:"user_id,omitempty"`
	Reachable      *bool  `json:"reachable,omitempty"`
	Paid           *bool  `json:"paid,omitempty"`
	AmountDuePence *int64 `json:"amount_due_pence,omitempty"`
}

type yourRSVPView struct {
	Status      string `json:"status"`
	Covered     bool   `json:"covered"`
	Guests      int    `json:"guests"`
	CanAddGuest bool   `json:"can_add_guest"`

	PaymentID      *int64  `json:"payment_id,omitempty"`
	AmountDuePence *int64  `json:"amount_due_pence,omitempty"`
	Paid           bool    `json:"paid"`
	Reference      *string `json:"reference,omitempty"`
}

type payeeView struct {
	Kind     string  `json:"kind"`
	Value    string  `json:"value"`
	Label    *string `json:"label,omitempty"`
	DeepLink string  `json:"deep_link,omitempty"`
}

type eventView struct {
	Slug      string  `json:"slug"`
	GroupSlug string  `json:"group_slug"`
	GroupName string  `json:"group_name"`
	Title     *string `json:"title,omitempty"`
	Status    string  `json:"status"`
	StartsAt  string  `json:"starts_at"`
	EndsAt    string  `json:"ends_at"`
	Timezone  string  `json:"timezone"`
	Location  *string `json:"location,omitempty"`

	MinPlayers   *int    `json:"min_players,omitempty"`
	MaxPlayers   *int    `json:"max_players,omitempty"`
	RSVPCutoffAt *string `json:"rsvp_cutoff_at,omitempty"`
	RSVPPolicy   string  `json:"rsvp_policy"`
	CanRSVP      bool    `json:"can_rsvp"`

	GoingCount int  `json:"going_count"`
	GuestCount int  `json:"guest_count"`
	TotalCount int  `json:"total_count"`
	Uncapped   bool `json:"uncapped"`
	SpotsLeft *int `json:"spots_left"`
	Full      bool `json:"full"`

	Cost      costView       `json:"cost"`
	Attendees []attendeeView `json:"attendees"`
	Waitlist  []attendeeView `json:"waitlist,omitempty"`
	You       *yourRSVPView  `json:"you,omitempty"`

	SponsoredSlotsAvailable int `json:"sponsored_slots_available"`

	ShareMessage string `json:"share_message"`

	// Organiser-only.
	Payee               *payeeView `json:"payee,omitempty"`
	CancellationMessage string     `json:"cancellation_message,omitempty"`
	CancelledReason     *string    `json:"cancelled_reason,omitempty"`
}

// Wording and name formatting live in internal/text.
var firstName = text.FirstName

func (s *Server) buildEventView(ctx context.Context, e store.Event, g store.Group, identity auth.Identity, isOrganiser bool) (eventView, error) {
	capacity, err := s.store.Capacity(ctx, e.ID)
	if err != nil {
		return eventView{}, err
	}

	community, err := s.store.GetCommunity(ctx, "OpenJoga", s.currency)
	if err != nil {
		return eventView{}, err
	}

	quote := service.QuoteFor(e)
	view := eventView{
		Slug:         e.Slug,
		GroupSlug:    g.Slug,
		GroupName:    g.Name,
		Title:        e.Title,
		Status:       e.Status,
		StartsAt:     e.StartsAt,
		EndsAt:       e.EndsAt,
		Timezone:     e.Timezone,
		Location:     e.Location,
		MinPlayers:   e.MinPlayers,
		MaxPlayers:   e.MaxPlayers,
		RSVPCutoffAt: e.RSVPCutoffAt,
		RSVPPolicy:   e.RSVPPolicy,
		CanRSVP:      auth.CanRSVP(ctx, s.store, identity, e) == nil && service.AcceptsChanges(e),
		GoingCount:   capacity.Going,
		GuestCount:   capacity.Guests,
		TotalCount:   capacity.Total(),
		Uncapped:     capacity.Max == nil,
		SpotsLeft:    capacity.SpotsLeft(),
		Full:         capacity.Full(),
		Cost: costView{
			Type:         quote.Type,
			Currency:     community.Currency,
			Resolved:     quote.Resolved,
			PerHeadPence: quote.PerHeadPence,
			LowPence:     quote.LowPence,
			HighPence:    quote.HighPence,
			TotalPence:   quote.TotalPence,
		},
		ShareMessage:    s.svc.ShareMessage(e, capacity),
		CancelledReason: e.CancelledReason,

		Attendees: []attendeeView{},
		Waitlist:  []attendeeView{},
	}

	available, _, err := s.svc.AvailableSponsoredSlots(ctx, e.ID)
	if err != nil {
		return eventView{}, err
	}
	view.SponsoredSlotsAvailable = available

	rsvps, err := s.store.ListRSVPs(ctx, e.ID)
	if err != nil {
		return eventView{}, err
	}

	var reachable map[int64]bool
	var owed map[int64]store.Payment
	if isOrganiser {
		ids := make([]int64, 0, len(rsvps))
		for _, r := range rsvps {
			ids = append(ids, r.UserID)
		}
		if reachable, err = s.store.ReachableSet(ctx, ids); err != nil {
			return eventView{}, err
		}
		payments, err := s.store.ListPayments(ctx, e.ID)
		if err != nil {
			return eventView{}, err
		}
		owed = make(map[int64]store.Payment, len(payments))
		for _, p := range payments {
			owed[p.UserID] = p
		}
	}

	for _, r := range rsvps {
		if r.Status == store.RSVPOut {
			continue
		}
		attendee := attendeeView{
			Name:     firstName(r.DisplayName),
			Pronouns: r.Pronouns,
			Covered:  r.SponsorshipID != nil,
		}
		for _, guest := range r.Guests {
			attendee.Guests = append(attendee.Guests, guestView{
				ID:   guest.ID,
				Name: text.GuestLabel(guest.Name, r.DisplayName),
			})
		}

		if isOrganiser {
			attendee.RSVPID = &r.ID
			attendee.UserID = &r.UserID
			if v, ok := reachable[r.UserID]; ok {
				attendee.Reachable = &v
			}
			if p, ok := owed[r.UserID]; ok {
				paid := p.Status == store.PaymentPaid
				attendee.Paid = &paid
				attendee.AmountDuePence = &p.AmountDuePence
			}
		}

		switch r.Status {
		case store.RSVPWaitlisted:
			view.Waitlist = append(view.Waitlist, attendee)
		default:
			view.Attendees = append(view.Attendees, attendee)
		}

		if identity.UserID() != 0 && r.UserID == identity.UserID() {
			you := &yourRSVPView{
				Status:      r.Status,
				Covered:     r.SponsorshipID != nil,
				Guests:      len(r.Guests),
				CanAddGuest: r.Status == store.RSVPGoing && len(r.Guests) < g.MaxGuestsPerRSVP && !capacity.Full(),
			}
			if p, err := s.store.GetPaymentForUser(ctx, e.ID, r.UserID); err == nil {
				you.PaymentID = &p.ID
				you.AmountDuePence = &p.AmountDuePence
				you.Paid = p.Status == store.PaymentPaid
				you.Reference = p.Reference
			}
			view.You = you
		}
	}

	if isOrganiser {
		view.CancellationMessage = s.svc.CancellationMessage(e, e.StartsAt)
	}
	owesAfterTheGame := view.You != nil && view.You.PaymentID != nil && !view.You.Paid &&
		view.You.AmountDuePence != nil && *view.You.AmountDuePence > 0 &&
		e.EndsAt <= store.Now()
	if isOrganiser || e.ShowPaymentPreGame || owesAfterTheGame {
		var due int64
		if view.You != nil && quote.PerHeadPence != nil {
			due = service.AmountDue(*quote.PerHeadPence, view.You.Guests, view.You.Covered)
		}
		if payee := service.PayeeFor(g, due, community.Currency); payee != nil {
			view.Payee = &payeeView{
				Kind: payee.Kind, Value: payee.Value,
				Label: payee.Label, DeepLink: payee.DeepLink,
			}
		}
	}

	return view, nil
}

type debtView struct {
	UserID     int64  `json:"user_id"`
	Name       string `json:"name"`
	Games      int    `json:"games"`
	TotalPence int64  `json:"total_pence"`
	Reachable  bool   `json:"reachable"`
	WhatsAppURL string `json:"whatsapp_url,omitempty"`
}

func (s *Server) buildDebtView(ctx context.Context, d store.Debt) debtView {
	view := debtView{
		UserID: d.UserID, Name: d.UserName, Games: d.Games,
		TotalPence: d.TotalPence, Reachable: d.Reachable,
	}
	u, err := s.store.GetUser(ctx, d.UserID)
	if err == nil && u.Phone != nil {
		view.WhatsAppURL = service.WhatsAppMessageURL(*u.Phone,
			text.DebtChase(u.DisplayName, d.TotalPence, s.currency, d.Games))
	}
	return view
}
