package source

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClaudeCode_Name(t *testing.T) {
	s := ClaudeCode("/tmp/**/*.jsonl")
	if s.Name() != "claude-code" {
		t.Fatalf("Name() = %q, want %q", s.Name(), "claude-code")
	}
}

func TestClaudeCode_DiscoverAndParse(t *testing.T) {
	dir := t.TempDir()
	projDir := filepath.Join(dir, "-Users-sarah-dev-projects-ghost")
	if err := os.Mkdir(projDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(projDir, "session.jsonl")
	body := strings.Join([]string{
		`{"type":"user","message":{"role":"user","content":"hello"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"hi"}]}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	// Backdate so the active-window filter doesn't skip it.
	past := time.Now().Add(-1 * time.Hour)
	if err := os.Chtimes(path, past, past); err != nil {
		t.Fatal(err)
	}

	s := ClaudeCode(filepath.Join(dir, "**", "*.jsonl"))
	ctx := context.Background()

	convs, err := s.Discover(ctx, 5*time.Minute, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(convs) != 1 {
		t.Fatalf("Discover returned %d, want 1", len(convs))
	}
	c := convs[0]
	if c.Source != "claude-code" {
		t.Errorf("Source = %q, want claude-code", c.Source)
	}
	if c.Project != "Users-sarah-dev-projects-ghost" {
		t.Errorf("Project = %q, want Users-sarah-dev-projects-ghost", c.Project)
	}
	if c.ID != path {
		t.Errorf("ID = %q, want %q", c.ID, path)
	}

	h, err := s.ContentHash(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "sha256:") {
		t.Errorf("ContentHash = %q, want sha256: prefix", h)
	}

	turns, err := s.Parse(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 2 {
		t.Fatalf("Parse returned %d turns, want 2", len(turns))
	}
	if turns[0].Role != "user" || turns[0].Text != "hello" {
		t.Errorf("turn 0 = %+v", turns[0])
	}
	if turns[1].Role != "assistant" || turns[1].Text != "hi" {
		t.Errorf("turn 1 = %+v", turns[1])
	}
}

func TestClaudeCode_DiscoverSkipsActiveSessions(t *testing.T) {
	dir := t.TempDir()
	projDir := filepath.Join(dir, "-proj")
	if err := os.Mkdir(projDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(projDir, "live.jsonl")
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	// Fresh mtime — should be filtered out by a 5m active window.

	s := ClaudeCode(filepath.Join(dir, "**", "*.jsonl"))
	convs, err := s.Discover(context.Background(), 5*time.Minute, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(convs) != 0 {
		t.Fatalf("Discover returned %d, want 0 (active window should skip)", len(convs))
	}
}
