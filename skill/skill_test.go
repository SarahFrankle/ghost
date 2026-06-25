package skill

import (
	"strings"
	"testing"
)

func TestSkillIsWriteOnly(t *testing.T) {
	content := Content()
	if !strings.Contains(content, "ghost remember") {
		t.Fatal("SKILL.md must document the `ghost remember` write action")
	}
	if strings.Contains(content, "skip that entry") {
		t.Fatal("SKILL.md must no longer carry the lazy-load read prose")
	}
}

func TestContentEmbedded(t *testing.T) {
	c := Content()
	if !strings.Contains(c, "name: ghost") {
		t.Fatalf("SKILL.md did not embed correctly: %q", c)
	}
}
