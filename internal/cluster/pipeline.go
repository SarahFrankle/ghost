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

// Pipeline owns stage 2 end-to-end: load observation files, embed each
// observation (cache-aware), bucket, optionally canonicalize, then
// write clusters.json. The Canonicalizer field is optional; if nil,
// 2b is skipped (clusters keep their seed canonical text).
type Pipeline struct {
	Embedder        embedding.Embedder
	EmbeddingModel  string
	Cache           *embedding.Cache
	CacheSavePath   string
	ClustersPath    string
	CosineThreshold float32
	Canonicalizer   *Canonicalizer
	Workers         int
	Log             func(format string, args ...any)
	// TopicAliases, if non-nil, rewrites each observation's Topic via
	// its Resolve method before bucketing. Observations on disk are
	// untouched. A nil or empty map is a no-op.
	TopicAliases interface {
		Resolve(string) string
	}
	// Fingerprint, if non-empty, is written to the resulting clusters.json
	// so subsequent runs can detect whether inputs / prompts / models have
	// changed without rebuilding. Callers compute the expected value with
	// the same inputs and compare against the on-disk file.
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
	if p.TopicAliases != nil {
		for i := range members {
			if members[i].Kind == "topic" {
				members[i].Topic = p.TopicAliases.Resolve(members[i].Topic)
			}
		}
	}
	if len(members) == 0 {
		return SaveClusters(p.ClustersPath, ClustersFile{
			SchemaVersion:    SchemaVersion,
			EmbeddingModelID: p.EmbeddingModel,
			BuiltAt:          time.Now().UTC(),
		})
	}

	vectors, err := p.embedAll(ctx, members)
	if err != nil {
		return fmt.Errorf("embed: %w", err)
	}
	if err := p.Cache.Save(p.CacheSavePath); err != nil {
		p.logf("embedding cache save: %v", err)
	}

	clusters := Bucket(members, func(i int) []float32 { return vectors[i] }, p.CosineThreshold)

	if p.Canonicalizer != nil {
		if err := p.Canonicalizer.Apply(ctx, clusters); err != nil {
			p.logf("canonicalize: %v", err)
		}
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
			switch o.Kind {
			case "voice":
				subKey = o.Context
			case "topic":
				subKey = o.Topic
			}
			out = append(out, ClusterMember{
				ObservationHash: embedding.ObservationHash(o.Kind, subKey, o.Text),
				Source:          f.Source,
				Project:         f.Project,
				Kind:            o.Kind,
				Text:            o.Text,
				Evidence:        o.Evidence,
				Context:         o.Context,
				Topic:           o.Topic,
				Confidence:      o.Confidence,
			})
		}
	}
	return out, nil
}
