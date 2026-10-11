package http

import (
	"net/http"
	"strings"

	"openjoga-server/internal/store"
)

type notificationView struct {
	ID        int64   `json:"id"`
	Type      string  `json:"type"`
	Title     string  `json:"title"`
	Body      *string `json:"body,omitempty"`
	ReadAt    *string `json:"read_at,omitempty"`
	CreatedAt string  `json:"created_at"`
}

func (s *Server) listNotifications(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identity := identityFrom(ctx)
	if err := identity.RequireAccount(); err != nil {
		s.writeErr(w, r, err)
		return
	}

	notifications, err := s.store.ListNotifications(ctx, identity.UserID(), 50)
	if err != nil {
		s.writeErr(w, r, err)
		return
	}
	views := make([]notificationView, 0, len(notifications))
	for _, n := range notifications {
		views = append(views, notificationView{
			ID: n.ID, Type: n.Type, Title: n.Title, Body: n.Body,
			ReadAt: n.ReadAt, CreatedAt: n.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, views)
}

func (s *Server) markNotificationsRead(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identity := identityFrom(ctx)
	if err := identity.RequireAccount(); err != nil {
		s.writeErr(w, r, err)
		return
	}
	if err := s.store.MarkNotificationsRead(ctx, identity.UserID()); err != nil {
		s.writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) setPreference(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identity := identityFrom(ctx)
	if err := identity.RequireAccount(); err != nil {
		s.writeErr(w, r, err)
		return
	}

	var body struct {
		Channel   string `json:"channel"`
		EventType string `json:"event_type"`
		Enabled   bool   `json:"enabled"`
	}
	if err := decode(r, &body); err != nil {
		s.writeErr(w, r, err)
		return
	}
	if body.EventType == "" {
		body.EventType = "all"
	}

	if err := s.store.SetPreference(ctx, identity.UserID(), store.Preference{
		Channel: body.Channel, EventType: body.EventType, Enabled: body.Enabled,
	}); err != nil {
		s.writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// --- Web push -------------------------------------------------------------

type pushKeyView struct {
	PublicKey string `json:"public_key"`
	Enabled   bool   `json:"enabled"`

	RequiresHomeScreen bool `json:"requires_home_screen"`
}

func (s *Server) pushKey(w http.ResponseWriter, r *http.Request) {
	view := pushKeyView{
		Enabled:            s.pusher != nil,
		RequiresHomeScreen: isIOS(r.UserAgent()),
	}
	if s.pusher != nil {
		view.PublicKey = s.pusher.PublicKey()
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) subscribePush(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identity := identityFrom(ctx)
	if err := identity.RequireAccount(); err != nil {
		s.writeErr(w, r, err)
		return
	}

	var body struct {
		Endpoint string `json:"endpoint"`
		P256dh   string `json:"p256dh"`
		Auth     string `json:"auth"`
	}
	if err := decode(r, &body); err != nil {
		s.writeErr(w, r, err)
		return
	}

	if err := s.store.AddPushSubscription(ctx, store.PushSubscription{
		UserID: identity.UserID(), Endpoint: body.Endpoint,
		P256dh: body.P256dh, Auth: body.Auth,
	}); err != nil {
		s.writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]bool{"subscribed": true})
}

func (s *Server) unsubscribePush(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Endpoint string `json:"endpoint"`
	}
	if err := decode(r, &body); err != nil {
		s.writeErr(w, r, err)
		return
	}
	if err := s.store.DeletePushSubscription(r.Context(), body.Endpoint); err != nil {
		s.writeErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func isIOS(userAgent string) bool {
	ua := strings.ToLower(userAgent)
	return strings.Contains(ua, "iphone") || strings.Contains(ua, "ipad") ||
		(strings.Contains(ua, "macintosh") && strings.Contains(ua, "mobile"))
}
