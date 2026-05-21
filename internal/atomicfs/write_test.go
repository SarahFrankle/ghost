package atomicfs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAtomicCreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")
	if err := WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "hello" {
		t.Fatalf("contents = %q", string(b))
	}
}

func TestWriteAtomicLeavesNoTmpOnSuccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")
	if err := WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("expected 1 file, got %d: %v", len(entries), entries)
	}
}
