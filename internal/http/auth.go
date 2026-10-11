package http

import (
	"net/http"

	"openjoga-server/internal/auth"
	"openjoga-server/internal/service"
	"openjoga-server/internal/store"
)

type mergeOfferView struct {
	DisplayName string `json:"display_name"`
	RSVPCount   int    `json:"rsvp_count"`
}

type meView struct {
	UserID      int64   `json:"user_id,omitempty"`
	DisplayName string  `json:"display_name,omitempty"`
	Pronouns    *string `json:"pronouns,omitempty"`
	Tier        string  `json:"tier"`
	IsAdmin     bool    `json:"is_admin"`
	HasEmail    bool    `json:"has_email"`
	HasPhone    bool    `json:"has_phone"`

	PendingMerge *mergeOfferView `json:"pending_merge,omitempty"`
}

func tierName(t auth.Tier) string {
	switch t {
	case auth.TierVerified:
		return "verified"
	case auth.TierDevice:
		return "device"
	default:
		return "anonymous"
	}
}

func (s *Server) requestSignIn(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email string `json:"email"`
	}
	if err := decode(r, &body); err != nil {
		s.writeErr(w, r, err)
		return
	}

	if err := s.challenges.Request(r.Context(), body.Email, identityFrom(r.Context()).UserID()); err != nil {
		s.writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]bool{"sent": true})
}

func (s *Server) verifyCode(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email string `json:"email"`
		Code  string `json:"code"`
	}
	if err := decode(r, &body); err != nil {
		s.writeErr(w, r, err)
		return
	}

	out, err := s.challenges.VerifyCode(r.Context(), body.Email, body.Code,
		identityFrom(r.Context()).UserID())
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	s.completeSignIn(w, r, out)
}

func (s *Server) verifyLink(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token string `json:"token"`
	}
	if err := decode(r, &body); err != nil {
		s.writeErr(w, r, err)
		return
	}

	out, err := s.challenges.VerifyLink(r.Context(), body.Token,
		identityFrom(r.Context()).UserID())
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	s.completeSignIn(w, r, out)
}

func (s *Server) completeSignIn(w http.ResponseWriter, r *http.Request, out auth.Outcome) {
	ctx := r.Context()
	if err := auth.SetUser(ctx, s.sessions, out.UserID, true); err != nil {
		s.writeErr(w, r, err)
		return
	}

	if claimed, err := s.svc.ClaimInstanceIfUnowned(ctx, out.UserID); err != nil {
		s.writeErr(w, r, err)
		return
	} else if claimed {
		s.log.Info("instance claimed by first sign-in", "user", out.UserID)
	}

	view := meView{Tier: tierName(auth.TierVerified)}
	if u, err := s.store.GetUser(ctx, out.UserID); err == nil {
		view.UserID = u.ID
		view.DisplayName = u.DisplayName
		view.Pronouns = u.Pronouns
		view.IsAdmin = u.IsAdmin()
		view.HasEmail = u.Email != nil
		view.HasPhone = u.Phone != nil
	}

	if out.MergeOffer != nil {
		auth.SetPendingMerge(ctx, s.sessions, out.MergeOffer.FromUserID)
		view.PendingMerge = &mergeOfferView{
			DisplayName: out.MergeOffer.DisplayName,
			RSVPCount:   out.MergeOffer.RSVPCount,
		}
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) resolveMerge(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identity := identityFrom(ctx)
	if err := identity.RequireAccount(); err != nil {
		s.writeErr(w, r, err)
		return
	}

	from := auth.PendingMerge(ctx, s.sessions)
	if from == 0 {
		s.writeErr(w, r, store.ErrNotFound)
		return
	}

	var body struct {
		Accept bool `json:"accept"`
	}
	if err := decode(r, &body); err != nil {
		s.writeErr(w, r, err)
		return
	}

	var err error
	if body.Accept {
		err = s.challenges.ConfirmMerge(ctx, from, identity.UserID())
	} else {
		err = s.challenges.RejectMerge(ctx, from, identity.UserID())
	}
	if err != nil {
		s.writeErr(w, r, err)
		return
	}

	auth.ClearPendingMerge(ctx, s.sessions)
	writeJSON(w, http.StatusOK, map[string]bool{"merged": body.Accept})
}

func (s *Server) redeemClaimLink(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	userID, err := s.svc.RedeemClaimLink(ctx, chiParam(r, "token"))
	if err != nil {
		s.writeErr(w, r, err)
		return
	}

	if current := identityFrom(ctx).UserID(); current != 0 && current != userID {
		if err := s.store.Tx(ctx, func(tx *store.Store) error {
			return tx.MergeUsers(ctx, current, userID)
		}); err != nil {
			s.writeErr(w, r, err)
			return
		}
	}

	if err := auth.SetUser(ctx, s.sessions, userID, false); err != nil {
		s.writeErr(w, r, err)
		return
	}
	s.me(w, r)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if err := auth.Logout(r.Context(), s.sessions); err != nil {
		s.writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) updateMe(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identity := identityFrom(ctx)
	if err := identity.RequireAccount(); err != nil {
		s.writeErr(w, r, err)
		return
	}

	var body struct {
		DisplayName *string `json:"display_name"`
		Pronouns    *string `json:"pronouns"`
	}
	if err := decode(r, &body); err != nil {
		s.writeErr(w, r, err)
		return
	}
	if err := s.svc.UpdateProfile(ctx, identity.UserID(), service.ProfileUpdate{
		DisplayName: body.DisplayName,
		Pronouns:    body.Pronouns,
	}); err != nil {
		s.writeErr(w, r, err)
		return
	}
	s.me(w, r)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identity, err := auth.Resolve(ctx, s.store, auth.UserID(ctx, s.sessions))
	if err != nil {
		s.writeErr(w, r, err)
		return
	}

	view := meView{Tier: tierName(identity.Tier)}
	if identity.User != nil {
		view.UserID = identity.User.ID
		view.DisplayName = identity.User.DisplayName
		view.Pronouns = identity.User.Pronouns
		view.IsAdmin = identity.User.IsAdmin()
		view.HasEmail = identity.User.Email != nil
		view.HasPhone = identity.User.Phone != nil
	}
	if from := auth.PendingMerge(ctx, s.sessions); from != 0 {
		if u, err := s.store.GetUser(ctx, from); err == nil {
			count, _ := s.store.CountRSVPsForUser(ctx, from)
			view.PendingMerge = &mergeOfferView{DisplayName: u.DisplayName, RSVPCount: count}
		}
	}
	writeJSON(w, http.StatusOK, view)
}
