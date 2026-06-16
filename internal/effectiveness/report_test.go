package effectiveness

import "testing"

func TestSummarize(t *testing.T) {
	evs := []TopicReadEvent{
		{TopicSlug: "design-architecture", TriggerMatched: true, Fit: FitYes},
		{TopicSlug: "design-architecture", TriggerMatched: true, Fit: FitNo},
		{TopicSlug: "design-architecture", TriggerMatched: false, Fit: FitUnknown},
		{TopicSlug: "git", TriggerMatched: true, Fit: FitYes},
	}
	got := Summarize(evs)
	if len(got) != 2 {
		t.Fatalf("want 2 topics, got %d", len(got))
	}
	da := got[0]
	if da.Slug != "design-architecture" || da.Reads != 3 {
		t.Fatalf("da = %+v", da)
	}
	if da.TriggerMatched != 2 || da.FitYes != 1 || da.FitNo != 1 || da.FitUnknown != 1 {
		t.Errorf("da counts = %+v", da)
	}
}
