package notify

import (
	"context"
	"io"
	"net/http"

	webpush "github.com/SherClockHolmes/webpush-go"

	"openjoga-server/internal/store"
)

type Pusher struct {
	store      *store.Store
	publicKey  string
	privateKey string
	subject    string
}

func NewPusher(s *store.Store, publicKey, privateKey, subject string) *Pusher {
	return &Pusher{store: s, publicKey: publicKey, privateKey: privateKey, subject: subject}
}

func (p *Pusher) PublicKey() string { return p.publicKey }

func (p *Pusher) Send(ctx context.Context, sub store.PushSubscription, payload []byte) error {
	resp, err := webpush.SendNotificationWithContext(ctx, payload, &webpush.Subscription{
		Endpoint: sub.Endpoint,
		Keys:     webpush.Keys{P256dh: sub.P256dh, Auth: sub.Auth},
	}, &webpush.Options{
		Subscriber:      p.subject,
		VAPIDPublicKey:  p.publicKey,
		VAPIDPrivateKey: p.privateKey,
		TTL:             60 * 60 * 12,
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		return p.store.DeletePushSubscription(ctx, sub.Endpoint)
	}
	return nil
}

func GenerateVAPIDKeys() (privateKey, publicKey string, err error) {
	return webpush.GenerateVAPIDKeys()
}
