package synthesize

import (
	"context"
	"strings"
	"testing"

	"github.com/SarahFrankle/ghost/internal/cluster"
)

type fakeClient struct {
	gotUser string
	resp    string
}

func (f *fakeClient) Complete(ctx context.Context, model, system, user string) (string, error) {
	f.gotUser = user
	return f.resp, nil
}

func TestFilterRulesAppliesThresholds(t *testing.T) {
	clusters := []cluster.Cluster{
		{Kind: "rule", Canonical: "ok rule", EvidenceCount: 3, ProjectCount: 2},
		{Kind: "rule", Canonical: "too few projects", EvidenceCount: 5, ProjectCount: 1},
		{Kind: "rule", Canonical: "too little evidence", EvidenceCount: 1, ProjectCount: 1},
		{Kind: "identity", Canonical: "should be filtered out by kind", EvidenceCount: 9, ProjectCount: 9},
	}
	got := FilterRules(clusters, 2, 2)
	if len(got) != 1 || got[0].Canonical != "ok rule" {
		t.Fatalf("unexpected filtered set: %+v", got)
	}
}

func TestBuildRulesIncludesUserRulesInPrompt(t *testing.T) {
	f := &fakeClient{resp: "# Rules\n\n- foo\n"}
	clusters := []cluster.Cluster{
		{Kind: "rule", Canonical: "thing", EvidenceCount: 2, ProjectCount: 2,
			Members: []cluster.ClusterMember{{Text: "thing", Evidence: "turn 1", Project: "p"}}},
	}
	res := BuildRules(context.Background(), f, "smart", clusters, "rules.user.md\n- avoid em-dashes\n")
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	if !strings.Contains(f.gotUser, "avoid em-dashes") {
		t.Fatalf("user-rules not passed to model: %q", f.gotUser)
	}
}
