package effectiveness

import (
	"testing"

	"github.com/SarahFrankle/ghost/internal/transcript"
)

func TestDetectTopicReads_WindowAndGate(t *testing.T) {
	topicsDir := "/Users/sarah/.ghost/topics"
	triggers := map[string][]string{
		"design-architecture": {"refactor", "architecture"},
		"testing-discipline":  {"unit test"},
	}
	evs := []transcript.Event{
		{Line: 1, Timestamp: "T0", Role: "user", Kind: "text", Text: "help me refactor this package"},
		{Line: 2, Role: "assistant", Kind: "tool_use", Tool: "Read", Input: map[string]string{"file_path": topicsDir + "/design-architecture.md"}},
		{Line: 3, Role: "assistant", Kind: "text", Text: "sure"},
		{Line: 4, Timestamp: "T1", Role: "user", Kind: "text", Text: "now add a feature"},
		{Line: 5, Role: "assistant", Kind: "tool_use", Tool: "Read", Input: map[string]string{"file_path": topicsDir + "/testing-discipline.md"}},
		{Line: 6, Role: "assistant", Kind: "tool_use", Tool: "Read", Input: map[string]string{"file_path": "/etc/passwd"}},
	}

	got := DetectTopicReads("conv-1", evs, topicsDir, triggers)
	if len(got) != 2 {
		t.Fatalf("want 2 topic reads (non-topic Read ignored), got %d: %+v", len(got), got)
	}

	if got[0].TopicSlug != "design-architecture" || !got[0].TriggerMatched {
		t.Errorf("got[0] = %+v (want design-architecture, trigger matched)", got[0])
	}
	if got[0].TaskContextExcerpt != "help me refactor this package" {
		t.Errorf("got[0] context = %q", got[0].TaskContextExcerpt)
	}
	if got[0].Timestamp != "T0" {
		t.Errorf("got[0] ts = %q (should carry the bounding user turn ts)", got[0].Timestamp)
	}
	if got[0].Fit != FitUnknown {
		t.Errorf("got[0] fit = %q (gate stage leaves fit unknown)", got[0].Fit)
	}

	if got[1].TopicSlug != "testing-discipline" || got[1].TriggerMatched {
		t.Errorf("got[1] = %+v (want testing-discipline, trigger NOT matched)", got[1])
	}
	if got[1].TaskContextExcerpt != "now add a feature" {
		t.Errorf("got[1] context = %q", got[1].TaskContextExcerpt)
	}
}

func TestContextWindow_MultiBlockUserTurn(t *testing.T) {
	topicsDir := "/Users/sarah/.ghost/topics"
	evs := []transcript.Event{
		{Line: 1, Timestamp: "T0", Role: "user", Kind: "text", Text: "first block"},
		{Line: 1, Timestamp: "T0", Role: "user", Kind: "text", Text: "second block"},
		{Line: 2, Role: "assistant", Kind: "tool_use", Tool: "Read", Input: map[string]string{"file_path": topicsDir + "/git.md"}},
	}
	got := DetectTopicReads("c", evs, topicsDir, map[string][]string{})
	if len(got) != 1 {
		t.Fatalf("want 1, got %d", len(got))
	}
	if got[0].TaskContextExcerpt != "first block\nsecond block" {
		t.Errorf("context = %q (want both blocks joined)", got[0].TaskContextExcerpt)
	}
}

func TestContextWindow_SkipsSkillInjection(t *testing.T) {
	topicsDir := "/Users/sarah/.ghost/topics"
	evs := []transcript.Event{
		{Line: 1, Timestamp: "T0", Role: "user", Kind: "text", Text: "help me debug this crash"},
		{Line: 2, Role: "assistant", Kind: "text", Text: "I'll investigate"},
		{Line: 3, Role: "assistant", Kind: "tool_use", Tool: "Skill", Input: map[string]string{}},
		{Line: 4, Role: "user", Kind: "text", Text: "Base directory for this skill: /Users/sarah/.claude/skills/ghost\n# Ghost. Lazy-load topic guidance"},
		{Line: 5, Role: "assistant", Kind: "tool_use", Tool: "Read", Input: map[string]string{"file_path": topicsDir + "/debugging-and-troubleshooting.md"}},
	}
	got := DetectTopicReads("c", evs, topicsDir, map[string][]string{"debugging-and-troubleshooting": {"debug"}})
	if len(got) != 1 {
		t.Fatalf("want 1, got %d", len(got))
	}
	if got[0].TaskContextExcerpt != "help me debug this crash" {
		t.Errorf("context = %q (want the real user request, not the skill body)", got[0].TaskContextExcerpt)
	}
	if !got[0].TriggerMatched {
		t.Errorf("trigger should match 'debug' in the real request")
	}
	if got[0].Timestamp != "T0" {
		t.Errorf("ts=%q want T0", got[0].Timestamp)
	}
}

func TestNewEventsSince(t *testing.T) {
	evs := []TopicReadEvent{
		{TopicSlug: "a"}, {TopicSlug: "b"}, {TopicSlug: "c"},
	}
	lines := []int{2, 5, 9}
	kept, maxLine := NewEventsSince(evs, lines, 5)
	if len(kept) != 1 || kept[0].TopicSlug != "c" {
		t.Fatalf("want only 'c' (line 9 > 5), got %+v", kept)
	}
	if maxLine != 9 {
		t.Fatalf("maxLine = %d, want 9", maxLine)
	}
}
