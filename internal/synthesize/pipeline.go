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
	"github.com/SarahFrankle/ghost/internal/extract"
	"github.com/SarahFrankle/ghost/prompts"
)

// Pipeline orchestrates stage 3: it produces identity.md, rules.md,
// the capped set of topics/*.md, and index.md.
//
// Order of operations:
//  1. Build identity and rules in parallel (they don't depend on topic
//     slugs).
//  2. Run topic synthesis: one topic-model call per topic cluster
//     producing the body under that cluster's themed label; the slug
//     is derived from the label and the `# <label>` heading prepended.
//     Fail loudly on any per-cluster error or a label-slug collision.
//  3. Rank surviving topics by evidence, cap to MaxTopicEntries.
//  4. Build index.md from the capped TopicResult list.
//  5. Atomic write: tmpdir holds every file, then the pipeline wipes
//     ~/.ghost/topics/ and renames each file into place. If any stage
//     above failed, the tmpdir is preserved and ~/.ghost/ is left
//     intact.
type Pipeline struct {
	Client     anthropic.Client
	SmartModel string
	// TopicModel is used for topic synthesis (the high-volume stage).
	// Empty falls back to SmartModel so callers that only set SmartModel
	// keep their old behavior.
	TopicModel              string
	GhostDir                string
	MaxTopicEntries         int
	GeneralityModel         string // routing model (defaults to SmartModel if empty)
	VerdictsPath            string // e.g. <stateDir>/verdicts.json
	RecurrenceForConfidence int    // distinct-conversation count at which a soft preference earns confidence
	// Workers bounds how many topic clusters are synthesized concurrently.
	// Each synthesis is a `claude` subprocess; an unbounded fan-out starves
	// the parent-side stdin writers and trips claude's no-stdin timeout, so
	// this cap is load-bearing, not just a tuning knob. Values < 1 are
	// treated as 1.
	Workers int
	// Log, if non-nil, receives progress lines (e.g. topic merges).
	Log func(format string, args ...any)
	// Progress, if non-nil, is called once per synthesized topic with the
	// running count, for an in-place counter. The caller owns rendering
	// (rewriting line on a TTY, nothing when output is not a terminal).
	Progress func(done, total int)
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

	identityClusters := pickKind(cf.Clusters, extract.KindIdentity)
	prefClusters := pickKind(cf.Clusters, extract.KindPreference)

	genModel := p.GeneralityModel
	if genModel == "" {
		genModel = p.SmartModel
	}
	generalThemes, scopedThemes, routeErr := RouteByGenerality(
		ctx, p.Client, genModel, prefClusters, p.VerdictsPath, prompts.SynthesizeGeneralitySystemHash(), p.logf)
	if routeErr != nil {
		return fmt.Errorf("synthesize: generality routing: %w", routeErr)
	}
	// One confidence gate, applied to both destinations after routing. The LLM
	// verdict chose the destination; confidence decides survival. No per-branch
	// frequency floor (that would re-introduce the rejected hard-frequency gate).
	ruleClusters := gateByConfidence(generalThemes, p.RecurrenceForConfidence)
	topicClusters := gateByConfidence(scopedThemes, p.RecurrenceForConfidence)

	userRules := readUserRules(p.GhostDir)

	// Top-level files first. These never depend on topic slugs.
	p.logf("synthesize: identity.md (%d cluster(s))", len(identityClusters))
	identity := BuildIdentity(ctx, p.Client, p.SmartModel, identityClusters)
	p.logf("synthesize: rules.md (%d cluster(s))", len(ruleClusters))
	rules := BuildRules(ctx, p.Client, p.SmartModel, ruleClusters, userRules)
	results := []FileResult{identity, rules}

	// Topic synthesis. One call per themed cluster; any per-cluster error,
	// label-slug collision, or malformed body fails the whole rebuild.
	topicModel := p.TopicModel
	if topicModel == "" {
		topicModel = p.SmartModel
	}
	topicResults, topicFiles, topicErr := BuildTopics(ctx, p.Client, topicModel, topicClusters, p.Workers, p.logf, p.Progress)
	if topicErr != nil {
		// Nothing has been written to tmpDir yet (the write loop runs
		// below), so there is nothing to preserve for debugging — clean it
		// up rather than litter GhostDir with empty .tmp-synthesize-* dirs
		// across repeated failures. Prior ~/.ghost/topics/ is untouched.
		_ = os.RemoveAll(tmpDir)
		return fmt.Errorf("synthesize: %w", topicErr)
	}
	ranked := RankByEvidence(topicResults)
	capped := Cap(ranked, p.MaxTopicEntries, p.logf)

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
	p.logf("synthesize: index.md (%d topic(s))", len(capped))
	results = append(results, BuildIndex(ctx, p.Client, p.SmartModel, capped, nil))

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

// gateByConfidence keeps clusters the user would trust: those with a
// high-confidence member (a directly asserted fact/preference, valid even
// when stated once -- the stated-once case) OR those that recurred across at
// least recurrenceForConfidence distinct conversations (a soft preference that
// earned trust by repetition). This replaces the retired project_count gate;
// frequency feeds it but is not itself a gate.
func gateByConfidence(clusters []cluster.Cluster, recurrenceForConfidence int) []cluster.Cluster {
	out := make([]cluster.Cluster, 0, len(clusters))
	for _, c := range clusters {
		high := false
		for _, m := range c.Members {
			if m.Confidence == extract.ConfidenceHigh {
				high = true
				break
			}
		}
		recurred := recurrenceForConfidence > 0 && c.ConversationCount >= recurrenceForConfidence
		if high || recurred {
			out = append(out, c)
		}
	}
	return out
}

func pickKind(cs []cluster.Cluster, kind extract.Kind) []cluster.Cluster {
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
