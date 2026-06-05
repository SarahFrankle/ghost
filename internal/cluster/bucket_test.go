package cluster

import (
	"sort"
	"strings"
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

// obs0's real geometry: a member is over threshold to THREE clusters
// (0.78 / 0.83 / 0.84) and must join the highest, not the first seen.
// A, B, C are mutually < 0.75 so they stay as three separate seeds.
func TestBucketJoinsBestMatchNotFirst(t *testing.T) {
	members := []ClusterMember{
		{Kind: "topic", Text: "A", ObservationHash: "1"},
		{Kind: "topic", Text: "B", ObservationHash: "2"},
		{Kind: "topic", Text: "C", ObservationHash: "3"},
		{Kind: "topic", Text: "T", ObservationHash: "4"},
	}
	vecs := map[int][]float32{
		0: {0.78, 0.6258, 0, 0}, // cos(T,A)=0.78  (first over threshold)
		1: {0.83, 0, 0.5578, 0}, // cos(T,B)=0.83
		2: {0.84, 0, 0, 0.5426}, // cos(T,C)=0.84  (best)
		3: {1, 0, 0, 0},         // T
	}
	clusters := Bucket(members, func(i int) []float32 { return vecs[i] }, func(string) float32 { return 0.75 })

	if len(clusters) != 3 {
		t.Fatalf("expected 3 clusters (A, B, C+T), got %d", len(clusters))
	}
	for _, c := range clusters {
		texts := map[string]bool{}
		for _, m := range c.Members {
			texts[m.Text] = true
		}
		if texts["T"] {
			if len(c.Members) != 2 || !texts["C"] {
				t.Fatalf("T must join C (best match 0.84), got cluster %v", texts)
			}
		}
	}
}

// Centroid linkage: M is below threshold to the seed S1 (0.70) but above
// threshold to the cluster's running centroid after S2 joins. With
// seed-only linkage M would split off; with centroid linkage it joins.
func TestBucketCentroidLinkageAdmitsDriftedMember(t *testing.T) {
	members := []ClusterMember{
		{Kind: "topic", Text: "S1", ObservationHash: "1"},
		{Kind: "topic", Text: "S2", ObservationHash: "2"},
		{Kind: "topic", Text: "M", ObservationHash: "3"},
	}
	vecs := map[int][]float32{
		0: {1, 0},        // S1
		1: {0.8, 0.6},    // cos(S1,S2)=0.80 -> joins; sum becomes {1.8,0.6}
		2: {0.7, 0.7141}, // cos(M,S1)=0.70 (<0.75) but cos(M,sum)=0.89
	}
	clusters := Bucket(members, func(i int) []float32 { return vecs[i] }, func(string) float32 { return 0.75 })

	if len(clusters) != 1 || len(clusters[0].Members) != 3 {
		t.Fatalf("centroid linkage should admit M into one 3-member cluster, got %d clusters", len(clusters))
	}
}

// Output must not depend on input order (members are sorted by
// ObservationHash internally).
func TestBucketDeterministicAcrossInputOrder(t *testing.T) {
	mk := func(order []int) []Cluster {
		base := []ClusterMember{
			{Kind: "topic", Text: "S1", ObservationHash: "1"},
			{Kind: "topic", Text: "S2", ObservationHash: "2"},
			{Kind: "topic", Text: "M", ObservationHash: "3"},
		}
		vmap := map[string][]float32{"1": {1, 0}, "2": {0.8, 0.6}, "3": {0.7, 0.7141}}
		members := make([]ClusterMember, len(order))
		for i, o := range order {
			members[i] = base[o]
		}
		return Bucket(members, func(i int) []float32 { return vmap[members[i].ObservationHash] }, func(string) float32 { return 0.75 })
	}
	sig := func(cs []Cluster) string {
		parts := []string{}
		for _, c := range cs {
			hs := []string{}
			for _, m := range c.Members {
				hs = append(hs, m.ObservationHash)
			}
			sort.Strings(hs)
			parts = append(parts, c.Canonical+":"+strings.Join(hs, ","))
		}
		sort.Strings(parts)
		return strings.Join(parts, "|")
	}
	if sig(mk([]int{0, 1, 2})) != sig(mk([]int{2, 1, 0})) {
		t.Fatalf("clustering must be identical regardless of input order")
	}
}

// Canonical is the medoid (member nearest the centroid), not the
// first-by-hash seed. Here P2 is nearest the centroid of {P1,P2,P3}.
func TestBucketCanonicalIsMedoid(t *testing.T) {
	members := []ClusterMember{
		{Kind: "topic", Text: "P1", ObservationHash: "1"}, // seed by hash order
		{Kind: "topic", Text: "P2", ObservationHash: "2"}, // medoid
		{Kind: "topic", Text: "P3", ObservationHash: "3"},
	}
	vecs := map[int][]float32{
		0: {1, 0},
		1: {0.95, 0.31},
		2: {0.8, 0.6},
	}
	clusters := Bucket(members, func(i int) []float32 { return vecs[i] }, func(string) float32 { return 0.75 })

	if len(clusters) != 1 {
		t.Fatalf("expected all three to merge, got %d clusters", len(clusters))
	}
	if clusters[0].Canonical != "P2" {
		t.Fatalf("canonical should be medoid P2, got %q", clusters[0].Canonical)
	}
}
