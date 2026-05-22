package extract

import "testing"

func TestObservationsFingerprintDistinguishesInputs(t *testing.T) {
	base := ObservationsFingerprint("claude-code", "p", "sha256:abc", "claude-haiku")

	cases := map[string]string{
		"different source":       ObservationsFingerprint("opencode", "p", "sha256:abc", "claude-haiku"),
		"different project":      ObservationsFingerprint("claude-code", "q", "sha256:abc", "claude-haiku"),
		"different content hash": ObservationsFingerprint("claude-code", "p", "sha256:def", "claude-haiku"),
		"different model":        ObservationsFingerprint("claude-code", "p", "sha256:abc", "claude-sonnet"),
	}
	for name, fp := range cases {
		if fp == base {
			t.Errorf("%s should change fingerprint", name)
		}
	}
	if same := ObservationsFingerprint("claude-code", "p", "sha256:abc", "claude-haiku"); same != base {
		t.Errorf("same inputs should yield same fingerprint")
	}
}
