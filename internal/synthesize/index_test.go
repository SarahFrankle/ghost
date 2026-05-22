package synthesize

import (
	"context"
	"strings"
	"testing"

	"github.com/SarahFrankle/ghost/internal/cluster"
)

func TestRankAndCapTopics(t *testing.T) {
	groups := map[string][]cluster.Cluster{
		"testing": {{EvidenceCount: 10}, {EvidenceCount: 3}}, // 13
		"git":     {{EvidenceCount: 5}},                      // 5
		"writing": {{EvidenceCount: 7}},                      // 7
	}
	ranked := RankTopicsByEvidence(groups)
	if len(ranked) != 3 || ranked[0].Slug != "testing" || ranked[1].Slug != "writing" || ranked[2].Slug != "git" {
		t.Fatalf("unexpected ranking: %+v", ranked)
	}
	capped := CapTopics(ranked, 2)
	if len(capped) != 2 || capped[0].Slug != "testing" || capped[1].Slug != "writing" {
		t.Fatalf("unexpected cap: %+v", capped)
	}
}

func TestBuildIndexProducesOneEntryPerRankedTopic(t *testing.T) {
	f := &fakeClient{resp: "# Index\n\n## Topics\n- topics/testing.md (triggers: tests, pytest)\n- topics/git.md (triggers: rebase, branch)\n"}
	ranked := []RankedTopic{
		{Slug: "testing", EvidenceTotal: 13, Canonicals: []string{"prefer table-driven"}},
		{Slug: "git", EvidenceTotal: 5, Canonicals: []string{"rebase before merge"}},
	}
	res := BuildIndex(context.Background(), f, "smart", ranked)
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	if res.Name != "index.md" {
		t.Fatalf("expected index.md, got %s", res.Name)
	}
	if !strings.Contains(f.gotUser, "topics/testing.md") || !strings.Contains(f.gotUser, "topics/git.md") {
		t.Fatalf("prompt did not list both topic paths: %q", f.gotUser)
	}
}

func TestBuildIndexEmptyWhenNoTopics(t *testing.T) {
	f := &fakeClient{resp: "should not be called"}
	res := BuildIndex(context.Background(), f, "smart", nil)
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	if !strings.Contains(res.Content, "No lazy-loaded topics") {
		t.Fatalf("expected placeholder, got %q", res.Content)
	}
}
