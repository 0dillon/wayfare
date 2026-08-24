// DepthMetric reports observed depth and executable depth separately for a
// corridor, and never collapses them into one number.
//
// A quote is not proof of liquidity: a market can quote a price for $100
// with almost nothing behind it. Observed depth is what the book advertises.
// Executable depth is what a payment would actually fill against.
//
// This produces two MetricResults, reported side by side. A single merged
// "depth" number hides which of the two moved.
//
// These are metrics, not checks. No threshold exists yet.
package checks

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/dex"
)

// DepthMetric measures observed and executable depth for a corridor.
type DepthMetric struct {
	DEX   *dex.Client
	Sizes []decimal.Decimal
}

var defaultDepthSizes = []decimal.Decimal{
	decimal.NewFromInt(1),
	decimal.NewFromInt(10),
	decimal.NewFromInt(100),
	decimal.NewFromInt(1000),
	decimal.NewFromInt(5000),
}

// Describe implements Metric.
func (DepthMetric) Describe() Descriptor {
	return Descriptor{
		ID:    "depth.observed-executable",
		Scope: ScopeCorridor,
		Cost:  CostExpensive,
		Title: "Observed depth vs executable depth",
		CanDetermine: "Observed depth from the order book (number of levels " +
			"on each side), and executable depth from pathfinding across " +
			"multiple sizes (the maximum destination amount reachable).",
		CannotDetermine: "Whether those levels represent executable liquidity " +
			"or stale offers.",
	}
}

// RunObserved returns the observed depth metric from the order book.
func (m DepthMetric) RunObserved(ctx context.Context, s Subject) MetricResult {
	d := Descriptor{
		ID:           "depth.observed",
		Scope:        ScopeCorridor,
		Cost:         CostOneRequest,
		Title:        "Observed order book depth",
		CanDetermine: "The number of bid and ask levels on the direct order book.",
		CannotDetermine: "Whether those levels represent executable liquidity " +
			"or stale offers.",
	}
	at := time.Now().UTC()

	if s.Send.Code == "" || s.Receive.Code == "" {
		return MetricUndetermined(d, s, "no send or receive asset specified")
	}
	if m.DEX == nil {
		return MetricUndetermined(d, s, "no DEX client available")
	}

	h, err := m.DEX.OrderBook(ctx, s.Send, s.Receive)
	if err != nil {
		return MetricUndetermined(d, s,
			fmt.Sprintf("order book fetch failed: %v", err))
	}

	evidence := Evidence{
		Source:     fmt.Sprintf("/order_book %s/%s", s.Send.Code, s.Receive.Code),
		ObservedAt: at,
	}

	if h.BidLevels == 0 && h.AskLevels == 0 {
		evidence.Observed = "no levels"
		return MetricUndetermined(d, s, "order book is empty", evidence)
	}

	total := h.BidLevels + h.AskLevels
	evidence.Observed = fmt.Sprintf("bids=%d, asks=%d, total=%d",
		h.BidLevels, h.AskLevels, total)

	summary := fmt.Sprintf(
		"%d observed levels: %d bids, %d asks",
		total, h.BidLevels, h.AskLevels)

	return MetricValue(d, s, decimal.NewFromInt(int64(total)), UnitCount, summary, evidence)
}

// RunExecutable returns the executable depth metric from pathfinding.
func (m DepthMetric) RunExecutable(ctx context.Context, s Subject) MetricResult {
	d := Descriptor{
		ID:    "depth.executable",
		Scope: ScopeCorridor,
		Cost:  CostExpensive,
		Title: "Executable depth from pathfinding",
		CanDetermine: "The maximum destination amount reachable via Horizon " +
			"pathfinding, and the size at which the receive amount stops growing.",
		CannotDetermine: "Whether the executable amount reflects a sustainable " +
			"market or a one-time fill.",
	}
	at := time.Now().UTC()

	if s.Send.Code == "" || s.Receive.Code == "" {
		return MetricUndetermined(d, s, "no send or receive asset specified")
	}
	if m.DEX == nil {
		return MetricUndetermined(d, s, "no DEX client available")
	}

	sizes := m.Sizes
	if len(sizes) == 0 {
		sizes = defaultDepthSizes
	}

	evidence := Evidence{
		Source:     fmt.Sprintf("/paths/strict-send %s/%s", s.Send.Code, s.Receive.Code),
		ObservedAt: at,
	}

	var maxReceive decimal.Decimal
	var maxReceiveSize decimal.Decimal
	pricedCount := 0
	var probeErrors []string

	for _, size := range sizes {
		path, err := m.DEX.BestPath(ctx, s.Send, size, s.Receive)
		if err != nil {
			if strings.Contains(err.Error(), "no path found") {
				continue // no liquidity at this size, not a probe failure
			}
			probeErrors = append(probeErrors, fmt.Sprintf("size=%s: %v", size, err))
			continue
		}
		if path == nil {
			continue
		}
		pricedCount++
		if path.DestAmount.GreaterThan(maxReceive) {
			maxReceive = path.DestAmount
			maxReceiveSize = size
		}
	}

	if len(probeErrors) > 0 {
		evidence.Observed = fmt.Sprintf("probed %d sizes, %d errors: %s",
			len(sizes), len(probeErrors), strings.Join(probeErrors, "; "))
		return MetricUndetermined(d, s,
			fmt.Sprintf("%d of %d probe requests failed", len(probeErrors), len(sizes)), evidence)
	}

	if pricedCount == 0 {
		evidence.Observed = fmt.Sprintf("probed %d sizes, no path found", len(sizes))
		return MetricUndetermined(d, s,
			fmt.Sprintf("no path found at any of the %d sizes probed", len(sizes)), evidence)
	}

	evidence.Observed = fmt.Sprintf(
		"max_receive=%s at size=%s, priced_at=%d/%d_sizes",
		maxReceive, maxReceiveSize, pricedCount, len(sizes))

	summary := fmt.Sprintf(
		"max destination %s %s at %s %s send",
		maxReceive, s.Receive.Code, maxReceiveSize, s.Send.Code)

	return MetricValue(d, s, maxReceive, UnitAmount, summary, evidence)
}

// Run implements the Metric interface by returning the observed depth metric.
func (m DepthMetric) Run(ctx context.Context, s Subject) MetricResult {
	return m.RunObserved(ctx, s)
}
