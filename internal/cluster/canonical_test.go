package cluster

import (
	"context"
	"strings"
	"testing"
)

type fakeLLM struct{ resp string }

func (f *fakeLLM) Complete(ctx context.Context, model, system, user string) (string, error) {
	return f.resp, nil
}

func TestCanonicalizeSingletonSkipsLLM(t *testing.T) {
	clusters := []Cluster{
		{Kind: "rule", Members: []ClusterMember{{Text: "only one"}}, Canonical: "only one"},
	}
	called := false
	c := &Canonicalizer{Client: &fakeLLM{resp: "SHOULD NOT BE CALLED"}, Model: "x"}
	c.OnCall = func() { called = true }
	if err := c.Apply(context.Background(), clusters); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("singleton bucket must skip LLM")
	}
	if clusters[0].Canonical != "only one" {
		t.Fatalf("singleton canonical mutated: %q", clusters[0].Canonical)
	}
}

func TestCanonicalizeMultiUsesLLMResponse(t *testing.T) {
	clusters := []Cluster{
		{Kind: "rule", Members: []ClusterMember{{Text: "A"}, {Text: "B"}}, Canonical: "A"},
	}
	c := &Canonicalizer{Client: &fakeLLM{resp: `{"canonical":"prefer X","same":true}`}, Model: "x"}
	if err := c.Apply(context.Background(), clusters); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(clusters[0].Canonical, "prefer X") {
		t.Fatalf("canonical not set from LLM: %q", clusters[0].Canonical)
	}
}
