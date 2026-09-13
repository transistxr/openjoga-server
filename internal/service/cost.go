package service

import "openjoga-server/internal/store"

// Quote is what a player is told a game will cost them.
type Quote struct {
	Type string

	PerHeadPence *int64

	LowPence  *int64
	HighPence *int64

	TotalPence *int64
	Resolved   bool
}

func QuoteFor(e store.Event) Quote {
	switch e.CostType {
	case store.CostFixed:
		return Quote{Type: store.CostFixed, PerHeadPence: e.CostPerHeadPence, Resolved: true}

	case store.CostSplit:
		q := Quote{Type: store.CostSplit, TotalPence: e.CostTotalPence}

		if e.CostPerHeadPence != nil {
			q.PerHeadPence = e.CostPerHeadPence
			q.Resolved = true
			return q
		}
		if e.CostTotalPence == nil {
			return q
		}
		total := *e.CostTotalPence

		if e.MinPlayers != nil && *e.MinPlayers > 0 {
			q.HighPence = ptr(ResolvePerHead(total, *e.MinPlayers))
		}
		if e.MaxPlayers != nil && *e.MaxPlayers > 0 {
			q.LowPence = ptr(ResolvePerHead(total, *e.MaxPlayers))
		}
		return q

	default:
		return Quote{Type: store.CostFree, PerHeadPence: ptr(int64(0)), Resolved: true}
	}
}

func ResolvePerHead(totalPence int64, slots int) int64 {
	if slots <= 0 {
		return 0
	}
	n := int64(slots)
	return (totalPence + n - 1) / n
}

func AmountDue(perHeadPence int64, guests int, sponsored bool) int64 {
	slots := int64(1 + guests)
	if sponsored {
		slots--
	}
	return perHeadPence * slots
}

func DefaultRSVPPolicy(costType string) string {
	if costType == store.CostFree {
		return store.PolicyOpen
	}
	return store.PolicyContactable
}
