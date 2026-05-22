package source

import (
	"context"
	"time"

	"github.com/SarahFrankle/ghost/internal/transcript"
)

// ClaudeCode returns a Source that reads Claude Code transcript files
// matching glob (doublestar syntax supported). It delegates I/O to
// internal/transcript so the wire-format code has exactly one home.
func ClaudeCode(glob string) Source {
	return &claudeCodeSource{glob: glob}
}

type claudeCodeSource struct {
	glob string
}

func (s *claudeCodeSource) Name() string { return "claude-code" }

func (s *claudeCodeSource) Discover(ctx context.Context, activeWindow time.Duration, now time.Time) ([]Conversation, error) {
	ts, err := transcript.Discover(s.glob, activeWindow, now)
	if err != nil {
		return nil, err
	}
	out := make([]Conversation, 0, len(ts))
	for _, t := range ts {
		out = append(out, Conversation{
			ID:      t.Path,
			Source:  s.Name(),
			Project: t.Project,
			ModTime: t.ModTime,
		})
	}
	return out, nil
}

func (s *claudeCodeSource) ContentHash(ctx context.Context, c Conversation) (string, error) {
	return transcript.ContentHash(c.ID)
}

func (s *claudeCodeSource) Parse(ctx context.Context, c Conversation) ([]Turn, error) {
	raw, err := transcript.Parse(c.ID)
	if err != nil {
		return nil, err
	}
	out := make([]Turn, 0, len(raw))
	for _, t := range raw {
		out = append(out, Turn{Index: t.Index, Role: t.Role, Text: t.Text})
	}
	return out, nil
}
