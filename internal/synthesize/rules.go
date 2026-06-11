package synthesize

import (
	"context"
	"fmt"
	"strings"

	"github.com/SarahFrankle/ghost/internal/anthropic"
	"github.com/SarahFrankle/ghost/internal/cluster"
	"github.com/SarahFrankle/ghost/prompts"
)

// BuildRules synthesizes rules.md from the general (routed + confidence-gated)
// clusters. It does no gating itself: routing chose these for the general
// destination and the confidence gate already decided which survive. The arg
// is the routed+gated general clusters.
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
