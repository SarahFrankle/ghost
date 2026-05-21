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
