// Command ladder prices a corridor at a range of sizes and reports the
// effective rate, loss against the reference mid, and verdict at each.
//
// It exists to answer one question: is the loss on this corridor
// size-dependent, or is it structural? The answer decides what the product is.
//
//	go run ./cmd/ladder                  # USDC -> NGNC
//	go run ./cmd/ladder -to GHSC         # USDC -> GHSC, benchmarked against GHS
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/Fury03/wayfare/asset"
	"github.com/Fury03/wayfare/dex"
	"github.com/Fury03/wayfare/refrate"
	"github.com/Fury03/wayfare/route"
)

// corridor names a destination token and the fiat currency it claims to track.
// The reference pair is the token's peg, not the token itself — nobody
// publishes a mid-market rate for "NGNC".
type corridor struct {
	dest    asset.Asset
	refPair string // ISO-4217 code of the fiat the token is pegged to
}

var corridors = map[string]corridor{
	"NGNC": {asset.NGNC(), "NGN"},
	"GHSC": {asset.GHSC(), "GHS"},
	"KESC": {asset.KESC(), "KES"},
}

func main() {
	var (
		to        = flag.String("to", "NGNC", "destination asset code (NGNC, GHSC, KESC)")
		sizesFlag = flag.String("sizes", "0.1,1,5,10,25,50,100,250,500,1000,2500,5000",
			"comma-separated send amounts in USDC")
	)
	flag.Parse()

	c, ok := corridors[strings.ToUpper(*to)]
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown destination %q; known: NGNC, GHSC, KESC\n", *to)
		os.Exit(2)
	}

	eng := &route.Engine{
		DEX:     &dex.Client{},
		RefRate: &refrate.Checked{Inner: &refrate.ExchangeRateAPI{}},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	fmt.Printf("corridor USDC -> %s, benchmarked against USD/%s\n", c.dest.Code, c.refPair)
	fmt.Printf("run at %s\n\n", time.Now().UTC().Format(time.RFC3339))
	fmt.Printf("%-8s %14s %12s %9s %-10s %s\n",
		"SEND", "RECEIVE", "RATE", "LOSS%", "VERDICT", "PATH")

	priced := 0
	for _, s := range strings.Split(*sizesFlag, ",") {
		s = strings.TrimSpace(s)
		amt, err := decimal.NewFromString(s)
		if err != nil {
			fmt.Fprintf(os.Stderr, "bad size %q: %v\n", s, err)
			continue
		}

		res, err := eng.Quote(ctx, route.Request{
			SendAsset:      asset.USDC(),
			SendAmount:     amt,
			ReceiveAsset:   c.dest,
			ReferenceBase:  "USD",
			ReferenceQuote: c.refPair,
		})
		if err != nil {
			fmt.Printf("%-8s  ERROR: %v\n", s, err)
			continue
		}

		if len(res.Quotes) == 0 {
			fmt.Printf("%-8s %14s %12s %9s %-10s %s\n",
				s, "-", "-", "-", "NO ROUTE", strings.Join(res.Notes, "; "))
			continue
		}

		priced++
		q := res.Quotes[0]
		fmt.Printf("%-8s %14s %12s %9s %-10s %s\n",
			s,
			q.ReceiveAmount.StringFixed(2),
			q.EffectiveRate.StringFixed(2),
			q.LossPct.StringFixed(2),
			q.Verdict.String(),
			q.Description,
		)
		for _, w := range q.Warnings {
			fmt.Printf("%-8s   warn: %s\n", "", w)
		}
		if res.Recommended == nil {
			fmt.Printf("%-8s   (engine recommends nothing at this size)\n", "")
		}
	}

	if r, err := (&refrate.ExchangeRateAPI{}).Rate(ctx, "USD", c.refPair); err == nil {
		fmt.Printf("\nreference mid: %s USD/%s via %s, as of %s\n",
			r.Mid.StringFixed(4), c.refPair, r.Source, r.AsOf.UTC().Format(time.RFC3339))
	}
	if priced == 0 {
		fmt.Printf("no size could be priced for USDC -> %s\n", c.dest.Code)
	}
}
