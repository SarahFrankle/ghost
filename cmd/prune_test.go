package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/SarahFrankle/ghost/internal/extract"
	"github.com/SarahFrankle/ghost/internal/ledger"
)

// writeObs writes an observations file under obsDir and returns its base name.
func writeObs(t *testing.T, obsDir, name, source string) string {
	t.Helper()
	f := extract.ObservationsFile{Source: source, Observations: []extract.Observation{}}
	b, _ := json.MarshalIndent(f, "", "  ")
	if err := os.WriteFile(filepath.Join(obsDir, name), b, 0o644); err != nil {
		t.Fatal(err)
	}
	return name
}

func TestPruneStateDropsSidecarAndVanished(t *testing.T) {
	out := t.TempDir()
	stateDir := filepath.Join(out, ".state")
	obsDir := filepath.Join(stateDir, "observations")
	if err := os.MkdirAll(obsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// A real transcript (kept), a sidecar (pruned), a vanished source (pruned).
	real := filepath.Join(out, "real.jsonl")
	sidecar := filepath.Join(out, "sidecar.jsonl")
	gone := filepath.Join(out, "gone.jsonl")
	_ = os.WriteFile(real, []byte(`{"type":"user","message":{"role":"user","content":"hi"}}`+"\n"), 0o644)
	_ = os.WriteFile(sidecar, []byte(`{"type":"ai-title","aiTitle":"t","sessionId":"s"}`+"\n"), 0o644)

	realObs := writeObs(t, obsDir, "real.json", real)
	sidecarObs := writeObs(t, obsDir, "sidecar.json", sidecar)
	goneObs := writeObs(t, obsDir, "gone.json", gone)

	l := ledger.New()
	l.Mark(real, ledger.Entry{ObservationsFile: filepath.Join(".state/observations", realObs)})
	l.Mark(sidecar, ledger.Entry{ObservationsFile: filepath.Join(".state/observations", sidecarObs)})
	l.Mark(gone, ledger.Entry{ObservationsFile: filepath.Join(".state/observations", goneObs)})
	ledgerPath := filepath.Join(stateDir, "ledger.json")
	if err := l.Save(ledgerPath); err != nil {
		t.Fatal(err)
	}

	// Dry-run changes nothing.
	entries, files, err := pruneState(out, true)
	if err != nil {
		t.Fatal(err)
	}
	if entries != 2 || files != 2 {
		t.Fatalf("dry-run counts = %d entries, %d files; want 2, 2", entries, files)
	}
	if _, err := os.Stat(filepath.Join(obsDir, sidecarObs)); err != nil {
		t.Fatalf("dry-run removed a file: %v", err)
	}

	// Real run drops the two bad entries + files, keeps the real one.
	entries, files, err = pruneState(out, false)
	if err != nil {
		t.Fatal(err)
	}
	if entries != 2 || files != 2 {
		t.Fatalf("counts = %d entries, %d files; want 2, 2", entries, files)
	}
	reloaded, _ := ledger.Load(ledgerPath)
	if _, ok := reloaded.Conversations[real]; !ok {
		t.Fatal("real entry was pruned")
	}
	if len(reloaded.Conversations) != 1 {
		t.Fatalf("ledger has %d entries; want 1", len(reloaded.Conversations))
	}
	if _, err := os.Stat(filepath.Join(obsDir, realObs)); err != nil {
		t.Fatalf("real obs file removed: %v", err)
	}
	for _, n := range []string{sidecarObs, goneObs} {
		if _, err := os.Stat(filepath.Join(obsDir, n)); !os.IsNotExist(err) {
			t.Fatalf("%s should be removed; err=%v", n, err)
		}
	}

	// Idempotent: second run is a no-op.
	entries, files, err = pruneState(out, false)
	if err != nil {
		t.Fatal(err)
	}
	if entries != 0 || files != 0 {
		t.Fatalf("second run not a no-op: %d entries, %d files", entries, files)
	}
}
