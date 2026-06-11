package synthesize

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/SarahFrankle/ghost/internal/cluster"
)

// fakeVerdictClient returns a canned JSON verdicts payload and records calls.
type fakeVerdictClient struct {
	calls int
	reply string
}

func (f *fakeVerdictClient) Complete(_ context.Context, _ string, _, _ string) (string, error) {
	f.calls++
	return f.reply, nil
}

func TestRouteByGenerality_SplitsAndCaches(t *testing.T) {
	clusters := []cluster.Cluster{
		{Canonical: "root cause over symptom", Members: []cluster.ClusterMember{{ObservationHash: "a"}}},
		{Canonical: "pytest fixtures for db", Members: []cluster.ClusterMember{{ObservationHash: "b"}}},
	}
	fc := &fakeVerdictClient{reply: `{"verdicts":[{"label":"root cause over symptom","general":true},{"label":"pytest fixtures for db","general":false}]}`}
	path := filepath.Join(t.TempDir(), "verdicts.json")

	gen, scoped, err := RouteByGenerality(context.Background(), fc, "claude-opus-4-8", clusters, path, "promptHashA", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(gen) != 1 || gen[0].Canonical != "root cause over symptom" {
		t.Fatalf("want 1 general (root cause), got %+v", gen)
	}
	if len(scoped) != 1 || scoped[0].Canonical != "pytest fixtures for db" {
		t.Fatalf("want 1 scoped (pytest), got %+v", scoped)
	}

	before := fc.calls
	if _, _, err := RouteByGenerality(context.Background(), fc, "claude-opus-4-8", clusters, path, "promptHashA", nil); err != nil {
		t.Fatal(err)
	}
	if fc.calls != before {
		t.Fatalf("expected cache hit, model was called again (%d -> %d)", before, fc.calls)
	}
}

// A partial / out-of-label response must degrade (default missing themes to
// scoped) rather than fail the rebuild, and must match by position not label.
func TestRouteByGenerality_PartialResponseDegradesToScoped(t *testing.T) {
	clusters := []cluster.Cluster{
		{Canonical: "root cause over symptom", Members: []cluster.ClusterMember{{ObservationHash: "a"}}},
		{Canonical: "pytest fixtures for db", Members: []cluster.ClusterMember{{ObservationHash: "b"}}},
	}
	fc := &fakeVerdictClient{reply: `{"verdicts":[{"label":"Root cause over symptom.","general":true}]}`}
	path := filepath.Join(t.TempDir(), "verdicts.json")

	gen, scoped, err := RouteByGenerality(context.Background(), fc, "claude-opus-4-8", clusters, path, "promptHashA", nil)
	if err != nil {
		t.Fatalf("partial response must not error: %v", err)
	}
	if len(gen) != 1 || gen[0].Canonical != "root cause over symptom" {
		t.Fatalf("position-0 verdict must bind despite altered label; got general=%+v", gen)
	}
	if len(scoped) != 1 || scoped[0].Canonical != "pytest fixtures for db" {
		t.Fatalf("missing verdict must default to scoped; got scoped=%+v", scoped)
	}
}
