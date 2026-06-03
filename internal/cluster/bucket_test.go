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
	clusters := Bucket(members, func(i int) []float32 { return vecs[i] }, func(string) float32 { return 0.85 })

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
	clusters := Bucket(members, identical, func(string) float32 { return 0.5 })
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
	clusters := Bucket(members, identical, func(string) float32 { return 0.5 })
	if len(clusters) != 2 {
		t.Fatalf("different voice contexts must not merge: got %d", len(clusters))
	}
}

func TestBucketAppliesPerKindThreshold(t *testing.T) {
	members := []ClusterMember{
		{Kind: "topic", Text: "documentation should be example-first", Project: "a"},
		{Kind: "topic", Text: "docs should lead with examples", Project: "b"},
		{Kind: "rule", Text: "no mocks in integration tests", Project: "a"},
		{Kind: "rule", Text: "avoid mocks at boundaries", Project: "b"},
	}
	vecs := map[int][]float32{
		0: {1, 0, 0},
		1: {0.8, 0.6, 0}, // ~0.80 cosine with vec 0
		2: {0, 1, 0},
		3: {0, 0.8, 0.6}, // ~0.80 cosine with vec 2
	}
	thresholdFor := func(kind string) float32 {
		if kind == "topic" {
			return 0.75
		}
		return 0.85
	}
	clusters := Bucket(members, func(i int) []float32 { return vecs[i] }, thresholdFor)

	// Topics: at threshold 0.75, vec 0 and vec 1 should merge.
	// Rules: at threshold 0.85, vec 2 and vec 3 should NOT merge.
	var topicCount, ruleCount int
	var mergedTopic bool
	for _, c := range clusters {
		switch c.Kind {
		case "topic":
			topicCount++
			if len(c.Members) == 2 {
				mergedTopic = true
			}
		case "rule":
			ruleCount++
		}
	}
	if !mergedTopic {
		t.Fatalf("topic observations should have merged at 0.75: %+v", clusters)
	}
	if ruleCount != 2 {
		t.Fatalf("rule observations should NOT have merged at 0.85: got %d rule clusters", ruleCount)
	}
	if topicCount != 1 {
		t.Fatalf("expected 1 topic cluster, got %d", topicCount)
	}
}
