// Package api serves the corridor monitor over HTTP.
//
// The wire shape for a measured corridor lives in package route
// (route.CorridorJSON / route.ToCorridorJSON) so that this HTTP handler and
// cmd/ladder's -json mode emit identical JSON from the same conversion code.
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

	pegQuote, ok := asset.FiatPeg(recvAsset)
	if !ok {
		writeError(w, http.StatusBadRequest, fmt.Sprintf(
			"no verified fiat peg for %s, so there is no independent rate to score it against",
			recvAsset.Code))
		return
	}
	pegBase, ok := asset.FiatPeg(sendAsset)
	if !ok {
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

	writeJSON(w, http.StatusOK, route.ToCorridorJSON(res, pegBase+"/"+pegQuote))
}

func (s *Server) handleAssets(w http.ResponseWriter, r *http.Request) {
	type entry struct {
		route.AssetJSON
		Corridor bool `json:"can_be_destination"`
	}
	out := make([]entry, 0)
	for _, code := range asset.KnownCodes() {
		a, _ := asset.Lookup(code)
		_, hasPeg := asset.FiatPeg(a)
		out = append(out, entry{AssetJSON: route.ToAssetJSON(a), Corridor: hasPeg})
	}
	writeJSON(w, http.StatusOK, map[string]any{"assets": out})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// helpers --------------------------------------------------------------------

const maxSizes = 24

func param(r *http.Request, key, fallback string) string {
	if v := strings.TrimSpace(r.URL.Query().Get(key)); v != "" {
		return v
	}
	return fallback
}

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
