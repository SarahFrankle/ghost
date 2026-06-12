package cluster

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/SarahFrankle/ghost/internal/extract"
	"github.com/SarahFrankle/ghost/internal/fingerprint"
)

// ObservationsFingerprints reads every *.json file under obsDir, decodes it as
// an ObservationsFile, and returns one content fingerprint per file, sorted
// lexically. The fingerprint hashes only what flows downstream into clustering
// and synthesis — the file's Source, Project, and the content of each
// observation (kind, text, evidence, context, confidence) — and deliberately
// ignores the stored extract Fingerprint and ExtractedAt timestamp. This
// decouples the cluster cache key from the extract prompt and model: a prompt
// or model bump that re-extracts to byte-identical observations no longer
// forces a full cluster + synthesize rebuild.
//
// Files with zero observations are skipped entirely: a transcript that yields
// no observations contributes nothing to clustering or synthesis, so it must
// not perturb the cache key. Without this, every newly-processed empty
// transcript would append an entry here, change the clusters fingerprint, and
// force a wasteful re-cluster + re-synthesize that produces identical output.
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
		if len(f.Observations) == 0 {
			continue
		}
		out = append(out, observationsContentFingerprint(f))
	}
	sort.Strings(out)
	return out, nil
}

// observationsContentFingerprint hashes the downstream-relevant content of one
// observations file. Per-observation contributions are sorted so model output
// reordering of otherwise-identical observations is not treated as a change.
func observationsContentFingerprint(f extract.ObservationsFile) string {
	obs := make([]string, 0, len(f.Observations))
	for _, o := range f.Observations {
		obs = append(obs, strings.Join(
			[]string{o.Kind, o.Confidence, o.Context, o.Text, o.Evidence}, "\x1f"))
	}
	sort.Strings(obs)
	parts := make([]string, 0, len(obs)+3)
	parts = append(parts, "obs-content/v1", "source="+f.Source, "project="+f.Project)
	parts = append(parts, obs...)
	return fingerprint.Compute(parts...)
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
