package runstore

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func ctx() context.Context { return context.Background() }

// fixedRecord is a record with every field pinned to a constant, used by the
// hash-pinning test. Nothing here may change without a Version bump.
func fixedRecord() *Record {
	return &Record{
		Version:    1,
		Seq:        1,
		RecordedAt: time.Date(2026, 8, 21, 22, 30, 40, 0, time.UTC),
		Corridor:   "USDC-NGNC",
		Integrity:  "DIRECT",
		DependsOn:  []string{},
		Reference: Reference{
			Mid:           "1350.2568",
			Source:        "currency-api",
			AsOf:          "2026-08-21T00:00:00Z",
			ScoredAgainst: "currency-api",
		},
		FloorLossPct:    "25.02",
		FloorSize:       "0.1",
		WorstLossPct:    "97.68",
		WorstSize:       "5000",
		Recommended:     nil,
		RecommendedSize: "",
		Finding:         "No usable size.",
		Rungs: []Rung{{
			SendAmount: "0.1", Priced: true, Integrity: "DIRECT",
			ReceiveAmount: "102.78", EffectiveRate: "1027.84",
			LossPct: "24.65", Verdict: "UNUSABLE", Path: "USDC -> NGNC",
		}},
		PrevHash: GenesisPrevHash,
	}
}

// TestRecordHashIsPinned freezes the hash of a known record.
//
// A failure here means the field set, the field order, or the JSON encoding
// settings changed. Any of those changes the preimage of every record ever
// written, which silently invalidates every stored chain — a reader verifying
// last month's history against this build would be told it was tampered with.
//
// So this is NOT a test to update casually. If it fails, the correct response
// is a Version bump and a migration, and updating the constant below is the
// last step of that work rather than the fix for a red build.
func TestRecordHashIsPinned(t *testing.T) {
	// Established 2026-08-21 when the format was defined, by computing it
	// from fixedRecord above. Every value in that fixture is part of it.
	const want = "sha256:1872c8f154123508633ecb2ffdc0c6918539b744f2d1be0c7edc61173d4edca2"

	got, err := fixedRecord().ComputeHash()
	if err != nil {
		t.Fatalf("ComputeHash: %v", err)
	}
	if got != want {
		t.Errorf("record hash = %s, want %s\n\n"+
			"The preimage changed. This means the field set, the field order, or the\n"+
			"encoding settings of runstore.Record are not what they were, and every\n"+
			"previously stored chain now fails verification against this build.\n"+
			"That is a Version bump plus a migration, not a constant to update.",
			got, want)
	}
}

// TestPrevHashIsInsideThePreimage is what makes the structure a chain rather
// than a list of independently-hashed records. If prev_hash were outside the
// hashed bytes, an editor could rewrite history and re-link it freely.
func TestPrevHashIsInsideThePreimage(t *testing.T) {
	a := fixedRecord()
	b := fixedRecord()
	b.PrevHash = "sha256:" + strings.Repeat("a", 64)

	ha, err := a.ComputeHash()
	if err != nil {
		t.Fatal(err)
	}
	hb, err := b.ComputeHash()
	if err != nil {
		t.Fatal(err)
	}
	if ha == hb {
		t.Error("changing prev_hash did not change the record hash; the records are not chained")
	}
}

// TestNilAndEmptySlicesHashAlike guards a subtle way two identical
// measurements could hash differently: nil and empty slices encode as null and
// [], so a record's hash would depend on how its caller happened to build it.
func TestNilAndEmptySlicesHashAlike(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	withNil := fixedRecord()
	withNil.DependsOn = nil
	withNil.Rungs = nil
	if err := s.Append(ctx(), withNil); err != nil {
		t.Fatalf("Append: %v", err)
	}

	if withNil.DependsOn == nil || withNil.Rungs == nil {
		t.Fatal("Append must normalise nil slices before hashing")
	}
	if err := withNil.VerifySelf(); err != nil {
		t.Errorf("record written with nil slices does not verify: %v", err)
	}
}

func TestAppendChainsAndVerifies(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	var prev string
	for i := 0; i < 5; i++ {
		r := fixedRecord()
		r.FloorLossPct = "25.0" + string(rune('0'+i))
		if err := s.Append(ctx(), r); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
		if got, want := r.Seq, int64(i+1); got != want {
			t.Errorf("Seq = %d, want %d", got, want)
		}
		if i == 0 && r.PrevHash != GenesisPrevHash {
			t.Errorf("first record prev_hash = %s, want genesis", r.PrevHash)
		}
		if i > 0 && r.PrevHash != prev {
			t.Errorf("record %d prev_hash = %s, want %s", i, r.PrevHash, prev)
		}
		prev = r.Hash
	}

	if err := s.Verify(ctx(), "USDC-NGNC"); err != nil {
		t.Errorf("Verify on an untouched chain: %v", err)
	}
}

// TestVerifyDetectsTampering is the whole point of the package. A chain that
// cannot detect an edited past record is decoration.
func TestVerifyDetectsTampering(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		r := fixedRecord()
		r.FloorLossPct = "25.0" + string(rune('0'+i))
		if err := s.Append(ctx(), r); err != nil {
			t.Fatal(err)
		}
	}

	// Improve a loss figure in the middle of the history, the realistic
	// abuse: a number quietly adjusted long after it was published.
	path := filepath.Join(dir, "USDC-NGNC"+FileExt)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 5 {
		t.Fatalf("expected 5 records, got %d", len(lines))
	}
	lines[2] = strings.Replace(lines[2], `"floor_loss_pct":"25.02"`, `"floor_loss_pct":"5.02"`, 1)
	if !strings.Contains(lines[2], `"floor_loss_pct":"5.02"`) {
		t.Fatal("test setup failed: the field was not edited")
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err = s.Verify(ctx(), "USDC-NGNC")
	if err == nil {
		t.Fatal("Verify passed on a chain with an edited record")
	}
	if !strings.Contains(err.Error(), "seq 3") {
		t.Errorf("error should name the offending record (seq 3), got: %v", err)
	}

	// Reopening must fail too, so a tampered store cannot be appended to.
	if _, err := Open(dir); err == nil {
		t.Error("Open accepted a store with a broken chain")
	}
}

func TestLatestAndRecent(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	if got, err := s.Latest(ctx(), "USDC-NGNC"); err != nil || got != nil {
		t.Errorf("Latest on an empty store = %v, %v; want nil, nil", got, err)
	}

	for i := 0; i < 4; i++ {
		r := fixedRecord()
		r.Integrity = []string{"DIRECT", "DIRECT", "DERIVATIVE", "NO-MARKET"}[i]
		if err := s.Append(ctx(), r); err != nil {
			t.Fatal(err)
		}
	}

	latest, err := s.Latest(ctx(), "USDC-NGNC")
	if err != nil {
		t.Fatal(err)
	}
	if latest.Integrity != "NO-MARKET" || latest.Seq != 4 {
		t.Errorf("Latest = seq %d %s, want seq 4 NO-MARKET", latest.Seq, latest.Integrity)
	}

	// Recent(corridor, 2) is the whole of #24's read path: compare the last
	// two runs and report an integrity change.
	recent, err := s.Recent(ctx(), "USDC-NGNC", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 2 {
		t.Fatalf("Recent returned %d records, want 2", len(recent))
	}
	if recent[0].Integrity != "NO-MARKET" || recent[1].Integrity != "DERIVATIVE" {
		t.Errorf("Recent = [%s %s], want [NO-MARKET DERIVATIVE] (newest first)",
			recent[0].Integrity, recent[1].Integrity)
	}
}

// TestReopenResumesTheChain covers the restart path: a redeployed monitor must
// continue the existing chain, not start a second one.
func TestReopenResumesTheChain(t *testing.T) {
	dir := t.TempDir()

	first, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	r1 := fixedRecord()
	if err := first.Append(ctx(), r1); err != nil {
		t.Fatal(err)
	}

	second, err := Open(dir)
	if err != nil {
		t.Fatalf("reopening a valid store: %v", err)
	}
	r2 := fixedRecord()
	if err := second.Append(ctx(), r2); err != nil {
		t.Fatal(err)
	}

	if r2.Seq != 2 {
		t.Errorf("Seq after reopen = %d, want 2", r2.Seq)
	}
	if r2.PrevHash != r1.Hash {
		t.Errorf("chain did not resume: prev_hash = %s, want %s", r2.PrevHash, r1.Hash)
	}
	if err := second.Verify(ctx(), "USDC-NGNC"); err != nil {
		t.Errorf("Verify after reopen: %v", err)
	}
}

// TestUnknownVersionIsRefused mirrors the snapshot format's rule: a schema
// this build does not understand is an error, never a best-effort parse.
func TestUnknownVersionIsRefused(t *testing.T) {
	dir := t.TempDir()
	line := `{"version":99,"seq":1,"corridor":"USDC-NGNC","prev_hash":"` +
		GenesisPrevHash + `","hash":"sha256:x"}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "USDC-NGNC"+FileExt), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Open(dir)
	if err == nil {
		t.Fatal("Open accepted a record with an unknown version")
	}
	if !strings.Contains(err.Error(), "version 99") {
		t.Errorf("error should name the version found, got: %v", err)
	}
}

// TestNopStoreIsSafe pins the degrade-gracefully requirement: a monitor with
// no history configured behaves exactly as it did before this package existed.
func TestNopStoreIsSafe(t *testing.T) {
	var s Store = Nop{}
	if err := s.Append(ctx(), fixedRecord()); err != nil {
		t.Errorf("Nop.Append: %v", err)
	}
	got, err := s.Latest(ctx(), "USDC-NGNC")
	if err != nil || got != nil {
		t.Errorf("Nop.Latest = %v, %v; want nil, nil", got, err)
	}
	if err := s.Verify(ctx(), "USDC-NGNC"); err != nil {
		t.Errorf("Nop.Verify: %v", err)
	}
}
