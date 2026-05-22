package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppendUserRuleCreatesFileIfMissing(t *testing.T) {
	dir := t.TempDir()
	if err := appendUserRule(dir, "prefer local-first LLMs"); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "rules.user.md"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	if !strings.HasPrefix(s, "# Rules (user-authored)") {
		t.Fatalf("missing header on new file: %q", s)
	}
	if !strings.Contains(s, "- prefer local-first LLMs") {
		t.Fatalf("rule not appended: %q", s)
	}
}

func TestAppendUserRuleAppendsToExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.user.md")
	if err := os.WriteFile(path, []byte("# Rules (user-authored)\n\n- existing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := appendUserRule(dir, "new one"); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), "- existing") || !strings.Contains(string(body), "- new one") {
		t.Fatalf("unexpected body: %q", string(body))
	}
}
