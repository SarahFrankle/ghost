package skill

import (
	"strings"
	"testing"
)

func TestContentEmbedded(t *testing.T) {
	c := Content()
	if !strings.Contains(c, "name: ghost") || !strings.Contains(c, "index.md") {
		t.Fatalf("SKILL.md did not embed correctly: %q", c)
	}
}
