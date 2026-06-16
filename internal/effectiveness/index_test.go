package effectiveness

import "testing"

func TestParseIndexTriggers(t *testing.T) {
	src := `# Index

## Topics
- topics/documentation-practices.md (triggers: documentation, docs, write docs, readme)
- topics/design-architecture.md (triggers: design, architecture, refactor)
- topics/no-triggers.md
- garbage line that should be ignored
`
	got := ParseIndexTriggers(src)
	if len(got["design-architecture"]) != 3 || got["design-architecture"][1] != "architecture" {
		t.Errorf("design-architecture = %v", got["design-architecture"])
	}
	if len(got["documentation-practices"]) != 4 {
		t.Errorf("documentation-practices = %v", got["documentation-practices"])
	}
	if _, ok := got["no-triggers"]; !ok {
		t.Errorf("no-triggers slug should be present with empty triggers")
	}
	if len(got["no-triggers"]) != 0 {
		t.Errorf("no-triggers should have 0 triggers, got %v", got["no-triggers"])
	}
}
