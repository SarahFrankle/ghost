package cmd

import (
	"testing"

	"github.com/SarahFrankle/ghost/internal/extract"
	"github.com/SarahFrankle/ghost/internal/seed"
)

func TestSeedObsContributionEmptyWhenAbsent(t *testing.T) {
	members, h1 := seedObsContribution(t.TempDir())
	if len(members) != 0 {
		t.Fatalf("want 0 members, got %d", len(members))
	}
	_, h2 := seedObsContribution(t.TempDir())
	if h1 != h2 {
		t.Fatal("absent-file hash must be stable")
	}
}

func TestSeedObsContributionLoadsMembers(t *testing.T) {
	dir := t.TempDir()
	o := extract.Observation{Kind: extract.KindIdentity, Text: "Jira: SF-1234", Evidence: "user", Confidence: extract.ConfidenceHigh}
	if err := seed.AppendSeedObservation(seed.SeedObservationsPath(dir), o); err != nil {
		t.Fatal(err)
	}
	members, h := seedObsContribution(dir)
	if len(members) != 1 || members[0].Text != "Jira: SF-1234" {
		t.Fatalf("unexpected members: %+v", members)
	}
	if h == "" {
		t.Fatal("non-empty seed must produce a non-empty hash")
	}
}
