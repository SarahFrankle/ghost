package synthesize

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SarahFrankle/ghost/internal/cluster"
)

type scriptedClient struct {
	bySystem map[string]string // system-prompt-substring → response
	err      map[string]error  // system-prompt-substring → error
}

func (s *scriptedClient) Complete(ctx context.Context, model, system, user string) (string, error) {
	for key, e := range s.err {
		if strings.Contains(system, key) {
			return "", e
		}
	}
	for key, resp := range s.bySystem {
		if strings.Contains(system, key) {
			return resp, nil
		}
	}
	return "", nil
}

func TestPipelineWritesBothFilesAtomically(t *testing.T) {
	ghostDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(ghostDir, "rules.user.md"), []byte("- avoid em-dashes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cf := cluster.ClustersFile{
		Clusters: []cluster.Cluster{
			{Kind: "identity", Canonical: "works at Miro", EvidenceCount: 3, ProjectCount: 2},
			{Kind: "rule", Canonical: "prefer integration tests", EvidenceCount: 3, ProjectCount: 2,
				Members: []cluster.ClusterMember{{Text: "x", Evidence: "t1", Project: "p"}}},
		},
	}
	client := &scriptedClient{bySystem: map[string]string{
		"# Identity": "# Identity\n\nworks at Miro.\n",
		"# Rules":    "# Rules\n\n- prefer integration tests\n",
	}}
	p := &Pipeline{
		Client:          client,
		SmartModel:      "smart",
		GhostDir:        ghostDir,
		MinRuleEvidence: 2,
		MinRuleProjects: 2,
	}
	if err := p.Run(context.Background(), cf); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"identity.md", "rules.md"} {
		b, err := os.ReadFile(filepath.Join(ghostDir, name))
		if err != nil {
			t.Fatalf("%s missing: %v", name, err)
		}
		if !strings.HasPrefix(string(b), "# ") {
			t.Fatalf("%s missing heading: %q", name, string(b))
		}
	}
}

func TestPipelinePreservesPriorGenerationOnPartialFailure(t *testing.T) {
	ghostDir := t.TempDir()
	priorIdentity := "# Identity\n\nprior.\n"
	priorRules := "# Rules\n\n- prior\n"
	_ = os.WriteFile(filepath.Join(ghostDir, "identity.md"), []byte(priorIdentity), 0o644)
	_ = os.WriteFile(filepath.Join(ghostDir, "rules.md"), []byte(priorRules), 0o644)

	cf := cluster.ClustersFile{Clusters: []cluster.Cluster{
		{Kind: "identity", Canonical: "x", EvidenceCount: 1, ProjectCount: 1},
		{Kind: "rule", Canonical: "y", EvidenceCount: 3, ProjectCount: 2,
			Members: []cluster.ClusterMember{{Text: "y", Evidence: "t", Project: "p"}}},
	}}

	client := &scriptedClient{
		bySystem: map[string]string{"# Identity": "# Identity\n\nnew\n"},
		err:      map[string]error{"# Rules": errFakeRulesFail},
	}
	p := &Pipeline{Client: client, SmartModel: "x", GhostDir: ghostDir,
		MinRuleEvidence: 2, MinRuleProjects: 2}

	if err := p.Run(context.Background(), cf); err == nil {
		t.Fatal("expected error on partial failure")
	}
	got, _ := os.ReadFile(filepath.Join(ghostDir, "identity.md"))
	if string(got) != priorIdentity {
		t.Fatalf("prior identity.md was overwritten despite partial failure: %q", string(got))
	}
	got, _ = os.ReadFile(filepath.Join(ghostDir, "rules.md"))
	if string(got) != priorRules {
		t.Fatalf("prior rules.md was overwritten: %q", string(got))
	}
	entries, _ := os.ReadDir(ghostDir)
	foundTmp := false
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), ".tmp-synthesize-") {
			foundTmp = true
		}
	}
	if !foundTmp {
		t.Fatal("expected preserved tmpdir on partial failure")
	}
}

var errFakeRulesFail = &stringErr{"forced rules failure"}

type stringErr struct{ s string }

func (e *stringErr) Error() string { return e.s }

func TestPipelineWritesTopicsSubdir(t *testing.T) {
	dir := t.TempDir()
	f := &fakeClient{resp: "# Out\n\nbody.\n"}
	p := &Pipeline{
		Client: f, SmartModel: "smart", GhostDir: dir,
		MinRuleEvidence: 2, MinRuleProjects: 2,
	}
	cf := cluster.ClustersFile{Clusters: []cluster.Cluster{
		{Kind: "identity", Canonical: "id", EvidenceCount: 2, ProjectCount: 2,
			Members: []cluster.ClusterMember{{Text: "id", Evidence: "t", Project: "p"}}},
		{Kind: "topic", SubKey: "testing", Canonical: "prefer table-driven",
			EvidenceCount: 3, ProjectCount: 2,
			Members: []cluster.ClusterMember{{Text: "prefer table-driven", Evidence: "t", Project: "p"}}},
	}}
	if err := p.Run(context.Background(), cf); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"identity.md", "rules.md", "topics/testing.md"} {
		if _, err := os.Stat(filepath.Join(dir, want)); err != nil {
			t.Fatalf("missing %s: %v", want, err)
		}
	}
}

func TestPipelinePartialFailureLeavesPriorTopicsIntact(t *testing.T) {
	dir := t.TempDir()
	// Seed an existing topics/ directory.
	if err := os.MkdirAll(filepath.Join(dir, "topics"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "topics", "old.md"), []byte("# Old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Client that errors on the second Complete call (BuildRules).
	f := &countingFakeClient{respondAfter: func(n int) (string, error) {
		if n == 2 {
			return "", errors.New("boom")
		}
		return "# Out\n", nil
	}}
	p := &Pipeline{Client: f, SmartModel: "smart", GhostDir: dir, MinRuleEvidence: 1, MinRuleProjects: 1}
	cf := cluster.ClustersFile{Clusters: []cluster.Cluster{
		{Kind: "identity", Canonical: "x", EvidenceCount: 1, ProjectCount: 1,
			Members: []cluster.ClusterMember{{Text: "x", Evidence: "t", Project: "p"}}},
		{Kind: "rule", Canonical: "y", EvidenceCount: 1, ProjectCount: 1,
			Members: []cluster.ClusterMember{{Text: "y", Evidence: "t", Project: "p"}}},
	}}
	err := p.Run(context.Background(), cf)
	if err == nil {
		t.Fatal("expected partial-failure error")
	}
	// Prior topics survive.
	if _, statErr := os.Stat(filepath.Join(dir, "topics", "old.md")); statErr != nil {
		t.Fatalf("prior topics/old.md was destroyed by failed run: %v", statErr)
	}
}

type countingFakeClient struct {
	n            int
	respondAfter func(n int) (string, error)
}

func (c *countingFakeClient) Complete(ctx context.Context, model, system, user string) (string, error) {
	c.n++
	return c.respondAfter(c.n)
}

func TestPipelineRespectsTopicCap(t *testing.T) {
	dir := t.TempDir()
	f := &fakeClient{resp: "# Out\n"}
	p := &Pipeline{
		Client: f, SmartModel: "smart", GhostDir: dir,
		MinRuleEvidence: 1, MinRuleProjects: 1, MaxTopicEntries: 1,
	}
	cf := cluster.ClustersFile{Clusters: []cluster.Cluster{
		{Kind: "topic", SubKey: "a", Canonical: "ca", EvidenceCount: 10, ProjectCount: 1,
			Members: []cluster.ClusterMember{{Text: "ca", Evidence: "t", Project: "p"}}},
		{Kind: "topic", SubKey: "b", Canonical: "cb", EvidenceCount: 1, ProjectCount: 1,
			Members: []cluster.ClusterMember{{Text: "cb", Evidence: "t", Project: "p"}}},
	}}
	if err := p.Run(context.Background(), cf); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "topics", "a.md")); err != nil {
		t.Fatalf("expected topics/a.md (highest evidence): %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "topics", "b.md")); !os.IsNotExist(err) {
		t.Fatalf("expected topics/b.md to be capped out, got err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "index.md")); err != nil {
		t.Fatalf("expected index.md: %v", err)
	}
}
