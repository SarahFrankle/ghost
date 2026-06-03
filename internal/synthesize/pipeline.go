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
// Order of operations:
//  1. Build identity and rules in parallel (they don't depend on topic
//     slugs).
//  2. Run topic synthesis: one smart-model call per topic cluster
//     producing a body that starts with `# <Title>`; slugify each
//     title; merge any slug collisions and re-synthesize to a
//     unique-slug fixpoint; fail loudly on any per-cluster error.
//  3. Rank surviving topics by evidence, cap to MaxTopicEntries.
//  4. Build index.md from the capped TopicResult list.
//  5. Atomic write: tmpdir holds every file, then the pipeline wipes
//     ~/.ghost/topics/ and renames each file into place. If any stage
//     above failed, the tmpdir is preserved and ~/.ghost/ is left
//     intact.
type Pipeline struct {
	Client          anthropic.Client
	SmartModel      string
	GhostDir        string
	MinRuleEvidence int
	MinRuleProjects int
	MaxTopicEntries int
	// Log, if non-nil, receives progress lines (e.g. topic merges).
	Log func(format string, args ...any)
}

func (p *Pipeline) logf(format string, args ...any) {
	if p.Log != nil {
		p.Log(format, args...)
	}
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
	topicClusters := pickKind(cf.Clusters, "topic")

	userRules := readUserRules(p.GhostDir)

	// Top-level files first. These never depend on topic slugs.
	results := []FileResult{
		BuildIdentity(ctx, p.Client, p.SmartModel, identityClusters),
		BuildRules(ctx, p.Client, p.SmartModel, ruleClusters, userRules),
	}

	// Topic synthesis. Slug collisions are merged (not failed); any
	// per-cluster error or malformed body fails the whole rebuild.
	topicResults, topicFiles, topicErr := BuildTopics(ctx, p.Client, p.SmartModel, topicClusters, p.logf)
	if topicErr != nil {
		// Nothing has been written to tmpDir yet (the write loop runs
		// below), so there is nothing to preserve for debugging — clean it
		// up rather than litter GhostDir with empty .tmp-synthesize-* dirs
		// across repeated failures. Prior ~/.ghost/topics/ is untouched.
		_ = os.RemoveAll(tmpDir)
		return fmt.Errorf("synthesize: %w", topicErr)
	}
	ranked := RankByEvidence(topicResults)
	capped := Cap(ranked, p.MaxTopicEntries)

	// Re-filter files to the capped set so dropped topics don't get
	// written.
	keep := map[string]bool{}
	for _, t := range capped {
		keep[fmt.Sprintf("topics/%s.md", t.Slug)] = true
	}
	for _, f := range topicFiles {
		if keep[f.Name] {
			results = append(results, f)
		}
	}
	results = append(results, BuildIndex(ctx, p.Client, p.SmartModel, capped))

	// Collect any errors from identity/rules/index. Topic errors were
	// returned above.
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
