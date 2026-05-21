package ledger

import (
	"path/filepath"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ledger.json")

	l, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if l.SchemaVersion != 1 {
		t.Fatalf("default schema version = %d", l.SchemaVersion)
	}

	l.Mark("conv-a", Entry{
		ContentHash:      "sha256:abc",
		ObservationsFile: ".state/observations/abc.json",
		MessageCount:     10,
	})
	if err := l.Save(path); err != nil {
		t.Fatal(err)
	}

	l2, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := l2.Conversations["conv-a"].ContentHash; got != "sha256:abc" {
		t.Fatalf("conv-a content_hash = %q", got)
	}
}

func TestNeedsProcessing(t *testing.T) {
	l := New()
	l.Mark("conv-a", Entry{ContentHash: "sha256:abc"})
	if !l.NeedsProcessing("conv-b", "sha256:zzz") {
		t.Fatalf("unknown conv should need processing")
	}
	if l.NeedsProcessing("conv-a", "sha256:abc") {
		t.Fatalf("matching hash should not need processing")
	}
	if !l.NeedsProcessing("conv-a", "sha256:def") {
		t.Fatalf("changed hash should need processing")
	}
}

func TestForget(t *testing.T) {
	l := New()
	l.Mark("conv-a", Entry{ContentHash: "sha256:abc"})
	if !l.Forget("conv-a") {
		t.Fatalf("Forget should report removed")
	}
	if l.Forget("conv-a") {
		t.Fatalf("Forget on missing should return false")
	}
}
