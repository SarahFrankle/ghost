# Ghost Phase 1 — Extract Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship a walking skeleton that extracts observations from Claude Code transcripts and writes them to disk, so we can hand-review extract quality on the real corpus before building anything downstream.

**Architecture:** Go CLI (`ghost`) with a Cobra-based command surface. Compose pipeline is one stage in v1 (`extract`), running a bounded worker pool over per-transcript JSONL files, calling the Anthropic API with the `cheap` model, validating + scrubbing the result, and writing atomic per-transcript observation files. A JSON ledger tracks (path, content_hash, processed_at) so reruns are incremental. No clustering, no synthesis, no skill — those are Phase 2/3.

**Tech Stack:** Go 1.22+, Cobra (CLI), BurntSushi/toml (config), anthropic-sdk-go (LLM), standard library for everything else.

**Spec reference:** `docs/specs/2026-05-20-ghost-design.md` — Phase 1 scope is defined in the "Phasing" section; data model and stage-1 details under "Data model" and "Stage 1 — extract".

---

## File Structure

```
ghost/
  go.mod
  go.sum
  main.go                          # cobra root, wires subcommands
  cmd/
    root.go                        # root cmd, persistent flags (--config, -v)
    compose.go                     # `ghost compose` (extract-only in phase 1)
    status.go                      # `ghost status`
    forget.go                      # `ghost forget <path>`
    show.go                        # `ghost show observations --recent`
  internal/
    config/
      config.go                    # TOML load + defaults + Effective()
      config_test.go
    paths/
      paths.go                     # ~/.ghost resolution, mkdir helpers
    transcript/
      discover.go                  # glob, mtime skip, project tag
      discover_test.go
      hash.go                      # sha256 of file content
      hash_test.go
      parse.go                     # JSONL → []Turn
      parse_test.go
      testdata/
        sample.jsonl
    ledger/
      ledger.go                    # Load, Save, mutex, Mark, Forget
      ledger_test.go
    secrets/
      scrub.go                     # regex patterns + Filter()
      scrub_test.go
    atomicfs/
      write.go                     # WriteFileAtomic (tmp + rename)
      write_test.go
    anthropic/
      client.go                    # thin wrapper: Complete(role, system, user) → string
    extract/
      extract.go                   # Run(ctx, transcript) → ObservationsFile
      prompt.go                    # embedded system prompt
      schema.go                    # Observation, ObservationsFile types + Validate()
      extract_test.go              # uses fake anthropic client
      testdata/
        golden_transcript.jsonl
        golden_observations.json
  prompts/
    extract.system.md              # embedded via //go:embed
```

Splits are by responsibility. `transcript/` owns reading; `ledger/` owns state; `secrets/` owns the deterministic filter; `extract/` owns the LLM-driven stage. CLI commands in `cmd/` are thin — they parse flags and call into `internal/`.

---

### Task 1: Bootstrap module and root command

**Files:**
- Create: `go.mod`, `main.go`, `cmd/root.go`

- [ ] **Step 1: Initialize module**

```bash
cd /Users/sarah/dev/projects/ghost
go mod init github.com/SarahFrankle/ghost
go get github.com/spf13/cobra@latest
```

- [ ] **Step 2: Write `main.go`**

```go
package main

import (
	"fmt"
	"os"

	"github.com/SarahFrankle/ghost/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
```

- [ ] **Step 3: Write `cmd/root.go`**

```go
package cmd

import (
	"github.com/spf13/cobra"
)

var (
	verbose    bool
	configPath string
)

var rootCmd = &cobra.Command{
	Use:   "ghost",
	Short: "Synthesize identity, rules, topics, and voice from Claude Code transcripts",
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")
	rootCmd.PersistentFlags().StringVar(&configPath, "config", "", "path to config.toml (default ~/.ghost/config.toml)")
}
```

- [ ] **Step 4: Verify build**

Run: `go build ./...`
Expected: builds cleanly, produces `./ghost` binary.
Run: `./ghost --help`
Expected: prints the root cmd help.

- [ ] **Step 5: Commit**

```bash
git checkout -b phase-1-extract
git add go.mod go.sum main.go cmd/
git commit -m "feat: bootstrap ghost CLI with cobra root command"
```

---

### Task 2: Config loader with baked-in defaults

**Files:**
- Create: `internal/config/config.go`, `internal/config/config_test.go`, `internal/paths/paths.go`

- [ ] **Step 1: Write the failing test `internal/config/config_test.go`**

```go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultsWhenNoFile(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(filepath.Join(dir, "missing.toml"))
	if err != nil {
		t.Fatalf("Load on missing file: %v", err)
	}
	if cfg.Models.Cheap == "" || cfg.Models.Smart == "" {
		t.Fatalf("defaults not populated: %+v", cfg.Models)
	}
	if cfg.Batching.ExtractWorkers != 5 {
		t.Fatalf("default extract_workers = %d, want 5", cfg.Batching.ExtractWorkers)
	}
}

func TestOverridesFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := `
[batching]
extract_workers = 9
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Batching.ExtractWorkers != 9 {
		t.Fatalf("override not applied: %d", cfg.Batching.ExtractWorkers)
	}
	if cfg.Models.Cheap == "" {
		t.Fatalf("default cheap model lost after partial override")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/...`
Expected: FAIL — package does not exist.

- [ ] **Step 3: Implement `internal/config/config.go`**

```go
package config

import (
	"errors"
	"io/fs"
	"os"

	"github.com/BurntSushi/toml"
)

type Models struct {
	Cheap     string `toml:"cheap"`
	Smart     string `toml:"smart"`
	Embedding string `toml:"embedding"`
}

type Thresholds struct {
	RuleMinEvidenceCount    int     `toml:"rule_min_evidence_count"`
	RuleMinProjectCount     int     `toml:"rule_min_project_count"`
	VoiceMinEvidenceCount   int     `toml:"voice_min_evidence_count"`
	ClusterCosineThreshold  float64 `toml:"cluster_cosine_threshold"`
}

type Paths struct {
	TranscriptsGlob string `toml:"transcripts_glob"`
	OutputDir       string `toml:"output_dir"`
}

type Batching struct {
	DefaultLimit    int `toml:"default_limit"`
	ExtractWorkers  int `toml:"extract_workers"`
}

type Index struct {
	MaxTopicEntries int `toml:"max_topic_entries"`
}

type Voice struct {
	Enabled bool `toml:"enabled"`
}

type Config struct {
	Models     Models     `toml:"models"`
	Thresholds Thresholds `toml:"thresholds"`
	Paths      Paths      `toml:"paths"`
	Batching   Batching   `toml:"batching"`
	Index      Index      `toml:"index"`
	Voice      Voice      `toml:"voice"`
}

func Defaults() Config {
	return Config{
		Models: Models{
			Cheap:     "claude-haiku-4-5-20251001",
			Smart:     "claude-opus-4-7",
			Embedding: "voyage-3-lite",
		},
		Thresholds: Thresholds{
			RuleMinEvidenceCount:   2,
			RuleMinProjectCount:    2,
			VoiceMinEvidenceCount:  2,
			ClusterCosineThreshold: 0.85,
		},
		Paths: Paths{
			TranscriptsGlob: "~/.claude/projects/**/*.jsonl",
			OutputDir:       "~/.ghost",
		},
		Batching: Batching{
			DefaultLimit:   0,
			ExtractWorkers: 5,
		},
		Index: Index{MaxTopicEntries: 20},
		Voice: Voice{Enabled: false},
	}
}

// Load returns Defaults() overlaid with whatever fields are set in the TOML file at path.
// A missing file is not an error — defaults are returned.
func Load(path string) (Config, error) {
	cfg := Defaults()
	_, err := toml.DecodeFile(path, &cfg)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	return cfg, nil
}
```

- [ ] **Step 4: Write `internal/paths/paths.go`**

```go
package paths

import (
	"os"
	"path/filepath"
	"strings"
)

// Expand resolves a leading ~ to the user's home directory.
func Expand(p string) (string, error) {
	if !strings.HasPrefix(p, "~") {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p, err
	}
	return filepath.Join(home, strings.TrimPrefix(p, "~")), nil
}

// EnsureDir creates dir (and parents) if missing.
func EnsureDir(dir string) error {
	return os.MkdirAll(dir, 0o755)
}
```

- [ ] **Step 5: Run tests**

Run: `go get github.com/BurntSushi/toml && go test ./internal/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/config internal/paths go.mod go.sum
git commit -m "feat: config loader with baked-in defaults"
```

---

### Task 3: Transcript discovery + content hashing

**Files:**
- Create: `internal/transcript/discover.go`, `internal/transcript/discover_test.go`, `internal/transcript/hash.go`, `internal/transcript/hash_test.go`

- [ ] **Step 1: Write failing test `internal/transcript/hash_test.go`**

```go
package transcript

import (
	"os"
	"path/filepath"
	"testing"
)

func TestContentHashStable(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.jsonl")
	if err := os.WriteFile(p, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h1, err := ContentHash(p)
	if err != nil {
		t.Fatal(err)
	}
	h2, err := ContentHash(p)
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 || h1 == "" {
		t.Fatalf("hash unstable: %q vs %q", h1, h2)
	}
}

func TestContentHashChangesOnAppend(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.jsonl")
	_ = os.WriteFile(p, []byte("hello\n"), 0o644)
	h1, _ := ContentHash(p)
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o644)
	_, _ = f.WriteString("world\n")
	_ = f.Close()
	h2, _ := ContentHash(p)
	if h1 == h2 {
		t.Fatalf("hash should change after append")
	}
}
```

- [ ] **Step 2: Implement `internal/transcript/hash.go`**

```go
package transcript

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
)

// ContentHash returns "sha256:<hex>" for the file's full contents.
func ContentHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}
```

- [ ] **Step 3: Write failing test `internal/transcript/discover_test.go`**

```go
package transcript

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDiscoverIgnoresRecentlyModified(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "old.jsonl")
	fresh := filepath.Join(dir, "fresh.jsonl")
	_ = os.WriteFile(old, []byte("{}\n"), 0o644)
	_ = os.WriteFile(fresh, []byte("{}\n"), 0o644)
	// Backdate `old` by 10 minutes.
	past := time.Now().Add(-10 * time.Minute)
	_ = os.Chtimes(old, past, past)

	got, err := Discover(filepath.Join(dir, "*.jsonl"), 5*time.Minute, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Path != old {
		t.Fatalf("expected only %q; got %+v", old, got)
	}
}

func TestProjectFromPath(t *testing.T) {
	in := "/Users/sarah/.claude/projects/-Users-sarah-dev-projects/abc.jsonl"
	if p := projectFromPath(in); p != "Users-sarah-dev-projects" {
		t.Fatalf("project = %q", p)
	}
}
```

- [ ] **Step 4: Implement `internal/transcript/discover.go`**

```go
package transcript

import (
	"path/filepath"
	"strings"
	"time"

	doublestar "github.com/bmatcuk/doublestar/v4"
)

// Transcript is a discovered file with metadata.
type Transcript struct {
	Path    string
	Project string
	ModTime time.Time
}

// Discover returns transcripts matching glob whose mtime is older than
// `now - activeWindow` (the active-session skip from the spec).
func Discover(glob string, activeWindow time.Duration, now time.Time) ([]Transcript, error) {
	matches, err := doublestar.FilepathGlob(glob)
	if err != nil {
		return nil, err
	}
	cutoff := now.Add(-activeWindow)
	out := make([]Transcript, 0, len(matches))
	for _, p := range matches {
		fi, err := osStat(p)
		if err != nil {
			continue
		}
		if fi.ModTime().After(cutoff) {
			continue
		}
		out = append(out, Transcript{
			Path:    p,
			Project: projectFromPath(p),
			ModTime: fi.ModTime(),
		})
	}
	return out, nil
}

// projectFromPath extracts the project segment from a Claude Code transcript path.
// Claude Code encodes cwd into the parent directory name, prefixed with `-`.
func projectFromPath(p string) string {
	parent := filepath.Base(filepath.Dir(p))
	return strings.TrimPrefix(parent, "-")
}
```

Add a tiny `os.Stat` indirection so tests stay simple:

```go
// in discover.go
import "os"
var osStat = os.Stat
```

- [ ] **Step 5: Run tests**

Run: `go get github.com/bmatcuk/doublestar/v4 && go test ./internal/transcript/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/transcript go.mod go.sum
git commit -m "feat: transcript discovery with active-session skip and content hashing"
```

---

### Task 4: Ledger

**Files:**
- Create: `internal/ledger/ledger.go`, `internal/ledger/ledger_test.go`

- [ ] **Step 1: Write failing test `internal/ledger/ledger_test.go`**

```go
package ledger

import (
	"path/filepath"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ledger.json")

	l, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if l.SchemaVersion != 1 {
		t.Fatalf("default schema version = %d", l.SchemaVersion)
	}

	l.Mark("conv-a", Entry{
		ContentHash:      "sha256:abc",
		ObservationsFile: ".state/observations/abc.json",
		MessageCount:     10,
	})
	if err := l.Save(path); err != nil {
		t.Fatal(err)
	}

	l2, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := l2.Conversations["conv-a"].ContentHash; got != "sha256:abc" {
		t.Fatalf("conv-a content_hash = %q", got)
	}
}

func TestNeedsProcessing(t *testing.T) {
	l := New()
	l.Mark("conv-a", Entry{ContentHash: "sha256:abc"})
	if !l.NeedsProcessing("conv-b", "sha256:zzz") {
		t.Fatalf("unknown conv should need processing")
	}
	if l.NeedsProcessing("conv-a", "sha256:abc") {
		t.Fatalf("matching hash should not need processing")
	}
	if !l.NeedsProcessing("conv-a", "sha256:def") {
		t.Fatalf("changed hash should need processing")
	}
}

func TestForget(t *testing.T) {
	l := New()
	l.Mark("conv-a", Entry{ContentHash: "sha256:abc"})
	if !l.Forget("conv-a") {
		t.Fatalf("Forget should report removed")
	}
	if l.Forget("conv-a") {
		t.Fatalf("Forget on missing should return false")
	}
}
```

- [ ] **Step 2: Implement `internal/ledger/ledger.go`**

```go
package ledger

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"sync"
	"time"
)

const CurrentSchemaVersion = 1

type Entry struct {
	ContentHash      string    `json:"content_hash"`
	ProcessedAt      time.Time `json:"processed_at"`
	ObservationsFile string    `json:"observations_file"`
	MessageCount     int       `json:"message_count"`
}

type LastCompose struct {
	At              time.Time `json:"at"`
	StagesRun       []string  `json:"stages_run"`
	PromptsVersion  string    `json:"prompts_version"`
}

type Ledger struct {
	mu            sync.Mutex
	SchemaVersion int                `json:"schema_version"`
	Conversations map[string]Entry   `json:"conversations"`
	LastCompose   *LastCompose       `json:"last_compose,omitempty"`
}

func New() *Ledger {
	return &Ledger{
		SchemaVersion: CurrentSchemaVersion,
		Conversations: map[string]Entry{},
	}
}

// Load reads ledger.json from path, or returns an empty ledger if absent.
func Load(path string) (*Ledger, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return New(), nil
		}
		return nil, err
	}
	l := New()
	if err := json.Unmarshal(b, l); err != nil {
		return nil, err
	}
	if l.Conversations == nil {
		l.Conversations = map[string]Entry{}
	}
	return l, nil
}

// Save atomically writes the ledger to path.
func (l *Ledger) Save(path string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	b, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (l *Ledger) Mark(path string, e Entry) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if e.ProcessedAt.IsZero() {
		e.ProcessedAt = time.Now().UTC()
	}
	l.Conversations[path] = e
}

func (l *Ledger) NeedsProcessing(path, contentHash string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	cur, ok := l.Conversations[path]
	if !ok {
		return true
	}
	return cur.ContentHash != contentHash
}

func (l *Ledger) Forget(path string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.Conversations[path]; !ok {
		return false
	}
	delete(l.Conversations, path)
	return true
}

func (l *Ledger) SetLastCompose(stages []string, promptsVersion string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.LastCompose = &LastCompose{
		At:             time.Now().UTC(),
		StagesRun:      stages,
		PromptsVersion: promptsVersion,
	}
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/ledger/...`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/ledger
git commit -m "feat: ledger with content-hash-based processing detection"
```

---

### Task 5: JSONL transcript parser

**Files:**
- Create: `internal/transcript/parse.go`, `internal/transcript/parse_test.go`, `internal/transcript/testdata/sample.jsonl`

- [ ] **Step 1: Inspect a real transcript so the type matches**

Run: `ls ~/.claude/projects/ | head -3 && head -2 "$(find ~/.claude/projects -name '*.jsonl' | head -1)"`
Expected: shows the JSONL shape. Each line is one event. We only need: role, content (text), and a turn index.

(Why this step is here: the exact JSONL schema isn't documented in the spec — read it once, then write the test against the real shape.)

- [ ] **Step 2: Write `internal/transcript/testdata/sample.jsonl`**

Use 3–5 representative lines copied (and lightly trimmed) from the real file. Include at least one user message and one assistant message.

- [ ] **Step 3: Write failing test `internal/transcript/parse_test.go`**

```go
package transcript

import (
	"testing"
)

func TestParseReturnsTurns(t *testing.T) {
	turns, err := Parse("testdata/sample.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) == 0 {
		t.Fatal("expected at least one turn")
	}
	var sawUser, sawAssistant bool
	for _, tn := range turns {
		switch tn.Role {
		case "user":
			sawUser = true
		case "assistant":
			sawAssistant = true
		}
		if tn.Text == "" {
			t.Errorf("turn %d has empty text", tn.Index)
		}
	}
	if !sawUser || !sawAssistant {
		t.Fatalf("expected both roles; user=%v assistant=%v", sawUser, sawAssistant)
	}
}
```

- [ ] **Step 4: Implement `internal/transcript/parse.go`**

```go
package transcript

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
)

type Turn struct {
	Index int
	Role  string // "user" | "assistant" | "system" | "tool"
	Text  string
}

// rawEvent is a minimal projection over the Claude Code JSONL schema —
// only the fields extract needs. Unknown fields are ignored.
type rawEvent struct {
	Type    string `json:"type"`
	Role    string `json:"role"`
	Message struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
	Content json.RawMessage `json:"content"`
}

// Parse reads a Claude Code transcript JSONL and returns one Turn per message
// event (user/assistant), with content blocks flattened to text.
func Parse(path string) ([]Turn, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var turns []Turn
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 16<<20) // allow large lines
	idx := 0
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev rawEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue // skip malformed lines rather than abort
		}
		role, content := pickRoleAndContent(ev)
		if role == "" || len(content) == 0 {
			continue
		}
		text := flattenContent(content)
		if strings.TrimSpace(text) == "" {
			continue
		}
		turns = append(turns, Turn{Index: idx, Role: role, Text: text})
		idx++
	}
	return turns, sc.Err()
}

func pickRoleAndContent(ev rawEvent) (string, json.RawMessage) {
	if ev.Message.Role != "" {
		return ev.Message.Role, ev.Message.Content
	}
	return ev.Role, ev.Content
}

// flattenContent handles both string content and content-block arrays:
//   - "hello"                              → "hello"
//   - [{"type":"text","text":"hello"}]     → "hello"
// Tool-use / tool-result blocks are skipped.
func flattenContent(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return ""
	}
	var b strings.Builder
	for _, blk := range blocks {
		if blk.Type == "text" && blk.Text != "" {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(blk.Text)
		}
	}
	return b.String()
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/transcript/...`
Expected: PASS. If the schema is different from what `rawEvent` expects, adjust based on what you saw in Step 1.

- [ ] **Step 6: Commit**

```bash
git add internal/transcript
git commit -m "feat: JSONL transcript parser flattening content blocks to turns"
```

---

### Task 6: Secret scrubbing

**Files:**
- Create: `internal/secrets/scrub.go`, `internal/secrets/scrub_test.go`

- [ ] **Step 1: Write failing test `internal/secrets/scrub_test.go`**

```go
package secrets

import "testing"

func TestDetectsCommonPatterns(t *testing.T) {
	samples := []string{
		"sk-ant-api03-abc123def456ghi789jkl012",
		"ghp_aBcDeFgHiJkLmNoPqRsTuVwXyZ0123456789",
		"AKIAIOSFODNN7EXAMPLE",
		"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NSJ9.abc",
		"Authorization: Bearer xyz123abc",
		"-----BEGIN RSA PRIVATE KEY-----",
	}
	for _, s := range samples {
		hit, pat := Detect(s)
		if !hit {
			t.Errorf("expected hit for %q (got pat=%q)", s, pat)
		}
	}
}

func TestDoesNotFlagNormalText(t *testing.T) {
	clean := []string{
		"the user prefers integration tests",
		"break comments at end of thought",
		"works at Miro on Content Security team",
	}
	for _, s := range clean {
		if hit, pat := Detect(s); hit {
			t.Errorf("false positive on %q (pat=%q)", s, pat)
		}
	}
}
```

- [ ] **Step 2: Implement `internal/secrets/scrub.go`**

```go
package secrets

import "regexp"

// pattern is a named regex used for detection + logging.
type pattern struct {
	name string
	re   *regexp.Regexp
}

var patterns = []pattern{
	{"anthropic_key", regexp.MustCompile(`sk-ant-[A-Za-z0-9_\-]{20,}`)},
	{"openai_key", regexp.MustCompile(`sk-[A-Za-z0-9]{20,}`)},
	{"github_token", regexp.MustCompile(`gh[pous]_[A-Za-z0-9]{30,}`)},
	{"aws_access_key", regexp.MustCompile(`\b(?:AKIA|ASIA)[A-Z0-9]{16}\b`)},
	{"jwt", regexp.MustCompile(`eyJ[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{10,}\.[A-Za-z0-9_\-]{5,}`)},
	{"bearer_header", regexp.MustCompile(`(?i)Authorization:\s*Bearer\s+[A-Za-z0-9._\-]{8,}`)},
	{"pem_block", regexp.MustCompile(`-----BEGIN [A-Z ]+ KEY-----`)},
	// Long high-entropy hex/base64 run, length-bounded to keep false positives down.
	{"long_hex", regexp.MustCompile(`\b[0-9a-f]{48,}\b`)},
	{"long_base64", regexp.MustCompile(`\b[A-Za-z0-9+/]{48,}={0,2}\b`)},
}

// Detect returns (true, patternName) if any pattern matches.
func Detect(s string) (bool, string) {
	for _, p := range patterns {
		if p.re.MatchString(s) {
			return true, p.name
		}
	}
	return false, ""
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/secrets/...`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/secrets
git commit -m "feat: secret pattern detector for extract scrubbing"
```

---

### Task 7: Atomic file writes

**Files:**
- Create: `internal/atomicfs/write.go`, `internal/atomicfs/write_test.go`

- [ ] **Step 1: Write failing test `internal/atomicfs/write_test.go`**

```go
package atomicfs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAtomicCreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")
	if err := WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "hello" {
		t.Fatalf("contents = %q", string(b))
	}
}

func TestWriteAtomicLeavesNoTmpOnSuccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")
	if err := WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("expected 1 file, got %d: %v", len(entries), entries)
	}
}
```

- [ ] **Step 2: Implement `internal/atomicfs/write.go`**

```go
package atomicfs

import (
	"os"
	"path/filepath"
)

// WriteFile writes data to a sibling temp file in the same directory and
// renames into place. The rename is atomic on POSIX filesystems.
func WriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".tmp-"+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Chmod(perm); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/atomicfs/...`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/atomicfs
git commit -m "feat: atomic file write helper"
```

---

### Task 8: Observation schema and validation

**Files:**
- Create: `internal/extract/schema.go`, `internal/extract/schema_test.go`

- [ ] **Step 1: Write failing test `internal/extract/schema_test.go`**

```go
package extract

import "testing"

func TestValidateAcceptsAllKinds(t *testing.T) {
	cases := []Observation{
		{Kind: "identity", Text: "works at Miro", Evidence: "turn 1"},
		{Kind: "rule", Text: "don't mock the database", Evidence: "turn 9"},
		{Kind: "topic", Topic: "testing", Text: "integration > mocks", Evidence: "turn 9"},
		{Kind: "voice", Context: "cli-chat", Text: "lowercase fragments", Evidence: "turns 3,7"},
	}
	for _, c := range cases {
		if err := c.Validate(); err != nil {
			t.Errorf("Validate(%+v) = %v", c, err)
		}
	}
}

func TestValidateRejectsBadKind(t *testing.T) {
	o := Observation{Kind: "feeling", Text: "x", Evidence: "y"}
	if err := o.Validate(); err == nil {
		t.Fatal("expected error for unknown kind")
	}
}

func TestValidateRejectsMissingSubKey(t *testing.T) {
	bad := []Observation{
		{Kind: "voice", Text: "x", Evidence: "y"},                  // missing Context
		{Kind: "topic", Text: "x", Evidence: "y"},                  // missing Topic
		{Kind: "identity", Text: "", Evidence: "y"},                // missing Text
		{Kind: "identity", Text: "x"},                              // missing Evidence
	}
	for _, o := range bad {
		if err := o.Validate(); err == nil {
			t.Errorf("expected error for %+v", o)
		}
	}
}
```

- [ ] **Step 2: Implement `internal/extract/schema.go`**

```go
package extract

import (
	"errors"
	"fmt"
	"time"
)

type Observation struct {
	Kind       string `json:"kind"`                 // identity | rule | topic | voice
	Text       string `json:"text"`
	Evidence   string `json:"evidence"`
	Confidence string `json:"confidence,omitempty"`
	Topic      string `json:"topic,omitempty"`      // required if Kind=topic
	Context    string `json:"context,omitempty"`    // required if Kind=voice
}

type ObservationsFile struct {
	Source       string        `json:"source"`
	Project      string        `json:"project"`
	ContentHash  string        `json:"content_hash"`
	ExtractedAt  time.Time     `json:"extracted_at"`
	Observations []Observation `json:"observations"`
}

var validKinds = map[string]bool{
	"identity": true, "rule": true, "topic": true, "voice": true,
}

func (o Observation) Validate() error {
	if !validKinds[o.Kind] {
		return fmt.Errorf("invalid kind %q", o.Kind)
	}
	if o.Text == "" {
		return errors.New("text required")
	}
	if o.Evidence == "" {
		return errors.New("evidence required")
	}
	if o.Kind == "topic" && o.Topic == "" {
		return errors.New("topic kind requires topic field")
	}
	if o.Kind == "voice" && o.Context == "" {
		return errors.New("voice kind requires context field")
	}
	return nil
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/extract/...`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/extract
git commit -m "feat: observation schema and per-record validation"
```

---

### Task 9: Anthropic client wrapper

**Files:**
- Create: `internal/anthropic/client.go`

- [ ] **Step 1: Define the interface and implementation**

```go
package anthropic

import (
	"context"
	"fmt"
	"os"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// Client is the surface extract needs. Keep it tiny so tests can fake it.
type Client interface {
	Complete(ctx context.Context, model, system, user string) (string, error)
}

type sdkClient struct{ c *sdk.Client }

func New() (Client, error) {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("ANTHROPIC_API_KEY not set")
	}
	c := sdk.NewClient(option.WithAPIKey(key))
	return &sdkClient{c: &c}, nil
}

func (s *sdkClient) Complete(ctx context.Context, model, system, user string) (string, error) {
	resp, err := s.c.Messages.New(ctx, sdk.MessageNewParams{
		Model:     sdk.F(model),
		MaxTokens: sdk.F(int64(4096)),
		System:    sdk.F([]sdk.TextBlockParam{sdk.NewTextBlock(system)}),
		Messages: sdk.F([]sdk.MessageParam{
			sdk.NewUserMessage(sdk.NewTextBlock(user)),
		}),
	})
	if err != nil {
		return "", err
	}
	var out string
	for _, blk := range resp.Content {
		if blk.Type == sdk.ContentBlockTypeText {
			out += blk.Text
		}
	}
	return out, nil
}
```

- [ ] **Step 2: Add dependency, verify build**

Run:
```bash
go get github.com/anthropics/anthropic-sdk-go@latest
go build ./...
```
Expected: builds cleanly. If the SDK surface differs from above (it has changed historically), adjust to the current SDK shape — but keep the `Client` interface stable so callers don't change.

- [ ] **Step 3: Commit**

```bash
git add internal/anthropic go.mod go.sum
git commit -m "feat: thin Anthropic SDK wrapper exposing Complete()"
```

---

### Task 10: Extract stage

**Files:**
- Create: `internal/extract/prompt.go`, `prompts/extract.system.md`, `internal/extract/extract.go`, `internal/extract/extract_test.go`, `internal/extract/testdata/golden_transcript.jsonl`

- [ ] **Step 1: Write `prompts/extract.system.md`**

```markdown
You read one Claude Code conversation and emit atomic observations about the user.

Output strict JSON of the shape:

{
  "observations": [
    {"kind": "identity"|"rule"|"topic"|"voice", "text": "...", "evidence": "turn N: ...", "confidence": "high"|"medium"|"low", "topic": "<required if kind=topic>", "context": "<required if kind=voice>"}
  ]
}

Rules:
- "identity": third-person facts about who the user is (role, team, stack, organization).
- "rule": durable preferences for how Claude should behave with them. Must be stated as an instruction or correction by the user.
- "topic": preferences scoped to a specific domain (testing, git, writing, etc.). Always include a "topic" slug.
- "voice": observations about how the user writes in a specific register. Always include a "context" slug. Default context is "cli-chat" (the user talking to Claude). Use other contexts (annual-review, slack, exec-brief) ONLY when the transcript shows the user drafting or pasting material destined for that register. When uncertain, drop the observation.
- Every observation cites "turn N: <short quote>" as evidence.
- Prefer dropping over guessing. Empty observations array is valid.
- No prose outside the JSON object.
```

- [ ] **Step 2: Write `internal/extract/prompt.go`**

```go
package extract

import _ "embed"

//go:embed ../../prompts/extract.system.md
var systemPrompt string

// SystemPrompt returns the embedded extract system prompt.
func SystemPrompt() string { return systemPrompt }
```

- [ ] **Step 3: Write `internal/extract/extract.go`**

```go
package extract

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/SarahFrankle/ghost/internal/anthropic"
	"github.com/SarahFrankle/ghost/internal/secrets"
	"github.com/SarahFrankle/ghost/internal/transcript"
)

// Logger is the minimal sink extract uses to report dropped records.
type Logger interface {
	Printf(format string, args ...any)
}

type Runner struct {
	Client anthropic.Client
	Model  string
	Log    Logger
}

// Run extracts observations from one transcript.
// On success, returns a populated ObservationsFile. Malformed and
// secret-bearing observations are silently dropped (logged via r.Log).
func (r *Runner) Run(ctx context.Context, t transcript.Transcript, contentHash string) (ObservationsFile, error) {
	turns, err := transcript.Parse(t.Path)
	if err != nil {
		return ObservationsFile{}, err
	}
	if len(turns) == 0 {
		return ObservationsFile{
			Source:       t.Path,
			Project:      t.Project,
			ContentHash:  contentHash,
			ExtractedAt:  time.Now().UTC(),
			Observations: []Observation{},
		}, nil
	}

	userPayload := renderTurns(turns)
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
		kept = append(kept, o)
	}

	return ObservationsFile{
		Source:       t.Path,
		Project:      t.Project,
		ContentHash:  contentHash,
		ExtractedAt:  time.Now().UTC(),
		Observations: kept,
	}, nil
}

func (r *Runner) logf(format string, args ...any) {
	if r.Log != nil {
		r.Log.Printf(format, args...)
	}
}

func renderTurns(turns []transcript.Turn) string {
	var b strings.Builder
	for _, t := range turns {
		fmt.Fprintf(&b, "turn %d (%s): %s\n", t.Index, t.Role, t.Text)
	}
	return b.String()
}

// parseObservations is permissive about leading/trailing prose around the JSON.
func parseObservations(raw string) ([]Observation, error) {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("no JSON object found")
	}
	var wrap struct {
		Observations []Observation `json:"observations"`
	}
	if err := json.Unmarshal([]byte(raw[start:end+1]), &wrap); err != nil {
		return nil, err
	}
	return wrap.Observations, nil
}
```

- [ ] **Step 4: Write `internal/extract/extract_test.go` using a fake client**

```go
package extract

import (
	"context"
	"strings"
	"testing"

	"github.com/SarahFrankle/ghost/internal/transcript"
)

type fakeClient struct{ resp string }

func (f *fakeClient) Complete(ctx context.Context, model, system, user string) (string, error) {
	return f.resp, nil
}

func TestRunDropsInvalidAndSecretObservations(t *testing.T) {
	fake := &fakeClient{resp: `{
		"observations": [
			{"kind":"identity","text":"works at Miro","evidence":"turn 1"},
			{"kind":"rule","text":"API key is sk-ant-api03-aaaaaaaaaaaaaaaaaaaaaaa","evidence":"turn 2"},
			{"kind":"voice","text":"lowercase","evidence":"turn 3"}
		]
	}`}
	r := &Runner{Client: fake, Model: "test-model"}

	out, err := r.Run(context.Background(), transcript.Transcript{
		Path: "testdata/golden_transcript.jsonl", Project: "p1",
	}, "sha256:abc")
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Observations) != 1 {
		t.Fatalf("expected 1 kept obs, got %d: %+v", len(out.Observations), out.Observations)
	}
	if !strings.Contains(out.Observations[0].Text, "Miro") {
		t.Fatalf("kept wrong observation: %+v", out.Observations[0])
	}
}
```

- [ ] **Step 5: Provide `internal/extract/testdata/golden_transcript.jsonl`**

A minimal 2-line file — the test doesn't inspect its content (the fake client returns canned output), but `transcript.Parse` must succeed. Reuse `internal/transcript/testdata/sample.jsonl` shape:

```json
{"type":"user","message":{"role":"user","content":"hello"}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"hi"}]}}
```

- [ ] **Step 6: Run tests**

Run: `go test ./internal/extract/...`
Expected: PASS. Three observations in, one survives validation + scrubbing.

- [ ] **Step 7: Commit**

```bash
git add internal/extract prompts/extract.system.md
git commit -m "feat: extract stage with schema validation and secret scrubbing"
```

---

### Task 11: `ghost compose --stages extract`

**Files:**
- Create: `cmd/compose.go`

- [ ] **Step 1: Implement `cmd/compose.go`**

```go
package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/SarahFrankle/ghost/internal/anthropic"
	"github.com/SarahFrankle/ghost/internal/atomicfs"
	"github.com/SarahFrankle/ghost/internal/config"
	"github.com/SarahFrankle/ghost/internal/extract"
	"github.com/SarahFrankle/ghost/internal/ledger"
	"github.com/SarahFrankle/ghost/internal/paths"
	"github.com/SarahFrankle/ghost/internal/transcript"
)

var (
	composeLimit  int
	composeStages string
	composeDry    bool
)

var composeCmd = &cobra.Command{
	Use:   "compose",
	Short: "Run the ghost compose pipeline",
	RunE: func(cmd *cobra.Command, args []string) error {
		stages := strings.Split(composeStages, ",")
		if len(stages) != 1 || stages[0] != "extract" {
			return fmt.Errorf("phase 1 supports only --stages extract (got %q)", composeStages)
		}
		return runExtract(cmd.Context())
	},
}

func init() {
	composeCmd.Flags().IntVar(&composeLimit, "limit", 0, "process at most N unprocessed transcripts (0 = all)")
	composeCmd.Flags().StringVar(&composeStages, "stages", "extract", "comma-separated stages (phase 1: extract only)")
	composeCmd.Flags().BoolVar(&composeDry, "dry-run", false, "list what would be processed and exit")
	rootCmd.AddCommand(composeCmd)
}

func runExtract(ctx context.Context) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	outDir, err := paths.Expand(cfg.Paths.OutputDir)
	if err != nil {
		return err
	}
	stateDir := filepath.Join(outDir, ".state")
	obsDir := filepath.Join(stateDir, "observations")
	if err := paths.EnsureDir(obsDir); err != nil {
		return err
	}

	ledgerPath := filepath.Join(stateDir, "ledger.json")
	l, err := ledger.Load(ledgerPath)
	if err != nil {
		return err
	}

	glob, err := paths.Expand(cfg.Paths.TranscriptsGlob)
	if err != nil {
		return err
	}
	transcripts, err := transcript.Discover(glob, 5*time.Minute, time.Now())
	if err != nil {
		return err
	}

	type job struct {
		t    transcript.Transcript
		hash string
	}
	pending := make([]job, 0, len(transcripts))
	for _, t := range transcripts {
		h, err := transcript.ContentHash(t.Path)
		if err != nil {
			log.Printf("hash %s: %v", t.Path, err)
			continue
		}
		if !l.NeedsProcessing(t.Path, h) {
			continue
		}
		pending = append(pending, job{t: t, hash: h})
	}
	sort.Slice(pending, func(i, j int) bool {
		return pending[i].t.ModTime.Before(pending[j].t.ModTime)
	})
	if composeLimit > 0 && len(pending) > composeLimit {
		pending = pending[:composeLimit]
	}

	if composeDry {
		fmt.Printf("would process %d transcript(s):\n", len(pending))
		for _, p := range pending {
			fmt.Printf("  %s\n", p.t.Path)
		}
		return nil
	}
	if len(pending) == 0 {
		fmt.Println("nothing to do")
		return nil
	}

	client, err := anthropic.New()
	if err != nil {
		return err
	}
	runner := &extract.Runner{
		Client: client,
		Model:  cfg.Models.Cheap,
		Log:    log.Default(),
	}

	workers := cfg.Batching.ExtractWorkers
	if workers < 1 {
		workers = 1
	}
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	var processed, failed int
	var mu sync.Mutex

	for _, j := range pending {
		j := j
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			result, err := runner.Run(ctx, j.t, j.hash)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				log.Printf("extract %s: %v", j.t.Path, err)
				failed++
				return
			}
			obsFileName := observationsFileName(j.hash) + ".json"
			obsRelPath := filepath.Join(".state/observations", obsFileName)
			obsAbsPath := filepath.Join(stateDir, "observations", obsFileName)

			body, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				log.Printf("marshal %s: %v", j.t.Path, err)
				failed++
				return
			}
			if err := atomicfs.WriteFile(obsAbsPath, body, 0o644); err != nil {
				log.Printf("write %s: %v", obsAbsPath, err)
				failed++
				return
			}
			l.Mark(j.t.Path, ledger.Entry{
				ContentHash:      j.hash,
				ObservationsFile: obsRelPath,
				MessageCount:     len(result.Observations),
			})
			processed++
			fmt.Printf("extracted %d observation(s) from %s\n", len(result.Observations), filepath.Base(j.t.Path))
		}()
	}
	wg.Wait()

	l.SetLastCompose([]string{"extract"}, "")
	if err := l.Save(ledgerPath); err != nil {
		return err
	}
	fmt.Printf("done: processed=%d failed=%d\n", processed, failed)
	return nil
}

func observationsFileName(contentHash string) string {
	trimmed := strings.TrimPrefix(contentHash, "sha256:")
	if len(trimmed) > 16 {
		trimmed = trimmed[:16]
	}
	// also include a salt of the full hash so the prefix collision risk drops to nil
	sum := sha256.Sum256([]byte(contentHash))
	return trimmed + "-" + hex.EncodeToString(sum[:4])
}

func loadConfig() (config.Config, error) {
	path := configPath
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return config.Config{}, err
		}
		path = filepath.Join(home, ".ghost", "config.toml")
	}
	return config.Load(path)
}
```

- [ ] **Step 2: Verify build**

Run: `go build ./...`
Expected: clean build.

- [ ] **Step 3: Smoke-test against a real transcript (with a tiny limit)**

Run: `ANTHROPIC_API_KEY=$ANTHROPIC_API_KEY ./ghost compose --limit 1`
Expected: one line per processed transcript, ledger and observations file written under `~/.ghost/.state/`.

If something looks off, fix the bug and rerun. Do not commit until the smoke test passes.

- [ ] **Step 4: Commit**

```bash
git add cmd/compose.go
git commit -m "feat: ghost compose --stages extract with bounded worker pool"
```

---

### Task 12: `ghost status`

**Files:**
- Create: `cmd/status.go`

- [ ] **Step 1: Implement `cmd/status.go`**

```go
package cmd

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/SarahFrankle/ghost/internal/ledger"
	"github.com/SarahFrankle/ghost/internal/paths"
	"github.com/SarahFrankle/ghost/internal/transcript"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show ledger summary: total / processed / pending / dirty",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		outDir, _ := paths.Expand(cfg.Paths.OutputDir)
		ledgerPath := filepath.Join(outDir, ".state", "ledger.json")
		l, err := ledger.Load(ledgerPath)
		if err != nil {
			return err
		}

		glob, _ := paths.Expand(cfg.Paths.TranscriptsGlob)
		transcripts, err := transcript.Discover(glob, 5*time.Minute, time.Now())
		if err != nil {
			return err
		}

		var processed, pending, dirty int
		for _, t := range transcripts {
			h, err := transcript.ContentHash(t.Path)
			if err != nil {
				continue
			}
			entry, ok := l.Conversations[t.Path]
			switch {
			case !ok:
				pending++
			case entry.ContentHash != h:
				dirty++
			default:
				processed++
			}
		}

		fmt.Printf("transcripts: total=%d  processed=%d  pending=%d  dirty=%d\n",
			len(transcripts), processed, pending, dirty)
		if l.LastCompose != nil {
			fmt.Printf("last compose: %s (stages: %v)\n", l.LastCompose.At.Format(time.RFC3339), l.LastCompose.StagesRun)
		} else {
			fmt.Println("last compose: never")
		}
		return nil
	},
}

func init() { rootCmd.AddCommand(statusCmd) }
```

- [ ] **Step 2: Verify**

Run: `go build ./... && ./ghost status`
Expected: prints totals; matches what you saw from `compose --limit 1`.

- [ ] **Step 3: Commit**

```bash
git add cmd/status.go
git commit -m "feat: ghost status command"
```

---

### Task 13: `ghost forget` and `ghost show observations`

**Files:**
- Create: `cmd/forget.go`, `cmd/show.go`

- [ ] **Step 1: Implement `cmd/forget.go`**

```go
package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/SarahFrankle/ghost/internal/ledger"
	"github.com/SarahFrankle/ghost/internal/paths"
)

var forgetCmd = &cobra.Command{
	Use:   "forget <transcript-path>",
	Short: "Drop a conversation's observations and ledger entry",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		target := args[0]
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		outDir, _ := paths.Expand(cfg.Paths.OutputDir)
		ledgerPath := filepath.Join(outDir, ".state", "ledger.json")
		l, err := ledger.Load(ledgerPath)
		if err != nil {
			return err
		}

		entry, ok := l.Conversations[target]
		if !ok {
			return fmt.Errorf("not in ledger: %s", target)
		}
		obsPath := filepath.Join(outDir, entry.ObservationsFile)
		if err := os.Remove(obsPath); err != nil && !os.IsNotExist(err) {
			return err
		}
		l.Forget(target)
		if err := l.Save(ledgerPath); err != nil {
			return err
		}
		fmt.Printf("forgot %s\n", target)
		fmt.Println("note: synthesis (when it exists) is now stale; rerun compose --stages cluster,synthesize when Phase 2 ships.")
		_ = ledger.CurrentSchemaVersion // referenced for go vet
		return nil
	},
}

func init() { rootCmd.AddCommand(forgetCmd) }
```

- [ ] **Step 2: Implement `cmd/show.go`**

```go
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/spf13/cobra"

	"github.com/SarahFrankle/ghost/internal/extract"
	"github.com/SarahFrankle/ghost/internal/ledger"
	"github.com/SarahFrankle/ghost/internal/paths"
)

var showRecent int

var showCmd = &cobra.Command{
	Use:   "show",
	Short: "Show ghost outputs",
}

var showObservationsCmd = &cobra.Command{
	Use:   "observations",
	Short: "Print recently extracted observations",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		outDir, _ := paths.Expand(cfg.Paths.OutputDir)
		l, err := ledger.Load(filepath.Join(outDir, ".state", "ledger.json"))
		if err != nil {
			return err
		}

		type row struct {
			path  string
			entry ledger.Entry
		}
		rows := make([]row, 0, len(l.Conversations))
		for p, e := range l.Conversations {
			rows = append(rows, row{p, e})
		}
		sort.Slice(rows, func(i, j int) bool {
			return rows[i].entry.ProcessedAt.After(rows[j].entry.ProcessedAt)
		})
		if showRecent > 0 && len(rows) > showRecent {
			rows = rows[:showRecent]
		}

		for _, r := range rows {
			full := filepath.Join(outDir, r.entry.ObservationsFile)
			body, err := os.ReadFile(full)
			if err != nil {
				fmt.Fprintf(os.Stderr, "skip %s: %v\n", full, err)
				continue
			}
			var f extract.ObservationsFile
			if err := json.Unmarshal(body, &f); err != nil {
				fmt.Fprintf(os.Stderr, "parse %s: %v\n", full, err)
				continue
			}
			fmt.Printf("\n=== %s (%s) — %d obs, processed %s\n",
				filepath.Base(r.path), f.Project, len(f.Observations),
				r.entry.ProcessedAt.Format(time.RFC3339))
			for _, o := range f.Observations {
				sub := o.Kind
				if o.Topic != "" {
					sub = o.Kind + ":" + o.Topic
				} else if o.Context != "" {
					sub = o.Kind + ":" + o.Context
				}
				fmt.Printf("  [%s] %s\n      ← %s\n", sub, o.Text, o.Evidence)
			}
		}
		return nil
	},
}

func init() {
	showObservationsCmd.Flags().IntVar(&showRecent, "recent", 5, "show observations from N most recent transcripts (0 = all)")
	showCmd.AddCommand(showObservationsCmd)
	rootCmd.AddCommand(showCmd)
}
```

- [ ] **Step 3: Verify end-to-end**

Run:
```bash
go build ./...
./ghost compose --limit 2
./ghost show observations --recent 2
./ghost forget "<one of the transcript paths shown by status>"
./ghost status
```
Expected: extract runs, show prints the kept observations with kind/topic/context decorations and evidence, forget drops one entry, status reflects the removal.

- [ ] **Step 4: Commit**

```bash
git add cmd/forget.go cmd/show.go
git commit -m "feat: ghost forget and ghost show observations"
```

---

## End of Phase 1

At this point you should be able to:

- Run `ghost compose --limit N` repeatedly, draining your transcript backlog in chunks.
- Use `ghost status` to see total / processed / pending / dirty counts.
- Use `ghost show observations --recent N` to hand-review extract quality.
- Use `ghost forget <path>` to drop a misleading conversation before Phase 2.

Phase 1 exit criteria from the spec: hand-review of ~20 transcripts' observations shows the cheap model captures identity / rules / topics / voice signals usefully and secret scrubbing drops credentials reliably. When that's verified, Phase 2 (clustering + synthesis MVP) gets its own plan.
