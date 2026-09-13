package auth

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"openjoga-server/internal/id"
	"openjoga-server/internal/store"
	"openjoga-server/internal/text"
)

var (
	ErrBadEmail    = errors.New(text.ErrBadEmail)
	ErrBadCode     = errors.New(text.ErrBadCode)
	ErrCodeExpired = errors.New(text.ErrCodeExpired)
	ErrTooMany     = errors.New(text.ErrTooMany)
	ErrRateLimited = errors.New(text.ErrRateLimited)
)

type Mailer interface {
	Send(ctx context.Context, to, subject, body string) error
}

type Challenges struct {
	store  *store.Store
	mailer Mailer
	pepper []byte

	baseURL string
	ttl     time.Duration

	maxPerWindow int
	window       time.Duration

	rejectionsAllowed int
	rejectionWindow   time.Duration
}

func NewChallenges(s *store.Store, m Mailer, pepper []byte, baseURL string) *Challenges {
	return &Challenges{
		store:        s,
		mailer:       m,
		pepper:       pepper,
		baseURL:      baseURL,
		ttl:          20 * time.Minute,
		maxPerWindow: 5,
		window:       15 * time.Minute,

		rejectionsAllowed: 2,
		rejectionWindow:   24 * time.Hour,
	}
}

type Outcome struct {
	UserID int64

	MergeOffer *MergeOffer
}

type MergeOffer struct {
	FromUserID  int64
	DisplayName string
	RSVPCount   int
}

func (c *Challenges) Request(ctx context.Context, email string, requestingUserID int64) error {
	address, err := parseEmail(email)
	if err != nil {
		return err
	}

	since := store.FormatTime(time.Now().Add(-c.window))
	recent, err := c.store.CountChallengesSince(ctx, address, since)
	if err != nil {
		return err
	}
	if recent >= c.maxPerWindow {
		return ErrRateLimited
	}

	if err := c.store.InvalidateChallenges(ctx, address); err != nil {
		return err
	}

	linkToken, code := id.New(), id.Code()

	var claim *int64
	if requestingUserID != 0 {
		trusted, err := c.mayClaim(ctx, requestingUserID)
		if err != nil {
			return err
		}
		if trusted {
			claim = &requestingUserID
		}
	}

	if _, err := c.store.CreateChallenge(ctx, store.Challenge{
		Identifier:    address,
		LinkTokenHash: id.Hash(linkToken),
		CodeHash:      id.HMACCode(c.pepper, address, code),
		ClaimUserID:   claim,
		MaxAttempts:   5,
		ExpiresAt:     store.FormatTime(time.Now().Add(c.ttl)),
	}); err != nil {
		return err
	}

	return c.mailer.Send(ctx, address, text.SignInSubject,
		text.SignInEmail(c.linkURL(linkToken), code, c.ttl))
}

func (c *Challenges) linkURL(token string) string {
	return fmt.Sprintf("%s/auth/callback?token=%s", c.baseURL, token)
}

func (c *Challenges) VerifyCode(ctx context.Context, email, code string, openingUserID int64) (Outcome, error) {
	address, err := parseEmail(email)
	if err != nil {
		return Outcome{}, err
	}

	ch, err := c.store.GetActiveChallenge(ctx, address)
	if errors.Is(err, store.ErrNotFound) {
		return Outcome{}, ErrCodeExpired
	}
	if err != nil {
		return Outcome{}, err
	}
	if ch.Exhausted() {
		return Outcome{}, ErrTooMany
	}

	if err := c.store.IncrementChallengeAttempts(ctx, ch.ID); err != nil {
		return Outcome{}, err
	}

	if !id.Equal(ch.CodeHash, id.HMACCode(c.pepper, address, strings.TrimSpace(code))) {
		if ch.Attempts+1 >= ch.MaxAttempts {
			_ = c.store.ConsumeChallenge(ctx, ch.ID)
			return Outcome{}, ErrTooMany
		}
		return Outcome{}, ErrBadCode
	}

	return c.resolve(ctx, ch, openingUserID, true)
}

func (c *Challenges) VerifyLink(ctx context.Context, token string, openingUserID int64) (Outcome, error) {
	ch, err := c.store.GetChallengeByLinkToken(ctx, id.Hash(token))
	if errors.Is(err, store.ErrNotFound) {
		return Outcome{}, ErrCodeExpired
	}
	if err != nil {
		return Outcome{}, err
	}
	if ch.Consumed() || ch.Expired() {
		return Outcome{}, ErrCodeExpired
	}
	return c.resolve(ctx, ch, openingUserID, false)
}

func (c *Challenges) resolve(ctx context.Context, ch store.Challenge, openingUserID int64, sameDevice bool) (Outcome, error) {
	if err := c.store.ConsumeChallenge(ctx, ch.ID); err != nil {
		return Outcome{}, err
	}

	var claimed int64
	if ch.ClaimUserID != nil {
		claimed = *ch.ClaimUserID
	}

	var out Outcome
	err := c.store.Tx(ctx, func(tx *store.Store) error {
		target, err := tx.GetUserByProvenEmail(ctx, ch.Identifier)
		if errors.Is(err, store.ErrNotFound) {
			target, err = tx.CreateDeviceUser(ctx, displayNameFor(ctx, tx, openingUserID, ch.Identifier))
			if err != nil {
				return err
			}
			if err := tx.VerifyEmail(ctx, target.ID, ch.Identifier); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}

		if openingUserID != 0 && openingUserID != target.ID {
			if err := tx.MergeUsers(ctx, openingUserID, target.ID); err != nil {
				return err
			}
			if claimed == openingUserID {
				claimed = 0 // already handled
			}
		}

		out.UserID = target.ID

		if claimed != 0 && claimed != target.ID {
			offer, err := buildMergeOffer(ctx, tx, claimed)
			if err != nil {
				return err
			}
			out.MergeOffer = offer
		}
		return nil
	})
	return out, err
}

func (c *Challenges) ConfirmMerge(ctx context.Context, from, to int64) error {
	return c.store.Tx(ctx, func(tx *store.Store) error {
		return tx.MergeUsers(ctx, from, to)
	})
}

func (c *Challenges) RejectMerge(ctx context.Context, from, to int64) error {
	return c.store.Record(ctx, &to, store.ActionRejectClaim, "user", &from,
		fmt.Sprintf(`{"declined_by":%d}`, to))
}

func (c *Challenges) mayClaim(ctx context.Context, userID int64) (bool, error) {
	since := store.FormatTime(time.Now().Add(-c.rejectionWindow))
	rejections, err := c.store.CountActionsAgainst(ctx,
		store.ActionRejectClaim, "user", userID, since)
	if err != nil {
		return false, err
	}
	return rejections < c.rejectionsAllowed, nil
}

func buildMergeOffer(ctx context.Context, s *store.Store, from int64) (*MergeOffer, error) {
	u, err := s.GetUser(ctx, from)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	rsvps, err := s.CountRSVPsForUser(ctx, from)
	if err != nil {
		return nil, err
	}
	if rsvps == 0 {
		return nil, nil
	}

	return &MergeOffer{
		FromUserID:  from,
		DisplayName: u.DisplayName,
		RSVPCount:   rsvps,
	}, nil
}

func displayNameFor(ctx context.Context, s *store.Store, openingUserID int64, email string) string {
	if openingUserID != 0 {
		if u, err := s.GetUser(ctx, openingUserID); err == nil && u.DisplayName != "" {
			return u.DisplayName
		}
	}
	if local, _, ok := strings.Cut(email, "@"); ok && local != "" {
		return local
	}
	return "Player"
}

func parseEmail(raw string) (string, error) {
	address, err := mail.ParseAddress(strings.TrimSpace(raw))
	if err != nil {
		return "", ErrBadEmail
	}
	return store.NormalizeEmail(address.Address), nil
}
