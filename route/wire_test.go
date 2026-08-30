package route

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Wayfare-labs/wayfare/asset"
)

// TestToAssetJSONPublishesSEP38WireForm pins GitHub issue #178: the README's
// "Asset identity" section specifies the wire form as stellar:CODE:ISSUER,
// stellar:native, or iso4217:CODE, and AssetJSON must carry it — not just the
// separate code and issuer fields the shape already had.
//
// Each case round-trips through asset.ParseSEP38 back to an equal asset.Asset,
// which is what makes this test able to fail: a wrong or dropped Asset field
// fails to parse, or parses back to a different asset than the one that went
// in.
func TestToAssetJSONPublishesSEP38WireForm(t *testing.T) {
	cases := []struct {
		name string
		a    asset.Asset
		want string
	}{
		{"issued Stellar asset", asset.USDC(), "stellar:USDC:" + asset.USDCIssuer},
		{"native XLM", asset.Native(), "stellar:native"},
		{"fiat", asset.Fiat("NGN"), "iso4217:NGN"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			j := ToAssetJSON(tc.a)

			if j.Asset != tc.want {
				t.Fatalf("Asset = %q, want %q", j.Asset, tc.want)
			}
			if j.Code != tc.a.Code {
				t.Errorf("Code = %q, want %q — the separate field must survive alongside the new one",
					j.Code, tc.a.Code)
			}
			if j.Issuer != tc.a.Issuer {
				t.Errorf("Issuer = %q, want %q", j.Issuer, tc.a.Issuer)
			}

			back, err := asset.ParseSEP38(j.Asset)
			if err != nil {
				t.Fatalf("asset.ParseSEP38(%q) failed: %v", j.Asset, err)
			}
			if !back.Equal(tc.a) {
				t.Errorf("round trip produced %+v, want %+v", back, tc.a)
			}
		})
	}
}

// TestAssetJSONMarshalsTheWireFormField covers the actual bytes on the wire,
// not just the Go struct: a client reading raw JSON must see an "asset" key
// carrying the SEP-38 string.
func TestAssetJSONMarshalsTheWireFormField(t *testing.T) {
	j := ToAssetJSON(asset.NGNC())

	buf, err := json.Marshal(j)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(buf, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	got, ok := m["asset"]
	if !ok {
		t.Fatal(`marshaled AssetJSON has no "asset" key`)
	}
	want := "stellar:NGNC:" + asset.LinkIOIssuer
	if got != want {
		t.Errorf(`"asset" = %v, want %q`, got, want)
	}
	if !strings.Contains(string(buf), `"code":"NGNC"`) {
		t.Errorf("marshaled output %s no longer carries the separate code field", buf)
	}
}

// TestAssetJSONZeroValueOmitsTheWireForm covers a producer that has only a
// bare code to work from — e.g. a stale-path fallback for an asset the
// verified registry does not recognise — where Kind and Issuer are not known.
// The wire form must be absent rather than a guess built from a bare code,
// per the project's rule that an unavailable identity is never synthesised.
func TestAssetJSONZeroValueOmitsTheWireForm(t *testing.T) {
	j := AssetJSON{Code: "UNKNOWN"}

	buf, err := json.Marshal(j)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(buf), `"asset"`) {
		t.Errorf("marshaled output %s carries an \"asset\" key with no verified identity to back it", buf)
	}
}
