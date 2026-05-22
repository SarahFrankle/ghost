# Chunk 3 design: embedding-based topics

Buildable design for chunk 3 of the ghost rewrite plan. Synthesises
the architectural spec (`2026-05-22-topics-by-embedding.md`) and the
three load-bearing decisions (`2026-05-22-chunk-3-decisions.md`) into
a single source of truth for the implementation plan.

**Goal:** stop using the slug as the bucketing key for topic
observations. Bucket topic observations by content-embedding cosine
similarity. Name each resulting cluster at synthesize time via a small
local-Ollama call, then write the body via the existing smart-model
call.

**Non-goals (deferred):** flat-CLI (chunk 4), README pass (chunk 5),
local-Ollama for any other call besides slug-naming, MCP server,
`ghost eval`, conversation compression.

## Scope: what gets deleted

- `internal/canonicalize/` — package, prompts, tests, all of it.
- `prompts/canonicalize.slug.system.md`.
- `KnownTopics` plumbing in `cmd/compose.go`.
- The `canonicalize` stage from the `--stages` known-stages list.
- `KNOWN TOPICS` section and slug-shape rules in
  `prompts/extract.system.md`.
- `Topic` field on `internal/extract/Observation` and its validation.
- `TopicAliases` field on `cluster.Pipeline` and the
  resolve-at-read shim.
- `.state/canonical_cache.json` and `.state/slug_aliases.json`
  (config + code references; the files themselves get nuked as part of
  the corpus rebuild).

Estimated net deletion: 600–800 lines.

## Scope: what gets added or changed

### New files

- `internal/ollama/llm.go` — Ollama HTTP `/api/generate` client.
  Satisfies the existing `anthropic.Client.Complete(ctx, model,
  system, user) (string, error)` interface. (Interface rename to
  `llm.Client` is out of scope — separate hygiene chunk.)
- `internal/ollama/llm_test.go` — uses `httptest.Server` to mock the
  Ollama HTTP endpoint.
- `prompts/synthesize.topic-slug.system.md` — naming prompt. Input:
  cluster canonicals + sample members. Output: one kebab-case slug,
  1–4 words, no abbreviations, no quoting.

### Changed files

- `internal/extract/schema.go` — drop `Topic` field from
  `Observation`. Drop the corresponding validation rule.
- `prompts/extract.system.md` — strip the `KNOWN TOPICS` section and
  the slug-shape rules. Topic-kind observations carry `kind`, `text`,
  `evidence` only.
- `internal/cluster/bucket.go` — drop the SubKey special case for
  `kind: topic`. All three kinds bucket the same way: pool all
  observations of that kind, cluster by cosine. Threshold lookup is
  per-kind.
- `internal/cluster/pipeline.go` — remove `TopicAliases` field,
  remove the resolve-at-read shim. Read both cosine thresholds from
  config.
- `internal/config/config.go` — replace
  `Thresholds.ClusterCosineThreshold` (single field) with
  `Thresholds.ClusterCosineIdentityRule` (default `0.85`) and
  `Thresholds.ClusterCosineTopic` (default `0.75`). Add
  `Models.NamingLocal` (default `"qwen2.5:3b"`) and
  `Models.NamingBackend` (default `"ollama"`; accepts `"anthropic"`
  as escape hatch).
- `internal/synthesize/topics.go` — rewrite. Input changes from
  `map[string][]cluster.Cluster` (slug-keyed) to `[]cluster.Cluster`
  (each cluster is one topic). Per cluster: naming client → slug,
  then smart-anthropic client + slug → body. Emit
  `FileResult{Name: "topics/<slug>.md", ...}`.
- `internal/synthesize/topics_test.go` — fixture + assertion updates.
- `internal/synthesize/pipeline.go` — wire the naming client through
  to `BuildTopics`. Pipeline now takes two LLM clients (smart, naming)
  instead of one. Reorder the run so `BuildIndex` executes *after*
  topic synthesis, taking the produced `(cluster, slug)` pairs as
  input instead of a pre-keyed map. Ranking by evidence count still
  happens, just on the post-naming results.
- `internal/synthesize/index.go` — `BuildIndex` signature changes
  from `map[string][]cluster.Cluster` (keyed by SubKey) to
  `[]TopicResult` (cluster + slug + evidence count). Same prompt,
  different input shape.
- `cmd/compose.go` — construct the naming client at startup based on
  `Models.NamingBackend`. Remove `KnownTopics` plumbing. Remove
  `canonicalize` from the stage list. Pass both clients to the
  cluster and synthesize pipelines.
- `prompts/synthesize.topics.system.md` — drop the "given a slug"
  framing. Receive the slug from the caller and write the body under
  a `# <Title>` derived from the slug.
- `prompts/prompts.go` — add accessor for the new naming prompt.

### Data flow after chunk 3

```
extract:
  per transcript → cheap-anthropic
  observation = { kind, text, evidence }      // no Topic field

cluster (per kind, all in one pool):
  identity obs → cosine 0.85 → identity clusters
  rule obs     → cosine 0.85 → rule clusters
  topic obs    → cosine 0.75 → topic clusters // no SubKey, no canonicalize

synthesize:
  identity clusters → smart-anthropic → identity.md   (unchanged)
  rule clusters     → smart-anthropic → rules.md      (unchanged)
  for each topic cluster:
    cluster → local-ollama (naming) → "<slug>"
    if slug collides with prior slug in this run:
      FAIL with both clusters' canonicals
    cluster + slug → smart-anthropic (body) → topics/<slug>.md
  ranked topic results (cluster + slug + body) → smart-anthropic → index.md
  // BuildIndex now runs AFTER topic synthesis so it can use the slugs
  // that the naming calls produced; the previous chunk-2 contract
  // (slugs known up front from SubKey) no longer holds.
```

## Slug collision handling

Two clusters returning the same slug is a *signal*, not a recovery
case. The clustering threshold for topics is wrong: two clusters
semantically distinct enough to be separate produced identical names.
Synthesis fails with an error containing both clusters' canonicals so
the user can either tune the topic cosine threshold or, in rare cases,
manually merge two genuinely-equivalent clusters before re-running.

Do not auto-suffix (`-2`), do not retry with "give a different name."
The signal is the value.

## Local Ollama integration

The naming client implements the same `Complete(ctx, model, system,
user) (string, error)` interface as the Anthropic client. The
synthesize pipeline takes it as a separate field
(`NamingClient`); call sites that name pass it explicitly. Selection
at startup:

```go
switch cfg.Models.NamingBackend {
case "ollama":
    namingClient = ollama.New(cfg.Models.NamingLocal)
case "anthropic":
    namingClient = anthropicClient // reuse the existing cheap-model client
default:
    return fmt.Errorf("unknown naming backend: %q", cfg.Models.NamingBackend)
}
```

Ollama failure modes (connection refused, model not pulled) surface
as errors from `Complete`. Each failure fails one topic; the rest of
synthesis continues. No silent fallback to Anthropic — fallback would
hide configuration mistakes that should be visible.

## Atomicity and fingerprints

Pipeline atomicity is unchanged from chunk 2: all topic files write
to a tmpdir; the tmpdir's `topics/` only replaces `~/.ghost/topics/`
if every cluster succeeded. Partial failure leaves prior topics
intact.

Fingerprint impact on first compose after chunk 3:

- Extract fingerprint changes (prompt edited) → all observations
  re-extracted.
- Cluster fingerprint changes (bucketing code + threshold split) →
  clusters recomputed.
- Synthesize topic fingerprint changes (new prompts, new code path,
  new naming dependency) → topic files regenerated.
- Identity and rules fingerprints unchanged in structure, but their
  inputs (observations) re-fingerprint transitively, so those files
  also rebuild.

Full corpus rebuild is the cost of chunk 3 by design and is paid
once.

## Testing strategy

- **`internal/cluster/bucket_test.go`** — extend to cover the new
  per-kind threshold lookup. Identity/rule path stays at 0.85; topic
  path uses 0.75. Same fixture, different expected groupings.
- **`internal/synthesize/topics_test.go`** — three new cases:
  (a) clusters with distinct semantics produce distinct slugs and
  bodies; (b) two clusters that produce the same slug surface a
  collision error containing both canonicals; (c) naming-client
  failure on one cluster fails only that topic.
- **`internal/ollama/llm_test.go`** — happy path, error from server,
  empty response, malformed slug (whitespace/quotes — sanitised by
  the caller).
- **End-to-end smoke** — run `ghost compose` on the real corpus.
  Verify: no `internal/canonicalize/` references remain; no
  `.state/slug_aliases.json` recreated; `~/.ghost/topics/` populated
  with files whose names match their `# <Title>` lines; index.md
  references those slugs.

## Migration

State files to remove before first chunk-3 compose:

```bash
rm -rf ~/.ghost/.state/clusters.json
rm -rf ~/.ghost/.state/canonical_cache.json
rm -rf ~/.ghost/.state/slug_aliases.json
rm -rf ~/.ghost/topics/
```

Observations and embeddings stay valid until extract re-runs (their
content hash is unchanged); the new compose run will regenerate them
when extract's fingerprint mismatches anyway. Removing them up front
is optional but makes the rebuild explicit.

Config migration: `~/.ghost/config.toml` users with a custom
`cluster_cosine_threshold` need to rename it to
`cluster_cosine_identity_rule` and add `cluster_cosine_topic = 0.75`.
For Sarah's setup (defaults), no edit needed.

## Verification

Chunk 3 is complete when:

1. `go test ./...` passes.
2. `internal/canonicalize/` no longer exists.
3. `grep -r KNOWN_TOPICS ghost/` returns no matches.
4. `grep -r '"Topic"' internal/extract/` returns no matches.
5. `ghost compose` on the real corpus produces a populated
   `~/.ghost/topics/` directory with no `.state/slug_aliases.json`
   recreated.
6. Re-running `ghost compose` immediately afterwards produces the
   same slug set (stability under re-run on unchanged corpus).
7. Two obvious-synonym observations from prior runs (e.g., one citing
   "docs" and one citing "documentation") land in the same topic file.

## Open risks

- **Topic cosine threshold (0.75) is a guess.** It may produce
  fragmented clusters (too tight) or over-merged clusters (too loose)
  on Sarah's actual corpus. First chunk-3 compose is the calibration
  run; expect to tune the default before chunk 4. Mitigation: the
  threshold is config, not code.
- **Local naming-model choice (`qwen2.5:3b`) is also a guess.** May
  produce noisy slugs (paraphrases, abbreviations) or refuse the task
  format. Mitigation: `NamingBackend = "anthropic"` is the documented
  escape hatch; switching is a one-line config edit.
- **First compose after chunk 3 is a full corpus rebuild.** Cost is
  bounded (extract on cheap-Anthropic, synthesize on smart-Anthropic;
  estimable via `ghost compose --estimate` before running).
