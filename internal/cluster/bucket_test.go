package cluster

import (
	"testing"
)

func TestBucketGroupsSimilarMembers(t *testing.T) {
	members := []ClusterMember{
		{Kind: "rule", Text: "use integration tests", Project: "ghost"},
		{Kind: "rule", Text: "prefer real DB tests", Project: "other"},
		{Kind: "rule", Text: "use semantic commits", Project: "ghost"},
		{Kind: "identity", Text: "works at Miro", Project: "ghost"},
	}
	vecs := map[int][]float32{
		0: {1, 0, 0},
		1: {0.95, 0.1, 0},
		2: {0, 1, 0},
		3: {0, 0, 1},
	}
	clusters := Bucket(members, func(i int) []float32 { return vecs[i] }, 0.85)

	if len(clusters) != 3 {
		t.Fatalf("expected 3 clusters (2 rule + 1 identity), got %d: %+v", len(clusters), clusters)
	}
	for _, c := range clusters {
		if c.Kind == "rule" && len(c.Members) == 2 {
			if c.EvidenceCount != 2 || c.ProjectCount != 2 {
				t.Fatalf("merged rule cluster counts wrong: ev=%d proj=%d", c.EvidenceCount, c.ProjectCount)
			}
		}
	}
}

func TestBucketDoesNotCrossKind(t *testing.T) {
	members := []ClusterMember{
		{Kind: "rule", Text: "x"},
		{Kind: "identity", Text: "x"},
	}
	identical := func(int) []float32 { return []float32{1, 0} }
	clusters := Bucket(members, identical, 0.5)
	if len(clusters) != 2 {
		t.Fatalf("identity and rule must not merge: got %d clusters", len(clusters))
	}
}

func TestBucketRespectsSubKey(t *testing.T) {
	members := []ClusterMember{
		{Kind: "voice", Context: "cli-chat", Text: "lowercase"},
		{Kind: "voice", Context: "slack", Text: "lowercase"},
	}
	identical := func(int) []float32 { return []float32{1, 0} }
	clusters := Bucket(members, identical, 0.5)
	if len(clusters) != 2 {
		t.Fatalf("different voice contexts must not merge: got %d", len(clusters))
	}
}
