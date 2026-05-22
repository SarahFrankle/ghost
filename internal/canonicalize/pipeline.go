package canonicalize

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/SarahFrankle/ghost/internal/anthropic"
	"github.com/SarahFrankle/ghost/internal/embedding"
	"github.com/SarahFrankle/ghost/prompts"
)

// Pipeline runs the cheap-model slug canonicalizer over a flat list of
// observed topic slugs and returns an alias map. It does NOT touch
// disk; the caller is responsible for persisting the result so the
// existing alias map can be merged in and reviewed.
type Pipeline struct {
	Client anthropic.Client
	Model  string
	Log    func(format string, args ...any)
	// Embedder, EmbeddingModel, and SimilarityThreshold enable
	// semantic-similarity candidate proposals. When Embedder is nil
	// the pipeline falls back to string heuristics only.
	Embedder            embedding.Embedder
	EmbeddingModel      string
	SimilarityThreshold float32
	// MaxSamplesPerSlug caps how many observation texts get joined
	// into each slug's embedding fingerprint. 0 means use a sensible
	// default (5).
	MaxSamplesPerSlug int
}

type judgeResponse struct {
	Same      bool   `json:"same"`
	Canonical string `json:"canonical"`
}

// Run proposes candidate groups, asks the judge about each, and
// returns a new alias map. slugSamples maps each observed slug to a
// sample of observation texts under it (used for embedding
// fingerprints when an embedder is configured); pass nil if you only
// want string heuristics.
//
// Existing aliases (passed in) are honored: slugs already mapped are
// pre-collapsed before grouping so we don't re-judge resolved
// variants. The returned map is the merge of existing and
// newly-confirmed entries.
func (p *Pipeline) Run(ctx context.Context, slugSamples map[string][]string, existing Aliases) (Aliases, error) {
	if existing == nil {
		existing = Aliases{}
	}

	// Collapse slugs through existing aliases so we judge only the
	// canonical form of slugs that already have a resolution. Samples
	// are merged onto the canonical slug.
	canonicalSamples := map[string][]string{}
	for s, samples := range slugSamples {
		c := existing.Resolve(s)
		canonicalSamples[c] = append(canonicalSamples[c], samples...)
	}
	slugs := make([]string, 0, len(canonicalSamples))
	for s := range canonicalSamples {
		slugs = append(slugs, s)
	}
	sort.Strings(slugs)

	stringGroups := Propose(slugs)
	p.logf("canonicalize: %d distinct slug(s); %d string-heuristic group(s)", len(slugs), len(stringGroups))

	groups := stringGroups
	if p.Embedder != nil && p.SimilarityThreshold > 0 {
		maxSamples := p.MaxSamplesPerSlug
		if maxSamples <= 0 {
			maxSamples = 5
		}
		embGroups, err := EmbedPropose(ctx, p.Embedder, p.EmbeddingModel, canonicalSamples, p.SimilarityThreshold, maxSamples)
		if err != nil {
			p.logf("canonicalize: embed propose failed: %v (falling back to string-only)", err)
		} else {
			p.logf("canonicalize: %d embedding-similarity group(s)", len(embGroups))
			groups = unionGroups(groups, embGroups)
			p.logf("canonicalize: %d candidate group(s) after union", len(groups))
		}
	}

	merged := maps.Clone(existing)

	for i, g := range groups {
		p.logf("canonicalize: [%d/%d] judging %v", i+1, len(groups), g)
		resp, err := p.judge(ctx, g)
		if err != nil {
			p.logf("canonicalize: judge failed for %v: %v", g, err)
			continue
		}
		if !resp.Same || resp.Canonical == "" {
			p.logf("canonicalize: %v -> not same; keeping separate", g)
			continue
		}
		if !slices.Contains(g, resp.Canonical) {
			p.logf("canonicalize: judge chose %q which is not in candidate group %v; ignoring", resp.Canonical, g)
			continue
		}
		for _, s := range g {
			if s == resp.Canonical {
				continue
			}
			merged[s] = resp.Canonical
		}
		p.logf("canonicalize: %v -> %s", g, resp.Canonical)
	}

	return collapse(merged), nil
}

// unionGroups merges overlapping groups across the two proposers via
// union-find on the distinct slug set. A slug appearing in groups
// from both proposers pulls them into the same final group.
func unionGroups(a, b [][]string) [][]string {
	index := map[string]int{}
	all := []string{}
	add := func(s string) int {
		if i, ok := index[s]; ok {
			return i
		}
		index[s] = len(all)
		all = append(all, s)
		return len(all) - 1
	}
	pairs := func(gs [][]string) {
		for _, g := range gs {
			for _, s := range g {
				add(s)
			}
		}
	}
	pairs(a)
	pairs(b)

	uf := newUF(len(all))
	apply := func(gs [][]string) {
		for _, g := range gs {
			if len(g) < 2 {
				continue
			}
			first := index[g[0]]
			for _, s := range g[1:] {
				uf.union(first, index[s])
			}
		}
	}
	apply(a)
	apply(b)

	out := map[int][]string{}
	for s, i := range index {
		root := uf.find(i)
		out[root] = append(out[root], s)
	}
	var result [][]string
	for _, g := range out {
		if len(g) < 2 {
			continue
		}
		sort.Strings(g)
		result = append(result, g)
	}
	sort.Slice(result, func(i, j int) bool { return result[i][0] < result[j][0] })
	return result
}

func (p *Pipeline) judge(ctx context.Context, group []string) (judgeResponse, error) {
	var b strings.Builder
	b.WriteString("CANDIDATE SLUGS:\n")
	for _, s := range group {
		fmt.Fprintf(&b, "- %s\n", s)
	}
	raw, err := p.Client.Complete(ctx, p.Model, prompts.CanonicalizeSlugSystem(), b.String())
	if err != nil {
		return judgeResponse{}, err
	}
	span, ok := firstBalancedObject(raw)
	if !ok {
		return judgeResponse{}, fmt.Errorf("no JSON object in judge response")
	}
	var out judgeResponse
	if err := json.Unmarshal([]byte(span), &out); err != nil {
		return judgeResponse{}, err
	}
	return out, nil
}

func (p *Pipeline) logf(format string, args ...any) {
	if p.Log != nil {
		p.Log(format, args...)
	}
}

// firstBalancedObject mirrors the extractor's permissive JSON span
// finder so the judge can wrap its output in stray prose.
func firstBalancedObject(raw string) (string, bool) {
	start := strings.Index(raw, "{")
	if start < 0 {
		return "", false
	}
	depth, inStr, esc := 0, false, false
	for i := start; i < len(raw); i++ {
		c := raw[i]
		if inStr {
			if esc {
				esc = false
				continue
			}
			switch c {
			case '\\':
				esc = true
			case '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return raw[start : i+1], true
			}
		}
	}
	return "", false
}
