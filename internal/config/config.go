package config

import (
	"errors"
	"io/fs"
	"os"

	"github.com/BurntSushi/toml"
)

type Models struct {
	Cheap     string `toml:"cheap"`
	Smart     string `toml:"smart"`
	Embedding string `toml:"embedding"`
}

type Thresholds struct {
	RuleMinEvidenceCount int `toml:"rule_min_evidence_count"`
	RuleMinProjectCount  int `toml:"rule_min_project_count"`
	// ClusterCosineIdentityRule is the cosine threshold for bucketing
	// identity and rule observations. Tight by default: these kinds want
	// near-duplicate merging only.
	ClusterCosineIdentityRule float64 `toml:"cluster_cosine_identity_rule"`
	// ClusterCosineTopic is the cosine threshold for bucketing topic
	// observations. Looser than identity/rule so semantically related
	// preferences ("docs should lead with examples" / "example-first
	// documentation") land in one topic cluster.
	ClusterCosineTopic float64 `toml:"cluster_cosine_topic"`
}

type Paths struct {
	TranscriptsGlob string `toml:"transcripts_glob"`
	OutputDir       string `toml:"output_dir"`
}

type Batching struct {
	ExtractWorkers int `toml:"extract_workers"`
}

type Index struct {
	MaxTopicEntries int `toml:"max_topic_entries"`
}

type Config struct {
	Models     Models     `toml:"models"`
	Thresholds Thresholds `toml:"thresholds"`
	Paths      Paths      `toml:"paths"`
	Batching   Batching   `toml:"batching"`
	Index      Index      `toml:"index"`
}

func Defaults() Config {
	return Config{
		Models: Models{
			Cheap:     "claude-haiku-4-5-20251001",
			Smart:     "claude-opus-4-7",
			Embedding: "voyage-3-lite",
		},
		Thresholds: Thresholds{
			RuleMinEvidenceCount:      2,
			RuleMinProjectCount:       2,
			ClusterCosineIdentityRule: 0.85,
			ClusterCosineTopic:        0.75,
		},
		Paths: Paths{
			TranscriptsGlob: "~/.claude/projects/**/*.jsonl",
			OutputDir:       "~/.ghost",
		},
		Batching: Batching{
			ExtractWorkers: 5,
		},
		Index: Index{MaxTopicEntries: 20},
	}
}

// Load returns Defaults() overlaid with whatever fields are set in the TOML file at path.
// A missing file is not an error — defaults are returned.
func Load(path string) (Config, error) {
	cfg := Defaults()
	_, err := toml.DecodeFile(path, &cfg)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	return cfg, nil
}
