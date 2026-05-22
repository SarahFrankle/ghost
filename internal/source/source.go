// Package source defines a pluggable conversation provider abstraction.
//
// A Source produces Conversations (identity records) and supplies I/O
// (content hashing, parsing) on demand. The pipeline depends only on this
// package; concrete providers live in sibling files (claude_code.go, etc.).
package source

import (
	"context"
	"time"
)

// Conversation is the identity record for one unit of input.
type Conversation struct {
	ID      string
	Source  string
	Project string
	ModTime time.Time
}

// NewConversation builds a Conversation with Source stamped from src.Name(),
// so callers cannot accidentally diverge the field from the producing source.
func NewConversation(src Source, id, project string, modTime time.Time) Conversation {
	return Conversation{ID: id, Source: src.Name(), Project: project, ModTime: modTime}
}

// Turn is one user/assistant message after parsing.
type Turn struct {
	Index int
	Role  string
	Text  string
}

// Source is a pluggable conversation provider.
type Source interface {
	Name() string
	Discover(ctx context.Context, activeWindow time.Duration, now time.Time) ([]Conversation, error)
	ContentHash(ctx context.Context, c Conversation) (string, error)
	Parse(ctx context.Context, c Conversation) ([]Turn, error)
}
