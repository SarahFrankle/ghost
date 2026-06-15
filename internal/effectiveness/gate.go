package effectiveness

import (
	"strings"

	"github.com/SarahFrankle/ghost/internal/transcript"
)

// DetectTopicReads scans an ordered event stream for Read tool calls of files
// directly under topicsDir, and returns one ungraded TopicReadEvent per hit.
// Fit is left FitUnknown; the judge stage fills it in later.
//
// The task-context window for a read is the user text turn(s) from the most
// recent user turn at/before the read through (exclusive of) the next user
// turn. Intervening tool events are ignored for context.
func DetectTopicReads(transcriptID string, evs []transcript.Event, topicsDir string, triggers map[string][]string) []TopicReadEvent {
	prefix := strings.TrimSuffix(topicsDir, "/") + "/"
	var out []TopicReadEvent
	for i, ev := range evs {
		if ev.Kind != "tool_use" || ev.Tool != "Read" {
			continue
		}
		slug, ok := topicSlug(ev.Input["file_path"], prefix)
		if !ok {
			continue
		}
		ctx, ctxTS := contextWindow(evs, i)
		out = append(out, TopicReadEvent{
			Timestamp:          ctxTS,
			TranscriptID:       transcriptID,
			TopicSlug:          slug,
			TaskContextExcerpt: ctx,
			TriggerMatched:     triggerMatched(ctx, triggers[slug]),
			Fit:                FitUnknown,
		})
	}
	return out
}

// topicSlug returns the slug for a file_path directly under prefix
// (prefix + "<slug>.md"), or ok=false. Nested paths are rejected.
func topicSlug(fp, prefix string) (string, bool) {
	if !strings.HasPrefix(fp, prefix) {
		return "", false
	}
	rest := strings.TrimPrefix(fp, prefix)
	if strings.Contains(rest, "/") || !strings.HasSuffix(rest, ".md") {
		return "", false
	}
	return strings.TrimSuffix(rest, ".md"), true
}

// contextWindow gathers user text turns from the most recent user turn at or
// before index readIdx, up to (not including) the next user turn. Returns the
// joined text and the timestamp of the first bounding user turn.
func contextWindow(evs []transcript.Event, readIdx int) (string, string) {
	start := -1
	for j := readIdx; j >= 0; j-- {
		if evs[j].Kind == "text" && evs[j].Role == "user" {
			start = j
			break
		}
	}
	if start == -1 {
		return "", ""
	}
	ts := evs[start].Timestamp
	var parts []string
	for j := start; j < len(evs); j++ {
		if evs[j].Kind != "text" || evs[j].Role != "user" {
			continue
		}
		if j > start {
			break // next user turn ends the window
		}
		parts = append(parts, evs[j].Text)
	}
	return strings.Join(parts, "\n"), ts
}

// triggerMatched reports whether any trigger phrase appears (case-insensitive
// substring) in the task context.
func triggerMatched(ctx string, triggers []string) bool {
	lc := strings.ToLower(ctx)
	for _, t := range triggers {
		if t != "" && strings.Contains(lc, strings.ToLower(t)) {
			return true
		}
	}
	return false
}
