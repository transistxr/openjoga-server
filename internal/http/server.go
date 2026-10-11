package http

import (
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/justinas/nosurf"

	"openjoga-server/internal/auth"
	"openjoga-server/internal/notify"
	"openjoga-server/internal/service"
	"openjoga-server/internal/store"
	"openjoga-server/internal/text"
)

type Server struct {
	store      *store.Store
	svc        *service.Service
	sessions   *scs.SessionManager
	challenges *auth.Challenges
	pusher     *notify.Pusher
	log        *slog.Logger

	webOrigin  string
	currency   string
	dev        bool
	trustProxy bool
}

type Options struct {
	Store      *store.Store
	Service    *service.Service
	Sessions   *scs.SessionManager
	Challenges *auth.Challenges
	Pusher     *notify.Pusher
	Log        *slog.Logger
	WebOrigin  string
	Currency   string
	Dev        bool
	TrustProxy bool
}

func NewServer(o Options) *Server {
	return &Server{
		store: o.Store, svc: o.Service, sessions: o.Sessions,
		challenges: o.Challenges, pusher: o.Pusher, log: o.Log,
		webOrigin: o.WebOrigin, currency: o.Currency, dev: o.Dev,
		trustProxy: o.TrustProxy,
	}
}

func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()

	reads := newRateLimiter(300, time.Minute)
	writes := newRateLimiter(60, time.Minute)
	logins := newRateLimiter(10, 15*time.Minute)

	r.Use(withRequestID, s.recoverPanics, s.withCORS, securityHeaders)
	r.Use(s.sessions.LoadAndSave, s.withIdentity)
	r.Use(s.limit(reads))

	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Get("/csrf", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"token": nosurf.Token(r)})
	})

	r.Group(func(r chi.Router) {
		r.Use(s.limit(logins))
		r.Post("/auth/request", s.requestSignIn)
		r.Post("/auth/verify", s.verifyCode)
		r.Post("/auth/callback", s.verifyLink)
		r.Post("/claim/{token}", s.redeemClaimLink)
	})

	r.Post("/auth/merge", s.resolveMerge)
	r.Post("/auth/logout", s.logout)
	r.Get("/auth/me", s.me)
	r.Patch("/auth/me", s.updateMe)

	r.Group(func(r chi.Router) {
		r.Use(s.limit(writes))

		// Community
		r.Get("/community", s.getCommunity)
		r.Patch("/community", s.updateCommunity)
		r.Get("/community/members", s.listCommunityMembers)
		r.Post("/community/members", s.addMemberByHand)
		r.Patch("/community/members/{uid}", s.setCommunityRole)
		r.Post("/community/merge", s.mergeUsers)
		r.Get("/community/audit", s.listAudit)

		// Groups
		r.Get("/groups", s.listGroups)
		r.Post("/groups", s.createGroup)
		r.Get("/groups/{gslug}", s.getGroup)
		r.Patch("/groups/{gslug}", s.updateGroup)
		r.Get("/groups/{gslug}/members", s.listGroupMembers)
		r.Post("/groups/{gslug}/members/{uid}/approve", s.approveMember)
		r.Patch("/groups/{gslug}/members/{uid}", s.setGroupRole)
		r.Delete("/groups/{gslug}/members/{uid}", s.removeMember)
		r.Get("/groups/{gslug}/invites", s.listInvites)
		r.Post("/groups/{gslug}/invites", s.createInvite)
		r.Delete("/invites/{id}", s.revokeInvite)
		r.Post("/join/{token}", s.redeemInvite)
		r.Get("/groups/{gslug}/events", s.listEvents)
		r.Post("/groups/{gslug}/events", s.createEvent)
		r.Get("/groups/{gslug}/kitty", s.getKitty)
		r.Post("/groups/{gslug}/kitty/entries", s.addKittyEntry)
		r.Get("/groups/{gslug}/debts", s.listDebts)

		// Events
		r.Get("/events/{slug}", s.getEvent)
		r.Patch("/events/{slug}", s.updateEvent)
		r.Post("/events/{slug}/cancel", s.cancelEvent)
		r.Post("/events/{slug}/rsvp", s.setRSVP)
		r.Delete("/events/{slug}/rsvps/{id}", s.removeRSVP)
		r.Post("/events/{slug}/guests", s.addGuest)
		r.Delete("/events/{slug}/guests/{id}", s.removeGuest)
		r.Get("/events/{slug}/payments", s.listEventPayments)
		r.Post("/events/{slug}/attendance", s.markAttendance)
		r.Get("/events/{slug}/sponsorships", s.listSponsorships)
		r.Post("/events/{slug}/sponsorships", s.offerSponsorship)
		r.Post("/sponsorships/{id}/approve", s.approveSponsorship)
		r.Delete("/sponsorships/{id}", s.revokeSponsorship)
		r.Patch("/payments/{id}", s.markPaid)

		// Me
		r.Get("/me/events", s.listMyEvents)
		r.Get("/me/notifications", s.listNotifications)
		r.Post("/me/notifications/read", s.markNotificationsRead)
		r.Patch("/me/notification-preferences", s.setPreference)
		r.Get("/me/push-key", s.pushKey)
		r.Post("/me/push-subscription", s.subscribePush)
		r.Delete("/me/push-subscription", s.unsubscribePush)
	})

	return s.csrf(r)
}

func (s *Server) csrf(next http.Handler) http.Handler {
	handler := nosurf.New(next)
	handler.SetBaseCookie(http.Cookie{
		Path:     "/",
		HttpOnly: false,
		Secure:   !s.dev,
		SameSite: http.SameSiteLaxMode,
	})
	handler.SetFailureHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusForbidden, errorBody{Error: text.ErrBadCSRF})
	}))

	handler.SetIsTLSFunc(func(r *http.Request) bool {
		if r.TLS != nil {
			return true
		}
		return r.Header.Get("X-Forwarded-Proto") == "https"
	})

	allowed, err := url.Parse(s.webOrigin)
	if err == nil && s.webOrigin != "" {
		handler.SetIsAllowedOriginFunc(func(u *url.URL) bool {
			return u.Scheme == allowed.Scheme && u.Host == allowed.Host
		})
	}
	return handler
}
