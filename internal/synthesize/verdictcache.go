package synthesize

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"sort"
	"strings"

	"github.com/SarahFrankle/ghost/internal/atomicfs"
	"github.com/SarahFrankle/ghost/internal/cluster"
	"github.com/SarahFrankle/ghost/internal/fingerprint"
)

// VerdictsFile is the persisted generality routing decision: theme label ->
// general?, plus the fingerprint of the inputs that produced it.
type VerdictsFile struct {
	Fingerprint string          `json:"fingerprint"`
	Verdicts    map[string]bool `json:"verdicts"` // theme label -> general?
}

// VerdictFingerprint keys the verdict cache on exactly what the verdict
// depends on: the generality prompt, the routing model, and a per-theme
// membership signal (sorted member ObservationHashes). A prompt edit, a model
// swap, or new conversations joining a theme all bust it; a stable theme with
// stable membership reuses the cached verdict. This is deliberately NOT
// ThemesFingerprint, which is blind to membership shifts under a stable label.
func VerdictFingerprint(clusters []cluster.Cluster, generalityPromptHash, model string) string {
	parts := []string{"verdicts/v1", generalityPromptHash, model}
	rows := make([]string, 0, len(clusters))
	for _, c := range clusters {
		hashes := make([]string, 0, len(c.Members))
		for _, m := range c.Members {
			hashes = append(hashes, m.ObservationHash)
		}
		sort.Strings(hashes)
		parts := make([]string, 0, len(hashes)+1)
		parts = append(parts, c.Canonical)
		parts = append(parts, hashes...)
		rows = append(rows, strings.Join(parts, "|"))
	}
	sort.Strings(rows)
	parts = append(parts, rows...)
	return fingerprint.Compute(parts...)
}

func LoadVerdicts(path string) (VerdictsFile, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return VerdictsFile{}, nil
		}
		return VerdictsFile{}, err
	}
	var f VerdictsFile
	if err := json.Unmarshal(b, &f); err != nil {
		return VerdictsFile{}, err
	}
	return f, nil
}

func SaveVerdicts(path string, f VerdictsFile) error {
	body, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return atomicfs.WriteFile(path, body, 0o644)
}
