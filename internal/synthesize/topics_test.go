package synthesize

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

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
	trs, files, err := buildTopics(context.Background(), synth, []cluster.Cluster{tc("errs"), tc("docs")}, 4, nil, nil)
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
	trs, files, err := buildTopics(context.Background(), synth, []cluster.Cluster{tc("a"), tc("b")}, 4, nil, nil)
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
	trs, _, err := buildTopics(context.Background(), synth, []cluster.Cluster{tc("a"), tc("b"), tc("c")}, 4, nil, nil)
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
	_, _, err := buildTopics(context.Background(), synth, []cluster.Cluster{tc("a"), tc("b"), tc("d"), tc("e")}, 4, nil, nil)
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
	r1, _, err := buildTopics(context.Background(), synth, in, 4, nil, nil)
	if err != nil {
		t.Fatalf("run 1 error: %v", err)
	}
	r2, _, err := buildTopics(context.Background(), synth, in, 4, nil, nil)
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

func TestBuildTopicsBoundsConcurrency(t *testing.T) {
	// Each synthesis is a `claude` subprocess. An unbounded fan-out starves
	// the parent-side stdin writers and trips claude's no-stdin timeout, so
	// synthesis must never run more than `workers` clusters at once. Distinct
	// markers give distinct slugs (no merge), so this is a single round of
	// len(cs) synths — enough to exceed the limit if the cap is missing.
	const (
		nClusters = 16
		limit     = 4
	)
	var mu sync.Mutex
	var inflight, maxInflight int
	synth := func(ctx context.Context, c cluster.Cluster) (string, string, error) {
		mu.Lock()
		inflight++
		if inflight > maxInflight {
			maxInflight = inflight
		}
		mu.Unlock()
		// Hold the slot so concurrent launches actually overlap.
		time.Sleep(20 * time.Millisecond)
		mu.Lock()
		inflight--
		mu.Unlock()
		return c.Canonical, bodyFor(c.Canonical), nil
	}
	cs := make([]cluster.Cluster, nClusters)
	for i := range cs {
		cs[i] = tc(fmt.Sprintf("t%02d", i))
	}
	if _, _, err := buildTopics(context.Background(), synth, cs, limit, nil, nil); err != nil {
		t.Fatalf("buildTopics error: %v", err)
	}
	if maxInflight > limit {
		t.Fatalf("observed %d concurrent syntheses, want <= %d", maxInflight, limit)
	}
}

func TestBuildTopicsReportsProgress(t *testing.T) {
	// Each synthesized topic must fire the progress callback once, with a
	// monotonic count and a stable total, so a long otherwise-silent run can
	// render an in-place counter. Distinct markers => distinct slugs => one
	// round of n synths.
	const n = 5
	cs := make([]cluster.Cluster, n)
	for i := range cs {
		cs[i] = tc(fmt.Sprintf("t%02d", i))
	}
	synth := func(ctx context.Context, c cluster.Cluster) (string, string, error) {
		return c.Canonical, bodyFor(c.Canonical), nil
	}
	var mu sync.Mutex
	var calls, maxDone int
	progress := func(done, total int) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		if total != n {
			t.Errorf("progress total = %d, want %d", total, n)
		}
		if done > maxDone {
			maxDone = done
		}
	}
	if _, _, err := buildTopics(context.Background(), synth, cs, 4, nil, progress); err != nil {
		t.Fatalf("buildTopics error: %v", err)
	}
	if calls != n {
		t.Fatalf("progress fired %d times, want %d", calls, n)
	}
	if maxDone != n {
		t.Fatalf("max done = %d, want %d", maxDone, n)
	}
}

func TestBuildTopicsMalformedBodyFails(t *testing.T) {
	s := &scriptedClient{responses: []string{"Some preamble.\n\n# Title\n"}}
	_, _, err := BuildTopics(context.Background(), s, "smart", []cluster.Cluster{tc("c")}, 4, nil, nil)
	if err == nil {
		t.Fatal("BuildTopics should fail when body's first line is not an H1")
	}
}

func TestBuildTopicsClientErrorFails(t *testing.T) {
	s := &scriptedClient{responses: []string{""}, errs: []error{errors.New("boom")}}
	_, _, err := BuildTopics(context.Background(), s, "smart", []cluster.Cluster{tc("c")}, 4, nil, nil)
	if err == nil {
		t.Fatal("BuildTopics should fail when the client errors")
	}
}

func TestBuildTopicsLongTitleTruncatesNotFails(t *testing.T) {
	// A single over-length title must no longer abort the whole rebuild;
	// it is truncated to a valid slug and the topic survives.
	long := "This Title Is Far Too Long To Be A Reasonable Slug For A Topic File"
	synth := func(ctx context.Context, c cluster.Cluster) (string, string, error) {
		return long, bodyFor(long), nil
	}
	trs, files, err := buildTopics(context.Background(), synth, []cluster.Cluster{tc("x")}, 4, nil, nil)
	if err != nil {
		t.Fatalf("buildTopics should not fail on a long title: %v", err)
	}
	if len(trs) != 1 || len(files) != 1 {
		t.Fatalf("want 1 topic/1 file, got %d/%d", len(trs), len(files))
	}
	if trs[0].Slug != "this-title-is-far-too-long-to-be-a" {
		t.Fatalf("slug = %q, want this-title-is-far-too-long-to-be-a", trs[0].Slug)
	}
}

func TestBuildTopicsUnslugifiableTitleFails(t *testing.T) {
	synth := func(ctx context.Context, c cluster.Cluster) (string, string, error) {
		return "!!!", bodyFor("!!!"), nil
	}
	_, _, err := buildTopics(context.Background(), synth, []cluster.Cluster{tc("x")}, 4, nil, nil)
	if err == nil {
		t.Fatal("buildTopics should fail when a title does not slugify")
	}
}
