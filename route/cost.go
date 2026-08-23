// CostDecomposition breaks the effective transfer cost into separately-reported
// components: FX loss, fees, slippage, and expected failure cost.
//
// Currently, the verdict reports a single loss percentage against fair value.
// That number is useful but opaque. Showing the decomposition turns a single
// verdict into actionable information.
//
// Each component is computed and reported independently. Expected failure cost
// stays explicitly unknown until failure history exists.
package route

import (
	"github.com/shopspring/decimal"
)

// CostComponent names one piece of the effective transfer cost.
type CostComponent string

const (
	CostFXLoss          CostComponent = "fx_loss"
	CostFees            CostComponent = "fees"
	CostSlippage        CostComponent = "slippage"
	CostExpectedFailure CostComponent = "expected_failure"
)

// CostPart is one component of the effective transfer cost.
type CostPart struct {
	Component  CostComponent
	Amount     decimal.Decimal
	Pct        decimal.Decimal
	Determined bool
	Reason     string
}

// CostDecomposition is the full breakdown of a route's effective transfer cost.
type CostDecomposition struct {
	Parts        []CostPart
	TotalLossPct decimal.Decimal
}

// Decompose splits a priced route's effective transfer cost into components.
func Decompose(q Quote, mid decimal.Decimal) CostDecomposition {
	parts := make([]CostPart, 0, 4)

	// FX loss: difference between effective rate and mid, as a percentage.
	fxLossPct := q.LossPct
	fxLossAmount := q.LossAmount
	parts = append(parts, CostPart{
		Component:  CostFXLoss,
		Amount:     fxLossAmount,
		Pct:        fxLossPct,
		Determined: true,
	})

	// Fees: negligible for DEX routes (Stellar network fee ~0.00001 XLM).
	parts = append(parts, CostPart{
		Component:  CostFees,
		Amount:     decimal.Zero,
		Pct:        decimal.Zero,
		Determined: true,
	})

	// Slippage: undetermined without a comparison across sizes.
	parts = append(parts, CostPart{
		Component:  CostSlippage,
		Amount:     decimal.Zero,
		Pct:        decimal.Zero,
		Determined: false,
		Reason:     "single quote available; slippage requires a comparison across sizes",
	})

	// Expected failure cost: explicitly unknown.
	parts = append(parts, CostPart{
		Component:  CostExpectedFailure,
		Amount:     decimal.Zero,
		Pct:        decimal.Zero,
		Determined: false,
		Reason:     "no failure history exists yet; runstore is collecting but has not accumulated enough observations",
	})

	return CostDecomposition{
		Parts:        parts,
		TotalLossPct: q.LossPct,
	}
}
