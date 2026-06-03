# Chunk 3: Embedding-Based Topics Implementation Plan

> **STATUS (2026-06-03):** Tasks 1–16 implemented on branch `chunk-3-embedding-topics` (13 commits, net −1054 LOC). Build/vet/full test suite green (75 tests). NOT yet merged to `main`. Task 17 (state migration) done by Sarah; Task 18 (e2e) in progress and surfaced two real findings: (a) slug collisions on generic titles ("runbook-entries", "pull-request-templates") because `nomic-embed-text` compresses all topic-pair cosines into 0.65–0.74 (ceiling 0.7431), so the 0.75 default (tuned for Voyage) never merges; (b) RESOLVED — no `cluster_cosine_topic` value clears collisions on `nomic-embed-text`; they live at the titling step, not bucketing. Fail-loud-on-collision was replaced with **collision → merge** (see `docs/superpowers/plans/2026-06-03-chunk-3-collision-merge.md` and `docs/specs/2026-06-03-chunk-3-collision-merge-design.md`; Decision 1 revised in `docs/specs/2026-05-22-chunk-3-decisions.md`). The `0.75` default ships unchanged. E2e green: `compose --stages cluster,synthesize` completes at 0.75, merging the PR-family and runbook collisions automatically. Ready to squash-merge per [[feedback-squash-per-chunk]].

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop using a free-text slug as the bucketing key for topic observations. Bucket topic observations by content-embedding cosine similarity (like identity/rule already do, but with a separate looser threshold). Synthesize each topic cluster with one smart-model call whose body starts with `# <Title>`; the caller slugifies the title to derive `topics/<slug>.md`.

**Architecture:** Net deletion. The `internal/canonicalize/` package, `KNOWN TOPICS` prompt section, slug-shape rules, `Topic` field on `Observation`, `TopicAliases` shim, and `canonicalize` stage all go away. `Bucket` learns a per-kind threshold lookup. A new pure `slugify` helper turns parsed H1s into filenames. Topic synthesis runs as two passes inside the topic stage: parallel body generation, then a sequential collision check on slugs. `BuildIndex` runs after topic synthesis so it can see produced slugs.

**Tech Stack:** Go (existing toolchain), Anthropic Claude (existing client), Voyage/Ollama embeddings (existing, unchanged).

**Source of truth:** `docs/specs/2026-05-22-chunk-3-design.md` (revised) and `docs/specs/2026-05-22-chunk-3-decisions.md` (revised). If this plan conflicts with the design, the design wins — stop and reconcile before coding.

**Before starting:** create a feature branch from `main` (e.g. `chunk-3-embedding-topics`). Per-task commits stay on the branch; the chunk merges to `main` as one squash commit per [[feedback-squash-per-chunk]].

---

## File Structure

### Create

- `internal/synthesize/slugify.go` — pure `Slug(title string) (string, error)` and `ParseH1(body string) (string, error)`. No filesystem, no LLM.
- `internal/synthesize/slugify_test.go` — table-driven tests.

### Modify

- `prompts/extract.system.md` — drop `KNOWN TOPICS` section and the slug-shape rules; topic observations carry `kind, text, evidence` only.
- `prompts/synthesize.topics.system.md` — rewrite; receive a cluster of observations and emit a body starting with `# <Title>`.
- `prompts/prompts.go` — remove `CanonicalizeSlugSystem` accessor.
- `internal/extract/schema.go` — drop `Topic` field and its validation.
- `internal/extract/schema_test.go` — drop the topic-without-slug case.
- `internal/extract/extract.go` — drop `KnownTopics` field and the known-topics block in `renderPayload`.
- `internal/extract/extract_test.go` — drop fixtures that exercise KnownTopics.
- `internal/cluster/types.go` — `SubKey()` returns `""` for `kind=topic` (voice keeps its `Context` discriminator).
- `internal/cluster/bucket.go` — `Bucket` takes a `ThresholdFor func(kind string) float32` instead of a single `threshold` argument.
- `internal/cluster/bucket_test.go` — update call sites; add a topic-with-loose-threshold case.
- `internal/cluster/pipeline.go` — drop `TopicAliases` field and the topic-rewrite shim; pass a per-kind threshold function; drop the `Canonicalizer` field and its `Apply` call.
- `internal/cluster/pipeline_test.go` — update call sites.
- `internal/config/config.go` — replace `ClusterCosineThreshold` (single) with `ClusterCosineIdentityRule` (default `0.85`) and `ClusterCosineTopic` (default `0.75`); drop `CanonicalizeSimilarityThreshold`.
- `internal/config/config_test.go` — assert the new defaults.
- `internal/synthesize/topics.go` — rewrite. Drop `GroupTopicClusters`. New `BuildTopics` takes `[]cluster.Cluster` (one topic per cluster); generates bodies in parallel; parses H1; slugifies; returns `[]TopicResult` plus per-cluster `FileResult`s.
- `internal/synthesize/topics_test.go` — replace existing tests with cases for distinct slugs, collision, malformed body.
- `internal/synthesize/index.go` — `BuildIndex` takes `[]TopicResult` directly (drop `RankedTopic`).
- `internal/synthesize/index_test.go` — update call sites.
- `internal/synthesize/pipeline.go` — reorder: build topic results first, run collision check, then `BuildIndex`. Strict atomicity: any topic failure (LLM error, parse error, slugifier reject, or slug collision) fails the whole topics rebuild and leaves prior `~/.ghost/topics/` intact.
- `internal/synthesize/pipeline_test.go` — update fixtures; add a collision case and a malformed-body case.
- `cmd/compose.go` — drop `runCanonicalize`; drop `"canonicalize"` from `parseStages` and the dispatch switch; drop the `canonicalize` flag accessor; drop `KnownTopics` plumbing in `runExtract`; drop `listKnownTopics`; drop `observedTopicSlugSamples`; drop the alias loader; update `runCluster` to use the new threshold fields and drop the canonicalizer; update `synthesizeFingerprint` to drop `CanonicalizeSlugSystemHash` (already gone) and instead include both threshold values.

### Delete

- `internal/canonicalize/` — whole package.
- `prompts/canonicalize.slug.system.md`.

### Untouched (deliberate)

- `internal/embedding/`, `internal/source/`, `internal/ledger/`, `internal/fingerprint/`, `internal/atomicfs/`, `internal/paths/`, `internal/pricing/`, `internal/secrets/`, `internal/anthropic/`.
- `internal/synthesize/identity.go`, `rules.go` — unchanged (their inputs are observations on disk that re-fingerprint, but the synth code itself doesn't change).

---

## Task ordering rationale

Bottom-up: data-model and prompt changes first (they ripple), then bucketing, then deletion of canonicalize, then the new topics flow, then the pipeline reorder, then wiring in `cmd/`. End-to-end verification last.

Each task is one focused commit. Tests come first when the unit has tests; for deletions and plumbing changes, a build-and-test step stands in.

---

## Task 1: Drop the `Topic` field from `Observation`

**Files:**
- Modify: `internal/extract/schema.go`
- Modify: `internal/extract/schema_test.go`

- [ ] **Step 1: Update the failing test**

Open `internal/extract/schema_test.go` and remove any case that exercises `Topic`. Then add a new case asserting that a topic-kind observation with only `kind/text/evidence` is valid.

```go
func TestTopicKindValidatesWithoutTopicField(t *testing.T) {
    o := Observation{Kind: "topic", Text: "prefer table-driven tests", Evidence: "turn 4: prefer tables"}
    if err := o.Validate(); err != nil {
        t.Fatalf("topic-kind observation should validate without a Topic field: %v", err)
    }
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/extract/ -run TestTopicKindValidatesWithoutTopicField -v`
Expected: FAIL with "topic kind requires topic field".

- [ ] **Step 3: Update the struct and validation**

Edit `internal/extract/schema.go`:

```go
type Observation struct {
    Kind       string `json:"kind"`
    Text       string `json:"text"`
    Evidence   string `json:"evidence"`
    Confidence string `json:"confidence,omitempty"`
    Context    string `json:"context,omitempty"`
}

// ...

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
    if o.Kind == "voice" && o.Context == "" {
        return errors.New("voice kind requires context field")
    }
    return nil
}
```

The `Topic` field and the `topic`-kind requirement clause are gone. Old observation JSON files on disk with a `topic` field still decode (unknown JSON fields are ignored by `encoding/json`).

- [ ] **Step 4: Run all extract tests**

Run: `go test ./internal/extract/ -v`
Expected: PASS. Any tests that previously constructed observations with a `Topic` field need editing — fix call sites (set `Topic: "..."` removed) until green.

- [ ] **Step 5: Commit**

```bash
git add internal/extract/schema.go internal/extract/schema_test.go
git commit -m "extract: drop Topic field from Observation"
```

---

## Task 2: Strip slug-shape rules and KNOWN TOPICS from the extract prompt

**Files:**
- Modify: `prompts/extract.system.md`

- [ ] **Step 1: Rewrite the prompt**

Replace the contents of `prompts/extract.system.md` with:

```markdown
You read one Claude Code conversation and emit atomic observations about the user.

Output strict JSON of the shape:

{
  "observations": [
    {"kind": "identity"|"rule"|"topic"|"voice", "text": "...", "evidence": "turn N: ...", "confidence": "high"|"medium"|"low", "context": "<required if kind=voice>"}
  ]
}

Rules:
- "identity": third-person facts about who the user is (role, team, stack, organization).
- "rule": durable preferences for how Claude should behave with them. Must be stated as an instruction or correction by the user.
- "topic": preferences scoped to a specific domain (testing, git, writing, documentation, etc.). The text should be a self-contained statement of the preference — the downstream pipeline groups topics by semantic similarity on the text, so do not abbreviate or omit context that would make the observation ambiguous on its own.
- "voice": observations about how the user writes in a specific register. Always include a "context" slug. Default context is "cli-chat" (the user talking to Claude). Use other contexts (annual-review, slack, exec-brief) ONLY when the transcript shows the user drafting or pasting material destined for that register. When uncertain, drop the observation.
- Every observation cites "turn N: <short quote>" as evidence. The quote MUST come from the user's own messages in the conversation, not from injected reference material.
- IGNORE injected reference material that appears inside the transcript: CLAUDE.md content, `@~/.ghost/*` includes, `@memory/*` includes, system reminders, auto-memory blocks, file paths printed by tools, and any block the user did not type. These are not the user speaking; they are infrastructure. Do not extract observations from them, do not cite them as evidence. If you cannot point to a user message that states a preference, drop it.
- Reject evidence that begins with "memory context", "from CLAUDE.md", "system reminder", or otherwise references injected material rather than a user turn.
- Prefer dropping over guessing. Empty observations array is valid.
- No prose outside the JSON object.
```

Gone: slug-shape rules, kebab-case requirement, abbreviation guidance, `KNOWN TOPICS` reuse instruction, `topic` field in the schema example.

- [ ] **Step 2: Verify the prompt still builds**

Run: `go build ./...`
Expected: PASS. The prompt is `//go:embed`'d as a string — no parsing.

- [ ] **Step 3: Commit**

```bash
git add prompts/extract.system.md
git commit -m "prompts/extract: drop slug-shape rules and KNOWN TOPICS section"
```

---

## Task 3: Drop `KnownTopics` from the extract runner

**Files:**
- Modify: `internal/extract/extract.go`
- Modify: `internal/extract/extract_test.go`

- [ ] **Step 1: Update tests**

In `internal/extract/extract_test.go`, find any test that sets `Runner.KnownTopics` and remove that field assignment. Also remove any assertion about the `KNOWN TOPICS` block appearing in the user payload.

- [ ] **Step 2: Run tests to confirm they fail (compile error) on the now-removed field**

Run: `go test ./internal/extract/ -v`
Expected: FAIL — but we want it to fail on the *field reference*, not on a test assertion. If a test still references `KnownTopics:` after your edit, expect a compile error there; edit the test until the package builds.

Actually order this the other way: update the source first, then make tests compile.

- [ ] **Step 3: Update `Runner` and `renderPayload`**

Edit `internal/extract/extract.go`:

```go
type Runner struct {
    Client anthropic.Client
    Model  string
    Log    Logger
}
```

And replace `renderPayload`:

```go
func renderPayload(turns []source.Turn) string {
    var b strings.Builder
    for _, t := range turns {
        fmt.Fprintf(&b, "turn %d (%s): %s\n", t.Index, t.Role, t.Text)
    }
    return b.String()
}
```

Update the call site inside `Run`:

```go
userPayload := renderPayload(turns)
```

- [ ] **Step 4: Verify build and tests pass**

Run: `go test ./internal/extract/ -v`
Expected: PASS. If any test still references `KnownTopics`, remove that field assignment.

- [ ] **Step 5: Commit**

```bash
git add internal/extract/extract.go internal/extract/extract_test.go
git commit -m "extract: drop KnownTopics plumbing from Runner and renderPayload"
```

---

## Task 4: Split the cosine threshold config field

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

- [ ] **Step 1: Update the failing test**

In `internal/config/config_test.go`, replace any defaults assertion for `ClusterCosineThreshold` with:

```go
func TestDefaultsClusterCosineThresholds(t *testing.T) {
    d := Defaults()
    if d.Thresholds.ClusterCosineIdentityRule != 0.85 {
        t.Fatalf("identity/rule default = %v, want 0.85", d.Thresholds.ClusterCosineIdentityRule)
    }
    if d.Thresholds.ClusterCosineTopic != 0.75 {
        t.Fatalf("topic default = %v, want 0.75", d.Thresholds.ClusterCosineTopic)
    }
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/config/ -run TestDefaultsClusterCosineThresholds -v`
Expected: FAIL with "undefined: ClusterCosineIdentityRule" or similar.

- [ ] **Step 3: Update the struct and defaults**

Edit `internal/config/config.go`:

```go
type Thresholds struct {
    RuleMinEvidenceCount      int     `toml:"rule_min_evidence_count"`
    RuleMinProjectCount       int     `toml:"rule_min_project_count"`
    VoiceMinEvidenceCount     int     `toml:"voice_min_evidence_count"`
    ClusterCosineIdentityRule float64 `toml:"cluster_cosine_identity_rule"`
    ClusterCosineTopic        float64 `toml:"cluster_cosine_topic"`
}
```

In `Defaults()`:

```go
Thresholds: Thresholds{
    RuleMinEvidenceCount:      2,
    RuleMinProjectCount:       2,
    VoiceMinEvidenceCount:     2,
    ClusterCosineIdentityRule: 0.85,
    ClusterCosineTopic:        0.75,
},
```

Gone: `ClusterCosineThreshold`, `CanonicalizeSimilarityThreshold`.

- [ ] **Step 4: Build the rest of the repo to find call sites**

Run: `go build ./...`
Expected: FAIL — `cmd/compose.go` and `internal/cluster/pipeline.go` still reference the old fields. We fix those in later tasks; leave this build broken until then.

For this commit, scope to config only. Verify the config package itself is green:

Run: `go test ./internal/config/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "config: split ClusterCosineThreshold into identity/rule + topic"
```

The repo build is intentionally broken between this commit and Task 8 (where `cmd/compose.go` is updated). Per-task commits stay on the chunk branch and squash to one merge commit per [[feedback-squash-per-chunk]] — intermediate brokenness is fine.

---

## Task 5: Stop sub-keying topic observations

**Files:**
- Modify: `internal/cluster/types.go`

- [ ] **Step 1: Drop `Topic` from `ClusterMember` and from `SubKey()`**

Edit `internal/cluster/types.go`:

```go
package cluster

import "time"

const SchemaVersion = 1

type ClusterMember struct {
    ObservationHash string `json:"observation_hash"`
    Source          string `json:"source"`
    Project         string `json:"project"`
    Kind            string `json:"kind"`
    Text            string `json:"text"`
    Evidence        string `json:"evidence"`
    Context         string `json:"context,omitempty"`
    Confidence      string `json:"confidence,omitempty"`
}

func (m ClusterMember) SubKey() string {
    if m.Kind == "voice" {
        return m.Context
    }
    return ""
}

type Cluster struct {
    Kind          string          `json:"kind"`
    SubKey        string          `json:"sub_key,omitempty"`
    Canonical     string          `json:"canonical"`
    Members       []ClusterMember `json:"members"`
    EvidenceCount int             `json:"evidence_count"`
    ProjectCount  int             `json:"project_count"`
}

type ClustersFile struct {
    SchemaVersion    int       `json:"schema_version"`
    EmbeddingModelID string    `json:"embedding_model_id"`
    BuiltAt          time.Time `json:"built_at"`
    Fingerprint      string    `json:"fingerprint,omitempty"`
    Clusters         []Cluster `json:"clusters"`
}
```

Topic observations now all share `SubKey() == ""`, so `Bucket` pools them together.

- [ ] **Step 2: Build the package**

Run: `go build ./internal/cluster/`
Expected: FAIL — `pipeline.go` references `members[i].Topic`. We fix that in Task 7.

For this commit, the cluster package is intentionally broken. Move to the next task.

- [ ] **Step 3: Commit**

```bash
git add internal/cluster/types.go
git commit -m "cluster: drop Topic field from ClusterMember; SubKey ignores kind=topic"
```

---

## Task 6: Per-kind threshold in `Bucket`

**Files:**
- Modify: `internal/cluster/bucket.go`
- Modify: `internal/cluster/bucket_test.go`

- [ ] **Step 1: Update the existing tests for the new signature**

In `internal/cluster/bucket_test.go`, change calls to take a function. Replace the three existing calls:

```go
identityRule := func(string) float32 { return 0.85 }
clusters := Bucket(members, func(i int) []float32 { return vecs[i] }, identityRule)
```

```go
identical := func(int) []float32 { return []float32{1, 0} }
clusters := Bucket(members, identical, func(string) float32 { return 0.5 })
```

(Apply the same shape to all three existing tests.)

- [ ] **Step 2: Add a new test for per-kind threshold**

Append to `internal/cluster/bucket_test.go`:

```go
func TestBucketAppliesPerKindThreshold(t *testing.T) {
    members := []ClusterMember{
        {Kind: "topic", Text: "documentation should be example-first", Project: "a"},
        {Kind: "topic", Text: "docs should lead with examples", Project: "b"},
        {Kind: "rule", Text: "no mocks in integration tests", Project: "a"},
        {Kind: "rule", Text: "avoid mocks at boundaries", Project: "b"},
    }
    vecs := map[int][]float32{
        0: {1, 0, 0},
        1: {0.8, 0.6, 0},   // ~0.80 cosine with vec 0
        2: {0, 1, 0},
        3: {0, 0.8, 0.6},   // ~0.80 cosine with vec 2
    }
    thresholdFor := func(kind string) float32 {
        if kind == "topic" {
            return 0.75
        }
        return 0.85
    }
    clusters := Bucket(members, func(i int) []float32 { return vecs[i] }, thresholdFor)

    // Topics: at threshold 0.75, vec 0 and vec 1 should merge.
    // Rules: at threshold 0.85, vec 2 and vec 3 should NOT merge.
    var topicCount, ruleCount int
    var mergedTopic bool
    for _, c := range clusters {
        switch c.Kind {
        case "topic":
            topicCount++
            if len(c.Members) == 2 {
                mergedTopic = true
            }
        case "rule":
            ruleCount++
        }
    }
    if !mergedTopic {
        t.Fatalf("topic observations should have merged at 0.75: %+v", clusters)
    }
    if ruleCount != 2 {
        t.Fatalf("rule observations should NOT have merged at 0.85: got %d rule clusters", ruleCount)
    }
    if topicCount != 1 {
        t.Fatalf("expected 1 topic cluster, got %d", topicCount)
    }
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/cluster/ -run TestBucket -v`
Expected: FAIL with a signature mismatch — `Bucket` still takes `float32`.

- [ ] **Step 4: Change `Bucket`'s signature**

Edit `internal/cluster/bucket.go`:

```go
package cluster

import "github.com/SarahFrankle/ghost/internal/embedding"

// Bucket partitions members by (kind, sub_key) and within each partition
// merges members whose embeddings are within thresholdFor(kind) cosine of
// an existing cluster's first member (single-linkage agglomerative).
//
// thresholdFor is consulted per-partition: identity and rule typically want
// a tight threshold (e.g. 0.85), topic wants a looser one (e.g. 0.75).
func Bucket(members []ClusterMember, vecOf func(i int) []float32, thresholdFor func(kind string) float32) []Cluster {
    type key struct{ kind, sub string }
    parts := map[key][]int{}
    for i, m := range members {
        k := key{m.Kind, m.SubKey()}
        parts[k] = append(parts[k], i)
    }

    var out []Cluster
    for k, idxs := range parts {
        threshold := thresholdFor(k.kind)
        var clusters [][]int
        for _, i := range idxs {
            vi := vecOf(i)
            placed := false
            for ci, c := range clusters {
                vj := vecOf(c[0])
                if embedding.Cosine(vi, vj) >= threshold {
                    clusters[ci] = append(c, i)
                    placed = true
                    break
                }
            }
            if !placed {
                clusters = append(clusters, []int{i})
            }
        }

        for _, c := range clusters {
            mems := make([]ClusterMember, 0, len(c))
            projects := map[string]struct{}{}
            for _, i := range c {
                mems = append(mems, members[i])
                if members[i].Project != "" {
                    projects[members[i].Project] = struct{}{}
                }
            }
            out = append(out, Cluster{
                Kind:          k.kind,
                SubKey:        k.sub,
                Canonical:     members[c[0]].Text,
                Members:       mems,
                EvidenceCount: len(mems),
                ProjectCount:  len(projects),
            })
        }
    }
    return out
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cluster/ -run TestBucket -v`
Expected: PASS for all four tests. (`pipeline.go` may still be broken — that's Task 7.)

- [ ] **Step 6: Commit**

```bash
git add internal/cluster/bucket.go internal/cluster/bucket_test.go
git commit -m "cluster: per-kind threshold lookup in Bucket"
```

---

## Task 7: Update cluster pipeline (drop TopicAliases, Canonicalizer, accept per-kind thresholds)

**Files:**
- Modify: `internal/cluster/pipeline.go`
- Modify: `internal/cluster/pipeline_test.go`

- [ ] **Step 1: Inspect existing test fixtures**

Run: `go test ./internal/cluster/ -v` (expect compile failure)

Open `internal/cluster/pipeline_test.go` and note which `Pipeline` fields the tests set. We must remove `TopicAliases`, `Canonicalizer`, and `CosineThreshold` references and add a `ThresholdFor` function.

- [ ] **Step 2: Rewrite `Pipeline` struct and `Run`**

Edit `internal/cluster/pipeline.go`. Replace the file's contents with:

```go
package cluster

import (
    "context"
    "encoding/json"
    "fmt"
    "os"
    "path/filepath"
    "time"

    "github.com/SarahFrankle/ghost/internal/embedding"
    "github.com/SarahFrankle/ghost/internal/extract"
)

// Pipeline owns stage 2 end-to-end: load observation files, embed each
// observation (cache-aware), bucket per-kind, then write clusters.json.
// Topic observations bucket by cosine on .Text alone (no SubKey).
type Pipeline struct {
    Embedder       embedding.Embedder
    EmbeddingModel string
    Cache          *embedding.Cache
    CacheSavePath  string
    ClustersPath   string
    // ThresholdFor returns the cosine threshold for a given kind. Callers
    // typically supply identity/rule = 0.85, topic = 0.75.
    ThresholdFor func(kind string) float32
    Workers      int
    Log          func(format string, args ...any)
    // Fingerprint, if non-empty, is written to the resulting clusters.json
    // so subsequent runs can detect input/threshold/model changes without
    // rebuilding.
    Fingerprint string
}

func (p *Pipeline) logf(format string, args ...any) {
    if p.Log != nil {
        p.Log(format, args...)
    }
}

func (p *Pipeline) Run(ctx context.Context, observationsDir string) error {
    members, err := loadAllObservations(observationsDir)
    if err != nil {
        return fmt.Errorf("load observations: %w", err)
    }
    if len(members) == 0 {
        return SaveClusters(p.ClustersPath, ClustersFile{
            SchemaVersion:    SchemaVersion,
            EmbeddingModelID: p.EmbeddingModel,
            BuiltAt:          time.Now().UTC(),
        })
    }

    vectors, err := p.embedAll(ctx, members)
    if err != nil {
        return fmt.Errorf("embed: %w", err)
    }
    if err := p.Cache.Save(p.CacheSavePath); err != nil {
        p.logf("embedding cache save: %v", err)
    }

    clusters := Bucket(members, func(i int) []float32 { return vectors[i] }, p.ThresholdFor)

    return SaveClusters(p.ClustersPath, ClustersFile{
        SchemaVersion:    SchemaVersion,
        EmbeddingModelID: p.EmbeddingModel,
        BuiltAt:          time.Now().UTC(),
        Fingerprint:      p.Fingerprint,
        Clusters:         clusters,
    })
}

func (p *Pipeline) embedAll(ctx context.Context, members []ClusterMember) ([][]float32, error) {
    out := make([][]float32, len(members))

    missingIdx := []int{}
    missingTexts := []string{}
    for i, m := range members {
        if v, ok := p.Cache.Get(m.ObservationHash); ok {
            out[i] = v
            continue
        }
        missingIdx = append(missingIdx, i)
        missingTexts = append(missingTexts, m.Text)
    }
    if len(missingIdx) == 0 {
        return out, nil
    }

    vecs, err := p.Embedder.Embed(ctx, p.EmbeddingModel, missingTexts)
    if err != nil {
        return nil, err
    }
    if len(vecs) != len(missingIdx) {
        return nil, fmt.Errorf("embedder returned %d vectors for %d inputs", len(vecs), len(missingIdx))
    }
    for j, idx := range missingIdx {
        out[idx] = vecs[j]
        p.Cache.Put(members[idx].ObservationHash, vecs[j])
    }
    return out, nil
}

func loadAllObservations(observationsDir string) ([]ClusterMember, error) {
    var out []ClusterMember
    entries, err := os.ReadDir(observationsDir)
    if err != nil {
        return nil, err
    }
    for _, e := range entries {
        if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
            continue
        }
        path := filepath.Join(observationsDir, e.Name())
        b, err := os.ReadFile(path)
        if err != nil {
            return nil, err
        }
        var f extract.ObservationsFile
        if err := json.Unmarshal(b, &f); err != nil {
            return nil, fmt.Errorf("decode %s: %w", e.Name(), err)
        }
        for _, o := range f.Observations {
            subKey := ""
            if o.Kind == "voice" {
                subKey = o.Context
            }
            out = append(out, ClusterMember{
                ObservationHash: embedding.ObservationHash(o.Kind, subKey, o.Text),
                Source:          f.Source,
                Project:         f.Project,
                Kind:            o.Kind,
                Text:            o.Text,
                Evidence:        o.Evidence,
                Context:         o.Context,
                Confidence:      o.Confidence,
            })
        }
    }
    return out, nil
}
```

Gone: `TopicAliases`, `Canonicalizer`, `Apply` call, `members[i].Topic` rewrite, the topic branch in `loadAllObservations`.

- [ ] **Step 3: Update pipeline tests**

In `internal/cluster/pipeline_test.go`, fix construction sites. Wherever a test sets `CosineThreshold:` on a `Pipeline`, replace with:

```go
ThresholdFor: func(string) float32 { return 0.85 },
```

Remove any `TopicAliases:` and `Canonicalizer:` assignments. If a test fixture creates an observation with `Topic: "..."`, drop that field — it no longer exists on `Observation`. If a test asserts that a topic alias rewrites a topic field, delete the test; the behavior is gone.

- [ ] **Step 4: Run cluster tests**

Run: `go test ./internal/cluster/ -v`
Expected: PASS. If `canonical.go` / `canonical_cache.go` / `canonical_test.go` (the in-package canonicalizer that runs at stage 2b) still exist and are part of the test surface, leave them for now — they're owned by `internal/cluster` not `internal/canonicalize`. If pipeline tests no longer reference them, they're unused but compile. We address removal in Task 9.

Note: if `internal/cluster/canonical_test.go` references the now-removed `Canonicalizer` field on `Pipeline`, you may need to remove those tests. Check on a case-by-case basis: tests that exercise `Canonicalizer.Apply` directly (without going through the pipeline) can stay; tests that wire it into a pipeline must be deleted because `Pipeline.Canonicalizer` is gone.

- [ ] **Step 5: Commit**

```bash
git add internal/cluster/pipeline.go internal/cluster/pipeline_test.go
git commit -m "cluster: drop TopicAliases shim and Canonicalizer; pipeline takes per-kind thresholds"
```

---

## Task 8: Delete the `internal/canonicalize/` package

**Files:**
- Delete: `internal/canonicalize/` (whole package)
- Delete: `prompts/canonicalize.slug.system.md`
- Modify: `prompts/prompts.go` — remove `CanonicalizeSlugSystem`

- [ ] **Step 1: Verify nothing inside `internal/cluster/` imports `internal/canonicalize/`**

Run: `grep -rn 'internal/canonicalize' internal/ cmd/`
Expected output: matches in `cmd/compose.go` only (Task 11 fixes those). If `internal/cluster/` still imports it, find the import and remove it before deleting — but Task 7 already dropped the only usage.

- [ ] **Step 2: Delete the package and prompt**

```bash
rm -rf internal/canonicalize/
rm prompts/canonicalize.slug.system.md
```

- [ ] **Step 3: Remove the prompt accessor**

Edit `prompts/prompts.go`. Remove this block:

```go
//go:embed canonicalize.slug.system.md
var canonicalizeSlugSystem string

func CanonicalizeSlugSystem() string { return canonicalizeSlugSystem }
```

- [ ] **Step 4: Confirm prompts package builds**

Run: `go build ./prompts/`
Expected: PASS.

The rest of the repo (cmd/compose.go) still references the deleted package. Fix in Task 11.

- [ ] **Step 5: Commit**

```bash
git add -A internal/canonicalize prompts/
git commit -m "canonicalize: delete package, prompt, and accessor"
```

---

## Task 9: Delete the in-cluster Canonicalizer (stage 2b)

**Files:**
- Delete: `internal/cluster/canonical.go`
- Delete: `internal/cluster/canonical_cache.go`
- Delete: `internal/cluster/canonical_test.go`
- Modify: `prompts/prompts.go` — remove `ClusterCanonicalSystem` if unused
- Modify: `prompts/cluster.canonical.system.md` — delete if unused

Stage 2b (the in-cluster LLM-driven canonical-naming step) is also gone with chunk 3 — clusters use their seed member text as the canonical, which is what `Bucket` already does. The `Canonicalizer` was applied as `p.Canonicalizer.Apply(ctx, clusters)` after bucketing; that call was removed in Task 7. The remaining files are dead code.

- [ ] **Step 1: Verify no remaining usages**

Run: `grep -rn 'cluster.Canonicalizer\|cluster.LoadCanonicalCache\|ClusterCanonicalSystem' internal/ cmd/`
Expected output: matches in `cmd/compose.go` only (Task 11 fixes). If anything else still uses these, stop and reconcile.

- [ ] **Step 2: Delete the files**

```bash
rm internal/cluster/canonical.go internal/cluster/canonical_cache.go internal/cluster/canonical_test.go
```

- [ ] **Step 3: Delete the embedded prompt**

```bash
rm prompts/cluster.canonical.system.md
```

Edit `prompts/prompts.go`. Remove:

```go
//go:embed cluster.canonical.system.md
var clusterCanonicalSystem string

func ClusterCanonicalSystem() string { return clusterCanonicalSystem }

func ClusterCanonicalSystemHash() string { return hashOf(clusterCanonicalSystem) }
```

- [ ] **Step 4: Verify cluster + prompts build**

Run: `go build ./internal/cluster/ ./prompts/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add -A internal/cluster prompts/
git commit -m "cluster: delete in-cluster Canonicalizer and stage-2b prompt"
```

---

## Task 10: Slugifier helper

**Files:**
- Create: `internal/synthesize/slugify.go`
- Create: `internal/synthesize/slugify_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/synthesize/slugify_test.go`:

```go
package synthesize

import "testing"

func TestSlugHappyPaths(t *testing.T) {
    cases := []struct {
        in, want string
    }{
        {"Testing", "testing"},
        {"Error Handling", "error-handling"},
        {"  Documentation  ", "documentation"},
        {"Pull Requests & Reviews", "pull-requests-reviews"},
        {"API Design (REST)", "api-design-rest"},
        {"CI/CD", "ci-cd"},
        {"git: rebase before merge", "git-rebase-before-merge"},
    }
    for _, c := range cases {
        got, err := Slug(c.in)
        if err != nil {
            t.Fatalf("Slug(%q) returned error: %v", c.in, err)
        }
        if got != c.want {
            t.Fatalf("Slug(%q) = %q, want %q", c.in, got, c.want)
        }
    }
}

func TestSlugRejects(t *testing.T) {
    cases := []string{
        "",
        "   ",
        "12345",
        "!!!",
        "this-title-is-far-too-long-to-be-a-reasonable-slug-for-a-topic-file",
    }
    for _, in := range cases {
        if _, err := Slug(in); err == nil {
            t.Fatalf("Slug(%q) should have rejected", in)
        }
    }
}

func TestParseH1ExtractsTitle(t *testing.T) {
    body := "# Error Handling\n\nSome content here.\n"
    title, err := ParseH1(body)
    if err != nil {
        t.Fatalf("ParseH1 returned error: %v", err)
    }
    if title != "Error Handling" {
        t.Fatalf("got %q, want %q", title, "Error Handling")
    }
}

func TestParseH1RejectsMissingHeading(t *testing.T) {
    body := "No heading here.\n"
    if _, err := ParseH1(body); err == nil {
        t.Fatal("ParseH1 should have rejected body without H1 first line")
    }
}

func TestParseH1RejectsNonFirstLineHeading(t *testing.T) {
    body := "Some preamble.\n\n# Title\n"
    if _, err := ParseH1(body); err == nil {
        t.Fatal("ParseH1 should reject H1 not on the first non-empty line")
    }
}

func TestParseH1AllowsLeadingBlankLine(t *testing.T) {
    body := "\n# Title\n\nbody.\n"
    title, err := ParseH1(body)
    if err != nil {
        t.Fatalf("ParseH1 returned error: %v", err)
    }
    if title != "Title" {
        t.Fatalf("got %q, want %q", title, "Title")
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/synthesize/ -run "TestSlug|TestParseH1" -v`
Expected: FAIL with "undefined: Slug" / "undefined: ParseH1".

- [ ] **Step 3: Implement the slugifier**

Create `internal/synthesize/slugify.go`:

```go
package synthesize

import (
    "bufio"
    "errors"
    "fmt"
    "strings"
    "unicode"
)

// maxSlugLen is the upper bound on slug length. Longer than this and the
// title is almost certainly not a clean noun phrase — reject and let the
// caller fail the topic.
const maxSlugLen = 40

// Slug turns a title string into a kebab-case filename slug. Deterministic.
//
// Rules:
//   - lowercase
//   - any run of non-[a-z0-9] characters collapses to a single '-'
//   - leading/trailing '-' trimmed
//   - reject if result is empty, longer than maxSlugLen, or contains no letter
func Slug(title string) (string, error) {
    var b strings.Builder
    prevDash := true // treat start of string as if it had a trailing dash, so leading garbage doesn't emit one
    for _, r := range strings.TrimSpace(title) {
        switch {
        case r >= 'A' && r <= 'Z':
            b.WriteRune(unicode.ToLower(r))
            prevDash = false
        case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
            b.WriteRune(r)
            prevDash = false
        default:
            if !prevDash {
                b.WriteByte('-')
                prevDash = true
            }
        }
    }
    s := strings.TrimRight(b.String(), "-")

    if s == "" {
        return "", fmt.Errorf("slugify: empty result from %q", title)
    }
    if len(s) > maxSlugLen {
        return "", fmt.Errorf("slugify: result %q exceeds max length %d", s, maxSlugLen)
    }
    hasLetter := false
    for _, r := range s {
        if r >= 'a' && r <= 'z' {
            hasLetter = true
            break
        }
    }
    if !hasLetter {
        return "", fmt.Errorf("slugify: result %q has no letter", s)
    }
    return s, nil
}

// ParseH1 extracts the title from the first non-empty line of body, which
// must be of the form `# <Title>`. Whitespace before/after the heading
// text is trimmed. Returns an error if no such heading is the first
// non-empty line.
func ParseH1(body string) (string, error) {
    scanner := bufio.NewScanner(strings.NewReader(body))
    for scanner.Scan() {
        line := strings.TrimSpace(scanner.Text())
        if line == "" {
            continue
        }
        if !strings.HasPrefix(line, "# ") {
            return "", errors.New("ParseH1: first non-empty line is not a level-1 heading")
        }
        title := strings.TrimSpace(strings.TrimPrefix(line, "# "))
        if title == "" {
            return "", errors.New("ParseH1: heading has no title text")
        }
        return title, nil
    }
    return "", errors.New("ParseH1: body is empty")
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/synthesize/ -run "TestSlug|TestParseH1" -v`
Expected: PASS for all six tests.

- [ ] **Step 5: Commit**

```bash
git add internal/synthesize/slugify.go internal/synthesize/slugify_test.go
git commit -m "synthesize: add Slug and ParseH1 helpers"
```

---

## Task 11: Rewrite the topic synthesis prompt

**Files:**
- Modify: `prompts/synthesize.topics.system.md`

- [ ] **Step 1: Replace the prompt**

Replace the contents of `prompts/synthesize.topics.system.md` with:

```markdown
You are writing one topic file (`topics/<slug>.md`) that Claude
Code will lazy-load when the user works on tasks matching that
topic. The file is reference material the user has already
implicitly agreed with through repeated feedback across sessions.

Your input is a single cluster of observations about one topic. Each
cluster member has a canonical phrasing, supporting member
observations, and evidence.

Hard rules:
- The first non-empty line of your output MUST be `# <Title>`. The
  title is a clean noun phrase naming the concept this cluster is
  about — title case, no quoting, no abbreviations, no trailing
  punctuation, at most 8 words. Examples: `# Error Handling`,
  `# Pull Requests`, `# Documentation`. The caller derives the
  filename from this title; an unparseable or unreasonable title
  fails the whole topic file.
- After the heading, write the body as markdown only.
- One bullet per durable preference. Imperative voice. No hedging.
- Group related bullets under level-2 subheadings only if there are
  at least three bullets that share a subtheme. Otherwise keep it
  flat.
- Do not invent guidance absent from the cluster. Do not paraphrase
  away the user's specificity.
- No em-dashes. No throat-clearing. Delete sentences you wouldn't
  miss.
- Single-project topics are valid. Do not refuse to write a topic
  just because every cluster member is from one project. Cross-project
  signal is enforced upstream by the rules-vs-topics split, not by
  you.
```

- [ ] **Step 2: Verify the prompts package builds**

Run: `go build ./prompts/`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add prompts/synthesize.topics.system.md
git commit -m "prompts/synthesize.topics: emit H1 first; caller slugifies"
```

---

## Task 12: Rewrite `BuildTopics`

**Files:**
- Modify: `internal/synthesize/topics.go`
- Modify: `internal/synthesize/topics_test.go`

- [ ] **Step 1: Replace the tests**

Replace the contents of `internal/synthesize/topics_test.go` with:

```go
package synthesize

import (
    "context"
    "errors"
    "strings"
    "testing"

    "github.com/SarahFrankle/ghost/internal/cluster"
)

// scriptedClient returns a different response for each Complete call,
// in order. Used to drive distinct per-cluster outputs.
type scriptedClient struct {
    responses []string
    errs      []error
    calls     int
}

func (s *scriptedClient) Complete(ctx context.Context, model, system, user string) (string, error) {
    i := s.calls
    s.calls++
    if i < len(s.errs) && s.errs[i] != nil {
        return "", s.errs[i]
    }
    if i >= len(s.responses) {
        return "", errors.New("scriptedClient: no response for call")
    }
    return s.responses[i], nil
}

func topicCluster(canonical, text string) cluster.Cluster {
    return cluster.Cluster{
        Kind:          "topic",
        Canonical:     canonical,
        EvidenceCount: 1,
        ProjectCount:  1,
        Members:       []cluster.ClusterMember{{Text: text, Evidence: "turn 1", Project: "p"}},
    }
}

func TestBuildTopicsDistinctClustersDistinctSlugs(t *testing.T) {
    s := &scriptedClient{responses: []string{
        "# Error Handling\n\n- Prefer typed errors.\n",
        "# Documentation\n\n- Lead with examples.\n",
    }}
    clusters := []cluster.Cluster{
        topicCluster("prefer typed errors", "prefer typed errors"),
        topicCluster("lead with examples", "lead with examples"),
    }
    results, _, err := BuildTopics(context.Background(), s, "smart", clusters)
    if err != nil {
        t.Fatalf("BuildTopics returned error: %v", err)
    }
    if len(results) != 2 {
        t.Fatalf("expected 2 results, got %d", len(results))
    }
    names := map[string]bool{}
    for _, r := range results {
        names[r.Name] = true
    }
    if !names["topics/error-handling.md"] || !names["topics/documentation.md"] {
        t.Fatalf("expected slugs error-handling and documentation, got %v", names)
    }
}

func TestBuildTopicsSlugCollisionFails(t *testing.T) {
    s := &scriptedClient{responses: []string{
        "# Error Handling\n\n- foo\n",
        "# Error Handling\n\n- bar\n",
    }}
    clusters := []cluster.Cluster{
        topicCluster("first", "first"),
        topicCluster("second", "second"),
    }
    _, _, err := BuildTopics(context.Background(), s, "smart", clusters)
    if err == nil {
        t.Fatal("BuildTopics should fail on slug collision")
    }
    if !strings.Contains(err.Error(), "collision") {
        t.Fatalf("error should mention collision: %v", err)
    }
    if !strings.Contains(err.Error(), "first") || !strings.Contains(err.Error(), "second") {
        t.Fatalf("error should name both canonicals: %v", err)
    }
}

func TestBuildTopicsMalformedBodyFails(t *testing.T) {
    s := &scriptedClient{responses: []string{
        "Some preamble.\n\n# Title\n",
    }}
    clusters := []cluster.Cluster{topicCluster("c", "c")}
    _, _, err := BuildTopics(context.Background(), s, "smart", clusters)
    if err == nil {
        t.Fatal("BuildTopics should fail when body's first line is not an H1")
    }
}

func TestBuildTopicsClientErrorFails(t *testing.T) {
    s := &scriptedClient{
        responses: []string{""},
        errs:      []error{errors.New("boom")},
    }
    clusters := []cluster.Cluster{topicCluster("c", "c")}
    _, _, err := BuildTopics(context.Background(), s, "smart", clusters)
    if err == nil {
        t.Fatal("BuildTopics should fail when the client errors")
    }
}
```

The old `GroupTopicClusters` test is gone — the function is deleted.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/synthesize/ -run TestBuildTopics -v`
Expected: FAIL with signature mismatch.

- [ ] **Step 3: Rewrite `topics.go`**

Replace the contents of `internal/synthesize/topics.go` with:

```go
package synthesize

import (
    "context"
    "fmt"
    "sort"
    "strings"
    "sync"

    "github.com/SarahFrankle/ghost/internal/anthropic"
    "github.com/SarahFrankle/ghost/internal/cluster"
    "github.com/SarahFrankle/ghost/prompts"
)

// TopicResult is one synthesized topic: the cluster it came from, the
// slug derived from the body's H1, the body itself, and the total
// evidence count. Used by BuildIndex to rank and link topics.
type TopicResult struct {
    Cluster       cluster.Cluster
    Slug          string
    Title         string
    Body          string
    EvidenceTotal int
}

// BuildTopics synthesizes one topic per kind=topic cluster.
//
// Pass 1: parallel — each cluster gets one smart-model call producing a
// markdown body whose first non-empty line is `# <Title>`. The caller
// parses the H1 and slugifies the title.
//
// Pass 2: sequential collision check — if any two clusters' slugs
// match, the whole call fails with an error naming both canonicals.
// Slug collisions are a signal that the topic cosine threshold is
// wrong; failing loudly is the value.
//
// On success returns []TopicResult (one per cluster, slug-sorted for
// determinism) and []FileResult (one per cluster, `topics/<slug>.md`,
// ready for the pipeline to write).
//
// On any per-cluster failure (LLM error, malformed body, slugifier
// reject) the whole call fails. Topics are an all-or-nothing rebuild;
// partial success is not a useful state because `index.md` references
// the slug set.
func BuildTopics(ctx context.Context, client anthropic.Client, model string, clusters []cluster.Cluster) ([]TopicResult, []FileResult, error) {
    // Filter to kind=topic. Drop any cluster the caller mistakenly passed in.
    topics := make([]cluster.Cluster, 0, len(clusters))
    for _, c := range clusters {
        if c.Kind == "topic" {
            topics = append(topics, c)
        }
    }
    if len(topics) == 0 {
        return nil, nil, nil
    }

    type out struct {
        idx   int
        title string
        body  string
        err   error
    }
    results := make([]out, len(topics))
    var wg sync.WaitGroup
    for i, c := range topics {
        i, c := i, c
        wg.Add(1)
        go func() {
            defer wg.Done()
            payload := renderTopicPayload(c)
            raw, err := client.Complete(ctx, model, prompts.SynthesizeTopicsSystem(), payload)
            if err != nil {
                results[i] = out{idx: i, err: fmt.Errorf("cluster %q: %w", c.Canonical, err)}
                return
            }
            body := ensureTrailingNewline(strings.TrimSpace(raw))
            title, err := ParseH1(body)
            if err != nil {
                results[i] = out{idx: i, err: fmt.Errorf("cluster %q: %w", c.Canonical, err)}
                return
            }
            results[i] = out{idx: i, title: title, body: body}
        }()
    }
    wg.Wait()

    // Aggregate errors before doing slug work — if any body call failed,
    // we fail the whole rebuild.
    var failed []string
    for _, r := range results {
        if r.err != nil {
            failed = append(failed, r.err.Error())
        }
    }
    if len(failed) > 0 {
        return nil, nil, fmt.Errorf("topics: %d cluster(s) failed: %s", len(failed), strings.Join(failed, "; "))
    }

    // Slugify all titles; collect collisions before producing any output.
    type slugRow struct {
        idx   int
        slug  string
        title string
    }
    slugs := make([]slugRow, len(results))
    bySlug := map[string][]int{}
    for _, r := range results {
        slug, err := Slug(r.title)
        if err != nil {
            return nil, nil, fmt.Errorf("topics: slugify cluster %q (title %q): %w", topics[r.idx].Canonical, r.title, err)
        }
        slugs[r.idx] = slugRow{idx: r.idx, slug: slug, title: r.title}
        bySlug[slug] = append(bySlug[slug], r.idx)
    }
    for slug, idxs := range bySlug {
        if len(idxs) > 1 {
            canonicals := make([]string, 0, len(idxs))
            for _, i := range idxs {
                canonicals = append(canonicals, fmt.Sprintf("%q", topics[i].Canonical))
            }
            return nil, nil, fmt.Errorf("topics: slug collision on %q from clusters %s — tune cluster_cosine_topic threshold", slug, strings.Join(canonicals, ", "))
        }
    }

    // Build outputs in slug-sorted order for determinism.
    sort.Slice(slugs, func(i, j int) bool { return slugs[i].slug < slugs[j].slug })

    trs := make([]TopicResult, 0, len(slugs))
    files := make([]FileResult, 0, len(slugs))
    for _, s := range slugs {
        c := topics[s.idx]
        body := results[s.idx].body
        trs = append(trs, TopicResult{
            Cluster:       c,
            Slug:          s.slug,
            Title:         s.title,
            Body:          body,
            EvidenceTotal: c.EvidenceCount,
        })
        files = append(files, FileResult{
            Name:    fmt.Sprintf("topics/%s.md", s.slug),
            Content: body,
        })
    }
    return trs, files, nil
}

func renderTopicPayload(c cluster.Cluster) string {
    var b strings.Builder
    b.WriteString("CLUSTER:\n")
    b.WriteString(renderClusters([]cluster.Cluster{c}))
    return b.String()
}
```

The function `GroupTopicClusters` is gone. Anything that still references it from `pipeline.go` will fail to build — that's Task 13.

- [ ] **Step 4: Run topic tests to verify they pass**

Run: `go test ./internal/synthesize/ -run TestBuildTopics -v`
Expected: PASS for all four tests.

The package as a whole may not build yet (`pipeline.go` calls `GroupTopicClusters`). Move to Task 13.

- [ ] **Step 5: Commit**

```bash
git add internal/synthesize/topics.go internal/synthesize/topics_test.go
git commit -m "synthesize: rewrite BuildTopics to one call per cluster with H1-derived slug"
```

---

## Task 13: Rewrite `BuildIndex` to take `[]TopicResult`

**Files:**
- Modify: `internal/synthesize/index.go`
- Modify: `internal/synthesize/index_test.go`

- [ ] **Step 1: Update the test**

Open `internal/synthesize/index_test.go`. Replace any construction of `RankedTopic` with `TopicResult`. The shape:

```go
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
```

Delete the old `TestRankTopicsByEvidence` test if present — ranking moves into the pipeline.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/synthesize/ -run TestBuildIndex -v`
Expected: FAIL with signature mismatch.

- [ ] **Step 3: Rewrite `index.go`**

Replace `internal/synthesize/index.go` with:

```go
package synthesize

import (
    "context"
    "fmt"
    "sort"
    "strings"

    "github.com/SarahFrankle/ghost/internal/anthropic"
    "github.com/SarahFrankle/ghost/prompts"
)

// RankByEvidence returns a copy of topics sorted by EvidenceTotal
// descending, with ties broken alphabetically by slug for determinism.
func RankByEvidence(topics []TopicResult) []TopicResult {
    out := make([]TopicResult, len(topics))
    copy(out, topics)
    sort.SliceStable(out, func(i, j int) bool {
        if out[i].EvidenceTotal != out[j].EvidenceTotal {
            return out[i].EvidenceTotal > out[j].EvidenceTotal
        }
        return out[i].Slug < out[j].Slug
    })
    return out
}

// Cap returns at most max entries from topics.
func Cap(topics []TopicResult, max int) []TopicResult {
    if max <= 0 || len(topics) <= max {
        return topics
    }
    return topics[:max]
}

// BuildIndex asks the smart model to emit index.md from a ranked list
// of TopicResult. Topics must already be ranked + capped by the caller.
func BuildIndex(ctx context.Context, client anthropic.Client, model string, topics []TopicResult) FileResult {
    if len(topics) == 0 {
        return FileResult{Name: "index.md", Content: "# Index\n\nNo lazy-loaded topics yet.\n"}
    }
    var b strings.Builder
    b.WriteString("RANKED TOPICS (highest evidence first):\n")
    for _, t := range topics {
        fmt.Fprintf(&b, "- slug=%s file=topics/%s.md evidence=%d title=%q\n", t.Slug, t.Slug, t.EvidenceTotal, t.Title)
        if t.Cluster.Canonical != "" {
            fmt.Fprintf(&b, "    canonical: %s\n", t.Cluster.Canonical)
        }
    }
    raw, err := client.Complete(ctx, model, prompts.SynthesizeIndexSystem(), b.String())
    if err != nil {
        return FileResult{Name: "index.md", Err: fmt.Errorf("index: %w", err)}
    }
    return FileResult{Name: "index.md", Content: ensureTrailingNewline(strings.TrimSpace(raw))}
}
```

`RankedTopic`, `RankTopicsByEvidence`, and `CapTopics` are replaced by `TopicResult`, `RankByEvidence`, and `Cap`.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/synthesize/ -run TestBuildIndex -v`
Expected: PASS for both tests.

Package may still fail to build (`pipeline.go` references old names). Task 14 fixes.

- [ ] **Step 5: Commit**

```bash
git add internal/synthesize/index.go internal/synthesize/index_test.go
git commit -m "synthesize: BuildIndex takes []TopicResult directly"
```

---

## Task 14: Reorder the synthesize pipeline; enforce strict atomicity

**Files:**
- Modify: `internal/synthesize/pipeline.go`
- Modify: `internal/synthesize/pipeline_test.go`

- [ ] **Step 1: Read the current pipeline tests**

Run: `cat internal/synthesize/pipeline_test.go` (in your terminal — not for the plan reader) to see what fixtures exist. The tests use `fakeClient` and construct a `Pipeline{Client: ..., SmartModel: "smart", GhostDir: tmpDir, ...}` then call `Run`. They assert against files written under `GhostDir`.

Plan: keep test fixtures' shape, but the new flow means we need a scripted client so each LLM call gets a distinct response in this order: identity body, rules body, then one body per topic cluster, then index body.

- [ ] **Step 2: Rewrite `pipeline.go`**

Replace `internal/synthesize/pipeline.go` with:

```go
package synthesize

import (
    "context"
    "errors"
    "fmt"
    "os"
    "path/filepath"
    "strings"
    "time"

    "github.com/SarahFrankle/ghost/internal/anthropic"
    "github.com/SarahFrankle/ghost/internal/cluster"
)

// Pipeline orchestrates stage 3: it produces identity.md, rules.md,
// the capped set of topics/*.md, and index.md.
//
// Order of operations:
//  1. Build identity and rules in parallel (they don't depend on topic
//     slugs).
//  2. Run topic synthesis: one smart-model call per topic cluster
//     producing a body that starts with `# <Title>`; slugify each
//     title; fail loudly on slug collision or any per-cluster error.
//  3. Rank surviving topics by evidence, cap to MaxTopicEntries.
//  4. Build index.md from the capped TopicResult list.
//  5. Atomic write: tmpdir holds every file, then the pipeline wipes
//     ~/.ghost/topics/ and renames each file into place. If any stage
//     above failed, the tmpdir is preserved and ~/.ghost/ is left
//     intact.
type Pipeline struct {
    Client          anthropic.Client
    SmartModel      string
    GhostDir        string
    MinRuleEvidence int
    MinRuleProjects int
    MaxTopicEntries int
}

func (p *Pipeline) Run(ctx context.Context, cf cluster.ClustersFile) error {
    if p.GhostDir == "" {
        return errors.New("synthesize: GhostDir required")
    }
    if err := os.MkdirAll(p.GhostDir, 0o755); err != nil {
        return err
    }
    tmpDir, err := os.MkdirTemp(p.GhostDir, ".tmp-synthesize-"+time.Now().UTC().Format("20060102T150405")+"-")
    if err != nil {
        return err
    }

    identityClusters := pickKind(cf.Clusters, "identity")
    ruleClusters := FilterRules(cf.Clusters, p.MinRuleEvidence, p.MinRuleProjects)
    topicClusters := pickKind(cf.Clusters, "topic")

    userRules := readUserRules(p.GhostDir)

    // Top-level files first. These never depend on topic slugs.
    results := []FileResult{
        BuildIdentity(ctx, p.Client, p.SmartModel, identityClusters),
        BuildRules(ctx, p.Client, p.SmartModel, ruleClusters, userRules),
    }

    // Topic synthesis. Any per-cluster error, malformed body, or slug
    // collision fails the whole rebuild.
    topicResults, topicFiles, topicErr := BuildTopics(ctx, p.Client, p.SmartModel, topicClusters)
    if topicErr != nil {
        return fmt.Errorf("synthesize: %w (tmpdir preserved at %s)", topicErr, tmpDir)
    }
    ranked := RankByEvidence(topicResults)
    capped := Cap(ranked, p.MaxTopicEntries)

    // Re-filter files to the capped set so dropped topics don't get
    // written.
    keep := map[string]bool{}
    for _, t := range capped {
        keep[fmt.Sprintf("topics/%s.md", t.Slug)] = true
    }
    for _, f := range topicFiles {
        if keep[f.Name] {
            results = append(results, f)
        }
    }
    results = append(results, BuildIndex(ctx, p.Client, p.SmartModel, capped))

    // Collect any errors from identity/rules/index. Topic errors were
    // returned above.
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

    // Refresh topics/ as a unit so removed topics vanish. This MUST be
    // after the partial-failure gate above: a failed run must not destroy
    // prior topics.
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
}

func pickKind(cs []cluster.Cluster, kind string) []cluster.Cluster {
    out := make([]cluster.Cluster, 0, len(cs))
    for _, c := range cs {
        if c.Kind == kind {
            out = append(out, c)
        }
    }
    return out
}

func readUserRules(ghostDir string) string {
    b, err := os.ReadFile(filepath.Join(ghostDir, "rules.user.md"))
    if err != nil {
        return "(rules.user.md does not exist; no user-authored rules)"
    }
    return string(b)
}
```

- [ ] **Step 3: Update pipeline tests**

Open `internal/synthesize/pipeline_test.go`. The existing tests likely use a `fakeClient` that returns one fixed response for every call. The new flow requires multiple distinct responses. Replace `fakeClient` usage with `scriptedClient` (defined in `topics_test.go`) where needed, or switch all of `pipeline_test.go` to use `scriptedClient`.

Concrete edits:

1. Find every `fakeClient{resp: "..."}` construction in `pipeline_test.go`. For each test:
   - Count expected LLM calls: 1 for identity + 1 for rules + N for topic clusters + 1 for index.
   - Replace `fakeClient` with `scriptedClient` listing exactly that many responses.
   - For topic responses, the body MUST start with a `# <Title>` line so `ParseH1` and `Slug` succeed.

2. Add one new test asserting the collision path leaves prior files intact:

```go
func TestPipelineSlugCollisionPreservesPriorTopics(t *testing.T) {
    tmp := t.TempDir()
    // Seed a prior topics dir.
    if err := os.MkdirAll(filepath.Join(tmp, "topics"), 0o755); err != nil {
        t.Fatal(err)
    }
    prior := filepath.Join(tmp, "topics", "old.md")
    if err := os.WriteFile(prior, []byte("# Old\n"), 0o644); err != nil {
        t.Fatal(err)
    }

    s := &scriptedClient{responses: []string{
        "# Identity\n\nbody.\n",
        "# Rules\n\nbody.\n",
        "# Conflict\n\nfirst.\n",
        "# Conflict\n\nsecond.\n",
        // index never reached
    }}
    cf := cluster.ClustersFile{Clusters: []cluster.Cluster{
        {Kind: "identity", Canonical: "x", Members: []cluster.ClusterMember{{Text: "x", Evidence: "turn 1"}}, EvidenceCount: 1, ProjectCount: 1},
        {Kind: "topic", Canonical: "first", Members: []cluster.ClusterMember{{Text: "first", Evidence: "turn 1", Project: "p"}}, EvidenceCount: 1, ProjectCount: 1},
        {Kind: "topic", Canonical: "second", Members: []cluster.ClusterMember{{Text: "second", Evidence: "turn 1", Project: "p"}}, EvidenceCount: 1, ProjectCount: 1},
    }}
    p := &Pipeline{Client: s, SmartModel: "smart", GhostDir: tmp, MaxTopicEntries: 20}
    if err := p.Run(context.Background(), cf); err == nil {
        t.Fatal("expected collision error")
    }
    if _, err := os.Stat(prior); err != nil {
        t.Fatalf("prior topic was destroyed by failing run: %v", err)
    }
}
```

(Add necessary imports: `os`, `path/filepath`, `context`, `testing`, `github.com/SarahFrankle/ghost/internal/cluster`.)

3. Delete or update any test that asserts on `GroupTopicClusters` behavior — that function no longer exists.

- [ ] **Step 4: Run all synthesize tests**

Run: `go test ./internal/synthesize/ -v`
Expected: PASS for all tests.

If a test fails because the scripted client ran out of responses, count the calls again — you may have missed a stage.

- [ ] **Step 5: Commit**

```bash
git add internal/synthesize/pipeline.go internal/synthesize/pipeline_test.go
git commit -m "synthesize: pipeline runs topics before index; strict atomicity on topic failure"
```

---

## Task 15: Wire up `cmd/compose.go`

**Files:**
- Modify: `cmd/compose.go`

This is the biggest single edit. Several concerns: drop canonicalize stage, drop KnownTopics plumbing, update cluster pipeline construction, update synthesize fingerprint, drop now-dead helpers.

- [ ] **Step 1: Drop the `canonicalize` stage from `parseStages` and the dispatch**

Edit the dispatch loop in `composeCmd.RunE`:

```go
for _, s := range stages {
    switch s {
    case "extract":
        if err := runExtract(cmd.Context()); err != nil {
            return err
        }
    case "cluster":
        if err := runCluster(cmd.Context()); err != nil {
            return err
        }
    case "synthesize":
        if err := runSynthesize(cmd.Context()); err != nil {
            return err
        }
    default:
        return fmt.Errorf("unknown stage %q", s)
    }
}
```

Edit `parseStages`:

```go
func parseStages(raw string) ([]string, error) {
    if raw == "all" {
        return []string{"extract", "cluster", "synthesize"}, nil
    }
    known := map[string]int{"extract": 0, "cluster": 1, "synthesize": 2}
    parts := strings.Split(raw, ",")
    for _, p := range parts {
        if _, ok := known[p]; !ok {
            return nil, fmt.Errorf("unknown stage %q (want one of: extract, cluster, synthesize, all)", p)
        }
    }
    sort.SliceStable(parts, func(i, j int) bool { return known[parts[i]] < known[parts[j]] })
    return parts, nil
}
```

Update the `--stages` flag help text:

```go
composeCmd.Flags().StringVar(&composeStages, "stages", "extract", "comma-separated stages: extract,cluster,synthesize, or all")
```

- [ ] **Step 2: Drop `runCanonicalize` and its helpers entirely**

Delete the whole `runCanonicalize` function and its helper `observedTopicSlugSamples`.

- [ ] **Step 3: Drop `KnownTopics` plumbing from `runExtract`**

Inside `runExtract`, change the `Runner` construction:

```go
runner := &extract.Runner{
    Client: client,
    Model:  cfg.Models.Cheap,
    Log:    log.Default(),
}
```

Delete the `listKnownTopics` helper.

- [ ] **Step 4: Update `runCluster` for the new threshold shape and dropped canonicalizer**

Replace the body of `runCluster` with:

```go
func runCluster(ctx context.Context) error {
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
    clustersPath := filepath.Join(stateDir, "clusters.json")

    obsFingerprints, err := cluster.ObservationsFingerprints(obsDir)
    if err != nil {
        return fmt.Errorf("scan observations fingerprints: %w", err)
    }
    embModelForFP := cfg.Models.Embedding
    if os.Getenv("VOYAGE_API_KEY") == "" {
        embModelForFP = os.Getenv("OLLAMA_EMBEDDING_MODEL")
        if embModelForFP == "" {
            embModelForFP = "nomic-embed-text"
        }
    }
    thresholdFor := func(kind string) float32 {
        if kind == "topic" {
            return float32(cfg.Thresholds.ClusterCosineTopic)
        }
        return float32(cfg.Thresholds.ClusterCosineIdentityRule)
    }
    expectedFP := cluster.ClustersFingerprint(
        obsFingerprints,
        embModelForFP,
        cfg.Models.Cheap,
        "", // formerly ClusterCanonicalSystemHash — stage 2b is gone
        thresholdFor("identity"), // include both thresholds in the FP via a deterministic encoding below
    )
    // ClustersFingerprint's last argument is a single float32 today. Including
    // both thresholds requires either adding an overload or hashing them in
    // ourselves. See Step 5.
    _ = expectedFP

    if !composeRecluster {
        if existing, err := cluster.LoadClusters(clustersPath); err == nil && existing.Fingerprint == expectedFP {
            fmt.Println("cluster: up to date (fingerprint match)")
            return nil
        }
    }

    emb, embModel := selectEmbedder(cfg.Models.Embedding)
    log.Printf("cluster: using embedder %T model=%s", emb, embModel)
    cache, err := embedding.LoadCache(filepath.Join(stateDir, "embeddings.json"), embModel)
    if err != nil {
        return err
    }

    p := &cluster.Pipeline{
        Embedder:       emb,
        EmbeddingModel: embModel,
        Cache:          cache,
        CacheSavePath:  filepath.Join(stateDir, "embeddings.json"),
        ClustersPath:   clustersPath,
        ThresholdFor:   thresholdFor,
        Workers:        cfg.Batching.ExtractWorkers,
        Log:            log.Printf,
        Fingerprint:    expectedFP,
    }
    if err := p.Run(ctx, obsDir); err != nil {
        return err
    }

    l, err := ledger.Load(filepath.Join(stateDir, "ledger.json"))
    if err != nil {
        return err
    }
    l.SetLastCompose([]string{"cluster"}, "")
    if err := l.Save(filepath.Join(stateDir, "ledger.json")); err != nil {
        return err
    }
    fmt.Println("cluster: done")
    return nil
}
```

Gone: the canonicalizer construction, the cache loader for canonicals, the alias loader, the canonicalize-prompt fingerprint contribution.

- [ ] **Step 5: Update `cluster.ClustersFingerprint` to take both thresholds**

If `cluster.ClustersFingerprint` currently takes a single `float32`, change its signature to take both thresholds. Open `internal/cluster/fingerprint.go` and adjust:

```go
func ClustersFingerprint(
    obsFingerprints []string,
    embeddingModel, cheapModel, canonicalPromptHash string,
    identityRuleThreshold, topicThreshold float32,
) string {
    return fingerprint.Compute(
        "cluster/v2",
        // ... existing inputs ...
        fmt.Sprintf("identity_rule_threshold=%.4f", identityRuleThreshold),
        fmt.Sprintf("topic_threshold=%.4f", topicThreshold),
    )
}
```

(Open the file to see current shape; adapt to match. Bump the version namespace from `v1` to `v2` so existing fingerprints definitionally miss on the first chunk-3 run — the corpus rebuild is expected.)

Then in `runCluster` call:

```go
expectedFP := cluster.ClustersFingerprint(
    obsFingerprints,
    embModelForFP,
    cfg.Models.Cheap,
    "", // stage 2b is gone; pass empty string to keep the parameter explicit
    float32(cfg.Thresholds.ClusterCosineIdentityRule),
    float32(cfg.Thresholds.ClusterCosineTopic),
)
```

Update `internal/cluster/fingerprint_test.go` if it exists and pins a specific value — the value will change because of the v2 namespace.

Optionally remove the `canonicalPromptHash` parameter entirely if no caller needs it any more (cleaner). If you do, also update tests.

- [ ] **Step 6: Update `synthesizeFingerprint`**

In `cmd/compose.go`, change `synthesizeFingerprint` to drop the now-deleted `SynthesizeTopicsSystemHash` call's contribution if the prompt rewrite implies it, and add thresholds:

```go
func synthesizeFingerprint(clustersFP, smartModel string, minRuleEvidence, minRuleProjects, maxTopicEntries int) string {
    return fingerprint.Compute(
        "synthesize/v2",
        clustersFP,
        smartModel,
        prompts.SynthesizeIdentitySystemHash(),
        prompts.SynthesizeRulesSystemHash(),
        prompts.SynthesizeTopicsSystemHash(),
        prompts.SynthesizeIndexSystemHash(),
        fmt.Sprintf("rule_evidence=%d", minRuleEvidence),
        fmt.Sprintf("rule_projects=%d", minRuleProjects),
        fmt.Sprintf("max_topics=%d", maxTopicEntries),
    )
}
```

The bump from `v1` to `v2` guarantees stale synthesis caches miss on the first chunk-3 run.

- [ ] **Step 7: Drop the alias loader from imports**

Remove `"github.com/SarahFrankle/ghost/internal/canonicalize"` from the import block at the top of `cmd/compose.go`. If `runSynthesize` referenced anything from that package, fix it too (it shouldn't).

- [ ] **Step 8: Build the whole repo**

Run: `go build ./...`
Expected: PASS. If anything is unresolved (e.g. `cmd/estimate.go` referencing a deleted helper, `cmd/status.go` referencing canonicalize state), fix those call sites — they may need similar trims. Run `grep -rn 'canonicalize\|KnownTopics\|TopicAliases' cmd/ internal/` to find leftovers.

- [ ] **Step 9: Run all tests**

Run: `go test ./...`
Expected: PASS. Any failure means a fingerprint test expected the old value or a cmd test referenced a deleted helper — fix in place.

- [ ] **Step 10: Commit**

```bash
git add cmd/compose.go internal/cluster/fingerprint.go internal/cluster/fingerprint_test.go
git commit -m "cmd/compose: drop canonicalize stage and KnownTopics plumbing; wire per-kind thresholds"
```

---

## Task 16: Sweep for leftover references

**Files:** anywhere a stale name might linger

- [ ] **Step 1: Grep for ghosts**

Run each of these and confirm zero matches (excluding the design docs and this plan):

```bash
grep -rn 'canonicalize' --include='*.go' .
grep -rn 'KnownTopics' --include='*.go' .
grep -rn 'TopicAliases' --include='*.go' .
grep -rn 'KNOWN TOPICS' --include='*.md' prompts/
grep -rn 'ClusterCosineThreshold\b' --include='*.go' .
grep -rn 'GroupTopicClusters\|RankedTopic\|RankTopicsByEvidence\|CapTopics' --include='*.go' .
grep -rn '"Topic"' --include='*.go' internal/extract/
```

Expected: no matches in Go source. If `cmd/estimate.go`, `cmd/status.go`, or `cmd/show.go` still reference any of these, edit them now.

- [ ] **Step 2: Build and test the whole tree**

Run: `go build ./... && go test ./...`
Expected: PASS.

- [ ] **Step 3: Commit any sweep edits**

If the previous step produced any edits:

```bash
git add -A
git commit -m "sweep: remove residual references to canonicalize / KnownTopics / TopicAliases"
```

If nothing changed, skip the commit.

---

## Task 17: Migration script for `~/.ghost/`

**Files:** none. This is an instruction for the user, not code.

- [ ] **Step 1: Document the state files that must be removed**

The corpus rebuild is paid once. Before running the first chunk-3 `ghost compose`, Sarah should remove stale state files. Print these commands to the chunk handoff (or run them when ready):

```bash
rm -f ~/.ghost/.state/clusters.json
rm -f ~/.ghost/.state/canonical_cache.json
rm -f ~/.ghost/.state/slug_aliases.json
rm -f ~/.ghost/.state/synthesize.fingerprint
rm -rf ~/.ghost/topics/
```

Observations and embeddings stay on disk — their content-hash gating still applies, and they'll re-extract automatically as the extract fingerprint (`extract/v1`) sees that the prompt changed.

Config edits: if `~/.ghost/config.toml` set a custom `cluster_cosine_threshold` or `canonicalize_similarity_threshold`, edit it:

- Rename `cluster_cosine_threshold` → `cluster_cosine_identity_rule`.
- Add `cluster_cosine_topic = 0.75`.
- Delete `canonicalize_similarity_threshold`.

For Sarah (using defaults) no edit is needed.

- [ ] **Step 2: Commit** (no-op if nothing changed)

This task is documentation-only. No commit.

---

## Task 18: End-to-end verification

**Files:** none — this is a manual run.

- [ ] **Step 1: Estimate cost**

Run: `go run . compose --estimate --stages all`
Expected: a token + cost estimate. Note the synthesize line — it should be smart-model body calls × the cluster count (no separate naming calls). Sanity-check the number against your wallet budget before proceeding.

- [ ] **Step 2: Run extract on a small sample**

Run: `go run . compose --stages extract --limit 5`
Expected: 5 transcripts re-extract because the extract prompt hash changed; observations land in `.state/observations/`.

- [ ] **Step 3: Run cluster**

Run: `go run . compose --stages cluster`
Expected: `cluster: done`. `~/.ghost/.state/clusters.json` exists with a `cluster/v2` namespace fingerprint.

- [ ] **Step 4: Run synthesize**

Run: `go run . compose --stages synthesize`
Expected: `synthesize: wrote identity.md, rules.md, topics/*.md, index.md`. Inspect `~/.ghost/topics/` — filenames should be kebab-case derived from each file's `# <Title>` first line.

- [ ] **Step 5: Verify stability on re-run**

Run: `go run . compose --stages synthesize` again immediately.
Expected: `synthesize: up to date (fingerprint match)`.

- [ ] **Step 6: Verify slug consistency**

Spot-check 3–4 topic files:

```bash
for f in ~/.ghost/topics/*.md; do
  title=$(head -1 "$f" | sed 's/^# //')
  slug=$(basename "$f" .md)
  echo "$slug  ←  $title"
done
```

Expected: each slug is the lowercase, kebab-case form of its title.

- [ ] **Step 7: Verify synonym merging**

If your prior corpus had separate `docs` and `documentation` topics, they should now be in one file. If they're still split, the topic cosine threshold (`0.75`) may be too tight for your embedder — tune via `~/.ghost/config.toml` and re-run from cluster.

- [ ] **Step 8: Run the full test suite once more**

Run: `go test ./...`
Expected: PASS.

- [ ] **Step 9: Final sanity grep**

Run: `grep -rn 'slug_aliases\|canonical_cache' ~/.ghost/.state/ 2>/dev/null`
Expected: no matches (the files were removed in Task 17 and nothing recreates them).

---

## Done condition

Chunk 3 is complete when:

1. `go test ./...` passes from a clean build.
2. `internal/canonicalize/` no longer exists.
3. `grep -rn 'KNOWN TOPICS' prompts/` returns no matches.
4. `grep -rn 'Topic ' internal/extract/schema.go` returns no matches.
5. `ghost compose` on the real corpus produces a populated `~/.ghost/topics/` with no `.state/slug_aliases.json` or `.state/canonical_cache.json` recreated.
6. Re-running `ghost compose` immediately afterwards produces `up to date (fingerprint match)` and the same slug set.
7. At least one obvious-synonym pair from prior runs (e.g. `docs` and `documentation`) now lands in the same topic file.

When all green, squash-merge the chunk branch to `main` as one commit per [[feedback-squash-per-chunk]]:

```bash
git checkout main
git merge --squash chunk-3-embedding-topics
git commit -m "chunk 3: embedding-based topics (delete canonicalize; H1-derived slugs)"
```
