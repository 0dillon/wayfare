// Package api serves the corridor monitor over HTTP.
//
// The wire types here are declared explicitly rather than by marshalling the
// engine's own structs. A JSON contract that tracks internal field names
// changes whenever the internals do, and the fields that matter most on this
// endpoint — the integrity state, and the null recommendation on a broken
// corridor — are exactly the ones a client must be able to rely on.
//
// Money crosses the wire as decimal strings. Serialising a rate as a JSON
// number invites a client to parse it into a float64, which is the same
// rounding bug this project refuses internally.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/route"
)

// Server serves the monitor API and its UI.
type Server struct {
	Engine *route.Engine

	// Timeout bounds a single corridor measurement. A full ladder is a
	// dozen round trips to Horizon, so this is generous by HTTP standards.
	Timeout time.Duration
}

func (s *Server) timeout() time.Duration {
	if s.Timeout > 0 {
		return s.Timeout
	}
	return 90 * time.Second
}

// Handler returns the routed handler for the whole service.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/corridor", s.handleCorridor)
	mux.HandleFunc("/api/assets", s.handleAssets)
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.Handle("/", uiHandler())
	return mux
}

// wire types -----------------------------------------------------------------

type quoteJSON struct {
	Description   string   `json:"description"`
	Source        string   `json:"source"`
	ReceiveAmount string   `json:"receive_amount"`
	EffectiveRate string   `json:"effective_rate"`
	LossPct       string   `json:"loss_pct"`
	LossAmount    string   `json:"loss_amount"`
	Verdict       string   `json:"verdict"`
	Warnings      []string `json:"warnings"`
}

type rungJSON struct {
	SendAmount string     `json:"send_amount"`
	Priced     bool       `json:"priced"`
	Integrity  string     `json:"integrity"`
	Quote      *quoteJSON `json:"quote"`
	Notes      []string   `json:"notes"`
	Error      string     `json:"error,omitempty"`
}

type corridorJSON struct {
	SendAsset    assetJSON `json:"send_asset"`
	ReceiveAsset assetJSON `json:"receive_asset"`

	// Integrity is DIRECT, DERIVATIVE, NO-MARKET or UNKNOWN. A client that
	// renders only the loss curve will misreport the last two.
	Integrity string      `json:"integrity"`
	DependsOn []assetJSON `json:"depends_on"`

	ReferenceMid    string `json:"reference_mid"`
	ReferenceSource string `json:"reference_source"`
	ReferencePair   string `json:"reference_pair"`

	Floor     string `json:"floor_loss_pct"`
	FloorSize string `json:"floor_size"`
	WorstLoss string `json:"worst_loss_pct"`
	WorstSize string `json:"worst_size"`

	// Recommended is null when no size produced an acceptable route, which
	// is the normal outcome on a broken corridor. Clients must render null
	// as "none", never fall back to the best-scoring quote.
	Recommended     *quoteJSON `json:"recommended"`
	RecommendedSize string     `json:"recommended_size,omitempty"`

	Finding    string     `json:"finding"`
	Rungs      []rungJSON `json:"rungs"`
	MeasuredAt string     `json:"measured_at"`
}

type assetJSON struct {
	Code   string `json:"code"`
	Issuer string `json:"issuer,omitempty"`
	Peg    string `json:"peg,omitempty"`
}

func toAssetJSON(a asset.Asset) assetJSON {
	j := assetJSON{Code: a.Code, Issuer: a.Issuer}
	if peg, ok := asset.FiatPeg(a); ok {
		j.Peg = peg
	}
	return j
}

func toQuoteJSON(q *route.Quote) *quoteJSON {
	if q == nil {
		return nil
	}
	w := q.Warnings
	if w == nil {
		w = []string{}
	}
	return &quoteJSON{
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

func toCorridorJSON(l *route.LadderResult, pair string) corridorJSON {
	out := corridorJSON{
		SendAsset:       toAssetJSON(l.Request.SendAsset),
		ReceiveAsset:    toAssetJSON(l.Request.ReceiveAsset),
		Integrity:       l.Integrity.String(),
		DependsOn:       []assetJSON{},
		ReferenceMid:    l.ReferenceMid.String(),
		ReferenceSource: l.ReferenceSource,
		ReferencePair:   pair,
		Floor:           l.Floor.StringFixed(2),
		FloorSize:       l.FloorSize.String(),
		WorstLoss:       l.WorstLoss.StringFixed(2),
		WorstSize:       l.WorstSize.String(),
		Recommended:     toQuoteJSON(l.Recommended),
		Finding:         l.Finding,
		Rungs:           make([]rungJSON, 0, len(l.Rungs)),
		MeasuredAt:      time.Now().UTC().Format(time.RFC3339),
	}
	if l.Recommended != nil {
		out.RecommendedSize = l.RecommendedSize.String()
	}
	for _, d := range l.DependsOn {
		out.DependsOn = append(out.DependsOn, toAssetJSON(d))
	}

	for _, r := range l.Rungs {
		rj := rungJSON{
			SendAmount: r.SendAmount.String(),
			Priced:     r.Priced(),
			Integrity:  route.IntegrityUnknown.String(),
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
				rj.Quote = toQuoteJSON(&r.Result.Quotes[0])
			}
		}
		out.Rungs = append(out.Rungs, rj)
	}
	return out
}

// handlers -------------------------------------------------------------------

func (s *Server) handleCorridor(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "only GET is supported")
		return
	}

	from := param(r, "from", "USDC")
	to := param(r, "to", "NGNC")

	sendAsset, ok := asset.Lookup(from)
	if !ok {
		writeError(w, http.StatusBadRequest, fmt.Sprintf(
			"unknown send asset %q; verified assets are %s",
			from, strings.Join(asset.KnownCodes(), ", ")))
		return
	}
	recvAsset, ok := asset.Lookup(to)
	if !ok {
		writeError(w, http.StatusBadRequest, fmt.Sprintf(
			"unknown receive asset %q; verified assets are %s",
			to, strings.Join(asset.KnownCodes(), ", ")))
		return
	}

	// The benchmark is the destination token's peg. A token with no
	// verified peg has nothing to be scored against, and scoring it against
	// a guess is precisely the failure this tool exists to catch.
	pegQuote, ok := asset.FiatPeg(recvAsset)
	if !ok {
		writeError(w, http.StatusBadRequest, fmt.Sprintf(
			"no verified fiat peg for %s, so there is no independent rate to score it against",
			recvAsset.Code))
		return
	}
	pegBase, ok := asset.FiatPeg(sendAsset)
	if !ok {
		// USDC has no fiat peg entry; it is the dollar leg by construction.
		pegBase = "USD"
	}

	sizes, err := parseSizes(r.URL.Query().Get("sizes"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.timeout())
	defer cancel()

	res, err := s.Engine.Ladder(ctx, route.LadderRequest{
		SendAsset:      sendAsset,
		ReceiveAsset:   recvAsset,
		Sizes:          sizes,
		ReferenceBase:  pegBase,
		ReferenceQuote: pegQuote,
	})
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, context.DeadlineExceeded) {
			status = http.StatusGatewayTimeout
		}
		writeError(w, status, "measuring corridor: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, toCorridorJSON(res, pegBase+"/"+pegQuote))
}

func (s *Server) handleAssets(w http.ResponseWriter, r *http.Request) {
	type entry struct {
		assetJSON
		Corridor bool `json:"can_be_destination"`
	}
	out := make([]entry, 0)
	for _, code := range asset.KnownCodes() {
		a, _ := asset.Lookup(code)
		_, hasPeg := asset.FiatPeg(a)
		out = append(out, entry{assetJSON: toAssetJSON(a), Corridor: hasPeg})
	}
	writeJSON(w, http.StatusOK, map[string]any{"assets": out})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// helpers --------------------------------------------------------------------

// maxSizes bounds a request's ladder. Each size is a separate round trip to
// Horizon, so an unbounded list would let one request generate arbitrary load
// on a shared public service.
const maxSizes = 24

func param(r *http.Request, key, fallback string) string {
	if v := strings.TrimSpace(r.URL.Query().Get(key)); v != "" {
		return v
	}
	return fallback
}

// parseSizes reads the optional sizes parameter. Empty means the default
// ladder.
func parseSizes(raw string) ([]decimal.Decimal, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	if len(parts) > maxSizes {
		return nil, fmt.Errorf("too many sizes: %d requested, limit is %d", len(parts), maxSizes)
	}
	out := make([]decimal.Decimal, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		d, err := decimal.NewFromString(p)
		if err != nil {
			return nil, fmt.Errorf("bad size %q: not a number", p)
		}
		if !d.IsPositive() {
			return nil, fmt.Errorf("bad size %q: must be positive", p)
		}
		out = append(out, d)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
