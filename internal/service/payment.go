package service

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"

	"openjoga-server/internal/store"
	"openjoga-server/internal/text"
)

// PayeeDetails is how a player settles up. Organiser-tier: it reaches people
// through the pitch-side collect view, not the public event page.
type PayeeDetails struct {
	Kind  string
	Value string
	Label *string

	DeepLink string
}

// PayeeFor builds what to show a player who owes money.
func PayeeFor(g store.Group, amountPence int64, currency string) *PayeeDetails {
	if !g.HasPaymentDetails() {
		return nil
	}
	details := &PayeeDetails{Kind: *g.PaymentKind, Value: *g.PaymentValue, Label: g.PaymentLabel}

	if *g.PaymentKind == "upi" {
		details.DeepLink = upiLink(*g.PaymentValue, payeeName(g), amountPence, currency)
	}
	return details
}

func upiLink(vpa, payee string, amountPence int64, currency string) string {
	q := url.Values{}
	q.Set("pa", vpa)
	q.Set("pn", payee)
	q.Set("cu", currency)
	if amountPence > 0 {
		q.Set("am", fmt.Sprintf("%d.%02d", amountPence/100, amountPence%100))
	}
	return "upi://pay?" + q.Encode()
}

func payeeName(g store.Group) string {
	if g.PaymentLabel != nil && *g.PaymentLabel != "" {
		return *g.PaymentLabel
	}
	return g.Name
}

const MaxReferenceLength = 64

var (
	ErrNeedReference = errors.New(text.ErrNeedReference)
	ErrBadReference  = errors.New(text.ErrBadReference)
)

func (s *Service) MarkPaid(ctx context.Context, paymentID, by int64, status, rawReference string, selfMarking bool) error {
	reference := strings.TrimSpace(rawReference)
	if utf8.RuneCountInString(reference) > MaxReferenceLength {
		return ErrBadReference
	}
	if selfMarking && status == store.PaymentPaid && reference == "" {
		return ErrNeedReference
	}

	var ref *string
	if reference != "" {
		ref = &reference
	}
	if err := s.Store.SetPaymentStatus(ctx, paymentID, status, by, ref); err != nil {
		return err
	}
	return s.Store.Record(ctx, &by, store.ActionMarkPaid, "payment", &paymentID,
		fmt.Sprintf(`{"status":%q,"self":%t}`, status, selfMarking))
}

func (s *Service) Debts(ctx context.Context, groupID *int64) ([]store.Debt, error) {
	debts, err := s.Store.ListDebts(ctx, groupID)
	if err != nil {
		return nil, err
	}
	if len(debts) == 0 {
		return debts, nil
	}

	ids := make([]int64, len(debts))
	for i, d := range debts {
		ids[i] = d.UserID
	}
	reachable, err := s.Store.ReachableSet(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range debts {
		debts[i].Reachable = reachable[debts[i].UserID]
	}
	return debts, nil
}

func (s *Service) AddKittyEntry(ctx context.Context, e store.KittyEntry, by int64) error {
	e.CreatedBy = &by
	if err := s.Store.AddKittyEntry(ctx, e); err != nil {
		return err
	}
	return s.Store.Record(ctx, &by, store.ActionKittyEntry, "group", &e.GroupID,
		fmt.Sprintf(`{"type":%q,"amount_pence":%d}`, e.Type, e.AmountPence))
}
