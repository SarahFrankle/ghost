package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/SarahFrankle/ghost/internal/config"
	"github.com/SarahFrankle/ghost/internal/extract"
	"github.com/SarahFrankle/ghost/internal/ledger"
)

func TestObservationsFileName(t *testing.T) {
	// Deterministic: same hash -> same name; the "sha256:" prefix is stripped
	// from the readable portion but still feeds the disambiguating suffix.
	a := observationsFileName("sha256:" + "abcdef0123456789aabbccddeeff")
	b := observationsFileName("sha256:" + "abcdef0123456789aabbccddeeff")
	if a != b {
		t.Fatalf("not deterministic: %q vs %q", a, b)
	}
	if got := a[:16]; got != "abcdef0123456789" {
		t.Fatalf("readable prefix = %q; want first 16 hex of trimmed hash", got)
	}
	// Different full hashes that share a 16-char prefix must not collide,
	// because the suffix hashes the full input.
	c := observationsFileName("sha256:" + "abcdef0123456789ZZZZ")
	if a == c {
		t.Fatal("distinct hashes produced identical file name")
	}
}

func TestObservationsStale(t *testing.T) {
	out := t.TempDir()
	obsDir := filepath.Join(out, ".state", "observations")
	if err := os.MkdirAll(obsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const (
		convID = "/x/real.jsonl"
		src    = "real.jsonl"
		proj   = "ghost"
		hash   = "sha256:deadbeef"
		model  = "claude-haiku-4-5-20251001"
	)
	rel := filepath.Join(".state/observations", "real.json")
	write := func(fp string) {
		writeObsFile(t, filepath.Join(out, rel), extract.ObservationsFile{Source: src, Project: proj, Fingerprint: fp})
	}

	l := ledger.New()
	// No ledger entry at all -> stale.
	if !observationsStale(out, l, convID, src, proj, hash, model) {
		t.Fatal("missing ledger entry should be stale")
	}

	l.Mark(convID, ledger.Entry{ObservationsFile: rel})
	// Entry points at a file that doesn't exist yet -> stale.
	if !observationsStale(out, l, convID, src, proj, hash, model) {
		t.Fatal("missing observations file should be stale")
	}

	// Matching fingerprint -> fresh.
	write(extract.ObservationsFingerprint(src, proj, hash, model))
	if observationsStale(out, l, convID, src, proj, hash, model) {
		t.Fatal("matching fingerprint should be fresh")
	}

	// Fingerprint for a different model -> stale.
	if !observationsStale(out, l, convID, src, proj, hash, "claude-opus-4-8") {
		t.Fatal("model change should invalidate the cached fingerprint")
	}
}

func TestSynthesizeOutputsFresh(t *testing.T) {
	out := t.TempDir()
	sidecar := filepath.Join(out, ".state", "synthesize.fp")
	if err := os.MkdirAll(filepath.Dir(sidecar), 0o755); err != nil {
		t.Fatal(err)
	}
	const fp = "fingerprint-123"

	// No sidecar -> not fresh.
	if synthesizeOutputsFresh(out, sidecar, fp) {
		t.Fatal("absent sidecar should not be fresh")
	}

	if err := os.WriteFile(sidecar, []byte(fp+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Sidecar matches but the output files are missing -> not fresh.
	if synthesizeOutputsFresh(out, sidecar, fp) {
		t.Fatal("missing output files should not be fresh")
	}

	for _, n := range []string{"identity.md", "rules.md", "index.md"} {
		if err := os.WriteFile(filepath.Join(out, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Sidecar matches and all files present -> fresh.
	if !synthesizeOutputsFresh(out, sidecar, fp) {
		t.Fatal("matching fingerprint with all files present should be fresh")
	}

	// Fingerprint drift -> not fresh.
	if synthesizeOutputsFresh(out, sidecar, "different") {
		t.Fatal("fingerprint mismatch should not be fresh")
	}
}

func TestEmbeddingModelName(t *testing.T) {
	cfg := config.Defaults()
	cfg.Models.Embedding = "voyage-3-lite"

	t.Run("voyage when key set", func(t *testing.T) {
		t.Setenv("VOYAGE_API_KEY", "set")
		t.Setenv("OLLAMA_EMBEDDING_MODEL", "")
		if got := embeddingModelName(cfg); got != "voyage-3-lite" {
			t.Fatalf("got %q; want the configured Voyage model", got)
		}
	})

	t.Run("ollama override when no key", func(t *testing.T) {
		t.Setenv("VOYAGE_API_KEY", "")
		t.Setenv("OLLAMA_EMBEDDING_MODEL", "mxbai-embed-large")
		if got := embeddingModelName(cfg); got != "mxbai-embed-large" {
			t.Fatalf("got %q; want the OLLAMA_EMBEDDING_MODEL override", got)
		}
	})

	t.Run("ollama default when nothing set", func(t *testing.T) {
		t.Setenv("VOYAGE_API_KEY", "")
		t.Setenv("OLLAMA_EMBEDDING_MODEL", "")
		if got := embeddingModelName(cfg); got != "nomic-embed-text" {
			t.Fatalf("got %q; want the nomic-embed-text default", got)
		}
	})
}
