package synthesize

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/SarahFrankle/ghost/internal/anthropic"
	"github.com/SarahFrankle/ghost/internal/cluster"
)

// Pipeline orchestrates stage 3: it produces identity.md, rules.md,
// the capped set of topics/*.md, and index.md.
//
// Write strategy:
//  1. Create ~/.ghost/.tmp-synthesize-<ts>/.
//  2. Call each generator into the tmpdir (top-level files plus
//     nested topics/<slug>.md).
//  3. If ANY generator returned an error, leave the tmpdir in place
//     (for inspection) and return a partial-failure error. The prior
//     generation in ~/.ghost/ remains authoritative.
//  4. If all generators succeeded, wipe ~/.ghost/topics/ (so removed
//     topics disappear), then rename each file from the tmpdir into
//     ~/.ghost/ and remove the tmpdir.
//
// The topics wipe runs AFTER the partial-failure gate: a failed run
// must not destroy prior topics. POSIX has no atomic multi-file dir-
// merge, so step 4 renames file-by-file.
type Pipeline struct {
	Client          anthropic.Client
	SmartModel      string
	GhostDir        string
	MinRuleEvidence int
	MinRuleProjects int
	MaxTopicEntries int
}

func (p *Pipeline) Run(ctx context.Context, cf cluster.ClustersFile) error {
	if p.GhostDir == "" {
		return errors.New("synthesize: GhostDir required")
	}
	if err := os.MkdirAll(p.GhostDir, 0o755); err != nil {
		return err
	}
	tmpDir, err := os.MkdirTemp(p.GhostDir, ".tmp-synthesize-"+time.Now().UTC().Format("20060102T150405")+"-")
	if err != nil {
		return err
	}

	identityClusters := pickKind(cf.Clusters, "identity")
	ruleClusters := FilterRules(cf.Clusters, p.MinRuleEvidence, p.MinRuleProjects)
	topicGroups := GroupTopicClusters(cf.Clusters)

	userRules := readUserRules(p.GhostDir)

	results := []FileResult{
		BuildIdentity(ctx, p.Client, p.SmartModel, identityClusters),
		BuildRules(ctx, p.Client, p.SmartModel, ruleClusters, userRules),
	}
	ranked := RankTopicsByEvidence(topicGroups)
	capped := CapTopics(ranked, p.MaxTopicEntries)

	// Only generate topic files that survived the cap. A topic the
	// index cannot reference is dead weight against the always-loaded
	// budget.
	keep := make(map[string][]cluster.Cluster, len(capped))
	for _, r := range capped {
		keep[r.Slug] = topicGroups[r.Slug]
	}

	results = append(results, BuildTopics(ctx, p.Client, p.SmartModel, keep)...)
	results = append(results, BuildIndex(ctx, p.Client, p.SmartModel, capped))

	var failed []string
	for _, r := range results {
		if r.Err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", r.Name, r.Err))
			continue
		}
		dst := filepath.Join(tmpDir, r.Name)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			failed = append(failed, fmt.Sprintf("%s: mkdir: %v", r.Name, err))
			continue
		}
		if err := os.WriteFile(dst, []byte(r.Content), 0o644); err != nil {
			failed = append(failed, fmt.Sprintf("%s: write: %v", r.Name, err))
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("synthesize partial failure (tmpdir preserved at %s): %s", tmpDir, strings.Join(failed, "; "))
	}

	// Refresh topics/ as a unit so removed topics vanish. This MUST be
	// after the partial-failure gate above: a failed run must not destroy
	// prior topics.
	topicsDst := filepath.Join(p.GhostDir, "topics")
	if err := os.RemoveAll(topicsDst); err != nil {
		return fmt.Errorf("clean topics/: %w (tmpdir preserved at %s)", err, tmpDir)
	}

	for _, r := range results {
		src := filepath.Join(tmpDir, r.Name)
		dst := filepath.Join(p.GhostDir, r.Name)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w (tmpdir preserved at %s)", filepath.Dir(dst), err, tmpDir)
		}
		if err := os.Rename(src, dst); err != nil {
			return fmt.Errorf("rename %s: %w (tmpdir preserved at %s)", r.Name, err, tmpDir)
		}
	}
	_ = os.RemoveAll(tmpDir)
	return nil
}

func pickKind(cs []cluster.Cluster, kind string) []cluster.Cluster {
	out := make([]cluster.Cluster, 0, len(cs))
	for _, c := range cs {
		if c.Kind == kind {
			out = append(out, c)
		}
	}
	return out
}

func readUserRules(ghostDir string) string {
	b, err := os.ReadFile(filepath.Join(ghostDir, "rules.user.md"))
	if err != nil {
		return "(rules.user.md does not exist; no user-authored rules)"
	}
	return string(b)
}
