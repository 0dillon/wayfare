// Command ladder prices a corridor at a range of sizes and reports the
// effective rate, loss against the reference mid, and verdict at each.
//
// It exists to answer one question: is the loss on this corridor
// size-dependent, or is it structural? The answer decides what the product is.
//
//	go run ./cmd/ladder                  # USDC -> NGNC
//	go run ./cmd/ladder -to GHSC         # USDC -> GHSC, benchmarked against GHS
//	go run ./cmd/ladder -to GHSC -json   # same, as JSON on stdout
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/dex"
	"github.com/Wayfare-labs/wayfare/refrate"
	"github.com/Wayfare-labs/wayfare/route"
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
		jsonOut = flag.Bool("json", false, "emit JSON on stdout instead of the text table")
	)
	flag.Parse()

	c, ok := corridors[strings.ToUpper(*to)]
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown destination %q; known: NGNC, GHSC, KESC\n", *to)
		os.Exit(2)
	}

	sizes, err := parseSizes(*sizesFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	eng := &route.Engine{
		DEX:     &dex.Client{},
		RefRate: &refrate.Checked{Inner: &refrate.ExchangeRateAPI{}},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	result, err := eng.Ladder(ctx, route.LadderRequest{
		SendAsset:      asset.USDC(),
		ReceiveAsset:   c.dest,
		Sizes:          sizes,
		ReferenceBase:  "USD",
		ReferenceQuote: c.refPair,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "measuring corridor: %v\n", err)
		os.Exit(1)
	}

	if *jsonOut {
		printJSON(result, "USD/"+c.refPair)
	} else {
		printTable(ctx, result, c)
	}

	// A ladder with no recommended size at any point is the normal shape of
	// a broken corridor, not an error — but it is a fact scripts need to be
	// able to detect without parsing prose, so the exit code carries it.
	if !result.Viable() {
		os.Exit(1)
	}
}

// parseSizes reads the -sizes flag. A malformed entry is reported and
// skipped rather than aborting the whole ladder, matching the tool's
// long-standing behaviour of measuring what it can.
func parseSizes(raw string) ([]decimal.Decimal, error) {
	var out []decimal.Decimal
	for _, s := range strings.Split(raw, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		amt, err := decimal.NewFromString(s)
		if err != nil {
			fmt.Fprintf(os.Stderr, "bad size %q: %v\n", s, err)
			continue
		}
		out = append(out, amt)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no valid sizes in %q", raw)
	}
	return out, nil
}

// printJSON writes the shared wire shape and nothing else to stdout, so
// `go run ./cmd/ladder -to GHSC -json | jq` works.
func printJSON(result *route.LadderResult, pair string) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(route.ToCorridorJSON(result, pair)); err != nil {
		// Encoding a well-formed struct to stdout should not fail; if it
		// does, say so on stderr rather than emitting partial JSON.
		fmt.Fprintf(os.Stderr, "encoding result: %v\n", err)
		os.Exit(1)
	}
}

// printTable renders the human-readable text table. This is the default
// output and its format is unchanged from before -json existed.
func printTable(ctx context.Context, result *route.LadderResult, c corridor) {
	fmt.Printf("corridor USDC -> %s, benchmarked against USD/%s\n", c.dest.Code, c.refPair)
	fmt.Printf("run at %s\n\n", time.Now().UTC().Format(time.RFC3339))
	fmt.Printf("%-8s %14s %12s %9s %-10s %-11s %s\n",
		"SEND", "RECEIVE", "RATE", "LOSS%", "VERDICT", "INTEGRITY", "PATH")

	priced := 0
	for _, r := range result.Rungs {
		s := r.SendAmount.String()

		if r.Err != nil {
			fmt.Printf("%-8s  ERROR: %v\n", s, r.Err)
			continue
		}
		if r.Result == nil || len(r.Result.Quotes) == 0 {
			integrity, notes := "-", ""
			if r.Result != nil {
				integrity = r.Result.Integrity.String()
				notes = strings.Join(r.Result.Notes, "; ")
			}
			fmt.Printf("%-8s %14s %12s %9s %-10s %-11s %s\n",
				s, "-", "-", "-", "-", integrity, notes)
			continue
		}

		priced++
		q := r.Result.Quotes[0]
		fmt.Printf("%-8s %14s %12s %9s %-10s %-11s %s\n",
			s,
			q.ReceiveAmount.StringFixed(2),
			q.EffectiveRate.StringFixed(2),
			q.LossPct.StringFixed(2),
			q.Verdict.String(),
			r.Result.Integrity.String(),
			q.Description,
		)
		for _, w := range q.Warnings {
			fmt.Printf("%-8s   warn: %s\n", "", w)
		}
		if r.Result.Recommended == nil {
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

