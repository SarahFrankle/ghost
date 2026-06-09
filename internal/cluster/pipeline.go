package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/SarahFrankle/ghost/internal/embedding"
	"github.com/SarahFrankle/ghost/internal/extract"
)

// Pipeline owns stage 2 end-to-end: load observation files, then split by
// kind. Identity/rule/voice observations embed (cache-aware) and bucket by
// cosine; topic observations bypass embedding entirely and route through
// Topics (label→theme→group). The merged cluster set is written to
// clusters.json.
type Pipeline struct {
	Embedder       embedding.Embedder
	EmbeddingModel string
	Cache          *embedding.Cache
	CacheSavePath  string
	ClustersPath   string
	// ThresholdFor returns the cosine threshold for a given kind. Only
	// identity/rule/voice reach it now (topics no longer cosine-cluster).
	ThresholdFor func(kind string) float32
	Workers      int
	Log          func(format string, args ...any)
	// Topics, if non-nil, produces topic clusters via label→theme→group.
	// Topic observations bypass embedding/cosine entirely. When nil, topic
	// observations are dropped (no topic clusters) — callers set it in
	// production.
	Topics *TopicGrouper
	// Fingerprint, if non-empty, is written to the resulting clusters.json
	// so subsequent runs can detect input/threshold/model changes without
	// rebuilding.
	Fingerprint string
}

func (p *Pipeline) logf(format string, args ...any) {
	if p.Log != nil {
		p.Log(format, args...)
	}
}

func (p *Pipeline) Run(ctx context.Context, observationsDir string) error {
	members, err := loadAllObservations(observationsDir)
	if err != nil {
		return fmt.Errorf("load observations: %w", err)
	}
	if len(members) == 0 {
		return SaveClusters(p.ClustersPath, ClustersFile{
			SchemaVersion:    SchemaVersion,
			EmbeddingModelID: p.EmbeddingModel,
			BuiltAt:          time.Now().UTC(),
			Fingerprint:      p.Fingerprint,
		})
	}

	var topicMembers, rest []ClusterMember
	for _, m := range members {
		if m.Kind == "topic" {
			topicMembers = append(topicMembers, m)
		} else {
			rest = append(rest, m)
		}
	}

	var clusters []Cluster
	if len(rest) > 0 {
		vectors, err := p.embedAll(ctx, rest)
		if err != nil {
			return fmt.Errorf("embed: %w", err)
		}
		if err := p.Cache.Save(p.CacheSavePath); err != nil {
			p.logf("embedding cache save: %v", err)
		}
		clusters = append(clusters, Bucket(rest, func(i int) []float32 { return vectors[i] }, p.ThresholdFor)...)
	}

	if p.Topics != nil && len(topicMembers) > 0 {
		topicClusters, err := p.Topics.Run(ctx, topicMembers)
		if err != nil {
			return fmt.Errorf("topics: %w", err)
		}
		clusters = append(clusters, topicClusters...)
	}

	return SaveClusters(p.ClustersPath, ClustersFile{
		SchemaVersion:    SchemaVersion,
		EmbeddingModelID: p.EmbeddingModel,
		BuiltAt:          time.Now().UTC(),
		Fingerprint:      p.Fingerprint,
		Clusters:         clusters,
	})
}

func (p *Pipeline) embedAll(ctx context.Context, members []ClusterMember) ([][]float32, error) {
	out := make([][]float32, len(members))

	missingIdx := []int{}
	missingTexts := []string{}
	for i, m := range members {
		if v, ok := p.Cache.Get(m.ObservationHash); ok {
			out[i] = v
			continue
		}
		missingIdx = append(missingIdx, i)
		missingTexts = append(missingTexts, m.Text)
	}
	p.logf("cluster: embedding %d new observation(s), %d cached", len(missingIdx), len(members)-len(missingIdx))
	if len(missingIdx) == 0 {
		return out, nil
	}

	vecs, err := p.Embedder.Embed(ctx, p.EmbeddingModel, missingTexts)
	if err != nil {
		return nil, err
	}
	if len(vecs) != len(missingIdx) {
		return nil, fmt.Errorf("embedder returned %d vectors for %d inputs", len(vecs), len(missingIdx))
	}
	for j, idx := range missingIdx {
		out[idx] = vecs[j]
		p.Cache.Put(members[idx].ObservationHash, vecs[j])
	}
	return out, nil
}

// LoadObservations walks observationsDir for *.json observation files and
// returns the flattened ClusterMember slice. Exported so commands (e.g.
// topics-preview) can load members without rebuilding clusters.json.
func LoadObservations(observationsDir string) ([]ClusterMember, error) {
	return loadAllObservations(observationsDir)
}

// loadAllObservations walks observationsDir for *.json files, decodes
// each as an extract.ObservationsFile, and flattens into a slice of
// ClusterMember with stable observation hashes.
func loadAllObservations(observationsDir string) ([]ClusterMember, error) {
	var out []ClusterMember
	entries, err := os.ReadDir(observationsDir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		path := filepath.Join(observationsDir, e.Name())
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var f extract.ObservationsFile
		if err := json.Unmarshal(b, &f); err != nil {
			return nil, fmt.Errorf("decode %s: %w", e.Name(), err)
		}
		for _, o := range f.Observations {
			subKey := ""
			if o.Kind == "voice" {
				subKey = o.Context
			}
			out = append(out, ClusterMember{
				ObservationHash: embedding.ObservationHash(o.Kind, subKey, o.Text),
				Source:          f.Source,
				Project:         f.Project,
				Kind:            o.Kind,
				Text:            o.Text,
				Evidence:        o.Evidence,
				Context:         o.Context,
				Confidence:      o.Confidence,
			})
		}
	}
	return out, nil
}
