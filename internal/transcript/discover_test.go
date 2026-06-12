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

func TestDiscoverSkipsSubagentTranscripts(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "subagents")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	top := filepath.Join(dir, "top.jsonl")
	subagent := filepath.Join(sub, "agent-x.jsonl")
	_ = os.WriteFile(top, []byte("{}\n"), 0o644)
	_ = os.WriteFile(subagent, []byte("{}\n"), 0o644)
	past := time.Now().Add(-10 * time.Minute)
	_ = os.Chtimes(top, past, past)
	_ = os.Chtimes(subagent, past, past)

	got, err := Discover(filepath.Join(dir, "**", "*.jsonl"), 5*time.Minute, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Path != top {
		t.Fatalf("expected only %q; got %+v", top, got)
	}
}

func TestDiscoverSkipsAITitleSidecars(t *testing.T) {
	dir := t.TempDir()
	convo := filepath.Join(dir, "convo.jsonl")
	sidecar := filepath.Join(dir, "sidecar.jsonl")
	_ = os.WriteFile(convo, []byte(`{"type":"user","message":{"role":"user","content":"hi"}}`+"\n"), 0o644)
	_ = os.WriteFile(sidecar, []byte(`{"type":"ai-title","aiTitle":"Some title","sessionId":"abc"}`+"\n"), 0o644)
	past := time.Now().Add(-10 * time.Minute)
	_ = os.Chtimes(convo, past, past)
	_ = os.Chtimes(sidecar, past, past)

	got, err := Discover(filepath.Join(dir, "*.jsonl"), 5*time.Minute, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Path != convo {
		t.Fatalf("expected only %q; got %+v", convo, got)
	}
}

func TestProjectFromPath(t *testing.T) {
	in := "/Users/sarah/.claude/projects/-Users-sarah-dev-projects/abc.jsonl"
	if p := projectFromPath(in); p != "Users-sarah-dev-projects" {
		t.Fatalf("project = %q", p)
	}
}
