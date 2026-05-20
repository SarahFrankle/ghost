---
title: Ghost — design
date: 2026-05-20
status: approved (design phase)
---

# Ghost

Ghost reads your Claude Code conversation history and synthesizes
four kinds of output: identity context (who you are), behavior rules
(how Claude should work with you), voice templates (how you write,
per register), and indexed topic guidance (deeper material loaded on
demand). It replaces hand-curated memory with synthesis from
transcripts, so feedback given in one repo shapes Claude's behavior
in every repo.

The central design distinction: identity is *context for Claude*, not
a *template for Claude to mimic*. Knowing you work in Kotlin on
backend services helps Claude calibrate its answers, but it does not
narrow Claude away from frontend questions or any other domain.
Claude stays a generalist; identity sharpens context, rules govern
behavior, voice files exist only to be referenced when ghostwriting
on your behalf.

The shape of the output is inspired by `muse`, but ghost is a rewrite
with a different target: token economy at runtime, batching at compose
time, and a clear split between immutable observations and regenerable
synthesis.

## Goals

- One source: Claude Code transcripts under `~/.claude/projects/`.
- Three-layer output:
  - **Core (always loaded):** identity (context about you), rules
    (how Claude should behave), index (lookup table for the rest).
  - **Topics (lazy):** domain-specific guidance loaded when the
    conversation touches a topic.
  - **Voice templates (lazy):** per-register reference of how you
    write, loaded only when Claude is ghostwriting on your behalf.
- Cross-repo synthesis: rules that show up across many projects get
  promoted to global; single-project guidance lives in scoped topic
  files.
- Batchable compose: process a few conversations at a time, verify,
  resume. Never forced to run the whole pipeline in one shot.
- Token economy at runtime: always-loaded files stay small; deeper
  guidance and voice files load only when the conversation needs
  them.

## Separation of concerns

Three things are easy to conflate. Keep them distinct:

| Layer | Question it answers | Frame |
|---|---|---|
| **Identity** | Who is the user? What's their context? | Third-person context *for* Claude. Informs but does not constrain. |
| **Rules** | How should Claude behave when collaborating with the user? | Direct instructions *to* Claude. Apply universally. |
| **Voice** | How does the user write in register X? | Reference material *about* the user, loaded only when ghostwriting in that register. |

Concretely: "Sarah works in Kotlin on backend services" is identity,
not a rule that restricts Claude to backend. "Break comments at end
of thought" is a rule, not a voice preference. "Sarah writes annual
reviews in formal, structured prose" is a voice template, loaded only
when drafting annual reviews — it does not affect how Claude phrases
its own responses in normal CLI sessions.

Voice itself is not one thing. The same user writes differently in a
CLI chat, an annual self-review, Slack, and an exec briefing. One
voice file per register, with enough evidence to be useful.

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
            +- stage 1: extract observations per transcript            [role: cheap]
            +- stage 2: cluster observations by kind / topic / context [role: cheap]
            +- stage 3: synthesize identity + rules + topics + voice   [role: smart]
            +- stage 4: refine (Orwell pass, dedup)                    [role: smart]
            |
            v
~/.ghost/
  identity.md                  (always loaded; third-person context about user)
  rules.md                     (always loaded; how Claude should behave)
  rules.user.md                (always loaded; manual rules, survives recompose)
  index.md                     (always loaded; triggers for topics AND voice)
  topics/*.md                  (lazy; deeper guidance per domain)
  voice/*.md                   (lazy; per-register ghostwriting reference)
  .state/
    ledger.json                (processed conversations + content hash)
    observations/*.json        (per-transcript, append-only in practice)
    clusters.json              (stage 2 output, regenerable)
```

### Wiring into Claude Code

`~/.claude/CLAUDE.md` gets four include lines:

```markdown
@~/.ghost/identity.md
@~/.ghost/rules.md
@~/.ghost/rules.user.md
@~/.ghost/index.md
```

Topics and voice files are not included. Claude reads them on demand
when the skill matches a trigger from `index.md`. Voice files load
only when Claude is being asked to ghostwrite in a specific register
(e.g., drafting an annual review), never just because the user
mentioned a topic that adjacent material covers.

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

**Synthesis** (`identity.md`, `rules.md`, `index.md`, `topics/*.md`,
`voice/*.md`) is the distilled view derived from the observations
corpus. Fully
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
      "context": "cli-chat",
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

## Configuration

`~/.ghost/config.toml` is the single source of truth for tunable
runtime values. Stages reference *roles*, not model IDs; the config
maps roles to current model IDs. When a new model is released, you
edit one file.

```toml
[models]
cheap = "claude-haiku-4-5-20251001"
smart = "claude-opus-4-7"

[thresholds]
rule_min_evidence_count = 2
rule_min_project_count = 2
voice_min_evidence_count = 2

[paths]
transcripts_glob = "~/.claude/projects/**/*.jsonl"
output_dir = "~/.ghost"

[batching]
default_limit = 0   # 0 = unlimited
```

A baked-in default config ships with the binary; `~/.ghost/config.toml`
overrides it field-by-field. `ghost config show` prints the effective
config. `ghost config edit` opens it in `$EDITOR`.

## Pipeline stages

### Stage 1 — `extract` (per transcript, role: cheap)

Input: one transcript JSONL.
Output: `.state/observations/<transcript_hash>.json`.

Mechanical pattern-spotting. Cheap model is appropriate. Each
observation must carry evidence; voice observations must also carry a
`context` field naming the register they describe.

Default voice context is `cli-chat` (the user talking to Claude).
Other contexts (`annual-review`, `slack`, `exec-brief`, etc.) are
inferred from transcript content when the user is drafting or pasting
material destined for that register. When uncertain, the extractor
drops the observation rather than guessing.

Skip the active session's transcript (detect by checking if the file
is still being written to, or by matching against the current
`CLAUDE_SESSION_ID` if available).

### Stage 2 — `cluster` (corpus-level, role: cheap)

Input: all observation files concatenated.
Output: `.state/clusters.json`.

Groups observations by `kind` (identity, rule, voice, topic), then
sub-groups voice by `context` and topic by topic name. Collapses
near-duplicates within each group and merges their evidence lists
and source projects. This is where "I've said this 5 times across
different sessions" becomes a strong signal versus a one-off comment.
Evidence list length and project count are the frequency signals
stage 3 uses.

### Stage 3 — `synthesize` (corpus-level, role: smart)

Input: clusters.
Output: drafts of `identity.md`, `rules.md`, `topics/*.md`,
`voice/*.md`, and `index.md`.

One smart-model call per output file with the relevant cluster slice
as input.

- `identity.md` consumes the identity cluster. Output is short
  (~15-25 lines), third-person, factual: role, team, primary
  languages and stack, organizational context, headline expertise.
  No first-person prose, no voice mimicry, no behavioral instructions.
  This is reference context for Claude, not a template.
- `rules.md` consumes the rule cluster filtered by evidence count ≥ 2
  AND project count ≥ 2. One-off comments do not become global rules;
  rules that appear in only one project do not become global rules.
  These are instructions to Claude that govern Claude's behavior
  regardless of register.
- `topics/<name>.md` consumes its topic cluster, including
  single-project topics. A rule that appears in only one repo lives
  here, not in global rules.
- `voice/<context>.md` consumes the voice cluster for that context,
  filtered by evidence count ≥ `voice_min_evidence_count` (default
  2). Output describes the user's writing style in that register:
  diction, sentence shape, register, lowercase/sentence-case habits,
  framing patterns. Each file makes clear it is reference material
  for ghostwriting in that register — NOT a directive that affects
  how Claude phrases its own responses generally.
- `index.md` is generated last. For each topic file AND each voice
  file, the smart model produces trigger phrases the skill uses to
  decide when to load. Voice triggers are narrower than topic
  triggers and key on ghostwriting tasks (e.g., "drafting an annual
  review", "writing a Slack post in my voice"), not on the register
  generally being mentioned.

### Stage 4 — `refine` (per output file, role: smart)

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
description: Use at the start of any task. Checks the ghost index and
  reads matching topic OR voice files before responding. Triggers on
  any task touching an entry listed in ~/.ghost/index.md.
---

# Ghost — lazy-load topic and voice guidance

You have identity context and a rule set always loaded. You also have
an index at ~/.ghost/index.md listing two kinds of lazy-loaded files:

- **topic files** under `~/.ghost/topics/` — deeper guidance for
  specific domains (testing, writing, git-workflow, etc.)
- **voice files** under `~/.ghost/voice/` — references for how the
  user writes in specific registers, used ONLY when ghostwriting on
  their behalf

## Mechanical check (before responding to the user)

1. Read ~/.ghost/index.md if you have not already this session.
2. Match the user's request against the triggers for each entry.
3. If a TOPIC entry matches, Read that topic file before writing
   code or answering.
4. If a VOICE entry matches AND the user is asking you to draft or
   write something in that register, Read that voice file before
   producing the draft. Do NOT load a voice file just because the
   register is mentioned — only when you're being asked to write in
   it.
5. If nothing matches, proceed without loading anything.

A file loaded once per session stays in context — do not re-Read it.

## Identity is context, not a template

The always-loaded `identity.md` tells you who the user is. Use it to
calibrate your answers (their stack, expertise, organization), not as
a template to mimic. The user is a specialist in some areas; you stay
a generalist across all areas.

## Voice files do not affect your normal speech

Voice files describe the user's writing style in a specific register.
They are reference material for ghostwriting. They do NOT instruct
you to adopt that voice in your normal responses. When you load
`voice/cli-chat.md` because the user is asking you to draft a CLI
message in their voice, mirror that voice in the draft only — your
surrounding response stays in your normal voice.

## What NOT to load

Do not load every topic or voice file at session start. The whole
point is lazy loading. Loading everything defeats the token-economy
design.
```

### Slash commands

- `/ghost show` — print identity + rules + manual rules.
- `/ghost topics` — list topic files with last-modified.
- `/ghost voice` — list voice files (one per register).
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
conversations, ask a judge LLM, given this conversation, does the
identity describe the person accurately, do the rules cover their
preferences, and (for voice files) does the relevant voice file
match how they wrote here? Output a score per dimension (identity
match, rule coverage, voice match per context, false positives). Run
after prompt changes to catch regressions. Resist building a
multi-dimension eval harness on day one.

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
- Voice context detection at extract time. Defaulting to `cli-chat`
  is correct for most Claude Code transcripts. Detecting when the
  user is drafting another register inside a CLI session (annual
  review, Slack post, exec brief) needs a clear heuristic — likely
  the user explicitly framing the task ("help me draft my annual
  review"). Worth tuning the extract prompt and watching false
  positives in early eval runs.

## Summary

Ghost is a small Go CLI plus a Claude Code skill. The CLI does
out-of-band synthesis from Claude Code transcripts in four stages,
maintaining a hashed ledger so compose is resumable and batchable.
The skill enforces a three-layer runtime: a small always-loaded core
(identity context, behavior rules, lookup index); a lazy library of
topic files for domain guidance; and a lazy library of voice files,
one per writing register, loaded only when ghostwriting. Identity is
context for Claude, not a template Claude mimics. Voice is reference
material for ghostwriting, not a directive that affects Claude's
normal responses. Cross-project frequency is the signal that
distinguishes "how Sarah works" from "how Sarah works in one
specific repo."
