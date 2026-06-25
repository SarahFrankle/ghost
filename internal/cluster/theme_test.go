package cluster

import (
	"path/filepath"
	"testing"
)

func TestThemesFingerprintStableAndSensitive(t *testing.T) {
	a := ThemesFingerprint([]string{"b", "a"}, "sonnet", "ih", "mh", "sh")
	b := ThemesFingerprint([]string{"a", "b"}, "sonnet", "ih", "mh", "sh") // order-insensitive
	if a != b {
		t.Fatal("fingerprint should be order-insensitive")
	}
	if a == ThemesFingerprint([]string{"a", "b"}, "sonnet", "ih2", "mh", "sh") {
		t.Fatal("identify-prompt change must alter fingerprint")
	}
	if a == ThemesFingerprint([]string{"a", "b"}, "sonnet", "ih", "mh2", "sh") {
		t.Fatal("map-prompt change must alter fingerprint")
	}
	if a == ThemesFingerprint([]string{"a", "b", "c"}, "sonnet", "ih", "mh", "sh") {
		t.Fatal("label-set change must alter fingerprint")
	}
}

func TestThemesRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "themes.json")
	want := ThemesFile{Fingerprint: "fp", Mapping: map[string]string{"git": "Git Workflow"}}
	if err := SaveThemes(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadThemes(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Fingerprint != "fp" || got.Mapping["git"] != "Git Workflow" {
		t.Fatalf("round trip mismatch: %+v", got)
	}
}

func TestThemesFingerprintSeedHashBusts(t *testing.T) {
	a := ThemesFingerprint([]string{"l"}, "m", "i", "p", "seedA")
	b := ThemesFingerprint([]string{"l"}, "m", "i", "p", "seedB")
	if a == b {
		t.Fatal("editing the seed hash must bust the themes fingerprint")
	}
}

func TestValidateMappingFlagsMissing(t *testing.T) {
	err := validateMapping([]string{"a", "b"}, map[string]string{"a": "A"})
	if err == nil {
		t.Fatal("expected error for missing label b")
	}
}
