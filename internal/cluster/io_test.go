package cluster

import (
	"path/filepath"
	"testing"
	"time"
)

func TestClustersFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "clusters.json")
	in := ClustersFile{
		SchemaVersion:    SchemaVersion,
		EmbeddingModelID: "voyage-3-lite",
		BuiltAt:          time.Now().UTC(),
		Clusters: []Cluster{
			{Kind: "rule", Canonical: "x", Members: []ClusterMember{{Text: "x"}}, EvidenceCount: 1, ProjectCount: 1},
		},
	}
	if err := SaveClusters(path, in); err != nil {
		t.Fatal(err)
	}
	out, err := LoadClusters(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Clusters) != 1 || out.Clusters[0].Canonical != "x" {
		t.Fatalf("round-trip mismatch: %+v", out)
	}
}
