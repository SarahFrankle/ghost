# Chunk 1: Source Interface Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Introduce a `Source` interface in `internal/source/` and migrate callers off direct use of `internal/transcript/`, so future sources (opencode, codex, github, slack) can plug in without surgery across the pipeline.

**Architecture:** Pure refactor. No behavior change. `internal/transcript/` keeps its Claude Code wire-format code. New `internal/source/` package defines a `Source` interface (Discover / ContentHash / Parse) plus `Conversation` and `Turn` data types. One implementation, `ClaudeCodeSource`, wraps `transcript.Discover`, `transcript.ContentHash`, `transcript.Parse`. Callers in `cmd/` and `internal/extract/` switch to source types. Existing tests must continue to pass with no logic change.

**Tech Stack:** Go 1.x, standard library, existing `internal/transcript/` (untouched).

---

## File Structure

Files to create:
- `internal/source/source.go` — `Source` interface, `Conversation`, `Turn` types
- `internal/source/source_test.go` — interface compile check
- `internal/source/claude_code.go` — `ClaudeCodeSource` implementation
- `internal/source/claude_code_test.go` — adapter tests using a temp transcript

Files to modify:
- `internal/extract/extract.go` — switch `Run` signature from `transcript.Transcript` to `source.Conversation`; replace `transcript.Parse` call with `Source.Parse`
- `internal/extract/extract_test.go` — update fixtures to use `source.Conversation`
- `cmd/compose.go` — construct `source.ClaudeCode()`, call `src.Discover` / `src.ContentHash`
- `cmd/estimate.go` — same migration
- `cmd/status.go` — same migration

Files untouched (deliberate):
- `internal/transcript/*.go` — remains the Claude Code wire-format module. `ClaudeCodeSource` delegates to it. No moves, no renames.
- `internal/ledger/`, `internal/cluster/`, `internal/synthesize/`, `internal/canonicalize/` — they don't reference the transcript types.

---

## Interface Design

```go
package source

import (
    "context"
    "time"
)

// Conversation is the identity record for one unit of input.
// It is just metadata; I/O happens through the Source.
type Conversation struct {
    ID      string    // stable source-specific identifier (file path for ClaudeCode)
    Source  string    // source name, e.g. "claude-code"
    Project string    // optional cohort label (empty if N/A)
    ModTime time.Time // last-modified time used for active-window filtering
}

// Turn is one user/assistant message after parsing.
type Turn struct {
    Index int
    Role  string // "user" | "assistant" | "system" | "tool"
    Text  string
}

// Source is a pluggable conversation provider.
type Source interface {
    // Name returns a stable identifier used in Conversation.Source.
    Name() string

    // Discover lists conversations whose ModTime is older than now-activeWindow.
    // activeWindow=0 means no skip.
    Discover(ctx context.Context, activeWindow time.Duration, now time.Time) ([]Conversation, error)

    // ContentHash returns a stable content fingerprint, formatted "sha256:<hex>".
    ContentHash(ctx context.Context, c Conversation) (string, error)

    // Parse returns the conversation's turns, with non-text content blocks dropped.
    Parse(ctx context.Context, c Conversation) ([]Turn, error)
}
```

Rationale: `Discover` doesn't hash (matches current laziness — compose hashes only what passes the ledger filter). `ContentHash` and `Parse` take `context.Context` because future sources are network-bound.

---

## Task 1: Define Source interface and data types

**Files:**
- Create: `internal/source/source.go`
- Create: `internal/source/source_test.go`

- [ ] **Step 1.1: Write the failing test**

Create `internal/source/source_test.go`:

```go
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
```

- [ ] **Step 1.2: Run test to verify it fails**

Run: `go test ./internal/source/...`
Expected: FAIL — package does not exist yet.

- [ ] **Step 1.3: Write the interface and types**

Create `internal/source/source.go`:

```go
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
```

- [ ] **Step 1.4: Run test to verify it passes**

Run: `go test ./internal/source/...`
Expected: PASS (3 tests).

- [ ] **Step 1.5: Run the full test suite to confirm no breakage**

Run: `go test ./...`
Expected: all existing tests still pass (we haven't modified any callers yet).

- [ ] **Step 1.6: Commit**

```bash
git add internal/source/source.go internal/source/source_test.go
git commit -m "feat(source): introduce Source interface and Conversation/Turn types"
```

---

## Task 2: Implement ClaudeCodeSource

**Files:**
- Create: `internal/source/claude_code.go`
- Create: `internal/source/claude_code_test.go`

- [ ] **Step 2.1: Write the failing test**

Create `internal/source/claude_code_test.go`:

```go
package source

import (
    "context"
    "os"
    "path/filepath"
    "strings"
    "testing"
    "time"
)

func TestClaudeCode_Name(t *testing.T) {
    s := ClaudeCode("/tmp/**/*.jsonl")
    if s.Name() != "claude-code" {
        t.Fatalf("Name() = %q, want %q", s.Name(), "claude-code")
    }
}

func TestClaudeCode_DiscoverAndParse(t *testing.T) {
    dir := t.TempDir()
    projDir := filepath.Join(dir, "-Users-sarah-dev-projects-ghost")
    if err := os.Mkdir(projDir, 0o755); err != nil {
        t.Fatal(err)
    }
    path := filepath.Join(projDir, "session.jsonl")
    body := strings.Join([]string{
        `{"type":"user","message":{"role":"user","content":"hello"}}`,
        `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"hi"}]}}`,
    }, "\n") + "\n"
    if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
        t.Fatal(err)
    }
    // Backdate the file so the active-window filter doesn't skip it.
    past := time.Now().Add(-1 * time.Hour)
    if err := os.Chtimes(path, past, past); err != nil {
        t.Fatal(err)
    }

    s := ClaudeCode(filepath.Join(dir, "**", "*.jsonl"))
    ctx := context.Background()

    convs, err := s.Discover(ctx, 5*time.Minute, time.Now())
    if err != nil {
        t.Fatal(err)
    }
    if len(convs) != 1 {
        t.Fatalf("Discover returned %d, want 1", len(convs))
    }
    c := convs[0]
    if c.Source != "claude-code" {
        t.Errorf("Source = %q, want claude-code", c.Source)
    }
    if c.Project != "Users-sarah-dev-projects-ghost" {
        t.Errorf("Project = %q, want Users-sarah-dev-projects-ghost", c.Project)
    }
    if c.ID != path {
        t.Errorf("ID = %q, want %q", c.ID, path)
    }

    h, err := s.ContentHash(ctx, c)
    if err != nil {
        t.Fatal(err)
    }
    if !strings.HasPrefix(h, "sha256:") {
        t.Errorf("ContentHash = %q, want sha256: prefix", h)
    }

    turns, err := s.Parse(ctx, c)
    if err != nil {
        t.Fatal(err)
    }
    if len(turns) != 2 {
        t.Fatalf("Parse returned %d turns, want 2", len(turns))
    }
    if turns[0].Role != "user" || turns[0].Text != "hello" {
        t.Errorf("turn 0 = %+v", turns[0])
    }
    if turns[1].Role != "assistant" || turns[1].Text != "hi" {
        t.Errorf("turn 1 = %+v", turns[1])
    }
}

func TestClaudeCode_DiscoverSkipsActiveSessions(t *testing.T) {
    dir := t.TempDir()
    projDir := filepath.Join(dir, "-proj")
    if err := os.Mkdir(projDir, 0o755); err != nil {
        t.Fatal(err)
    }
    path := filepath.Join(projDir, "live.jsonl")
    if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
        t.Fatal(err)
    }
    // Fresh mtime (now) — should be filtered out by a 5m active window.

    s := ClaudeCode(filepath.Join(dir, "**", "*.jsonl"))
    convs, err := s.Discover(context.Background(), 5*time.Minute, time.Now())
    if err != nil {
        t.Fatal(err)
    }
    if len(convs) != 0 {
        t.Fatalf("Discover returned %d, want 0 (active window should skip)", len(convs))
    }
}
```

- [ ] **Step 2.2: Run test to verify it fails**

Run: `go test ./internal/source/...`
Expected: FAIL — `ClaudeCode` undefined.

- [ ] **Step 2.3: Write the ClaudeCodeSource implementation**

Create `internal/source/claude_code.go`:

```go
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
```

- [ ] **Step 2.4: Run test to verify it passes**

Run: `go test ./internal/source/...`
Expected: PASS (5 tests across both files).

- [ ] **Step 2.5: Run the full test suite to confirm no breakage**

Run: `go test ./...`
Expected: existing tests still pass.

- [ ] **Step 2.6: Commit**

```bash
git add internal/source/claude_code.go internal/source/claude_code_test.go
git commit -m "feat(source): add ClaudeCodeSource adapter over internal/transcript"
```

---

## Task 3: Migrate internal/extract to source types

Goal: change `extract.Runner.Run` from taking `transcript.Transcript` + `contentHash` to taking `source.Source` + `source.Conversation` + `contentHash`. The Source is needed because Run parses internally.

**Files:**
- Modify: `internal/extract/extract.go`
- Modify: `internal/extract/extract_test.go`

- [ ] **Step 3.1: Read the current test file to understand fixture shape**

Run: `cat internal/extract/extract_test.go | head -80`

Note the current `transcript.Transcript` construction sites — these need updating.

- [ ] **Step 3.2: Update extract_test.go to use source types**

Replace every `transcript.Transcript{...}` literal with `source.Conversation{...}`, every `transcript.Turn` with `source.Turn`. Where the test currently calls `runner.Run(ctx, t, hash)`, change to `runner.Run(ctx, src, c, hash)` and supply a fake Source.

Add this fake Source helper at the top of `internal/extract/extract_test.go`:

```go
type fakeSource struct {
    turns []source.Turn
}

func (fakeSource) Name() string { return "fake" }
func (fakeSource) Discover(ctx context.Context, w time.Duration, now time.Time) ([]source.Conversation, error) {
    return nil, nil
}
func (fakeSource) ContentHash(ctx context.Context, c source.Conversation) (string, error) {
    return "sha256:fake", nil
}
func (f fakeSource) Parse(ctx context.Context, c source.Conversation) ([]source.Turn, error) {
    return f.turns, nil
}
```

Update the imports block to include `"context"`, `"time"`, and `"github.com/SarahFrankle/ghost/internal/source"`. Remove the `internal/transcript` import if no longer used by tests.

- [ ] **Step 3.3: Run the test to verify it fails**

Run: `go test ./internal/extract/...`
Expected: FAIL — signature mismatch on `Run`, or compile error referencing undefined `Run(ctx, src, c, hash)`.

- [ ] **Step 3.4: Update extract.go signature**

In `internal/extract/extract.go`:

Change the import block: remove `"github.com/SarahFrankle/ghost/internal/transcript"`, add `"github.com/SarahFrankle/ghost/internal/source"`.

Change the `Run` signature:

```go
// Run extracts observations from one conversation. On success, returns
// a populated ObservationsFile. Malformed and secret-bearing observations
// are dropped (logged via r.Log).
func (r *Runner) Run(ctx context.Context, src source.Source, c source.Conversation, contentHash string) (ObservationsFile, error) {
    turns, err := src.Parse(ctx, c)
    if err != nil {
        return ObservationsFile{}, err
    }
    if !hasAssistantTurn(turns) {
        return ObservationsFile{
            Source:       c.ID,
            Project:      c.Project,
            ContentHash:  contentHash,
            ExtractedAt:  time.Now().UTC(),
            Observations: []Observation{},
        }, nil
    }

    userPayload := renderPayload(r.KnownTopics, turns)
    raw, err := r.Client.Complete(ctx, r.Model, SystemPrompt(), userPayload)
    if err != nil {
        return ObservationsFile{}, fmt.Errorf("anthropic: %w", err)
    }

    parsed, err := parseObservations(raw)
    if err != nil {
        return ObservationsFile{}, fmt.Errorf("parse model output: %w", err)
    }

    kept := make([]Observation, 0, len(parsed))
    for _, o := range parsed {
        if err := o.Validate(); err != nil {
            r.logf("drop: schema invalid: %v", err)
            continue
        }
        if hit, pat := secrets.Detect(o.Text); hit {
            r.logf("drop: secret pattern %s in text", pat)
            continue
        }
        if hit, pat := secrets.Detect(o.Evidence); hit {
            r.logf("drop: secret pattern %s in evidence", pat)
            continue
        }
        if isInjectedSource(o.Evidence) {
            r.logf("drop: evidence cites injected material, not a user turn: %q", o.Evidence)
            continue
        }
        kept = append(kept, o)
    }

    return ObservationsFile{
        Source:       c.ID,
        Project:      c.Project,
        ContentHash:  contentHash,
        ExtractedAt:  time.Now().UTC(),
        Observations: kept,
    }, nil
}
```

Change `hasAssistantTurn` and `renderPayload` parameter types from `[]transcript.Turn` to `[]source.Turn`:

```go
func hasAssistantTurn(turns []source.Turn) bool {
    for _, t := range turns {
        if t.Role == "assistant" {
            return true
        }
    }
    return false
}
```

And update `renderPayload`'s signature similarly (search for the other `transcript.Turn` reference in the file).

- [ ] **Step 3.5: Run the test to verify it passes**

Run: `go test ./internal/extract/...`
Expected: PASS.

- [ ] **Step 3.6: Run the full test suite — expect callers in cmd/ to break**

Run: `go test ./...`
Expected: `cmd/compose`, `cmd/estimate`, `cmd/status` fail to compile. That's expected — Task 4 fixes them.

- [ ] **Step 3.7: Commit**

```bash
git add internal/extract/extract.go internal/extract/extract_test.go
git commit -m "refactor(extract): take source.Source/Conversation instead of transcript types"
```

---

## Task 4: Migrate cmd/ callers to use source.ClaudeCode

**Files:**
- Modify: `cmd/compose.go`
- Modify: `cmd/estimate.go`
- Modify: `cmd/status.go`

- [ ] **Step 4.1: Read each file's current transcript usage**

Run: `grep -n "transcript\." cmd/compose.go cmd/estimate.go cmd/status.go`

Note the construction site for the glob — likely from `config.Sources.Glob` or similar.

- [ ] **Step 4.2: Update cmd/compose.go**

In `cmd/compose.go`:

Replace the import `"github.com/SarahFrankle/ghost/internal/transcript"` with `"github.com/SarahFrankle/ghost/internal/source"`.

Replace `transcripts, err := transcript.Discover(glob, 5*time.Minute, time.Now())` with:

```go
src := source.ClaudeCode(glob)
ctx := cmd.Context()
convs, err := src.Discover(ctx, 5*time.Minute, time.Now())
```

(If `cmd.Context()` is not in scope here, use `context.Background()` — match what the surrounding code already does; do not invent new context propagation.)

Replace `transcript.ContentHash(t.Path)` with `src.ContentHash(ctx, c)` where `c` is the loop variable. Rename loop variable from `t transcript.Transcript` to `c source.Conversation`. Update any `t.Path` → `c.ID`, `t.Project` → `c.Project`, `t.ModTime` → `c.ModTime`.

Update the call into extract.Runner to pass `src` and `c`:

```go
obs, err := runner.Run(ctx, src, c, contentHash)
```

- [ ] **Step 4.3: Update cmd/estimate.go**

Same pattern. Replace import. Replace `transcript.Discover(glob, 0, nowFn())` with `source.ClaudeCode(glob).Discover(ctx, 0, nowFn())`. Replace `transcript.ContentHash(t.Path)` with `src.ContentHash(ctx, c)`. Rename loop variable.

- [ ] **Step 4.4: Update cmd/status.go**

Same pattern. Replace import. Replace `transcript.Discover(glob, 5*time.Minute, time.Now())` with `source.ClaudeCode(glob).Discover(ctx, 5*time.Minute, time.Now())`. Replace `transcript.ContentHash(t.Path)` with `src.ContentHash(ctx, c)`.

- [ ] **Step 4.5: Verify build**

Run: `go build ./...`
Expected: clean build.

- [ ] **Step 4.6: Run the full test suite**

Run: `go test ./...`
Expected: PASS across all packages. Behavior is unchanged; this is a pure refactor.

- [ ] **Step 4.7: Smoke test against a real corpus**

Run: `go run . compose --limit 1 --stages extract`
Expected: identical output to pre-refactor — one observation file under `.state/observations/` for a recent transcript. Compare against `git stash` baseline if uncertain.

- [ ] **Step 4.8: Commit**

```bash
git add cmd/compose.go cmd/estimate.go cmd/status.go
git commit -m "refactor(cmd): use source.ClaudeCode instead of transcript directly"
```

---

## Self-Review Checklist

- [ ] **Spec coverage:** Every item in `docs/specs/2026-05-22-findings.md` chunk 1 description is implemented:
  - Source interface defined ✓ (Task 1)
  - One implementation, ClaudeCodeSource ✓ (Task 2)
  - No CLI changes ✓ (cmd/ commands still take same flags, produce same output)
  - No cache layout changes ✓ (ledger and observation file format untouched)
- [ ] **No placeholders:** Every code block contains real code, every command has expected output.
- [ ] **Type consistency:** `source.Source`, `source.Conversation`, `source.Turn` used uniformly. Field names `ID` / `Source` / `Project` / `ModTime` consistent across files.
- [ ] **No behavior change:** All existing tests pass without modification to their assertions (only fixture types change).

## Out of Scope

These belong to later chunks; do **not** include in this PR:
- Fingerprinting of derived artifacts (chunk 2)
- Embedding-based topic clustering (chunk 3)
- CLI flattening / `--stages` removal (chunk 4)
- README updates (chunk 5)
- Deleting `internal/transcript/` (stays as the wire-format implementation)
- Adding a second source (opencode, codex, etc.)
- Per-source observe prompts

## Verification Done

When all tasks check off and `go run . compose --limit 1 --stages extract` produces identical output to baseline, chunk 1 is complete.
