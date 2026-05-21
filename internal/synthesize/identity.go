package synthesize

import (
	"context"
	"fmt"
	"strings"

	"github.com/SarahFrankle/ghost/internal/anthropic"
	"github.com/SarahFrankle/ghost/internal/cluster"
	"github.com/SarahFrankle/ghost/prompts"
)

// BuildIdentity calls the smart model to synthesize identity.md from
// the identity slice of clusters. Caller passes in only the relevant
// clusters; selection is not BuildIdentity's job.
func BuildIdentity(ctx context.Context, client anthropic.Client, model string, identityClusters []cluster.Cluster) FileResult {
	if len(identityClusters) == 0 {
		return FileResult{Name: "identity.md", Content: "# Identity\n\nNo identity observations yet.\n"}
	}
	payload := renderClusters(identityClusters)
	raw, err := client.Complete(ctx, model, prompts.SynthesizeIdentitySystem(), payload)
	if err != nil {
		return FileResult{Name: "identity.md", Err: fmt.Errorf("identity: %w", err)}
	}
	return FileResult{Name: "identity.md", Content: ensureTrailingNewline(strings.TrimSpace(raw))}
}

func renderClusters(cs []cluster.Cluster) string {
	var b strings.Builder
	for i, c := range cs {
		fmt.Fprintf(&b, "cluster %d (evidence=%d, projects=%d): %s\n", i+1, c.EvidenceCount, c.ProjectCount, c.Canonical)
		for j, m := range c.Members {
			fmt.Fprintf(&b, "  member %d (project=%s): %s [evidence: %s]\n", j+1, m.Project, m.Text, m.Evidence)
		}
	}
	return b.String()
}

func ensureTrailingNewline(s string) string {
	if !strings.HasSuffix(s, "\n") {
		return s + "\n"
	}
	return s
}
