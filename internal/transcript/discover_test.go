package transcript

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDiscoverIgnoresRecentlyModified(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "old.jsonl")
	fresh := filepath.Join(dir, "fresh.jsonl")
	_ = os.WriteFile(old, []byte("{}\n"), 0o644)
	_ = os.WriteFile(fresh, []byte("{}\n"), 0o644)
	past := time.Now().Add(-10 * time.Minute)
	_ = os.Chtimes(old, past, past)

	got, err := Discover(filepath.Join(dir, "*.jsonl"), 5*time.Minute, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Path != old {
		t.Fatalf("expected only %q; got %+v", old, got)
	}
}

func TestProjectFromPath(t *testing.T) {
	in := "/Users/sarah/.claude/projects/-Users-sarah-dev-projects/abc.jsonl"
	if p := projectFromPath(in); p != "Users-sarah-dev-projects" {
		t.Fatalf("project = %q", p)
	}
}
