# Phase 2 handoff — cluster + synthesize complete

**Date:** 2026-05-21
**Branch:** `phase-1-extract` (12 commits ahead of `cb24d7b`, the Phase 1
final commit; 23 commits ahead of `main`)
**Status:** Phase 2 implementation complete; all unit tests green
(36 tests across 14 packages); end-to-end smoke against the real
corpus has NOT been run yet (requires `VOYAGE_API_KEY`).

## What Phase 2 ships

Two new compose stages and two always-loaded synthesized files:

- `ghost compose --stages cluster` — loads every
  `.state/observations/*.json`, embeds each observation via Voyage
  (cached by `(kind|sub_key|text)` hash in `.state/embeddings.json`),
  partitions by `(kind, sub_key)`, performs single-linkage
  agglomerative bucketing at `cluster_cosine_threshold`, picks a
  canonical phrasing per multi-member bucket via the cheap model, and
  writes `.state/clusters.json` with Go-computed `EvidenceCount` /
  `ProjectCount`.
- `ghost compose --stages synthesize` — reads `clusters.json`, calls
  the smart model once per output file (`identity.md`, `rules.md`)
  into a sibling tmpdir, and renames each file into `~/.ghost/` only
  if both generations succeeded. Rules are filtered by evidence ≥ N
  and projects ≥ N **in Go** before the LLM call; the rules prompt
  also receives the current `rules.user.md` for subtractive
  synthesis. Partial failure preserves the prior generation and
  leaves the tmpdir on disk for inspection.
- `ghost compose --stages all` — runs `extract → cluster →
  synthesize` in order. `parseStages` accepts arbitrary comma-
  separated stage subsets and enforces canonical order.
- `ghost status` now reports cluster count, embedding model id, and
  presence of `identity.md` / `rules.md`.

CLAUDE.md verification: `~/.claude/CLAUDE.md` already loads
`@~/.ghost/identity.md` and `@~/.ghost/rules.md` (lines 3–4) from the
Phase 1 handoff. No edit needed.

## How the new pieces are wired

- **Voyage HTTP client** (`internal/embedding/voyage.go`) implements
  the `Embedder` interface and is the only network dependency added
  in Phase 2. Constructed via `NewVoyageFromEnv()`; reads
  `VOYAGE_API_KEY`. Tests inject a `httptest.Server` directly via the
  exported `BaseURL` / `HTTPClient` fields — no real network in CI.
- **Embedding cache** (`internal/embedding/cache.go`) gates by
  `embedding_model_id`. A mismatched model id discards every cached
  vector on load — cosine distributions are not portable across
  models. Saves through `atomicfs.WriteFile`.
- **Cluster pipeline** (`internal/cluster/pipeline.go`) is corpus-
  level: it loads every observation file every run. The embedding
  cache makes this cheap; partial re-runs are not supported in
  Phase 2 and are not needed.
- **Canonical phrasing** (`internal/cluster/canonical.go`) skips
  singleton clusters (no LLM call) and is non-fatal on failure: a
  cluster keeps its seed text when the cheap model errors or returns
  unparseable JSON. The balanced-brace JSON extractor is local to
  cluster — duplicating the small parser from `internal/extract`
  keeps the two packages independent. If both diverge in interesting
  ways later, extract a shared util.
- **Synthesis pipeline** (`internal/synthesize/pipeline.go`) writes
  identity.md first, then rules.md, so a crash mid-rename leaves the
  prior rules.md authoritative (the file that actually changes
  behavior). POSIX has no atomic multi-file dir-merge, so atomicity
  is per-file; the all-or-nothing guarantee comes from the
  partial-failure check before any rename.
- **Cheap and smart LLM calls** both go through
  `anthropic.Client.Complete` so the Phase 1 isolation flags
  (`--setting-sources ""`, `--no-session-persistence`, etc.) apply
  uniformly. No new `claude` CLI flags introduced.

## What's NOT done in Phase 2

- **End-to-end smoke against the real corpus.** Skipped because
  Sarah has not yet provisioned a `VOYAGE_API_KEY`. The unit tests
  cover bucketing, canonicalization, filtering, tmpdir
  partial-failure, and round-trip IO. Once `VOYAGE_API_KEY` is set:

  ```
  go run . compose --stages cluster
  go run . compose --stages synthesize
  cat ~/.ghost/identity.md ~/.ghost/rules.md
  ```

  With Sarah's current single-extracted-transcript corpus, expect a
  small handful of single-member clusters and a `# Rules\n\nNo
  cross-project rules inferred yet.` placeholder until a second
  project's transcripts are extracted.

- **No tuning of `cluster_cosine_threshold` against real data.** The
  default (0.85) is the spec's number. Real-corpus reads will likely
  require adjustment — wait until there are enough observations to
  see the distribution.

- **No backfill of `LastCompose.PromptsVersion`.** Both stage 2 and
  stage 3 call `SetLastCompose([]string{<stage>}, "")` — same as
  Phase 1. Prompt-drift detection remains a minor deferred item.

- **No incremental cluster mode.** Stage 2 rebuilds `clusters.json`
  in full every run. Embedding cache makes this acceptable.

## Phase 3 scope (per spec, unchanged)

- Synthesis for `topics/*.md`, `voice/*.md`, and `index.md`.
- Voice synthesis gating (per-context threshold separate from rules).
- `/ghost` skill with lazy loading via the index.
- Slash commands beyond `add-rule` / `forget`.
- Topic-level evidence ranking.

## Outstanding minor items carried forward

Still deferred from Phase 1 (none made worse by Phase 2):

- `paths.Expand` errors silently ignored in `cmd/status.go`,
  `cmd/forget.go`, `cmd/show.go`. Status now also silently ignores a
  stat-error on `~/.ghost/` itself — acceptable since the loop falls
  through to "missing" messages.
- `--verbose` persistent flag still unread.
- `LastCompose.PromptsVersion` still `""`.
- `internal/transcript/discover.go` has an `osStat` indirection no
  test exercises.

## Commits added in Phase 2 (in order)

```
4a5dcec feat(embedding): add Embedder interface, ObservationHash, Cosine
34deafd feat(embedding): add model-id-gated cache for embeddings.json
de5be44 feat(embedding): add Voyage HTTP client implementing Embedder
0ac60b5 feat(cluster): add agglomerative bucketing with Go-side counts
2a11c50 feat(cluster): add cheap-LLM canonical phrasing (stage 2b)
86cc9b0 feat(cluster): wire embedding + bucket + canonical into a pipeline
af43422 feat(compose): dispatch --stages cluster (extract + cluster + synthesize-stub)
a732dcd feat(synthesize): identity.md generator (single-file, smart model)
39c154c feat(synthesize): rules.md generator with Go-side filter + subtractive prompt
6ff964a feat(synthesize): tmpdir pipeline with per-file rename and partial-failure preservation
891cd9e feat(compose): wire --stages synthesize and --stages all end-to-end
9301aeb feat(status): report cluster and synthesis state
```

## What's NOT in git

- Phase 1's untracked working-tree edits to
  `docs/specs/2026-05-20-ghost-design.md` and the deleted
  `docs/plans/2026-05-20-ghost-implementation.md` are still
  untracked. Left alone, same as Phase 1 handoff said.
- `~/.ghost/.state/` is still local-only. Phase 2 has not added new
  files there yet (no `clusters.json` until the user runs `compose
  --stages cluster` with a Voyage key).

## Workflow for the next session

1. Provision `VOYAGE_API_KEY`. Run `ghost compose --stages cluster`
   and inspect `~/.ghost/.state/clusters.json`. If most rule clusters
   collapse into one giant cluster, raise the threshold; if nothing
   merges, lower it.
2. Run `ghost compose --stages synthesize` and read both output
   files. If identity.md looks reasonable and rules.md correctly says
   "no cross-project rules inferred yet" (only one project's worth
   of observations so far), Phase 2 is operationally validated.
3. Use Claude Code normally for ~2 weeks. Watch whether the loaded
   identity actually calibrates responses. That is the spec's
   qualitative Phase 2 exit criterion.
4. Then start Phase 3: write the plan, execute via
   subagent-driven-development.

Do not auto-merge `phase-1-extract` into `main` yet — human decides
after Phase 2 is validated against real data.
