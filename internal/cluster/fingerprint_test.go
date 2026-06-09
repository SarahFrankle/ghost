package cluster

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

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

func TestObservationsFingerprintsSortsAndReadsFile(t *testing.T) {
	dir := t.TempDir()
	write := func(name, fp string) {
		f := extract.ObservationsFile{Source: "s", Project: "p", ContentHash: "c", Fingerprint: fp}
		b, _ := json.Marshal(f)
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("z.json", "ZZZ")
	write("a.json", "AAA")
	write("m.json", "MMM")

	got, err := ObservationsFingerprints(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"AAA", "MMM", "ZZZ"}
	if len(got) != len(want) {
		t.Fatalf("len=%d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("position %d: got %q, want %q", i, got[i], want[i])
		}
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
