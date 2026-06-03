package synthesize

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SarahFrankle/ghost/internal/cluster"
)

// funcClient routes each Complete call through fn, keyed off the user
// payload. It holds no mutable state, so it is safe under BuildTopics'
// parallel per-cluster calls. Tests use prefix/substring matching on the
// payload to return a distinct, deterministic body per call regardless of
// the order parallel topic calls arrive in.
type funcClient struct {
	fn func(user string) (string, error)
}

func (c *funcClient) Complete(ctx context.Context, model, system, user string) (string, error) {
	return c.fn(user)
}

func TestPipelineWritesBothFilesAtomically(t *testing.T) {
	ghostDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(ghostDir, "rules.user.md"), []byte("- avoid em-dashes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cf := cluster.ClustersFile{
		Clusters: []cluster.Cluster{
			{Kind: "identity", Canonical: "works at Miro", EvidenceCount: 3, ProjectCount: 2,
				Members: []cluster.ClusterMember{{Text: "works at Miro", Evidence: "t1", Project: "p"}}},
			{Kind: "rule", Canonical: "prefer integration tests", EvidenceCount: 3, ProjectCount: 2,
				Members: []cluster.ClusterMember{{Text: "x", Evidence: "t1", Project: "p"}}},
		},
	}
	client := &funcClient{fn: func(user string) (string, error) {
		switch {
		case strings.Contains(user, "works at Miro"):
			return "# Identity\n\nworks at Miro.\n", nil
		case strings.Contains(user, "prefer integration tests"):
			return "# Rules\n\n- prefer integration tests\n", nil
		}
		return "", fmt.Errorf("unexpected payload: %q", user)
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
		{Kind: "identity", Canonical: "ident-canon", EvidenceCount: 1, ProjectCount: 1,
			Members: []cluster.ClusterMember{{Text: "ident-canon", Evidence: "t", Project: "p"}}},
		{Kind: "rule", Canonical: "rule-canon", EvidenceCount: 3, ProjectCount: 2,
			Members: []cluster.ClusterMember{{Text: "rule-canon", Evidence: "t", Project: "p"}}},
	}}

	client := &funcClient{fn: func(user string) (string, error) {
		switch {
		case strings.Contains(user, "ident-canon"):
			return "# Identity\n\nnew\n", nil
		case strings.Contains(user, "rule-canon"):
			return "", fmt.Errorf("forced rules failure")
		}
		return "", fmt.Errorf("unexpected payload: %q", user)
	}}
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

func TestPipelineWritesTopicsSubdir(t *testing.T) {
	dir := t.TempDir()
	client := &funcClient{fn: func(user string) (string, error) {
		switch {
		case strings.HasPrefix(user, "RANKED TOPICS"):
			return "# Index\n", nil
		case strings.Contains(user, "id-canon"):
			return "# Identity\n\nbody.\n", nil
		case strings.Contains(user, "topic-canon"):
			return "# Testing\n\nbody.\n", nil
		}
		return "", fmt.Errorf("unexpected payload: %q", user)
	}}
	p := &Pipeline{
		Client: client, SmartModel: "smart", GhostDir: dir,
		MinRuleEvidence: 2, MinRuleProjects: 2,
	}
	cf := cluster.ClustersFile{Clusters: []cluster.Cluster{
		{Kind: "identity", Canonical: "id-canon", EvidenceCount: 2, ProjectCount: 2,
			Members: []cluster.ClusterMember{{Text: "id-canon", Evidence: "t", Project: "p"}}},
		{Kind: "topic", Canonical: "topic-canon",
			EvidenceCount: 3, ProjectCount: 2,
			Members: []cluster.ClusterMember{{Text: "topic-canon", Evidence: "t", Project: "p"}}},
	}}
	if err := p.Run(context.Background(), cf); err != nil {
		t.Fatal(err)
	}
	// Topic filename is derived from the body's H1 ("# Testing"), not from
	// any upstream slug.
	for _, want := range []string{"identity.md", "rules.md", "topics/testing.md"} {
		if _, err := os.Stat(filepath.Join(dir, want)); err != nil {
			t.Fatalf("missing %s: %v", want, err)
		}
	}
}

func TestPipelineRespectsTopicCap(t *testing.T) {
	dir := t.TempDir()
	client := &funcClient{fn: func(user string) (string, error) {
		switch {
		case strings.HasPrefix(user, "RANKED TOPICS"):
			return "# Index\n", nil
		case strings.Contains(user, "alpha-topic"):
			return "# Alpha\n", nil
		case strings.Contains(user, "beta-topic"):
			return "# Beta\n", nil
		}
		return "", fmt.Errorf("unexpected payload: %q", user)
	}}
	p := &Pipeline{
		Client: client, SmartModel: "smart", GhostDir: dir,
		MinRuleEvidence: 1, MinRuleProjects: 1, MaxTopicEntries: 1,
	}
	cf := cluster.ClustersFile{Clusters: []cluster.Cluster{
		{Kind: "topic", Canonical: "alpha-topic", EvidenceCount: 10, ProjectCount: 1,
			Members: []cluster.ClusterMember{{Text: "alpha-topic", Evidence: "t", Project: "p"}}},
		{Kind: "topic", Canonical: "beta-topic", EvidenceCount: 1, ProjectCount: 1,
			Members: []cluster.ClusterMember{{Text: "beta-topic", Evidence: "t", Project: "p"}}},
	}}
	if err := p.Run(context.Background(), cf); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "topics", "alpha.md")); err != nil {
		t.Fatalf("expected topics/alpha.md (highest evidence): %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "topics", "beta.md")); !os.IsNotExist(err) {
		t.Fatalf("expected topics/beta.md to be capped out, got err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "index.md")); err != nil {
		t.Fatalf("expected index.md: %v", err)
	}
}

func TestPipelineSlugCollisionMergesAndSucceeds(t *testing.T) {
	dir := t.TempDir()
	// Seed a prior topics dir with an unrelated topic.
	if err := os.MkdirAll(filepath.Join(dir, "topics"), 0o755); err != nil {
		t.Fatal(err)
	}
	prior := filepath.Join(dir, "topics", "old.md")
	if err := os.WriteFile(prior, []byte("# Old\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Both topic clusters produce the title "Conflict" → same slug → merged
	// into one cluster and re-synthesized. The merged cluster contains both
	// "first-canon" and "second-canon" members.
	client := &funcClient{fn: func(user string) (string, error) {
		switch {
		case strings.HasPrefix(user, "RANKED TOPICS"):
			return "# Index\n", nil
		case strings.Contains(user, "ident-canon"):
			return "# Identity\n\nbody.\n", nil
		case strings.Contains(user, "first-canon") && strings.Contains(user, "second-canon"):
			// merged re-synthesis call
			return "# Conflict\n\nmerged.\n", nil
		case strings.Contains(user, "first-canon"):
			return "# Conflict\n\nfirst.\n", nil
		case strings.Contains(user, "second-canon"):
			return "# Conflict\n\nsecond.\n", nil
		}
		return "", fmt.Errorf("unexpected payload: %q", user)
	}}
	cf := cluster.ClustersFile{Clusters: []cluster.Cluster{
		{Kind: "identity", Canonical: "ident-canon", EvidenceCount: 1, ProjectCount: 1,
			Members: []cluster.ClusterMember{{Text: "ident-canon", Evidence: "t", Project: "p"}}},
		{Kind: "topic", Canonical: "first-canon", EvidenceCount: 1, ProjectCount: 1,
			Members: []cluster.ClusterMember{{Text: "first-canon", Evidence: "t", Project: "p"}}},
		{Kind: "topic", Canonical: "second-canon", EvidenceCount: 1, ProjectCount: 1,
			Members: []cluster.ClusterMember{{Text: "second-canon", Evidence: "t", Project: "p"}}},
	}}
	p := &Pipeline{Client: client, SmartModel: "smart", GhostDir: dir, MaxTopicEntries: 20}
	if err := p.Run(context.Background(), cf); err != nil {
		t.Fatalf("expected successful run after merge, got: %v", err)
	}
	// Merged topic should exist.
	if _, err := os.Stat(filepath.Join(dir, "topics", "conflict.md")); err != nil {
		t.Fatalf("merged topic conflict.md not found: %v", err)
	}
	// Prior unrelated topic is replaced (topics/ wiped on success).
	if _, err := os.Stat(prior); err == nil {
		t.Fatal("prior topic old.md should have been removed on successful run")
	}
}

func TestPipelineTopicSynthFailurePreservesPriorTopics(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "topics"), 0o755); err != nil {
		t.Fatal(err)
	}
	prior := filepath.Join(dir, "topics", "old.md")
	if err := os.WriteFile(prior, []byte("# Old\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	client := &funcClient{fn: func(user string) (string, error) {
		switch {
		case strings.HasPrefix(user, "RANKED TOPICS"):
			return "# Index\n", nil
		case strings.Contains(user, "ident-canon"):
			return "# Identity\n\nbody.\n", nil
		case strings.Contains(user, "boom-canon"):
			return "", fmt.Errorf("synth boom")
		default:
			return "# Other\n\nbody.\n", nil
		}
	}}
	cf := cluster.ClustersFile{Clusters: []cluster.Cluster{
		{Kind: "identity", Canonical: "ident-canon",
			Members: []cluster.ClusterMember{{Text: "ident-canon", Evidence: "t", Project: "p"}}},
		{Kind: "topic", Canonical: "boom-canon", EvidenceCount: 1,
			Members: []cluster.ClusterMember{{Text: "boom-canon", Evidence: "t", Project: "p"}}},
	}}
	p := &Pipeline{Client: client, SmartModel: "smart", GhostDir: dir, MaxTopicEntries: 20}
	if err := p.Run(context.Background(), cf); err == nil {
		t.Fatal("expected error when topic synthesis fails")
	}
	if _, err := os.Stat(prior); err != nil {
		t.Fatalf("prior topic was destroyed by a failed run: %v", err)
	}
}
