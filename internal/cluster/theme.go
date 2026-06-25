package cluster

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"

	"github.com/SarahFrankle/ghost/internal/atomicfs"
	"github.com/SarahFrankle/ghost/internal/fingerprint"
)

// ThemesFile is the persisted label→theme mapping plus the fingerprint of its
// inputs (unique label set + theme model + theme prompt).
type ThemesFile struct {
	Fingerprint string            `json:"fingerprint"`
	Mapping     map[string]string `json:"mapping"`
}

// ThemesFingerprint composes the cache key for themes.json: the sorted unique
// label set, the theme model, and both theme prompt hashes (identify + map).
// Any new label, model swap, or edit to either prompt busts it.
func ThemesFingerprint(uniqueLabels []string, themeModel, identifyPromptHash, mapPromptHash, seedHash string) string {
	sorted := append([]string(nil), uniqueLabels...)
	sort.Strings(sorted)
	parts := make([]string, 0, len(sorted)+5)
	parts = append(parts, "themes/v3", themeModel, identifyPromptHash, mapPromptHash, seedHash)
	parts = append(parts, sorted...)
	return fingerprint.Compute(parts...)
}

func LoadThemes(path string) (ThemesFile, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ThemesFile{}, nil
		}
		return ThemesFile{}, err
	}
	var f ThemesFile
	if err := json.Unmarshal(b, &f); err != nil {
		return ThemesFile{}, err
	}
	return f, nil
}

func SaveThemes(path string, f ThemesFile) error {
	body, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return atomicfs.WriteFile(path, body, 0o644)
}

// uniqueSorted returns the sorted, de-duplicated set of labels.
func uniqueSorted(labels []string) []string {
	seen := map[string]struct{}{}
	for _, l := range labels {
		seen[l] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for l := range seen {
		out = append(out, l)
	}
	sort.Strings(out)
	return out
}

// validateMapping checks that every unique input label has a non-empty theme.
// This is the theme half of the ID-validation contract: a label the theme
// step forgot would otherwise drop its observations silently.
func validateMapping(uniqueLabels []string, mapping map[string]string) error {
	var missing []string
	for _, l := range uniqueLabels {
		if t, ok := mapping[l]; !ok || t == "" {
			missing = append(missing, l)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("theme: %d label(s) missing from mapping: %v", len(missing), missing)
	}
	return nil
}
