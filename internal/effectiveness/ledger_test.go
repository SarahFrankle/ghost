package effectiveness

import (
	"path/filepath"
	"testing"
)

func TestAuditLedger_OffsetRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "audit-ledger.json")
	l, err := LoadAuditLedger(p)
	if err != nil {
		t.Fatal(err)
	}
	if l.ScannedLines("t1") != 0 {
		t.Fatalf("fresh ledger should report 0 lines")
	}
	l.SetScannedLines("t1", 42)
	if err := l.Save(p); err != nil {
		t.Fatal(err)
	}
	reloaded, err := LoadAuditLedger(p)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.ScannedLines("t1") != 42 {
		t.Fatalf("want 42, got %d", reloaded.ScannedLines("t1"))
	}
	if reloaded.ScannedLines("unknown") != 0 {
		t.Fatalf("unknown id should be 0")
	}
}
