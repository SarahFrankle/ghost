package effectiveness

import (
	"context"
	"path/filepath"
	"testing"
)

type fakeClient struct {
	calls int
	reply string
}

func (f *fakeClient) Complete(ctx context.Context, model, system, user string) (string, error) {
	f.calls++
	return f.reply, nil
}

func TestJudge_ParsesAndCaches(t *testing.T) {
	fc := &fakeClient{reply: "FIT: yes — directly about refactoring\n"}
	cachePath := filepath.Join(t.TempDir(), "judge-cache.json")
	j := NewJudge(fc, "model-x", "system prompt", "promptHash", cachePath)

	ev := TopicReadEvent{TopicSlug: "design-architecture", TaskContextExcerpt: "refactor this"}
	fit, reason, err := j.Judge(context.Background(), ev, "topic body content")
	if err != nil {
		t.Fatal(err)
	}
	if fit != FitYes || reason != "directly about refactoring" {
		t.Fatalf("fit=%q reason=%q", fit, reason)
	}
	if _, _, err := j.Judge(context.Background(), ev, "topic body content"); err != nil {
		t.Fatal(err)
	}
	if fc.calls != 1 {
		t.Fatalf("want 1 client call (cache hit on 2nd), got %d", fc.calls)
	}
	if err := j.SaveCache(); err != nil {
		t.Fatal(err)
	}
}

func TestParseFitLine_SkipsPreamble(t *testing.T) {
	fit, reason := parseFitLine("Here is my answer:\nFIT: yes — central to the task")
	if fit != FitYes || reason != "central to the task" {
		t.Fatalf("fit=%q reason=%q", fit, reason)
	}
}

func TestJudge_DoesNotCacheUnknown(t *testing.T) {
	fc := &fakeClient{reply: "I cannot answer that"} // no FIT line -> FitUnknown
	j := NewJudge(fc, "m", "sys", "h", filepath.Join(t.TempDir(), "c.json"))
	ev := TopicReadEvent{TopicSlug: "x", TaskContextExcerpt: "y"}
	if fit, _, _ := j.Judge(context.Background(), ev, "body"); fit != FitUnknown {
		t.Fatalf("want FitUnknown")
	}
	// second call must hit the client again (not cached)
	if _, _, err := j.Judge(context.Background(), ev, "body"); err != nil {
		t.Fatal(err)
	}
	if fc.calls != 2 {
		t.Fatalf("want 2 client calls (unknown not cached), got %d", fc.calls)
	}
}

func TestParseFitLine(t *testing.T) {
	cases := map[string]struct {
		fit    Fit
		reason string
	}{
		"FIT: yes — central":        {FitYes, "central"},
		"FIT: partial - tangential": {FitPartial, "tangential"},
		"FIT: no — unrelated":       {FitNo, "unrelated"},
		"garbage":                   {FitUnknown, ""},
	}
	for in, want := range cases {
		fit, reason := parseFitLine(in)
		if fit != want.fit || reason != want.reason {
			t.Errorf("%q -> fit=%q reason=%q, want %q/%q", in, fit, reason, want.fit, want.reason)
		}
	}
}
