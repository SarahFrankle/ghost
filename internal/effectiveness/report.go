package effectiveness

import "sort"

// TopicSummary aggregates topic-read events for one topic slug.
type TopicSummary struct {
	Slug           string
	Reads          int
	TriggerMatched int
	FitYes         int
	FitPartial     int
	FitNo          int
	FitUnknown     int
}

// Summarize aggregates events per topic, sorted by read count descending then
// slug ascending.
func Summarize(evs []TopicReadEvent) []TopicSummary {
	m := map[string]*TopicSummary{}
	for _, e := range evs {
		s := m[e.TopicSlug]
		if s == nil {
			s = &TopicSummary{Slug: e.TopicSlug}
			m[e.TopicSlug] = s
		}
		s.Reads++
		if e.TriggerMatched {
			s.TriggerMatched++
		}
		switch e.Fit {
		case FitYes:
			s.FitYes++
		case FitPartial:
			s.FitPartial++
		case FitNo:
			s.FitNo++
		default:
			s.FitUnknown++
		}
	}
	out := make([]TopicSummary, 0, len(m))
	for _, s := range m {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Reads != out[j].Reads {
			return out[i].Reads > out[j].Reads
		}
		return out[i].Slug < out[j].Slug
	})
	return out
}
