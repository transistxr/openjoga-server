package http

import (
	"net/http"

	"openjoga-server/internal/auth"
	"openjoga-server/internal/service"
	"openjoga-server/internal/store"
	"openjoga-server/internal/text"
)

type paymentView struct {
	ID             int64  `json:"id"`
	UserID         int64  `json:"user_id"`
	Name           string `json:"name"`
	AmountDuePence int64  `json:"amount_due_pence"`
	Status         string `json:"status"`
	Covered        bool   `json:"covered"`
	Reference *string `json:"reference,omitempty"`
}

func (s *Server) listEventPayments(w http.ResponseWriter, r *http.Request) {
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

	payments, err := s.store.ListPayments(ctx, e.ID)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	community, err := s.store.GetCommunity(ctx, "OpenJoga", s.currency)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}

	views := make([]paymentView, 0, len(payments))
	var outstanding int64
	for _, p := range payments {
		if p.Status == store.PaymentOwed {
			outstanding += p.AmountDuePence
		}
		views = append(views, paymentView{
			ID: p.ID, UserID: p.UserID, Name: firstName(p.UserName),
			AmountDuePence: p.AmountDuePence, Status: p.Status, Covered: p.Covered(),
			Reference: p.Reference,
		})
	}

	body := map[string]any{
		"payments":          views,
		"outstanding_pence": outstanding,
		"currency":          community.Currency,
	}
	if payee := service.PayeeFor(g, 0, community.Currency); payee != nil {
		body["payee"] = payeeView{
			Kind: payee.Kind, Value: payee.Value,
			Label: payee.Label, DeepLink: payee.DeepLink,
		}
	}
	writeJSON(w, http.StatusOK, body)
}

func (s *Server) markPaid(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	paymentID, err := chiID(r, "id")
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	payment, err := s.store.GetPayment(ctx, paymentID)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	e, err := s.store.GetEvent(ctx, payment.EventID)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}

	identity := identityFrom(ctx)
	organiser := identity.RequireOrganiser(ctx, s.store, e.GroupID) == nil

	var body struct {
		Status string `json:"status"`
		Reference string `json:"reference"`
	}
	if err := decode(r, &body); err != nil {
		s.writeErr(w, r, err)
		return
	}

	selfMarking := false
	switch {
	case organiser:
	case payment.UserID == identity.UserID() && body.Status == store.PaymentPaid:
		selfMarking = true
	default:
		s.writeErr(w, r, auth.ErrForbidden)
		return
	}

	if err := s.svc.MarkPaid(ctx, paymentID, identity.UserID(), body.Status,
		body.Reference, selfMarking); err != nil {
		s.writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": body.Status})
}

func (s *Server) listDebts(w http.ResponseWriter, r *http.Request) {
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

	debts, err := s.svc.Debts(ctx, &groupID)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	views := make([]debtView, 0, len(debts))
	for _, d := range debts {
		views = append(views, s.buildDebtView(ctx, d))
	}
	writeJSON(w, http.StatusOK, views)
}

// --- Kitty ----------------------------------------------------------------

type kittyEntryView struct {
	Type        string  `json:"type"`
	AmountPence int64   `json:"amount_pence"`
	Note        *string `json:"note,omitempty"`
	CreatedAt   string  `json:"created_at"`
}

func (s *Server) getKitty(w http.ResponseWriter, r *http.Request) {
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

	entries, err := s.store.ListKittyEntries(ctx, groupID)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	balance, err := s.store.KittyBalance(ctx, groupID)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}

	views := make([]kittyEntryView, 0, len(entries))
	for _, e := range entries {
		views = append(views, kittyEntryView{
			Type: e.Type, AmountPence: e.AmountPence, Note: e.Note, CreatedAt: e.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"balance_pence": balance,
		"entries":       views,
	})
}

func (s *Server) addKittyEntry(w http.ResponseWriter, r *http.Request) {
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
		Type        string  `json:"type"`
		AmountPence int64   `json:"amount_pence"`
		UserID      *int64  `json:"user_id"`
		EventID     *int64  `json:"event_id"`
		Note        *string `json:"note"`
	}
	if err := decode(r, &body); err != nil {
		s.writeErr(w, r, err)
		return
	}

	if err := s.svc.AddKittyEntry(ctx, store.KittyEntry{
		GroupID: groupID, Type: body.Type, AmountPence: body.AmountPence,
		UserID: body.UserID, EventID: body.EventID, Note: body.Note,
	}, identity.UserID()); err != nil {
		s.writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]bool{"ok": true})
}

// --- Sponsorships ---------------------------------------------------------

type sponsorshipView struct {
	ID        int64   `json:"id"`
	Sponsor   string  `json:"sponsor"`
	Slots     int     `json:"slots"`
	Claimed   int     `json:"claimed"`
	Remaining int     `json:"remaining"`
	Status    string  `json:"status"`
	Note      *string `json:"note,omitempty"`
}

func (s *Server) listSponsorships(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	e, _, organiser, err := s.eventContext(r)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}

	status := store.SponsorshipActive
	if organiser {
		status = ""
	}
	sponsorships, err := s.store.ListSponsorships(ctx, e.ID, status)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}

	views := make([]sponsorshipView, 0, len(sponsorships))
	for _, sp := range sponsorships {
		views = append(views, sponsorshipView{
			ID: sp.ID, Sponsor: firstName(sp.SponsorName), Slots: sp.Slots,
			Claimed: sp.Claimed, Remaining: sp.Remaining(), Status: sp.Status, Note: sp.Note,
		})
	}
	writeJSON(w, http.StatusOK, views)
}

func (s *Server) offerSponsorship(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	e, g, _, err := s.eventContext(r)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	identity := identityFrom(ctx)

	if err := identity.RequireVerified(); err != nil {
		s.writeErr(w, r, err)
		return
	}

	var body struct {
		Slots int     `json:"slots"`
		Note  *string `json:"note"`
	}
	if err := decode(r, &body); err != nil {
		s.writeErr(w, r, err)
		return
	}

	sponsorship, err := s.svc.OfferSponsorship(ctx, e, g, identity.UserID(), body.Slots, body.Note)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}

	if sponsorship.Status == store.SponsorshipPending {
		members, _ := s.store.ListMembers(ctx, e.GroupID, store.MemberActive)
		for _, m := range members {
			if m.IsOrganiser() {
				_ = s.svc.NotifyUser(ctx, m.UserID, store.NotifySponsorship,
					text.TitleSponsorOffer, text.BodySponsorOffer(identity.DisplayName()),
					&e.ID, s.svc.EventURL(e.Slug))
			}
		}
	}

	writeJSON(w, http.StatusCreated, sponsorshipView{
		ID: sponsorship.ID, Sponsor: firstName(sponsorship.SponsorName),
		Slots: sponsorship.Slots, Claimed: sponsorship.Claimed,
		Remaining: sponsorship.Remaining(), Status: sponsorship.Status, Note: sponsorship.Note,
	})
}

func (s *Server) approveSponsorship(w http.ResponseWriter, r *http.Request) {
	s.sponsorshipAction(w, r, true)
}

func (s *Server) revokeSponsorship(w http.ResponseWriter, r *http.Request) {
	s.sponsorshipAction(w, r, false)
}

func (s *Server) sponsorshipAction(w http.ResponseWriter, r *http.Request, approve bool) {
	ctx := r.Context()
	id, err := chiID(r, "id")
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	sponsorship, err := s.store.GetSponsorship(ctx, id)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	e, err := s.store.GetEvent(ctx, sponsorship.EventID)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}

	identity := identityFrom(ctx)
	if err := identity.RequireOrganiser(ctx, s.store, e.GroupID); err != nil {
		s.writeErr(w, r, err)
		return
	}

	if approve {
		err = s.svc.ApproveSponsorship(ctx, id, identity.UserID())
	} else {
		err = s.svc.RevokeSponsorship(ctx, id, identity.UserID())
	}
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
