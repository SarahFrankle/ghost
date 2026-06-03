# Chunk 3 collision → merge Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace fail-on-slug-collision in topic synthesis with a merge-and-resynthesize loop that runs to a unique-slug fixpoint, so `ghost compose` completes on the real corpus and chunk 3 can ship.

**Architecture:** A slug collision is the merge signal, not an error. Colliding topic clusters are combined by a pure `mergeClusters` helper and re-synthesized; the loop repeats until every slug is unique. The synthesis call is injected as a `synthFunc` so the loop is testable without a live model — the same "pure logic, caller supplies the impure part" pattern as `internal/cluster/bucket.go`.

**Tech Stack:** Go (existing toolchain), Anthropic Claude smart-model client (existing), `nomic-embed-text` via Ollama for embeddings (existing, unchanged).

**Source of truth:** `docs/specs/2026-06-03-chunk-3-collision-merge-design.md`. If this plan conflicts with the design, the design wins — stop and reconcile before coding.

**Branch:** continue on `chunk-3-embedding-topics`. Per-task commits stay on the branch; the chunk squash-merges to `main` as one commit per [[feedback-squash-per-chunk]].

---

## File Structure

### Modify
- `internal/synthesize/topics.go` — add pure `mergeClusters`; add `synthFunc` type; extract the per-cluster synth+parse into an injectable function; replace the pass-2 collision check with the merge-to-fixpoint loop; add a `logf func(string, ...any)` parameter to `BuildTopics`. Public result types (`TopicResult`, `FileResult`) unchanged.
- `internal/synthesize/topics_test.go` — drop the collision-fails test; add `mergeClusters` unit tests and loop tests (merge, fixpoint, caching, determinism) driven by a fake `synthFunc`; keep malformed-body and client-error tests, updated for the new `BuildTopics` signature.
- `internal/synthesize/pipeline.go` — add a `Log func(format string, args ...any)` field and a nil-safe `logf` method; pass `p.logf` into `BuildTopics`; update the stale "fail loudly on slug collision" comments.
- `cmd/compose.go` — wire `Log: log.Printf` into the `synthesize.Pipeline` in `runSynthesize`.
- `docs/superpowers/plans/2026-05-22-chunk-3-embedding-topics.md` — update the STATUS block: the threshold-tweak finish-line is dead; collision → merge is the finish-line.

### Untouched (deliberate)
- `internal/cluster/*` — bucketing is correct; the fix is entirely in synthesis.
- `internal/synthesize/slugify.go`, `index.go` — no change.
- `internal/config/config.go` — `cluster_cosine_topic` stays `0.75`; now a granularity preference, not a correctness knob.

---

## Task 1: Pure `mergeClusters` helper

**Files:**
- Modify: `internal/synthesize/topics.go`
- Test: `internal/synthesize/topics_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/synthesize/topics_test.go`:

```go
func TestMergeClustersCombinesMembersAndCounts(t *testing.T) {
	a := cluster.Cluster{
		Kind: "topic", Canonical: "A", EvidenceCount: 1, ProjectCount: 1,
		Members: []cluster.ClusterMember{{ObservationHash: "h2", Text: "a1", Project: "p1"}},
	}
	b := cluster.Cluster{
		Kind: "topic", Canonical: "B", EvidenceCount: 2, ProjectCount: 2,
		Members: []cluster.ClusterMember{
			{ObservationHash: "h3", Text: "b2", Project: "p2"},
			{ObservationHash: "h1", Text: "b1", Project: "p1"},
		},
	}
	m := mergeClusters([]cluster.Cluster{a, b})

	if m.Kind != "topic" {
		t.Fatalf("Kind = %q, want topic", m.Kind)
	}
	if len(m.Members) != 3 {
		t.Fatalf("len(Members) = %d, want 3", len(m.Members))
	}
	// Members sorted by ObservationHash: h1, h2, h3.
	if m.Members[0].ObservationHash != "h1" || m.Members[1].ObservationHash != "h2" || m.Members[2].ObservationHash != "h3" {
		t.Fatalf("members not sorted by hash: %+v", m.Members)
	}
	if m.EvidenceCount != 3 {
		t.Fatalf("EvidenceCount = %d, want 3 (member count)", m.EvidenceCount)
	}
	if m.ProjectCount != 2 {
		t.Fatalf("ProjectCount = %d, want 2 (union of p1,p2)", m.ProjectCount)
	}
	// b has the higher EvidenceCount, so its canonical names the merge.
	if m.Canonical != "B" {
		t.Fatalf("Canonical = %q, want B (highest-evidence input cluster)", m.Canonical)
	}
}

func TestMergeClustersCanonicalTieBreak(t *testing.T) {
	a := cluster.Cluster{
		Kind: "topic", Canonical: "zebra", EvidenceCount: 1,
		Members: []cluster.ClusterMember{{ObservationHash: "h1", Text: "x"}},
	}
	b := cluster.Cluster{
		Kind: "topic", Canonical: "apple", EvidenceCount: 1,
		Members: []cluster.ClusterMember{{ObservationHash: "h2", Text: "y"}},
	}
	m := mergeClusters([]cluster.Cluster{a, b})
	if m.Canonical != "apple" {
		t.Fatalf("Canonical = %q, want apple (lexicographically smallest on evidence tie)", m.Canonical)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/synthesize/ -run TestMergeClusters -v`
Expected: FAIL with "undefined: mergeClusters".

- [ ] **Step 3: Implement `mergeClusters`**

Add to `internal/synthesize/topics.go` (the `sort` import is already present):

```go
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
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/synthesize/ -run TestMergeClusters -v`
Expected: PASS for both tests.

- [ ] **Step 5: Commit**

```bash
git add internal/synthesize/topics.go internal/synthesize/topics_test.go
git commit -m "synthesize: add pure mergeClusters helper"
```

---

## Task 2: Merge-to-fixpoint loop in `BuildTopics`

**Files:**
- Modify: `internal/synthesize/topics.go`
- Test: `internal/synthesize/topics_test.go`

This task replaces fail-on-collision with the merge loop, introduces an injectable `synthFunc`, and adds a `logf` parameter. The malformed-body and client-error failure modes are preserved.

- [ ] **Step 1: Replace the topic tests**

Replace the entire contents of `internal/synthesize/topics_test.go` with the following. (The `mergeClusters` tests from Task 1 are included here so the file is complete; do not duplicate them.)

```go
package synthesize

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/SarahFrankle/ghost/internal/cluster"
)

// scriptedClient returns a different response for each Complete call, in
// order. Used by the BuildTopics-level tests that drive a single cluster
// (where call order is unambiguous).
type scriptedClient struct {
	responses []string
	errs      []error
	mu        sync.Mutex
	calls     int
}

func (s *scriptedClient) Complete(ctx context.Context, model, system, user string) (string, error) {
	s.mu.Lock()
	i := s.calls
	s.calls++
	s.mu.Unlock()
	if i < len(s.errs) && s.errs[i] != nil {
		return "", s.errs[i]
	}
	if i >= len(s.responses) {
		return "", errors.New("scriptedClient: no response for call")
	}
	return s.responses[i], nil
}

// tc builds a one-member topic cluster whose canonical, member text, and
// observation hash are all the given marker, so tests can identify it.
func tc(marker string) cluster.Cluster {
	return cluster.Cluster{
		Kind: "topic", Canonical: marker, EvidenceCount: 1, ProjectCount: 1,
		Members: []cluster.ClusterMember{{ObservationHash: marker, Text: marker, Project: "p"}},
	}
}

// memberSet returns the set of member texts in a (possibly merged) cluster.
func memberSet(c cluster.Cluster) map[string]bool {
	s := map[string]bool{}
	for _, m := range c.Members {
		s[m.Text] = true
	}
	return s
}

// bodyFor wraps a title in a minimal valid topic body.
func bodyFor(title string) string { return "# " + title + "\n\n- x\n" }

func TestMergeClustersCombinesMembersAndCounts(t *testing.T) {
	a := cluster.Cluster{
		Kind: "topic", Canonical: "A", EvidenceCount: 1, ProjectCount: 1,
		Members: []cluster.ClusterMember{{ObservationHash: "h2", Text: "a1", Project: "p1"}},
	}
	b := cluster.Cluster{
		Kind: "topic", Canonical: "B", EvidenceCount: 2, ProjectCount: 2,
		Members: []cluster.ClusterMember{
			{ObservationHash: "h3", Text: "b2", Project: "p2"},
			{ObservationHash: "h1", Text: "b1", Project: "p1"},
		},
	}
	m := mergeClusters([]cluster.Cluster{a, b})

	if m.Kind != "topic" {
		t.Fatalf("Kind = %q, want topic", m.Kind)
	}
	if len(m.Members) != 3 {
		t.Fatalf("len(Members) = %d, want 3", len(m.Members))
	}
	if m.Members[0].ObservationHash != "h1" || m.Members[1].ObservationHash != "h2" || m.Members[2].ObservationHash != "h3" {
		t.Fatalf("members not sorted by hash: %+v", m.Members)
	}
	if m.EvidenceCount != 3 {
		t.Fatalf("EvidenceCount = %d, want 3", m.EvidenceCount)
	}
	if m.ProjectCount != 2 {
		t.Fatalf("ProjectCount = %d, want 2", m.ProjectCount)
	}
	if m.Canonical != "B" {
		t.Fatalf("Canonical = %q, want B", m.Canonical)
	}
}

func TestMergeClustersCanonicalTieBreak(t *testing.T) {
	a := cluster.Cluster{
		Kind: "topic", Canonical: "zebra", EvidenceCount: 1,
		Members: []cluster.ClusterMember{{ObservationHash: "h1", Text: "x"}},
	}
	b := cluster.Cluster{
		Kind: "topic", Canonical: "apple", EvidenceCount: 1,
		Members: []cluster.ClusterMember{{ObservationHash: "h2", Text: "y"}},
	}
	m := mergeClusters([]cluster.Cluster{a, b})
	if m.Canonical != "apple" {
		t.Fatalf("Canonical = %q, want apple", m.Canonical)
	}
}

func TestBuildTopicsDistinctNoMerge(t *testing.T) {
	titles := map[string]string{"errs": "Error Handling", "docs": "Documentation"}
	synth := func(ctx context.Context, c cluster.Cluster) (string, string, error) {
		ti := titles[c.Canonical]
		return ti, bodyFor(ti), nil
	}
	trs, files, err := buildTopics(context.Background(), synth, []cluster.Cluster{tc("errs"), tc("docs")}, nil)
	if err != nil {
		t.Fatalf("buildTopics error: %v", err)
	}
	if len(trs) != 2 || len(files) != 2 {
		t.Fatalf("want 2 topics/2 files, got %d/%d", len(trs), len(files))
	}
	names := map[string]bool{}
	for _, f := range files {
		names[f.Name] = true
	}
	if !names["topics/error-handling.md"] || !names["topics/documentation.md"] {
		t.Fatalf("unexpected slugs: %v", names)
	}
}

func TestBuildTopicsMergesCollidingClusters(t *testing.T) {
	// Any cluster containing member "a" or "b" is titled the same -> they merge.
	synth := func(ctx context.Context, c cluster.Cluster) (string, string, error) {
		ms := memberSet(c)
		ti := "Documentation"
		if ms["a"] || ms["b"] {
			ti = "Pull Requests"
		}
		return ti, bodyFor(ti), nil
	}
	trs, files, err := buildTopics(context.Background(), synth, []cluster.Cluster{tc("a"), tc("b")}, nil)
	if err != nil {
		t.Fatalf("buildTopics error: %v", err)
	}
	if len(trs) != 1 || len(files) != 1 {
		t.Fatalf("want 1 merged topic, got %d results / %d files", len(trs), len(files))
	}
	if trs[0].Slug != "pull-requests" {
		t.Fatalf("slug = %q, want pull-requests", trs[0].Slug)
	}
	if trs[0].EvidenceTotal != 2 {
		t.Fatalf("EvidenceTotal = %d, want 2 (summed)", trs[0].EvidenceTotal)
	}
	ms := memberSet(trs[0].Cluster)
	if !ms["a"] || !ms["b"] {
		t.Fatalf("merged cluster missing members: %v", ms)
	}
}

func TestBuildTopicsFixpointSecondOrderMerge(t *testing.T) {
	// Round 1: a->Alpha, b->Alpha, c->Bravo. Alpha collides -> merge {a,b}.
	// Round 2: {a,b}->Bravo (has both a&b), c is cached Bravo. Bravo collides -> merge {a,b,c}.
	// Round 3: {a,b,c}->Bravo, unique -> done.
	synth := func(ctx context.Context, c cluster.Cluster) (string, string, error) {
		ms := memberSet(c)
		var ti string
		switch {
		case ms["a"] && ms["b"]:
			ti = "Bravo"
		case ms["a"] || ms["b"]:
			ti = "Alpha"
		case ms["c"]:
			ti = "Bravo"
		}
		return ti, bodyFor(ti), nil
	}
	trs, _, err := buildTopics(context.Background(), synth, []cluster.Cluster{tc("a"), tc("b"), tc("c")}, nil)
	if err != nil {
		t.Fatalf("buildTopics error: %v", err)
	}
	if len(trs) != 1 {
		t.Fatalf("want 1 topic after fixpoint, got %d", len(trs))
	}
	if trs[0].Slug != "bravo" {
		t.Fatalf("slug = %q, want bravo", trs[0].Slug)
	}
	if trs[0].EvidenceTotal != 3 {
		t.Fatalf("EvidenceTotal = %d, want 3", trs[0].EvidenceTotal)
	}
	ms := memberSet(trs[0].Cluster)
	if !ms["a"] || !ms["b"] || !ms["c"] {
		t.Fatalf("merged cluster missing members: %v", ms)
	}
}

func TestBuildTopicsCachesUnmergedBodies(t *testing.T) {
	var mu sync.Mutex
	calls := map[string]int{}
	keyOf := func(c cluster.Cluster) string {
		var ts []string
		for _, m := range c.Members {
			ts = append(ts, m.Text)
		}
		sort.Strings(ts)
		return strings.Join(ts, "|")
	}
	synth := func(ctx context.Context, c cluster.Cluster) (string, string, error) {
		mu.Lock()
		calls[keyOf(c)]++
		mu.Unlock()
		ms := memberSet(c)
		switch {
		case ms["a"] || ms["b"]:
			return "Pull Requests", bodyFor("Pull Requests"), nil
		case ms["d"]:
			return "Documentation", bodyFor("Documentation"), nil
		default:
			return "Testing", bodyFor("Testing"), nil
		}
	}
	_, _, err := buildTopics(context.Background(), synth, []cluster.Cluster{tc("a"), tc("b"), tc("d"), tc("e")}, nil)
	if err != nil {
		t.Fatalf("buildTopics error: %v", err)
	}
	// Round 1 synths a,b,d,e once each; a&b collide -> merge; round 2 synths a|b once.
	// d and e are never re-synthesized.
	for _, k := range []string{"a", "b", "d", "e", "a|b"} {
		if calls[k] != 1 {
			t.Fatalf("calls[%q] = %d, want 1; full map %v", k, calls[k], calls)
		}
	}
	if len(calls) != 5 {
		t.Fatalf("unexpected synth calls: %v", calls)
	}
}

func TestBuildTopicsDeterministic(t *testing.T) {
	synth := func(ctx context.Context, c cluster.Cluster) (string, string, error) {
		ms := memberSet(c)
		var ti string
		switch {
		case ms["a"] && ms["b"]:
			ti = "Bravo"
		case ms["a"] || ms["b"]:
			ti = "Alpha"
		case ms["c"]:
			ti = "Bravo"
		}
		return ti, bodyFor(ti), nil
	}
	in := []cluster.Cluster{tc("a"), tc("b"), tc("c")}
	r1, _, err := buildTopics(context.Background(), synth, in, nil)
	if err != nil {
		t.Fatalf("run 1 error: %v", err)
	}
	r2, _, err := buildTopics(context.Background(), synth, in, nil)
	if err != nil {
		t.Fatalf("run 2 error: %v", err)
	}
	if len(r1) != len(r2) {
		t.Fatalf("nondeterministic length: %d vs %d", len(r1), len(r2))
	}
	for i := range r1 {
		if r1[i].Slug != r2[i].Slug || r1[i].Cluster.Canonical != r2[i].Cluster.Canonical {
			t.Fatalf("nondeterministic result at %d: %+v vs %+v", i, r1[i], r2[i])
		}
		for j := range r1[i].Cluster.Members {
			if r1[i].Cluster.Members[j].ObservationHash != r2[i].Cluster.Members[j].ObservationHash {
				t.Fatalf("nondeterministic member order at topic %d member %d", i, j)
			}
		}
	}
}

func TestBuildTopicsMalformedBodyFails(t *testing.T) {
	s := &scriptedClient{responses: []string{"Some preamble.\n\n# Title\n"}}
	_, _, err := BuildTopics(context.Background(), s, "smart", []cluster.Cluster{tc("c")}, nil)
	if err == nil {
		t.Fatal("BuildTopics should fail when body's first line is not an H1")
	}
}

func TestBuildTopicsClientErrorFails(t *testing.T) {
	s := &scriptedClient{responses: []string{""}, errs: []error{errors.New("boom")}}
	_, _, err := BuildTopics(context.Background(), s, "smart", []cluster.Cluster{tc("c")}, nil)
	if err == nil {
		t.Fatal("BuildTopics should fail when the client errors")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/synthesize/ -run "TestBuildTopics|TestMergeClusters" -v`
Expected: FAIL — `buildTopics` is undefined and `BuildTopics` still has the old 4-argument signature.

- [ ] **Step 3: Rewrite `topics.go`**

Replace the entire contents of `internal/synthesize/topics.go` with:

```go
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

		bySlug := map[string][]int{}
		slugOf := make([]string, len(work))
		for i, w := range work {
			slug, err := Slug(w.title)
			if err != nil {
				return nil, nil, fmt.Errorf("topics: slugify cluster %q (title %q): %w", w.cluster.Canonical, w.title, err)
			}
			slugOf[i] = slug
			bySlug[slug] = append(bySlug[slug], i)
		}

		slugs := make([]string, 0, len(bySlug))
		for s := range bySlug {
			slugs = append(slugs, s)
		}
		sort.Strings(slugs)

		next := make([]*topicWork, 0, len(work))
		collided := false
		for _, slug := range slugs {
			idxs := bySlug[slug]
			if len(idxs) == 1 {
				next = append(next, work[idxs[0]])
				continue
			}
			collided = true
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
		if !collided {
			break
		}
	}

	return emitTopics(work)
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
// synthesized, collision-free working set.
func emitTopics(work []*topicWork) ([]TopicResult, []FileResult, error) {
	type row struct {
		slug string
		w    *topicWork
	}
	rows := make([]row, 0, len(work))
	for _, w := range work {
		slug, err := Slug(w.title)
		if err != nil {
			return nil, nil, fmt.Errorf("topics: slugify cluster %q (title %q): %w", w.cluster.Canonical, w.title, err)
		}
		rows = append(rows, row{slug: slug, w: w})
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
	return trs, files, nil
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
```

Note: the `mergeClusters` body is identical to Task 1's — it now lives in the rewritten file. Do not define it twice.

- [ ] **Step 4: Run the synthesize tests to verify they pass**

Run: `go test ./internal/synthesize/ -run "TestBuildTopics|TestMergeClusters" -v`
Expected: PASS for all tests.

The package as a whole will not build yet — `pipeline.go` still calls `BuildTopics` with the old 4-argument signature. That's Task 3.

- [ ] **Step 5: Commit**

```bash
git add internal/synthesize/topics.go internal/synthesize/topics_test.go
git commit -m "synthesize: collision is a merge signal — loop topics to a unique-slug fixpoint"
```

---

## Task 3: Wire the merge log through the pipeline and command

**Files:**
- Modify: `internal/synthesize/pipeline.go`
- Modify: `cmd/compose.go`

- [ ] **Step 1: Add a logger to the synthesize Pipeline**

In `internal/synthesize/pipeline.go`, add a `Log` field to the `Pipeline` struct. The struct currently ends with `MaxTopicEntries int`:

```go
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
```

- [ ] **Step 2: Pass the logger into BuildTopics and fix the stale comments**

In `internal/synthesize/pipeline.go`, change the call site:

```go
	topicResults, topicFiles, topicErr := BuildTopics(ctx, p.Client, p.SmartModel, topicClusters, p.logf)
```

Update the two stale comments in the same file that describe the old fail-on-collision behavior:

- In the `Pipeline` doc comment, change the step-2 line from:

```
//  2. Run topic synthesis: one smart-model call per topic cluster
//     producing a body that starts with `# <Title>`; slugify each
//     title; fail loudly on slug collision or any per-cluster error.
```

  to:

```
//  2. Run topic synthesis: one smart-model call per topic cluster
//     producing a body that starts with `# <Title>`; slugify each
//     title; merge any slug collisions and re-synthesize to a
//     unique-slug fixpoint; fail loudly on any per-cluster error.
```

- Just above the `BuildTopics` call, change:

```go
	// Topic synthesis. Any per-cluster error, malformed body, or slug
	// collision fails the whole rebuild.
```

  to:

```go
	// Topic synthesis. Slug collisions are merged (not failed); any
	// per-cluster error or malformed body fails the whole rebuild.
```

- [ ] **Step 3: Build the synthesize package**

Run: `go build ./internal/synthesize/`
Expected: PASS.

- [ ] **Step 4: Wire `log.Printf` in the command**

In `cmd/compose.go`, in `runSynthesize`, the `synthesize.Pipeline` is constructed as a struct literal ending with `MaxTopicEntries: cfg.Index.MaxTopicEntries,`. Add the `Log` field:

```go
	p := &synthesize.Pipeline{
		Client:          client,
		SmartModel:      cfg.Models.Smart,
		GhostDir:        outDir,
		MinRuleEvidence: cfg.Thresholds.RuleMinEvidenceCount,
		MinRuleProjects: cfg.Thresholds.RuleMinProjectCount,
		MaxTopicEntries: cfg.Index.MaxTopicEntries,
		Log:             log.Printf,
	}
```

The `log` package is already imported in `cmd/compose.go` (used for the fingerprint-sidecar warning). If `go build` reports it as unused or missing, adjust the import accordingly.

- [ ] **Step 5: Build the whole repo**

Run: `go build ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/synthesize/pipeline.go cmd/compose.go
git commit -m "synthesize: log topic merges; wire pipeline logger from compose"
```

---

## Task 4: Full verification (build, vet, race)

**Files:** none (verification only)

- [ ] **Step 1: Vet**

Run: `go vet ./...`
Expected: no output (clean).

- [ ] **Step 2: Full test suite with race detector**

Run: `go test -race ./...`
Expected: PASS across all packages. The synthesize loop uses goroutines in `synthRound`; the race detector must report no data races.

- [ ] **Step 3: Commit (only if any fixups were needed)**

If steps 1–2 required changes, commit them:

```bash
git add -A
git commit -m "synthesize: fixups from vet/race verification"
```

If nothing changed, skip this step.

---

## Task 5: End-to-end at the default threshold

**Files:** none (verification only)

This proves the real corpus now composes clean at `cluster_cosine_topic = 0.75` (no config override), which the threshold sweep could not achieve.

- [ ] **Step 1: Run cluster + synthesize on the real corpus**

Run: `go run . compose --stages cluster,synthesize`
Expected: exit 0. Stdout/stderr should include `topics: merged N clusters -> "..."` lines for the PR-family collisions, then `synthesize: wrote identity.md, rules.md, topics/*.md, index.md`. No `slug collision` error.

- [ ] **Step 2: Verify the topics directory and index are populated and consistent**

Run: `ls ~/.ghost/topics/ && echo "---" && grep -c '(topics/' ~/.ghost/index.md`
Expected: a populated `topics/` directory; `index.md` references topic slugs. Spot-check that a merged PR file (e.g. `topics/pull-requests.md`) contains bullets from more than one of the original colliding clusters.

- [ ] **Step 3: Verify stability under re-run**

Run: `go run . compose --stages synthesize`
Expected: either `synthesize: up to date (fingerprint match)`, or a clean re-run producing the same slug set. No collision error.

- [ ] **Step 4: No commit**

This task changes only `~/.ghost/` state, not the repo. Nothing to commit.

---

## Task 6: Update the chunk-3 plan status

**Files:**
- Modify: `docs/superpowers/plans/2026-05-22-chunk-3-embedding-topics.md`

- [ ] **Step 1: Replace the STATUS block**

In `docs/superpowers/plans/2026-05-22-chunk-3-embedding-topics.md`, replace the dead threshold-tweak finish-line in the STATUS block (the sentence beginning "To finish chunk 3: set `cluster_cosine_topic = 0.70`...") with:

```
> To finish chunk 3: collision → merge is implemented (see
> `docs/superpowers/plans/2026-06-03-chunk-3-collision-merge.md` and
> `docs/specs/2026-06-03-chunk-3-collision-merge-design.md`). The
> threshold sweep proved no `cluster_cosine_topic` value clears
> collisions on `nomic-embed-text` — collisions live at the titling
> step, not bucketing — so fail-loud-on-collision was replaced with
> merge-to-fixpoint. The default `0.75` ships unchanged. After e2e
> verification, squash-merge per [[feedback-squash-per-chunk]].
```

- [ ] **Step 2: Commit**

```bash
git add docs/superpowers/plans/2026-05-22-chunk-3-embedding-topics.md
git commit -m "docs: chunk-3 finish-line is collision→merge, not threshold tuning"
```

---

## Task 7: Finish the branch

**Files:** none (integration)

- [ ] **Step 1: Confirm everything is green and committed**

Run: `git status && go test ./...`
Expected: clean working tree; all tests pass.

- [ ] **Step 2: Squash-merge to main**

REQUIRED SUB-SKILL: Use superpowers:finishing-a-development-branch to complete the integration. Per [[feedback-squash-per-chunk]], the chunk lands on `main` as one squash commit named for the whole chunk 3 (embedding-based topics + collision→merge), with the granular per-task commits left on the branch.

---

## Self-Review

- **Spec coverage:**
  - `mergeClusters` (pure: member concat + sort, evidence sum, project union, canonical selection) → Task 1, re-stated in Task 2's file rewrite.
  - Fixpoint loop, no fail branch for collisions → Task 2 (`buildTopics`).
  - Injected `synthFunc` for testability → Task 2.
  - Body caching (re-synth only merged clusters) → Task 2 (`topicWork.synthed`, `synthRound`), verified by `TestBuildTopicsCachesUnmergedBodies`.
  - Termination/determinism → Task 2 (sorted slug iteration, sorted members), verified by `TestBuildTopicsDeterministic`.
  - Synthesis-error all-or-nothing preserved → Task 2 (`synthRound` aggregation), `TestBuildTopicsClientErrorFails`/`TestBuildTopicsMalformedBodyFails`.
  - Merge log line → Task 2 (`logf`) + Task 3 (wiring).
  - Pipeline atomicity / rank / cap untouched → no task modifies them; merged `EvidenceCount` is summed so cap behavior is automatic.
  - Decision-1 reversal recorded → already committed (`3508ddc`); plan STATUS update → Task 6.
  - Real e2e at 0.75 → Task 5.
- **Placeholder scan:** none — every code/command step is complete.
- **Type consistency:** `BuildTopics(ctx, client, model, clusters, logf)` and `buildTopics(ctx, synth, clusters, logf)` signatures are consistent across Tasks 2–3 and the test file. `synthFunc` returns `(title, body string, err error)` everywhere. `topicWork` fields (`cluster`, `title`, `body`, `synthed`) are used consistently. `Pipeline.Log`/`logf` consistent across Task 3.
