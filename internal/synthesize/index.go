package synthesize

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/SarahFrankle/ghost/internal/anthropic"
	"github.com/SarahFrankle/ghost/prompts"
)

// RankByEvidence returns a copy of topics sorted by EvidenceTotal
// descending, with ties broken alphabetically by slug for determinism.
func RankByEvidence(topics []TopicResult) []TopicResult {
	out := make([]TopicResult, len(topics))
	copy(out, topics)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].EvidenceTotal != out[j].EvidenceTotal {
			return out[i].EvidenceTotal > out[j].EvidenceTotal
		}
		return out[i].Slug < out[j].Slug
	})
	return out
}

// Cap returns at most max entries from topics.
func Cap(topics []TopicResult, max int) []TopicResult {
	if max <= 0 || len(topics) <= max {
		return topics
	}
	return topics[:max]
}

// BuildIndex asks the smart model to emit index.md from a ranked list
// of TopicResult. Topics must already be ranked + capped by the caller.
func BuildIndex(ctx context.Context, client anthropic.Client, model string, topics []TopicResult) FileResult {
	if len(topics) == 0 {
		return FileResult{Name: "index.md", Content: "# Index\n\nNo lazy-loaded topics yet.\n"}
	}
	var b strings.Builder
	b.WriteString("RANKED TOPICS (highest evidence first):\n")
	for _, t := range topics {
		fmt.Fprintf(&b, "- slug=%s file=topics/%s.md evidence=%d title=%q\n", t.Slug, t.Slug, t.EvidenceTotal, t.Title)
		if t.Cluster.Canonical != "" {
			fmt.Fprintf(&b, "    canonical: %s\n", t.Cluster.Canonical)
		}
	}
	raw, err := client.Complete(ctx, model, prompts.SynthesizeIndexSystem(), b.String())
	if err != nil {
		return FileResult{Name: "index.md", Err: fmt.Errorf("index: %w", err)}
	}
	return FileResult{Name: "index.md", Content: ensureTrailingNewline(strings.TrimSpace(raw))}
}
