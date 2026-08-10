package asset

import (
	"reflect"
	"testing"
)

// TestLookup covers case-insensitive resolution of a verified code and that
// an unrecognised code is reported as absent rather than guessed at.
func TestLookup(t *testing.T) {
	cases := []struct {
		code string
		want Asset
		ok   bool
	}{
		{"USDC", USDC(), true},
		{"usdc", USDC(), true},
		{" NgNc ", NGNC(), true},
		{"NOTREAL", Asset{}, false},
		{"", Asset{}, false},
	}
	for _, c := range cases {
		got, ok := Lookup(c.code)
		if ok != c.ok {
			t.Errorf("Lookup(%q) ok = %v, want %v", c.code, ok, c.ok)
			continue
		}
		if ok && !got.Equal(c.want) {
			t.Errorf("Lookup(%q) = %+v, want %+v", c.code, got, c.want)
		}
	}
}

// TestKnownCodes pins that the list is sorted, since callers render it
// directly (e.g. in CLI help output) and an unsorted map iteration order
// would make that output nondeterministic.
func TestKnownCodes(t *testing.T) {
	got := KnownCodes()
	want := []string{"GHSC", "KESC", "NGNC", "USDC"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("KnownCodes() = %v, want %v", got, want)
	}
}

// TestFiatPeg covers a registered token, a correct code from the wrong
// issuer, and native XLM — the three cases the doc comment on FiatPeg
// promises not to guess on.
func TestFiatPeg(t *testing.T) {
	if peg, ok := FiatPeg(NGNC()); !ok || peg != "NGN" {
		t.Errorf("FiatPeg(NGNC()) = (%q, %v), want (\"NGN\", true)", peg, ok)
	}
	if peg, ok := FiatPeg(GHSC()); !ok || peg != "GHS" {
		t.Errorf("FiatPeg(GHSC()) = (%q, %v), want (\"GHS\", true)", peg, ok)
	}
	if peg, ok := FiatPeg(KESC()); !ok || peg != "KES" {
		t.Errorf("FiatPeg(KESC()) = (%q, %v), want (\"KES\", true)", peg, ok)
	}

	impostor := Stellar("NGNC", "GAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAWHF5")
	if _, ok := FiatPeg(impostor); ok {
		t.Error("FiatPeg must return false for the right code from the wrong issuer")
	}

	if _, ok := FiatPeg(Native()); ok {
		t.Error("FiatPeg must return false for native XLM, a bridge asset")
	}

	if _, ok := FiatPeg(USDC()); ok {
		t.Error("FiatPeg must return false for a verified token with no registered peg")
	}
}

// TestIsFiatToken exercises the same cases through the boolean-only helper,
// since callers on the hot classification path use this form directly.
func TestIsFiatToken(t *testing.T) {
	for _, a := range []Asset{NGNC(), GHSC(), KESC()} {
		if !IsFiatToken(a) {
			t.Errorf("IsFiatToken(%s) = false, want true", a)
		}
	}

	impostor := Stellar("NGNC", "GAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAWHF5")
	if IsFiatToken(impostor) {
		t.Error("IsFiatToken must return false for an unregistered issuer")
	}
	if IsFiatToken(Native()) {
		t.Error("IsFiatToken must return false for XLM, a bridge asset")
	}
}
