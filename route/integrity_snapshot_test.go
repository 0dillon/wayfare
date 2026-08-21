package route_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/dex"
	"github.com/Wayfare-labs/wayfare/refrate"
	"github.com/Wayfare-labs/wayfare/route"
	"github.com/Wayfare-labs/wayfare/snapshot"
)

// The integrity taxonomy re-driven from recorded mainnet responses.
//
// route_test.go covers the same states with hand-built path sets, which is
// what a unit test should do. These run the classifier over the actual bodies
// Horizon returned for the three corridors, so the states the project
// publishes are the states its own recorded evidence produces — not just the
// states its fixtures were written to produce.
//
// If these ever disagree with the hand-built cases, the fixtures are wrong.

func loadSnap(t *testing.T, prefix string) *snapshot.Manifest {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join("../testdata/snapshots", prefix+"-*"))
	if err != nil || len(matches) == 0 {
		t.Skipf("no snapshot matching %q; capture one with cmd/ladder -record", prefix)
	}
	m, err := snapshot.Load(matches[0])
	if err != nil {
		t.Fatalf("loading %s: %v", matches[0], err)
	}
	return m
}

// engineOver builds an engine answering only from a snapshot, with the
// reference mid pinned so the assertions are about integrity rather than about
// whatever the rate provider says today.
func engineOver(m *snapshot.Manifest, pair, mid string) *route.Engine {
	return &route.Engine{
		DEX: &dex.Client{
			HorizonURL: "https://horizon.stellar.org",
			HTTPClient: m.HTTPClient(),
		},
		RefRate: refrate.NewStatic(map[string]decimal.Decimal{
			pair: decimal.RequireFromString(mid),
		}),
	}
}

func quoteAt(t *testing.T, e *route.Engine, recv asset.Asset, quoteCode, amount string) *route.Result {
	t.Helper()
	res, err := e.Quote(context.Background(), route.Request{
		SendAsset:      asset.USDC(),
		SendAmount:     decimal.RequireFromString(amount),
		ReceiveAsset:   recv,
		ReferenceBase:  "USD",
		ReferenceQuote: quoteCode,
	})
	if err != nil {
		t.Fatalf("Quote(%s): %v", amount, err)
	}
	return res
}

// TestRecordedNGNCIsDirect: every path reaches NGNC without traversing another
// fiat token, so an independent market exists.
func TestRecordedNGNCIsDirect(t *testing.T) {
	e := engineOver(loadSnap(t, "usdc-ngnc"), "USD/NGN", "1350.2568")

	res := quoteAt(t, e, asset.NGNC(), "NGN", "100")
	if res.Integrity != route.IntegrityDirect {
		t.Errorf("Integrity = %s, want DIRECT (XLM is a bridge asset, not a fiat token)",
			res.Integrity)
	}
	if len(res.DependsOn) != 0 {
		t.Errorf("DependsOn = %v, want empty for a direct corridor", res.DependsOn)
	}
	if res.Recommended != nil {
		t.Error("a corridor measured at a ~54% loss must not be recommended")
	}
}

// TestRecordedGHSCIsDerivative: every recorded path traverses NGNC, at every
// size. This is the case a loss percentage alone cannot express.
func TestRecordedGHSCIsDerivative(t *testing.T) {
	e := engineOver(loadSnap(t, "usdc-ghsc"), "USD/GHS", "11.0912")

	// Checked across the ladder rather than at one size, because the claim
	// is that GHSC has no independent market anywhere — not that it happened
	// to lack one at 100 USDC.
	for _, amount := range []string{"0.1", "1", "100", "5000"} {
		res := quoteAt(t, e, asset.GHSC(), "GHS", amount)

		if res.Integrity != route.IntegrityDerivative {
			t.Errorf("at %s USDC: Integrity = %s, want DERIVATIVE",
				amount, res.Integrity)
			continue
		}
		if len(res.DependsOn) != 1 || res.DependsOn[0].Code != "NGNC" {
			t.Errorf("at %s USDC: DependsOn = %v, want exactly NGNC",
				amount, res.DependsOn)
		}
		if len(res.Quotes) > 0 {
			warnings := strings.Join(res.Quotes[0].Warnings, " ")
			if !strings.Contains(warnings, "derivative corridor") {
				t.Errorf("at %s USDC: quote does not carry the dependency warning: %v",
					amount, res.Quotes[0].Warnings)
			}
		}
	}
}

// TestRecordedKESCHasNoMarket: zero paths at every recorded size. Distinct
// from bad pricing, and the reason a loss grade cannot carry this state.
func TestRecordedKESCHasNoMarket(t *testing.T) {
	e := engineOver(loadSnap(t, "usdc-kesc"), "USD/KES", "129.4263")

	for _, amount := range []string{"0.1", "100", "5000"} {
		res := quoteAt(t, e, asset.KESC(), "KES", amount)

		if res.Integrity != route.IntegrityNoMarket {
			t.Errorf("at %s USDC: Integrity = %s, want NO-MARKET", amount, res.Integrity)
		}
		if len(res.Quotes) != 0 {
			t.Errorf("at %s USDC: got %d quotes, want none", amount, len(res.Quotes))
		}
		if res.Recommended != nil {
			t.Errorf("at %s USDC: Recommended must be nil where there is no market", amount)
		}
		if res.Integrity.Priceable() {
			t.Errorf("at %s USDC: a corridor with no paths must not report as priceable", amount)
		}
	}
}

// TestRecordedLadderFindingsMatchTheirState checks the ladder-level summary
// each corridor produces, since that string is what the UI and the API lead
// with and is the most likely place for a state to be described as the wrong
// kind of failure.
func TestRecordedLadderFindingsMatchTheirState(t *testing.T) {
	cases := []struct {
		prefix   string
		recv     asset.Asset
		pair     string
		mid      string
		quote    string
		want     route.Integrity
		contains string
	}{
		{"usdc-ngnc", asset.NGNC(), "USD/NGN", "1350.2568", "NGN",
			route.IntegrityDirect, "No usable size"},
		{"usdc-ghsc", asset.GHSC(), "USD/GHS", "11.0912", "GHS",
			route.IntegrityDerivative, "Derivative corridor"},
		{"usdc-kesc", asset.KESC(), "USD/KES", "129.4263", "KES",
			route.IntegrityNoMarket, "absence of a price"},
	}

	for _, tc := range cases {
		t.Run(tc.prefix, func(t *testing.T) {
			m := loadSnap(t, tc.prefix)
			e := engineOver(m, tc.pair, tc.mid)

			res, err := e.Ladder(context.Background(), route.LadderRequest{
				SendAsset:      asset.USDC(),
				ReceiveAsset:   tc.recv,
				Sizes:          route.DefaultSizes,
				ReferenceBase:  "USD",
				ReferenceQuote: tc.quote,
			})
			if err != nil {
				t.Fatalf("Ladder: %v", err)
			}

			if res.Integrity != tc.want {
				t.Errorf("ladder Integrity = %s, want %s", res.Integrity, tc.want)
			}
			if !strings.Contains(res.Finding, tc.contains) {
				t.Errorf("finding = %q, want it to contain %q", res.Finding, tc.contains)
			}
			if res.Viable() {
				t.Errorf("no corridor in the recorded set is viable, but %s reported one",
					tc.prefix)
			}
		})
	}
}
