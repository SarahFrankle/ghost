package transcript

import (
	"os"
	"path/filepath"
	"testing"
)

func writeJSONL(t *testing.T, lines []string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "t.jsonl")
	if err := os.WriteFile(p, []byte(joinLines(lines)), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func joinLines(lines []string) string {
	out := ""
	for _, l := range lines {
		out += l + "\n"
	}
	return out
}

func TestParseEvents_TextAndToolUse(t *testing.T) {
	p := writeJSONL(t, []string{
		`{"timestamp":"2026-06-15T10:00:00Z","message":{"role":"user","content":[{"type":"text","text":"review the schema"}]}}`,
		`{"timestamp":"2026-06-15T10:00:01Z","message":{"role":"assistant","content":[{"type":"tool_use","name":"Read","input":{"file_path":"/Users/sarah/.ghost/topics/design-architecture.md"}}]}}`,
		`{"timestamp":"2026-06-15T10:00:02Z","message":{"role":"user","content":"next task"}}`,
	})

	evs, err := ParseEvents(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 3 {
		t.Fatalf("want 3 events, got %d: %+v", len(evs), evs)
	}
	if evs[0].Kind != "text" || evs[0].Role != "user" || evs[0].Text != "review the schema" {
		t.Errorf("ev0 = %+v", evs[0])
	}
	if evs[1].Kind != "tool_use" || evs[1].Tool != "Read" {
		t.Errorf("ev1 = %+v", evs[1])
	}
	if got := evs[1].Input["file_path"]; got != "/Users/sarah/.ghost/topics/design-architecture.md" {
		t.Errorf("ev1 file_path = %q", got)
	}
	if evs[1].Line != 2 {
		t.Errorf("ev1 line = %d, want 2", evs[1].Line)
	}
	if evs[2].Kind != "text" || evs[2].Text != "next task" {
		t.Errorf("ev2 = %+v", evs[2])
	}
	if evs[0].Timestamp != "2026-06-15T10:00:00Z" {
		t.Errorf("ev0 ts = %q", evs[0].Timestamp)
	}
}

func TestParseEvents_SkipsMalformedLine(t *testing.T) {
	p := writeJSONL(t, []string{
		`not json`,
		`{"message":{"role":"user","content":[{"type":"text","text":"hi"}]}}`,
	})
	evs, err := ParseEvents(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].Text != "hi" || evs[0].Line != 2 {
		t.Fatalf("got %+v", evs)
	}
}
