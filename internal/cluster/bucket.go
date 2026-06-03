package cluster

import "github.com/SarahFrankle/ghost/internal/embedding"

// Bucket partitions members by (kind, sub_key) and within each partition
// merges members whose embeddings are within thresholdFor(kind) cosine of
// an existing cluster's first member (single-linkage agglomerative).
//
// vecOf maps a member index to its embedding. The function never calls
// the embedder itself — callers pre-load vectors so this stays purely
// deterministic and trivially testable.
//
// thresholdFor is consulted per-partition: identity and rule typically want
// a tight threshold (e.g. 0.85), topic wants a looser one (e.g. 0.75).
//
// Buckets of size 1 are returned as-is. Counts are computed here so the
// LLM never produces a number that drives a downstream threshold.
func Bucket(members []ClusterMember, vecOf func(i int) []float32, thresholdFor func(kind string) float32) []Cluster {
	type key struct{ kind, sub string }
	parts := map[key][]int{}
	for i, m := range members {
		k := key{m.Kind, m.SubKey()}
		parts[k] = append(parts[k], i)
	}

	var out []Cluster
	for k, idxs := range parts {
		threshold := thresholdFor(k.kind)
		var clusters [][]int
		for _, i := range idxs {
			vi := vecOf(i)
			placed := false
			for ci, c := range clusters {
				vj := vecOf(c[0])
				if embedding.Cosine(vi, vj) >= threshold {
					clusters[ci] = append(c, i)
					placed = true
					break
				}
			}
			if !placed {
				clusters = append(clusters, []int{i})
			}
		}

		for _, c := range clusters {
			mems := make([]ClusterMember, 0, len(c))
			projects := map[string]struct{}{}
			for _, i := range c {
				mems = append(mems, members[i])
				if members[i].Project != "" {
					projects[members[i].Project] = struct{}{}
				}
			}
			out = append(out, Cluster{
				Kind:          k.kind,
				SubKey:        k.sub,
				Canonical:     members[c[0]].Text,
				Members:       mems,
				EvidenceCount: len(mems),
				ProjectCount:  len(projects),
			})
		}
	}
	return out
}
