package synthesize

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/SarahFrankle/ghost/internal/anthropic"
	"github.com/SarahFrankle/ghost/internal/cluster"
	"github.com/SarahFrankle/ghost/prompts"
)

// RankedTopic is one row in the ranked input to BuildIndex.
type RankedTopic struct {
	Slug          string
	EvidenceTotal int
	Canonicals    []string
}

// RankTopicsByEvidence sums evidence per topic slug and sorts descending.
// Ties break alphabetically by slug for determinism.
func RankTopicsByEvidence(groups map[string][]cluster.Cluster) []RankedTopic {
	out := make([]RankedTopic, 0, len(groups))
	for slug, cs := range groups {
		total := 0
		canon := make([]string, 0, len(cs))
		for _, c := range cs {
			total += c.EvidenceCount
			if c.Canonical != "" {
				canon = append(canon, c.Canonical)
			}
		}
		out = append(out, RankedTopic{Slug: slug, EvidenceTotal: total, Canonicals: canon})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].EvidenceTotal != out[j].EvidenceTotal {
			return out[i].EvidenceTotal > out[j].EvidenceTotal
		}
		return out[i].Slug < out[j].Slug
	})
	return out
}

// CapTopics returns at most max entries from ranked.
func CapTopics(ranked []RankedTopic, max int) []RankedTopic {
	if max <= 0 || len(ranked) <= max {
		return ranked
	}
	return ranked[:max]
}

// BuildIndex asks the smart model to emit index.md from a ranked topic list.
func BuildIndex(ctx context.Context, client anthropic.Client, model string, ranked []RankedTopic) FileResult {
	if len(ranked) == 0 {
		return FileResult{Name: "index.md", Content: "# Index\n\nNo lazy-loaded topics yet.\n"}
	}
	var b strings.Builder
	b.WriteString("RANKED TOPICS (highest evidence first):\n")
	for _, r := range ranked {
		fmt.Fprintf(&b, "- slug=%s file=topics/%s.md evidence=%d\n", r.Slug, r.Slug, r.EvidenceTotal)
		for _, c := range r.Canonicals {
			fmt.Fprintf(&b, "    canonical: %s\n", c)
		}
	}
	raw, err := client.Complete(ctx, model, prompts.SynthesizeIndexSystem(), b.String())
	if err != nil {
		return FileResult{Name: "index.md", Err: fmt.Errorf("index: %w", err)}
	}
	return FileResult{Name: "index.md", Content: ensureTrailingNewline(strings.TrimSpace(raw))}
}
