package synthesize

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/SarahFrankle/ghost/internal/cluster"
)

func tr(slug string) TopicResult {
	return TopicResult{Slug: slug, Title: slug, Cluster: cluster.Cluster{Canonical: slug}}
}

func TestCategorizePinnedPassThrough(t *testing.T) {
	calls := 0
	client := &fakeClient{complete: func(_ context.Context, _, _, _ string) (string, error) {
		calls++
		return `{"categories": {"unit-tests": "testing"}}`, nil
	}}
	got, err := Categorize(context.Background(), client,
		"m",
		[]TopicResult{tr("pr-creation"), tr("unit-tests")},
		map[string]string{"pr-creation": "pr"},
		filepath.Join(t.TempDir(), "categories.json"),
		"hash")
	if err != nil {
		t.Fatal(err)
	}
	if got["pr-creation"] != "pr" {
		t.Fatalf("pinned slug should pass through verbatim, got %q", got["pr-creation"])
	}
	if got["unit-tests"] != "testing" {
		t.Fatalf("unpinned slug should get a category, got %q", got["unit-tests"])
	}
}

func TestCategorizeCacheHit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "categories.json")
	calls := 0
	client := &fakeClient{complete: func(_ context.Context, _, _, _ string) (string, error) {
		calls++
		return `{"categories": {"unit-tests": "testing"}}`, nil
	}}
	topics := []TopicResult{tr("unit-tests")}
	for range 2 {
		if _, err := Categorize(context.Background(), client, "m", topics, nil, path, "hash"); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatalf("second call should hit cache; client called %d times", calls)
	}
}
