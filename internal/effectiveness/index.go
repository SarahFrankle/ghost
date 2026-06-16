package effectiveness

import (
	"regexp"
	"strings"
)

// slugRe and triggersRe parse an index.md topic line:
// "- topics/<slug>.md" optionally followed by "(triggers: a, b, c)".
// Defensive: a line with no parseable slug is skipped; a slug with no
// triggers clause maps to an empty list.
var (
	slugRe     = regexp.MustCompile(`topics/([a-z0-9-]+)\.md`)
	triggersRe = regexp.MustCompile(`\(triggers:\s*([^)]*)\)`)
)

// ParseIndexTriggers extracts slug -> trigger phrases from index.md prose.
// It tolerates format drift: lines without a topics/<slug>.md token are
// ignored; a recognized slug with no (triggers: ...) clause yields an empty
// (non-nil) slice so the slug is still known.
func ParseIndexTriggers(indexMD string) map[string][]string {
	out := map[string][]string{}
	for line := range strings.SplitSeq(indexMD, "\n") {
		sm := slugRe.FindStringSubmatch(line)
		if sm == nil {
			continue
		}
		slug := sm[1]
		triggers := []string{}
		if tm := triggersRe.FindStringSubmatch(line); tm != nil {
			for part := range strings.SplitSeq(tm[1], ",") {
				if p := strings.TrimSpace(part); p != "" {
					triggers = append(triggers, p)
				}
			}
		}
		out[slug] = triggers
	}
	return out
}
