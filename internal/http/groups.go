package http

import (
	"net/http"
	"strings"

	"openjoga-server/internal/auth"
	"openjoga-server/internal/service"
	"openjoga-server/internal/store"
	"openjoga-server/internal/text"
)

func (s *Server) groupContext(r *http.Request) (store.Group, auth.Identity, error) {
	ctx := r.Context()
	identity := identityFrom(ctx)

	g, err := s.store.GetGroupBySlug(ctx, chiParam(r, "gslug"))
	if err != nil {
		return store.Group{}, identity, err
	}
	visible, err := s.svc.CanSeeGroup(ctx, g, identity.UserID(), identity.IsCommunityAdmin())
	if err != nil {
		return store.Group{}, identity, err
	}
	if !visible {
		return store.Group{}, identity, store.ErrNotFound
	}
	return g, identity, nil
}

type groupView struct {
	Slug               string  `json:"slug"`
	Name               string  `json:"name"`
	Sport              *string `json:"sport,omitempty"`
	Description        *string `json:"description,omitempty"`
	Visibility         string  `json:"visibility"`
	DefaultLocation    *string `json:"default_location,omitempty"`
	MaxGuestsPerRSVP   int     `json:"max_guests_per_rsvp"`
	MaxSlotsPerSponsor int     `json:"max_slots_per_sponsor"`
	TrackKitty         bool    `json:"track_kitty"`

	Payee *payeeView `json:"payee,omitempty"`
}

func (s *Server) groupToView(g store.Group, organiser bool) groupView {
	view := groupView{
		Slug: g.Slug, Name: g.Name, Sport: g.Sport, Description: g.Description,
		Visibility: g.Visibility, DefaultLocation: g.DefaultLocation,
		MaxGuestsPerRSVP: g.MaxGuestsPerRSVP, MaxSlotsPerSponsor: g.MaxSlotsPerSponsor,
		TrackKitty: g.TrackKitty,
	}
	if organiser && g.HasPaymentDetails() {
		view.Payee = &payeeView{Kind: *g.PaymentKind, Value: *g.PaymentValue, Label: g.PaymentLabel}
	}
	return view
}

func (s *Server) listGroups(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identity := identityFrom(ctx)

	groups, err := s.store.ListGroups(ctx, identity.UserID(), identity.IsCommunityAdmin())
	if err != nil {
		s.writeErr(w, r, err)
		return
	}

	views := make([]groupView, 0, len(groups))
	for _, g := range groups {
		views = append(views, s.groupToView(g,
			identity.RequireOrganiser(ctx, s.store, g.ID) == nil))
	}
	writeJSON(w, http.StatusOK, views)
}

func (s *Server) getGroup(w http.ResponseWriter, r *http.Request) {
	g, identity, err := s.groupContext(r)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	organiser := identity.RequireOrganiser(r.Context(), s.store, g.ID) == nil
	writeJSON(w, http.StatusOK, s.groupToView(g, organiser))
}

func (s *Server) createGroup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identity := identityFrom(ctx)
	if err := identity.RequireCommunityAdmin(); err != nil {
		s.writeErr(w, r, err)
		return
	}

	var body struct {
		Name               string  `json:"name"`
		Sport              *string `json:"sport"`
		Description        *string `json:"description"`
		Visibility         string  `json:"visibility"`
		DefaultLocation    *string `json:"default_location"`
		MaxGuestsPerRSVP   *int    `json:"max_guests_per_rsvp"`
		MaxSlotsPerSponsor *int    `json:"max_slots_per_sponsor"`
		TrackKitty         bool    `json:"track_kitty"`
	}
	if err := decode(r, &body); err != nil {
		s.writeErr(w, r, err)
		return
	}
	if body.Name == "" {
		s.writeErr(w, r, service.ErrInvalid)
		return
	}

	g := store.Group{
		Name: body.Name, Sport: body.Sport, Description: body.Description,
		Visibility: body.Visibility, DefaultLocation: body.DefaultLocation,
		TrackKitty: body.TrackKitty, MaxGuestsPerRSVP: 3, MaxSlotsPerSponsor: 4,
	}
	if body.MaxGuestsPerRSVP != nil {
		g.MaxGuestsPerRSVP = *body.MaxGuestsPerRSVP
	}
	if body.MaxSlotsPerSponsor != nil {
		g.MaxSlotsPerSponsor = *body.MaxSlotsPerSponsor
	}

	created, err := s.svc.CreateGroup(ctx, g, identity.UserID())
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, s.groupToView(created, true))
}

func (s *Server) updateGroup(w http.ResponseWriter, r *http.Request) {
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
		Name               *string `json:"name"`
		Sport              *string `json:"sport"`
		Description        *string `json:"description"`
		Visibility         *string `json:"visibility"`
		DefaultLocation    *string `json:"default_location"`
		MaxGuestsPerRSVP   *int    `json:"max_guests_per_rsvp"`
		MaxSlotsPerSponsor *int    `json:"max_slots_per_sponsor"`
		TrackKitty         *bool   `json:"track_kitty"`
		PaymentKind        *string `json:"payment_kind"`
		PaymentValue       *string `json:"payment_value"`
		PaymentLabel       *string `json:"payment_label"`
	}
	if err := decode(r, &body); err != nil {
		s.writeErr(w, r, err)
		return
	}

	if body.Name != nil {
		g.Name = *body.Name
	}
	if body.Sport != nil {
		g.Sport = body.Sport
	}
	if body.Description != nil {
		g.Description = body.Description
	}
	if body.Visibility != nil {
		g.Visibility = *body.Visibility
	}
	if body.DefaultLocation != nil {
		g.DefaultLocation = body.DefaultLocation
	}
	if body.MaxGuestsPerRSVP != nil {
		g.MaxGuestsPerRSVP = *body.MaxGuestsPerRSVP
	}
	if body.MaxSlotsPerSponsor != nil {
		g.MaxSlotsPerSponsor = *body.MaxSlotsPerSponsor
	}
	if body.TrackKitty != nil {
		g.TrackKitty = *body.TrackKitty
	}

	paymentChanged := body.PaymentKind != nil || body.PaymentValue != nil || body.PaymentLabel != nil
	if body.PaymentKind != nil {
		g.PaymentKind = body.PaymentKind
	}
	if body.PaymentValue != nil {
		g.PaymentValue = body.PaymentValue
	}
	if body.PaymentLabel != nil {
		g.PaymentLabel = body.PaymentLabel
	}

	if err := s.store.UpdateGroup(ctx, g); err != nil {
		s.writeErr(w, r, err)
		return
	}
	if paymentChanged {
		_ = s.store.Record(ctx, nilIfZero(identity.UserID()),
			store.ActionUpdatePaymentInfo, "group", &g.ID, "")
	}
	writeJSON(w, http.StatusOK, s.groupToView(g, true))
}

// --- Members --------------------------------------------------------------

type memberView struct {
	UserID    int64   `json:"user_id"`
	Name      string  `json:"name"`
	Pronouns  *string `json:"pronouns,omitempty"`
	Role      string  `json:"role"`
	Status    string  `json:"status"`
	JoinedAt  string  `json:"joined_at"`
	Reachable bool    `json:"reachable"`
}

func (s *Server) listGroupMembers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	g, identity, err := s.groupContext(r)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	groupID := g.ID
	organiser := identity.RequireOrganiser(ctx, s.store, groupID) == nil
	if !organiser {
		if err := identity.RequireVerified(); err != nil {
			s.writeErr(w, r, err)
			return
		}
		member, err := s.store.IsActiveMember(ctx, groupID, identity.UserID())
		if err != nil || !member {
			s.writeErr(w, r, auth.ErrForbidden)
			return
		}
	}

	status := store.MemberActive
	if organiser && r.URL.Query().Get("status") == store.MemberPending {
		status = store.MemberPending
	}
	members, err := s.store.ListMembers(ctx, groupID, status)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}

	ids := make([]int64, len(members))
	for i, m := range members {
		ids[i] = m.UserID
	}
	reachable, err := s.store.ReachableSet(ctx, ids)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}

	views := make([]memberView, 0, len(members))
	for _, m := range members {
		views = append(views, memberView{
			UserID: m.UserID, Name: firstName(m.UserName), Pronouns: m.UserPronouns,
			Role: m.Role, Status: m.Status, JoinedAt: m.JoinedAt,
			Reachable: reachable[m.UserID],
		})
	}
	writeJSON(w, http.StatusOK, views)
}

func (s *Server) approveMember(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	g, identity, err := s.groupContext(r)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	groupID := g.ID
	userID, err := chiID(r, "uid")
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	if err := identity.RequireOrganiser(ctx, s.store, groupID); err != nil {
		s.writeErr(w, r, err)
		return
	}
	if err := s.svc.ApproveMember(ctx, groupID, userID, identity.UserID()); err != nil {
		s.writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"approved": true})
}

func (s *Server) removeMember(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	g, identity, err := s.groupContext(r)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	groupID := g.ID
	userID, err := chiID(r, "uid")
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	if err := identity.RequireOrganiser(ctx, s.store, groupID); err != nil {
		s.writeErr(w, r, err)
		return
	}
	if err := s.svc.RemoveMember(ctx, groupID, userID, identity.UserID()); err != nil {
		s.writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"removed": true})
}

// --- Invites --------------------------------------------------------------

type inviteView struct {
	ID               int64   `json:"id"`
	Kind             string  `json:"kind"`
	Role             string  `json:"role"`
	RequiresApproval bool    `json:"requires_approval"`
	Label            *string `json:"label,omitempty"`
	ExpiresAt        *string `json:"expires_at,omitempty"`
	Uses             int     `json:"uses"`

	URL string `json:"url,omitempty"`
}

func (s *Server) createInvite(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	g, identity, err := s.groupContext(r)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	groupID := g.ID

	if err := identity.RequireVerified(); err != nil {
		s.writeErr(w, r, err)
		return
	}
	member, err := s.store.IsActiveMember(ctx, groupID, identity.UserID())
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	if !member && !identity.IsCommunityAdmin() {
		s.writeErr(w, r, auth.ErrForbidden)
		return
	}
	isOrganiser := identity.RequireOrganiser(ctx, s.store, groupID) == nil

	var body struct {
		Kind  string  `json:"kind"`
		Role  string  `json:"role"`
		Label *string `json:"label"`
	}
	if err := decode(r, &body); err != nil {
		s.writeErr(w, r, err)
		return
	}

	invite, token, err := s.svc.CreateInvite(ctx, store.Invite{
		Kind: body.Kind, Scope: store.ScopeGroup, GroupID: &groupID,
		Role: body.Role, Label: body.Label,
	}, identity.UserID(), isOrganiser)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}

	writeJSON(w, http.StatusCreated, inviteView{
		ID: invite.ID, Kind: invite.Kind, Role: invite.Role,
		RequiresApproval: invite.RequiresApproval, Label: invite.Label,
		ExpiresAt: invite.ExpiresAt, Uses: invite.Uses,
		URL: s.svc.InviteURL(token),
	})
}

func (s *Server) listInvites(w http.ResponseWriter, r *http.Request) {
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
	invites, err := s.store.ListInvites(ctx, &groupID)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}

	views := make([]inviteView, 0, len(invites))
	for _, i := range invites {
		views = append(views, inviteView{
			ID: i.ID, Kind: i.Kind, Role: i.Role,
			RequiresApproval: i.RequiresApproval, Label: i.Label,
			ExpiresAt: i.ExpiresAt, Uses: i.Uses,
		})
	}
	writeJSON(w, http.StatusOK, views)
}

func (s *Server) revokeInvite(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	inviteID, err := chiID(r, "id")
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	invite, err := s.store.GetInvite(ctx, inviteID)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	identity := identityFrom(ctx)
	if invite.GroupID != nil {
		if err := identity.RequireOrganiser(ctx, s.store, *invite.GroupID); err != nil {
			s.writeErr(w, r, err)
			return
		}
	} else if err := identity.RequireCommunityAdmin(); err != nil {
		s.writeErr(w, r, err)
		return
	}

	if err := s.svc.RevokeInvite(ctx, inviteID, identity.UserID()); err != nil {
		s.writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"revoked": true})
}

func (s *Server) redeemInvite(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identity := identityFrom(ctx)

	var body struct {
		DisplayName string `json:"display_name"`
		Email       string `json:"email"`
		Phone       string `json:"phone"`
	}
	if err := decode(r, &body); err != nil {
		s.writeErr(w, r, err)
		return
	}

	details := service.JoinDetails{
		Email: strings.TrimSpace(body.Email),
		Phone: strings.TrimSpace(body.Phone),
	}

	userID := identity.UserID()
	if userID == 0 {
		name, err := service.NormalizeDisplayName(body.DisplayName)
		if err != nil {
			s.writeErr(w, r, err)
			return
		}
		created, err := s.store.CreateDeviceUser(ctx, name)
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

	if !identity.IsVerified() && !details.HasContact() {
		s.writeErr(w, r, auth.ErrNeedContact)
		return
	}

	out, err := s.svc.RedeemInvite(ctx, chiParam(r, "token"), userID, details)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}

	if out.Status == store.MemberPending && out.GroupID != nil {
		organisers, _ := s.store.ListMembers(ctx, *out.GroupID, store.MemberActive)
		for _, m := range organisers {
			if m.IsOrganiser() {
				_ = s.svc.NotifyUser(ctx, m.UserID, store.NotifyJoinRequest,
					text.TitleJoinRequest, text.BodyJoinRequest(identity.DisplayName()),
					nil, s.svc.BaseURL)
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"group_id": out.GroupID,
		"status":   out.Status,
		"attested": out.Attested,
	})
}

func (s *Server) setGroupRole(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	g, identity, err := s.groupContext(r)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	userID, err := chiID(r, "uid")
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	if err := identity.RequireOrganiser(ctx, s.store, g.ID); err != nil {
		s.writeErr(w, r, err)
		return
	}

	var body struct {
		Role string `json:"role"`
	}
	if err := decode(r, &body); err != nil {
		s.writeErr(w, r, err)
		return
	}
	if err := s.svc.SetGroupRole(ctx, g.ID, userID, body.Role,
		identity.UserID(), identity.IsCommunityAdmin()); err != nil {
		s.writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"role": body.Role})
}
