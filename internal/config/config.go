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
	RuleMinEvidenceCount   int     `toml:"rule_min_evidence_count"`
	RuleMinProjectCount    int     `toml:"rule_min_project_count"`
	VoiceMinEvidenceCount  int     `toml:"voice_min_evidence_count"`
	ClusterCosineThreshold float64 `toml:"cluster_cosine_threshold"`
	// CanonicalizeSimilarityThreshold controls how aggressive the
	// slug-canonicalizer's embedding proposer is. Lower → more
	// candidate groups proposed → more LLM judge calls. The judge
	// filters false positives so a generous default is fine. Set to
	// 0 to disable embedding-based proposals entirely.
	CanonicalizeSimilarityThreshold float64 `toml:"canonicalize_similarity_threshold"`
}

type Paths struct {
	TranscriptsGlob string `toml:"transcripts_glob"`
	OutputDir       string `toml:"output_dir"`
}

type Batching struct {
	DefaultLimit   int `toml:"default_limit"`
	ExtractWorkers int `toml:"extract_workers"`
}

type Index struct {
	MaxTopicEntries int `toml:"max_topic_entries"`
}

type Voice struct {
	Enabled bool `toml:"enabled"`
}

type Config struct {
	Models     Models     `toml:"models"`
	Thresholds Thresholds `toml:"thresholds"`
	Paths      Paths      `toml:"paths"`
	Batching   Batching   `toml:"batching"`
	Index      Index      `toml:"index"`
	Voice      Voice      `toml:"voice"`
}

func Defaults() Config {
	return Config{
		Models: Models{
			Cheap:     "claude-haiku-4-5-20251001",
			Smart:     "claude-opus-4-7",
			Embedding: "voyage-3-lite",
		},
		Thresholds: Thresholds{
			RuleMinEvidenceCount:   2,
			RuleMinProjectCount:    2,
			VoiceMinEvidenceCount:  2,
			ClusterCosineThreshold:          0.85,
			CanonicalizeSimilarityThreshold: 0.75,
		},
		Paths: Paths{
			TranscriptsGlob: "~/.claude/projects/**/*.jsonl",
			OutputDir:       "~/.ghost",
		},
		Batching: Batching{
			DefaultLimit:   0,
			ExtractWorkers: 5,
		},
		Index: Index{MaxTopicEntries: 20},
		Voice: Voice{Enabled: false},
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
