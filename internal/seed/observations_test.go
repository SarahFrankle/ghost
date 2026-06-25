package seed

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/SarahFrankle/ghost/internal/extract"
)

func TestLoadSeedObservationsMissingIsEmpty(t *testing.T) {
	f, err := LoadSeedObservations(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("missing file must not error: %v", err)
	}
	if len(f.Observations) != 0 {
		t.Fatalf("want empty, got %d", len(f.Observations))
	}
}

func TestAppendSeedObservationCreatesAndAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "seed-observations.json")
	o := extract.Observation{Kind: extract.KindIdentity, Text: "Jira: SF-1234", Evidence: "stated directly by user", Confidence: extract.ConfidenceHigh}
	if err := AppendSeedObservation(path, o); err != nil {
		t.Fatal(err)
	}
	if err := AppendSeedObservation(path, o); err != nil {
		t.Fatal(err)
	}
	f, err := LoadSeedObservations(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Observations) != 2 {
		t.Fatalf("want 2 observations, got %d", len(f.Observations))
	}
	if f.Source != "seed" || f.Project != "user-seed" {
		t.Fatalf("unexpected envelope: source=%q project=%q", f.Source, f.Project)
	}
}

func TestAppendSeedObservationRejectsInvalid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "seed-observations.json")
	err := AppendSeedObservation(path, extract.Observation{Kind: extract.KindIdentity, Text: ""})
	if err == nil {
		t.Fatal("empty text must be rejected")
	}
}

func TestObservationsHashChangesWithContent(t *testing.T) {
	a := extract.ObservationsFile{Observations: []extract.Observation{{Kind: extract.KindIdentity, Text: "x"}}}
	b := extract.ObservationsFile{Observations: []extract.Observation{{Kind: extract.KindIdentity, Text: "y"}}}
	if ObservationsHash(a) == ObservationsHash(b) {
		t.Fatal("hash must differ on different content")
	}
	ha1, ha2 := ObservationsHash(a), ObservationsHash(a)
	if ha1 != ha2 {
		t.Fatal("hash must be stable")
	}
}

func TestAppendSeedObservationFileIsValidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "seed-observations.json")
	o := extract.Observation{Kind: extract.KindPreference, Text: "prefer bullets", Evidence: "stated directly by user", Confidence: extract.ConfidenceHigh}
	if err := AppendSeedObservation(path, o); err != nil {
		t.Fatal(err)
	}
	if _, err := os.ReadFile(path); err != nil {
		t.Fatal(err)
	}
}
