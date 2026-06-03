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

// ClustersFingerprint composes the cache key for clusters.json. Clustering
// is now embedding-only (no LLM stage 2b), so the inputs are the embedding
// model and the two per-kind cosine thresholds. The "cluster/v2" namespace
// ensures pre-chunk-3 fingerprints definitionally miss on the first run.
func ClustersFingerprint(obsFingerprints []string, embeddingModel string, identityRuleThreshold, topicThreshold float32) string {
	parts := make([]string, 0, len(obsFingerprints)+4)
	parts = append(parts, "cluster/v2", embeddingModel,
		fmt.Sprintf("identity_rule=%g", identityRuleThreshold),
		fmt.Sprintf("topic=%g", topicThreshold))
	parts = append(parts, obsFingerprints...)
	return fingerprint.Compute(parts...)
}
