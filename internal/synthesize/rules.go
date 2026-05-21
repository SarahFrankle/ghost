package synthesize

import (
	"context"
	"fmt"
	"strings"

	"github.com/SarahFrankle/ghost/internal/anthropic"
	"github.com/SarahFrankle/ghost/internal/cluster"
	"github.com/SarahFrankle/ghost/prompts"
)

// FilterRules keeps only rule clusters whose evidence and project
// counts meet the configured minimums. The filter is intentionally in
// Go, never in the LLM, so the synthesis prompt cannot smuggle in a
// rule that fails the cross-project threshold.
func FilterRules(clusters []cluster.Cluster, minEvidence, minProjects int) []cluster.Cluster {
	out := make([]cluster.Cluster, 0, len(clusters))
	for _, c := range clusters {
		if c.Kind != "rule" {
			continue
		}
		if c.EvidenceCount < minEvidence || c.ProjectCount < minProjects {
			continue
		}
		out = append(out, c)
	}
	return out
}

func BuildRules(ctx context.Context, client anthropic.Client, model string, filtered []cluster.Cluster, userRules string) FileResult {
	if len(filtered) == 0 {
		return FileResult{Name: "rules.md", Content: "# Rules\n\nNo cross-project rules inferred yet.\n"}
	}
	var b strings.Builder
	b.WriteString("RULES.USER.MD (authoritative; do not contradict):\n")
	b.WriteString(strings.TrimSpace(userRules))
	b.WriteString("\n\nCANDIDATE CLUSTERS:\n")
	b.WriteString(renderClusters(filtered))

	raw, err := client.Complete(ctx, model, prompts.SynthesizeRulesSystem(), b.String())
	if err != nil {
		return FileResult{Name: "rules.md", Err: fmt.Errorf("rules: %w", err)}
	}
	return FileResult{Name: "rules.md", Content: ensureTrailingNewline(strings.TrimSpace(raw))}
}
