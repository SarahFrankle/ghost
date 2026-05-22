package canonicalize

import (
	"sort"
	"strings"
)

// Propose groups slugs that the cheap string heuristics flag as
// possibly the same topic. Each returned group has 2+ slugs. Groups
// are not exhaustive — the LLM judge confirms. False positives are
// fine here; false negatives are the failure mode to avoid, so the
// heuristics are deliberately generous.
//
// Heuristics, in order of how often they fire in practice:
//  1. Shared lemma after stripping common English suffixes
//     (`-ing`, `-s`, `-er`, `-ation`) from the final path segment.
//     Catches `runbook-refactor` vs `runbook-refactoring`, `test` vs
//     `testing`, `alert` vs `alerting`.
//  2. One slug's path-tail equals another slug. Catches `api-design`
//     vs `engineering/api-design`, `git` vs `git-workflow` (false
//     positive — judge will reject).
//  3. Levenshtein distance ≤ 2 on the full slug. Catches typos and
//     minor variants the suffix rules miss.
func Propose(slugs []string) [][]string {
	if len(slugs) < 2 {
		return nil
	}

	// uf maintains union-find over slug indices; any heuristic that
	// flags two slugs unions them so transitive overlaps end up in
	// one group instead of many overlapping pairs.
	uf := newUF(len(slugs))

	lemmas := make([]string, len(slugs))
	tails := make([]string, len(slugs))
	for i, s := range slugs {
		lemmas[i] = lemma(s)
		tails[i] = pathTail(s)
	}

	// Heuristic 1: same lemma.
	byLemma := map[string][]int{}
	for i, l := range lemmas {
		byLemma[l] = append(byLemma[l], i)
	}
	for _, idxs := range byLemma {
		for k := 1; k < len(idxs); k++ {
			uf.union(idxs[0], idxs[k])
		}
	}

	// Heuristic 2: shared first kebab-token. `git`, `git-workflow`, and
	// `git-config` group together. False positives (`git` vs
	// `git-workflow` may be distinct) are filtered by the LLM judge.
	// The first token must be ≥ 3 chars to avoid grouping every slug
	// starting with `a-` or similar.
	byFirstToken := map[string][]int{}
	for i, s := range slugs {
		t := firstKebabToken(s)
		if len(t) < 3 {
			continue
		}
		byFirstToken[t] = append(byFirstToken[t], i)
	}
	for _, idxs := range byFirstToken {
		for k := 1; k < len(idxs); k++ {
			uf.union(idxs[0], idxs[k])
		}
	}

	// Heuristic 3: one slug is another's path tail.
	byTail := map[string][]int{}
	for i, s := range slugs {
		byTail[s] = append(byTail[s], i) // full slug also indexes itself
	}
	for i, t := range tails {
		if t == slugs[i] {
			continue // no `/` in this slug; nothing to match against
		}
		for _, j := range byTail[t] {
			uf.union(i, j)
		}
	}

	// Heuristic 4: Levenshtein ≤ 2 on the full slug. O(N^2) but N is
	// the number of distinct topic slugs in a single user's corpus —
	// realistically dozens, not thousands.
	for i := range slugs {
		for j := i + 1; j < len(slugs); j++ {
			if uf.find(i) == uf.find(j) {
				continue
			}
			if levenshteinAtMost(slugs[i], slugs[j], 2) {
				uf.union(i, j)
			}
		}
	}

	groups := map[int][]string{}
	for i, s := range slugs {
		root := uf.find(i)
		groups[root] = append(groups[root], s)
	}
	var out [][]string
	for _, g := range groups {
		if len(g) < 2 {
			continue
		}
		sort.Strings(g)
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i][0] < out[j][0] })
	return out
}

// firstKebabToken returns the first hyphen-separated token of the
// first path segment. For `engineering/api-design` it returns
// `engineering`; for `runbook-refactor` it returns `runbook`.
func firstKebabToken(s string) string {
	if i := strings.Index(s, "/"); i >= 0 {
		s = s[:i]
	}
	if i := strings.Index(s, "-"); i >= 0 {
		return s[:i]
	}
	return s
}

// pathTail returns the substring after the last `/`. Slugs without `/`
// return themselves.
func pathTail(s string) string {
	if i := strings.LastIndex(s, "/"); i >= 0 {
		return s[i+1:]
	}
	return s
}

// lemma collapses common morphological variations on the final path
// segment so `runbook-refactoring` and `runbook-refactor` hash to the
// same key. It's intentionally crude — the LLM judge has the final say.
func lemma(s string) string {
	tail := pathTail(s)
	// Operate on the LAST kebab-token of the tail; `runbook-refactoring`
	// → strip from `refactoring`, not from `runbook`.
	parts := strings.Split(tail, "-")
	last := parts[len(parts)-1]
	for _, suf := range []string{"ation", "ing", "ers", "er", "es", "s"} {
		if len(last) > len(suf)+2 && strings.HasSuffix(last, suf) {
			last = strings.TrimSuffix(last, suf)
			break
		}
	}
	parts[len(parts)-1] = last
	return strings.Join(parts, "-")
}

// levenshteinAtMost reports whether edit distance between a and b is ≤
// maxDist. Early-exit when the running minimum exceeds maxDist.
func levenshteinAtMost(a, b string, maxDist int) bool {
	if abs(len(a)-len(b)) > maxDist {
		return false
	}
	if a == b {
		return true
	}
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr[0] = i
		rowMin := curr[0]
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min3(curr[j-1]+1, prev[j]+1, prev[j-1]+cost)
			rowMin = min(rowMin, curr[j])
		}
		if rowMin > maxDist {
			return false
		}
		prev, curr = curr, prev
	}
	return prev[len(b)] <= maxDist
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func min3(a, b, c int) int {
	m := min(a, b)
	if c < m {
		m = c
	}
	return m
}

// uf is a small union-find over [0, n).
type uf struct{ parent []int }

func newUF(n int) *uf {
	p := make([]int, n)
	for i := range p {
		p[i] = i
	}
	return &uf{parent: p}
}

func (u *uf) find(x int) int {
	for u.parent[x] != x {
		u.parent[x] = u.parent[u.parent[x]]
		x = u.parent[x]
	}
	return x
}

func (u *uf) union(a, b int) {
	ra, rb := u.find(a), u.find(b)
	if ra != rb {
		u.parent[ra] = rb
	}
}
