package cluster

import (
	"sort"

	"github.com/SarahFrankle/ghost/internal/embedding"
)

// Bucket partitions members by (kind, sub_key) and, within each partition,
// assigns every member to the existing cluster whose centroid it is most
// similar to — provided that similarity is >= thresholdFor(kind). A member
// matching no cluster seeds a new one. This is best-match agglomerative
// clustering with centroid linkage.
//
// Cosine is scale-invariant, so each cluster's centroid is tracked as the
// running SUM of its member vectors: comparing a candidate against the sum
// is identical to comparing against the mean, with no per-step division.
//
// Members are processed in ObservationHash order so the result is
// reproducible regardless of input (directory) order. Each cluster's
// Canonical is its medoid — the member nearest the centroid — so the
// representative text actually represents the cluster rather than being
// whichever member happened to seed it.
//
// vecOf maps a member index to its embedding. The function never calls the
// embedder itself — callers pre-load vectors so this stays purely
// deterministic and trivially testable. Counts are computed here so the LLM
// never produces a number that drives a downstream threshold.
func Bucket(members []ClusterMember, vecOf func(i int) []float32, thresholdFor func(kind string) float32) []Cluster {
	type key struct{ kind, sub string }
	parts := map[key][]int{}
	for i, m := range members {
		k := key{m.Kind, m.SubKey()}
		parts[k] = append(parts[k], i)
	}

	// Stable partition order so output is reproducible.
	keys := make([]key, 0, len(parts))
	for k := range parts {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(a, b int) bool {
		if keys[a].kind != keys[b].kind {
			return keys[a].kind < keys[b].kind
		}
		return keys[a].sub < keys[b].sub
	})

	type group struct {
		idxs []int
		sum  []float32 // running centroid (un-normalized; cosine is scale-invariant)
	}

	var out []Cluster
	for _, k := range keys {
		idxs := parts[k]
		// Deterministic member order: ObservationHash ascending.
		sort.Slice(idxs, func(a, b int) bool {
			return members[idxs[a]].ObservationHash < members[idxs[b]].ObservationHash
		})

		threshold := thresholdFor(k.kind)
		var groups []*group
		for _, i := range idxs {
			vi := vecOf(i)
			best, bestScore := -1, float32(-1)
			for gi, g := range groups {
				s := embedding.Cosine(vi, g.sum)
				if s >= threshold && s > bestScore { // strict > => ties go to lowest index
					bestScore = s
					best = gi
				}
			}
			if best == -1 {
				groups = append(groups, &group{idxs: []int{i}, sum: cloneVec(vi)})
				continue
			}
			g := groups[best]
			g.idxs = append(g.idxs, i)
			addVec(g.sum, vi)
		}

		for _, g := range groups {
			mems := make([]ClusterMember, 0, len(g.idxs))
			projects := map[string]struct{}{}
			convs := map[string]struct{}{}
			for _, i := range g.idxs {
				mems = append(mems, members[i])
				if members[i].Project != "" {
					projects[members[i].Project] = struct{}{}
				}
				if members[i].ConversationID != "" {
					convs[members[i].ConversationID] = struct{}{}
				}
			}
			out = append(out, Cluster{
				Kind:              k.kind,
				SubKey:            k.sub,
				Canonical:         medoidText(g.idxs, g.sum, members, vecOf),
				Members:           mems,
				EvidenceCount:     len(mems),
				ProjectCount:      len(projects),
				ConversationCount: len(convs),
			})
		}
	}
	return out
}

// cloneVec returns an independent copy so a cluster's running sum never
// aliases a caller-owned vector.
func cloneVec(v []float32) []float32 {
	out := make([]float32, len(v))
	copy(out, v)
	return out
}

// addVec adds v into dst in place. Vectors from one embedding model share a
// dimension. A vector of a different length can never reach addVec for an
// existing group: Cosine returns 0 on a length mismatch, so a mismatched
// vector falls below threshold, seeds its own group, and only ever has
// same-length vectors added to it.
func addVec(dst, v []float32) {
	for i := range dst {
		dst[i] += v[i]
	}
}

// medoidText returns the Text of the member nearest the cluster centroid
// (the sum vector). idxs is already in ObservationHash order, so ties resolve
// deterministically to the lowest hash.
func medoidText(idxs []int, sum []float32, members []ClusterMember, vecOf func(i int) []float32) string {
	best, bestScore := idxs[0], float32(-2) // -2 is below any valid cosine (>= -1)
	for _, i := range idxs {
		if s := embedding.Cosine(vecOf(i), sum); s > bestScore {
			bestScore = s
			best = i
		}
	}
	return members[best].Text
}
