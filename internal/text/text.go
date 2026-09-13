package text

import (
	"fmt"
	"strings"
	"time"
)

func FirstName(name string) string {
	name = strings.TrimSpace(name)
	first, rest, found := strings.Cut(name, " ")
	if !found || rest == "" {
		return first
	}
	return first + " " + string([]rune(strings.TrimSpace(rest))[0]) + "."
}

func GivenName(name string) string {
	first, _, _ := strings.Cut(strings.TrimSpace(name), " ")
	return first
}

func GuestLabel(name *string, host string) string {
	if name != nil && *name != "" {
		return *name
	}
	return FirstName(host) + "'s +1"
}

func Money(pence int64, currency string) string {
	symbols := map[string]string{"GBP": "£", "EUR": "€", "USD": "$", "INR": "₹"}
	symbol, ok := symbols[currency]
	if !ok {
		symbol = currency + " "
	}
	return fmt.Sprintf("%s%d.%02d", symbol, pence/100, pence%100)
}

func Plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func EventTime(startsAt, timezone string) string {
	t, err := time.Parse(time.RFC3339, startsAt)
	if err != nil {
		return startsAt
	}
	if loc, err := time.LoadLocation(timezone); err == nil {
		t = t.In(loc)
	}
	return t.Format("Mon 2 Jan, 15:04")
}


const SignInSubject = "Your OpenJoga sign-in link"

func SignInEmail(link, code string, ttl time.Duration) string {
	return fmt.Sprintf(`Tap to sign in:

%s

Or enter this code: %s

The code only works on the device you asked from. Either way it expires in %d
minutes, and if you did not ask for this you can ignore it.
`, link, code, int(ttl.Minutes()))
}

// --- Notifications ---------------------------------------------------------

const (
	TitleGameOn       = "Game on"
	TitleGameOff      = "Game off"
	TitleReminder     = "RSVPs closing soon"
	TitleYoureIn      = "You're in"
	TitleJoinRequest  = "Someone wants to join"
	TitleSponsorOffer = "Someone's offering to cover slots"
)

func BodyConfirmed(when string) string {
	return fmt.Sprintf("%s is confirmed. See you there.", when)
}

func BodyCancelledAtCutoff(when string) string {
	return fmt.Sprintf("%s is cancelled — not enough players by the cutoff.", when)
}

func BodyCancelledByOrganiser(when string) string {
	return fmt.Sprintf("%s is off. The organiser called it.", when)
}

func BodyReminder(when string, going int, needs *int) string {
	if needs != nil && going < *needs {
		return fmt.Sprintf("%s — %d in, needs %d or it's off.", when, going, *needs)
	}
	return fmt.Sprintf("%s — %d in so far.", when, going)
}

func BodyPromoted() string {
	return "A slot opened up and you've got it."
}

func BodyJoinRequest(name string) string {
	return fmt.Sprintf("%s is waiting to be let in.", FirstName(name))
}

func BodySponsorOffer(name string) string {
	return fmt.Sprintf("%s wants to cover players for this game.", FirstName(name))
}

// --- Messages a person sends themselves ------------------------------------
func Share(total int, max, spotsLeft *int, url string) string {
	if max != nil && spotsLeft != nil {
		return fmt.Sprintf("I'm in — %d/%d, %d spots left. Who's next? %s",
			total, *max, *spotsLeft, url)
	}
	return fmt.Sprintf("I'm in — %d playing so far. Join us: %s", total, url)
}

func Cancellation(title, when, url string) string {
	if title == "" {
		title = FallbackGameTitle
	}
	return fmt.Sprintf("%s on %s is OFF. Sorry all. Details: %s", title, when, url)
}

func Welcome(claimURL string) string {
	return fmt.Sprintf("You're in! Tap here to see games and RSVP: %s", claimURL)
}

func DebtChase(name string, pence int64, currency string, games int) string {
	return fmt.Sprintf("Hi %s — you're down for %s from %d %s. Settle up next time?",
		GivenName(name), Money(pence, currency), games, Plural(games, "game", "games"))
}

// --- Fixed copy ------------------------------------------------------------

const (
	FallbackGameTitle   = "the game"
	ReasonBelowMinimum  = "not enough players by the cutoff"
	NoteRoundingSurplus = "rounding surplus from splitting the pitch fee"
)

// --- Error text ------------------------------------------------------------

const (
	ErrGameClosed  = "this game is no longer taking changes"
	ErrGameFull    = "this game is full"
	ErrGuestLimit  = "that is more guests than this group allows"
	ErrNoSponsored = "there are no covered slots left"
	ErrSponsorCap  = "that is more slots than one person may cover here"
	ErrNotAllowed  = "not allowed"
	ErrInvalid     = "invalid request"
	ErrInviteUsed  = "that invite has expired or been used up"
	ErrClaimUsed   = "that link has expired or been used"
	ErrLastOwner   = "an instance must always have at least one owner"
	ErrBadEmail    = "that does not look like an email address"
	ErrBadCode     = "that code is not right"
	ErrCodeExpired = "that code has expired, ask for a new one"
	ErrTooMany     = "too many attempts, ask for a new code"
	ErrRateLimited = "too many requests for this address, try again shortly"
	ErrForbidden   = "forbidden"
	ErrNeedAccount = "this needs an identity"
	ErrNeedContact = "this needs a verified contact"
	ErrNotFound    = "not found"
	ErrServerFault = "something went wrong"
	ErrBadCSRF     = "bad or missing CSRF token"
	ErrSlowDown    = "slow down"

	ErrNeedReference    = "add the reference from your payment app so this can be checked"
	ErrBadReference     = "that reference is too long"
	ErrPaymentNeedsGate = "to show payment details before the game, restrict who can join -- " +
		"otherwise anyone with the link would see them"
	ErrContactTaken = "somebody already uses that email or number -- sign in with it instead"
)
