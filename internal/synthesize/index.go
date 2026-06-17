package synthesize

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/SarahFrankle/ghost/internal/anthropic"
	"github.com/SarahFrankle/ghost/internal/cluster"
	"github.com/SarahFrankle/ghost/internal/extract"
	"github.com/SarahFrankle/ghost/prompts"
)

func hasHighConfidenceMember(c cluster.Cluster) bool {
	for _, m := range c.Members {
		if m.Confidence == extract.ConfidenceHigh {
			return true
		}
	}
	return false
}

// RankByEvidence returns a copy of topics sorted with high-confidence themes
// first, then by EvidenceTotal descending, with ties broken alphabetically by
// slug for determinism. High-confidence themes rank first so a directly
// asserted (stated-once) theme is never truncated out by Cap below.
func RankByEvidence(topics []TopicResult) []TopicResult {
	out := make([]TopicResult, len(topics))
	copy(out, topics)
	sort.SliceStable(out, func(i, j int) bool {
		hi, hj := hasHighConfidenceMember(out[i].Cluster), hasHighConfidenceMember(out[j].Cluster)
		if hi != hj {
			return hi
		}
		if out[i].EvidenceTotal != out[j].EvidenceTotal {
			return out[i].EvidenceTotal > out[j].EvidenceTotal
		}
		return out[i].Slug < out[j].Slug
	})
	return out
}

// Cap truncates only the soft (non-high-confidence) tail. High-confidence
// themes are pinned and exempt from the cap: they were protected by the
// confidence gate and must not be silently dropped by evidence truncation.
func Cap(topics []TopicResult, max int, logf func(string, ...any)) []TopicResult {
	if max <= 0 {
		return topics
	}
	high := 0
	for _, t := range topics {
		if hasHighConfidenceMember(t.Cluster) {
			high++
		}
	}
	if high >= max {
		if high > max && logf != nil {
			logf("topics: %d high-confidence themes exceed MaxTopicEntries=%d; keeping all (raise the cap)", high, max)
		}
		kept := topics[:0:0]
		for _, t := range topics {
			if hasHighConfidenceMember(t.Cluster) {
				kept = append(kept, t)
			}
		}
		return kept
	}
	if len(topics) <= max {
		return topics
	}
	return topics[:max]
}

// BuildIndex asks the smart model to emit index.md from a ranked list of
// TopicResult. Topics must already be ranked + capped by the caller. When
// categories is non-empty, each topic carries its category so the model can
// group under ### subheadings; when nil/empty (categorization failed) the
// index is rendered flat.
func BuildIndex(ctx context.Context, client anthropic.Client, model string, topics []TopicResult, categories map[string]string) FileResult {
	if len(topics) == 0 {
		return FileResult{Name: "index.md", Content: "# Index\n\nNo lazy-loaded topics yet.\n"}
	}
	var b strings.Builder
	b.WriteString("RANKED TOPICS (highest evidence first):\n")
	for _, t := range topics {
		fmt.Fprintf(&b, "- slug=%s file=topics/%s.md evidence=%d title=%q\n", t.Slug, t.Slug, t.EvidenceTotal, t.Title)
		if cat := categories[t.Slug]; cat != "" {
			fmt.Fprintf(&b, "    category: %s\n", cat)
		}
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
