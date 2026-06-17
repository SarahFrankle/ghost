package skill

import (
	"strings"
	"testing"
)

func TestSkillEmbedsResolveOrSkip(t *testing.T) {
	content := Content()
	if !strings.Contains(content, "skip that entry") {
		t.Fatal("SKILL.md must contain the resolve-or-skip directive")
	}
}

func TestContentEmbedded(t *testing.T) {
	c := Content()
	if !strings.Contains(c, "name: ghost") || !strings.Contains(c, "index.md") {
		t.Fatalf("SKILL.md did not embed correctly: %q", c)
	}
}
