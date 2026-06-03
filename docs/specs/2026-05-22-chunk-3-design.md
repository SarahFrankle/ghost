# Chunk 3 design: embedding-based topics

> **Revised 2026-05-22.** Original design used a separate cheap-local
> Ollama call to name each cluster, then a smart-model call for the
> body (Decision 1 option B). Revised to a single smart-model call per
> cluster that emits a body beginning with `# <Title>`; the caller
> slugifies the title to produce the filename (Decision 1 option C).
> See decisions doc for rationale. The revision deletes the Ollama
> naming client, the naming prompt, and the naming-backend config
> path. Atomicity contract is also clarified (strict; partial topic
> failure fails the whole topic rebuild) and a slug sanitiser contract
> is now spelled out.

Buildable design for chunk 3 of the ghost rewrite plan. Synthesises
the architectural spec (`2026-05-22-topics-by-embedding.md`) and the
three load-bearing decisions (`2026-05-22-chunk-3-decisions.md`) into
a single source of truth for the implementation plan.

**Goal:** stop using the slug as the bucketing key for topic
observations. Bucket topic observations by content-embedding cosine
similarity. Name each resulting cluster at synthesize time via a
single smart-model call that writes the body with a leading `# <Title>`;
the caller slugifies the title to derive the filename.

**Non-goals (deferred):** flat-CLI (chunk 4), README pass (chunk 5),
local-Ollama offload for any synthesis call, MCP server, `ghost eval`,
conversation compression.

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

- `internal/synthesize/slugify.go` — pure function that turns a
  parsed H1 string into a filename slug. Deterministic, no LLM.
  Contract spelled out in the "Slug derivation" section below.
- `internal/synthesize/slugify_test.go` — table-driven tests for the
  slugifier, including reject cases.

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
  `Thresholds.ClusterCosineTopic` (default `0.75`). No new model
  fields — the existing smart-model client handles topic synthesis
  end-to-end.
- `internal/synthesize/topics.go` — rewrite. Input changes from
  `map[string][]cluster.Cluster` (slug-keyed) to `[]cluster.Cluster`
  (each cluster is one topic). Per cluster: smart-anthropic call →
  body (markdown beginning with `# <Title>`). Caller parses the H1,
  slugifies it via `slugify.Slug`, and emits
  `FileResult{Name: "topics/<slug>.md", ...}`. Two-pass within the
  topic stage: pass 1 generates all bodies and parses H1s; pass 2
  detects slug collisions across the run and fails synthesis if any
  occur (see "Slug collision handling"). Body calls in pass 1 run in
  parallel; collision check is sequential and cheap.
- `internal/synthesize/topics_test.go` — fixture + assertion updates.
- `internal/synthesize/pipeline.go` — `BuildTopics` no longer takes
  a slug-keyed input. Reorder the run so `BuildIndex` executes
  *after* topic synthesis, taking the produced `[]TopicResult`
  (cluster + slug + body + evidence count) as input. Ranking by
  evidence count still happens, just on the post-synthesis results.
- `internal/synthesize/index.go` — `BuildIndex` signature changes
  from `map[string][]cluster.Cluster` (keyed by SubKey) to
  `[]TopicResult`. Same prompt, different input shape.
- `cmd/compose.go` — remove `KnownTopics` plumbing. Remove
  `canonicalize` from the stage list. No new client wiring (the
  existing smart-anthropic client is reused).
- `prompts/synthesize.topics.system.md` — rewrite. Drop the "given a
  slug" framing entirely. The prompt receives a cluster of
  observations and must emit a markdown body whose first line is
  `# <Title>` — a clean noun-phrase title naming the concept the
  cluster represents. Title rules: title-case, no quoting, no
  abbreviations, ≤8 words.

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
  topic pass 1 (parallel per cluster):
    cluster → smart-anthropic → body starting with "# <Title>"
    parse H1; slug = slugify(Title)
  topic pass 2 (sequential, cheap):
    if any two slugs collide:
      FAIL with both clusters' canonicals
    else:
      emit topics/<slug>.md for each
  ranked topic results (cluster + slug + body) → smart-anthropic → index.md
  // BuildIndex runs AFTER topic synthesis so it can use the slugs
  // derived from each cluster's body. The previous chunk-2 contract
  // (slugs known up front from SubKey) no longer holds.
```

## Slug derivation

`internal/synthesize/slugify.Slug(title string) (string, error)`.
Deterministic, no LLM. Contract:

1. Trim leading/trailing whitespace.
2. Lowercase.
3. Replace any run of non-`[a-z0-9]` characters with a single `-`.
4. Trim leading/trailing `-`.
5. Reject (return error) if the result is empty, longer than 40
   characters, or contains no alphabetic character.

Reject failures fail the *topic* (not all of synthesis): the cluster
is recorded with an error; the topics rebuild as a whole fails per
the atomicity rule below. The user sees which cluster produced the
bad title and can iterate on the prompt.

The body prompt's title rules (title-case, ≤8 words, no quoting, no
abbreviations) keep the slugifier's reject path rare.

## Slug collision handling

Two clusters whose H1s slugify to the same string is a *signal*, not
a recovery case. The clustering threshold for topics is wrong: two
clusters semantically distinct enough to be separate produced
identical names. Synthesis fails with an error containing both
clusters' canonicals so the user can tune the topic cosine threshold
and re-run.

Do not auto-suffix (`-2`), do not retry with "give a different name."
The signal is the value.

Detection happens in pass 2 (sequential, cheap) after all body calls
in pass 1 complete in parallel. This means a collision is discovered
only after paying for every cluster's body call — but since the body
call is required regardless to detect the duplicate H1, this is the
minimum work needed and incurs no extra LLM cost over an
optimistically-succeeding run.

## Atomicity and fingerprints

Pipeline atomicity is strict: all topic files write to a tmpdir; the
tmpdir's `topics/` only replaces `~/.ghost/topics/` if *every*
cluster succeeded (body produced, H1 parsed, slug derived, no
collision with any other cluster in the run). Partial failure —
including a single LLM error, a single slugifier reject, or any slug
collision — fails the entire topics rebuild and leaves prior
`~/.ghost/topics/` intact.

This trades partial-progress recoverability for an always-consistent
on-disk state. Topic files are co-referenced by `index.md`; a partial
topics directory paired with a stale index would be worse than no
update at all.

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
  bodies; (b) two clusters whose H1s slugify to the same string
  surface a collision error containing both canonicals and fail the
  topics rebuild; (c) an unparseable body (no `# Title` first line)
  or a slugifier reject fails the topics rebuild with a clear error.
- **`internal/synthesize/slugify_test.go`** — table-driven: happy
  paths (spaces → hyphens, mixed case → lowercase, punctuation
  stripped), edge cases (leading/trailing whitespace, runs of
  punctuation collapsed), reject cases (empty input, all-numeric,
  >40 chars).
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
- **The body prompt must reliably emit a `# Title` first line.** If
  the smart model drifts (no H1, or H1 on line 2 after a preamble),
  the topic fails the slugifier reject path and the whole rebuild
  fails. Mitigation: explicit format instruction in the prompt; the
  smart model has high reliability for this shape. If drift becomes
  a real failure mode in practice, fall back to a tolerant parser
  (first H1 anywhere in the body) before adding a structured
  envelope.
- **First compose after chunk 3 is a full corpus rebuild.** Cost is
  bounded (extract on cheap-Anthropic, synthesize on smart-Anthropic;
  estimable via `ghost compose --estimate` before running).
