package synthesize

import (
	"context"
	"strings"
	"testing"

	"github.com/SarahFrankle/ghost/internal/cluster"
)

// A high-confidence stated-once theme must survive Cap even when a soft theme
// has higher evidence: the confidence gate protected it, evidence truncation
// must not undo that.
func TestCapPinsHighConfidence(t *testing.T) {
	highConf := TopicResult{
		Slug: "direct-fact", EvidenceTotal: 1,
		Cluster: cluster.Cluster{Members: []cluster.ClusterMember{{Confidence: "high"}}},
	}
	soft := TopicResult{
		Slug: "soft", EvidenceTotal: 10,
		Cluster: cluster.Cluster{Members: []cluster.ClusterMember{{Confidence: "low"}}},
	}
	ranked := RankByEvidence([]TopicResult{soft, highConf})
	out := Cap(ranked, 1, nil)
	if len(out) != 1 || out[0].Slug != "direct-fact" {
		t.Fatalf("want high-confidence direct-fact pinned, got %+v", out)
	}
}

func TestBuildIndexFromTopicResults(t *testing.T) {
	f := &fakeClient{resp: "# Index\n\n## Topics\n- topics/testing.md (triggers: tests, pytest)\n- topics/git.md (triggers: rebase, branch)\n"}
	topics := []TopicResult{
		{Slug: "testing", Title: "Testing", Body: "# Testing\n\n- prefer tables.\n", EvidenceTotal: 4},
		{Slug: "git", Title: "Git", Body: "# Git\n\n- rebase before merge.\n", EvidenceTotal: 5},
	}
	r := BuildIndex(context.Background(), f, "smart", topics)
	if r.Err != nil {
		t.Fatalf("BuildIndex: %v", r.Err)
	}
	if !strings.Contains(r.Content, "topics/testing.md") {
		t.Fatalf("index missing testing link: %q", r.Content)
	}
}

func TestBuildIndexEmptyTopics(t *testing.T) {
	f := &fakeClient{resp: "should not be called"}
	r := BuildIndex(context.Background(), f, "smart", nil)
	if r.Err != nil {
		t.Fatalf("BuildIndex: %v", r.Err)
	}
	if !strings.Contains(r.Content, "No lazy-loaded topics yet.") {
		t.Fatalf("expected empty-state index, got %q", r.Content)
	}
}
