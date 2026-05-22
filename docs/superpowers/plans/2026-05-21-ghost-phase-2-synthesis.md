# Ghost Phase 2 — Cluster + Synthesize Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn the observation files produced by Phase 1 into two always-loaded markdown files (`~/.ghost/identity.md`, `~/.ghost/rules.md`) by adding stage 2 clustering (embeddings + cheap-LLM canonical phrasing + Go-side counts) and stage 3 synthesis (smart model, atomic multi-file write).

**Architecture:**
- Stage 2 (`ghost compose --stages cluster`) loads every `.state/observations/*.json`, embeds each observation's `text` via Voyage (cached by `(obs_hash, model_id)` in `.state/embeddings.json`), partitions by `(kind, sub_key)`, performs single-linkage agglomerative bucketing at `cluster_cosine_threshold`, picks a canonical phrasing per multi-entry bucket with the cheap model, then writes `.state/clusters.json` with Go-computed `EvidenceCount` / `ProjectCount`.
- Stage 3 (`ghost compose --stages synthesize`) reads `clusters.json`, runs one smart-model call per output file (`identity.md`, `rules.md`) into a sibling tmpdir, and atomically renames into `~/.ghost/`. Rules are filtered by evidence ≥2 ∧ projects ≥2 *in Go* before the LLM call; the prompt also receives the current `rules.user.md` for subtractive synthesis. Partial failure preserves the prior generation.
- Smart and cheap LLM calls reuse the existing `anthropic.Client` (shell-out to `claude -p`). Voyage is a new HTTP client behind an `Embedder` interface so tests stay LLM-free.

**Tech Stack:** Go 1.22+, existing internal packages (`anthropic`, `atomicfs`, `config`, `ledger`, `paths`), `net/http` for Voyage, `crypto/sha256` for cache keys. No new third-party deps.

**Spec reference:** `docs/specs/2026-05-20-ghost-design.md` — Stage 2 (lines 308–337), Stage 3 (339–401), Always-loaded budget (403–421), Phase 2 phasing (688–705).

**Phase 1 deferrals carried in:** All three "Important" items from the Phase 1 review have been fixed on `phase-1-extract` before Phase 2 begins (ledger save cadence, balanced JSON brace scan, secret-scrub false-positive removal). The minor items remain deferred.

---

## File Structure

```
ghost/
  cmd/
    compose.go                       # extend --stages parsing (cluster, synthesize, all)
    status.go                        # surface cluster + synthesis state
  internal/
    embedding/
      embedding.go                   # Embedder interface, cosine, observation_hash
      embedding_test.go
      voyage.go                      # Voyage HTTP client implementing Embedder
      cache.go                       # embeddings.json: load, lookup, store, model-id gate
      cache_test.go
    cluster/
      types.go                       # Cluster, ClusterMember, ClustersFile
      bucket.go                      # partition + agglomerative single-linkage merge
      bucket_test.go
      canonical.go                   # cheap-LLM canonical phrasing per bucket
      canonical_test.go
      pipeline.go                    # Run(): observations → clusters.json
      pipeline_test.go
      io.go                          # load/save clusters.json
      io_test.go
    synthesize/
      types.go                       # FileResult, Run summary
      identity.go                    # build prompt + invoke smart model
      rules.go                       # filter + subtractive prompt + invoke smart model
      pipeline.go                    # tmpdir orchestration + atomic rename + partial-failure
      pipeline_test.go
  prompts/
    cluster.canonical.system.md      # 2b system prompt
    synthesize.identity.system.md    # stage 3 identity prompt
    synthesize.rules.system.md       # stage 3 rules prompt (subtractive)
    prompts.go                       # extend with three new accessors
```

Each `_test.go` file uses fakes (no real network/LLM calls). The Voyage HTTP path is exercised through `httptest.Server`. LLM paths use the `fakeClient` pattern already in `internal/extract/extract_test.go`.

---

## Conventions

- **Atomic writes:** single files use `internal/atomicfs.WriteFile`. Directories use tmpdir + `os.Rename` of each file in turn (the spec's "atomic multi-file" rename is per-file once all generations succeed — POSIX has no atomic dir merge).
- **Cache schema:** every state file carries `schema_version: 1`. `Load` accepts only schema_version==1; anything higher returns an error.
- **Observation hash:** `sha256("<kind>|<sub_key>|<text>")` lowercase hex. Stable across runs so embeddings cache durably.
- **Sub-key:** identity→`""`, rule→`""`, voice→`context`, topic→`topic`.
- **Concurrency:** stage 2 embedding fetches use a bounded worker pool (default 5, reuse `config.Batching.ExtractWorkers`). Canonical phrasing is sequential to keep prompt isolation simple.
- **No new deps.** Voyage uses `net/http`. JSON via `encoding/json`. SHA256 via `crypto/sha256`.

---

## Task 1: Embedder interface + cosine + observation hash

**Files:**
- Create: `internal/embedding/embedding.go`
- Create: `internal/embedding/embedding_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/embedding/embedding_test.go
package embedding

import (
	"math"
	"testing"
)

func TestObservationHashIsStable(t *testing.T) {
	a := ObservationHash("rule", "", "prefer integration tests")
	b := ObservationHash("rule", "", "prefer integration tests")
	if a != b {
		t.Fatalf("hash unstable: %s vs %s", a, b)
	}
	if len(a) != 64 {
		t.Fatalf("hash length = %d, want 64 hex chars", len(a))
	}
	if ObservationHash("rule", "", "x") == ObservationHash("identity", "", "x") {
		t.Fatalf("kind not part of hash")
	}
	if ObservationHash("voice", "cli-chat", "x") == ObservationHash("voice", "slack", "x") {
		t.Fatalf("sub_key not part of hash")
	}
}

func TestCosine(t *testing.T) {
	got := Cosine([]float32{1, 0}, []float32{1, 0})
	if math.Abs(float64(got)-1.0) > 1e-6 {
		t.Fatalf("identical vectors cosine = %v, want 1", got)
	}
	got = Cosine([]float32{1, 0}, []float32{0, 1})
	if math.Abs(float64(got)) > 1e-6 {
		t.Fatalf("orthogonal cosine = %v, want 0", got)
	}
	if Cosine([]float32{1, 0}, []float32{0, 0}) != 0 {
		t.Fatalf("zero-vector cosine must be 0, not NaN")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/embedding/`
Expected: compile error (package not built yet).

- [ ] **Step 3: Implement**

```go
// internal/embedding/embedding.go
package embedding

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"math"
)

// Embedder is the minimal surface stage 2 needs. Implementations must
// return one vector per input text, in the same order.
type Embedder interface {
	Embed(ctx context.Context, model string, texts []string) ([][]float32, error)
}

// ObservationHash is the cache key for an observation's embedding.
// Kind + sub_key + text fully determine the embedded payload.
func ObservationHash(kind, subKey, text string) string {
	sum := sha256.Sum256([]byte(kind + "|" + subKey + "|" + text))
	return hex.EncodeToString(sum[:])
}

// Cosine returns the cosine similarity of two vectors, or 0 if either
// has zero magnitude (defensive — Voyage does not normally return zero
// vectors but a malformed response shouldn't NaN-poison the cluster step).
func Cosine(a, b []float32) float32 {
	if len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return float32(dot / (math.Sqrt(na) * math.Sqrt(nb)))
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/embedding/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/embedding/embedding.go internal/embedding/embedding_test.go
git commit -m "feat(embedding): add Embedder interface, ObservationHash, Cosine"
```

---

## Task 2: Embedding cache (embeddings.json with model-id gate)

**Files:**
- Create: `internal/embedding/cache.go`
- Create: `internal/embedding/cache_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/embedding/cache_test.go
package embedding

import (
	"path/filepath"
	"testing"
)

func TestCacheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "embeddings.json")

	c, err := LoadCache(path, "voyage-3-lite")
	if err != nil {
		t.Fatal(err)
	}
	if !c.Empty() {
		t.Fatalf("fresh cache should be empty")
	}
	c.Put("hash-a", []float32{1, 2, 3})
	if err := c.Save(path); err != nil {
		t.Fatal(err)
	}

	c2, err := LoadCache(path, "voyage-3-lite")
	if err != nil {
		t.Fatal(err)
	}
	v, ok := c2.Get("hash-a")
	if !ok || len(v) != 3 || v[0] != 1 {
		t.Fatalf("roundtrip lost vector: %v %v", v, ok)
	}
}

func TestCacheModelMismatchDiscards(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "embeddings.json")
	c, _ := LoadCache(path, "voyage-3-lite")
	c.Put("hash-a", []float32{1})
	_ = c.Save(path)

	c2, err := LoadCache(path, "voyage-3-medium")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := c2.Get("hash-a"); ok {
		t.Fatalf("model-id mismatch must discard cached entries")
	}
	if c2.Model() != "voyage-3-medium" {
		t.Fatalf("new model not adopted")
	}
}
```

- [ ] **Step 2: Run tests, verify they fail**

Run: `go test ./internal/embedding/ -run Cache`
Expected: undefined symbols.

- [ ] **Step 3: Implement**

```go
// internal/embedding/cache.go
package embedding

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"sync"

	"github.com/SarahFrankle/ghost/internal/atomicfs"
)

const cacheSchemaVersion = 1

type cacheFile struct {
	SchemaVersion    int                  `json:"schema_version"`
	EmbeddingModelID string               `json:"embedding_model_id"`
	Entries          map[string][]float32 `json:"entries"`
}

// Cache is an in-memory embedding cache backed by embeddings.json.
// A model-id mismatch on load discards every entry — cosine
// distributions differ across models, so old vectors are unsafe.
type Cache struct {
	mu      sync.Mutex
	model   string
	entries map[string][]float32
}

func LoadCache(path, currentModel string) (*Cache, error) {
	c := &Cache{model: currentModel, entries: map[string][]float32{}}
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return c, nil
		}
		return nil, err
	}
	var raw cacheFile
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	if raw.SchemaVersion > cacheSchemaVersion {
		return nil, errors.New("embeddings.json schema_version newer than binary supports")
	}
	if raw.EmbeddingModelID == currentModel && raw.Entries != nil {
		c.entries = raw.Entries
	}
	return c, nil
}

func (c *Cache) Empty() bool       { c.mu.Lock(); defer c.mu.Unlock(); return len(c.entries) == 0 }
func (c *Cache) Model() string     { c.mu.Lock(); defer c.mu.Unlock(); return c.model }
func (c *Cache) Get(h string) ([]float32, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.entries[h]
	return v, ok
}
func (c *Cache) Put(h string, v []float32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[h] = v
}

func (c *Cache) Save(path string) error {
	c.mu.Lock()
	body, err := json.MarshalIndent(cacheFile{
		SchemaVersion:    cacheSchemaVersion,
		EmbeddingModelID: c.model,
		Entries:          c.entries,
	}, "", "  ")
	c.mu.Unlock()
	if err != nil {
		return err
	}
	return atomicfs.WriteFile(path, body, 0o644)
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/embedding/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/embedding/cache.go internal/embedding/cache_test.go
git commit -m "feat(embedding): add model-id-gated cache for embeddings.json"
```

---

## Task 3: Voyage HTTP client

**Files:**
- Create: `internal/embedding/voyage.go`
- Modify: `internal/embedding/embedding_test.go` (add Voyage test using httptest)

- [ ] **Step 1: Write the failing test**

```go
// append to internal/embedding/embedding_test.go
func TestVoyageEmbedHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("missing/wrong auth header: %q", r.Header.Get("Authorization"))
		}
		var body struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Model != "voyage-3-lite" {
			t.Errorf("model = %q", body.Model)
		}
		if len(body.Input) != 2 {
			t.Errorf("expected 2 inputs, got %d", len(body.Input))
		}
		_, _ = w.Write([]byte(`{"data":[{"embedding":[0.1,0.2],"index":0},{"embedding":[0.3,0.4],"index":1}]}`))
	}))
	defer srv.Close()

	v := &Voyage{APIKey: "test-key", BaseURL: srv.URL, HTTPClient: srv.Client()}
	vecs, err := v.Embed(context.Background(), "voyage-3-lite", []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 2 || vecs[0][0] != 0.1 || vecs[1][1] != 0.4 {
		t.Fatalf("unexpected vectors: %v", vecs)
	}
}

func TestVoyageReturnsErrorOn4xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer srv.Close()
	v := &Voyage{APIKey: "x", BaseURL: srv.URL, HTTPClient: srv.Client()}
	_, err := v.Embed(context.Background(), "voyage-3-lite", []string{"a"})
	if err == nil {
		t.Fatal("expected error on 401")
	}
}
```

Also add to the existing imports at the top of `embedding_test.go`:

```go
import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)
```

- [ ] **Step 2: Run tests, verify they fail**

Run: `go test ./internal/embedding/`
Expected: undefined `Voyage`.

- [ ] **Step 3: Implement**

```go
// internal/embedding/voyage.go
package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const DefaultVoyageBaseURL = "https://api.voyageai.com/v1"

// Voyage implements Embedder via the Voyage AI HTTP API.
//
// Auth uses the VOYAGE_API_KEY environment variable when constructed
// via NewVoyageFromEnv. Tests inject APIKey, BaseURL, and HTTPClient
// directly to avoid touching the environment or the real endpoint.
type Voyage struct {
	APIKey     string
	BaseURL    string
	HTTPClient *http.Client
}

func NewVoyageFromEnv() (*Voyage, error) {
	key := os.Getenv("VOYAGE_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("VOYAGE_API_KEY not set (required for ghost cluster stage)")
	}
	return &Voyage{
		APIKey:     key,
		BaseURL:    DefaultVoyageBaseURL,
		HTTPClient: &http.Client{Timeout: 60 * time.Second},
	}, nil
}

type voyageRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type voyageResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
}

func (v *Voyage) Embed(ctx context.Context, model string, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	body, err := json.Marshal(voyageRequest{Model: model, Input: texts})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", v.BaseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+v.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := v.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("voyage embed: status %d: %s", resp.StatusCode, string(raw))
	}
	var parsed voyageResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("voyage embed: decode: %w", err)
	}
	if len(parsed.Data) != len(texts) {
		return nil, fmt.Errorf("voyage embed: returned %d vectors for %d inputs", len(parsed.Data), len(texts))
	}
	// Voyage may return out-of-order; sort by index.
	out := make([][]float32, len(texts))
	for _, d := range parsed.Data {
		if d.Index < 0 || d.Index >= len(out) {
			return nil, fmt.Errorf("voyage embed: bad index %d", d.Index)
		}
		out[d.Index] = d.Embedding
	}
	return out, nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/embedding/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/embedding/voyage.go internal/embedding/embedding_test.go
git commit -m "feat(embedding): add Voyage HTTP client implementing Embedder"
```

---

## Task 4: Cluster types + agglomerative bucketing

**Files:**
- Create: `internal/cluster/types.go`
- Create: `internal/cluster/bucket.go`
- Create: `internal/cluster/bucket_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/cluster/bucket_test.go
package cluster

import (
	"testing"
)

func TestBucketGroupsSimilarMembers(t *testing.T) {
	members := []ClusterMember{
		{Kind: "rule", Text: "use integration tests", Project: "ghost"},
		{Kind: "rule", Text: "prefer real DB tests", Project: "other"},
		{Kind: "rule", Text: "use semantic commits", Project: "ghost"},
		{Kind: "identity", Text: "works at Miro", Project: "ghost"},
	}
	vecs := map[int][]float32{
		0: {1, 0, 0},
		1: {0.95, 0.1, 0},
		2: {0, 1, 0},
		3: {0, 0, 1},
	}
	clusters := Bucket(members, func(i int) []float32 { return vecs[i] }, 0.85)

	if len(clusters) != 3 {
		t.Fatalf("expected 3 clusters (2 rule + 1 identity), got %d: %+v", len(clusters), clusters)
	}
	for _, c := range clusters {
		if c.Kind == "rule" && len(c.Members) == 2 {
			if c.EvidenceCount != 2 || c.ProjectCount != 2 {
				t.Fatalf("merged rule cluster counts wrong: ev=%d proj=%d", c.EvidenceCount, c.ProjectCount)
			}
		}
	}
}

func TestBucketDoesNotCrossKind(t *testing.T) {
	members := []ClusterMember{
		{Kind: "rule", Text: "x"},
		{Kind: "identity", Text: "x"},
	}
	identical := func(int) []float32 { return []float32{1, 0} }
	clusters := Bucket(members, identical, 0.5)
	if len(clusters) != 2 {
		t.Fatalf("identity and rule must not merge: got %d clusters", len(clusters))
	}
}

func TestBucketRespectsSubKey(t *testing.T) {
	members := []ClusterMember{
		{Kind: "voice", Context: "cli-chat", Text: "lowercase"},
		{Kind: "voice", Context: "slack", Text: "lowercase"},
	}
	identical := func(int) []float32 { return []float32{1, 0} }
	clusters := Bucket(members, identical, 0.5)
	if len(clusters) != 2 {
		t.Fatalf("different voice contexts must not merge: got %d", len(clusters))
	}
}
```

- [ ] **Step 2: Run tests, verify they fail**

Run: `go test ./internal/cluster/`
Expected: undefined symbols.

- [ ] **Step 3: Implement types**

```go
// internal/cluster/types.go
package cluster

import "time"

const SchemaVersion = 1

// ClusterMember is one observation as it appears inside a cluster.
type ClusterMember struct {
	ObservationHash string `json:"observation_hash"`
	Source          string `json:"source"`
	Project         string `json:"project"`
	Kind            string `json:"kind"`
	Text            string `json:"text"`
	Evidence        string `json:"evidence"`
	Context         string `json:"context,omitempty"`
	Topic           string `json:"topic,omitempty"`
	Confidence      string `json:"confidence,omitempty"`
}

// SubKey returns the partition discriminator inside a kind.
// voice → Context, topic → Topic, others → "".
func (m ClusterMember) SubKey() string {
	switch m.Kind {
	case "voice":
		return m.Context
	case "topic":
		return m.Topic
	}
	return ""
}

// Cluster is a group of observations that describe the same thing.
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
	Clusters         []Cluster `json:"clusters"`
}
```

- [ ] **Step 4: Implement bucket**

```go
// internal/cluster/bucket.go
package cluster

import "github.com/SarahFrankle/ghost/internal/embedding"

// Bucket partitions members by (kind, sub_key) and within each partition
// merges members whose embeddings are within `threshold` cosine of an
// existing cluster's first member (single-linkage agglomerative).
//
// vecOf maps a member index to its embedding. The function never calls
// the embedder itself — callers pre-load vectors so this stays purely
// deterministic and trivially testable.
//
// Buckets of size 1 are returned as-is. Counts are computed here so the
// LLM never produces a number that drives a downstream threshold.
func Bucket(members []ClusterMember, vecOf func(i int) []float32, threshold float32) []Cluster {
	// 1) partition by (kind, sub_key)
	type key struct{ kind, sub string }
	parts := map[key][]int{}
	for i, m := range members {
		k := key{m.Kind, m.SubKey()}
		parts[k] = append(parts[k], i)
	}

	var out []Cluster
	for k, idxs := range parts {
		// Single-linkage greedy: for each member, attach to first existing
		// cluster whose seed vector is within threshold, else start new.
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
				Canonical:     members[c[0]].Text, // placeholder; set by canonical step
				Members:       mems,
				EvidenceCount: len(mems),
				ProjectCount:  len(projects),
			})
		}
	}
	return out
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/cluster/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/cluster/types.go internal/cluster/bucket.go internal/cluster/bucket_test.go
git commit -m "feat(cluster): add agglomerative bucketing with Go-side counts"
```

---

## Task 5: Canonical-phrasing LLM step (stage 2b)

**Files:**
- Create: `prompts/cluster.canonical.system.md`
- Modify: `prompts/prompts.go` (add accessor)
- Create: `internal/cluster/canonical.go`
- Create: `internal/cluster/canonical_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/cluster/canonical_test.go
package cluster

import (
	"context"
	"strings"
	"testing"
)

type fakeLLM struct{ resp string }

func (f *fakeLLM) Complete(ctx context.Context, model, system, user string) (string, error) {
	return f.resp, nil
}

func TestCanonicalizeSingletonSkipsLLM(t *testing.T) {
	clusters := []Cluster{
		{Kind: "rule", Members: []ClusterMember{{Text: "only one"}}, Canonical: "only one"},
	}
	called := false
	c := &Canonicalizer{Client: &fakeLLM{resp: "SHOULD NOT BE CALLED"}, Model: "x"}
	c.OnCall = func() { called = true }
	if err := c.Apply(context.Background(), clusters); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("singleton bucket must skip LLM")
	}
	if clusters[0].Canonical != "only one" {
		t.Fatalf("singleton canonical mutated: %q", clusters[0].Canonical)
	}
}

func TestCanonicalizeMultiUsesLLMResponse(t *testing.T) {
	clusters := []Cluster{
		{Kind: "rule", Members: []ClusterMember{{Text: "A"}, {Text: "B"}}, Canonical: "A"},
	}
	c := &Canonicalizer{Client: &fakeLLM{resp: `{"canonical":"prefer X"}`}, Model: "x"}
	if err := c.Apply(context.Background(), clusters); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(clusters[0].Canonical, "prefer X") {
		t.Fatalf("canonical not set from LLM: %q", clusters[0].Canonical)
	}
}
```

- [ ] **Step 2: Run tests, verify they fail**

Run: `go test ./internal/cluster/`
Expected: undefined symbols.

- [ ] **Step 3: Create the system prompt**

Write `prompts/cluster.canonical.system.md`:

```markdown
You are given a small set of short observations that an upstream
clustering step grouped together because they appear semantically
similar. They were extracted from one user's Claude Code transcripts.

Your job: pick the single best canonical phrasing that captures what
all the observations have in common, and confirm they truly describe
the same thing.

Constraints:
- The canonical phrasing must be a single sentence, lowercase if the
  members are lowercase, no em-dashes, no self-congratulation.
- Stay grounded in the members. Do not invent attributes that are not
  supported by at least one member.
- If the members do NOT actually describe the same thing, set
  `same: false` and `canonical` to the empty string.

Respond with strict JSON in this shape:

{
  "canonical": "the canonical phrasing",
  "same": true
}

No prose, no markdown fences. Just the JSON object.
```

- [ ] **Step 4: Extend the prompts package**

Edit `prompts/prompts.go` — add embed + accessor below the existing ones:

```go
//go:embed cluster.canonical.system.md
var clusterCanonicalSystem string

// ClusterCanonicalSystem returns the embedded prompt for stage 2b.
func ClusterCanonicalSystem() string { return clusterCanonicalSystem }
```

- [ ] **Step 5: Implement Canonicalizer**

```go
// internal/cluster/canonical.go
package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/SarahFrankle/ghost/internal/anthropic"
	"github.com/SarahFrankle/ghost/prompts"
)

// Canonicalizer fills in Cluster.Canonical for multi-member clusters
// using a cheap-model call. Single-member clusters keep their member's
// text. Failures are non-fatal: the cluster keeps its seed text and the
// error is reported via Log.
type Canonicalizer struct {
	Client anthropic.Client
	Model  string
	Log    func(format string, args ...any)
	OnCall func() // test hook; nil in production
}

type canonicalResponse struct {
	Canonical string `json:"canonical"`
	Same      bool   `json:"same"`
}

func (c *Canonicalizer) Apply(ctx context.Context, clusters []Cluster) error {
	for i := range clusters {
		if len(clusters[i].Members) < 2 {
			continue
		}
		if c.OnCall != nil {
			c.OnCall()
		}
		payload := renderForCanonical(clusters[i])
		raw, err := c.Client.Complete(ctx, c.Model, prompts.ClusterCanonicalSystem(), payload)
		if err != nil {
			c.logf("canonical: cluster %d %s: %v", i, clusters[i].Kind, err)
			continue
		}
		parsed, err := parseCanonical(raw)
		if err != nil {
			c.logf("canonical: cluster %d: parse: %v", i, err)
			continue
		}
		if !parsed.Same || strings.TrimSpace(parsed.Canonical) == "" {
			c.logf("canonical: cluster %d: model says members are not the same; keeping seed", i)
			continue
		}
		clusters[i].Canonical = parsed.Canonical
	}
	return nil
}

func (c *Canonicalizer) logf(format string, args ...any) {
	if c.Log != nil {
		c.Log(format, args...)
	}
}

func renderForCanonical(cl Cluster) string {
	var b strings.Builder
	fmt.Fprintf(&b, "kind: %s\n", cl.Kind)
	if cl.SubKey != "" {
		fmt.Fprintf(&b, "sub_key: %s\n", cl.SubKey)
	}
	b.WriteString("members:\n")
	for i, m := range cl.Members {
		fmt.Fprintf(&b, "  %d: %s\n", i+1, m.Text)
	}
	return b.String()
}

// parseCanonical reuses the same balanced-brace strategy as extract
// but is duplicated here to keep cluster independent of extract. If
// either parser changes meaningfully, prefer extracting a shared util.
func parseCanonical(raw string) (canonicalResponse, error) {
	start := strings.Index(raw, "{")
	if start < 0 {
		return canonicalResponse{}, fmt.Errorf("no JSON object")
	}
	depth, inStr, esc := 0, false, false
	for i := start; i < len(raw); i++ {
		c := raw[i]
		if inStr {
			if esc {
				esc = false
				continue
			}
			switch c {
			case '\\':
				esc = true
			case '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				var out canonicalResponse
				if err := json.Unmarshal([]byte(raw[start:i+1]), &out); err != nil {
					return out, err
				}
				return out, nil
			}
		}
	}
	return canonicalResponse{}, fmt.Errorf("unbalanced JSON")
}
```

- [ ] **Step 6: Run tests**

Run: `go test ./internal/cluster/ ./prompts/...`
Expected: PASS (prompts has no tests; ensure build).

- [ ] **Step 7: Commit**

```bash
git add prompts/cluster.canonical.system.md prompts/prompts.go internal/cluster/canonical.go internal/cluster/canonical_test.go
git commit -m "feat(cluster): add cheap-LLM canonical phrasing (stage 2b)"
```

---

## Task 6: Cluster pipeline + clusters.json IO

**Files:**
- Create: `internal/cluster/io.go`
- Create: `internal/cluster/io_test.go`
- Create: `internal/cluster/pipeline.go`
- Create: `internal/cluster/pipeline_test.go`

- [ ] **Step 1: Write the IO test**

```go
// internal/cluster/io_test.go
package cluster

import (
	"path/filepath"
	"testing"
	"time"
)

func TestClustersFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "clusters.json")
	in := ClustersFile{
		SchemaVersion:    SchemaVersion,
		EmbeddingModelID: "voyage-3-lite",
		BuiltAt:          time.Now().UTC(),
		Clusters: []Cluster{
			{Kind: "rule", Canonical: "x", Members: []ClusterMember{{Text: "x"}}, EvidenceCount: 1, ProjectCount: 1},
		},
	}
	if err := SaveClusters(path, in); err != nil {
		t.Fatal(err)
	}
	out, err := LoadClusters(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Clusters) != 1 || out.Clusters[0].Canonical != "x" {
		t.Fatalf("round-trip mismatch: %+v", out)
	}
}
```

- [ ] **Step 2: Write the pipeline test**

```go
// internal/cluster/pipeline_test.go
package cluster

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

type fakeEmbedder struct{}

func (fakeEmbedder) Embed(ctx context.Context, model string, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		// Trivial deterministic embedding: first byte → 1-hot in dim 8.
		v := make([]float32, 8)
		if len(t) > 0 {
			v[int(t[0])%8] = 1
		}
		out[i] = v
	}
	return out, nil
}

func TestPipelineProducesClustersJSON(t *testing.T) {
	stateDir := t.TempDir()
	obsDir := filepath.Join(stateDir, "observations")
	if err := os.MkdirAll(obsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Two observation files, three rules, one of which is duplicated text.
	body := `{
		"source": "/p/proj-a.jsonl", "project": "proj-a", "content_hash": "sha256:a",
		"extracted_at": "2026-05-21T00:00:00Z",
		"observations": [
			{"kind":"rule","text":"alpha rule","evidence":"turn 1"},
			{"kind":"identity","text":"works at Miro","evidence":"turn 2"}
		]
	}`
	if err := os.WriteFile(filepath.Join(obsDir, "a.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	body2 := `{
		"source": "/p/proj-b.jsonl", "project": "proj-b", "content_hash": "sha256:b",
		"extracted_at": "2026-05-21T00:00:00Z",
		"observations": [
			{"kind":"rule","text":"alpha rule","evidence":"turn 5"}
		]
	}`
	if err := os.WriteFile(filepath.Join(obsDir, "b.json"), []byte(body2), 0o644); err != nil {
		t.Fatal(err)
	}

	cache, _ := loadCacheForTest(t, stateDir, "test-emb")

	p := &Pipeline{
		Embedder:        fakeEmbedder{},
		EmbeddingModel:  "test-emb",
		Cache:           cache,
		CacheSavePath:   filepath.Join(stateDir, "embeddings.json"),
		ClustersPath:    filepath.Join(stateDir, "clusters.json"),
		CosineThreshold: 0.85,
		Canonicalizer:   nil, // skip 2b in this test
		Workers:         2,
	}
	if err := p.Run(context.Background(), obsDir); err != nil {
		t.Fatal(err)
	}
	got, err := LoadClusters(p.ClustersPath)
	if err != nil {
		t.Fatal(err)
	}
	// alpha rule (proj-a) + alpha rule (proj-b) → one cluster ev=2 proj=2
	// identity → its own cluster ev=1
	var foundMergedRule bool
	for _, c := range got.Clusters {
		if c.Kind == "rule" && c.EvidenceCount == 2 && c.ProjectCount == 2 {
			foundMergedRule = true
		}
	}
	if !foundMergedRule {
		t.Fatalf("expected merged rule cluster ev=2 proj=2, got: %+v", got.Clusters)
	}
}
```

Add helper at the top of `pipeline_test.go`:

```go
import (
	"github.com/SarahFrankle/ghost/internal/embedding"
)

func loadCacheForTest(t *testing.T, stateDir, model string) (*embedding.Cache, string) {
	t.Helper()
	path := filepath.Join(stateDir, "embeddings.json")
	c, err := embedding.LoadCache(path, model)
	if err != nil {
		t.Fatal(err)
	}
	return c, path
}
```

- [ ] **Step 3: Verify failing**

Run: `go test ./internal/cluster/`
Expected: undefined `Pipeline`, `LoadClusters`, `SaveClusters`.

- [ ] **Step 4: Implement io.go**

```go
// internal/cluster/io.go
package cluster

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/SarahFrankle/ghost/internal/atomicfs"
)

func SaveClusters(path string, f ClustersFile) error {
	if f.SchemaVersion == 0 {
		f.SchemaVersion = SchemaVersion
	}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return atomicfs.WriteFile(path, b, 0o644)
}

func LoadClusters(path string) (ClustersFile, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return ClustersFile{}, err
	}
	var f ClustersFile
	if err := json.Unmarshal(b, &f); err != nil {
		return f, err
	}
	if f.SchemaVersion > SchemaVersion {
		return f, fmt.Errorf("clusters.json schema_version=%d newer than binary supports", f.SchemaVersion)
	}
	return f, nil
}
```

- [ ] **Step 5: Implement pipeline.go**

```go
// internal/cluster/pipeline.go
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
// observation (cache-aware), bucket, optionally canonicalize, then
// write clusters.json. The Canonicalizer field is optional; if nil,
// 2b is skipped (clusters keep their seed canonical text).
type Pipeline struct {
	Embedder        embedding.Embedder
	EmbeddingModel  string
	Cache           *embedding.Cache
	CacheSavePath   string
	ClustersPath    string
	CosineThreshold float32
	Canonicalizer   *Canonicalizer
	Workers         int
	Log             func(format string, args ...any)
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
		// Empty corpus is a valid state — write an empty clusters.json
		// so downstream stages have a deterministic input.
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

	clusters := Bucket(members, func(i int) []float32 { return vectors[i] }, p.CosineThreshold)

	if p.Canonicalizer != nil {
		if err := p.Canonicalizer.Apply(ctx, clusters); err != nil {
			p.logf("canonicalize: %v", err)
		}
	}

	return SaveClusters(p.ClustersPath, ClustersFile{
		SchemaVersion:    SchemaVersion,
		EmbeddingModelID: p.EmbeddingModel,
		BuiltAt:          time.Now().UTC(),
		Clusters:         clusters,
	})
}

func (p *Pipeline) embedAll(ctx context.Context, members []ClusterMember) ([][]float32, error) {
	out := make([][]float32, len(members))

	// 1) cache pass
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

	// 2) batched fetch via the embedder; Voyage accepts list input,
	// so one call covers everything missing. Workers field is reserved
	// for the multi-batch case if a future provider caps batch size.
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

// loadAllObservations walks observationsDir for *.json files, decodes
// each as an extract.ObservationsFile, and flattens into a slice of
// ClusterMember with stable observation hashes.
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
			switch o.Kind {
			case "voice":
				subKey = o.Context
			case "topic":
				subKey = o.Topic
			}
			out = append(out, ClusterMember{
				ObservationHash: embedding.ObservationHash(o.Kind, subKey, o.Text),
				Source:          f.Source,
				Project:         f.Project,
				Kind:            o.Kind,
				Text:            o.Text,
				Evidence:        o.Evidence,
				Context:         o.Context,
				Topic:           o.Topic,
				Confidence:      o.Confidence,
			})
		}
	}
	return out, nil
}
```

- [ ] **Step 6: Run tests**

Run: `go test ./internal/cluster/ ./...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/cluster/io.go internal/cluster/io_test.go internal/cluster/pipeline.go internal/cluster/pipeline_test.go
git commit -m "feat(cluster): wire embedding + bucket + canonical into a pipeline"
```

---

## Task 7: Wire `compose --stages cluster`

**Files:**
- Modify: `cmd/compose.go`

- [ ] **Step 1: Read the current command**

Already in context: stage flag accepts only `extract`. Loosen the parser and dispatch.

- [ ] **Step 2: Replace the stage gate and add cluster path**

In `cmd/compose.go`, replace:

```go
		stages := strings.Split(composeStages, ",")
		if len(stages) != 1 || stages[0] != "extract" {
			return fmt.Errorf("phase 1 supports only --stages extract (got %q)", composeStages)
		}
		return runExtract(cmd.Context())
```

with:

```go
		stages, err := parseStages(composeStages)
		if err != nil {
			return err
		}
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
		return nil
```

And add helpers at the bottom of `compose.go`:

```go
// parseStages accepts comma-separated stage names or the literal "all".
// Order is enforced: extract → cluster → synthesize.
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

	emb, err := embedding.NewVoyageFromEnv()
	if err != nil {
		return err
	}
	cache, err := embedding.LoadCache(filepath.Join(stateDir, "embeddings.json"), cfg.Models.Embedding)
	if err != nil {
		return err
	}

	client, err := anthropic.New()
	if err != nil {
		return err
	}
	canon := &cluster.Canonicalizer{
		Client: client,
		Model:  cfg.Models.Cheap,
		Log:    log.Printf,
	}

	p := &cluster.Pipeline{
		Embedder:        emb,
		EmbeddingModel:  cfg.Models.Embedding,
		Cache:           cache,
		CacheSavePath:   filepath.Join(stateDir, "embeddings.json"),
		ClustersPath:    filepath.Join(stateDir, "clusters.json"),
		CosineThreshold: float32(cfg.Thresholds.ClusterCosineThreshold),
		Canonicalizer:   canon,
		Workers:         cfg.Batching.ExtractWorkers,
		Log:             log.Printf,
	}
	if err := p.Run(ctx, obsDir); err != nil {
		return err
	}

	// Record stage in ledger so status can report it.
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

// runSynthesize is implemented in Task 10. Stub for the dispatch:
func runSynthesize(ctx context.Context) error {
	return fmt.Errorf("synthesize: not implemented yet")
}
```

Add to the existing imports at the top of `cmd/compose.go`:

```go
	"github.com/SarahFrankle/ghost/internal/cluster"
	"github.com/SarahFrankle/ghost/internal/embedding"
```

- [ ] **Step 3: Build and run existing tests**

Run: `go build ./... && go test ./...`
Expected: PASS. (`runSynthesize` stub is fine; the synthesize stage isn't reachable until Task 11.)

- [ ] **Step 4: Manual smoke (optional, requires VOYAGE_API_KEY)**

```bash
go run . compose --stages cluster
```

Expected: writes `~/.ghost/.state/clusters.json` and `~/.ghost/.state/embeddings.json`. With Sarah's current single-transcript corpus this will produce a handful of single-member clusters.

- [ ] **Step 5: Commit**

```bash
git add cmd/compose.go
git commit -m "feat(compose): dispatch --stages cluster (extract + cluster + synthesize-stub)"
```

---

## Task 8: Identity synthesis (single file, smart model)

**Files:**
- Create: `prompts/synthesize.identity.system.md`
- Modify: `prompts/prompts.go`
- Create: `internal/synthesize/types.go`
- Create: `internal/synthesize/identity.go`

- [ ] **Step 1: Write the identity system prompt**

`prompts/synthesize.identity.system.md`:

```markdown
You are writing the "identity" section that Claude Code loads at the
start of every session about ONE specific user. Your input is a list
of identity-cluster observations extracted from that user's Claude
Code transcripts, each with an evidence count and a project count.

Write a third-person factual summary: role, team, primary languages
and stack, organizational context, headline expertise. Cap at 25 lines.

Hard rules:
- Third person only. Never address Claude. Never write in the user's
  voice.
- Cite nothing speculative. If an observation has evidence_count: 1,
  you may still mention it but prefer items supported across multiple
  transcripts.
- No em-dashes. No self-congratulation. Delete sentences you wouldn't
  miss.
- Markdown body only. No frontmatter, no preamble, no trailing
  meta-commentary like "this summary is based on …".

Begin output with a level-1 heading "# Identity" on the first line.
```

- [ ] **Step 2: Extend prompts.go**

```go
//go:embed synthesize.identity.system.md
var synthesizeIdentitySystem string

func SynthesizeIdentitySystem() string { return synthesizeIdentitySystem }
```

- [ ] **Step 3: Implement types and identity.go**

```go
// internal/synthesize/types.go
package synthesize

// FileResult is the per-output-file outcome of a synthesis run.
type FileResult struct {
	Name    string // e.g. "identity.md"
	Content string
	Err     error
}
```

```go
// internal/synthesize/identity.go
package synthesize

import (
	"context"
	"fmt"
	"strings"

	"github.com/SarahFrankle/ghost/internal/anthropic"
	"github.com/SarahFrankle/ghost/internal/cluster"
	"github.com/SarahFrankle/ghost/prompts"
)

// BuildIdentity calls the smart model to synthesize identity.md from
// the identity slice of clusters. Caller passes in only the relevant
// clusters; selection is not BuildIdentity's job.
func BuildIdentity(ctx context.Context, client anthropic.Client, model string, identityClusters []cluster.Cluster) FileResult {
	if len(identityClusters) == 0 {
		return FileResult{Name: "identity.md", Content: "# Identity\n\nNo identity observations yet.\n"}
	}
	payload := renderClusters(identityClusters)
	raw, err := client.Complete(ctx, model, prompts.SynthesizeIdentitySystem(), payload)
	if err != nil {
		return FileResult{Name: "identity.md", Err: fmt.Errorf("identity: %w", err)}
	}
	return FileResult{Name: "identity.md", Content: ensureTrailingNewline(strings.TrimSpace(raw))}
}

func renderClusters(cs []cluster.Cluster) string {
	var b strings.Builder
	for i, c := range cs {
		fmt.Fprintf(&b, "cluster %d (evidence=%d, projects=%d): %s\n", i+1, c.EvidenceCount, c.ProjectCount, c.Canonical)
		for j, m := range c.Members {
			fmt.Fprintf(&b, "  member %d (project=%s): %s [evidence: %s]\n", j+1, m.Project, m.Text, m.Evidence)
		}
	}
	return b.String()
}

func ensureTrailingNewline(s string) string {
	if !strings.HasSuffix(s, "\n") {
		return s + "\n"
	}
	return s
}
```

- [ ] **Step 4: Build**

Run: `go build ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add prompts/synthesize.identity.system.md prompts/prompts.go internal/synthesize/types.go internal/synthesize/identity.go
git commit -m "feat(synthesize): identity.md generator (single-file, smart model)"
```

---

## Task 9: Rules synthesis (filter + subtractive)

**Files:**
- Create: `prompts/synthesize.rules.system.md`
- Modify: `prompts/prompts.go`
- Create: `internal/synthesize/rules.go`
- Create: `internal/synthesize/rules_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/synthesize/rules_test.go
package synthesize

import (
	"context"
	"strings"
	"testing"

	"github.com/SarahFrankle/ghost/internal/cluster"
)

type fakeClient struct {
	gotUser string
	resp    string
}

func (f *fakeClient) Complete(ctx context.Context, model, system, user string) (string, error) {
	f.gotUser = user
	return f.resp, nil
}

func TestFilterRulesAppliesThresholds(t *testing.T) {
	clusters := []cluster.Cluster{
		{Kind: "rule", Canonical: "ok rule", EvidenceCount: 3, ProjectCount: 2},
		{Kind: "rule", Canonical: "too few projects", EvidenceCount: 5, ProjectCount: 1},
		{Kind: "rule", Canonical: "too little evidence", EvidenceCount: 1, ProjectCount: 1},
		{Kind: "identity", Canonical: "should be filtered out by kind", EvidenceCount: 9, ProjectCount: 9},
	}
	got := FilterRules(clusters, 2, 2)
	if len(got) != 1 || got[0].Canonical != "ok rule" {
		t.Fatalf("unexpected filtered set: %+v", got)
	}
}

func TestBuildRulesIncludesUserRulesInPrompt(t *testing.T) {
	f := &fakeClient{resp: "# Rules\n\n- foo\n"}
	clusters := []cluster.Cluster{
		{Kind: "rule", Canonical: "thing", EvidenceCount: 2, ProjectCount: 2,
			Members: []cluster.ClusterMember{{Text: "thing", Evidence: "turn 1", Project: "p"}}},
	}
	res := BuildRules(context.Background(), f, "smart", clusters, "rules.user.md\n- avoid em-dashes\n")
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	if !strings.Contains(f.gotUser, "avoid em-dashes") {
		t.Fatalf("user-rules not passed to model: %q", f.gotUser)
	}
}
```

- [ ] **Step 2: Verify failing**

Run: `go test ./internal/synthesize/`
Expected: undefined.

- [ ] **Step 3: Write the rules system prompt**

`prompts/synthesize.rules.system.md`:

```markdown
You are writing the "rules.md" file Claude Code loads on every
session. It tells Claude how this specific user wants Claude to
behave: defaults, prohibitions, preferences that recur across the
user's projects.

Your input is:
- A list of rule clusters that survived the
  evidence-count and project-count thresholds.
- The current `rules.user.md` contents, which are user-authored and
  AUTHORITATIVE. Your synthesized rules MUST NOT contradict
  rules.user.md. If a cluster would produce a rule that conflicts
  with anything in rules.user.md, OMIT it.

Hard rules:
- One bullet per rule. Imperative voice ("prefer X", "do not Y").
- No em-dashes. No hedging. No self-congratulation. Delete sentences
  you wouldn't miss.
- Do not invent rules absent from the cluster set. Do not paraphrase
  away the user's specificity.
- Markdown body only. Begin with a level-1 heading "# Rules" on the
  first line.
- If no clusters survive, emit "# Rules\n\nNo cross-project rules
  inferred yet." and nothing else.
```

- [ ] **Step 4: Extend prompts.go**

```go
//go:embed synthesize.rules.system.md
var synthesizeRulesSystem string

func SynthesizeRulesSystem() string { return synthesizeRulesSystem }
```

- [ ] **Step 5: Implement rules.go**

```go
// internal/synthesize/rules.go
package synthesize

import (
	"context"
	"fmt"
	"strings"

	"github.com/SarahFrankle/ghost/internal/anthropic"
	"github.com/SarahFrankle/ghost/internal/cluster"
	"github.com/SarahFrankle/ghost/prompts"
)

// FilterRules keeps only rule clusters whose evidence and project
// counts meet the configured minimums. The filter is intentionally in
// Go, never in the LLM, so the synthesis prompt cannot smuggle in a
// rule that fails the cross-project threshold.
func FilterRules(clusters []cluster.Cluster, minEvidence, minProjects int) []cluster.Cluster {
	out := make([]cluster.Cluster, 0, len(clusters))
	for _, c := range clusters {
		if c.Kind != "rule" {
			continue
		}
		if c.EvidenceCount < minEvidence || c.ProjectCount < minProjects {
			continue
		}
		out = append(out, c)
	}
	return out
}

func BuildRules(ctx context.Context, client anthropic.Client, model string, filtered []cluster.Cluster, userRules string) FileResult {
	if len(filtered) == 0 {
		return FileResult{Name: "rules.md", Content: "# Rules\n\nNo cross-project rules inferred yet.\n"}
	}
	var b strings.Builder
	b.WriteString("RULES.USER.MD (authoritative; do not contradict):\n")
	b.WriteString(strings.TrimSpace(userRules))
	b.WriteString("\n\nCANDIDATE CLUSTERS:\n")
	b.WriteString(renderClusters(filtered))

	raw, err := client.Complete(ctx, model, prompts.SynthesizeRulesSystem(), b.String())
	if err != nil {
		return FileResult{Name: "rules.md", Err: fmt.Errorf("rules: %w", err)}
	}
	return FileResult{Name: "rules.md", Content: ensureTrailingNewline(strings.TrimSpace(raw))}
}
```

- [ ] **Step 6: Run tests**

Run: `go test ./internal/synthesize/`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add prompts/synthesize.rules.system.md prompts/prompts.go internal/synthesize/rules.go internal/synthesize/rules_test.go
git commit -m "feat(synthesize): rules.md generator with Go-side filter + subtractive prompt"
```

---

## Task 10: Synthesize pipeline (tmpdir + atomic rename + partial failure)

**Files:**
- Create: `internal/synthesize/pipeline.go`
- Create: `internal/synthesize/pipeline_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/synthesize/pipeline_test.go
package synthesize

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SarahFrankle/ghost/internal/cluster"
)

type scriptedClient struct {
	bySystem map[string]string // system-prompt-substring → response
	err      map[string]error  // system-prompt-substring → error
}

func (s *scriptedClient) Complete(ctx context.Context, model, system, user string) (string, error) {
	for key, e := range s.err {
		if strings.Contains(system, key) {
			return "", e
		}
	}
	for key, resp := range s.bySystem {
		if strings.Contains(system, key) {
			return resp, nil
		}
	}
	return "", nil
}

func TestPipelineWritesBothFilesAtomically(t *testing.T) {
	ghostDir := t.TempDir()
	// Pre-populate rules.user.md
	if err := os.WriteFile(filepath.Join(ghostDir, "rules.user.md"), []byte("- avoid em-dashes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cf := cluster.ClustersFile{
		Clusters: []cluster.Cluster{
			{Kind: "identity", Canonical: "works at Miro", EvidenceCount: 3, ProjectCount: 2},
			{Kind: "rule", Canonical: "prefer integration tests", EvidenceCount: 3, ProjectCount: 2,
				Members: []cluster.ClusterMember{{Text: "x", Evidence: "t1", Project: "p"}}},
		},
	}
	client := &scriptedClient{bySystem: map[string]string{
		"# Identity": "# Identity\n\nworks at Miro.\n",
		"# Rules":    "# Rules\n\n- prefer integration tests\n",
	}}
	p := &Pipeline{
		Client:           client,
		SmartModel:       "smart",
		GhostDir:         ghostDir,
		MinRuleEvidence:  2,
		MinRuleProjects:  2,
	}
	if err := p.Run(context.Background(), cf); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"identity.md", "rules.md"} {
		b, err := os.ReadFile(filepath.Join(ghostDir, name))
		if err != nil {
			t.Fatalf("%s missing: %v", name, err)
		}
		if !strings.HasPrefix(string(b), "# ") {
			t.Fatalf("%s missing heading: %q", name, string(b))
		}
	}
}

func TestPipelinePreservesPriorGenerationOnPartialFailure(t *testing.T) {
	ghostDir := t.TempDir()
	// Seed with a prior generation.
	priorIdentity := "# Identity\n\nprior.\n"
	priorRules := "# Rules\n\n- prior\n"
	_ = os.WriteFile(filepath.Join(ghostDir, "identity.md"), []byte(priorIdentity), 0o644)
	_ = os.WriteFile(filepath.Join(ghostDir, "rules.md"), []byte(priorRules), 0o644)

	cf := cluster.ClustersFile{Clusters: []cluster.Cluster{
		{Kind: "identity", Canonical: "x", EvidenceCount: 1, ProjectCount: 1},
		{Kind: "rule", Canonical: "y", EvidenceCount: 3, ProjectCount: 2,
			Members: []cluster.ClusterMember{{Text: "y", Evidence: "t", Project: "p"}}},
	}}

	client := &scriptedClient{
		bySystem: map[string]string{"# Identity": "# Identity\n\nnew\n"},
		err:      map[string]error{"# Rules": errFakeRulesFail},
	}
	p := &Pipeline{Client: client, SmartModel: "x", GhostDir: ghostDir,
		MinRuleEvidence: 2, MinRuleProjects: 2}

	if err := p.Run(context.Background(), cf); err == nil {
		t.Fatal("expected error on partial failure")
	}
	got, _ := os.ReadFile(filepath.Join(ghostDir, "identity.md"))
	if string(got) != priorIdentity {
		t.Fatalf("prior identity.md was overwritten despite partial failure: %q", string(got))
	}
	got, _ = os.ReadFile(filepath.Join(ghostDir, "rules.md"))
	if string(got) != priorRules {
		t.Fatalf("prior rules.md was overwritten: %q", string(got))
	}
	// tmpdir is preserved
	entries, _ := os.ReadDir(ghostDir)
	foundTmp := false
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), ".tmp-synthesize-") {
			foundTmp = true
		}
	}
	if !foundTmp {
		t.Fatal("expected preserved tmpdir on partial failure")
	}
}

var errFakeRulesFail = &stringErr{"forced rules failure"}

type stringErr struct{ s string }

func (e *stringErr) Error() string { return e.s }
```

- [ ] **Step 2: Verify failing**

Run: `go test ./internal/synthesize/`
Expected: undefined `Pipeline`.

- [ ] **Step 3: Implement pipeline.go**

```go
// internal/synthesize/pipeline.go
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

// Pipeline orchestrates stage 3 for the always-loaded core
// (identity.md, rules.md). Topics/voice/index are out of scope in
// Phase 2.
//
// Write strategy:
//  1. Create ~/.ghost/.tmp-synthesize-<ts>/.
//  2. Call each generator into the tmpdir.
//  3. If ANY generator returned an error, leave the tmpdir in place
//     (for inspection) and return a partial-failure error. The prior
//     generation's files in ~/.ghost/ remain authoritative.
//  4. If all generators succeeded, rename each file from the tmpdir
//     into ~/.ghost/ and remove the tmpdir.
//
// POSIX has no atomic multi-file dir-merge, so step 4 renames file-by-
// file. The order is identity.md first then rules.md so a crash mid-
// step leaves identity stale-but-consistent rather than rules stale-
// but-consistent. (rules.md is what changes behavior — better to have
// the old version one cycle longer than to have rules referencing an
// identity the model didn't see.)
type Pipeline struct {
	Client          anthropic.Client
	SmartModel      string
	GhostDir        string
	MinRuleEvidence int
	MinRuleProjects int
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

	userRules := readUserRules(p.GhostDir)

	results := []FileResult{
		BuildIdentity(ctx, p.Client, p.SmartModel, identityClusters),
		BuildRules(ctx, p.Client, p.SmartModel, ruleClusters, userRules),
	}

	// Phase A: write every successful generation into tmpdir.
	var failed []string
	for _, r := range results {
		if r.Err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", r.Name, r.Err))
			continue
		}
		if err := os.WriteFile(filepath.Join(tmpDir, r.Name), []byte(r.Content), 0o644); err != nil {
			failed = append(failed, fmt.Sprintf("%s: write: %v", r.Name, err))
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("synthesize partial failure (tmpdir preserved at %s): %s", tmpDir, strings.Join(failed, "; "))
	}

	// Phase B: atomic-per-file rename into GhostDir.
	for _, r := range results {
		src := filepath.Join(tmpDir, r.Name)
		dst := filepath.Join(p.GhostDir, r.Name)
		if err := os.Rename(src, dst); err != nil {
			return fmt.Errorf("rename %s: %w (tmpdir preserved at %s)", r.Name, err, tmpDir)
		}
	}
	_ = os.Remove(tmpDir) // empty by now
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

- [ ] **Step 4: Run tests**

Run: `go test ./internal/synthesize/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/synthesize/pipeline.go internal/synthesize/pipeline_test.go
git commit -m "feat(synthesize): tmpdir pipeline with per-file rename and partial-failure preservation"
```

---

## Task 11: Wire `compose --stages synthesize` and `--stages all`

**Files:**
- Modify: `cmd/compose.go`

- [ ] **Step 1: Replace the stub**

Replace the stub `runSynthesize` in `cmd/compose.go` with:

```go
func runSynthesize(ctx context.Context) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	outDir, err := paths.Expand(cfg.Paths.OutputDir)
	if err != nil {
		return err
	}
	stateDir := filepath.Join(outDir, ".state")
	clustersPath := filepath.Join(stateDir, "clusters.json")

	cf, err := cluster.LoadClusters(clustersPath)
	if err != nil {
		return fmt.Errorf("load clusters.json (run `ghost compose --stages cluster` first): %w", err)
	}

	client, err := anthropic.New()
	if err != nil {
		return err
	}
	p := &synthesize.Pipeline{
		Client:          client,
		SmartModel:      cfg.Models.Smart,
		GhostDir:        outDir,
		MinRuleEvidence: cfg.Thresholds.RuleMinEvidenceCount,
		MinRuleProjects: cfg.Thresholds.RuleMinProjectCount,
	}
	if err := p.Run(ctx, cf); err != nil {
		return err
	}

	// Update ledger so status reports the latest stage.
	l, err := ledger.Load(filepath.Join(stateDir, "ledger.json"))
	if err != nil {
		return err
	}
	l.SetLastCompose([]string{"synthesize"}, "")
	if err := l.Save(filepath.Join(stateDir, "ledger.json")); err != nil {
		return err
	}
	fmt.Println("synthesize: wrote identity.md, rules.md")
	return nil
}
```

Add import:

```go
	"github.com/SarahFrankle/ghost/internal/synthesize"
```

- [ ] **Step 2: Build and test**

Run: `go build ./... && go test ./...`
Expected: PASS.

- [ ] **Step 3: End-to-end smoke**

With `VOYAGE_API_KEY` set, against the existing single-transcript corpus:

```bash
go run . compose --stages cluster
go run . compose --stages synthesize
ls -la ~/.ghost/
cat ~/.ghost/identity.md
cat ~/.ghost/rules.md
```

Expected: both files exist. `rules.md` likely contains the "No cross-project rules inferred yet." placeholder until a second project's transcripts are extracted. `identity.md` should be a short third-person summary.

Then test `--stages all` (will re-extract anything new, re-cluster, re-synthesize):

```bash
go run . compose --stages all
```

Expected: all three stages run in order, output files refreshed.

- [ ] **Step 4: Commit**

```bash
git add cmd/compose.go
git commit -m "feat(compose): wire --stages synthesize and --stages all end-to-end"
```

---

## Task 12: Status command surfaces cluster + synthesis state, verify CLAUDE.md wiring

**Files:**
- Modify: `cmd/status.go`
- Verify (read-only): `~/.claude/CLAUDE.md`

- [ ] **Step 1: Read current status output**

Inspect `cmd/status.go` to see the existing format. It already prints transcript / ledger summary. Extend with cluster + synthesis state.

- [ ] **Step 2: Add cluster + synthesis lines**

After the existing "last_compose" output, append:

```go
// clusters
clustersPath := filepath.Join(stateDir, "clusters.json")
if info, err := os.Stat(clustersPath); err == nil {
	cf, err := cluster.LoadClusters(clustersPath)
	if err == nil {
		fmt.Printf("clusters: %d (built %s, embedding=%s)\n", len(cf.Clusters), info.ModTime().Format(time.RFC3339), cf.EmbeddingModelID)
	} else {
		fmt.Printf("clusters: present but unreadable: %v\n", err)
	}
} else {
	fmt.Println("clusters: none (run: ghost compose --stages cluster)")
}

// synthesis outputs
for _, name := range []string{"identity.md", "rules.md"} {
	p := filepath.Join(outDir, name)
	if info, err := os.Stat(p); err == nil {
		fmt.Printf("%s: present (%d bytes, %s)\n", name, info.Size(), info.ModTime().Format(time.RFC3339))
	} else {
		fmt.Printf("%s: missing (run: ghost compose --stages synthesize)\n", name)
	}
}
```

Add imports as needed (`cluster`, `time`).

- [ ] **Step 3: Build and test**

```bash
go build ./...
go run . status
```

Expected: status output now includes cluster count and identity/rules presence.

- [ ] **Step 4: Verify CLAUDE.md includes**

Confirm `~/.claude/CLAUDE.md` already contains both lines (per handoff note they were added manually):

```
@~/.ghost/identity.md
@~/.ghost/rules.md
```

If missing, append them. Do NOT remove any other includes.

- [ ] **Step 5: Commit**

```bash
git add cmd/status.go
git commit -m "feat(status): report cluster and synthesis state"
```

---

## Task 13: Update spec status; write Phase 2 handoff

**Files:**
- Modify: `docs/specs/2026-05-20-ghost-design.md` (status field if present)
- Create: `docs/superpowers/handoffs/2026-MM-DD-phase-2-complete.md`

- [ ] **Step 1: Confirm Phase 2 exit criteria**

Re-read spec lines 703–705. Exit criterion is qualitative: "two weeks of normal Claude Code use." Implementation is done when end-to-end works against the real corpus, not when an arbitrary number of clusters appears.

- [ ] **Step 2: Write the handoff**

Use the same shape as `docs/superpowers/handoffs/2026-05-21-phase-1-complete.md`. Cover:
- What Phase 2 ships (the two stages + two synthesized files + status updates).
- Any review items deferred to Phase 3.
- Smoke-test confirmation on real corpus.
- Phase 3 scope reminder (topics, index, voice synthesis gating, skill, lazy loading).
- Anything not in git.

- [ ] **Step 3: Commit + push if appropriate**

```bash
git add docs/superpowers/handoffs/2026-MM-DD-phase-2-complete.md docs/specs/2026-05-20-ghost-design.md
git commit -m "docs: phase 2 handoff"
```

Do not auto-push or open a PR — the human decides whether to merge `phase-1-extract` first or keep accumulating.

---

## Notes for the implementing engineer

- **Voyage API key.** The cluster stage requires `VOYAGE_API_KEY` in the environment. The Phase 1 stage requires the `claude` CLI in PATH (already established). If the user does not have a Voyage key yet, pause at Task 7 and surface a clear error rather than fabricating an in-process embedding implementation.
- **Re-use, don't re-invent.** `internal/anthropic.Client` already encapsulates the right `claude -p` isolation flags. Both new LLM call sites (canonical phrasing, synthesis) must go through that interface.
- **Threshold knobs.** `cluster_cosine_threshold` defaults to 0.85. Real-corpus reads will likely require tuning. Do not hardcode it elsewhere — only `cmd/compose.go` should pull it from config.
- **Embedding model coupling.** If `[models].embedding` changes in `config.toml`, the next `compose --stages cluster` run will discard the on-disk cache and re-embed. This is intentional. Do not add a fast-path that reuses old vectors.
- **Stage 2 is corpus-level.** It loads every observation file every time. There is no incremental cluster mode in Phase 2 — `clusters.json` is rebuilt in full. The embedding cache makes this cheap.
- **Phase 2 does not touch `topics/`, `voice/`, or `index.md`.** Observations of those kinds are clustered (they pass through stage 2 unchanged) but no synthesis is produced. Phase 3 will add those files.
- **`/ghost` skill is still Phase 3.** The two CLAUDE.md includes are enough for Phase 2's exit criterion.
