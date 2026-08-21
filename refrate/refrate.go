// Package refrate supplies an independent reference mid-market rate.
//
// This package exists because of a measurement, not a feature request. On
// 2026-08-04 the best on-chain route for USDC to NGNC returned roughly 651
// NGN per USD while the real-world rate sat near 1,500. Presented on its own,
// "651" looks like a perfectly reasonable number. It is only recognisable as
// a catastrophic loss when compared against an outside source.
//
// So the reference rate is not decoration on a quote. It is the only thing
// standing between a user and a route that silently destroys half their
// money, and the routing engine treats a missing reference rate as a hard
// failure rather than degrading to "no spread shown".
package refrate

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// Rate is a mid-market rate for a currency pair at a point in time.
//
// Mid-market means no spread: the midpoint between what buyers pay and
// sellers accept. It is the honest benchmark to price a route against,
// because it is the rate nobody actually gets — every real route sits some
// distance from it, and that distance is the true cost.
type Rate struct {
	Base   string          // ISO-4217, e.g. "USD"
	Quote  string          // ISO-4217, e.g. "NGN"
	Mid    decimal.Decimal // units of Quote per one unit of Base
	AsOf   time.Time
	Source string // provider identity, surfaced so users can audit it
}

// Pair renders the currency pair, e.g. "USD/NGN".
func (r Rate) Pair() string { return r.Base + "/" + r.Quote }

// Age reports how stale the rate is relative to now.
func (r Rate) Age() time.Duration { return time.Since(r.AsOf) }

// Provider retrieves a reference rate for a pair.
//
// Implementations must return an error rather than a zero or guessed rate
// when they cannot answer. A wrong reference rate is worse than none: it
// would validate a bad route instead of flagging it.
type Provider interface {
	Rate(ctx context.Context, base, quote string) (Rate, error)
	Name() string
}

// ErrNoRate reports that a provider has no rate for the requested pair.
type ErrNoRate struct {
	Base, Quote, Source string
}

func (e *ErrNoRate) Error() string {
	return fmt.Sprintf("refrate: %s has no rate for %s/%s", e.Source, e.Base, e.Quote)
}

// ErrRateLimited reports that a provider refused because the caller has spent
// its quota.
//
// It is distinct from a generic failure because the remedy is different and
// the caller can act on it: a rate limit says "ask again later", where a
// decode failure says "this provider is broken". Collapsing the two would hide
// the single most likely way a free-tier feed fails under a schedule.
type ErrRateLimited struct {
	Source     string
	RetryAfter time.Duration
	Message    string
}

func (e *ErrRateLimited) Error() string {
	msg := fmt.Sprintf("refrate: %s rate-limited this request", e.Source)
	if e.RetryAfter > 0 {
		msg += fmt.Sprintf("; retry after %s", e.RetryAfter)
	}
	if e.Message != "" {
		msg += ": " + e.Message
	}
	return msg
}

// retryAfter reads the Retry-After header, which providers are free not to
// send. A missing or unparseable value reports zero rather than a guess — an
// invented backoff is the kind of plausible-looking number this project
// refuses everywhere else.
func retryAfter(resp *http.Response) time.Duration {
	v := strings.TrimSpace(resp.Header.Get("Retry-After"))
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

// Static is a Provider backed by hardcoded rates.
//
// Its purpose is tests and offline operation, and it is deliberately awkward
// to use in production: MaxAge is not applied to it, and callers are expected
// to surface its name so a stale hardcoded rate can never masquerade as live
// market data.
type Static struct {
	Rates map[string]decimal.Decimal // keyed "USD/NGN"
	Stamp time.Time
}

// NewStatic builds a Static provider from pair strings to rates.
func NewStatic(rates map[string]decimal.Decimal) *Static {
	return &Static{Rates: rates, Stamp: time.Now()}
}

// Name identifies the provider.
func (s *Static) Name() string { return "static" }

// Rate implements Provider.
func (s *Static) Rate(_ context.Context, base, quote string) (Rate, error) {
	key := base + "/" + quote
	v, ok := s.Rates[key]
	if !ok {
		return Rate{}, &ErrNoRate{Base: base, Quote: quote, Source: s.Name()}
	}
	stamp := s.Stamp
	if stamp.IsZero() {
		stamp = time.Now()
	}
	return Rate{
		Base:   base,
		Quote:  quote,
		Mid:    v,
		AsOf:   stamp,
		Source: s.Name(),
	}, nil
}

// Checked wraps a Provider and rejects rates older than MaxAge.
//
// FX moves, and a rate from last week would quietly turn the spread
// calculation into fiction. Wrapping is preferred over each provider policing
// itself so the staleness policy lives in one auditable place.
type Checked struct {
	Inner  Provider
	MaxAge time.Duration
}

// Name identifies the wrapped provider.
func (c *Checked) Name() string { return c.Inner.Name() }

// Rate implements Provider, rejecting stale results.
func (c *Checked) Rate(ctx context.Context, base, quote string) (Rate, error) {
	r, err := c.Inner.Rate(ctx, base, quote)
	if err != nil {
		return Rate{}, err
	}
	if c.MaxAge > 0 && r.Age() > c.MaxAge {
		return Rate{}, fmt.Errorf(
			"refrate: %s rate for %s is %s old, exceeds max age %s",
			r.Source, r.Pair(), r.Age().Round(time.Second), c.MaxAge)
	}
	return r, nil
}
