package cluster

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/SarahFrankle/ghost/internal/embedding"
)

type fakeEmbedder struct{}

// trackingEmbedder records all texts passed to Embed so tests can assert what
// was (or was not) embedded.
type trackingEmbedder struct {
	texts []string
}

func (te *trackingEmbedder) Embed(_ context.Context, _ string, texts []string) ([][]float32, error) {
	te.texts = append(te.texts, texts...)
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

func TestPipelineRoutesPreferencesThroughGrouper(t *testing.T) {
	stateDir := t.TempDir()
	obsDir := filepath.Join(stateDir, "observations")
	if err := os.MkdirAll(obsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Three preference observations (one theme) plus one identity observation:
	// preferences route through the grouper, identity still buckets by cosine.
	body := `{
		"source": "/p/proj-a.jsonl", "project": "proj-a", "content_hash": "sha256:a",
		"extracted_at": "2026-05-21T00:00:00Z",
		"observations": [
			{"kind":"preference","text":"a","evidence":"e"},
			{"kind":"preference","text":"b","evidence":"e"},
			{"kind":"preference","text":"c","evidence":"e"},
			{"kind":"identity","text":"works at Miro","evidence":"e"}
		]
	}`
	if err := os.WriteFile(filepath.Join(obsDir, "a.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	cache, _ := loadCacheForTest(t, stateDir, "test-emb")
	g := &TopicGrouper{
		Label:         func(context.Context, string) (string, error) { return "x", nil },
		ThemeIdentify: func(context.Context, []string) ([]string, error) { return []string{"Theme X"}, nil },
		ThemeMap: func(_ context.Context, _ []string, labels []string) (map[string]string, error) {
			m := map[string]string{}
			for _, l := range labels {
				m[l] = "Theme X"
			}
			return m, nil
		},
		Cache:                   mustLabelCache(t, stateDir),
		ThemesPath:              filepath.Join(stateDir, "themes.json"),
		ThemeModel:              "sonnet",
		ThemeIdentifyPromptHash: "ih",
		ThemeMapPromptHash:      "mh",
		MinClusterSize:          3,
		Workers:                 2,
	}
	p := &Pipeline{
		Embedder:       fakeEmbedder{},
		EmbeddingModel: "test-emb",
		Cache:          cache,
		CacheSavePath:  filepath.Join(stateDir, "embeddings.json"),
		ClustersPath:   filepath.Join(stateDir, "clusters.json"),
		ThresholdFor:   func(string) float32 { return 0.85 },
		Workers:        2,
		Topics:         g,
	}
	if err := p.Run(context.Background(), obsDir); err != nil {
		t.Fatal(err)
	}
	cf, err := LoadClusters(p.ClustersPath)
	if err != nil {
		t.Fatal(err)
	}
	var prefCount, identityCount int
	for _, c := range cf.Clusters {
		switch c.Kind {
		case "preference":
			prefCount++
			if c.Canonical != "Theme X" {
				t.Fatalf("preference canonical = %q, want Theme X", c.Canonical)
			}
			if c.EvidenceCount != 3 {
				t.Fatalf("preference evidence = %d, want 3", c.EvidenceCount)
			}
		case "identity":
			identityCount++
		}
	}
	if prefCount != 1 {
		t.Fatalf("preference clusters = %d, want 1", prefCount)
	}
	if identityCount != 1 {
		t.Fatalf("identity clusters = %d, want 1 (rest still cosine-bucketed)", identityCount)
	}
}

func TestPipeline_RoutesPreferenceToGrouper(t *testing.T) {
	stateDir := t.TempDir()
	obsDir := filepath.Join(stateDir, "observations")
	if err := os.MkdirAll(obsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Three preference observations (one theme) — they must route through the
	// grouper just like topic observations, NOT through embed+cosine.
	body := `{
		"source": "/p/proj-a.jsonl", "project": "proj-a", "content_hash": "sha256:a",
		"extracted_at": "2026-05-21T00:00:00Z",
		"observations": [
			{"kind":"preference","text":"a","evidence":"e"},
			{"kind":"preference","text":"b","evidence":"e"},
			{"kind":"preference","text":"c","evidence":"e"}
		]
	}`
	if err := os.WriteFile(filepath.Join(obsDir, "a.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	cache, _ := loadCacheForTest(t, stateDir, "test-emb")
	emb := &trackingEmbedder{}
	g := &TopicGrouper{
		Label:         func(context.Context, string) (string, error) { return "x", nil },
		ThemeIdentify: func(context.Context, []string) ([]string, error) { return []string{"Theme X"}, nil },
		ThemeMap: func(_ context.Context, _ []string, labels []string) (map[string]string, error) {
			m := map[string]string{}
			for _, l := range labels {
				m[l] = "Theme X"
			}
			return m, nil
		},
		Cache:                   mustLabelCache(t, stateDir),
		ThemesPath:              filepath.Join(stateDir, "themes.json"),
		ThemeModel:              "sonnet",
		ThemeIdentifyPromptHash: "ih",
		ThemeMapPromptHash:      "mh",
		MinClusterSize:          1,
		Workers:                 2,
	}
	p := &Pipeline{
		Embedder:       emb,
		EmbeddingModel: "test-emb",
		Cache:          cache,
		CacheSavePath:  filepath.Join(stateDir, "embeddings.json"),
		ClustersPath:   filepath.Join(stateDir, "clusters.json"),
		ThresholdFor:   func(string) float32 { return 0.85 },
		Workers:        2,
		Topics:         g,
	}
	if err := p.Run(context.Background(), obsDir); err != nil {
		t.Fatal(err)
	}

	// Preference must NOT have been embedded.
	if len(emb.texts) != 0 {
		t.Fatalf("embedder received %d text(s), want 0 (preference bypasses embedding)", len(emb.texts))
	}

	cf, err := LoadClusters(p.ClustersPath)
	if err != nil {
		t.Fatal(err)
	}
	var prefCount int
	for _, c := range cf.Clusters {
		if c.Kind == "preference" {
			prefCount++
			if c.Canonical != "Theme X" {
				t.Fatalf("preference canonical = %q, want Theme X", c.Canonical)
			}
			if c.EvidenceCount != 3 {
				t.Fatalf("preference evidence = %d, want 3", c.EvidenceCount)
			}
		}
	}
	if prefCount != 1 {
		t.Fatalf("preference clusters = %d, want 1", prefCount)
	}
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
