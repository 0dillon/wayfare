package route

import (
	"time"

	"github.com/Wayfare-labs/wayfare/asset"
)

// Wire types for a measured corridor. These are the single JSON contract
// shared by the HTTP API (api.Server) and cmd/ladder's -json mode. A second,
// independently-maintained shape for the same measurement is exactly the
// kind of drift this project exists to catch, so there is only one.
//
// Money crosses the wire as decimal strings. Serialising a rate as a JSON
// number invites a client to parse it into a float64, which is the same
// rounding bug this project refuses internally.

type QuoteJSON struct {
	Description   string   `json:"description"`
	Source        string   `json:"source"`
	ReceiveAmount string   `json:"receive_amount"`
	EffectiveRate string   `json:"effective_rate"`
	LossPct       string   `json:"loss_pct"`
	LossAmount    string   `json:"loss_amount"`
	Verdict       string   `json:"verdict"`
	Warnings      []string `json:"warnings"`
}

type RungJSON struct {
	SendAmount string     `json:"send_amount"`
	Priced     bool       `json:"priced"`
	Integrity  string     `json:"integrity"`
	Quote      *QuoteJSON `json:"quote"`
	Notes      []string   `json:"notes"`
	Error      string     `json:"error,omitempty"`
}

type CorridorJSON struct {
	SendAsset    AssetJSON `json:"send_asset"`
	ReceiveAsset AssetJSON `json:"receive_asset"`

	Integrity string      `json:"integrity"`
	DependsOn []AssetJSON `json:"depends_on"`

	ReferenceMid    string `json:"reference_mid"`
	ReferenceSource string `json:"reference_source"`
	ReferencePair   string `json:"reference_pair"`

	Floor     string `json:"floor_loss_pct"`
	FloorSize string `json:"floor_size"`
	WorstLoss string `json:"worst_loss_pct"`
	WorstSize string `json:"worst_size"`

	Recommended     *QuoteJSON `json:"recommended"`
	RecommendedSize string     `json:"recommended_size,omitempty"`

	Finding    string     `json:"finding"`
	Rungs      []RungJSON `json:"rungs"`
	MeasuredAt string     `json:"measured_at"`
}

type AssetJSON struct {
	Code   string `json:"code"`
	Issuer string `json:"issuer,omitempty"`
	Peg    string `json:"peg,omitempty"`
}

func ToAssetJSON(a asset.Asset) AssetJSON {
	j := AssetJSON{Code: a.Code, Issuer: a.Issuer}
	if peg, ok := asset.FiatPeg(a); ok {
		j.Peg = peg
	}
	return j
}

func ToQuoteJSON(q *Quote) *QuoteJSON {
	if q == nil {
		return nil
	}
	w := q.Warnings
	if w == nil {
		w = []string{}
	}
	return &QuoteJSON{
		Description:   q.Description,
		Source:        q.Source,
		ReceiveAmount: q.ReceiveAmount.String(),
		EffectiveRate: q.EffectiveRate.String(),
		LossPct:       q.LossPct.StringFixed(2),
		LossAmount:    q.LossAmount.StringFixed(2),
		Verdict:       q.Verdict.String(),
		Warnings:      w,
	}
}

func ToCorridorJSON(l *LadderResult, pair string) CorridorJSON {
	out := CorridorJSON{
		SendAsset:       ToAssetJSON(l.Request.SendAsset),
		ReceiveAsset:    ToAssetJSON(l.Request.ReceiveAsset),
		Integrity:       l.Integrity.String(),
		DependsOn:       []AssetJSON{},
		ReferenceMid:    l.ReferenceMid.String(),
		ReferenceSource: l.ReferenceSource,
		ReferencePair:   pair,
		Floor:           l.Floor.StringFixed(2),
		FloorSize:       l.FloorSize.String(),
		WorstLoss:       l.WorstLoss.StringFixed(2),
		WorstSize:       l.WorstSize.String(),
		Recommended:     ToQuoteJSON(l.Recommended),
		Finding:         l.Finding,
		Rungs:           make([]RungJSON, 0, len(l.Rungs)),
		MeasuredAt:      time.Now().UTC().Format(time.RFC3339),
	}
	if l.Recommended != nil {
		out.RecommendedSize = l.RecommendedSize.String()
	}
	for _, d := range l.DependsOn {
		out.DependsOn = append(out.DependsOn, ToAssetJSON(d))
	}

	for _, r := range l.Rungs {
		rj := RungJSON{
			SendAmount: r.SendAmount.String(),
			Priced:     r.Priced(),
			Integrity:  IntegrityUnknown.String(),
			Notes:      []string{},
		}
		if r.Err != nil {
			rj.Error = r.Err.Error()
		}
		if r.Result != nil {
			rj.Integrity = r.Result.Integrity.String()
			if r.Result.Notes != nil {
				rj.Notes = r.Result.Notes
			}
			if len(r.Result.Quotes) > 0 {
				rj.Quote = ToQuoteJSON(&r.Result.Quotes[0])
			}
		}
		out.Rungs = append(out.Rungs, rj)
	}
	return out
}
