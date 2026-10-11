package http

import (
	"net/http"

	"openjoga-server/internal/auth"
	"openjoga-server/internal/service"
	"openjoga-server/internal/store"
)

type communityView struct {
	Name     string `json:"name"`
	Currency string `json:"currency"`
}

func (s *Server) getCommunity(w http.ResponseWriter, r *http.Request) {
	c, err := s.store.GetCommunity(r.Context(), "OpenJoga", s.currency)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, communityView{Name: c.Name, Currency: c.Currency})
}

func (s *Server) updateCommunity(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := identityFrom(ctx).RequireCommunityAdmin(); err != nil {
		s.writeErr(w, r, err)
		return
	}

	var body struct {
		Name     string `json:"name"`
		Currency string `json:"currency"`
	}
	if err := decode(r, &body); err != nil {
		s.writeErr(w, r, err)
		return
	}
	if err := s.store.UpdateCommunity(ctx, body.Name, body.Currency); err != nil {
		s.writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, communityView{Name: body.Name, Currency: body.Currency})
}

type communityMemberView struct {
	UserID    int64   `json:"user_id"`
	Name      string  `json:"name"`
	Pronouns  *string `json:"pronouns,omitempty"`
	Role      string  `json:"role"`
	Reachable bool    `json:"reachable"`

	EmailVerifiedVia *string `json:"email_verified_via,omitempty"`
	PhoneVerifiedVia *string `json:"phone_verified_via,omitempty"`
}

func (s *Server) listCommunityMembers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := identityFrom(ctx).RequireCommunityAdmin(); err != nil {
		s.writeErr(w, r, err)
		return
	}

	users, err := s.store.ListCommunityMembers(ctx)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	ids := make([]int64, len(users))
	for i, u := range users {
		ids[i] = u.ID
	}
	reachable, err := s.store.ReachableSet(ctx, ids)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}

	views := make([]communityMemberView, 0, len(users))
	for _, u := range users {
		views = append(views, communityMemberView{
			UserID: u.ID, Name: u.DisplayName, Pronouns: u.Pronouns,
			Role:             u.CommunityRole,
			Reachable:        reachable[u.ID],
			EmailVerifiedVia: u.EmailVerifiedVia,
			PhoneVerifiedVia: u.PhoneVerifiedVia,
		})
	}
	writeJSON(w, http.StatusOK, views)
}

func (s *Server) addMemberByHand(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identity := identityFrom(ctx)
	if err := identity.RequireCommunityAdmin(); err != nil {
		s.writeErr(w, r, err)
		return
	}

	var body struct {
		Name    string `json:"name"`
		Email   string `json:"email"`
		Phone   string `json:"phone"`
		GroupID *int64 `json:"group_id"`
	}
	if err := decode(r, &body); err != nil {
		s.writeErr(w, r, err)
		return
	}
	if body.Name == "" {
		s.writeErr(w, r, service.ErrInvalid)
		return
	}

	added, err := s.svc.AddMember(ctx, body.Name, body.Email, body.Phone,
		body.GroupID, identity.UserID())
	if err != nil {
		s.writeErr(w, r, err)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"user_id":      added.User.ID,
		"name":         added.User.DisplayName,
		"claim_url":    added.ClaimURL,
		"whatsapp_url": added.WhatsAppURL,
	})
}

func (s *Server) mergeUsers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identity := identityFrom(ctx)
	if err := identity.RequireCommunityAdmin(); err != nil {
		s.writeErr(w, r, err)
		return
	}

	var body struct {
		From int64 `json:"from"`
		To   int64 `json:"to"`
	}
	if err := decode(r, &body); err != nil {
		s.writeErr(w, r, err)
		return
	}
	if err := s.svc.MergeUsers(ctx, body.From, body.To, identity.UserID()); err != nil {
		s.writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"merged": true})
}

type auditView struct {
	Action     string  `json:"action"`
	Actor      *string `json:"actor,omitempty"`
	TargetType *string `json:"target_type,omitempty"`
	TargetID   *int64  `json:"target_id,omitempty"`
	Detail     string  `json:"detail"`
	CreatedAt  string  `json:"created_at"`
}

func (s *Server) listAudit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := identityFrom(ctx).RequireCommunityAdmin(); err != nil {
		s.writeErr(w, r, err)
		return
	}

	entries, err := s.store.ListAudit(ctx, 200)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	views := make([]auditView, 0, len(entries))
	for _, e := range entries {
		views = append(views, auditView{
			Action: e.Action, Actor: e.ActorName, TargetType: e.TargetType,
			TargetID: e.TargetID, Detail: e.Detail, CreatedAt: e.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, views)
}

func (s *Server) setCommunityRole(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identity := identityFrom(ctx)
	if err := identity.RequireVerified(); err != nil {
		s.writeErr(w, r, err)
		return
	}
	if identity.User.CommunityRole != store.RoleOwner {
		s.writeErr(w, r, auth.ErrForbidden)
		return
	}
	userID, err := chiID(r, "uid")
	if err != nil {
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
	if err := s.svc.SetCommunityRole(ctx, userID, body.Role, identity.UserID()); err != nil {
		s.writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"role": body.Role})
}
