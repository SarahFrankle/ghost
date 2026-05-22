package cluster

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/SarahFrankle/ghost/internal/extract"
	"github.com/SarahFrankle/ghost/internal/fingerprint"
)

// ObservationsFingerprints reads every *.json file under obsDir, decodes
// it as an ObservationsFile, and returns each file's Fingerprint sorted
// lexically. Observations files written before chunk 2 lack a fingerprint;
// those contribute an empty slot, which still forces a rebuild after they
// are re-extracted (because the slot becomes non-empty).
func ObservationsFingerprints(obsDir string) ([]string, error) {
	entries, err := os.ReadDir(obsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(obsDir, e.Name()))
		if err != nil {
			return nil, err
		}
		var f extract.ObservationsFile
		if err := json.Unmarshal(b, &f); err != nil {
			return nil, fmt.Errorf("decode %s: %w", e.Name(), err)
		}
		out = append(out, f.Fingerprint)
	}
	sort.Strings(out)
	return out, nil
}

// ClustersFingerprint composes the cache key for clusters.json. canonPromptHash
// and canonModel may be empty when the canonicalizer is disabled.
func ClustersFingerprint(obsFingerprints []string, embeddingModel, canonModel, canonPromptHash string, cosineThreshold float32) string {
	parts := make([]string, 0, len(obsFingerprints)+5)
	parts = append(parts, "cluster/v1", embeddingModel, canonModel, canonPromptHash, fmt.Sprintf("%g", cosineThreshold))
	parts = append(parts, obsFingerprints...)
	return fingerprint.Compute(parts...)
}
