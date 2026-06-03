package cluster

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/SarahFrankle/ghost/internal/embedding"
)

type fakeEmbedder struct{}

func (fakeEmbedder) Embed(ctx context.Context, model string, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v := make([]float32, 8)
		if len(t) > 0 {
			v[int(t[0])%8] = 1
		}
		out[i] = v
	}
	return out, nil
}

func loadCacheForTest(t *testing.T, stateDir, model string) (*embedding.Cache, string) {
	t.Helper()
	path := filepath.Join(stateDir, "embeddings.json")
	c, err := embedding.LoadCache(path, model)
	if err != nil {
		t.Fatal(err)
	}
	return c, path
}

func TestPipelineProducesClustersJSON(t *testing.T) {
	stateDir := t.TempDir()
	obsDir := filepath.Join(stateDir, "observations")
	if err := os.MkdirAll(obsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{
		"source": "/p/proj-a.jsonl", "project": "proj-a", "content_hash": "sha256:a",
		"extracted_at": "2026-05-21T00:00:00Z",
		"observations": [
			{"kind":"rule","text":"alpha rule","evidence":"turn 1"},
			{"kind":"identity","text":"works at Miro","evidence":"turn 2"}
		]
	}`
	if err := os.WriteFile(filepath.Join(obsDir, "a.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	body2 := `{
		"source": "/p/proj-b.jsonl", "project": "proj-b", "content_hash": "sha256:b",
		"extracted_at": "2026-05-21T00:00:00Z",
		"observations": [
			{"kind":"rule","text":"alpha rule","evidence":"turn 5"}
		]
	}`
	if err := os.WriteFile(filepath.Join(obsDir, "b.json"), []byte(body2), 0o644); err != nil {
		t.Fatal(err)
	}

	cache, _ := loadCacheForTest(t, stateDir, "test-emb")

	p := &Pipeline{
		Embedder:       fakeEmbedder{},
		EmbeddingModel: "test-emb",
		Cache:          cache,
		CacheSavePath:  filepath.Join(stateDir, "embeddings.json"),
		ClustersPath:   filepath.Join(stateDir, "clusters.json"),
		ThresholdFor:   func(string) float32 { return 0.85 },
		Workers:        2,
	}
	if err := p.Run(context.Background(), obsDir); err != nil {
		t.Fatal(err)
	}
	got, err := LoadClusters(p.ClustersPath)
	if err != nil {
		t.Fatal(err)
	}
	var foundMergedRule bool
	for _, c := range got.Clusters {
		if c.Kind == "rule" && c.EvidenceCount == 2 && c.ProjectCount == 2 {
			foundMergedRule = true
		}
	}
	if !foundMergedRule {
		t.Fatalf("expected merged rule cluster ev=2 proj=2, got: %+v", got.Clusters)
	}
}
