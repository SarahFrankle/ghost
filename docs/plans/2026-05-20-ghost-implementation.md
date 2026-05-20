# Ghost Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a Go CLI plus a Claude Code skill that synthesizes a profile, rule set, and lazy-loaded topic library from Claude Code transcripts.

**Architecture:** Go binary owns an out-of-band 4-stage LLM pipeline (extract → cluster → synthesize → refine) with a hashed ledger for resumable batching. Output is a small always-loaded core (profile + rules + index) plus on-demand topic files. A Claude Code skill enforces lazy-loading at runtime.

**Tech Stack:** Go 1.22+, `github.com/spf13/cobra` (CLI), `github.com/anthropics/anthropic-sdk-go`, `github.com/BurntSushi/toml`, stdlib for everything else. Tests use `testing` + `testify/assert`.

**Spec:** `docs/specs/2026-05-20-ghost-design.md`

---

## Task 1: Project scaffold + CLI skeleton

**Files:**
- Create: `go.mod`, `cmd/ghost/main.go`, `cmd/ghost/version.go`, `.gitignore`

- [ ] **Step 1: Init module**

```bash
cd /Users/sarah/dev/projects/ghost
go mod init github.com/sfrankle/ghost
go get github.com/spf13/cobra@latest github.com/BurntSushi/toml@latest github.com/stretchr/testify@latest github.com/anthropics/anthropic-sdk-go@latest
```

- [ ] **Step 2: Write the failing test**

Create `cmd/ghost/main_test.go`:

```go
package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRootHelp(t *testing.T) {
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--help"})
	err := cmd.Execute()
	assert.NoError(t, err)
	assert.Contains(t, out.String(), "ghost")
	assert.Contains(t, out.String(), "compose")
}
```

- [ ] **Step 3: Run, expect failure**

```bash
go test ./cmd/ghost/...
```
Expected: FAIL — `newRootCmd` undefined.

- [ ] **Step 4: Implement scaffold**

`cmd/ghost/main.go`:

```go
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ghost",
		Short: "Synthesize a profile and rule set from Claude Code transcripts",
	}
	cmd.AddCommand(newComposeCmd(), newShowCmd(), newStatusCmd())
	return cmd
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
```

`cmd/ghost/compose.go`, `show.go`, `status.go` each get a one-line stub:

```go
package main

import "github.com/spf13/cobra"

func newComposeCmd() *cobra.Command {
	return &cobra.Command{Use: "compose", Short: "Run the synthesis pipeline"}
}
```

(Same shape for `show` and `status`.)

`.gitignore`:

```
/ghost
*.test
.DS_Store
```

- [ ] **Step 5: Run tests, expect pass**

```bash
go test ./... && go build ./cmd/ghost && ./ghost --help
```

- [ ] **Step 6: Commit**

```bash
git add . && git commit -m "feat: cli scaffold with cobra"
```

---

## Task 2: Config layer

**Files:**
- Create: `internal/config/config.go`, `internal/config/config_test.go`, `internal/config/defaults.go`

- [ ] **Step 1: Write the failing test**

`internal/config/config_test.go`:

```go
package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultsWhenNoFile(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "nope.toml"))
	require.NoError(t, err)
	assert.Equal(t, "claude-haiku-4-5-20251001", cfg.Models.Cheap)
	assert.Equal(t, "claude-opus-4-7", cfg.Models.Smart)
	assert.Equal(t, 2, cfg.Thresholds.RuleMinEvidenceCount)
	assert.Equal(t, 2, cfg.Thresholds.RuleMinProjectCount)
}

func TestUserConfigOverrides(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	require.NoError(t, os.WriteFile(path, []byte(`
[models]
cheap = "haiku-x"

[thresholds]
rule_min_evidence_count = 5
`), 0o644))

	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, "haiku-x", cfg.Models.Cheap)
	assert.Equal(t, "claude-opus-4-7", cfg.Models.Smart) // unchanged
	assert.Equal(t, 5, cfg.Thresholds.RuleMinEvidenceCount)
	assert.Equal(t, 2, cfg.Thresholds.RuleMinProjectCount) // unchanged
}
```

- [ ] **Step 2: Run, expect fail**

```bash
go test ./internal/config/...
```

- [ ] **Step 3: Implement**

`internal/config/config.go`:

```go
package config

import (
	"errors"
	"io/fs"
	"os"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Models     Models     `toml:"models"`
	Thresholds Thresholds `toml:"thresholds"`
	Paths      Paths      `toml:"paths"`
	Batching   Batching   `toml:"batching"`
}

type Models struct {
	Cheap string `toml:"cheap"`
	Smart string `toml:"smart"`
}

type Thresholds struct {
	RuleMinEvidenceCount int `toml:"rule_min_evidence_count"`
	RuleMinProjectCount  int `toml:"rule_min_project_count"`
}

type Paths struct {
	TranscriptsGlob string `toml:"transcripts_glob"`
	OutputDir       string `toml:"output_dir"`
}

type Batching struct {
	DefaultLimit int `toml:"default_limit"`
}

func Load(path string) (Config, error) {
	cfg := Defaults()
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}
```

`internal/config/defaults.go`:

```go
package config

func Defaults() Config {
	return Config{
		Models: Models{
			Cheap: "claude-haiku-4-5-20251001",
			Smart: "claude-opus-4-7",
		},
		Thresholds: Thresholds{
			RuleMinEvidenceCount: 2,
			RuleMinProjectCount:  2,
		},
		Paths: Paths{
			TranscriptsGlob: "~/.claude/projects/**/*.jsonl",
			OutputDir:       "~/.ghost",
		},
		Batching: Batching{DefaultLimit: 0},
	}
}
```

Note: TOML unmarshal merges into the pre-populated struct, so any field not in the user's TOML keeps its default. Verify the test for `Smart` unchanged confirms this.

- [ ] **Step 4: Run, expect pass; commit**

```bash
go test ./internal/config/... && git add . && git commit -m "feat: config loading with defaults"
```

---

## Task 3: Transcript discovery, parsing, and hashing

**Files:**
- Create: `internal/transcripts/transcripts.go`, `internal/transcripts/transcripts_test.go`, `internal/transcripts/testdata/sample.jsonl`

- [ ] **Step 1: Add fixture**

`internal/transcripts/testdata/sample.jsonl`:

```jsonl
{"type":"user","message":{"role":"user","content":"hello"}}
{"type":"assistant","message":{"role":"assistant","content":"hi"}}
{"type":"user","message":{"role":"user","content":"don't mock the database"}}
```

- [ ] **Step 2: Write the failing test**

`internal/transcripts/transcripts_test.go`:

```go
package transcripts

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	tr, err := Parse("testdata/sample.jsonl")
	require.NoError(t, err)
	assert.Len(t, tr.Messages, 3)
	assert.Equal(t, "user", tr.Messages[0].Role)
	assert.Contains(t, tr.Messages[2].Content, "don't mock")
}

func TestContentHashStable(t *testing.T) {
	a, err := Parse("testdata/sample.jsonl")
	require.NoError(t, err)
	b, err := Parse("testdata/sample.jsonl")
	require.NoError(t, err)
	assert.Equal(t, a.ContentHash, b.ContentHash)
	assert.True(t, len(a.ContentHash) > 16)
}

func TestProjectFromPath(t *testing.T) {
	// Claude Code encodes cwd as the directory name under ~/.claude/projects/
	got := ProjectFromPath("/Users/x/.claude/projects/-Users-x-dev-repoA/abc.jsonl")
	assert.Equal(t, "Users-x-dev-repoA", got)
}

func TestDiscover(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, copyFile("testdata/sample.jsonl", filepath.Join(dir, "a.jsonl")))
	require.NoError(t, copyFile("testdata/sample.jsonl", filepath.Join(dir, "b.jsonl")))
	paths, err := Discover(filepath.Join(dir, "*.jsonl"))
	require.NoError(t, err)
	assert.Len(t, paths, 2)
}
```

Add `copyFile` helper in the same `_test.go`.

- [ ] **Step 3: Run, expect fail; then implement**

`internal/transcripts/transcripts.go`:

```go
package transcripts

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type Transcript struct {
	Path        string
	Project     string
	ContentHash string
	Messages    []Message
}

type Message struct {
	Role    string
	Content string
}

type rawLine struct {
	Type    string `json:"type"`
	Message struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

func Parse(path string) (Transcript, error) {
	f, err := os.Open(path)
	if err != nil {
		return Transcript{}, err
	}
	defer f.Close()

	h := sha256.New()
	tr := Transcript{Path: path, Project: ProjectFromPath(path)}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		h.Write(line)
		h.Write([]byte{'\n'})
		var r rawLine
		if err := json.Unmarshal(line, &r); err != nil {
			continue // tolerate non-message lines
		}
		if r.Message.Role == "" {
			continue
		}
		tr.Messages = append(tr.Messages, Message{
			Role:    r.Message.Role,
			Content: flatten(r.Message.Content),
		})
	}
	if err := scanner.Err(); err != nil {
		return tr, err
	}
	tr.ContentHash = "sha256:" + hex.EncodeToString(h.Sum(nil))
	return tr, nil
}

func flatten(raw json.RawMessage) string {
	// Claude Code content may be a string or a structured array.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var arr []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &arr); err == nil {
		var parts []string
		for _, p := range arr {
			if p.Type == "text" {
				parts = append(parts, p.Text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return string(raw)
}

func ProjectFromPath(path string) string {
	dir := filepath.Base(filepath.Dir(path))
	return strings.TrimPrefix(dir, "-")
}

func Discover(glob string) ([]string, error) {
	expanded, err := expandHome(glob)
	if err != nil {
		return nil, err
	}
	return filepath.Glob(expanded)
}

func expandHome(p string) (string, error) {
	if !strings.HasPrefix(p, "~/") {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p, err
	}
	return filepath.Join(home, p[2:]), nil
}
```

Note: `filepath.Glob` doesn't support `**`. For the real glob, use a small recursive walk — add a helper that walks a base dir matching extension. Spec says `**/*.jsonl`; replace `filepath.Glob` with:

```go
func Discover(pattern string) ([]string, error) {
	expanded, err := expandHome(pattern)
	if err != nil {
		return nil, err
	}
	base, ext := splitGlobBase(expanded) // base = before `**`, ext = file ext after
	var out []string
	err = filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(p, ext) {
			out = append(out, p)
		}
		return nil
	})
	return out, err
}

func splitGlobBase(pattern string) (base, ext string) {
	if i := strings.Index(pattern, "**"); i >= 0 {
		base = strings.TrimSuffix(pattern[:i], "/")
		rest := pattern[i:]
		if j := strings.LastIndex(rest, "."); j >= 0 {
			ext = rest[j:]
		}
		return
	}
	return filepath.Dir(pattern), filepath.Ext(pattern)
}
```

Add imports: `io/fs`.

- [ ] **Step 4: Run tests; commit**

```bash
go test ./internal/transcripts/... && git add . && git commit -m "feat: transcript discovery, parsing, hashing, project tagging"
```

---

## Task 4: Ledger

**Files:**
- Create: `internal/ledger/ledger.go`, `internal/ledger/ledger_test.go`

- [ ] **Step 1: Write the failing test**

```go
package ledger

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.json")
	l := New()
	l.MarkProcessed("/p/a.jsonl", Entry{
		ContentHash:      "sha256:abc",
		ProcessedAt:      time.Now().UTC(),
		ObservationsFile: ".state/observations/a.json",
		MessageCount:     12,
	})
	require.NoError(t, l.Save(path))

	got, err := Load(path)
	require.NoError(t, err)
	e, ok := got.Get("/p/a.jsonl")
	require.True(t, ok)
	assert.Equal(t, "sha256:abc", e.ContentHash)
	assert.Equal(t, 12, e.MessageCount)
}

func TestStatusBuckets(t *testing.T) {
	l := New()
	l.MarkProcessed("/p/a.jsonl", Entry{ContentHash: "sha256:old"})

	known := map[string]string{
		"/p/a.jsonl": "sha256:new", // dirty: hash changed
		"/p/b.jsonl": "sha256:b",   // pending: not in ledger
	}
	s := l.Status(known)
	assert.Equal(t, 1, s.Processed)
	assert.Equal(t, 1, s.Pending)
	assert.Equal(t, 1, s.Dirty)
}
```

- [ ] **Step 2: Implement**

`internal/ledger/ledger.go`:

```go
package ledger

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

type Entry struct {
	ContentHash      string    `json:"content_hash"`
	ProcessedAt      time.Time `json:"processed_at"`
	ObservationsFile string    `json:"observations_file"`
	MessageCount     int       `json:"message_count"`
}

type LastCompose struct {
	At         time.Time `json:"at"`
	StagesRun  []string  `json:"stages_run"`
}

type Ledger struct {
	Conversations map[string]Entry `json:"conversations"`
	LastCompose   LastCompose      `json:"last_compose"`
}

func New() *Ledger {
	return &Ledger{Conversations: map[string]Entry{}}
}

func Load(path string) (*Ledger, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return New(), nil
	}
	if err != nil {
		return nil, err
	}
	l := New()
	if err := json.Unmarshal(data, l); err != nil {
		return nil, err
	}
	if l.Conversations == nil {
		l.Conversations = map[string]Entry{}
	}
	return l, nil
}

func (l *Ledger) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path) // atomic
}

func (l *Ledger) Get(path string) (Entry, bool) {
	e, ok := l.Conversations[path]
	return e, ok
}

func (l *Ledger) MarkProcessed(path string, e Entry) {
	if e.ProcessedAt.IsZero() {
		e.ProcessedAt = time.Now().UTC()
	}
	l.Conversations[path] = e
}

type StatusReport struct {
	Processed int
	Pending   int
	Dirty     int
}

// Status compares known transcripts (path -> current hash) against the ledger.
func (l *Ledger) Status(known map[string]string) StatusReport {
	r := StatusReport{}
	for path, currentHash := range known {
		e, ok := l.Conversations[path]
		switch {
		case !ok:
			r.Pending++
		case e.ContentHash != currentHash:
			r.Processed++
			r.Dirty++
		default:
			r.Processed++
		}
	}
	return r
}
```

- [ ] **Step 3: Run, commit**

```bash
go test ./internal/ledger/... && git add . && git commit -m "feat: ledger with atomic save and dirty detection"
```

---

## Task 5: Anthropic client wrapper

**Files:**
- Create: `internal/anthropic/client.go`, `internal/anthropic/fake.go`, `internal/anthropic/client_test.go`

The wrapper hides the SDK behind a small interface so every downstream stage is testable without API calls.

- [ ] **Step 1: Write the failing test**

```go
package anthropic

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFakeRecordsAndReturns(t *testing.T) {
	f := NewFake().With("cheap", "hello world")
	out, err := f.Complete(context.Background(), Request{Role: "cheap", User: "hi"})
	require.NoError(t, err)
	assert.Equal(t, "hello world", out)
	assert.Len(t, f.Calls(), 1)
	assert.Equal(t, "hi", f.Calls()[0].User)
}

func TestFakeErrorsOnUnmappedRole(t *testing.T) {
	f := NewFake()
	_, err := f.Complete(context.Background(), Request{Role: "smart"})
	assert.Error(t, err)
}
```

- [ ] **Step 2: Implement interface, real client, and fake**

`internal/anthropic/client.go`:

```go
package anthropic

import (
	"context"
	"fmt"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

type Request struct {
	Role        string // "cheap" or "smart"
	System      string
	User        string
	CacheSystem bool
}

type Client interface {
	Complete(ctx context.Context, req Request) (string, error)
}

type Models struct {
	Cheap string
	Smart string
}

type real struct {
	c      *sdk.Client
	models Models
}

func New(apiKey string, models Models) Client {
	c := sdk.NewClient(option.WithAPIKey(apiKey))
	return &real{c: &c, models: models}
}

func (r *real) Complete(ctx context.Context, req Request) (string, error) {
	model, err := r.modelFor(req.Role)
	if err != nil {
		return "", err
	}
	sys := []sdk.TextBlockParam{{Text: req.System}}
	if req.CacheSystem {
		sys[0].CacheControl = sdk.CacheControlEphemeralParam{Type: "ephemeral"}
	}
	resp, err := r.c.Messages.New(ctx, sdk.MessageNewParams{
		Model:     sdk.F(model),
		MaxTokens: sdk.F[int64](8192),
		System:    sdk.F(sys),
		Messages: sdk.F([]sdk.MessageParam{
			sdk.NewUserMessage(sdk.NewTextBlock(req.User)),
		}),
	})
	if err != nil {
		return "", err
	}
	if len(resp.Content) == 0 {
		return "", fmt.Errorf("empty response")
	}
	return resp.Content[0].Text, nil
}

func (r *real) modelFor(role string) (string, error) {
	switch role {
	case "cheap":
		return r.models.Cheap, nil
	case "smart":
		return r.models.Smart, nil
	default:
		return "", fmt.Errorf("unknown role: %q", role)
	}
}
```

> If the SDK API surface differs from the snippet above (the SDK evolves), translate field names; the test against the `Client` interface is what matters.

`internal/anthropic/fake.go`:

```go
package anthropic

import (
	"context"
	"fmt"
)

type Fake struct {
	responses map[string]string
	calls     []Request
}

func NewFake() *Fake {
	return &Fake{responses: map[string]string{}}
}

func (f *Fake) With(role, response string) *Fake {
	f.responses[role] = response
	return f
}

func (f *Fake) Complete(ctx context.Context, req Request) (string, error) {
	f.calls = append(f.calls, req)
	resp, ok := f.responses[req.Role]
	if !ok {
		return "", fmt.Errorf("fake has no response for role %q", req.Role)
	}
	return resp, nil
}

func (f *Fake) Calls() []Request { return f.calls }
```

- [ ] **Step 3: Run, commit**

```bash
go test ./internal/anthropic/... && git add . && git commit -m "feat: anthropic client interface with fake for tests"
```

---

## Task 6: Stage 1 — extract

**Files:**
- Create: `internal/extract/extract.go`, `internal/extract/extract_test.go`, `prompts/extract.md`

- [ ] **Step 1: Write the prompt**

`prompts/extract.md`:

```markdown
You are extracting atomic observations from a Claude Code transcript.

For each piece of evidence in the transcript, output one JSON observation. Categorize each as:
- "rule": an explicit do/don't preference the user expressed
- "voice": stylistic patterns in how the user writes
- "topic": domain-specific guidance scoped to a subject area
- "identity": role, expertise, perspective, organizational context

Each observation must cite evidence (turn number and a short quote).

Output a single JSON object: {"observations": [ ... ]}.

Be conservative. Do not infer rules from absence. Skip noise.
```

- [ ] **Step 2: Write the failing test**

`internal/extract/extract_test.go`:

```go
package extract

import (
	"context"
	"testing"

	"github.com/sfrankle/ghost/internal/anthropic"
	"github.com/sfrankle/ghost/internal/transcripts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtract(t *testing.T) {
	fake := anthropic.NewFake().With("cheap", `{
		"observations": [
			{"kind":"rule","text":"don't mock the database","evidence":"turn 3","confidence":"high"}
		]
	}`)

	tr := transcripts.Transcript{
		Path:        "/p/-Users-x-dev-repoA/abc.jsonl",
		Project:     "Users-x-dev-repoA",
		ContentHash: "sha256:abc",
		Messages: []transcripts.Message{
			{Role: "user", Content: "don't mock the database"},
		},
	}

	out, err := Run(context.Background(), fake, tr)
	require.NoError(t, err)
	assert.Equal(t, "sha256:abc", out.ContentHash)
	assert.Equal(t, "Users-x-dev-repoA", out.Project)
	require.Len(t, out.Observations, 1)
	assert.Equal(t, "rule", out.Observations[0].Kind)
}
```

- [ ] **Step 3: Implement**

`internal/extract/extract.go`:

```go
package extract

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/sfrankle/ghost/internal/anthropic"
	"github.com/sfrankle/ghost/internal/transcripts"
)

//go:embed ../../prompts/extract.md
var systemPrompt string

type Observation struct {
	Kind       string `json:"kind"`
	Topic      string `json:"topic,omitempty"`
	Text       string `json:"text"`
	Evidence   string `json:"evidence"`
	Confidence string `json:"confidence,omitempty"`
}

type Result struct {
	Source       string        `json:"source"`
	Project      string        `json:"project"`
	ContentHash  string        `json:"content_hash"`
	ExtractedAt  time.Time     `json:"extracted_at"`
	Observations []Observation `json:"observations"`
}

func Run(ctx context.Context, client anthropic.Client, tr transcripts.Transcript) (Result, error) {
	user := renderTranscript(tr)
	raw, err := client.Complete(ctx, anthropic.Request{
		Role:        "cheap",
		System:      systemPrompt,
		User:        user,
		CacheSystem: true,
	})
	if err != nil {
		return Result{}, err
	}

	var payload struct {
		Observations []Observation `json:"observations"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return Result{}, fmt.Errorf("parse extract response: %w; raw=%q", err, raw)
	}

	return Result{
		Source:       tr.Path,
		Project:      tr.Project,
		ContentHash:  tr.ContentHash,
		ExtractedAt:  time.Now().UTC(),
		Observations: payload.Observations,
	}, nil
}

func renderTranscript(tr transcripts.Transcript) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Project: %s\n\n", tr.Project)
	for i, m := range tr.Messages {
		fmt.Fprintf(&b, "[turn %d] %s: %s\n", i+1, m.Role, m.Content)
	}
	return b.String()
}
```

- [ ] **Step 4: Run, commit**

```bash
go test ./internal/extract/... && git add . && git commit -m "feat: stage 1 extract"
```

---

## Task 7: Stage 2 — cluster

**Files:**
- Create: `internal/cluster/cluster.go`, `internal/cluster/cluster_test.go`, `prompts/cluster.md`

- [ ] **Step 1: Prompt**

`prompts/cluster.md`:

```markdown
You are clustering atomic observations across many conversations.

Input: a JSON array of observations, each with kind, text, evidence, and source project.

Tasks:
1. Group observations by kind (rule, voice, topic, identity).
2. Within topic, sub-group by topic name.
3. Collapse near-duplicates. Merge their evidence lists and union their source projects.

Output JSON shape:
{
  "rules": [{"text": "...", "evidence": ["..."], "projects": ["..."]}],
  "voice": [{"text": "...", "evidence": ["..."], "projects": ["..."]}],
  "identity": [{"text": "...", "evidence": ["..."], "projects": ["..."]}],
  "topics": {
    "<topic_name>": [{"text": "...", "evidence": ["..."], "projects": ["..."]}]
  }
}

Preserve precise wording when collapsing duplicates; prefer the most specific phrasing.
```

- [ ] **Step 2: Write the failing test**

```go
package cluster

import (
	"context"
	"testing"

	"github.com/sfrankle/ghost/internal/anthropic"
	"github.com/sfrankle/ghost/internal/extract"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCluster(t *testing.T) {
	fake := anthropic.NewFake().With("cheap", `{
		"rules":[{"text":"don't mock the database","evidence":["convA:3","convB:1"],"projects":["repoA","repoB"]}],
		"voice":[], "identity":[], "topics":{"testing":[]}
	}`)

	results := []extract.Result{
		{Project: "repoA", Observations: []extract.Observation{{Kind: "rule", Text: "don't mock db", Evidence: "turn 3"}}},
		{Project: "repoB", Observations: []extract.Observation{{Kind: "rule", Text: "no db mocks", Evidence: "turn 1"}}},
	}

	out, err := Run(context.Background(), fake, results)
	require.NoError(t, err)
	assert.Len(t, out.Rules, 1)
	assert.ElementsMatch(t, []string{"repoA", "repoB"}, out.Rules[0].Projects)
}
```

- [ ] **Step 3: Implement**

`internal/cluster/cluster.go`:

```go
package cluster

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/sfrankle/ghost/internal/anthropic"
	"github.com/sfrankle/ghost/internal/extract"
)

//go:embed ../../prompts/cluster.md
var systemPrompt string

type Entry struct {
	Text     string   `json:"text"`
	Evidence []string `json:"evidence"`
	Projects []string `json:"projects"`
}

type Clusters struct {
	Rules    []Entry            `json:"rules"`
	Voice    []Entry            `json:"voice"`
	Identity []Entry            `json:"identity"`
	Topics   map[string][]Entry `json:"topics"`
}

type flatInput struct {
	Kind     string `json:"kind"`
	Topic    string `json:"topic,omitempty"`
	Text     string `json:"text"`
	Evidence string `json:"evidence"`
	Project  string `json:"project"`
}

func Run(ctx context.Context, client anthropic.Client, results []extract.Result) (Clusters, error) {
	var flat []flatInput
	for _, r := range results {
		for _, o := range r.Observations {
			flat = append(flat, flatInput{
				Kind: o.Kind, Topic: o.Topic, Text: o.Text,
				Evidence: o.Evidence, Project: r.Project,
			})
		}
	}
	user, err := json.Marshal(flat)
	if err != nil {
		return Clusters{}, err
	}
	raw, err := client.Complete(ctx, anthropic.Request{
		Role:        "cheap",
		System:      systemPrompt,
		User:        string(user),
		CacheSystem: true,
	})
	if err != nil {
		return Clusters{}, err
	}
	var out Clusters
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return Clusters{}, fmt.Errorf("parse cluster response: %w; raw=%q", err, raw)
	}
	if out.Topics == nil {
		out.Topics = map[string][]Entry{}
	}
	return out, nil
}
```

- [ ] **Step 4: Run, commit**

```bash
go test ./internal/cluster/... && git add . && git commit -m "feat: stage 2 cluster"
```

---

## Task 8: Stage 3 — synthesize

**Files:**
- Create: `internal/synthesize/synthesize.go`, `internal/synthesize/synthesize_test.go`, `prompts/synthesize_profile.md`, `synthesize_rules.md`, `synthesize_topics.md`, `synthesize_index.md`

- [ ] **Step 1: Prompts (four files)**

`prompts/synthesize_profile.md`:

```markdown
You write the user's profile in first-person prose. Input is clusters of "voice" and "identity" observations. Output: 40-80 lines of grounded, specific prose. Identity first, then voice. No headers, no bullets, no self-congratulation.
```

`prompts/synthesize_rules.md`:

```markdown
You write a mechanical do/don't rule list. Input is a filtered set of rules — each appears in at least 2 conversations across at least 2 projects. For each: one line, then a "Why:" line if the reason is not obvious. Group by rough topic. No commentary.
```

`prompts/synthesize_topics.md`:

```markdown
You write one topic file. Input: a topic name and its cluster entries. Output: a focused markdown document with the specific guidance a developer reading this topic needs. Cite source projects in passing when relevant.
```

`prompts/synthesize_index.md`:

```markdown
You generate the topic lookup index. Input: a list of topic files with a short content summary. For each, produce trigger phrases — short comma-separated keywords/topics that indicate the file should be loaded. Output the markdown index per the format shown.
```

- [ ] **Step 2: Write the failing test**

```go
package synthesize

import (
	"context"
	"testing"

	"github.com/sfrankle/ghost/internal/anthropic"
	"github.com/sfrankle/ghost/internal/cluster"
	"github.com/sfrankle/ghost/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSynthesizeFiltersRulesByEvidenceAndProjects(t *testing.T) {
	fake := anthropic.NewFake().
		With("smart", "PROFILE_TEXT") // intentionally same response for all smart calls in this test

	clusters := cluster.Clusters{
		Rules: []cluster.Entry{
			{Text: "keep", Evidence: []string{"a:1", "b:1"}, Projects: []string{"r1", "r2"}}, // passes
			{Text: "drop one project", Evidence: []string{"a:1", "a:2"}, Projects: []string{"r1"}}, // fails project count
			{Text: "drop one evidence", Evidence: []string{"a:1"}, Projects: []string{"r1", "r2"}}, // fails evidence count
		},
		Topics: map[string][]cluster.Entry{"testing": {{Text: "x"}}},
	}

	out, err := Run(context.Background(), fake, clusters, config.Defaults().Thresholds)
	require.NoError(t, err)
	assert.NotEmpty(t, out.Profile)
	assert.NotEmpty(t, out.Rules)
	assert.NotEmpty(t, out.Index)
	assert.Contains(t, out.Topics, "testing")
	// Inspect that the rules call only saw the kept rule:
	for _, c := range fake.Calls() {
		if c.System == rulesPromptForTest() { // helper getter
			assert.Contains(t, c.User, "keep")
			assert.NotContains(t, c.User, "drop one project")
			assert.NotContains(t, c.User, "drop one evidence")
		}
	}
}
```

(Add an exported test helper `rulesPromptForTest()` in `synthesize_test.go` that returns the embedded `rulesPrompt`. Or compare on a substring like `"mechanical do/don't"`.)

- [ ] **Step 3: Implement**

`internal/synthesize/synthesize.go`:

```go
package synthesize

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/sfrankle/ghost/internal/anthropic"
	"github.com/sfrankle/ghost/internal/cluster"
	"github.com/sfrankle/ghost/internal/config"
)

//go:embed ../../prompts/synthesize_profile.md
var profilePrompt string

//go:embed ../../prompts/synthesize_rules.md
var rulesPrompt string

//go:embed ../../prompts/synthesize_topics.md
var topicsPrompt string

//go:embed ../../prompts/synthesize_index.md
var indexPrompt string

type Output struct {
	Profile string
	Rules   string
	Index   string
	Topics  map[string]string // topic name -> file body
}

func Run(ctx context.Context, c anthropic.Client, clusters cluster.Clusters, th config.Thresholds) (Output, error) {
	out := Output{Topics: map[string]string{}}

	// Profile from voice + identity
	in, _ := json.Marshal(map[string]any{"voice": clusters.Voice, "identity": clusters.Identity})
	profile, err := c.Complete(ctx, anthropic.Request{Role: "smart", System: profilePrompt, User: string(in)})
	if err != nil {
		return out, fmt.Errorf("profile: %w", err)
	}
	out.Profile = profile

	// Rules — filter first
	kept := filterRules(clusters.Rules, th)
	in, _ = json.Marshal(kept)
	rules, err := c.Complete(ctx, anthropic.Request{Role: "smart", System: rulesPrompt, User: string(in)})
	if err != nil {
		return out, fmt.Errorf("rules: %w", err)
	}
	out.Rules = rules

	// One topic file per topic
	for name, entries := range clusters.Topics {
		in, _ := json.Marshal(map[string]any{"topic": name, "entries": entries})
		body, err := c.Complete(ctx, anthropic.Request{Role: "smart", System: topicsPrompt, User: string(in)})
		if err != nil {
			return out, fmt.Errorf("topic %s: %w", name, err)
		}
		out.Topics[name] = body
	}

	// Index from topic summaries
	summaries := map[string]string{}
	for name, body := range out.Topics {
		summaries[name] = firstNLines(body, 5)
	}
	in, _ = json.Marshal(summaries)
	index, err := c.Complete(ctx, anthropic.Request{Role: "smart", System: indexPrompt, User: string(in)})
	if err != nil {
		return out, fmt.Errorf("index: %w", err)
	}
	out.Index = index

	return out, nil
}

func filterRules(rules []cluster.Entry, th config.Thresholds) []cluster.Entry {
	out := make([]cluster.Entry, 0, len(rules))
	for _, r := range rules {
		if len(r.Evidence) < th.RuleMinEvidenceCount {
			continue
		}
		if len(r.Projects) < th.RuleMinProjectCount {
			continue
		}
		out = append(out, r)
	}
	return out
}

func firstNLines(s string, n int) string {
	count, end := 0, 0
	for i, r := range s {
		if r == '\n' {
			count++
			if count == n {
				end = i
				break
			}
		}
	}
	if end == 0 {
		return s
	}
	return s[:end]
}
```

- [ ] **Step 4: Run, commit**

```bash
go test ./internal/synthesize/... && git add . && git commit -m "feat: stage 3 synthesize with rule filtering"
```

---

## Task 9: Stage 4 — refine

**Files:**
- Create: `internal/refine/refine.go`, `internal/refine/refine_test.go`, `prompts/refine.md`

- [ ] **Step 1: Prompt**

`prompts/refine.md`:

```markdown
You apply an Orwell pass to a generated markdown document.

Rules:
- Delete every sentence you don't miss when it's gone.
- Strip em-dashes. Prefer commas, periods, parentheses.
- Strip self-congratulation, hedging, and "this is important" framing.
- Prefer short concrete sentences over long abstract ones.
- Preserve every load-bearing concrete fact (citations, project names, specific rules).

Output: the refined markdown only. No preamble, no commentary.
```

- [ ] **Step 2: Write the failing test**

```go
package refine

import (
	"context"
	"testing"

	"github.com/sfrankle/ghost/internal/anthropic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRefineEach(t *testing.T) {
	fake := anthropic.NewFake().With("smart", "REFINED")
	in := map[string]string{
		"profile.md": "raw",
		"rules.md":   "raw",
		"topics/testing.md": "raw",
	}
	out, err := RunMany(context.Background(), fake, in)
	require.NoError(t, err)
	for path := range in {
		assert.Equal(t, "REFINED", out[path], "file %s should be refined", path)
	}
	assert.Equal(t, 3, len(fake.Calls()))
}
```

- [ ] **Step 3: Implement**

`internal/refine/refine.go`:

```go
package refine

import (
	"context"
	_ "embed"

	"github.com/sfrankle/ghost/internal/anthropic"
)

//go:embed ../../prompts/refine.md
var systemPrompt string

func Run(ctx context.Context, c anthropic.Client, body string) (string, error) {
	return c.Complete(ctx, anthropic.Request{
		Role: "smart", System: systemPrompt, User: body, CacheSystem: true,
	})
}

func RunMany(ctx context.Context, c anthropic.Client, files map[string]string) (map[string]string, error) {
	out := make(map[string]string, len(files))
	for path, body := range files {
		refined, err := Run(ctx, c, body)
		if err != nil {
			return nil, err
		}
		out[path] = refined
	}
	return out, nil
}
```

- [ ] **Step 4: Run, commit**

```bash
go test ./internal/refine/... && git add . && git commit -m "feat: stage 4 refine"
```

---

## Task 10: Compose orchestrator

**Files:**
- Create: `internal/compose/compose.go`, `internal/compose/compose_test.go`
- Modify: `cmd/ghost/compose.go` (wire up command)

- [ ] **Step 1: Write the failing test**

```go
package compose

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/sfrankle/ghost/internal/anthropic"
	"github.com/sfrankle/ghost/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComposeExtractRespectsLimit(t *testing.T) {
	tmp := t.TempDir()
	projDir := filepath.Join(tmp, "projects", "-Users-x-dev-repoA")
	require.NoError(t, os.MkdirAll(projDir, 0o755))
	for _, name := range []string{"a.jsonl", "b.jsonl", "c.jsonl"} {
		require.NoError(t, os.WriteFile(filepath.Join(projDir, name),
			[]byte(`{"type":"user","message":{"role":"user","content":"x"}}`+"\n"), 0o644))
	}

	cfg := config.Defaults()
	cfg.Paths.TranscriptsGlob = filepath.Join(tmp, "projects", "**", "*.jsonl")
	cfg.Paths.OutputDir = filepath.Join(tmp, "ghost-out")

	fake := anthropic.NewFake().With("cheap", `{"observations":[]}`)

	opts := Options{Limit: 2, Stages: []string{"extract"}}
	r, err := Run(context.Background(), fake, cfg, opts)
	require.NoError(t, err)
	assert.Equal(t, 2, r.Extracted)

	// Ledger persisted under the configured output dir
	entries, _ := filepath.Glob(filepath.Join(cfg.Paths.OutputDir, ".state", "observations", "*.json"))
	sort.Strings(entries)
	assert.Len(t, entries, 2)
}
```

- [ ] **Step 2: Implement**

`internal/compose/compose.go`:

```go
package compose

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/sfrankle/ghost/internal/anthropic"
	"github.com/sfrankle/ghost/internal/cluster"
	"github.com/sfrankle/ghost/internal/config"
	"github.com/sfrankle/ghost/internal/extract"
	"github.com/sfrankle/ghost/internal/ledger"
	"github.com/sfrankle/ghost/internal/refine"
	"github.com/sfrankle/ghost/internal/synthesize"
	"github.com/sfrankle/ghost/internal/transcripts"
)

type Options struct {
	Limit         int
	Stages        []string // subset of: extract, cluster, synthesize, refine
	DryRun        bool
	SinceDuration string
	Project       string
}

type Result struct {
	Extracted  int
	Skipped    int
	Synthesized bool
}

func Run(ctx context.Context, c anthropic.Client, cfg config.Config, opts Options) (Result, error) {
	stages := defaultIfEmpty(opts.Stages, []string{"extract", "cluster", "synthesize", "refine"})
	stageSet := toSet(stages)

	outDir := expandHome(cfg.Paths.OutputDir)
	stateDir := filepath.Join(outDir, ".state")
	if err := os.MkdirAll(filepath.Join(stateDir, "observations"), 0o755); err != nil {
		return Result{}, err
	}

	lg, err := ledger.Load(filepath.Join(stateDir, "ledger.json"))
	if err != nil {
		return Result{}, err
	}

	r := Result{}

	if stageSet["extract"] {
		ex, err := runExtract(ctx, c, cfg, opts, lg, stateDir)
		if err != nil {
			return r, err
		}
		r.Extracted = ex
	}

	// Stages 2-4 are corpus-level
	if stageSet["cluster"] || stageSet["synthesize"] || stageSet["refine"] {
		results, err := loadAllObservations(stateDir)
		if err != nil {
			return r, err
		}
		if len(results) == 0 {
			return r, fmt.Errorf("no observations found; run --stages extract first")
		}

		var clusters cluster.Clusters
		if stageSet["cluster"] {
			clusters, err = cluster.Run(ctx, c, results)
			if err != nil {
				return r, err
			}
			persist(filepath.Join(stateDir, "clusters.json"), clusters)
		} else {
			err := loadJSON(filepath.Join(stateDir, "clusters.json"), &clusters)
			if err != nil {
				return r, fmt.Errorf("clusters not built; run --stages cluster first: %w", err)
			}
		}

		if stageSet["synthesize"] || stageSet["refine"] {
			synth, err := synthesize.Run(ctx, c, clusters, cfg.Thresholds)
			if err != nil {
				return r, err
			}
			files := map[string]string{
				"profile.md": synth.Profile,
				"rules.md":   synth.Rules,
				"index.md":   synth.Index,
			}
			for name, body := range synth.Topics {
				files[filepath.Join("topics", name+".md")] = body
			}

			if stageSet["refine"] {
				files, err = refine.RunMany(ctx, c, files)
				if err != nil {
					return r, err
				}
			}

			if err := writeOutputs(outDir, files); err != nil {
				return r, err
			}
			r.Synthesized = true
		}
	}

	return r, lg.Save(filepath.Join(stateDir, "ledger.json"))
}

func runExtract(ctx context.Context, c anthropic.Client, cfg config.Config, opts Options, lg *ledger.Ledger, stateDir string) (int, error) {
	paths, err := transcripts.Discover(cfg.Paths.TranscriptsGlob)
	if err != nil {
		return 0, err
	}
	sort.Strings(paths)

	processed := 0
	for _, p := range paths {
		if opts.Limit > 0 && processed >= opts.Limit {
			break
		}
		tr, err := transcripts.Parse(p)
		if err != nil {
			fmt.Fprintf(os.Stderr, "skip %s: %v\n", p, err)
			continue
		}
		if e, ok := lg.Get(p); ok && e.ContentHash == tr.ContentHash {
			continue // up to date
		}
		if opts.Project != "" && tr.Project != opts.Project {
			continue
		}
		res, err := extract.Run(ctx, c, tr)
		if err != nil {
			return processed, err
		}
		obsFile := filepath.Join(stateDir, "observations", filenameFor(tr.ContentHash)+".json")
		if err := persist(obsFile, res); err != nil {
			return processed, err
		}
		lg.MarkProcessed(p, ledger.Entry{
			ContentHash:      tr.ContentHash,
			ObservationsFile: obsFile,
			MessageCount:     len(tr.Messages),
		})
		processed++
	}
	return processed, nil
}

func loadAllObservations(stateDir string) ([]extract.Result, error) {
	files, err := filepath.Glob(filepath.Join(stateDir, "observations", "*.json"))
	if err != nil {
		return nil, err
	}
	out := make([]extract.Result, 0, len(files))
	for _, f := range files {
		var r extract.Result
		if err := loadJSON(f, &r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

func writeOutputs(outDir string, files map[string]string) error {
	for rel, body := range files {
		p := filepath.Join(outDir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			return err
		}
	}
	// Ensure rules.user.md exists (empty is fine)
	userRules := filepath.Join(outDir, "rules.user.md")
	if _, err := os.Stat(userRules); os.IsNotExist(err) {
		_ = os.WriteFile(userRules, []byte("# Manual rules (survives recompose)\n"), 0o644)
	}
	return nil
}

func persist(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func loadJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

func filenameFor(contentHash string) string {
	h := sha256.Sum256([]byte(contentHash))
	return hex.EncodeToString(h[:8])
}

func defaultIfEmpty(s, def []string) []string {
	if len(s) == 0 {
		return def
	}
	return s
}

func toSet(s []string) map[string]bool {
	m := map[string]bool{}
	for _, x := range s {
		m[x] = true
	}
	return m
}

func expandHome(p string) string {
	if len(p) >= 2 && p[:2] == "~/" {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, p[2:])
	}
	return p
}
```

- [ ] **Step 3: Wire up the CLI command**

`cmd/ghost/compose.go`:

```go
package main

import (
	"fmt"

	"github.com/sfrankle/ghost/internal/anthropic"
	"github.com/sfrankle/ghost/internal/compose"
	"github.com/sfrankle/ghost/internal/config"
	"github.com/spf13/cobra"
	"os"
)

func newComposeCmd() *cobra.Command {
	var opts compose.Options
	var stages []string
	cmd := &cobra.Command{
		Use:   "compose",
		Short: "Run the synthesis pipeline",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(expandHome("~/.ghost/config.toml"))
			if err != nil {
				return err
			}
			apiKey := os.Getenv("ANTHROPIC_API_KEY")
			if apiKey == "" {
				return fmt.Errorf("ANTHROPIC_API_KEY not set")
			}
			c := anthropic.New(apiKey, anthropic.Models{Cheap: cfg.Models.Cheap, Smart: cfg.Models.Smart})
			opts.Stages = stages
			r, err := compose.Run(cmd.Context(), c, cfg, opts)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "extracted=%d synthesized=%v\n", r.Extracted, r.Synthesized)
			return nil
		},
	}
	cmd.Flags().IntVar(&opts.Limit, "limit", 0, "max unprocessed transcripts per run")
	cmd.Flags().StringSliceVar(&stages, "stages", nil, "subset of stages to run (extract,cluster,synthesize,refine)")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "show what would be processed")
	cmd.Flags().StringVar(&opts.Project, "project", "", "only transcripts under this project dir")
	return cmd
}

func expandHome(p string) string {
	if len(p) >= 2 && p[:2] == "~/" {
		home, _ := os.UserHomeDir()
		return home + p[1:]
	}
	return p
}
```

- [ ] **Step 4: Run, commit**

```bash
go test ./... && go build ./cmd/ghost && git add . && git commit -m "feat: compose orchestrator with stage subsetting"
```

---

## Task 11: status command

**Files:**
- Create: `internal/compose/status.go`, `internal/compose/status_test.go`
- Modify: `cmd/ghost/status.go`

- [ ] **Step 1: Write the failing test**

```go
package compose

import (
	"path/filepath"
	"testing"

	"github.com/sfrankle/ghost/internal/config"
	"github.com/sfrankle/ghost/internal/ledger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStatusBuckets(t *testing.T) {
	tmp := t.TempDir()
	cfg := config.Defaults()
	cfg.Paths.OutputDir = tmp
	cfg.Paths.TranscriptsGlob = filepath.Join(tmp, "p", "**", "*.jsonl")

	// 3 transcripts on disk
	// (use the helper from transcripts test or inline writeFile here)
	// ... seed disk with a, b, c.jsonl ...

	lg := ledger.New()
	lg.MarkProcessed(filepath.Join(tmp, "p", "x", "a.jsonl"), ledger.Entry{ContentHash: "sha256:oldA"})
	require.NoError(t, lg.Save(filepath.Join(tmp, ".state", "ledger.json")))

	s, err := Status(cfg)
	require.NoError(t, err)
	assert.Equal(t, 3, s.Total)
}
```

- [ ] **Step 2: Implement**

```go
package compose

import (
	"path/filepath"

	"github.com/sfrankle/ghost/internal/config"
	"github.com/sfrankle/ghost/internal/ledger"
	"github.com/sfrankle/ghost/internal/transcripts"
)

type StatusSummary struct {
	Total     int
	Processed int
	Pending   int
	Dirty     int
}

func Status(cfg config.Config) (StatusSummary, error) {
	paths, err := transcripts.Discover(cfg.Paths.TranscriptsGlob)
	if err != nil {
		return StatusSummary{}, err
	}
	lg, err := ledger.Load(filepath.Join(expandHome(cfg.Paths.OutputDir), ".state", "ledger.json"))
	if err != nil {
		return StatusSummary{}, err
	}
	known := map[string]string{}
	for _, p := range paths {
		tr, err := transcripts.Parse(p)
		if err != nil {
			continue
		}
		known[p] = tr.ContentHash
	}
	r := lg.Status(known)
	return StatusSummary{Total: len(known), Processed: r.Processed, Pending: r.Pending, Dirty: r.Dirty}, nil
}
```

- [ ] **Step 3: Wire CLI**

`cmd/ghost/status.go`:

```go
package main

import (
	"fmt"

	"github.com/sfrankle/ghost/internal/compose"
	"github.com/sfrankle/ghost/internal/config"
	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Ledger summary: total / processed / pending / dirty",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load(expandHome("~/.ghost/config.toml"))
			if err != nil {
				return err
			}
			s, err := compose.Status(cfg)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "total=%d processed=%d pending=%d dirty=%d\n",
				s.Total, s.Processed, s.Pending, s.Dirty)
			return nil
		},
	}
}
```

- [ ] **Step 4: Run, commit**

```bash
go test ./... && git add . && git commit -m "feat: ghost status"
```

---

## Task 12: show, topics, add-rule, forget commands

**Files:**
- Modify: `cmd/ghost/show.go`, create `cmd/ghost/topics.go`, `cmd/ghost/add_rule.go`, `cmd/ghost/forget.go`

- [ ] **Step 1: Add commands to root**

Update `newRootCmd()` in `cmd/ghost/main.go` to include the new commands:

```go
cmd.AddCommand(newComposeCmd(), newShowCmd(), newStatusCmd(),
    newTopicsCmd(), newAddRuleCmd(), newForgetCmd(), newConfigCmd(), newEvalCmd())
```

- [ ] **Step 2: Write the failing tests**

`cmd/ghost/show_test.go`:

```go
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShowPrintsProfileAndRules(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	gd := filepath.Join(tmp, ".ghost")
	require.NoError(t, os.MkdirAll(gd, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(gd, "profile.md"), []byte("PROFILE"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(gd, "rules.md"), []byte("RULES"), 0o644))

	var out bytes.Buffer
	cmd := newShowCmd()
	cmd.SetOut(&out)
	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), "PROFILE")
	assert.Contains(t, out.String(), "RULES")
}

func TestAddRuleAppends(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	cmd := newAddRuleCmd()
	cmd.SetArgs([]string{"never use git push --force"})
	require.NoError(t, cmd.Execute())

	got, err := os.ReadFile(filepath.Join(tmp, ".ghost", "rules.user.md"))
	require.NoError(t, err)
	assert.Contains(t, string(got), "never use git push --force")
}
```

- [ ] **Step 3: Implement**

`cmd/ghost/show.go`:

```go
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

func newShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Print profile + rules",
		RunE: func(cmd *cobra.Command, _ []string) error {
			gd := expandHome("~/.ghost")
			for _, name := range []string{"profile.md", "rules.md", "rules.user.md"} {
				data, err := os.ReadFile(filepath.Join(gd, name))
				if err != nil && !os.IsNotExist(err) {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "=== %s ===\n%s\n", name, data)
			}
			return nil
		},
	}
}
```

`cmd/ghost/topics.go`:

```go
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

func newTopicsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "topics",
		Short: "List topic files with last-modified time",
		RunE: func(cmd *cobra.Command, _ []string) error {
			topicsDir := filepath.Join(expandHome("~/.ghost"), "topics")
			entries, err := os.ReadDir(topicsDir)
			if err != nil {
				if os.IsNotExist(err) {
					return nil
				}
				return err
			}
			for _, e := range entries {
				info, _ := e.Info()
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\n", e.Name(), info.ModTime().Format("2006-01-02 15:04"))
			}
			return nil
		},
	}
}
```

`cmd/ghost/add_rule.go`:

```go
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func newAddRuleCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add-rule <text>",
		Short: "Append a manual rule that survives recompose",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			text := strings.Join(args, " ")
			path := filepath.Join(expandHome("~/.ghost"), "rules.user.md")
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
			if err != nil {
				return err
			}
			defer f.Close()
			_, err = fmt.Fprintf(f, "\n- %s _(added %s)_\n", text, time.Now().UTC().Format("2006-01-02"))
			return err
		},
	}
}
```

`cmd/ghost/forget.go`:

```go
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/sfrankle/ghost/internal/ledger"
	"github.com/spf13/cobra"
)

func newForgetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "forget <conversation-path>",
		Short: "Drop a conversation's observations from the corpus",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p := args[0]
			stateDir := filepath.Join(expandHome("~/.ghost"), ".state")
			lg, err := ledger.Load(filepath.Join(stateDir, "ledger.json"))
			if err != nil {
				return err
			}
			e, ok := lg.Get(p)
			if !ok {
				return fmt.Errorf("conversation not in ledger: %s", p)
			}
			if err := os.Remove(e.ObservationsFile); err != nil && !os.IsNotExist(err) {
				return err
			}
			delete(lg.Conversations, p)
			fmt.Fprintln(cmd.OutOrStdout(), "forgotten:", p)
			return lg.Save(filepath.Join(stateDir, "ledger.json"))
		},
	}
}
```

- [ ] **Step 4: Run, commit**

```bash
go test ./... && git add . && git commit -m "feat: show, topics, add-rule, forget commands"
```

---

## Task 13: config show/edit command

**Files:**
- Create: `cmd/ghost/config.go`

- [ ] **Step 1: Write the failing test**

```go
package main

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigShowPrintsDefaults(t *testing.T) {
	t.Setenv("HOME", filepath.Join(t.TempDir())) // no config.toml present
	var out bytes.Buffer
	cmd := newConfigCmd()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"show"})
	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), "claude-opus-4-7")
}
```

- [ ] **Step 2: Implement**

`cmd/ghost/config.go`:

```go
package main

import (
	"os"
	"os/exec"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/sfrankle/ghost/internal/config"
	"github.com/spf13/cobra"
)

func newConfigCmd() *cobra.Command {
	c := &cobra.Command{Use: "config", Short: "Inspect or edit ghost config"}

	show := &cobra.Command{
		Use:   "show",
		Short: "Print the effective config (defaults merged with user overrides)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load(expandHome("~/.ghost/config.toml"))
			if err != nil {
				return err
			}
			return toml.NewEncoder(cmd.OutOrStdout()).Encode(cfg)
		},
	}

	edit := &cobra.Command{
		Use:   "edit",
		Short: "Open ~/.ghost/config.toml in $EDITOR",
		RunE: func(cmd *cobra.Command, _ []string) error {
			path := expandHome("~/.ghost/config.toml")
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			if _, err := os.Stat(path); os.IsNotExist(err) {
				cfg := config.Defaults()
				f, err := os.Create(path)
				if err != nil {
					return err
				}
				if err := toml.NewEncoder(f).Encode(cfg); err != nil {
					return err
				}
				f.Close()
			}
			editor := os.Getenv("EDITOR")
			if editor == "" {
				editor = "vi"
			}
			run := exec.Command(editor, path)
			run.Stdin, run.Stdout, run.Stderr = os.Stdin, os.Stdout, os.Stderr
			return run.Run()
		},
	}

	c.AddCommand(show, edit)
	return c
}
```

- [ ] **Step 3: Run, commit**

```bash
go test ./... && git add . && git commit -m "feat: config show/edit commands"
```

---

## Task 14: eval command

**Files:**
- Create: `internal/eval/eval.go`, `internal/eval/eval_test.go`, `prompts/eval.md`, `cmd/ghost/eval.go`

- [ ] **Step 1: Prompt**

`prompts/eval.md`:

```markdown
You are judging whether a synthesized profile + rule set accurately describes a person based on a held-out conversation.

Input:
- profile.md text
- rules.md text
- a held-out transcript

Output JSON: {"voice_match": 0-10, "rule_coverage": 0-10, "false_positives": 0-10, "notes": "..."}
- voice_match: how well does the profile describe the way the user talks here?
- rule_coverage: how many user preferences in this conversation are covered by rules?
- false_positives: how many rules in rules.md are contradicted by this conversation?
```

- [ ] **Step 2: Test (smoke)**

```go
package eval

import (
	"context"
	"testing"

	"github.com/sfrankle/ghost/internal/anthropic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEvalParsesScores(t *testing.T) {
	fake := anthropic.NewFake().With("smart", `{"voice_match":8,"rule_coverage":6,"false_positives":1,"notes":"ok"}`)
	score, err := Judge(context.Background(), fake, "PROFILE", "RULES", "TRANSCRIPT")
	require.NoError(t, err)
	assert.Equal(t, 8, score.VoiceMatch)
	assert.Equal(t, 1, score.FalsePositives)
}
```

- [ ] **Step 3: Implement**

`internal/eval/eval.go`:

```go
package eval

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/sfrankle/ghost/internal/anthropic"
)

//go:embed ../../prompts/eval.md
var systemPrompt string

type Score struct {
	VoiceMatch     int    `json:"voice_match"`
	RuleCoverage   int    `json:"rule_coverage"`
	FalsePositives int    `json:"false_positives"`
	Notes          string `json:"notes"`
}

func Judge(ctx context.Context, c anthropic.Client, profile, rules, transcript string) (Score, error) {
	user := fmt.Sprintf("PROFILE:\n%s\n\nRULES:\n%s\n\nTRANSCRIPT:\n%s", profile, rules, transcript)
	raw, err := c.Complete(ctx, anthropic.Request{Role: "smart", System: systemPrompt, User: user})
	if err != nil {
		return Score{}, err
	}
	var s Score
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return Score{}, fmt.Errorf("parse eval response: %w; raw=%q", err, raw)
	}
	return s, nil
}
```

`cmd/ghost/eval.go`: command that loads profile/rules and runs `Judge` against a held-out sample (10% of transcripts, deterministic by hash). Print mean scores. Implementation parallels `compose.go`.

- [ ] **Step 4: Run, commit**

```bash
go test ./... && git add . && git commit -m "feat: ghost eval"
```

---

## Task 15: Claude Code skill installation

**Files:**
- Create: `skill/SKILL.md`, `scripts/install-skill.sh`

- [ ] **Step 1: Write SKILL.md**

`skill/SKILL.md`:

```markdown
---
name: ghost
description: Use at the start of any task. Checks the ghost topic index and reads matching topic files before responding. Triggers on any task touching a topic listed in ~/.ghost/index.md.
---

# Ghost — lazy-load topic guidance

You have a global profile and rule set always loaded. You also have an index of deeper topic files at ~/.ghost/index.md.

## Mechanical check (before responding to the user)

1. Read ~/.ghost/index.md if you have not already this session.
2. Match the user's request against the triggers for each topic.
3. If any topic matches, Read that topic file BEFORE writing code or answering. Do not skip on the grounds that you "probably know."
4. If no topic matches, proceed without loading anything.

A topic loaded once per session stays in context — do not re-Read it.

## What NOT to load

Do not load every topic file at session start. The whole point is lazy loading. Loading all topics defeats the token-economy design.
```

- [ ] **Step 2: Install script**

`scripts/install-skill.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail
DEST="${HOME}/.claude/skills/ghost"
mkdir -p "${DEST}"
cp "$(dirname "$0")/../skill/SKILL.md" "${DEST}/SKILL.md"
echo "installed ghost skill at ${DEST}"
```

```bash
chmod +x scripts/install-skill.sh
```

- [ ] **Step 3: Commit**

```bash
git add . && git commit -m "feat: claude code skill + install script"
```

---

## Task 16: End-to-end smoke test

**Files:**
- Create: `e2e/smoke_test.go`

This test runs only when `GHOST_E2E=1`. It exercises the full pipeline against the fake client to confirm wiring.

- [ ] **Step 1: Write the test**

```go
//go:build e2e

package e2e

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sfrankle/ghost/internal/anthropic"
	"github.com/sfrankle/ghost/internal/compose"
	"github.com/sfrankle/ghost/internal/config"
	"github.com/stretchr/testify/require"
)

func TestSmoke(t *testing.T) {
	if os.Getenv("GHOST_E2E") == "" {
		t.Skip("set GHOST_E2E=1 to run")
	}
	tmp := t.TempDir()
	projDir := filepath.Join(tmp, "projects", "-Users-x-dev-repoA")
	require.NoError(t, os.MkdirAll(projDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projDir, "a.jsonl"),
		[]byte(`{"type":"user","message":{"role":"user","content":"don't mock the database"}}`+"\n"), 0o644))

	cfg := config.Defaults()
	cfg.Paths.TranscriptsGlob = filepath.Join(tmp, "projects", "**", "*.jsonl")
	cfg.Paths.OutputDir = filepath.Join(tmp, "ghost-out")

	fake := anthropic.NewFake().
		With("cheap", `{"observations":[{"kind":"rule","text":"no db mocks","evidence":"turn 1"}]}`).
		With("smart", "PROFILE OR RULES")
	// (cluster also uses cheap, so the cheap fake response above is reused — fine for smoke)

	r, err := compose.Run(context.Background(), fake, cfg, compose.Options{})
	require.NoError(t, err)
	require.True(t, r.Synthesized)

	for _, name := range []string{"profile.md", "rules.md", "index.md"} {
		_, err := os.Stat(filepath.Join(cfg.Paths.OutputDir, name))
		require.NoError(t, err, "missing %s", name)
	}
}
```

> The fake currently maps one response per role. If multiple distinct outputs per role are needed across stages, extend `Fake.With(role, response)` to push onto a queue and pop FIFO. Do this as a follow-up only if smoke proves the need.

- [ ] **Step 2: Run, commit**

```bash
GHOST_E2E=1 go test -tags=e2e ./e2e/... && git add . && git commit -m "test: end-to-end smoke"
```

---

## Task 17: First real compose against your transcripts

This is a verification milestone, not a code task. No commit.

- [ ] **Step 1: Build and install**

```bash
go install ./cmd/ghost
./scripts/install-skill.sh
```

- [ ] **Step 2: Cheap batch**

```bash
export ANTHROPIC_API_KEY=sk-...
ghost status
ghost compose --limit 5 --stages extract
ls ~/.ghost/.state/observations/
```

Open 1–2 observation files. Confirm:
- Evidence quotes appear sensible
- Project tag matches the transcript path
- No malformed JSON

- [ ] **Step 3: Drain backlog**

```bash
ghost compose --stages extract       # process the rest
ghost compose --stages cluster,synthesize,refine
ghost show
ghost topics
```

- [ ] **Step 4: Wire into Claude Code**

Edit `~/.claude/CLAUDE.md` and add above the existing `@memory/MEMORY.md` line:

```markdown
@~/.ghost/profile.md
@~/.ghost/rules.md
@~/.ghost/rules.user.md
@~/.ghost/index.md
```

Keep the existing memory line. Run side-by-side for two weeks per the spec's migration plan.

---

## Spec coverage self-check

Walking the spec section by section:

| Spec section | Implementing task(s) |
|---|---|
| Architecture (CLI + skill) | Tasks 1, 15 |
| Data flow / stages | Tasks 6, 7, 8, 9 |
| Wiring into Claude Code | Task 17 step 4 |
| Data model (observations + synthesis) | Tasks 6, 8 |
| Observation schema | Task 6 |
| Pipeline stages | Tasks 6–9 |
| Configuration | Tasks 2, 13 |
| Incremental compose & batching | Task 10 |
| Runtime (the skill) | Task 15 |
| Slash commands | Tasks 11, 12, 13, 14 |
| Testing & eval | Each task TDD + Task 14 + Task 16 |
| Migration | Task 17 |
| Open questions (embedding dedup, hit-rate tracking, session detection) | Deferred per spec |

No gaps identified.
