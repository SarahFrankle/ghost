---
title: Ghost — design
date: 2026-05-20
status: approved (design phase)
---

# Ghost

Ghost reads your Claude Code conversation history and synthesizes a
profile, rule set, and indexed library of topic files that load
automatically into every Claude Code session. It replaces hand-curated
memory with synthesis from transcripts, so feedback given in one repo
shapes Claude's behavior in every repo.

The shape of the output is inspired by `muse`, but ghost is a rewrite
with a different target: token economy at runtime, batching at compose
time, and a clear split between immutable observations and regenerable
synthesis.

## Goals

- One source: Claude Code transcripts under `~/.claude/projects/`.
- Two-tier output: a small always-loaded core (profile + rules + index)
  and a lazy library of topic files.
- Cross-repo synthesis: rules that show up across many projects get
  promoted to global; single-project guidance lives in scoped topic
  files.
- Batchable compose: process a few conversations at a time, verify,
  resume. Never forced to run the whole pipeline in one shot.
- Token economy at runtime: always-loaded files stay small; deeper
  guidance loads only when the conversation touches it.

## Non-goals

- Multi-source ingestion (Slack, GitHub, Codex). Source surface stays
  Claude Code only.
- Hosted deployment, S3 sync, sharing across users.
- PDF export, MCP server mode, clustering visualizations.
- Replacing project-local `CLAUDE.md` files. Ghost is global.

## Architecture

Two artifacts:

1. **`ghost` Go CLI.** Owns the compose pipeline. Reads transcripts,
   calls the Anthropic API in stages, writes output files. Stateful:
   maintains a ledger of which conversations have been processed.
2. **`ghost` Claude Code skill.** Thin in-session layer at
   `~/.claude/skills/ghost/SKILL.md`. Owns runtime integration —
   teaches Claude when to lazy-load topic files from the index. Also
   provides slash commands for read-only operations.

Compose runs out-of-band as a CLI invocation, not inside an interactive
Claude session. This is the central token-economy choice: synthesis
work happens against the Anthropic API directly with prompt caching and
per-stage model selection, not by dragging transcripts through a live
conversation context.

### Data flow

```
~/.claude/projects/**/*.jsonl   (source of truth)
            |
            v
       ghost compose            (CLI, batch, out-of-band)
            |
            +- stage 1: extract observations per transcript  [Haiku 4.5]
            +- stage 2: cluster observations into themes     [Haiku 4.5]
            +- stage 3: synthesize profile + rules + topics  [Opus 4.7]
            +- stage 4: refine (Orwell pass, dedup)          [Opus 4.7]
            |
            v
~/.ghost/
  profile.md                   (always loaded; voice + identity)
  rules.md                     (always loaded; mechanical do/don't)
  rules.user.md                (always loaded; manual escape hatch)
  index.md                     (always loaded; topic lookup table)
  topics/*.md                  (lazy; Claude reads on demand)
  .state/
    ledger.json                (processed conversations + content hash)
    observations/*.json        (per-transcript, append-only in practice)
    clusters.json              (stage 2 output, regenerable)
```

### Wiring into Claude Code

`~/.claude/CLAUDE.md` gets four include lines:

```markdown
@~/.ghost/profile.md
@~/.ghost/rules.md
@~/.ghost/rules.user.md
@~/.ghost/index.md
```

Topics are not included. Claude reads them on demand when the skill
matches a trigger from `index.md`.

## Data model

The model has two layers with very different mutability.

**Observations** (`.state/observations/<transcript_hash>.json`) are
atomic facts extracted from transcripts, each carrying evidence (source
conversation + turn) and a `kind` tag. A given transcript's
observations file is frozen once extracted; the corpus grows as new
transcripts are processed. Observations files change only when (a) a
new transcript is processed, (b) a known transcript's content hash
changes because Claude Code appended to it, or (c) the user explicitly
prunes with `ghost forget`.

**Synthesis** (`profile.md`, `rules.md`, `index.md`, `topics/*.md`) is
the distilled view derived from the observations corpus. Fully
regenerable: stages 2–4 reread the whole corpus and rewrite synthesis
from scratch. This is event sourcing — observations are the immutable
log, synthesis is the materialized view.

The split matters because LLM synthesis is non-deterministic. You want
to be able to tweak a synthesis prompt and rebuild the view without
re-paying for extraction.

### Observation schema

```json
{
  "source": "~/.claude/projects/-Users-sarah-dev-projects/abc.jsonl",
  "project": "Users-sarah-dev-projects",
  "content_hash": "sha256:...",
  "extracted_at": "2026-05-20T14:22:00Z",
  "observations": [
    {
      "kind": "rule",
      "text": "user wants comments broken at end of thought, not wrapped mid-sentence",
      "evidence": "turn 14: 'don't break the comment mid-sentence...'",
      "confidence": "high"
    },
    {
      "kind": "voice",
      "text": "uses lowercase, short fragments, no em-dashes",
      "evidence": "turns 3, 7, 22"
    },
    {
      "kind": "topic",
      "topic": "testing",
      "text": "prefers integration tests over mocks for database code",
      "evidence": "turn 9: 'don't mock the database'"
    },
    {
      "kind": "identity",
      "text": "works on Content Security team at Miro, 7 microservices",
      "evidence": "turn 1"
    }
  ]
}
```

`project` is derived from the transcript path (Claude Code encodes the
cwd into the directory name). It is the basis for cross-project
frequency scoring in stage 3.

## Pipeline stages

### Stage 1 — `extract` (per transcript, Haiku 4.5)

Input: one transcript JSONL.
Output: `.state/observations/<transcript_hash>.json`.

Mechanical pattern-spotting. Cheap model is appropriate. Each
observation must carry evidence — synthesis cites observations, and
observations cite transcripts.

Skip the active session's transcript (detect by checking if the file
is still being written to, or by matching against the current
`CLAUDE_SESSION_ID` if available).

### Stage 2 — `cluster` (corpus-level, Haiku 4.5)

Input: all observation files concatenated.
Output: `.state/clusters.json`.

Groups observations by topic, collapses near-duplicates, merges their
evidence lists. This is where "I've said this 5 times across different
sessions" becomes a strong signal versus a one-off comment. Evidence
list length is the frequency signal stage 3 uses.

### Stage 3 — `synthesize` (corpus-level, Opus 4.7)

Input: clusters.
Output: drafts of `profile.md`, `rules.md`, `index.md`, `topics/*.md`.

One Opus call per output file with the relevant cluster slice as input.

- `profile.md` consumes voice + identity clusters.
- `rules.md` consumes the rule cluster filtered by evidence count ≥ 2
  AND project count ≥ 2. One-off comments do not become global rules;
  rules that appear in only one project do not become global rules.
- `topics/<name>.md` consumes its topic cluster, including
  single-project topics. A rule that appears in only one repo lives
  here, not in global rules.
- `index.md` is generated last: for each topic file, Opus produces
  trigger phrases that the skill uses to decide when to load.

### Stage 4 — `refine` (per output file, Opus 4.7)

Applies an Orwell-style pass to each generated file: delete sentences
you wouldn't miss, kill em-dashes, strip self-congratulation, prefer
short concrete specifics. Separate from synthesis so the refine prompt
can be tuned without rerunning synthesis.

## Incremental compose and batching

The ledger (`~/.ghost/.state/ledger.json`) tracks processing state per
transcript:

```json
{
  "conversations": {
    "<transcript_path>": {
      "content_hash": "sha256:...",
      "processed_at": "2026-05-20T14:22:00Z",
      "observations_file": ".state/observations/abc123.json",
      "message_count": 47
    }
  },
  "last_compose": {
    "at": "2026-05-20T14:30:00Z",
    "stages_run": ["extract", "cluster", "synthesize", "refine"]
  }
}
```

Content hash is what makes the ledger correct: when Claude Code
appends to a JSONL file, the hash changes and the transcript is
re-extracted. Without hashing, the ledger goes silently stale.

Two independent batching axes:

1. `--limit N` — process at most N unprocessed conversations this run.
   Default: unlimited. Sorted oldest-first so backlog drains
   predictably.
2. `--stages extract` / `--stages extract,cluster` / `--stages all` —
   run only a subset of pipeline stages. Default: `all`.

This separation works because stage 1 is per-record (per-transcript)
and stages 2–4 are corpus-level. Extraction is resumable; synthesis is
a whole-corpus operation that only needs to run when you want the
materialized view refreshed.

Workflow this enables:

```bash
ghost compose --limit 5 --stages extract        # cheap, verify
ghost show observations --recent                # eyeball
ghost compose --limit 5 --stages extract        # next 5
# ... repeat ...
ghost compose --stages cluster,synthesize,refine  # roll up
```

Other knobs:

- `--dry-run` — show what would be processed.
- `--since 7d` — only transcripts modified in last N days.
- `--project <name>` — only transcripts under a specific project dir.
- `ghost status` — ledger summary: total / processed / pending / dirty.

## Runtime: the skill

The CLI builds artifacts. The skill is what makes Claude actually use
them with the right discipline.

### SKILL.md

```markdown
---
name: ghost
description: Use at the start of any task. Checks the ghost topic
  index and reads matching topic files before responding. Triggers on
  any task touching a topic listed in ~/.ghost/index.md.
---

# Ghost — lazy-load topic guidance

You have a global profile and rule set always loaded. You also have an
index of deeper topic files at ~/.ghost/index.md.

## Mechanical check (before responding to the user)

1. Read ~/.ghost/index.md if you have not already this session.
2. Match the user's request against the triggers for each topic.
3. If any topic matches, Read that topic file BEFORE writing code or
   answering. Do not skip on the grounds that you "probably know."
4. If no topic matches, proceed without loading anything.

A topic loaded once per session stays in context — do not re-Read it.

## What NOT to load

Do not load every topic file at session start. The whole point is lazy
loading. Loading all topics defeats the token-economy design.
```

### Slash commands

- `/ghost show` — print profile + rules.
- `/ghost topics` — list topic files with last-modified.
- `/ghost status` — ledger summary.
- `/ghost add-rule "<text>"` — append to `rules.user.md` (survives
  recompose).
- `/ghost forget <conv>` — drop a conversation's observations and
  recompose synthesis.

`compose` is intentionally NOT a slash command. It is the expensive
batch step and belongs at the terminal.

## Testing and eval

Pure-Go logic (ledger, hash, transcript parsing, glob/filter, project
tagging) gets standard unit tests.

LLM stages get golden-transcript fixtures in
`internal/<stage>/testdata/`: hand-picked transcript JSONLs with
hand-written expected observations. Tests use a similarity check
(embedding cosine or judge LLM) with a confidence threshold rather
than exact-match.

Each stage is a pure function taking inputs and returning outputs. No
filesystem or global state inside stage functions; that lives at the
CLI layer.

`GHOST_E2E=1` runs the real pipeline against a small fixture set; off
by default in CI.

`ghost eval` is a thin synthesis-quality check: hold out ~10% of
conversations, ask a judge LLM "given this conversation, does the
profile/rules describe this person accurately?" Output a score per
dimension (voice match, rule coverage, false positives). Run after
prompt changes to catch regressions. Resist building a multi-dimension
eval harness on day one.

## Migration off `~/.claude/memory/`

Day 1:

1. Build ghost, run `ghost compose` against the full transcript
   history (batched as desired).
2. Add the four `@~/.ghost/...` includes to `~/.claude/CLAUDE.md`
   above the existing `@memory/MEMORY.md` line.
3. Keep `@memory/MEMORY.md` active. Both load.

Use Claude Code normally for two weeks.

Week 1 review: read the generated files, cross-check against
hand-curated memory. Three buckets:

- Ghost captured it correctly → that memory file can be removed later.
- Ghost missed it → add as a manual rule via `/ghost add-rule`, or
  note for prompt tuning.
- Ghost got it wrong → tune the prompt or `ghost forget` the
  misleading source conversations.

Day 14:

1. Remove the `@memory/MEMORY.md` line from `~/.claude/CLAUDE.md`.
2. Move `~/.claude/memory/` to `~/.claude/memory.archive/`. Do not
   delete — it's the cross-check baseline if ghost ever regresses.

From this point: feedback given in sessions flows into transcripts
and gets picked up the next time `ghost compose` runs. The reactive
"save this as memory" pattern becomes unnecessary.

## Open questions

- Embedding-based dedup in stage 2 versus LLM-only dedup. Embedding
  cosine is cheaper and more deterministic; LLM dedup handles
  paraphrase better. Likely start with LLM, add embedding short-circuit
  if cost matters.
- Whether to track per-topic load frequency at runtime to surface
  candidates for promotion (topic → rule) or demotion (rule → topic).
  Defer until there is signal that the index is mis-tuned.
- Active-session transcript detection. `CLAUDE_SESSION_ID` may not be
  available outside the session; falling back to "file modified within
  the last N minutes" is a reasonable heuristic but worth confirming.

## Summary

Ghost is a small Go CLI plus a Claude Code skill. The CLI does
out-of-band synthesis from Claude Code transcripts in four stages,
maintaining a hashed ledger so compose is resumable and batchable. The
skill enforces a two-tier runtime: small always-loaded profile and
rules, plus a lazy-loaded library of topic files keyed by an
LLM-generated trigger index. Cross-project frequency is the signal
that distinguishes "how Sarah works" from "how Sarah works in one
specific repo."
