package cluster

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/SarahFrankle/ghost/internal/extract"
)

// TestSeedAnchoringStableAndUnforced exercises Tasks 1-3 together through
// TopicGrouper.Run with fake closures. It asserts four properties:
//
//	(a) a label matching a seed name lands on the exact seed slug
//	(b) the surviving topic's Canonical is byte-identical across two runs (stability)
//	(c) a seed name with no matching labels produces no cluster
//	(d) an unrelated single-member label is dropped as noise, not forced onto a seed
func TestSeedAnchoringStableAndUnforced(t *testing.T) {
	// Three observations: two about draft PRs (meet MinClusterSize=2), one about
	// testing (single member — noise under MinClusterSize=2).
	members := []ClusterMember{
		{ObservationHash: "h1", Kind: extract.KindPreference, Text: "opens PRs as draft"},
		{ObservationHash: "h2", Kind: extract.KindPreference, Text: "opens PRs as draft for self-review"},
		{ObservationHash: "h3", Kind: extract.KindPreference, Text: "writes unit tests first"},
	}

	newGrouper := func() *TopicGrouper {
		dir := t.TempDir()
		return &TopicGrouper{
			// Label assigns a raw label per observation text.
			Label: func(_ context.Context, text string) (string, error) {
				if strings.Contains(text, "PR") {
					return "draft pull requests", nil
				}
				return "test first", nil
			},
			// ThemeIdentify returns "testing-discipline" as the discovered theme.
			// The seed "pr-creation" is not discovered — Run unions it in via
			// unionThemes, proving the seed union path (Task 3).
			ThemeIdentify: func(_ context.Context, _ []string) ([]string, error) {
				return []string{"testing-discipline"}, nil
			},
			// ThemeMap maps labels onto the unioned theme set.
			// "draft pull requests" contains "pull request" → maps to seed "pr-creation".
			// "test first" → maps to "testing-discipline".
			// "deployment-rollback" is a seed with no label matches, so ThemeMap
			// never receives a label for it and no cluster is produced.
			ThemeMap: func(_ context.Context, _ []string, labels []string) (map[string]string, error) {
				m := map[string]string{}
				for _, l := range labels {
					if strings.Contains(l, "pull request") {
						m[l] = "pr-creation"
					} else {
						m[l] = "testing-discipline"
					}
				}
				return m, nil
			},
			// SeedNames: "pr-creation" has label evidence (will survive);
			// "deployment-rollback" has none (must produce no cluster).
			SeedNames:      []string{"pr-creation", "deployment-rollback"},
			MinClusterSize: 2,
			Workers:        1,
			Cache:          mustLabelCache(t, dir),
		}
	}

	run := func() []Cluster {
		c, err := newGrouper().Run(context.Background(), members)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}

	a, b := run(), run()

	// Build Canonical sets for both runs.
	canonA := map[string]bool{}
	for _, c := range a {
		canonA[c.Canonical] = true
	}
	canonB := map[string]bool{}
	for _, c := range b {
		canonB[c.Canonical] = true
	}

	// (a) label matching seed "pr-creation" lands on the exact seed slug.
	if !canonA["pr-creation"] {
		t.Fatalf("seeded label should land on exact seed slug pr-creation; got clusters: %v", canonicalNames(a))
	}

	// (c) seed with no matching labels must not produce a cluster.
	if canonA["deployment-rollback"] {
		t.Fatal("seed with no matching labels must not produce a cluster")
	}

	// (b) Canonical set is byte-identical across both runs (stability).
	if !reflect.DeepEqual(canonA, canonB) {
		t.Fatalf("canonical set not stable across runs; run1=%v run2=%v", canonicalNames(a), canonicalNames(b))
	}

	// (d) single-member "test first" label maps to "testing-discipline", which has
	// only one member (below MinClusterSize=2) and is dropped as noise, not forced
	// onto any seed.
	if canonA["testing-discipline"] {
		t.Fatal("single-member testing label should drop as noise, not be forced onto a seed")
	}
}

// canonicalNames returns the Canonical field of each cluster for diagnostic output.
func canonicalNames(cs []Cluster) []string {
	names := make([]string, len(cs))
	for i, c := range cs {
		names[i] = c.Canonical
	}
	return names
}
