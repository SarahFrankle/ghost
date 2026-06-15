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
	out, _ := DetectTopicReadsWithLines(transcriptID, evs, topicsDir, triggers)
	return out
}

// DetectTopicReadsWithLines is DetectTopicReads plus the JSONL line number each
// returned event came from (parallel slice), so the audit can resume by line.
func DetectTopicReadsWithLines(transcriptID string, evs []transcript.Event, topicsDir string, triggers map[string][]string) ([]TopicReadEvent, []int) {
	prefix := strings.TrimSuffix(topicsDir, "/") + "/"
	var out []TopicReadEvent
	var lines []int
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
		lines = append(lines, ev.Line)
	}
	return out, lines
}

// NewEventsSince keeps only events whose source line is strictly greater than
// scannedLines, and returns the max line seen (for advancing the ledger).
func NewEventsSince(evs []TopicReadEvent, lines []int, scannedLines int) (kept []TopicReadEvent, maxLine int) {
	maxLine = scannedLines
	for i, e := range evs {
		if lines[i] > maxLine {
			maxLine = lines[i]
		}
		if lines[i] > scannedLines {
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

// contextWindow gathers all contiguous user-text events of the bounding user
// turn at or before readIdx. It walks backward to find the first user-text
// event, then forward collecting consecutive user-text events, stopping at the
// first event that is not a user-text event (the assistant turn ends the user
// turn). Returns the joined text and the timestamp of the first event found.
func contextWindow(evs []transcript.Event, readIdx int) (string, string) {
	// Walk backward to find the most recent user-text event, then continue
	// backward to find the start of that contiguous user-text block.
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
	// Extend start backward through any additional contiguous user-text events.
	for start > 0 && evs[start-1].Role == "user" && evs[start-1].Kind == "text" {
		start--
	}
	ts := evs[start].Timestamp
	var parts []string
	for j := start; j < len(evs); j++ {
		if evs[j].Role == "user" && evs[j].Kind == "text" {
			parts = append(parts, evs[j].Text)
			continue
		}
		break // first non-(user text) event ends the user turn
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
