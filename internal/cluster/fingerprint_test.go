package cluster

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SarahFrankle/ghost/internal/extract"
	"github.com/SarahFrankle/ghost/internal/fingerprint"
)

func TestClustersFingerprintDistinguishesInputs(t *testing.T) {
	base := ClustersFingerprint([]string{"a", "b"}, "voyage-3-lite", 0.85, "haiku", "lp", "sonnet", "tip", "tmp", 3)
	cases := map[string]string{
		"obs added":               ClustersFingerprint([]string{"a", "b", "c"}, "voyage-3-lite", 0.85, "haiku", "lp", "sonnet", "tip", "tmp", 3),
		"embedding model":         ClustersFingerprint([]string{"a", "b"}, "nomic-embed-text", 0.85, "haiku", "lp", "sonnet", "tip", "tmp", 3),
		"identity/rule threshold": ClustersFingerprint([]string{"a", "b"}, "voyage-3-lite", 0.86, "haiku", "lp", "sonnet", "tip", "tmp", 3),
		"label model":             ClustersFingerprint([]string{"a", "b"}, "voyage-3-lite", 0.85, "haiku2", "lp", "sonnet", "tip", "tmp", 3),
		"label prompt":            ClustersFingerprint([]string{"a", "b"}, "voyage-3-lite", 0.85, "haiku", "lp2", "sonnet", "tip", "tmp", 3),
		"theme model":             ClustersFingerprint([]string{"a", "b"}, "voyage-3-lite", 0.85, "haiku", "lp", "opus", "tip", "tmp", 3),
		"theme identify prompt":   ClustersFingerprint([]string{"a", "b"}, "voyage-3-lite", 0.85, "haiku", "lp", "sonnet", "tip2", "tmp", 3),
		"theme map prompt":        ClustersFingerprint([]string{"a", "b"}, "voyage-3-lite", 0.85, "haiku", "lp", "sonnet", "tip", "tmp2", 3),
		"min cluster size":        ClustersFingerprint([]string{"a", "b"}, "voyage-3-lite", 0.85, "haiku", "lp", "sonnet", "tip", "tmp", 4),
	}
	for name, fp := range cases {
		if fp == base {
			t.Errorf("%s should change fingerprint", name)
		}
	}
}

func TestObservationsFingerprintsIgnoreExtractFingerprintAndTimestamp(t *testing.T) {
	dir := t.TempDir()
	obs := []extract.Observation{{Kind: "identity", Text: "t", Evidence: "turn 1: q"}}
	write := func(name, extractFP string, when time.Time) {
		f := extract.ObservationsFile{
			Source: "s", Project: "p", ContentHash: "c",
			Fingerprint: extractFP, ExtractedAt: when,
			Observations: obs,
		}
		b, _ := json.Marshal(f)
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Same observation content, different extract fingerprint + timestamp:
	// the content fingerprint must be identical so a prompt/model bump that
	// produces unchanged observations does not churn downstream caches.
	write("a.json", "EXTRACT-FP-1", time.Unix(1, 0))
	write("b.json", "EXTRACT-FP-2", time.Unix(2, 0))

	got, err := ObservationsFingerprints(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len=%d, want 2", len(got))
	}
	if got[0] != got[1] {
		t.Fatalf("identical content must yield identical fingerprints; got %q and %q", got[0], got[1])
	}
}

func TestObservationsFingerprintsChangeWithContent(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, obs []extract.Observation) {
		f := extract.ObservationsFile{Source: "s", Project: "p", Observations: obs}
		b, _ := json.Marshal(f)
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.json", []extract.Observation{{Kind: "identity", Text: "alpha", Evidence: "turn 1: q"}})
	write("b.json", []extract.Observation{{Kind: "identity", Text: "beta", Evidence: "turn 1: q"}})

	got, err := ObservationsFingerprints(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] == got[1] {
		t.Fatalf("differing content must yield differing fingerprints; got %v", got)
	}
}

func TestClustersFingerprintUsesV4Namespace(t *testing.T) {
	got := ClustersFingerprint([]string{"a"}, "m", 0.85, "haiku", "lp", "sonnet", "tip", "tmp", 3)
	want := fingerprint.Compute(
		"cluster/v4", "m",
		fmt.Sprintf("identity_rule=%g", float32(0.85)),
		"label_model=haiku", "label_prompt=lp",
		"theme_model=sonnet",
		"theme_identify_prompt=tip", "theme_map_prompt=tmp",
		fmt.Sprintf("min_cluster=%d", 3),
		"a",
	)
	if got != want {
		t.Fatalf("ClustersFingerprint must use the cluster/v4 namespace:\n got=%s\nwant=%s", got, want)
	}
}

func TestObservationsFingerprintsMissingDir(t *testing.T) {
	got, err := ObservationsFingerprints(filepath.Join(t.TempDir(), "nope"))
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("missing dir should return nil, got %v", got)
	}
}
