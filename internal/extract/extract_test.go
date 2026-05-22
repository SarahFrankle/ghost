package extract

import (
	"context"
	"strings"
	"testing"

	"github.com/SarahFrankle/ghost/internal/transcript"
)

type fakeClient struct{ resp string }

func (f *fakeClient) Complete(ctx context.Context, model, system, user string) (string, error) {
	return f.resp, nil
}

func TestRunDropsInvalidAndSecretObservations(t *testing.T) {
	fake := &fakeClient{resp: `{
		"observations": [
			{"kind":"identity","text":"works at Miro","evidence":"turn 1"},
			{"kind":"rule","text":"API key is sk-ant-api03-aaaaaaaaaaaaaaaaaaaaaaa","evidence":"turn 2"},
			{"kind":"voice","text":"lowercase","evidence":"turn 3"}
		]
	}`}
	r := &Runner{Client: fake, Model: "test-model"}

	out, err := r.Run(context.Background(), transcript.Transcript{
		Path: "testdata/golden_transcript.jsonl", Project: "p1",
	}, "sha256:abc")
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Observations) != 1 {
		t.Fatalf("expected 1 kept obs, got %d: %+v", len(out.Observations), out.Observations)
	}
	if !strings.Contains(out.Observations[0].Text, "Miro") {
		t.Fatalf("kept wrong observation: %+v", out.Observations[0])
	}
}

func TestRunDropsObservationsCitingInjectedMaterial(t *testing.T) {
	fake := &fakeClient{resp: `{
		"observations": [
			{"kind":"identity","text":"works at Miro","evidence":"turn 1: I work at Miro"},
			{"kind":"rule","text":"prefer local-first LLMs","evidence":"memory context: documented preference"},
			{"kind":"identity","text":"GitHub user X","evidence":"from CLAUDE.md auto-memory"},
			{"kind":"rule","text":"do Y","evidence":"system reminder: ..."},
			{"kind":"identity","text":"Sarah","evidence":"@~/.ghost/identity.md says ..."}
		]
	}`}
	r := &Runner{Client: fake, Model: "test"}
	out, err := r.Run(context.Background(), transcript.Transcript{
		Path: "testdata/golden_transcript.jsonl", Project: "p",
	}, "sha256:abc")
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Observations) != 1 || !strings.Contains(out.Observations[0].Text, "Miro") {
		t.Fatalf("expected only the turn-cited Miro observation, got: %+v", out.Observations)
	}
}

func TestIsInjectedSource(t *testing.T) {
	cases := map[string]bool{
		"turn 3: I prefer X":                false,
		"turn 12: ...":                      false,
		"memory context: documented pref":   true,
		"from CLAUDE.md":                    true,
		"system reminder injected this":     true,
		"@memory/MEMORY.md line 4":          true,
		"@~/.ghost/rules.md says X":         true,
		"  TURN 1: case-insensitive prefix": false,
		"turns 3-5: range form is fine":    false,
	}
	for in, want := range cases {
		if got := isInjectedSource(in); got != want {
			t.Errorf("isInjectedSource(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestParseObservationsHandlesBracesInsideStrings(t *testing.T) {
	// Trailing `}` inside the evidence quote previously confused the
	// LastIndex-based span and produced malformed JSON.
	raw := "Here is the output:\n" +
		`{"observations":[{"kind":"rule","text":"prefer { brace } style","evidence":"turn 1: user said \"use {foo} pattern\""}]}` +
		"\nlet me know if you want more."
	got, err := parseObservations(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got) != 1 || !strings.Contains(got[0].Text, "brace") {
		t.Fatalf("unexpected parse result: %+v", got)
	}
}
