package effectiveness

import (
	"path/filepath"
	"testing"
)

func TestAppendAndReadEvents(t *testing.T) {
	p := filepath.Join(t.TempDir(), "topic-reads.jsonl")
	a := TopicReadEvent{TranscriptID: "t1", TopicSlug: "design-architecture", TriggerMatched: true, Fit: FitYes}
	b := TopicReadEvent{TranscriptID: "t1", TopicSlug: "testing-discipline", Fit: FitUnknown}
	if err := AppendEvents(p, []TopicReadEvent{a, b}); err != nil {
		t.Fatal(err)
	}
	if err := AppendEvents(p, []TopicReadEvent{{TranscriptID: "t2", TopicSlug: "git", Fit: FitNo}}); err != nil {
		t.Fatal(err)
	}
	got, err := ReadEvents(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3, got %d", len(got))
	}
	if got[0].TopicSlug != "design-architecture" || !got[0].TriggerMatched || got[0].Fit != FitYes {
		t.Errorf("got[0] = %+v", got[0])
	}
	if got[2].TranscriptID != "t2" || got[2].Fit != FitNo {
		t.Errorf("got[2] = %+v", got[2])
	}
}

func TestReadEvents_MissingFileIsEmpty(t *testing.T) {
	got, err := ReadEvents(filepath.Join(t.TempDir(), "nope.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("want 0, got %d", len(got))
	}
}
