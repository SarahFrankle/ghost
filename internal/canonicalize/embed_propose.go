package canonicalize

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/SarahFrankle/ghost/internal/embedding"
)

// EmbedPropose groups slugs by semantic similarity using the supplied
// embedder. For each slug it builds a representative text from the
// slug itself plus up to maxSamples observation texts seen under that
// slug; that fingerprint is what gets embedded. Pairs whose cosine
// similarity meets or exceeds threshold are unioned into groups.
//
// The LLM judge filters false positives downstream so a generous
// threshold (e.g. 0.75 for nomic-embed-text) is the right default.
// Slugs with no associated sample texts get the slug alone.
func EmbedPropose(
	ctx context.Context,
	emb embedding.Embedder,
	model string,
	slugSamples map[string][]string,
	threshold float32,
	maxSamples int,
) ([][]string, error) {
	if emb == nil || len(slugSamples) < 2 {
		return nil, nil
	}

	slugs := make([]string, 0, len(slugSamples))
	for s := range slugSamples {
		slugs = append(slugs, s)
	}
	sort.Strings(slugs)

	texts := make([]string, len(slugs))
	for i, s := range slugs {
		texts[i] = buildFingerprint(s, slugSamples[s], maxSamples)
	}

	vecs, err := emb.Embed(ctx, model, texts)
	if err != nil {
		return nil, fmt.Errorf("embed slug fingerprints: %w", err)
	}
	if len(vecs) != len(slugs) {
		return nil, fmt.Errorf("embedder returned %d vectors for %d slugs", len(vecs), len(slugs))
	}

	uf := newUF(len(slugs))
	for i := range slugs {
		for j := i + 1; j < len(slugs); j++ {
			if embedding.Cosine(vecs[i], vecs[j]) >= threshold {
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
	return out, nil
}

// buildFingerprint produces the text that represents a slug for the
// embedder. Format: `topic: <slug>\n<sample1>\n<sample2>\n...` with at
// most maxSamples sample texts. Including the slug literal gives the
// embedder a hook for cases where samples are sparse or generic.
func buildFingerprint(slug string, samples []string, maxSamples int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "topic: %s\n", slug)
	n := len(samples)
	if maxSamples > 0 && n > maxSamples {
		n = maxSamples
	}
	for i := 0; i < n; i++ {
		b.WriteString(samples[i])
		b.WriteString("\n")
	}
	return b.String()
}
