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

// TopicResult is one synthesized topic: the cluster it came from, the
// slug derived from the body's H1, the body itself, and the total
// evidence count. Used by BuildIndex to rank and link topics.
type TopicResult struct {
	Cluster       cluster.Cluster
	Slug          string
	Title         string
	Body          string
	EvidenceTotal int
}

// synthFunc synthesizes one cluster into a title (the parsed H1) and a
// full markdown body. Injected so the merge loop is testable without a
// live model.
type synthFunc func(ctx context.Context, c cluster.Cluster) (title, body string, err error)

// BuildTopics synthesizes the kind=topic clusters into one file per
// surviving topic. It adapts the smart-model client into a synthFunc and
// delegates to buildTopics. The public result shape is unchanged.
func BuildTopics(ctx context.Context, client anthropic.Client, model string, clusters []cluster.Cluster, logf func(string, ...any)) ([]TopicResult, []FileResult, error) {
	synth := func(ctx context.Context, c cluster.Cluster) (string, string, error) {
		raw, err := client.Complete(ctx, model, prompts.SynthesizeTopicsSystem(), renderTopicPayload(c))
		if err != nil {
			return "", "", err
		}
		body := ensureTrailingNewline(strings.TrimSpace(raw))
		title, err := ParseH1(body)
		if err != nil {
			return "", "", err
		}
		return title, body, nil
	}
	return buildTopics(ctx, synth, clusters, logf)
}

// topicWork is one cluster moving through the merge loop, carrying its
// cached synthesis result. synthed=false means it needs (re-)synthesis.
type topicWork struct {
	cluster cluster.Cluster
	title   string
	body    string
	synthed bool
}

// buildTopics runs topic synthesis to a unique-slug fixpoint.
//
// Each round: synthesize every not-yet-synthesized cluster in parallel,
// slugify the titles, and group clusters by slug. Any group larger than
// one is a collision — the strongest possible signal that those clusters
// are the same topic (a smart model independently named them the same).
// Those clusters are merged into one via mergeClusters and re-synthesized
// next round. Clusters with a unique slug keep their cached body.
//
// Every collision round merges >=2 clusters into 1, so the working-set
// count strictly decreases and the loop terminates in <=N rounds. A
// collision-free corpus costs exactly one synthesis pass.
//
// Any synthesis error, malformed body (no leading H1), or slugifier
// reject fails the whole topics rebuild: index.md references the slug
// set, so partial success is not a useful state.
func buildTopics(ctx context.Context, synth synthFunc, clusters []cluster.Cluster, logf func(string, ...any)) ([]TopicResult, []FileResult, error) {
	work := make([]*topicWork, 0, len(clusters))
	for _, c := range clusters {
		if c.Kind == "topic" {
			work = append(work, &topicWork{cluster: c})
		}
	}
	if len(work) == 0 {
		return nil, nil, nil
	}

	for {
		if err := synthRound(ctx, synth, work); err != nil {
			return nil, nil, err
		}

		slugOf := make([]string, len(work))
		bySlug := map[string][]int{}
		for i, w := range work {
			slug, err := Slug(w.title)
			if err != nil {
				return nil, nil, fmt.Errorf("topics: slugify cluster %q (title %q): %w", w.cluster.Canonical, w.title, err)
			}
			slugOf[i] = slug
			bySlug[slug] = append(bySlug[slug], i)
		}

		collided := false
		for _, idxs := range bySlug {
			if len(idxs) > 1 {
				collided = true
				break
			}
		}
		if !collided {
			trs, files := emitTopics(work, slugOf)
			return trs, files, nil
		}

		slugs := make([]string, 0, len(bySlug))
		for s := range bySlug {
			slugs = append(slugs, s)
		}
		sort.Strings(slugs)

		next := make([]*topicWork, 0, len(work))
		for _, slug := range slugs {
			idxs := bySlug[slug]
			if len(idxs) == 1 {
				next = append(next, work[idxs[0]])
				continue
			}
			cs := make([]cluster.Cluster, 0, len(idxs))
			for _, i := range idxs {
				cs = append(cs, work[i].cluster)
			}
			next = append(next, &topicWork{cluster: mergeClusters(cs)})
			if logf != nil {
				logf("topics: merged %d clusters -> %q", len(idxs), slug)
			}
		}
		work = next
	}
}

// synthRound synthesizes every cluster in work that has no cached body,
// in parallel. Any error fails the round.
func synthRound(ctx context.Context, synth synthFunc, work []*topicWork) error {
	type res struct {
		idx   int
		title string
		body  string
		err   error
	}
	var todo []int
	for i, w := range work {
		if !w.synthed {
			todo = append(todo, i)
		}
	}
	if len(todo) == 0 {
		return nil
	}

	out := make([]res, len(todo))
	var wg sync.WaitGroup
	for j, idx := range todo {
		j, idx := j, idx
		wg.Add(1)
		go func() {
			defer wg.Done()
			title, body, err := synth(ctx, work[idx].cluster)
			out[j] = res{idx: idx, title: title, body: body, err: err}
		}()
	}
	wg.Wait()

	var failed []string
	for _, r := range out {
		if r.err != nil {
			failed = append(failed, fmt.Sprintf("cluster %q: %v", work[r.idx].cluster.Canonical, r.err))
			continue
		}
		work[r.idx].title = r.title
		work[r.idx].body = r.body
		work[r.idx].synthed = true
	}
	if len(failed) > 0 {
		return fmt.Errorf("topics: %d cluster(s) failed: %s", len(failed), strings.Join(failed, "; "))
	}
	return nil
}

// emitTopics builds the slug-sorted result and file lists from a fully
// synthesized, collision-free working set. slugOf[i] is the slug for
// work[i], already computed and validated by buildTopics.
func emitTopics(work []*topicWork, slugOf []string) ([]TopicResult, []FileResult) {
	type row struct {
		slug string
		w    *topicWork
	}
	rows := make([]row, len(work))
	for i, w := range work {
		rows[i] = row{slug: slugOf[i], w: w}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].slug < rows[j].slug })

	trs := make([]TopicResult, 0, len(rows))
	files := make([]FileResult, 0, len(rows))
	for _, r := range rows {
		trs = append(trs, TopicResult{
			Cluster:       r.w.cluster,
			Slug:          r.slug,
			Title:         r.w.title,
			Body:          r.w.body,
			EvidenceTotal: r.w.cluster.EvidenceCount,
		})
		files = append(files, FileResult{
			Name:    fmt.Sprintf("topics/%s.md", r.slug),
			Content: r.w.body,
		})
	}
	return trs, files
}

// mergeClusters combines colliding topic clusters into one synthetic
// cluster, mirroring how cluster.Bucket forms a cluster. Pure and
// deterministic: members are concatenated and sorted by ObservationHash
// (ties by Text) so the same input always yields the same re-synthesis
// payload. EvidenceCount is the total member count; ProjectCount is the
// size of the project union. Canonical is taken from the highest-evidence
// input cluster (ties broken by the lexicographically smallest Canonical)
// so the dominant cluster names the merged topic.
//
// Precondition: cs is non-empty — callers pass a collision group of >=2.
// An empty slice panics by design (a caller bug). The result is always
// Kind "topic" with an empty SubKey, since topic clusters carry no SubKey.
func mergeClusters(cs []cluster.Cluster) cluster.Cluster {
	var members []cluster.ClusterMember
	projects := map[string]struct{}{}
	for _, c := range cs {
		members = append(members, c.Members...)
		for _, m := range c.Members {
			if m.Project != "" {
				projects[m.Project] = struct{}{}
			}
		}
	}
	sort.Slice(members, func(i, j int) bool {
		if members[i].ObservationHash != members[j].ObservationHash {
			return members[i].ObservationHash < members[j].ObservationHash
		}
		return members[i].Text < members[j].Text
	})

	best := cs[0]
	for _, c := range cs[1:] {
		if c.EvidenceCount > best.EvidenceCount ||
			(c.EvidenceCount == best.EvidenceCount && c.Canonical < best.Canonical) {
			best = c
		}
	}

	return cluster.Cluster{
		Kind:          "topic",
		Canonical:     best.Canonical,
		Members:       members,
		EvidenceCount: len(members),
		ProjectCount:  len(projects),
	}
}

func renderTopicPayload(c cluster.Cluster) string {
	var b strings.Builder
	b.WriteString("CLUSTER:\n")
	b.WriteString(renderClusters([]cluster.Cluster{c}))
	return b.String()
}
