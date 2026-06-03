package synthesize

import (
	"context"
	"strings"
	"testing"
)

func TestBuildIndexFromTopicResults(t *testing.T) {
	f := &fakeClient{resp: "# Index\n\n## Topics\n- topics/testing.md (triggers: tests, pytest)\n- topics/git.md (triggers: rebase, branch)\n"}
	topics := []TopicResult{
		{Slug: "testing", Title: "Testing", Body: "# Testing\n\n- prefer tables.\n", EvidenceTotal: 4},
		{Slug: "git", Title: "Git", Body: "# Git\n\n- rebase before merge.\n", EvidenceTotal: 5},
	}
	r := BuildIndex(context.Background(), f, "smart", topics)
	if r.Err != nil {
		t.Fatalf("BuildIndex: %v", r.Err)
	}
	if !strings.Contains(r.Content, "topics/testing.md") {
		t.Fatalf("index missing testing link: %q", r.Content)
	}
}

func TestBuildIndexEmptyTopics(t *testing.T) {
	f := &fakeClient{resp: "should not be called"}
	r := BuildIndex(context.Background(), f, "smart", nil)
	if r.Err != nil {
		t.Fatalf("BuildIndex: %v", r.Err)
	}
	if !strings.Contains(r.Content, "No lazy-loaded topics yet.") {
		t.Fatalf("expected empty-state index, got %q", r.Content)
	}
}
