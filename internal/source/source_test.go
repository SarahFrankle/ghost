package source

import (
	"context"
	"testing"
	"time"
)

// Compile-time check that Source has the expected method set.
// If this file compiles, the interface signature is correct.
var _ Source = (*fakeSource)(nil)

type fakeSource struct{}

func (fakeSource) Name() string { return "fake" }
func (fakeSource) Discover(ctx context.Context, w time.Duration, now time.Time) ([]Conversation, error) {
	return nil, nil
}
func (fakeSource) ContentHash(ctx context.Context, c Conversation) (string, error) {
	return "", nil
}
func (fakeSource) Parse(ctx context.Context, c Conversation) ([]Turn, error) {
	return nil, nil
}

func TestConversationFields(t *testing.T) {
	c := Conversation{
		ID:      "/tmp/foo.jsonl",
		Source:  "claude-code",
		Project: "ghost",
		ModTime: time.Unix(0, 0),
	}
	if c.ID == "" || c.Source == "" {
		t.Fatal("Conversation fields not populated")
	}
}

func TestTurnFields(t *testing.T) {
	tr := Turn{Index: 0, Role: "user", Text: "hi"}
	if tr.Role != "user" || tr.Text != "hi" {
		t.Fatal("Turn fields not populated")
	}
}
