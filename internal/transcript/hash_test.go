package transcript

import (
	"os"
	"path/filepath"
	"testing"
)

func TestContentHashStable(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.jsonl")
	if err := os.WriteFile(p, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h1, err := ContentHash(p)
	if err != nil {
		t.Fatal(err)
	}
	h2, err := ContentHash(p)
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 || h1 == "" {
		t.Fatalf("hash unstable: %q vs %q", h1, h2)
	}
}

func TestContentHashChangesOnAppend(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.jsonl")
	_ = os.WriteFile(p, []byte("hello\n"), 0o644)
	h1, _ := ContentHash(p)
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o644)
	_, _ = f.WriteString("world\n")
	_ = f.Close()
	h2, _ := ContentHash(p)
	if h1 == h2 {
		t.Fatalf("hash should change after append")
	}
}
