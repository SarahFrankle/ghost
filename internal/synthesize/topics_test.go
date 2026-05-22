package synthesize

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/SarahFrankle/ghost/internal/cluster"
)

func TestGroupTopicClustersBySlug(t *testing.T) {
	cs := []cluster.Cluster{
		{Kind: "topic", SubKey: "testing", Canonical: "prefer table-driven", EvidenceCount: 4, ProjectCount: 2},
		{Kind: "topic", SubKey: "testing", Canonical: "avoid mocks at boundaries", EvidenceCount: 3, ProjectCount: 2},
		{Kind: "topic", SubKey: "git", Canonical: "rebase before merge", EvidenceCount: 5, ProjectCount: 3},
		{Kind: "rule", SubKey: "", Canonical: "should be skipped", EvidenceCount: 9, ProjectCount: 9},
		{Kind: "topic", SubKey: "", Canonical: "missing slug; skip", EvidenceCount: 2, ProjectCount: 2},
	}
	groups := GroupTopicClusters(cs)
	slugs := make([]string, 0, len(groups))
	for s := range groups {
		slugs = append(slugs, s)
	}
	sort.Strings(slugs)
	if strings.Join(slugs, ",") != "git,testing" {
		t.Fatalf("unexpected slugs: %v", slugs)
	}
	if len(groups["testing"]) != 2 || len(groups["git"]) != 1 {
		t.Fatalf("unexpected sizes: testing=%d git=%d", len(groups["testing"]), len(groups["git"]))
	}
}

func TestBuildTopicsCallsModelOncePerSlug(t *testing.T) {
	f := &fakeClient{resp: "# Testing\n\n- prefer table-driven\n"}
	groups := map[string][]cluster.Cluster{
		"testing": {{Kind: "topic", SubKey: "testing", Canonical: "prefer table-driven",
			EvidenceCount: 4, ProjectCount: 2,
			Members: []cluster.ClusterMember{{Text: "prefer table-driven", Evidence: "turn 3", Project: "p"}}}},
		"git": {{Kind: "topic", SubKey: "git", Canonical: "rebase before merge",
			EvidenceCount: 5, ProjectCount: 3,
			Members: []cluster.ClusterMember{{Text: "rebase before merge", Evidence: "turn 7", Project: "q"}}}},
	}
	results := BuildTopics(context.Background(), f, "smart", groups)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	names := map[string]bool{}
	for _, r := range results {
		names[r.Name] = true
		if r.Err != nil {
			t.Fatalf("%s: %v", r.Name, r.Err)
		}
		if !strings.HasSuffix(r.Name, ".md") || !strings.HasPrefix(r.Name, "topics/") {
			t.Fatalf("unexpected output filename: %s", r.Name)
		}
	}
	if !names["topics/testing.md"] || !names["topics/git.md"] {
		t.Fatalf("missing expected file: %v", names)
	}
}
