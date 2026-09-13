package service

import (
	"errors"

	"openjoga-server/internal/store"
	"openjoga-server/internal/text"
)

var (
	ErrClosed      = errors.New(text.ErrGameClosed)
	ErrFull        = errors.New(text.ErrGameFull)
	ErrGuestLimit  = errors.New(text.ErrGuestLimit)
	ErrNoSponsored = errors.New(text.ErrNoSponsored)
	ErrSponsorCap  = errors.New(text.ErrSponsorCap)
	ErrNotAllowed  = errors.New(text.ErrNotAllowed)
	ErrInvalid     = errors.New(text.ErrInvalid)
	ErrPaymentNeedsGate = errors.New(text.ErrPaymentNeedsGate)
)

type Service struct {
	Store *store.Store

	BaseURL string
}

func New(s *store.Store, baseURL string) *Service {
	return &Service{Store: s, BaseURL: baseURL}
}

func (s *Service) EventURL(slug string) string {
	return s.BaseURL + "/e/" + slug
}

func ptr[T any](v T) *T { return &v }
