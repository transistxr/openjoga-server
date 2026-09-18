package notify

import (
	"context"
	"encoding/json"
	"log/slog"

	"openjoga-server/internal/store"
)

type Mailer interface {
	Send(ctx context.Context, to, subject, body string) error
}

type Message struct {
	Type    string
	Title   string
	Body    string
	EventID *int64
	URL     string
}

type Notifier struct {
	store  *store.Store
	mailer Mailer
	pusher *Pusher
	log    *slog.Logger
}

func New(s *store.Store, m Mailer, p *Pusher, log *slog.Logger) *Notifier {
	return &Notifier{store: s, mailer: m, pusher: p, log: log}
}

func (n *Notifier) Send(ctx context.Context, userID int64, m Message) error {
	if _, err := n.store.CreateNotification(ctx, store.Notification{
		UserID:  userID,
		Type:    m.Type,
		EventID: m.EventID,
		Title:   m.Title,
		Body:    &m.Body,
	}); err != nil {
		return err
	}

	user, err := n.store.GetUser(ctx, userID)
	if err != nil {
		return err
	}

	if user.Email != nil {
		if ok, err := n.store.ChannelEnabled(ctx, userID, store.ChannelEmail, m.Type); err == nil && ok {
			if err := n.mailer.Send(ctx, *user.Email, m.Title, m.Body+"\n\n"+m.URL); err != nil {
				n.log.Warn("email delivery failed", "user", userID, "error", err)
			}
		}
	}

	if n.pusher != nil {
		if ok, err := n.store.ChannelEnabled(ctx, userID, store.ChannelWebPush, m.Type); err == nil && ok {
			n.push(ctx, userID, m)
		}
	}
	return nil
}

func (n *Notifier) push(ctx context.Context, userID int64, m Message) {
	subs, err := n.store.ListPushSubscriptions(ctx, userID)
	if err != nil {
		n.log.Warn("could not load push subscriptions", "user", userID, "error", err)
		return
	}
	if len(subs) == 0 {
		return
	}

	payload, err := json.Marshal(map[string]string{
		"title": m.Title,
		"body":  m.Body,
		"url":   m.URL,
	})
	if err != nil {
		return
	}

	for _, sub := range subs {
		if err := n.pusher.Send(ctx, sub, payload); err != nil {
			n.log.Warn("push delivery failed", "user", userID, "error", err)
		}
	}
}

func (n *Notifier) Mailer() Mailer { return n.mailer }
