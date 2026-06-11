package config

import (
	"errors"
	"io/fs"
	"os"

	"github.com/BurntSushi/toml"
)

type Models struct {
	Cheap string `toml:"cheap"`
	Smart string `toml:"smart"`
	// Topic is the model for topic synthesis — the highest-volume synth
	// stage (one call per topic cluster). It defaults to a mid-tier model
	// since topic bodies are summaries of already-extracted observations;
	// identity/rules/index keep using Smart. Empty falls back to Smart.
	Topic string `toml:"topic"`
	// Label is the model for the per-observation topic label step (cheap,
	// high-volume, cached). Empty falls back to Cheap.
	Label string `toml:"label"`
	// Theme is the model for the one-shot label-vocabulary consolidation.
	// Empty falls back to Smart.
	Theme     string `toml:"theme"`
	Embedding string `toml:"embedding"`
}

type Thresholds struct {
	// ClusterCosineIdentityRule is the cosine threshold for bucketing
	// identity and rule observations. Tight by default: these kinds want
	// near-duplicate merging only.
	ClusterCosineIdentityRule float64 `toml:"cluster_cosine_identity_rule"`
	// MinClusterSize is the minimum number of observations a themed label
	// must have to become a topic. Below this, observations are dropped as
	// noise (logged, not silently).
	MinClusterSize int `toml:"min_cluster_size"`
	// RecurrenceForConfidence is the distinct-conversation count at which a
	// SOFT (non-high-confidence) preference earns confidence and survives the
	// gate. Directly-asserted high-confidence themes survive at any count, so
	// this only governs the recurrence path. Replaces the retired project_count
	// gate; frequency feeds confidence here, it is not a standalone floor.
	RecurrenceForConfidence int `toml:"recurrence_for_confidence"`
}

type Paths struct {
	TranscriptsGlob string `toml:"transcripts_glob"`
	OutputDir       string `toml:"output_dir"`
}

type Batching struct {
	ExtractWorkers int `toml:"extract_workers"`
	// SynthWorkers bounds concurrent topic-synthesis subprocesses. Higher
	// than ExtractWorkers is fine since the stdin race that capped fan-out
	// is fixed at the client; this only guards system/API load. Values < 1
	// are treated as 1.
	SynthWorkers int `toml:"synth_workers"`
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
			Smart:     "claude-opus-4-8",
			Topic:     "claude-sonnet-4-6",
			Label:     "claude-haiku-4-5-20251001",
			Theme:     "claude-sonnet-4-6",
			Embedding: "voyage-3-lite",
		},
		Thresholds: Thresholds{
			ClusterCosineIdentityRule: 0.85,
			MinClusterSize:            3,
			RecurrenceForConfidence:   3,
		},
		Paths: Paths{
			TranscriptsGlob: "~/.claude/projects/**/*.jsonl",
			OutputDir:       "~/.ghost",
		},
		Batching: Batching{
			ExtractWorkers: 5,
			SynthWorkers:   8,
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
