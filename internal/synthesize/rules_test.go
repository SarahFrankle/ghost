package synthesize

import (
	"context"
	"strings"
	"testing"

	"github.com/SarahFrankle/ghost/internal/cluster"
)

type fakeClient struct {
	gotUser  string
	resp     string
	complete func(ctx context.Context, model, system, user string) (string, error)
}

func (f *fakeClient) Complete(ctx context.Context, model, system, user string) (string, error) {
	f.gotUser = user
	if f.complete != nil {
		return f.complete(ctx, model, system, user)
	}
	return f.resp, nil
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
