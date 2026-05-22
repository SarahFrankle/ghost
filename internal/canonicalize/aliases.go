// Package canonicalize merges near-synonym topic slugs into a single
// canonical form. It produces and reads a persistent alias map that
// downstream stages (cluster, extract) consult so the same concept
// stops appearing under multiple slugs.
//
// Observations on disk remain immutable: aliases are applied at read
// time only, mapping a stored Topic field through the map before it
// reaches bucketing.
package canonicalize

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"sort"

	"github.com/SarahFrankle/ghost/internal/atomicfs"
)

const aliasesSchemaVersion = 1

// Aliases maps a variant slug to its canonical slug. A slug missing
// from the map is its own canonical form. The map is transitive at
// load time: A->B and B->C collapse to A->C.
type Aliases map[string]string

type aliasesFile struct {
	SchemaVersion int       `json:"schema_version"`
	Model         string    `json:"model"`
	Entries       Aliases   `json:"entries"`
}

// Load reads aliases from path. A missing file returns an empty map.
// A schema version newer than this binary supports is an error so we
// fail loudly instead of silently dropping aliases. The Model field is
// purely informational — aliases don't expire when the model changes
// because the user has presumably reviewed them on disk.
func Load(path string) (Aliases, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Aliases{}, nil
		}
		return nil, err
	}
	var raw aliasesFile
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	if raw.SchemaVersion > aliasesSchemaVersion {
		return nil, errors.New("slug_aliases.json schema_version newer than binary supports")
	}
	if raw.Entries == nil {
		return Aliases{}, nil
	}
	return collapse(raw.Entries), nil
}

// Save writes aliases to path atomically. model is recorded for
// audit; it does not gate loading.
func Save(path, model string, a Aliases) error {
	body, err := json.MarshalIndent(aliasesFile{
		SchemaVersion: aliasesSchemaVersion,
		Model:         model,
		Entries:       a,
	}, "", "  ")
	if err != nil {
		return err
	}
	return atomicfs.WriteFile(path, body, 0o644)
}

// Resolve returns the canonical form of slug under a. If slug isn't a
// known variant, it's returned unchanged. Resolve assumes a is already
// collapsed (Load guarantees this).
func (a Aliases) Resolve(slug string) string {
	if v, ok := a[slug]; ok {
		return v
	}
	return slug
}

// CanonicalSlugs returns the set of slugs that are canonical (i.e.
// appear as a value in a, or are not referenced as a key). It's the
// "live" slug list — useful for seeding KNOWN TOPICS at extract time
// without including deprecated variants.
func CanonicalSlugs(a Aliases, observed []string) []string {
	seen := map[string]struct{}{}
	for _, s := range observed {
		seen[a.Resolve(s)] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// collapse follows chains so every value is a terminal canonical slug.
// Cycles (which the judge should never produce, but we defend against)
// are broken by leaving the second occurrence pointing at itself.
func collapse(a Aliases) Aliases {
	out := make(Aliases, len(a))
	for k := range a {
		visited := map[string]bool{k: true}
		cur := k
		for {
			next, ok := a[cur]
			if !ok || next == cur {
				break
			}
			if visited[next] {
				// cycle: leave cur as the terminal value to break the loop
				break
			}
			visited[next] = true
			cur = next
		}
		if cur != k {
			out[k] = cur
		}
	}
	return out
}
