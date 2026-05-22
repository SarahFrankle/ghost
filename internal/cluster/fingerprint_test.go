package cluster

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/SarahFrankle/ghost/internal/extract"
)

func TestClustersFingerprintDistinguishesInputs(t *testing.T) {
	base := ClustersFingerprint([]string{"a", "b"}, "voyage-3-lite", "claude-haiku", "promptHash", 0.82)
	cases := map[string]string{
		"obs added":         ClustersFingerprint([]string{"a", "b", "c"}, "voyage-3-lite", "claude-haiku", "promptHash", 0.82),
		"embedding model":   ClustersFingerprint([]string{"a", "b"}, "nomic-embed-text", "claude-haiku", "promptHash", 0.82),
		"canonical model":   ClustersFingerprint([]string{"a", "b"}, "voyage-3-lite", "claude-sonnet", "promptHash", 0.82),
		"prompt hash":       ClustersFingerprint([]string{"a", "b"}, "voyage-3-lite", "claude-haiku", "promptHash2", 0.82),
		"cosine threshold":  ClustersFingerprint([]string{"a", "b"}, "voyage-3-lite", "claude-haiku", "promptHash", 0.83),
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

func TestObservationsFingerprintsMissingDir(t *testing.T) {
	got, err := ObservationsFingerprints(filepath.Join(t.TempDir(), "nope"))
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("missing dir should return nil, got %v", got)
	}
}
