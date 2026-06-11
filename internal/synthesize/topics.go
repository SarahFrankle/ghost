package synthesize

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/SarahFrankle/ghost/internal/anthropic"
	"github.com/SarahFrankle/ghost/internal/cluster"
	"github.com/SarahFrankle/ghost/prompts"
)

// TopicResult is one synthesized topic: the cluster it came from, the slug
// derived from the cluster's themed label, the label as title, the body, and
// the total evidence count. Used by BuildIndex to rank and link topics.
type TopicResult struct {
	Cluster       cluster.Cluster
	Slug          string
	Title         string
	Body          string
	EvidenceTotal int
}

// synthFunc synthesizes one cluster into the markdown body that goes *under*
// the topic's title. The title is the cluster's themed label (c.Canonical),
// supplied to the model and prepended as the `# <label>` heading by
// buildTopics — the model never invents it. Injected so buildTopics is
// testable without a live model.
type synthFunc func(ctx context.Context, c cluster.Cluster) (body string, err error)

// BuildTopics synthesizes the kind=topic clusters into one file per topic. It
// adapts the smart-model client into a synthFunc and delegates to buildTopics.
// The public result shape is unchanged.
func BuildTopics(ctx context.Context, client anthropic.Client, model string, clusters []cluster.Cluster, workers int, logf func(string, ...any), progress func(done, total int)) ([]TopicResult, []FileResult, error) {
	synth := func(ctx context.Context, c cluster.Cluster) (string, error) {
		raw, err := client.Complete(ctx, model, prompts.SynthesizeTopicsSystem(), renderTopicPayload(c))
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(raw), nil
	}
	return buildTopics(ctx, synth, clusters, workers, logf, progress)
}

// buildTopics synthesizes each kind=topic cluster into exactly one topic file.
//
// The grouping stage has already consolidated observations into themed
// clusters, so there is no merging here: one synth call per cluster, the slug
// derived deterministically from the themed label (c.Canonical), and the file
// titled with that same label. The model writes only the body that goes under
// the title; buildTopics prepends the `# <label>` heading so the file title,
// slug, and index entry share one source of truth and can never drift.
//
// Any failure fails the whole topics rebuild — index.md references the slug
// set and topics are lazy-loaded as authoritative reference, so a partial set
// silently misleads downstream consumers. Loudly wrong beats silently wrong:
//   - a label that does not slugify,
//   - two distinct labels whose slugs collide (a theme-prompt bug — distinct
//     themes must yield distinct files, never a silent merge),
//   - any synth error,
//   - an empty body, or a body that opens with its own H1 (the model invented
//     a title instead of writing under the supplied one).
func buildTopics(ctx context.Context, synth synthFunc, clusters []cluster.Cluster, workers int, logf func(string, ...any), progress func(done, total int)) ([]TopicResult, []FileResult, error) {
	// Callers pass the routed scoped clusters directly; no kind filtering here.
	topics := clusters
	if len(topics) == 0 {
		return nil, nil, nil
	}

	if logf != nil {
		logf("synthesize: topics: %d cluster(s) to synthesize", len(topics))
	}

	// Slug every label up front and reject collisions before spending any
	// model calls: an unslugifiable label or a duplicate slug is a theme-prompt
	// bug, not a per-cluster failure, so catch it for free.
	slugOf := make([]string, len(topics))
	labelForSlug := map[string]string{}
	for i, c := range topics {
		slug, err := Slug(c.Canonical)
		if err != nil {
			return nil, nil, fmt.Errorf("topics: slugify label %q: %w", c.Canonical, err)
		}
		if prev, ok := labelForSlug[slug]; ok {
			return nil, nil, fmt.Errorf("topics: distinct labels %q and %q both slugify to %q", prev, c.Canonical, slug)
		}
		labelForSlug[slug] = c.Canonical
		slugOf[i] = slug
	}

	bodies, err := synthAll(ctx, synth, topics, workers, progress)
	if err != nil {
		return nil, nil, err
	}

	type row struct {
		slug    string
		cluster cluster.Cluster
		content string
	}
	rows := make([]row, len(topics))
	for i, c := range topics {
		body := strings.TrimSpace(bodies[i])
		if body == "" {
			return nil, nil, fmt.Errorf("topics: empty body for label %q", c.Canonical)
		}
		// The label is the title; a body with its own H1 means the model
		// invented one and would double-head the file.
		if _, err := ParseH1(body); err == nil {
			return nil, nil, fmt.Errorf("topics: body for label %q opens with its own H1; the title is supplied by the label", c.Canonical)
		}
		content := ensureTrailingNewline(fmt.Sprintf("# %s\n\n%s", c.Canonical, body))
		rows[i] = row{slug: slugOf[i], cluster: c, content: content}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].slug < rows[j].slug })

	trs := make([]TopicResult, 0, len(rows))
	files := make([]FileResult, 0, len(rows))
	for _, r := range rows {
		trs = append(trs, TopicResult{
			Cluster:       r.cluster,
			Slug:          r.slug,
			Title:         r.cluster.Canonical,
			Body:          r.content,
			EvidenceTotal: r.cluster.EvidenceCount,
		})
		files = append(files, FileResult{
			Name:    fmt.Sprintf("topics/%s.md", r.slug),
			Content: r.content,
		})
	}
	return trs, files, nil
}

// synthAll synthesizes every cluster's body in parallel, bounded by workers.
// Any single failure fails the whole batch.
func synthAll(ctx context.Context, synth synthFunc, topics []cluster.Cluster, workers int, progress func(done, total int)) ([]string, error) {
	// Bound the fan-out: each synth is a `claude` subprocess, and launching
	// every cluster at once starves the parent-side stdin writers, tripping
	// claude's no-stdin timeout. A semaphore caps in-flight subprocesses,
	// matching the extract stage.
	workers = max(workers, 1)
	sem := make(chan struct{}, workers)
	bodies := make([]string, len(topics))
	errs := make([]error, len(topics))
	total := len(topics)
	var progressMu sync.Mutex
	var done int
	var wg sync.WaitGroup
	for i := range topics {
		sem <- struct{}{}
		wg.Go(func() {
			defer func() { <-sem }()
			bodies[i], errs[i] = synth(ctx, topics[i])
			// One in-place counter update per completion; the caller decides
			// how to render it (rewriting line on a TTY, nothing otherwise).
			if progress != nil {
				progressMu.Lock()
				done++
				n := done
				progressMu.Unlock()
				progress(n, total)
			}
		})
	}
	wg.Wait()

	var failed []string
	for i, err := range errs {
		if err != nil {
			failed = append(failed, fmt.Sprintf("cluster %q: %v", topics[i].Canonical, err))
		}
	}
	if len(failed) > 0 {
		return nil, fmt.Errorf("topics: %d cluster(s) failed: %s", len(failed), strings.Join(failed, "; "))
	}
	return bodies, nil
}

func renderTopicPayload(c cluster.Cluster) string {
	return fmt.Sprintf("TITLE: %s\n\nCLUSTER:\n%s", c.Canonical, renderClusters([]cluster.Cluster{c}))
}
