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
			Line:               ev.Line,
			TopicSlug:          slug,
			TaskContextExcerpt: ctx,
			TriggerMatched:     triggerMatched(ctx, triggers[slug]),
			Fit:                FitUnknown,
		})
	}
	return out
}

// NewEventsSince keeps only events whose source line is strictly greater than
// scannedLines, and returns the max line seen (for advancing the ledger).
func NewEventsSince(evs []TopicReadEvent, scannedLines int) (kept []TopicReadEvent, maxLine int) {
	maxLine = scannedLines
	for _, e := range evs {
		if e.Line > maxLine {
			maxLine = e.Line
		}
		if e.Line > scannedLines {
			kept = append(kept, e)
		}
	}
	return kept, maxLine
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

// isSkillInjection reports whether a user-role text turn is actually Claude
// Code injecting an invoked skill's body (not the user's task). Such turns
// open with this fixed preamble.
func isSkillInjection(text string) bool {
	return strings.HasPrefix(strings.TrimSpace(text), "Base directory for this skill:")
}

// isUserText reports whether ev is a genuine user task turn: user-role text
// that is not a Claude Code skill-body injection.
func isUserText(ev transcript.Event) bool {
	return ev.Role == "user" && ev.Kind == "text" && !isSkillInjection(ev.Text)
}

// contextWindow gathers all contiguous user-text events of the bounding user
// turn at or before readIdx. It walks backward to find the first genuine
// user-text event, then forward collecting consecutive user-text events,
// stopping at the first event that is not a user-text event (the assistant
// turn ends the user turn). Skill-injection turns (the skill body Claude Code
// injects as a user-role message) are skipped so the genuine user request is
// captured instead. Returns the joined text and the timestamp of the first
// event found.
func contextWindow(evs []transcript.Event, readIdx int) (string, string) {
	// Walk backward to find the most recent genuine user-text event, then
	// continue backward to find the start of that contiguous user-text block.
	start := -1
	for j := readIdx; j >= 0; j-- {
		if isUserText(evs[j]) {
			start = j
			break
		}
	}
	if start == -1 {
		return "", ""
	}
	// Extend start backward through any additional contiguous user-text events.
	for start > 0 && isUserText(evs[start-1]) {
		start--
	}
	ts := evs[start].Timestamp
	var parts []string
	for j := start; j < len(evs); j++ {
		if !isUserText(evs[j]) {
			break // first non-(user text) event ends the user turn
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
