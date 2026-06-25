package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SarahFrankle/ghost/internal/extract"
	"github.com/SarahFrankle/ghost/internal/seed"
)

func TestParseKind(t *testing.T) {
	if k, err := parseKind("identity"); err != nil || k != extract.KindIdentity {
		t.Fatalf("identity: %v %v", k, err)
	}
	if k, err := parseKind("preference"); err != nil || k != extract.KindPreference {
		t.Fatalf("preference: %v %v", k, err)
	}
	if _, err := parseKind("voice"); err == nil {
		t.Fatal("voice must be rejected")
	}
	if _, err := parseKind(""); err == nil {
		t.Fatal("empty must be rejected")
	}
}

func TestRememberFactIdentity(t *testing.T) {
	dir := t.TempDir()
	if err := rememberFact(dir, extract.KindIdentity, "Jira: SF-1234"); err != nil {
		t.Fatal(err)
	}
	// seed observation recorded
	f, err := seed.LoadSeedObservations(seed.SeedObservationsPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Observations) != 1 || f.Observations[0].Kind != extract.KindIdentity || f.Observations[0].Confidence != extract.ConfidenceHigh {
		t.Fatalf("seed observation wrong: %+v", f.Observations)
	}
	// provisional append landed in identity.md, not rules.md
	body, err := os.ReadFile(filepath.Join(dir, "identity.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "ghost: pending until next compose") || !strings.Contains(string(body), "Jira: SF-1234") {
		t.Fatalf("identity.md missing provisional fact: %q", string(body))
	}
	if _, err := os.Stat(filepath.Join(dir, "rules.md")); !os.IsNotExist(err) {
		t.Fatal("identity fact must not touch rules.md")
	}
}

func TestRememberFactPreferenceGoesToRules(t *testing.T) {
	dir := t.TempDir()
	if err := rememberFact(dir, extract.KindPreference, "prefer bullets"); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "rules.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "prefer bullets") {
		t.Fatalf("rules.md missing provisional fact: %q", string(body))
	}
}
