package synthesize

import "testing"

import "github.com/SarahFrankle/ghost/internal/cluster"

func TestVerdictFingerprint_ChangesOnMembershipAndPrompt(t *testing.T) {
	cs := []cluster.Cluster{{
		Canonical: "structural diagnosis",
		Members:   []cluster.ClusterMember{{ObservationHash: "h1"}, {ObservationHash: "h2"}},
	}}
	base := VerdictFingerprint(cs, "promptHashA", "claude-opus-4-8")

	if VerdictFingerprint(cs, "promptHashA", "claude-opus-4-8") != base {
		t.Fatal("fingerprint must be stable for identical inputs")
	}
	if VerdictFingerprint(cs, "promptHashB", "claude-opus-4-8") == base {
		t.Fatal("prompt hash change must bust the fingerprint")
	}
	cs2 := []cluster.Cluster{{
		Canonical: "structural diagnosis",
		Members:   []cluster.ClusterMember{{ObservationHash: "h1"}, {ObservationHash: "h3"}},
	}}
	if VerdictFingerprint(cs2, "promptHashA", "claude-opus-4-8") == base {
		t.Fatal("membership change must bust the fingerprint even under a stable theme label")
	}
}
