# Phase 1 handoff — extract pipeline complete

**Date:** 2026-05-21
**Branch:** `phase-1-extract` (13 commits ahead of `main`)
**Status:** Phase 1 implementation complete; smoke-tested end-to-end; Phase 2 plan not yet written.

## What Phase 1 ships

The `ghost` Go CLI with four subcommands:

- `ghost compose [--limit N] [--stages extract] [--dry-run]` — for each
  unprocessed transcript: read JSONL → render turns → call `claude -p`
  with the extract system prompt → validate + scrub the response → write
  atomic per-transcript observations file → update ledger.
- `ghost status` — total / processed / pending / dirty counts plus
  last-compose timestamp.
- `ghost forget <transcript-path>` — drop a conversation's observations
  and ledger entry. Prints a stale-synthesis warning (relevant once
  Phase 2 ships).
- `ghost show observations [--recent N]` — pretty-print the N most
  recently extracted observation files for hand-review.

All Phase-1 spec requirements covered. Skill, lazy loading, clustering,
synthesis, and the `@~/.ghost/...` user-facing files are explicitly
deferred to Phase 2/3.

## How extract is wired (important context for Phase 2)

Ghost does NOT use the official Anthropic Go SDK or an
`ANTHROPIC_API_KEY`. Instead, `internal/anthropic/client.go` shells out
to the `claude` CLI to reuse Sarah's Claude Code subscription. The
isolation incantation that produces clean, transcript-grounded
extractions:

```
claude -p \
  --model <id> \
  --system-prompt <prompt> \
  --output-format text \
  --setting-sources "" \
  --disable-slash-commands \
  --tools "" \
  --no-session-persistence
```

Without `--setting-sources ""` the user's CLAUDE.md leaks into every
prompt and the model invents "observations" from its prose. Without
`--no-session-persistence` every subprocess call gets logged as a new
transcript under `~/.claude/projects/-<cwd>-/`, creating a feedback
loop where ghost extracts from its own prior outputs. Both bugs were
hit in real testing — keep these flags. Do NOT switch to `--bare` (it
forces API-key auth and refuses OAuth/keychain).

Phase 2 will add more `claude -p` calls (cluster's canonical-phrasing
step and synthesize's per-file generation). They should all go through
the same `anthropic.Client.Complete` interface so the isolation flags
apply uniformly.

## Smoke-test confirmation

- `ghost status` correctly reports 686 transcripts pending on Sarah's
  laptop.
- `ghost compose --limit 1` against a real data-discovery transcript
  produced 2 grounded observations (cited turn-N evidence, no
  CLAUDE.md leakage).
- `ghost forget` + re-`compose` reproduces, confirming hash-based
  re-detection works.
- 21 contaminated transcripts (ghost subprocess artifacts from before
  the `--no-session-persistence` fix) were deleted from
  `~/.claude/projects/-Users-sarah-dev-projects-ghost/`. No other
  contamination found.

## Outstanding issues to address in or before Phase 2

From the final code review (see `docs/superpowers/plans/2026-05-20-ghost-phase-1-extract.md`
plus review notes in chat history). Severity tags from that review:

### Important (worth fixing before draining a real backlog)

- **`cmd/compose.go` saves ledger only at the end of the run.** A
  Ctrl-C mid-run orphans every successfully-extracted observation file.
  Fix: `l.Save(ledgerPath)` after each successful `l.Mark` (mutex
  already held during the goroutine block).
- **`internal/extract/parseObservations` is brittle.** Uses naive
  `strings.Index("{")` / `LastIndex("}")` — if the model emits prose
  containing braces around the JSON, the span is wrong. Consider a
  balanced-object scan or use `--json-schema` on `claude -p` for
  structured output.
- **`internal/secrets/scrub.go` false positives.** The `long_hex` and
  `long_base64` patterns (48+ chars) will silently drop legitimate
  observations whose evidence quotes contain commit-SHA chains or
  base64 test fixtures. Raise thresholds or downgrade these to
  warn-only.

### Minor (defer unless they bite)

- `internal/ledger/Save` reimplements tmp+rename instead of using
  `atomicfs.WriteFile`; also doesn't `MkdirAll` if `.state/` is missing
  on first save.
- `paths.Expand` errors are silently ignored in `cmd/status.go`,
  `cmd/forget.go`, `cmd/show.go` (the `_ = err` pattern).
- `internal/transcript/discover.go` has an `osStat` indirection for
  testability that no test actually uses.
- `--verbose` persistent flag exists in `cmd/root.go` but nothing reads
  it.
- `LastCompose.PromptsVersion` is always set to `""`. The spec defines
  this field for prompt-drift detection; consider hashing the embedded
  prompt directory at startup so Phase 2's `ghost status` can flag
  drift.
- `internal/anthropic/client.go` hardcodes nothing per-model in the
  current shell-out path (good), but the SDK fallback path is removed
  entirely. If `claude` CLI is missing, error message could suggest
  installing Claude Code.

### Spec gaps (intentionally deferred in Phase 1)

- No `schema_version` mismatch handling on ledger load. Spec defers
  this until a v2 schema actually exists.
- No `--exclude` / `--project` / `--since` filters on `compose` or
  `discover`. Spec lists these as post-v1.

## Phase 2 scope (per spec)

Goal: deliver the always-loaded core (identity.md, rules.md) with no
lazy loading yet.

In scope:
- Stage 2 clustering — embedding-bucket + cheap LLM canonical phrasing
  + Go-side counts (`EvidenceCount`, `ProjectCount`).
- Stage 3 synthesis for `~/.ghost/identity.md` and `~/.ghost/rules.md`
  only.
- Atomic multi-file synthesis writes (tmpdir + directory rename).
- Wire the two `@~/.ghost/...` includes into Sarah's
  `~/.claude/CLAUDE.md` (already added there manually — verify it still
  matches what Phase 2 generates).
- `ghost compose --stages cluster,synthesize` and `--stages all`.

Out of scope: topics, index, voice, skill, slash commands beyond
`add-rule` / `forget`, any lazy loading.

Exit criterion: two weeks of normal Claude Code use shows identity
context calibrates responses correctly and synthesized rules reflect
feedback given across multiple projects.

## Workflow for the next session

1. Read `docs/specs/2026-05-20-ghost-design.md` (Phase 2 section + the
   "Stage 2", "Stage 3", "Synthesis" subsections).
2. Decide which Phase-1 outstanding issues to fix first (the three
   "Important" ones at minimum).
3. Write the Phase 2 plan into `docs/superpowers/plans/`.
4. Execute via subagent-driven-development like Phase 1.

## What's NOT in git

- The 21 deleted contaminated transcripts under
  `~/.claude/projects/-Users-sarah-dev-projects-ghost/` are gone from
  disk. They were never tracked.
- The working tree has unrelated user edits to
  `docs/specs/2026-05-20-ghost-design.md` and a deleted
  `docs/plans/2026-05-20-ghost-implementation.md` (an older,
  superseded plan). Both predate Phase 1 work — leave them alone.
- `~/.ghost/.state/` contains one extraction (from
  `agent-adde1b4.jsonl`). Not in git, expected to grow.
