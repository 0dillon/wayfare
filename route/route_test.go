package route

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/Fury03/wayfare/asset"
	"github.com/Fury03/wayfare/dex"
	"github.com/Fury03/wayfare/refrate"
)

// liveStrictSendResponse is the actual body Horizon returned for
//
//	/paths/strict-send?source_asset=USDC&source_amount=100&destination_assets=NGNC
//
// on mainnet, 2026-08-04. Both records are real: the XLM-bridged path paying
// 65,100 NGNC and the direct market paying 21,786.
//
// It is used unmodified as the fixture because the central claim of this
// project — that the corridor's best available route is not worth taking — is
// only as credible as the data behind it.
const liveStrictSendResponse = `{
  "_embedded": {
    "records": [
      {
        "source_asset_type": "credit_alphanum4",
        "source_asset_code": "USDC",
        "source_amount": "100.0000000",
        "destination_asset_type": "credit_alphanum4",
        "destination_asset_code": "NGNC",
        "destination_amount": "65100.1379550",
        "path": [ { "asset_type": "native" } ]
      },
      {
        "source_asset_type": "credit_alphanum4",
        "source_asset_code": "USDC",
        "source_amount": "100.0000000",
        "destination_asset_type": "credit_alphanum4",
        "destination_asset_code": "NGNC",
        "destination_amount": "21785.7821141",
        "path": []
      }
    ]
  }
}`

func horizonStub(t *testing.T, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
}

func usdToNGN(rate string) refrate.Provider {
	return refrate.NewStatic(map[string]decimal.Decimal{
		"USD/NGN": decimal.RequireFromString(rate),
	})
}

func ngnRequest(amount string) Request {
	return Request{
		SendAsset:      asset.USDC(),
		SendAmount:     decimal.RequireFromString(amount),
		ReceiveAsset:   asset.NGNC(),
		ReferenceBase:  "USD",
		ReferenceQuote: "NGN",
	}
}

// TestLiveCorridorIsRefused is the project's headline test.
//
// Given the real mainnet path data and a realistic USD/NGN mid of 1,500, the
// engine must decline to recommend anything. The best route pays 651 NGN per
// USD against a mid of 1,500 — a 56.6% loss. Ranking alone would have
// happily crowned it the winner.
func TestLiveCorridorIsRefused(t *testing.T) {
	srv := horizonStub(t, liveStrictSendResponse)
	defer srv.Close()

	e := &Engine{
		DEX:     &dex.Client{HorizonURL: srv.URL},
		RefRate: usdToNGN("1500"),
	}

	res, err := e.Quote(context.Background(), ngnRequest("100"))
	if err != nil {
		t.Fatalf("Quote: %v", err)
	}

	if len(res.Quotes) != 1 {
		t.Fatalf("expected 1 quote (the best DEX path), got %d", len(res.Quotes))
	}
	q := res.Quotes[0]

	// Best path must be the XLM bridge, not the direct market.
	if got, want := q.ReceiveAmount.String(), "65100.137955"; got != want {
		t.Errorf("ReceiveAmount = %s, want %s (the XLM-bridged path)", got, want)
	}
	if !strings.Contains(q.Description, "XLM") {
		t.Errorf("Description = %q, want the XLM hop named", q.Description)
	}

	// 65100.137955 / 100 = 651.00137955 NGN per USD
	if got := q.EffectiveRate.StringFixed(2); got != "651.00" {
		t.Errorf("EffectiveRate = %s, want 651.00", got)
	}

	// (1500 - 651.00) / 1500 = 56.6%
	if got := q.LossPct.StringFixed(1); got != "56.6" {
		t.Errorf("LossPct = %s, want 56.6", got)
	}

	if q.Verdict != VerdictUnusable {
		t.Errorf("Verdict = %s, want UNUSABLE", q.Verdict)
	}

	if res.Viable() {
		t.Fatal("engine recommended a route that loses 56.6% of the sender's money")
	}
	if res.Recommended != nil {
		t.Fatal("Recommended must be nil when every route is unusable")
	}

	joined := strings.Join(res.Notes, " ")
	if !strings.Contains(joined, "No viable route") {
		t.Errorf("expected an explicit no-viable-route note, got: %v", res.Notes)
	}
}

// TestLossAmountIsInRecipientCurrency checks the figure a user actually
// reacts to: not a percentage, but how much naira went missing.
func TestLossAmountIsInRecipientCurrency(t *testing.T) {
	srv := horizonStub(t, liveStrictSendResponse)
	defer srv.Close()

	e := &Engine{DEX: &dex.Client{HorizonURL: srv.URL}, RefRate: usdToNGN("1500")}
	res, err := e.Quote(context.Background(), ngnRequest("100"))
	if err != nil {
		t.Fatalf("Quote: %v", err)
	}

	// At mid, 100 USD is 150,000 NGN. The route delivers 65,100.14, so
	// 84,899.86 NGN is lost.
	if got, want := res.Quotes[0].LossAmount.StringFixed(2), "84899.86"; got != want {
		t.Errorf("LossAmount = %s, want %s", got, want)
	}
}

// TestGoodRouteIsRecommended is the control: with a mid the route can
// actually meet, the same machinery must recommend it. Without this, a test
// suite proving the engine refuses things would be satisfied by an engine
// that refuses everything.
func TestGoodRouteIsRecommended(t *testing.T) {
	srv := horizonStub(t, liveStrictSendResponse)
	defer srv.Close()

	// A mid of 660 puts the 651 route about 1.4% below mid.
	e := &Engine{DEX: &dex.Client{HorizonURL: srv.URL}, RefRate: usdToNGN("660")}
	res, err := e.Quote(context.Background(), ngnRequest("100"))
	if err != nil {
		t.Fatalf("Quote: %v", err)
	}

	if !res.Viable() {
		t.Fatal("expected a viable route when the rate is close to mid")
	}
	if res.Recommended.Verdict != VerdictGood {
		t.Errorf("Verdict = %s, want GOOD", res.Recommended.Verdict)
	}
}

// TestTokenDeliveryIsDisclosed ensures the user is told the on-chain leg ends
// in NGNC rather than naira in a bank account.
func TestTokenDeliveryIsDisclosed(t *testing.T) {
	srv := horizonStub(t, liveStrictSendResponse)
	defer srv.Close()

	e := &Engine{DEX: &dex.Client{HorizonURL: srv.URL}, RefRate: usdToNGN("660")}
	res, err := e.Quote(context.Background(), ngnRequest("100"))
	if err != nil {
		t.Fatalf("Quote: %v", err)
	}

	warnings := strings.Join(res.Quotes[0].Warnings, " ")
	if !strings.Contains(warnings, "not naira in a bank account") {
		t.Errorf("expected a token-delivery warning, got: %v", res.Quotes[0].Warnings)
	}
}

// TestReferenceRateIsRequired pins the engine's central dependency. Degrading
// to "rank without scoring" when the reference rate is missing is exactly the
// behaviour that would let a 56% loss be presented as a winner.
func TestReferenceRateIsRequired(t *testing.T) {
	e := &Engine{DEX: &dex.Client{HorizonURL: "http://example.invalid"}}
	if _, err := e.Quote(context.Background(), ngnRequest("100")); err == nil {
		t.Fatal("expected Quote to fail without a reference rate provider")
	}
}

// TestUnknownReferencePairFails checks that a missing rate is an error rather
// than a silent zero, which would make every route look infinitely good.
func TestUnknownReferencePairFails(t *testing.T) {
	srv := horizonStub(t, liveStrictSendResponse)
	defer srv.Close()

	e := &Engine{
		DEX:     &dex.Client{HorizonURL: srv.URL},
		RefRate: refrate.NewStatic(map[string]decimal.Decimal{"EUR/GBP": decimal.NewFromInt(1)}),
	}
	if _, err := e.Quote(context.Background(), ngnRequest("100")); err == nil {
		t.Fatal("expected an error when the reference pair is unavailable")
	}
}

func TestRejectsNonPositiveAmount(t *testing.T) {
	e := &Engine{RefRate: usdToNGN("1500")}
	for _, amt := range []string{"0", "-5"} {
		if _, err := e.Quote(context.Background(), ngnRequest(amt)); err == nil {
			t.Errorf("amount %s: expected an error", amt)
		}
	}
}

func TestVerdictThresholds(t *testing.T) {
	cases := []struct {
		loss string
		want Verdict
	}{
		{"0", VerdictGood},
		{"3", VerdictGood},
		{"3.01", VerdictFair},
		{"8", VerdictFair},
		{"8.01", VerdictPoor},
		{"20", VerdictPoor},
		{"20.01", VerdictUnusable},
		{"56.6", VerdictUnusable},
	}
	for _, c := range cases {
		if got := verdictFor(decimal.RequireFromString(c.loss)); got != c.want {
			t.Errorf("verdictFor(%s) = %s, want %s", c.loss, got, c.want)
		}
	}
}

// TestNoPathsProducesNoQuotes covers a corridor Horizon cannot route at all.
func TestNoPathsProducesNoQuotes(t *testing.T) {
	srv := horizonStub(t, `{"_embedded":{"records":[]}}`)
	defer srv.Close()

	e := &Engine{DEX: &dex.Client{HorizonURL: srv.URL}, RefRate: usdToNGN("1500")}
	res, err := e.Quote(context.Background(), ngnRequest("100"))
	if err != nil {
		t.Fatalf("Quote: %v", err)
	}
	if res.Viable() || len(res.Quotes) != 0 {
		t.Fatal("expected no quotes and no recommendation")
	}
	if len(res.Notes) == 0 {
		t.Error("expected a note explaining that nothing could be priced")
	}
}
