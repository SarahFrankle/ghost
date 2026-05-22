# Ghost Phase 3 — Lazy Loading Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend Phase 2 to produce lazy-loaded `topics/*.md` and a capped `index.md`, ship the `ghost` Claude Code skill that mechanically loads them, and add the v1 user-facing surface (`add-rule`, `show`, `install-skill`, `compose --estimate`) so the always-loaded core can stay small while domain guidance is reachable on demand.

**Architecture:**
- Stage 3 grows two new builders. `BuildTopics` groups all `kind=topic` clusters by `SubKey` and runs one smart-model call per topic, producing `topics/<slug>.md`. `BuildIndex` runs after topics, taking the resulting topic files plus their per-topic evidence/project counts, sorting by evidence desc, capping at `Index.MaxTopicEntries`, and asking the smart model to emit one index entry per topic with trigger phrases.
- The Pipeline's tmpdir orchestration is generalised to handle a `topics/` subdirectory and an additional top-level file (`index.md`), preserving the "all-or-nothing rename" property: synthesis either ships a new identity + rules + topics + index together or leaves the prior generation untouched.
- The runtime is a SKILL.md shipped as an embedded asset and installed by `ghost install-skill` into `~/.claude/skills/ghost/`. Claude Code loads it on demand based on its own skill-trigger logic; the file's body teaches Claude to read `~/.ghost/index.md` once per session, match triggers, and `Read` the matching topic file before responding.
- The user-facing CLI grows three small subcommands (`ghost add-rule`, an extended `ghost show`, `ghost install-skill`) and one new compose flag (`--estimate`). All are pure-Go; no LLM calls in the estimate path.
- Voice synthesis stays gated off (`[voice].enabled = false`). The codepath is plumbed defensively (config flag is read; when false, no voice clusters are processed and no `voice/` directory is created) so flipping the flag in a future release is a one-line change, but no prompt or output ships in this phase.

**Tech Stack:** Go 1.22+. All Phase 2 packages (`anthropic`, `atomicfs`, `cluster`, `config`, `synthesize`, `paths`) are reused. SKILL.md is embedded via `//go:embed` alongside the prompts. No new third-party deps.

**Spec reference:** `docs/specs/2026-05-20-ghost-design.md` — topics (lines 375–377), index cap (389, 422), SKILL.md (537–593), slash commands (594–605), estimate (514–520), Phase 3 phasing (717–728), migration (650–690).

**Phase 2 deferrals carried in:** None blocking. The new identity prompt has been verified session-agnostic on the real corpus. Cluster cosine threshold tuning is post-v1.

---

## File Structure

```
ghost/
  cmd/
    compose.go                       # --estimate flag + dispatch
    add_rule.go                      # ghost add-rule "<text>"
    install_skill.go                 # ghost install-skill
    show.go                          # extend with `ghost show` (no subcmd) and `topics`
    estimate.go                      # tokens-and-cost estimator (called from compose.go)
  internal/
    synthesize/
      topics.go                      # BuildTopics: cluster grouping + per-topic LLM call
      topics_test.go
      index.go                       # BuildIndex: rank, cap, LLM call for triggers
      index_test.go
      pipeline.go                    # extended: topics dir + index file in atomic write
      pipeline_test.go               # extended: partial-failure across new files
    pricing/
      pricing.go                     # model_id → (input $/MTok, output $/MTok)
      pricing_test.go
  prompts/
    synthesize.topics.system.md      # per-topic body prompt
    synthesize.index.system.md       # index-entry + triggers prompt
    prompts.go                       # two new accessors
  skill/
    SKILL.md                         # embedded; installed to ~/.claude/skills/ghost/
    skill.go                         # embed accessor + writer
    skill_test.go
  docs/
    migration-from-memory.md         # week-1 review + day-14 archive checklist
```

Each `_test.go` uses the existing `fakeClient` pattern (no real network or LLM calls).

---

## Task 1: Topic synthesis — grouping + rendering helper

**Files:**
- Create: `internal/synthesize/topics.go`
- Create: `internal/synthesize/topics_test.go`

- [ ] **Step 1: Write the failing test for grouping**

In `topics_test.go`:

```go
package synthesize

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/SarahFrankle/ghost/internal/cluster"
)

func TestGroupTopicClustersBySlug(t *testing.T) {
	cs := []cluster.Cluster{
		{Kind: "topic", SubKey: "testing", Canonical: "prefer table-driven", EvidenceCount: 4, ProjectCount: 2},
		{Kind: "topic", SubKey: "testing", Canonical: "avoid mocks at boundaries", EvidenceCount: 3, ProjectCount: 2},
		{Kind: "topic", SubKey: "git", Canonical: "rebase before merge", EvidenceCount: 5, ProjectCount: 3},
		{Kind: "rule", SubKey: "", Canonical: "should be skipped", EvidenceCount: 9, ProjectCount: 9},
		{Kind: "topic", SubKey: "", Canonical: "missing slug; skip", EvidenceCount: 2, ProjectCount: 2},
	}
	groups := GroupTopicClusters(cs)
	slugs := make([]string, 0, len(groups))
	for s := range groups {
		slugs = append(slugs, s)
	}
	sort.Strings(slugs)
	if strings.Join(slugs, ",") != "git,testing" {
		t.Fatalf("unexpected slugs: %v", slugs)
	}
	if len(groups["testing"]) != 2 || len(groups["git"]) != 1 {
		t.Fatalf("unexpected sizes: testing=%d git=%d", len(groups["testing"]), len(groups["git"]))
	}
}
```

- [ ] **Step 2: Run test, verify it fails**

Run: `go test ./internal/synthesize/ -run TestGroupTopicClustersBySlug -v`
Expected: FAIL (undefined: GroupTopicClusters).

- [ ] **Step 3: Implement grouping**

In `topics.go`:

```go
package synthesize

import (
	"context"
	"fmt"
	"strings"

	"github.com/SarahFrankle/ghost/internal/anthropic"
	"github.com/SarahFrankle/ghost/internal/cluster"
	"github.com/SarahFrankle/ghost/prompts"
)

// GroupTopicClusters partitions kind=topic clusters by SubKey (the
// topic slug). Clusters with kind != topic or an empty SubKey are
// dropped: a topic without a slug cannot be addressed by the index
// and would have nowhere to live on disk.
func GroupTopicClusters(cs []cluster.Cluster) map[string][]cluster.Cluster {
	out := map[string][]cluster.Cluster{}
	for _, c := range cs {
		if c.Kind != "topic" || strings.TrimSpace(c.SubKey) == "" {
			continue
		}
		out[c.SubKey] = append(out[c.SubKey], c)
	}
	return out
}
```

- [ ] **Step 4: Run test, verify it passes**

Run: `go test ./internal/synthesize/ -run TestGroupTopicClustersBySlug -v`
Expected: PASS.

- [ ] **Step 5: Write the failing test for BuildTopics single-call shape**

Append to `topics_test.go`:

```go
func TestBuildTopicsCallsModelOncePerSlug(t *testing.T) {
	f := &fakeClient{resp: "# Testing\n\n- prefer table-driven\n"}
	groups := map[string][]cluster.Cluster{
		"testing": {{Kind: "topic", SubKey: "testing", Canonical: "prefer table-driven",
			EvidenceCount: 4, ProjectCount: 2,
			Members: []cluster.ClusterMember{{Text: "prefer table-driven", Evidence: "turn 3", Project: "p"}}}},
		"git": {{Kind: "topic", SubKey: "git", Canonical: "rebase before merge",
			EvidenceCount: 5, ProjectCount: 3,
			Members: []cluster.ClusterMember{{Text: "rebase before merge", Evidence: "turn 7", Project: "q"}}}},
	}
	results := BuildTopics(context.Background(), f, "smart", groups)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	names := map[string]bool{}
	for _, r := range results {
		names[r.Name] = true
		if r.Err != nil {
			t.Fatalf("%s: %v", r.Name, r.Err)
		}
		if !strings.HasSuffix(r.Name, ".md") || !strings.HasPrefix(r.Name, "topics/") {
			t.Fatalf("unexpected output filename: %s", r.Name)
		}
	}
	if !names["topics/testing.md"] || !names["topics/git.md"] {
		t.Fatalf("missing expected file: %v", names)
	}
}
```

The existing `fakeClient` from `rules_test.go` only records the *last* call's `gotUser`. That is fine for this test (we only assert names and absence of error). A later test will use a per-call recorder.

- [ ] **Step 6: Run test, verify it fails**

Run: `go test ./internal/synthesize/ -run TestBuildTopicsCallsModelOncePerSlug -v`
Expected: FAIL (undefined: BuildTopics).

- [ ] **Step 7: Implement BuildTopics**

Append to `topics.go`:

```go
// BuildTopics produces one FileResult per topic slug. The caller is
// responsible for atomic placement; this function is pure
// (no filesystem). Output names use the "topics/<slug>.md" form so
// the pipeline can place them under a topics/ subdirectory verbatim.
func BuildTopics(ctx context.Context, client anthropic.Client, model string, groups map[string][]cluster.Cluster) []FileResult {
	results := make([]FileResult, 0, len(groups))
	for slug, cs := range groups {
		name := fmt.Sprintf("topics/%s.md", slug)
		payload := renderTopicPayload(slug, cs)
		raw, err := client.Complete(ctx, model, prompts.SynthesizeTopicsSystem(), payload)
		if err != nil {
			results = append(results, FileResult{Name: name, Err: fmt.Errorf("topic %s: %w", slug, err)})
			continue
		}
		results = append(results, FileResult{Name: name, Content: ensureTrailingNewline(strings.TrimSpace(raw))})
	}
	return results
}

func renderTopicPayload(slug string, cs []cluster.Cluster) string {
	var b strings.Builder
	fmt.Fprintf(&b, "TOPIC: %s\n\nCANDIDATE CLUSTERS:\n", slug)
	b.WriteString(renderClusters(cs))
	return b.String()
}
```

- [ ] **Step 8: Add the prompt accessor**

In `prompts/prompts.go`, append:

```go
//go:embed synthesize.topics.system.md
var synthesizeTopicsSystem string

func SynthesizeTopicsSystem() string { return synthesizeTopicsSystem }
```

- [ ] **Step 9: Write the topics prompt**

Create `prompts/synthesize.topics.system.md`:

```
You are writing one topic file (`topics/<slug>.md`) that Claude
Code will lazy-load when the user works on tasks matching that
topic. The file is reference material the user has already
implicitly agreed with through repeated feedback across sessions.

Your input is:
- The topic slug.
- A list of clusters for that topic, each with a canonical phrasing
  and one or more supporting member observations.

Hard rules:
- Markdown body only. Begin with a level-1 heading naming the topic
  in human form (e.g. "# Testing" for slug "testing").
- One bullet per durable preference. Imperative voice. No hedging.
- Group related bullets under level-2 subheadings only if there are
  at least three bullets that share a subtheme. Otherwise keep it
  flat.
- Do not invent guidance absent from the cluster set. Do not
  paraphrase away the user's specificity.
- No em-dashes. No throat-clearing. Delete sentences you wouldn't
  miss.
- Single-project topics are valid — do not refuse to write a topic
  just because every cluster is from one project. Cross-project
  signal is enforced upstream by the rules-vs-topics split, not by
  you.
```

- [ ] **Step 10: Run tests, verify they pass**

Run: `go test ./internal/synthesize/ -run TestBuildTopics -v` and `go build ./...`
Expected: PASS, build succeeds.

- [ ] **Step 11: Commit**

```bash
git add internal/synthesize/topics.go internal/synthesize/topics_test.go \
        prompts/synthesize.topics.system.md prompts/prompts.go
git commit -m "feat(synthesize): build per-topic markdown from topic clusters"
```

---

## Task 2: Pipeline writes topics atomically alongside identity + rules

**Files:**
- Modify: `internal/synthesize/pipeline.go`
- Modify: `internal/synthesize/pipeline_test.go`

- [ ] **Step 1: Write the failing test**

Add to `pipeline_test.go`:

```go
func TestPipelineWritesTopicsSubdir(t *testing.T) {
	dir := t.TempDir()
	f := &fakeClient{resp: "# Out\n\nbody.\n"}
	p := &Pipeline{
		Client: f, SmartModel: "smart", GhostDir: dir,
		MinRuleEvidence: 2, MinRuleProjects: 2,
	}
	cf := cluster.ClustersFile{Clusters: []cluster.Cluster{
		{Kind: "identity", Canonical: "id", EvidenceCount: 2, ProjectCount: 2,
			Members: []cluster.ClusterMember{{Text: "id", Evidence: "t", Project: "p"}}},
		{Kind: "topic", SubKey: "testing", Canonical: "prefer table-driven",
			EvidenceCount: 3, ProjectCount: 2,
			Members: []cluster.ClusterMember{{Text: "prefer table-driven", Evidence: "t", Project: "p"}}},
	}}
	if err := p.Run(context.Background(), cf); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"identity.md", "rules.md", "topics/testing.md"} {
		if _, err := os.Stat(filepath.Join(dir, want)); err != nil {
			t.Fatalf("missing %s: %v", want, err)
		}
	}
}
```

- [ ] **Step 2: Run test, verify it fails**

Run: `go test ./internal/synthesize/ -run TestPipelineWritesTopicsSubdir -v`
Expected: FAIL — topics file not written.

- [ ] **Step 3: Extend pipeline.Run to handle nested paths and topics**

Replace the body of `pipeline.go` `Run` between the tmpdir creation and the partial-failure check with:

```go
identityClusters := pickKind(cf.Clusters, "identity")
ruleClusters := FilterRules(cf.Clusters, p.MinRuleEvidence, p.MinRuleProjects)
topicGroups := GroupTopicClusters(cf.Clusters)

userRules := readUserRules(p.GhostDir)

results := []FileResult{
	BuildIdentity(ctx, p.Client, p.SmartModel, identityClusters),
	BuildRules(ctx, p.Client, p.SmartModel, ruleClusters, userRules),
}
results = append(results, BuildTopics(ctx, p.Client, p.SmartModel, topicGroups)...)

var failed []string
for _, r := range results {
	if r.Err != nil {
		failed = append(failed, fmt.Sprintf("%s: %v", r.Name, r.Err))
		continue
	}
	dst := filepath.Join(tmpDir, r.Name)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		failed = append(failed, fmt.Sprintf("%s: mkdir: %v", r.Name, err))
		continue
	}
	if err := os.WriteFile(dst, []byte(r.Content), 0o644); err != nil {
		failed = append(failed, fmt.Sprintf("%s: write: %v", r.Name, err))
	}
}
if len(failed) > 0 {
	return fmt.Errorf("synthesize partial failure (tmpdir preserved at %s): %s", tmpDir, strings.Join(failed, "; "))
}
```

For the per-file rename loop, replace it with a directory-aware version that wipes the destination `topics/` before re-populating, so deleted topics actually disappear:

```go
// Refresh topics/ as a unit so removed topics vanish.
topicsDst := filepath.Join(p.GhostDir, "topics")
if err := os.RemoveAll(topicsDst); err != nil {
	return fmt.Errorf("clean topics/: %w (tmpdir preserved at %s)", err, tmpDir)
}

for _, r := range results {
	src := filepath.Join(tmpDir, r.Name)
	dst := filepath.Join(p.GhostDir, r.Name)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w (tmpdir preserved at %s)", filepath.Dir(dst), err, tmpDir)
	}
	if err := os.Rename(src, dst); err != nil {
		return fmt.Errorf("rename %s: %w (tmpdir preserved at %s)", r.Name, err, tmpDir)
	}
}
_ = os.RemoveAll(tmpDir)
return nil
```

Note: identity.md and rules.md are top-level files; `filepath.Dir(dst)` for them is `p.GhostDir`, which already exists, so `MkdirAll` is a no-op. For topics, it creates `topics/` fresh after the wipe.

- [ ] **Step 4: Run tests, verify they pass**

Run: `go test ./internal/synthesize/ -v`
Expected: PASS, including the existing partial-failure test (which only used identity + rules) and the new topics test.

- [ ] **Step 5: Add a partial-failure test that involves topics**

Append:

```go
func TestPipelinePartialFailureLeavesPriorTopicsIntact(t *testing.T) {
	dir := t.TempDir()
	// Seed an existing topics/ directory.
	if err := os.MkdirAll(filepath.Join(dir, "topics"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "topics", "old.md"), []byte("# Old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// fakeClient that errors on the second call (BuildRules).
	calls := 0
	f := &countingFakeClient{respondAfter: func(n int) (string, error) {
		calls = n
		if n == 2 {
			return "", errors.New("boom")
		}
		return "# Out\n", nil
	}}
	p := &Pipeline{Client: f, SmartModel: "smart", GhostDir: dir, MinRuleEvidence: 1, MinRuleProjects: 1}
	cf := cluster.ClustersFile{Clusters: []cluster.Cluster{
		{Kind: "identity", Canonical: "x", EvidenceCount: 1, ProjectCount: 1,
			Members: []cluster.ClusterMember{{Text: "x", Evidence: "t", Project: "p"}}},
		{Kind: "rule", Canonical: "y", EvidenceCount: 1, ProjectCount: 1,
			Members: []cluster.ClusterMember{{Text: "y", Evidence: "t", Project: "p"}}},
	}}
	err := p.Run(context.Background(), cf)
	if err == nil {
		t.Fatal("expected partial-failure error")
	}
	// Prior topics survive.
	if _, statErr := os.Stat(filepath.Join(dir, "topics", "old.md")); statErr != nil {
		t.Fatalf("prior topics/old.md was destroyed by failed run: %v", statErr)
	}
	_ = calls
}

type countingFakeClient struct {
	n            int
	respondAfter func(n int) (string, error)
}

func (c *countingFakeClient) Complete(ctx context.Context, model, system, user string) (string, error) {
	c.n++
	return c.respondAfter(c.n)
}
```

This pins down the invariant that **the topics wipe happens only after all builders succeeded**, not at the start of Run.

- [ ] **Step 6: Run the partial-failure test, verify it fails**

Run: `go test ./internal/synthesize/ -run TestPipelinePartialFailureLeavesPriorTopicsIntact -v`
Expected: FAIL if the wipe is in the wrong place. Confirm.

- [ ] **Step 7: If failing, move the topics wipe to after the partial-failure gate**

Verify the `os.RemoveAll(topicsDst)` line in Step 3 sits **after** the `if len(failed) > 0` check, not before. If it doesn't, move it. Re-run.

- [ ] **Step 8: Run all synthesize tests, verify they pass**

Run: `go test ./internal/synthesize/ -v`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/synthesize/pipeline.go internal/synthesize/pipeline_test.go
git commit -m "feat(synthesize): write topics/*.md atomically with identity + rules"
```

---

## Task 3: Index synthesis — rank, cap, generate triggers

**Files:**
- Create: `internal/synthesize/index.go`
- Create: `internal/synthesize/index_test.go`
- Create: `prompts/synthesize.index.system.md`
- Modify: `prompts/prompts.go`

- [ ] **Step 1: Write the failing test for ranking + capping**

In `index_test.go`:

```go
package synthesize

import (
	"context"
	"strings"
	"testing"

	"github.com/SarahFrankle/ghost/internal/cluster"
)

func TestRankAndCapTopics(t *testing.T) {
	groups := map[string][]cluster.Cluster{
		"testing": {{EvidenceCount: 10}, {EvidenceCount: 3}}, // 13
		"git":     {{EvidenceCount: 5}},                       // 5
		"writing": {{EvidenceCount: 7}},                       // 7
	}
	ranked := RankTopicsByEvidence(groups)
	if len(ranked) != 3 || ranked[0].Slug != "testing" || ranked[1].Slug != "writing" || ranked[2].Slug != "git" {
		t.Fatalf("unexpected ranking: %+v", ranked)
	}
	capped := CapTopics(ranked, 2)
	if len(capped) != 2 || capped[0].Slug != "testing" || capped[1].Slug != "writing" {
		t.Fatalf("unexpected cap: %+v", capped)
	}
}
```

- [ ] **Step 2: Run test, verify it fails**

Run: `go test ./internal/synthesize/ -run TestRankAndCapTopics -v`
Expected: FAIL.

- [ ] **Step 3: Implement ranking + cap**

In `index.go`:

```go
package synthesize

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/SarahFrankle/ghost/internal/anthropic"
	"github.com/SarahFrankle/ghost/internal/cluster"
	"github.com/SarahFrankle/ghost/prompts"
)

type RankedTopic struct {
	Slug          string
	EvidenceTotal int
	Canonicals    []string
}

func RankTopicsByEvidence(groups map[string][]cluster.Cluster) []RankedTopic {
	out := make([]RankedTopic, 0, len(groups))
	for slug, cs := range groups {
		total := 0
		canon := make([]string, 0, len(cs))
		for _, c := range cs {
			total += c.EvidenceCount
			if c.Canonical != "" {
				canon = append(canon, c.Canonical)
			}
		}
		out = append(out, RankedTopic{Slug: slug, EvidenceTotal: total, Canonicals: canon})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].EvidenceTotal != out[j].EvidenceTotal {
			return out[i].EvidenceTotal > out[j].EvidenceTotal
		}
		return out[i].Slug < out[j].Slug
	})
	return out
}

func CapTopics(ranked []RankedTopic, max int) []RankedTopic {
	if max <= 0 || len(ranked) <= max {
		return ranked
	}
	return ranked[:max]
}
```

- [ ] **Step 4: Run test, verify it passes**

Run: `go test ./internal/synthesize/ -run TestRankAndCapTopics -v`
Expected: PASS.

- [ ] **Step 5: Write the failing test for BuildIndex**

Append:

```go
func TestBuildIndexProducesOneEntryPerRankedTopic(t *testing.T) {
	f := &fakeClient{resp: "# Index\n\n## Topics\n- topics/testing.md — triggers: tests, pytest\n- topics/git.md — triggers: rebase, branch\n"}
	ranked := []RankedTopic{
		{Slug: "testing", EvidenceTotal: 13, Canonicals: []string{"prefer table-driven"}},
		{Slug: "git", EvidenceTotal: 5, Canonicals: []string{"rebase before merge"}},
	}
	res := BuildIndex(context.Background(), f, "smart", ranked)
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	if res.Name != "index.md" {
		t.Fatalf("expected index.md, got %s", res.Name)
	}
	if !strings.Contains(f.gotUser, "topics/testing.md") || !strings.Contains(f.gotUser, "topics/git.md") {
		t.Fatalf("prompt did not list both topic paths: %q", f.gotUser)
	}
}

func TestBuildIndexEmptyWhenNoTopics(t *testing.T) {
	f := &fakeClient{resp: "should not be called"}
	res := BuildIndex(context.Background(), f, "smart", nil)
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	if !strings.Contains(res.Content, "No lazy-loaded topics") {
		t.Fatalf("expected placeholder, got %q", res.Content)
	}
}
```

- [ ] **Step 6: Run, verify failure**

Run: `go test ./internal/synthesize/ -run TestBuildIndex -v`
Expected: FAIL.

- [ ] **Step 7: Implement BuildIndex**

Append to `index.go`:

```go
func BuildIndex(ctx context.Context, client anthropic.Client, model string, ranked []RankedTopic) FileResult {
	if len(ranked) == 0 {
		return FileResult{Name: "index.md", Content: "# Index\n\nNo lazy-loaded topics yet.\n"}
	}
	var b strings.Builder
	b.WriteString("RANKED TOPICS (highest evidence first):\n")
	for _, r := range ranked {
		fmt.Fprintf(&b, "- slug=%s file=topics/%s.md evidence=%d\n", r.Slug, r.Slug, r.EvidenceTotal)
		for _, c := range r.Canonicals {
			fmt.Fprintf(&b, "    canonical: %s\n", c)
		}
	}
	raw, err := client.Complete(ctx, model, prompts.SynthesizeIndexSystem(), b.String())
	if err != nil {
		return FileResult{Name: "index.md", Err: fmt.Errorf("index: %w", err)}
	}
	return FileResult{Name: "index.md", Content: ensureTrailingNewline(strings.TrimSpace(raw))}
}
```

- [ ] **Step 8: Add the prompt and accessor**

In `prompts/prompts.go`, append:

```go
//go:embed synthesize.index.system.md
var synthesizeIndexSystem string

func SynthesizeIndexSystem() string { return synthesizeIndexSystem }
```

Create `prompts/synthesize.index.system.md`:

```
You are writing `index.md`, the lookup table Claude consults at the
start of every task to decide whether to lazy-load a topic file.

Your input is a ranked list of topics, highest evidence first. For
each topic you have:
- a slug (filename stem)
- the topics/<slug>.md path
- one or more canonical phrasings drawn from the topic's clusters

Output format — emit EXACTLY this structure and nothing else:

# Index

## Topics
- topics/<slug>.md — triggers: <comma-separated trigger phrases>
- ...

Hard rules:
- One line per topic, in the order given. Do not reorder.
- "triggers" are short, lowercase phrases a user would mention when
  the topic applies. Derive them from the canonical phrasings.
  Include the slug itself plus 2–5 additional phrases. No more.
- No prose, no preamble, no closing remarks. Just the heading and
  the list.
- No em-dashes inside triggers. Use commas.
- If the canonical phrasings name a tool (pytest, git, eslint),
  include the tool name as a trigger.
```

- [ ] **Step 9: Run tests, verify they pass**

Run: `go test ./internal/synthesize/ -v && go build ./...`
Expected: PASS.

- [ ] **Step 10: Commit**

```bash
git add internal/synthesize/index.go internal/synthesize/index_test.go \
        prompts/synthesize.index.system.md prompts/prompts.go
git commit -m "feat(synthesize): build capped index.md with topic triggers"
```

---

## Task 4: Pipeline integrates index and respects the cap

**Files:**
- Modify: `internal/synthesize/pipeline.go`
- Modify: `internal/synthesize/pipeline_test.go`
- Modify: `cmd/compose.go`

The Pipeline needs the index cap from config and must run BuildIndex after the topic builds so its ranking reflects whatever topic files actually shipped.

- [ ] **Step 1: Add the cap field to Pipeline**

In `pipeline.go`, extend the struct:

```go
type Pipeline struct {
	Client          anthropic.Client
	SmartModel      string
	GhostDir        string
	MinRuleEvidence int
	MinRuleProjects int
	MaxTopicEntries int
}
```

- [ ] **Step 2: Wire the cap into Run**

In `pipeline.go`, after computing `topicGroups` and before appending topic results, also rank and cap:

```go
ranked := RankTopicsByEvidence(topicGroups)
capped := CapTopics(ranked, p.MaxTopicEntries)

// Only generate topic files that survived the cap. Topics beyond the
// cap are still queryable via `ghost topics` (Phase 3 doesn't ship
// that subcommand — they simply don't appear in index.md) but their
// content is omitted from disk so the always-loaded budget is
// respected: a topic the index can't reference is dead weight.
keep := make(map[string][]cluster.Cluster, len(capped))
for _, r := range capped {
	keep[r.Slug] = topicGroups[r.Slug]
}

results = append(results, BuildTopics(ctx, p.Client, p.SmartModel, keep)...)
results = append(results, BuildIndex(ctx, p.Client, p.SmartModel, capped))
```

- [ ] **Step 3: Update compose.go to pass MaxTopicEntries**

In `cmd/compose.go` `runSynthesize`:

```go
p := &synthesize.Pipeline{
	Client:          client,
	SmartModel:      cfg.Models.Smart,
	GhostDir:        outDir,
	MinRuleEvidence: cfg.Thresholds.RuleMinEvidenceCount,
	MinRuleProjects: cfg.Thresholds.RuleMinProjectCount,
	MaxTopicEntries: cfg.Index.MaxTopicEntries,
}
```

And update the "synthesize: wrote ..." line:

```go
fmt.Println("synthesize: wrote identity.md, rules.md, topics/*.md, index.md")
```

- [ ] **Step 4: Write the failing test for cap enforcement**

In `pipeline_test.go`:

```go
func TestPipelineRespectsTopicCap(t *testing.T) {
	dir := t.TempDir()
	f := &fakeClient{resp: "# Out\n"}
	p := &Pipeline{
		Client: f, SmartModel: "smart", GhostDir: dir,
		MinRuleEvidence: 1, MinRuleProjects: 1, MaxTopicEntries: 1,
	}
	cf := cluster.ClustersFile{Clusters: []cluster.Cluster{
		{Kind: "topic", SubKey: "a", Canonical: "ca", EvidenceCount: 10, ProjectCount: 1,
			Members: []cluster.ClusterMember{{Text: "ca", Evidence: "t", Project: "p"}}},
		{Kind: "topic", SubKey: "b", Canonical: "cb", EvidenceCount: 1, ProjectCount: 1,
			Members: []cluster.ClusterMember{{Text: "cb", Evidence: "t", Project: "p"}}},
	}}
	if err := p.Run(context.Background(), cf); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "topics", "a.md")); err != nil {
		t.Fatalf("expected topics/a.md (highest evidence): %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "topics", "b.md")); !os.IsNotExist(err) {
		t.Fatalf("expected topics/b.md to be capped out, got err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "index.md")); err != nil {
		t.Fatalf("expected index.md: %v", err)
	}
}
```

- [ ] **Step 5: Run all synthesize tests, verify they pass**

Run: `go test ./internal/synthesize/ -v && go build ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/synthesize/pipeline.go internal/synthesize/pipeline_test.go cmd/compose.go
git commit -m "feat(synthesize): cap index at MaxTopicEntries; build index.md last"
```

---

## Task 5: Wire identity + rules + index includes into CLAUDE.md

The user's `~/.claude/CLAUDE.md` already includes `@~/.ghost/identity.md`, `@~/.ghost/rules.md`, `@~/.ghost/rules.user.md`, and `@~/.ghost/index.md` (verified in session context). No change needed. **Skip if confirmed**; otherwise add a one-line README note. Move on.

---

## Task 6: SKILL.md embedded asset + `ghost install-skill`

**Files:**
- Create: `skill/SKILL.md`
- Create: `skill/skill.go`
- Create: `skill/skill_test.go`
- Create: `cmd/install_skill.go`

- [ ] **Step 1: Write SKILL.md**

Create `skill/SKILL.md`:

```
---
name: ghost
description: Use at the start of any task. Checks the ghost index and reads matching topic files before responding. Triggers on any task touching an entry listed in ~/.ghost/index.md.
---

# Ghost — lazy-load topic guidance

You have identity context and a rule set always loaded. You also
have an index at `~/.ghost/index.md` listing lazy-loaded topic
files under `~/.ghost/topics/`.

## Mechanical check (before responding to the user)

1. Read `~/.ghost/index.md` if you have not already this session.
2. Match the user's request against the trigger phrases for each
   topic entry.
3. If a topic entry matches, Read `~/.ghost/topics/<slug>.md`
   before writing code or answering.
4. If nothing matches, proceed without loading anything.

A file loaded once per session stays in context — do not re-Read
it. Do not load every topic at session start. Lazy loading is the
whole point.

## Identity is context, not a template

The always-loaded `identity.md` tells you who the user is. Use it
to calibrate your answers (their stack, expertise, organization),
not as a template to mimic. The user is a specialist in some
areas; you stay a generalist across all areas.

## When the index is missing

If `~/.ghost/index.md` does not exist, ghost has not been
composed yet. Proceed normally and do not warn the user — they
will run `ghost compose` when they want it.
```

- [ ] **Step 2: Embed and expose it**

Create `skill/skill.go`:

```go
package skill

import _ "embed"

//go:embed SKILL.md
var skillMD string

// Content returns the SKILL.md body shipped with this binary.
func Content() string { return skillMD }

// DefaultInstallDir is where Claude Code expects user skills.
const DefaultInstallDir = "~/.claude/skills/ghost"
```

- [ ] **Step 3: Test the embed**

Create `skill/skill_test.go`:

```go
package skill

import (
	"strings"
	"testing"
)

func TestContentEmbedded(t *testing.T) {
	c := Content()
	if !strings.Contains(c, "name: ghost") || !strings.Contains(c, "index.md") {
		t.Fatalf("SKILL.md did not embed correctly: %q", c)
	}
}
```

Run: `go test ./skill/ -v`
Expected: PASS.

- [ ] **Step 4: Write the install command**

Create `cmd/install_skill.go`:

```go
package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/SarahFrankle/ghost/internal/atomicfs"
	"github.com/SarahFrankle/ghost/internal/paths"
	"github.com/SarahFrankle/ghost/skill"
)

var installSkillCmd = &cobra.Command{
	Use:   "install-skill",
	Short: "Write SKILL.md to ~/.claude/skills/ghost/",
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, err := paths.Expand(skill.DefaultInstallDir)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		target := filepath.Join(dir, "SKILL.md")
		if err := atomicfs.WriteFile(target, []byte(skill.Content()), 0o644); err != nil {
			return err
		}
		fmt.Printf("installed SKILL.md → %s\n", target)
		return nil
	},
}

func init() { rootCmd.AddCommand(installSkillCmd) }
```

- [ ] **Step 5: Build, run, verify the file appears**

Run:

```bash
go build -o ./ghost ./
./ghost install-skill
ls ~/.claude/skills/ghost/SKILL.md
```

Expected: the file exists. Open it; confirm contents match what you embedded.

- [ ] **Step 6: Commit**

```bash
git add skill/ cmd/install_skill.go
git commit -m "feat(skill): embed SKILL.md and add ghost install-skill"
```

---

## Task 7: `ghost add-rule` subcommand

**Files:**
- Create: `cmd/add_rule.go`
- Create: `cmd/add_rule_test.go`

- [ ] **Step 1: Write the failing test**

Create `cmd/add_rule_test.go`:

```go
package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppendUserRuleCreatesFileIfMissing(t *testing.T) {
	dir := t.TempDir()
	if err := appendUserRule(dir, "prefer local-first LLMs"); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "rules.user.md"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	if !strings.HasPrefix(s, "# Rules (user-authored)") {
		t.Fatalf("missing header on new file: %q", s)
	}
	if !strings.Contains(s, "- prefer local-first LLMs") {
		t.Fatalf("rule not appended: %q", s)
	}
}

func TestAppendUserRuleAppendsToExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.user.md")
	if err := os.WriteFile(path, []byte("# Rules (user-authored)\n\n- existing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := appendUserRule(dir, "new one"); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), "- existing") || !strings.Contains(string(body), "- new one") {
		t.Fatalf("unexpected body: %q", string(body))
	}
}
```

- [ ] **Step 2: Run, verify failure**

Run: `go test ./cmd/ -run TestAppendUserRule -v`
Expected: FAIL.

- [ ] **Step 3: Implement**

Create `cmd/add_rule.go`:

```go
package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/SarahFrankle/ghost/internal/paths"
)

var addRuleCmd = &cobra.Command{
	Use:   "add-rule <text>",
	Short: "Append a user-authored rule to ~/.ghost/rules.user.md",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		outDir, err := paths.Expand(cfg.Paths.OutputDir)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			return err
		}
		if err := appendUserRule(outDir, args[0]); err != nil {
			return err
		}
		fmt.Printf("appended to %s\n", filepath.Join(outDir, "rules.user.md"))
		return nil
	},
}

func appendUserRule(ghostDir, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("rule text required")
	}
	path := filepath.Join(ghostDir, "rules.user.md")
	body, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(body) == 0 {
		body = []byte("# Rules (user-authored)\n\nThese rules override anything in rules.md and survive recompose.\n\n")
	} else if !strings.HasSuffix(string(body), "\n") {
		body = append(body, '\n')
	}
	body = append(body, []byte("- "+text+"\n")...)
	return os.WriteFile(path, body, 0o644)
}

func init() { rootCmd.AddCommand(addRuleCmd) }
```

- [ ] **Step 4: Run tests**

Run: `go test ./cmd/ -v && go build ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/add_rule.go cmd/add_rule_test.go
git commit -m "feat(cli): ghost add-rule appends to rules.user.md"
```

---

## Task 8: `ghost show` (top-level) + `ghost show topics`

**Files:**
- Modify: `cmd/show.go`

The existing `ghost show observations` stays. Add two siblings: a top-level `ghost show` that prints identity + rules + rules.user, and `ghost show topics` that lists topic files with last-modified.

- [ ] **Step 1: Add the subcommands**

Edit `cmd/show.go`. Replace the `init()` with one that registers two new commands and keep observations:

```go
var showCoreCmd = &cobra.Command{
	Use:   "core",
	Short: "Print identity.md, rules.md, and rules.user.md",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		outDir, _ := paths.Expand(cfg.Paths.OutputDir)
		for _, name := range []string{"identity.md", "rules.md", "rules.user.md"} {
			full := filepath.Join(outDir, name)
			body, err := os.ReadFile(full)
			if err != nil {
				fmt.Printf("\n=== %s — (missing)\n", name)
				continue
			}
			fmt.Printf("\n=== %s ===\n%s", name, string(body))
		}
		return nil
	},
}

var showTopicsCmd = &cobra.Command{
	Use:   "topics",
	Short: "List topic files with last-modified",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		outDir, _ := paths.Expand(cfg.Paths.OutputDir)
		topicsDir := filepath.Join(outDir, "topics")
		entries, err := os.ReadDir(topicsDir)
		if err != nil {
			if os.IsNotExist(err) {
				fmt.Println("no topics yet")
				return nil
			}
			return err
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			fmt.Printf("  %-30s  %s\n", e.Name(), info.ModTime().Format(time.RFC3339))
		}
		return nil
	},
}

func init() {
	showObservationsCmd.Flags().IntVar(&showRecent, "recent", 5, "show observations from N most recent transcripts (0 = all)")
	showCmd.AddCommand(showObservationsCmd)
	showCmd.AddCommand(showCoreCmd)
	showCmd.AddCommand(showTopicsCmd)
	rootCmd.AddCommand(showCmd)
}
```

Add `"strings"` to the import block if it isn't there yet (it is — `show.go` already imports nothing of `strings`, so add it).

- [ ] **Step 2: Manual smoke test**

Run:

```bash
go build -o ./ghost ./
./ghost show core
./ghost show topics
```

Expected: `show core` prints identity + rules (or "(missing)" lines). `show topics` lists `.md` files from `~/.ghost/topics/` or prints "no topics yet" if absent.

- [ ] **Step 3: Commit**

```bash
git add cmd/show.go
git commit -m "feat(cli): ghost show core and ghost show topics"
```

---

## Task 9: Pricing table + `ghost compose --estimate`

**Files:**
- Create: `internal/pricing/pricing.go`
- Create: `internal/pricing/pricing_test.go`
- Create: `cmd/estimate.go`
- Modify: `cmd/compose.go`

The estimate path counts input bytes per stage, converts to approximate tokens (`bytes/4` — Anthropic's published heuristic), and multiplies by the per-model input price. Output cost is estimated at a flat 0.2× the input token count (synthesis outputs are short relative to inputs); we mark this clearly. Does not call the API.

- [ ] **Step 1: Pricing table with test**

Create `internal/pricing/pricing.go`:

```go
package pricing

import "strings"

// Price is per-million-tokens, USD. Source: Anthropic public pricing
// at time of writing. Update when models change.
type Price struct {
	InputPerMTok  float64
	OutputPerMTok float64
}

// Table maps the model-id prefix used in config to its price. Keys
// are matched with HasPrefix so date-suffixed model IDs
// (claude-haiku-4-5-20251001) still resolve.
var Table = map[string]Price{
	"claude-haiku-4-5":  {InputPerMTok: 1.0, OutputPerMTok: 5.0},
	"claude-opus-4-7":   {InputPerMTok: 15.0, OutputPerMTok: 75.0},
	"claude-sonnet-4-6": {InputPerMTok: 3.0, OutputPerMTok: 15.0},
	"voyage-3-lite":     {InputPerMTok: 0.02, OutputPerMTok: 0.0},
}

func Lookup(modelID string) (Price, bool) {
	for k, v := range Table {
		if strings.HasPrefix(modelID, k) {
			return v, true
		}
	}
	return Price{}, false
}

// EstimateTokens returns the rough token count for a byte payload,
// using Anthropic's published "~4 bytes per token" heuristic.
func EstimateTokens(bytes int) int { return (bytes + 3) / 4 }
```

Create `internal/pricing/pricing_test.go`:

```go
package pricing

import "testing"

func TestLookupResolvesDatedModelID(t *testing.T) {
	p, ok := Lookup("claude-haiku-4-5-20251001")
	if !ok {
		t.Fatal("Lookup did not resolve dated model id")
	}
	if p.InputPerMTok != 1.0 {
		t.Fatalf("unexpected price: %+v", p)
	}
}

func TestEstimateTokensRoughlyBytesOver4(t *testing.T) {
	if got := EstimateTokens(40); got != 10 {
		t.Fatalf("want 10, got %d", got)
	}
	if got := EstimateTokens(3); got != 1 {
		t.Fatalf("rounding up failed, got %d", got)
	}
}
```

Run: `go test ./internal/pricing/ -v`
Expected: PASS.

- [ ] **Step 2: Estimate command**

Create `cmd/estimate.go`:

```go
package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/SarahFrankle/ghost/internal/cluster"
	"github.com/SarahFrankle/ghost/internal/config"
	"github.com/SarahFrankle/ghost/internal/ledger"
	"github.com/SarahFrankle/ghost/internal/paths"
	"github.com/SarahFrankle/ghost/internal/pricing"
	"github.com/SarahFrankle/ghost/internal/transcript"
)

// runEstimate prints a per-stage token + cost estimate for the
// selected stages. It does not call any LLM. Output costs are an
// approximation at 0.2× input tokens (synthesis outputs are small
// relative to their prompts); the flag is intended for sanity, not
// billing.
func runEstimate(ctx context.Context, cfg config.Config, stages []string) error {
	outDir, _ := paths.Expand(cfg.Paths.OutputDir)
	stateDir := filepath.Join(outDir, ".state")

	for _, s := range stages {
		switch s {
		case "extract":
			if err := estimateExtract(cfg, outDir, stateDir); err != nil {
				return err
			}
		case "cluster":
			if err := estimateCluster(cfg, stateDir); err != nil {
				return err
			}
		case "synthesize":
			if err := estimateSynthesize(cfg, stateDir); err != nil {
				return err
			}
		}
	}
	return nil
}

func estimateExtract(cfg config.Config, outDir, stateDir string) error {
	glob, _ := paths.Expand(cfg.Paths.TranscriptsGlob)
	ts, err := transcript.Discover(glob, 0, nowFn())
	if err != nil {
		return err
	}
	l, _ := ledger.Load(filepath.Join(stateDir, "ledger.json"))

	var bytes int64
	pending := 0
	for _, t := range ts {
		h, err := transcript.ContentHash(t.Path)
		if err != nil {
			continue
		}
		if !l.NeedsProcessing(t.Path, h) {
			continue
		}
		info, err := os.Stat(t.Path)
		if err != nil {
			continue
		}
		bytes += info.Size()
		pending++
	}
	report("extract", cfg.Models.Cheap, int(bytes), pending)
	return nil
}

func estimateCluster(cfg config.Config, stateDir string) error {
	obsDir := filepath.Join(stateDir, "observations")
	entries, err := os.ReadDir(obsDir)
	if err != nil {
		return nil
	}
	var bytes int64
	files := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		bytes += info.Size()
		files++
	}
	report("cluster (canonical phrasing)", cfg.Models.Cheap, int(bytes), files)
	return nil
}

func estimateSynthesize(cfg config.Config, stateDir string) error {
	clustersPath := filepath.Join(stateDir, "clusters.json")
	body, err := os.ReadFile(clustersPath)
	if err != nil {
		fmt.Println("synthesize: clusters.json missing — run cluster first")
		return nil
	}
	var cf cluster.ClustersFile
	if err := json.Unmarshal(body, &cf); err != nil {
		return err
	}
	report("synthesize", cfg.Models.Smart, len(body), len(cf.Clusters))
	return nil
}

func report(stage, model string, inputBytes, units int) {
	tokens := pricing.EstimateTokens(inputBytes)
	p, ok := pricing.Lookup(model)
	if !ok {
		fmt.Printf("%s: model=%s units=%d input≈%d tokens (no pricing entry)\n", stage, model, units, tokens)
		return
	}
	inUSD := float64(tokens) / 1_000_000.0 * p.InputPerMTok
	outTokens := tokens / 5
	outUSD := float64(outTokens) / 1_000_000.0 * p.OutputPerMTok
	fmt.Printf("%s: model=%s units=%d input≈%d tok ($%.4f) output≈%d tok ($%.4f) total≈$%.4f\n",
		stage, model, units, tokens, inUSD, outTokens, outUSD, inUSD+outUSD)
}

// nowFn is overridable in tests.
var nowFn = func() time.Time { return time.Now() }
```

Add `"time"` to the imports.

- [ ] **Step 3: Wire `--estimate` into compose**

In `cmd/compose.go`:

```go
var composeEstimate bool
// ...
composeCmd.Flags().BoolVar(&composeEstimate, "estimate", false, "print per-stage token + cost estimate and exit")
```

Inside `composeCmd.RunE`, before the stage loop:

```go
if composeEstimate {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	return runEstimate(cmd.Context(), cfg, stages)
}
```

- [ ] **Step 4: Manual smoke test**

Run:

```bash
go build -o ./ghost ./
./ghost compose --estimate --stages all
```

Expected: three lines (extract, cluster, synthesize) with token + cost numbers. Order of magnitude only — these are estimates.

- [ ] **Step 5: Commit**

```bash
git add internal/pricing/ cmd/estimate.go cmd/compose.go
git commit -m "feat(compose): --estimate prints per-stage token + cost projections"
```

---

## Task 10: Migration doc

**Files:**
- Create: `docs/migration-from-memory.md`

- [ ] **Step 1: Write the migration checklist**

Create `docs/migration-from-memory.md`:

```markdown
# Migrating off ~/.claude/memory/

This document captures the operational steps for retiring the
hand-curated memory directory in favour of ghost's synthesized
artifacts. The technical work — atomic writes, always-loaded
includes — is already shipped. What remains is human review.

## Day 0 (already done)

- `~/.claude/CLAUDE.md` includes `@~/.ghost/identity.md`,
  `@~/.ghost/rules.md`, `@~/.ghost/rules.user.md`,
  `@~/.ghost/index.md`.
- `@~/.claude/projects/-Users-sarah-dev-projects-ghost/memory/MEMORY.md`
  (auto-memory) is still active. Both load. No conflict — auto-memory
  carries different content (per-project notes) from ghost's
  global synthesis.

## Week 1: review pass

After ~7 days of normal use:

1. `ghost show core` and read identity.md + rules.md end to end.
2. Open the relevant `~/.claude/memory/*.md` files. For each fact:
   - **Captured correctly** → no action.
   - **Missed** → if it's durable across projects, run
     `ghost add-rule "<text>"`. If it's project-scoped, leave it
     in memory.md — it will surface as a topic file once a second
     project agrees with it.
   - **Wrong** → identify the source conversation, then
     `ghost forget <transcript-path>`. Re-run
     `ghost compose --stages cluster,synthesize`.
3. `ghost show topics` — verify the lazy-loaded topics fire on
   tasks that mention them. If a topic doesn't fire, its triggers
   in `index.md` are wrong; either tune the prompt
   (`prompts/synthesize.index.system.md`) and recompose, or add an
   explicit user rule.

## Day 14: archive

If week-1 review came out clean:

1. Remove the `@memory/MEMORY.md` line from
   `~/.claude/CLAUDE.md`.
2. `mv ~/.claude/projects/-Users-sarah-dev-projects-ghost/memory ~/.claude/projects/-Users-sarah-dev-projects-ghost/memory.archive`.
3. Do not delete the archive. It is the cross-check baseline if
   ghost ever regresses.

From this point on, session feedback flows into transcripts and is
picked up by the next `ghost compose` run. The reactive
"save this as memory" pattern is no longer load-bearing.
```

- [ ] **Step 2: Commit**

```bash
git add docs/migration-from-memory.md
git commit -m "docs: phase 3 migration checklist"
```

---

## Task 11: End-to-end smoke against the real corpus

- [ ] **Step 1: Rebuild and run synthesis end-to-end**

Run:

```bash
go build -o ./ghost ./
./ghost compose --stages cluster,synthesize
ls -la ~/.ghost/
ls -la ~/.ghost/topics/
```

Expected:
- `~/.ghost/identity.md`, `rules.md`, `index.md` present and recent.
- `~/.ghost/topics/` contains at least one `<slug>.md` if any
  topic clusters survived. If the directory is empty or absent,
  that's a signal the corpus has no multi-evidence topics yet —
  not a bug.

- [ ] **Step 2: Read index.md and verify the format**

```bash
cat ~/.ghost/index.md
```

Expected: starts with `# Index`, has a `## Topics` section, one
bullet per topic, each bullet of the form
`- topics/<slug>.md — triggers: ...`. No extra prose.

- [ ] **Step 3: Install the skill and verify**

```bash
./ghost install-skill
cat ~/.claude/skills/ghost/SKILL.md | head -20
```

Expected: SKILL.md present at the install path.

- [ ] **Step 4: Add a user rule and re-synthesize**

```bash
./ghost add-rule "prefer reading code over recalling memories when verifying claims"
./ghost compose --stages synthesize
cat ~/.ghost/rules.user.md
cat ~/.ghost/rules.md
```

Expected:
- `rules.user.md` contains the new bullet.
- `rules.md` does not duplicate it (subtractive synthesis).

- [ ] **Step 5: Estimate**

```bash
./ghost compose --estimate --stages all
```

Expected: three lines of token + cost estimates. Numbers should look small (cents, not dollars).

- [ ] **Step 6: Final commit**

If anything in `~/.ghost/.state/` or the prompts needed adjustment based on the smoke results, fix it now. Then:

```bash
git status
git log --oneline phase-1-extract ^main
```

Verify the branch is in good shape. No commit if nothing to commit.

---

## Self-review notes

- **Spec coverage:**
  - Topics synthesis (spec 375–377): Task 1–4. ✓
  - Capped index.md (389, 422): Task 3, 4. ✓
  - SKILL.md trigger logic (537–593): Task 6. ✓
  - rules.user.md authoritative override (422–427): already in Phase 2 (`BuildRules` passes userRules). Task 7 adds the writer. ✓
  - `/ghost show`, `/ghost add-rule`, `/ghost forget` (v1 slash command set, line 755): Task 7 (add-rule), Task 8 (show core + topics), forget already exists from Phase 1. ✓
  - `ghost compose --estimate` (514–520): Task 9. ✓
  - Migration off `~/.claude/memory/` (650–690): Task 10. ✓
  - Voice off in v1 (Voice.Enabled = false): unchanged; no voice codepath added. ✓
- **Out of scope (deferred per spec):** voice synthesis, `ghost eval`, golden-transcript LLM fixtures, `/ghost scan`, `/ghost topics` / `/ghost voice` slash commands beyond what `ghost show topics` gives us.
- **Type consistency:** `Pipeline.MaxTopicEntries` (int), `RankedTopic.Slug` (string), `topics/<slug>.md` naming convention — all used consistently across Tasks 1–4.
- **Risk:** the only nontrivial atomic-write change is the topics-directory wipe (Task 2 Step 3). Task 2 Step 5–7 exists specifically to pin it in place via a regression test.
