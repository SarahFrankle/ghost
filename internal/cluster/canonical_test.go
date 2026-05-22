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

func TestCanonicalCacheHitSkipsLLM(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/canonical_cache.json"

	cache, _ := LoadCanonicalCache(path, "cheap-v1")
	cl := Cluster{Kind: "rule", Members: []ClusterMember{{Text: "a"}, {Text: "b"}}}
	cache.Put(CanonicalKey(cl), "cached phrasing")

	called := 0
	c := &Canonicalizer{
		Client: &fakeLLM{resp: `{"canonical":"FRESH","same":true}`},
		Model:  "x",
		Cache:  cache,
		OnCall: func() { called++ },
	}
	clusters := []Cluster{cl}
	if err := c.Apply(context.Background(), clusters); err != nil {
		t.Fatal(err)
	}
	if called != 0 {
		t.Fatalf("cache hit must skip LLM (called=%d)", called)
	}
	if clusters[0].Canonical != "cached phrasing" {
		t.Fatalf("canonical not loaded from cache: %q", clusters[0].Canonical)
	}
}

func TestCanonicalKeyStableUnderReorder(t *testing.T) {
	a := Cluster{Kind: "rule", Members: []ClusterMember{{Text: "x"}, {Text: "y"}}}
	b := Cluster{Kind: "rule", Members: []ClusterMember{{Text: "y"}, {Text: "x"}}}
	if CanonicalKey(a) != CanonicalKey(b) {
		t.Fatal("member order must not affect canonical key")
	}
}

func TestCanonicalCacheRoundTripDiscardsOnModelMismatch(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/canonical_cache.json"

	c1, _ := LoadCanonicalCache(path, "cheap-v1")
	c1.Put("k", "v")
	if err := c1.Save(path); err != nil {
		t.Fatal(err)
	}
	c2, err := LoadCanonicalCache(path, "cheap-v2")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := c2.Get("k"); ok {
		t.Fatal("model-id mismatch must discard entries")
	}
}
