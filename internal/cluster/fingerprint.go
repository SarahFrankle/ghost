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

// ClustersFingerprint composes the cache key for clusters.json. Identity/rule/
// voice still cluster by cosine (identityRuleThreshold); topics now go through
// label→theme→group, so the topic-path inputs join the key: label model + label
// prompt hash, theme model + the two theme-pass prompt hashes (identify + map),
// and minClusterSize. The "cluster/v4" namespace ensures pre-redesign clusters
// (cosine-for-topics) definitionally miss and are recomputed.
func ClustersFingerprint(obsFingerprints []string, embeddingModel string, identityRuleThreshold float32, labelModel, labelPromptHash, themeModel, themeIdentifyPromptHash, themeMapPromptHash string, minClusterSize int) string {
	parts := make([]string, 0, len(obsFingerprints)+9)
	parts = append(parts, "cluster/v4", embeddingModel,
		fmt.Sprintf("identity_rule=%g", identityRuleThreshold),
		"label_model="+labelModel, "label_prompt="+labelPromptHash,
		"theme_model="+themeModel,
		"theme_identify_prompt="+themeIdentifyPromptHash,
		"theme_map_prompt="+themeMapPromptHash,
		fmt.Sprintf("min_cluster=%d", minClusterSize))
	parts = append(parts, obsFingerprints...)
	return fingerprint.Compute(parts...)
}
