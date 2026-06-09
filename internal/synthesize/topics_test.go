package synthesize

import (
	"context"
	"errors"
	"fmt"
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

// tc builds a one-member topic cluster whose canonical (the themed label),
// member text, and observation hash are all the given marker, so tests can
// identify it.
func tc(marker string) cluster.Cluster {
	return cluster.Cluster{
		Kind: "topic", Canonical: marker, EvidenceCount: 1, ProjectCount: 1,
		Members: []cluster.ClusterMember{{ObservationHash: marker, Text: marker, Project: "p"}},
	}
}

// underTitle is a minimal valid synthesized body: the content that goes
// *under* the supplied title, with no H1 of its own (buildTopics prepends
// the `# <label>` heading).
const underTitle = "- x\n"

func TestBuildTopicsOneTopicPerCluster(t *testing.T) {
	var mu sync.Mutex
	calls := map[string]int{}
	synth := func(ctx context.Context, c cluster.Cluster) (string, error) {
		mu.Lock()
		calls[c.Canonical]++
		mu.Unlock()
		return underTitle, nil
	}
	in := []cluster.Cluster{
		{
			Kind: "topic", Canonical: "Error Handling", EvidenceCount: 2, ProjectCount: 1,
			Members: []cluster.ClusterMember{{ObservationHash: "h1", Text: "e1"}, {ObservationHash: "h2", Text: "e2"}},
		},
		{
			Kind: "topic", Canonical: "Documentation", EvidenceCount: 1, ProjectCount: 1,
			Members: []cluster.ClusterMember{{ObservationHash: "h3", Text: "d1"}},
		},
	}
	trs, files, err := buildTopics(context.Background(), synth, in, 4, nil, nil)
	if err != nil {
		t.Fatalf("buildTopics error: %v", err)
	}
	if len(trs) != 2 || len(files) != 2 {
		t.Fatalf("want 2 topics/2 files, got %d/%d", len(trs), len(files))
	}
	// Exactly one synth call per cluster: no re-synthesis, no fixpoint.
	for _, k := range []string{"Error Handling", "Documentation"} {
		if calls[k] != 1 {
			t.Fatalf("calls[%q] = %d, want 1; full map %v", k, calls[k], calls)
		}
	}
	// Slug and title both derive from the themed label; results sorted by slug.
	if trs[0].Slug != "documentation" || trs[0].Title != "Documentation" {
		t.Fatalf("trs[0] slug/title = %q/%q, want documentation/Documentation", trs[0].Slug, trs[0].Title)
	}
	if trs[1].Slug != "error-handling" || trs[1].Title != "Error Handling" {
		t.Fatalf("trs[1] slug/title = %q/%q, want error-handling/Error Handling", trs[1].Slug, trs[1].Title)
	}
	// Evidence count passes through from the cluster.
	if trs[1].EvidenceTotal != 2 {
		t.Fatalf("EvidenceTotal = %d, want 2", trs[1].EvidenceTotal)
	}
	// The file is titled with the supplied label as its H1, followed by the body.
	if files[0].Content != "# Documentation\n\n- x\n" {
		t.Fatalf("files[0].Content = %q, want titled body", files[0].Content)
	}
}

func TestBuildTopicsDuplicateSlugFails(t *testing.T) {
	// Two distinct themed labels that slugify identically signal a theme-prompt
	// bug; distinct themes must yield distinct files, never a silent merge.
	synth := func(ctx context.Context, c cluster.Cluster) (string, error) {
		return underTitle, nil
	}
	_, _, err := buildTopics(context.Background(), synth, []cluster.Cluster{tc("Pull Requests"), tc("pull requests")}, 4, nil, nil)
	if err == nil {
		t.Fatal("buildTopics should fail when two distinct labels slugify identically")
	}
	if !strings.Contains(err.Error(), "pull-requests") {
		t.Fatalf("error should name the colliding slug, got: %v", err)
	}
}

func TestBuildTopicsUnslugifiableLabelFails(t *testing.T) {
	synth := func(ctx context.Context, c cluster.Cluster) (string, error) {
		return underTitle, nil
	}
	_, _, err := buildTopics(context.Background(), synth, []cluster.Cluster{tc("!!!")}, 4, nil, nil)
	if err == nil {
		t.Fatal("buildTopics should fail when a label does not slugify")
	}
}

func TestBuildTopicsLongLabelTruncatesSlug(t *testing.T) {
	// An over-length label must not abort the rebuild: the slug is truncated to
	// a valid filename while the full label remains the title.
	long := "This Title Is Far Too Long To Be A Reasonable Slug For A Topic File"
	synth := func(ctx context.Context, c cluster.Cluster) (string, error) {
		return underTitle, nil
	}
	trs, files, err := buildTopics(context.Background(), synth, []cluster.Cluster{tc(long)}, 4, nil, nil)
	if err != nil {
		t.Fatalf("buildTopics should not fail on a long label: %v", err)
	}
	if len(trs) != 1 || len(files) != 1 {
		t.Fatalf("want 1 topic/1 file, got %d/%d", len(trs), len(files))
	}
	if trs[0].Slug != "this-title-is-far-too-long-to-be-a" {
		t.Fatalf("slug = %q, want this-title-is-far-too-long-to-be-a", trs[0].Slug)
	}
	if trs[0].Title != long {
		t.Fatalf("title = %q, want the full label", trs[0].Title)
	}
}

func TestBuildTopicsDeterministic(t *testing.T) {
	synth := func(ctx context.Context, c cluster.Cluster) (string, error) {
		return underTitle, nil
	}
	in := []cluster.Cluster{tc("Charlie"), tc("Alpha"), tc("Bravo")}
	r1, _, err := buildTopics(context.Background(), synth, in, 4, nil, nil)
	if err != nil {
		t.Fatalf("run 1 error: %v", err)
	}
	r2, _, err := buildTopics(context.Background(), synth, in, 4, nil, nil)
	if err != nil {
		t.Fatalf("run 2 error: %v", err)
	}
	if len(r1) != 3 || len(r2) != 3 {
		t.Fatalf("want 3 topics each, got %d and %d", len(r1), len(r2))
	}
	wantOrder := []string{"alpha", "bravo", "charlie"}
	for i := range r1 {
		if r1[i].Slug != wantOrder[i] || r2[i].Slug != wantOrder[i] {
			t.Fatalf("slug at %d = %q/%q, want %q (sorted, deterministic)", i, r1[i].Slug, r2[i].Slug, wantOrder[i])
		}
	}
}

func TestBuildTopicsBoundsConcurrency(t *testing.T) {
	// Each synthesis is a `claude` subprocess. An unbounded fan-out starves
	// the parent-side stdin writers and trips claude's no-stdin timeout, so
	// synthesis must never run more than `workers` clusters at once.
	const (
		nClusters = 16
		limit     = 4
	)
	var mu sync.Mutex
	var inflight, maxInflight int
	synth := func(ctx context.Context, c cluster.Cluster) (string, error) {
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
		return underTitle, nil
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
	// render an in-place counter.
	const n = 5
	cs := make([]cluster.Cluster, n)
	for i := range cs {
		cs[i] = tc(fmt.Sprintf("t%02d", i))
	}
	synth := func(ctx context.Context, c cluster.Cluster) (string, error) {
		return underTitle, nil
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

func TestBuildTopicsEmptyBodyFails(t *testing.T) {
	// An empty model response leaves the topic file with only a title; index.md
	// would link to a hollow file, so fail loud instead.
	s := &scriptedClient{responses: []string{"   \n"}}
	_, _, err := BuildTopics(context.Background(), s, "smart", []cluster.Cluster{tc("c")}, 4, nil, nil)
	if err == nil {
		t.Fatal("BuildTopics should fail when the synthesized body is empty")
	}
}

func TestBuildTopicsBodyWithOwnH1Fails(t *testing.T) {
	// The label supplies the title; a body that opens with its own H1 means the
	// model invented a title (would double-head the file). Fail loud.
	s := &scriptedClient{responses: []string{"# Invented Title\n\n- x\n"}}
	_, _, err := BuildTopics(context.Background(), s, "smart", []cluster.Cluster{tc("c")}, 4, nil, nil)
	if err == nil {
		t.Fatal("BuildTopics should fail when the body opens with its own H1")
	}
}

func TestBuildTopicsClientErrorFails(t *testing.T) {
	s := &scriptedClient{responses: []string{""}, errs: []error{errors.New("boom")}}
	_, _, err := BuildTopics(context.Background(), s, "smart", []cluster.Cluster{tc("c")}, 4, nil, nil)
	if err == nil {
		t.Fatal("BuildTopics should fail when the client errors")
	}
}
