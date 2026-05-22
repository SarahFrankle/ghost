package synthesize

import (
	"context"
	"fmt"
	"strings"

	"github.com/SarahFrankle/ghost/internal/anthropic"
	"github.com/SarahFrankle/ghost/internal/cluster"
	"github.com/SarahFrankle/ghost/prompts"
)

// GroupTopicClusters partitions kind=topic clusters by SubKey (the
// topic slug). Clusters with kind != topic or an empty SubKey are
// dropped: a topic without a slug cannot be addressed by the index
// and would have nowhere to live on disk.
func GroupTopicClusters(cs []cluster.Cluster) map[string][]cluster.Cluster {
	out := map[string][]cluster.Cluster{}
	for _, c := range cs {
		if c.Kind != "topic" || strings.TrimSpace(c.SubKey) == "" {
			continue
		}
		out[c.SubKey] = append(out[c.SubKey], c)
	}
	return out
}

// BuildTopics produces one FileResult per topic slug. The caller is
// responsible for atomic placement; this function is pure
// (no filesystem). Output names use the "topics/<slug>.md" form so
// the pipeline can place them under a topics/ subdirectory verbatim.
func BuildTopics(ctx context.Context, client anthropic.Client, model string, groups map[string][]cluster.Cluster) []FileResult {
	results := make([]FileResult, 0, len(groups))
	for slug, cs := range groups {
		name := fmt.Sprintf("topics/%s.md", slug)
		payload := renderTopicPayload(slug, cs)
		raw, err := client.Complete(ctx, model, prompts.SynthesizeTopicsSystem(), payload)
		if err != nil {
			results = append(results, FileResult{Name: name, Err: fmt.Errorf("topic %s: %w", slug, err)})
			continue
		}
		results = append(results, FileResult{Name: name, Content: ensureTrailingNewline(strings.TrimSpace(raw))})
	}
	return results
}

func renderTopicPayload(slug string, cs []cluster.Cluster) string {
	var b strings.Builder
	fmt.Fprintf(&b, "TOPIC: %s\n\nCANDIDATE CLUSTERS:\n", slug)
	b.WriteString(renderClusters(cs))
	return b.String()
}
