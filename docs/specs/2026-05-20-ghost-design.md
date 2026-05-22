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

The architecture has three load-bearing choices: token economy at
runtime (always-loaded files stay small and capped), batching at
compose time (synthesis runs out-of-band, not in-session), and a
clear split between immutable observations and regenerable synthesis
(so prompt tuning is cheap).

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
            +- stage 2: cluster                                        [embeddings + cheap LLM]
            |     2a: embedding-bucket by (kind, sub-key)              [deterministic, Go]
            |     2b: LLM picks canonical phrasing per multi-entry bucket [role: cheap]
            |     2c: count evidence + distinct projects               [deterministic, Go]
            +- stage 3: synthesize identity + rules + topics + voice   [role: smart]
            |     partial failures write what succeeded.
            |
            v
~/.ghost/
  identity.md                  (always loaded; third-person context about user)
  rules.md                     (always loaded; how Claude should behave)
  rules.user.md                (always loaded; manual rules, survives recompose)
  index.md                     (always loaded; capped; triggers for topics AND voice)
  topics/*.md                  (lazy; deeper guidance per domain)
  voice/*.md                   (lazy; per-register ghostwriting reference)
  .state/
    ledger.json                (schema_version, prompts_version, processed conversations + hash)
    observations/*.json        (per-transcript, append-only in practice)
    clusters.json              (stage 2 output, regenerable)
    embeddings.json            (stage 2a cache, regenerable)
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
corpus. Fully regenerable: stages 2–3 reread the whole corpus and
rewrite synthesis from scratch. This is event sourcing — observations
are the immutable log, synthesis is the materialized view.

Between observations and synthesis sits an intermediate shape:
**cluster members**. Each cluster carries a list of `ClusterMember`
records, each identifying a source observation by
`(content_hash, observation_index, project)` — `content_hash` names
the transcript's observations file and `observation_index` is the
position of the observation within that file's `observations` array.
The LLM chooses the canonical phrasing of a cluster, but the
*members* of a cluster come from deterministic embedding-based
bucketing, and frequency counts (evidence count, distinct project
count) are computed in Go from the member list — never from anything
the LLM emits.

Why counts are Go-side: counts gate threshold-based promotion (a
rule needs ≥2 evidence across ≥2 projects to land in `rules.md`).
Delegating those counts to a non-deterministic model would let prompt
drift silently move rules in and out of the always-loaded core
between compose runs. Keeping the count deterministic also preserves
the audit trail from any rendered rule back to its originating turns.

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
embedding = "voyage-3-lite"

[thresholds]
rule_min_evidence_count = 2
rule_min_project_count = 2
voice_min_evidence_count = 2
cluster_cosine_threshold = 0.85

[index]
max_topic_entries = 20

[voice]
enabled = false  # v1: extract + cluster voice observations, but do
                 # not synthesize voice/*.md until eval signal is good

[paths]
transcripts_glob = "~/.claude/projects/**/*.jsonl"
output_dir = "~/.ghost"

[batching]
default_limit = 0    # 0 = unlimited
extract_workers = 5
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
drops the observation rather than guessing. Dropped observations are
not written to disk and leave no trace; only the validated set lands
in the observations file.

**Active-session skip.** Skip any transcript whose mtime is within
the last 5 minutes. This is a heuristic — `CLAUDE_SESSION_ID` is not
reliably exposed outside the live session — but it is cheap, has no
false negatives that matter (the next compose picks it up), and
prevents the feedback loop where ghost extracts its own scaffolding
turns.

**Schema validation.** After the LLM returns JSON, validate each
observation: `kind ∈ {identity, rule, topic, voice}`, voice carries
`context`, topic carries `topic`. Drop malformed records with a
warning rather than silently accepting a typo'd `kind`.

**Secret scrubbing (post-filter, deterministic, Go).** Before writing
the observations file, drop any observation whose `evidence` (or
`text`) matches a credential pattern: API key prefixes (`sk-`, `pk-`,
`ghp_`, `gho_`, `AKIA`, `ASIA`, etc.), JWTs (`eyJ[A-Za-z0-9_-]+\.`),
`Authorization: Bearer …`, PEM block headers, high-entropy hex/base64
runs over a length threshold. Dropped records are logged with the
matched pattern (not the matched value) and never land on disk. This
is the load-bearing safety net — `/ghost scan` exists as a reactive
backstop, but extraction is where secrets are filtered out before
they reach `.state/observations/*.json` or the embedding cache.

### Stage 2 — `cluster` (corpus-level, two-pass)

Input: all observation files.
Output: `.state/clusters.json` (with embedding cache in
`.state/embeddings.json`).

**2a — Embedding bucket (deterministic, Go).** Compute embeddings
for each observation's `text` (cached by `(observation_hash,
embedding_model_id)` so reruns are free but a model change cleanly
invalidates). Partition by `(kind, sub-key)`: identity, rule, voice
sub-grouped by context, topic sub-grouped by topic name. Within each
partition, agglomerative-merge at cosine similarity ≥
`cluster_cosine_threshold`. Buckets of size 1 skip 2b entirely.

The cosine threshold is **model-coupled**: cosine distributions
differ between embedding models, so swapping `[models].embedding`
implies re-tuning the threshold. `embeddings.json` records the model
ID it was built with; a mismatch with config forces re-embedding
before clustering runs.

**2b — Canonical phrasing (role: cheap, per multi-entry bucket).**
For each bucket with >1 entry, the cheap model picks the canonical
phrasing and confirms the entries truly describe the same thing.
Each call sees ≤10 entries — context-bounded by construction.

**2c — Counts (deterministic, Go).** From each bucket's
`[]ClusterMember`, compute `EvidenceCount = len(members)` and
`ProjectCount = |distinct project keys|`. These are the frequency
signals stage 3 uses. The LLM never emits a number that drives a
threshold.

### Stage 3 — `synthesize` (corpus-level, role: smart)

Input: clusters with deterministic counts.
Output: `identity.md`, `rules.md`, `topics/*.md`, `voice/*.md`,
`index.md`.

One smart-model call per output file with the relevant cluster slice
as input. Each synthesis prompt carries explicit prose discipline:
delete sentences you wouldn't miss, no em-dashes, no
self-congratulation, prefer short concrete specifics. The output of
this stage is the final output written to disk.

- `identity.md` consumes the identity cluster. Output is short
  (~15 lines), third-person, factual, and **session-agnostic**:
  name, employer, role, team, contact, broad technical background.
  Project-specific facts (specific repos, services, frameworks tied
  to one codebase, recent branches, ticket numbers) MUST live in
  topic files, not here, because `identity.md` is loaded into every
  session regardless of which project the user is in. The identity
  synthesis prompt was tightened during Phase 2 (see commit history)
  after the initial generation surfaced too much project-specific
  detail; the corresponding observations remain in `clusters.json`
  and will flow into `topics/*.md` in Phase 3.
  No first-person prose, no voice mimicry, no behavioral instructions.
  This is reference context for Claude, not a template.
- `rules.md` consumes the rule cluster filtered (in Go, before the
  LLM call) by evidence count ≥ 2 AND project count ≥ 2. One-off
  comments do not become global rules; rules that appear in only
  one project do not become global rules. These are instructions to
  Claude that govern Claude's behavior regardless of register.
  **Subtractive synthesis against `rules.user.md`:** the current
  contents of `rules.user.md` are passed as context, and the prompt
  instructs the smart model to OMIT any synthesized rule that
  contradicts a user rule. This enforces precedence at compose time
  rather than relying on prose hints to Claude at runtime — two
  conflicting rules never land on disk simultaneously.
- `topics/<name>.md` consumes its topic cluster, including
  single-project topics. A rule that appears in only one repo lives
  here, not in global rules.
- `voice/<context>.md` consumes the voice cluster for that context,
  filtered by evidence count ≥ `voice_min_evidence_count` (default
  2). Output describes the user's writing style in that register:
  diction, sentence shape, register, lowercase/sentence-case habits,
  framing patterns. Each file makes clear it is reference material
  for ghostwriting in that register — NOT a directive that affects
  how Claude phrases its own responses generally. **Voice synthesis
  is gated** by `[voice].enabled` (default false in v1): voice
  observations are extracted and clustered, but voice files are not
  written until `ghost eval` shows voice-context inference is
  reliable on a labeled fixture set. See "Post-v1 tracking."
- `index.md` is generated last and is **capped** (see "Always-loaded
  budget" below). For each retained topic file AND each voice file,
  the smart model produces trigger phrases the skill uses to decide
  when to load. Voice triggers are narrower than topic triggers and
  key on ghostwriting tasks ("drafting an annual review"), not on
  the register being mentioned.

**Atomic writes.** Synthesis writes all output files to a sibling
tmpdir (`~/.ghost/.tmp-synthesize-<timestamp>/`), then renames the
directory contents into `~/.ghost/` as a unit only after every file
in the generation has been produced. The always-loaded set
(`identity.md`, `rules.md`, `index.md`) is never observed in a
mixed-generation state — Claude will not load new rules against a
stale index.

**Partial-failure handling.** Synthesis collects per-file errors
instead of returning on the first one. If ANY file fails, the
tmpdir is preserved and nothing is renamed into place; the previous
generation's outputs remain authoritative. A retry of `compose
--stages synthesize` regenerates only the failed files and, once
the set is complete, performs the atomic swap.

## Always-loaded budget

Only four files are always loaded into every Claude Code session:
`identity.md`, `rules.md`, `rules.user.md`, `index.md`. Their combined
size is the only token cost paid on every CLI invocation, forever —
so they are budgeted, not unbounded.

- `identity.md` is capped at ~25 lines by prompt construction.
- `rules.md` grows with rule count but is gated by the evidence + project
  thresholds. In practice it plateaus.
- `rules.user.md` is user-controlled. No cap, but the user owns it.
- `index.md` is capped at the top 20 topic entries by evidence count,
  plus all voice entries (voice registers are few). Topics outside
  the cap still exist on disk and remain loadable on explicit `Read`
  or via `ghost topics` — they just don't fire automatic triggers.

`rules.user.md` overrides `rules.md` when they conflict. The file
template states this at the top so the precedence is visible to
Claude when both are loaded.

## Failure modes

- **Stage 1 JSON malformed.** Drop the offending observation, log,
  continue. Never poison the corpus.
- **Stage 3 partial.** Write what succeeded, report the rest, allow
  resumption.
- **`ghost forget`.** Removes a conversation's observations and its
  ledger entry, but does NOT auto-recompose. The command prints
  "synthesis is now stale; run: ghost compose --stages
  cluster,synthesize". Silent staleness is the worst outcome.
- **Prompt drift.** The embedded prompt directory is hashed at build
  time. `ledger.json` records `prompts_version`. `ghost status`
  reports "prompts: drifted (was X, now Y)" when the binary's hash
  differs from the ledger's, signalling that synthesis should be
  rerun.
- **Ledger schema drift.** `ledger.json` carries `schema_version`.
  `Load` refuses to run on a newer version than the binary knows;
  this is the cheapest possible insurance against silent corruption
  after a binary upgrade.

## Incremental compose and batching

The ledger (`~/.ghost/.state/ledger.json`) tracks processing state per
transcript:

```json
{
  "schema_version": 1,
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
    "stages_run": ["extract", "cluster", "synthesize"],
    "prompts_version": "sha256:9f3b1c..."
  }
}
```

`schema_version` lets a newer binary refuse to operate on a ledger
shape it doesn't understand. `prompts_version` is a hash of the
binary's embedded prompt directory, used by `ghost status` to detect
when synthesis is stale relative to the current prompts.

Content hash is what makes the ledger correct: when Claude Code
appends to a JSONL file, the hash changes and the transcript is
re-extracted. Without hashing, the ledger goes silently stale.

Two independent batching axes:

1. `--limit N` — process at most N unprocessed conversations this run.
   Default: unlimited. Sorted oldest-first so backlog drains
   predictably.
2. `--stages extract` / `--stages extract,cluster` / `--stages all` —
   run only a subset of pipeline stages. Default: `all`. Stages can
   be run individually; each reads its predecessor's on-disk output
   (extract → `.state/observations/*.json`, cluster →
   `.state/clusters.json`).

This separation works because stage 1 is per-record (per-transcript)
and stages 2–3 are corpus-level. Extraction is resumable; clustering
and synthesis are whole-corpus operations that only need to run when
you want the materialized view refreshed.

`compose` runs extract calls in parallel with a bounded worker pool
(default 5). The ledger is mutex-guarded; per-transcript work is
otherwise independent. A 6-month backlog drains in minutes rather
than hours.

Workflow this enables:

```bash
ghost compose --limit 5 --stages extract        # cheap, verify
ghost show observations --recent                # eyeball
ghost compose --limit 5 --stages extract        # next 5
# ... repeat ...
ghost compose --stages cluster,synthesize       # roll up
```

Other knobs:

- `--dry-run` — show what would be processed.
- `--estimate` — count input tokens across the selected stages and
  print a per-stage cost estimate using current config model IDs and
  published prices. Does not call the API. Runs in seconds; intended
  before any large backfill so cost surprises happen before, not
  after.
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
- `/ghost status` — ledger summary, including whether prompts have
  drifted since the last compose.
- `/ghost add-rule "<text>"` — append to `rules.user.md` (survives
  recompose).
- `/ghost forget <conv>` — drop a conversation's observations and
  ledger entry. Prints a warning that synthesis is now stale.
- `/ghost scan` — grep the observation corpus for secret/credential
  patterns (API keys, JWTs, `Authorization: Bearer`, etc.). On-demand
  safety net; does not mutate state.

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

## Phasing

The build is split into three vertical slices. Each phase ships
something independently useful — you can stop at the end of any
phase and still benefit. Risk is front-loaded: phase 1 validates
that the cheapest, riskiest assumption (extract quality on real
transcripts) holds before anything downstream is built on top of it.

### Phase 1 — Extract only (walking skeleton)

Goal: prove extract quality on the real transcript corpus.

In scope:
- Transcript glob, content hashing, ledger (single schema version,
  no refusal logic yet).
- Stage 1 extract with secret scrubbing and schema validation.
- `ghost compose --stages extract --limit N`, `ghost status`,
  `ghost forget`.
- Per-transcript observation files written atomically (tmp + rename
  per file — no multi-file transaction needed yet).
- `ghost show observations --recent` for eyeball verification.

Out of scope for phase 1: clustering, synthesis, skill, CLAUDE.md
includes, any always-loaded files.

Exit criteria: hand-review of ~20 transcripts' observations shows
the cheap model captures identity / rules / topics / voice signals
usefully and secret scrubbing drops credentials reliably.

### Phase 2 — Synthesis MVP (identity + rules)

Goal: deliver the always-loaded core. No lazy loading yet.

In scope:
- Stage 2 clustering (both passes; counts in Go).
- Stage 3 synthesis for `identity.md` and `rules.md` only.
- Atomic multi-file synthesis writes (tmpdir + directory rename).
- Two `@~/.ghost/...` includes wired into `~/.claude/CLAUDE.md`
  (identity + rules).
- `ghost compose` with `--stages cluster,synthesize` and `all`.

Out of scope for phase 2: topics, index, voice, skill, slash
commands beyond `add-rule` / `forget`, lazy loading of any kind.

Exit criteria: two weeks of normal Claude Code use shows identity
context calibrates responses correctly and synthesized rules
reflect feedback given across multiple projects.

### Phase 3 — Lazy loading

Goal: enable the three-layer runtime architecture.

In scope:
- Stage 3 synthesis for `topics/*.md` and capped `index.md`.
- `SKILL.md` with mechanical-check trigger logic.
- `rules.user.md` plus subtractive-synthesis precedence at compose
  time.
- `ghost compose --estimate`.
- Migration off `~/.claude/memory/` per the migration section.

Exit criteria: lazy-loaded topics fire on relevant tasks and
`~/.claude/memory/` can be archived without losing fidelity.

### Why this ordering

- Phase 1 risk is **extract quality**. Cheapest to validate, blocks
  everything downstream.
- Phase 2 risk is **synthesis quality + cross-project frequency
  signal**. Validated by living with the always-loaded outputs.
- Phase 3 risk is **runtime trigger discipline** — whether the skill
  actually loads the right topic at the right time. Only meaningful
  to test once topics exist and there's a corpus to synthesize from.

## Post-v1 tracking

Items deferred from v1 with a clear trigger for revisiting. Each
entry: what, why it's deferred, and what signal unblocks it.

| Item | Deferred because | Unblock signal |
|---|---|---|
| Enable voice synthesis (`[voice].enabled = true`) | Voice-context inference at extract time is the biggest correctness risk | `ghost eval` shows >90% correct voice-context labeling on a hand-labeled fixture set |
| `ghost eval` itself (judge-LLM synthesis quality check) | Only needed to gate voice; voice is off in v1 | Voice enablement is being considered, OR a synthesis regression ships unnoticed and post-mortem identifies eval as the missing safety net |
| Golden-transcript LLM-stage fixtures with similarity scoring | Pure-Go unit tests cover the deterministic logic; LLM-stage fixtures are maintenance until eval exists | Comes online with `ghost eval` |
| `/ghost scan` reactive secret scanner | Stage 1 scrubbing in extract makes the reactive scanner redundant for the v1 corpus | A secret pattern slips through extract scrubbing and lands in observations |
| `--since` and `--project` filters on `ghost compose` | `--limit` covers the backfill case; these are convenience filters | User asks for either by name during normal use |
| Prompts-version drift detection in `ghost status` | Always-rerun is fine while prompts are changing frequently | Prompts stabilize and rerunning becomes wasteful |
| Ledger `schema_version` refusal logic | Only one schema exists; field is recorded but no version-mismatch handling | Schema actually changes in a breaking way |
| `ghost config edit` | `$EDITOR ~/.ghost/config.toml` works | Config edits become frequent enough that a wrapper earns its keep |
| `/ghost topics` and `/ghost voice` slash commands | `ls ~/.ghost/topics/` works; v1 ships `/ghost show`, `/ghost status`, `/ghost add-rule`, `/ghost forget` only | User asks for either by name |
| `ghost compose --prune-missing` to drop ledger entries for deleted transcripts | Real but rare; manual `ghost forget` is sufficient for now | User reports a stale-ledger incident, or backlog of missing entries exceeds ~10% of ledger |
| Multi-machine sync of `~/.ghost/` (laptop ↔ desktop) | Out of scope; no good answer without conflict semantics | User actively uses ghost on two machines and reports drift |
| Per-topic load-frequency tracking at runtime to refine the top-20 cap | No evidence the fixed cap is wrong | A topic with high evidence count is observed to fire frequently but sit outside the cap, or vice versa |
| Voice-context detection heuristics beyond explicit framing ("help me draft my annual review") | Explicit framing handles the common case; aggressive inference risks contaminating registers | Eval shows the explicit-framing baseline misses >20% of legitimate ghostwriting tasks |
| Multi-dimension eval harness | Day-one harnesses become maintenance burden before they earn their keep | A regression slips past the simple judge-LLM eval AND would have been caught by a dimensional breakdown |

When an item is picked up, move it out of this table into the
appropriate section and update the doc's `status` field.

## Open questions

- Embedding model choice. Any small embedding model with ~1k-dim
  output and per-call cost under $0.001 is fine; the specific choice
  is tunable in `config.toml`. Note the threshold-coupling caveat in
  stage 2a: swapping models invalidates the embedding cache and may
  require re-tuning `cluster_cosine_threshold`.
- Exact secret-pattern set for stage 1 scrubbing. Starting set is
  listed in the stage 1 description; the full pattern list lives in
  code (`internal/extract/secrets.go`) and will accrete as new
  patterns surface in eval or `/ghost scan` reports.

## Summary

Ghost is a small Go CLI plus a Claude Code skill. The CLI does
out-of-band synthesis from Claude Code transcripts in three stages
(extract, cluster, synthesize), maintaining a hashed ledger so
compose is resumable and batchable. Clustering is two-pass:
deterministic embedding-based bucketing in Go, then a cheap LLM call
to pick canonical phrasing per bucket. Frequency counts are computed
in Go from cluster members — never from anything the LLM emits. The
skill enforces a three-layer runtime: a small, capped always-loaded
core (identity context, behavior rules, lookup index); a lazy
library of topic files for domain guidance; and a lazy library of
voice files, one per writing register, loaded only when
ghostwriting. Identity is context for Claude, not a template Claude
mimics. Voice is reference material for ghostwriting, not a directive
that affects Claude's normal responses. Cross-project frequency is
the signal that distinguishes "how Sarah works" from "how Sarah works
in one specific repo."
