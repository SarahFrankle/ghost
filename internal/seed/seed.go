// Package seed reads the user-curated ~/.ghost/seed-topics.yml file.
// The seed expresses topic distinctions the user wants kept separate and named exactly
// as given; its leaf names anchor Pass-1 discovery and the Pass-2 candidate set
// so promoted topics get verbatim-stable slugs whenever they have evidence.
package seed

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"

	"gopkg.in/yaml.v3"

	"github.com/SarahFrankle/ghost/internal/fingerprint"
)

// Seed is the parsed seed file.
// Both sections are optional.
// Topics are leaf topics with no pinned parent; each Categories key is a pinned parent whose
// listed children are leaf topics.
type Seed struct {
	Topics     []string            `yaml:"topics"`
	Categories map[string][]string `yaml:"categories"`
}

// Topic is one flattened leaf topic.
// Parent is empty for Topics entries and the category key for Categories children.
type Topic struct {
	Name   string
	Parent string
}

// Load reads and parses the seed file at path.
// A missing file is not an error (absent seed = no anchoring): it returns an empty Seed.
// Unparseable YAML returns an error.
// A valid file with empty or duplicate leaf names drops those entries and reports them in warnings.
func Load(path string) (Seed, []string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Seed{}, nil, nil
		}
		return Seed{}, nil, err
	}
	var raw Seed
	if err := yaml.Unmarshal(b, &raw); err != nil {
		return Seed{}, nil, fmt.Errorf("seed: parse %s: %w", path, err)
	}
	return clean(raw)
}

// clean drops empty and duplicate leaf names across both sections, preserving
// the first occurrence, and reports each drop as a warning.
func clean(raw Seed) (Seed, []string, error) {
	out := Seed{}
	seen := map[string]struct{}{}
	var warns []string
	keep := func(name, where string) (string, bool) {
		if name == "" {
			warns = append(warns, fmt.Sprintf("seed: empty topic name in %s, skipped", where))
			return "", false
		}
		if _, dup := seen[name]; dup {
			warns = append(warns, fmt.Sprintf("seed: duplicate topic %q in %s, skipped", name, where))
			return "", false
		}
		seen[name] = struct{}{}
		return name, true
	}
	for _, t := range raw.Topics {
		if n, ok := keep(t, "topics"); ok {
			out.Topics = append(out.Topics, n)
		}
	}
	parents := make([]string, 0, len(raw.Categories))
	for p := range raw.Categories {
		parents = append(parents, p)
	}
	sort.Strings(parents)
	for _, p := range parents {
		for _, t := range raw.Categories[p] {
			if n, ok := keep(t, "categories."+p); ok {
				if out.Categories == nil {
					out.Categories = map[string][]string{}
				}
				out.Categories[p] = append(out.Categories[p], n)
			}
		}
	}
	return out, warns, nil
}

// Names returns the sorted, de-duplicated set of leaf topic names from both sections.
func (s Seed) Names() []string {
	out := make([]string, 0, len(s.Topics))
	out = append(out, s.Topics...)
	for _, children := range s.Categories {
		out = append(out, children...)
	}
	sort.Strings(out)
	return out
}

// Hash returns a content fingerprint of the seed's leaf names.
// It is mixed into the cluster fingerprints so editing the seed re-runs the theme step.
func (s Seed) Hash() string {
	return fingerprint.Compute(append([]string{"seed/v1"}, s.Names()...)...)
}

// Flatten returns every leaf as a Topic, sorted by name.
// Parent is empty for Topics entries and the category key for Categories children.
func (s Seed) Flatten() []Topic {
	out := make([]Topic, 0, len(s.Topics))
	for _, t := range s.Topics {
		out = append(out, Topic{Name: t})
	}
	for p, children := range s.Categories {
		for _, t := range children {
			out = append(out, Topic{Name: t, Parent: p})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
