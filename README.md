# ghost

Your Claude Code ghostwriter. Ghost reads your Claude Code
conversation history and distills it into always-loaded context, so
feedback you gave Claude in one repo months ago shapes how Claude
behaves in every repo today. No more re-explaining your preferences
each session.

It produces three kinds of output:

- **identity** — who you are, what you work on (context for Claude)
- **rules** — how Claude should behave when collaborating with you
- **topics** — deeper domain guidance, loaded on demand when the
  task matches

## Install

```bash
go install github.com/SarahFrankle/ghost@latest
```

Ghost shells out to the `claude` CLI for LLM calls — make sure it's
installed and logged in (`claude --version`). It also needs a local
[Ollama](https://ollama.com) for embeddings by default; see
[Requirements](#requirements).

Add these lines to `~/.claude/CLAUDE.md` so the core files load every
session:

```markdown
@~/.ghost/identity.md
@~/.ghost/rules.md
@~/.ghost/rules.user.md
@~/.ghost/index.md
```

Topic files are NOT `@`-included — they load on demand. Install the
ghost skill so Claude reads `index.md` and lazy-loads the matching
topic when your task triggers it:

```bash
ghost install-skill   # writes ~/.claude/skills/ghost/SKILL.md
```

## First run

`ghost compose` runs the full pipeline end-to-end. On a large
backlog you'll want to verify quality on a small batch first;
`--limit` bounds the extract stage so you can do that in one command:

```bash
# See what ghost will read
ghost status

# Process 5 transcripts end-to-end so you can verify quality
ghost compose --limit 5

# Look at what it produced
ghost show core
```

If you like what you see, drain the rest of your backlog in one shot:

```bash
ghost compose
```

## Working in batches

The pipeline splits into per-conversation work (`extract`) and
whole-corpus work (`cluster`, `synthesize`). Each stage is also a
standalone command, so you can run them independently:

```bash
# Cheap, fast — pull observations 10 transcripts at a time
ghost extract --limit 10
ghost extract --limit 10
# ... repeat until `ghost status` shows zero pending ...

# Then roll everything up into identity + rules + topics
ghost cluster
ghost synthesize
```

### Cadence: how often to run each stage

The three stages have different cost profiles and different "is it
worth re-running?" answers. A rough guide:

| Stage | Run how often | Why |
|---|---|---|
| `extract` | Often. Daily or after any session worth mining. | Cheap model, per-transcript, idempotent (the ledger skips unchanged transcripts), bounded by `--limit`. Running it more often means smaller batches and faster feedback. |
| `cluster` | Periodically. When extract has produced a meaningful chunk of new observations (a few dozen, or weekly). | Whole-corpus embedding + similarity clustering. Cheap per call but does work over everything; running it every five minutes is wasteful. |
| `synthesize` | Sparingly. When you actually want your `~/.ghost/` files refreshed — weekly, or after a cluster run you care about. | Smart model. This is the expensive step. The output is a regenerable view, so there's no cost to *delaying* a run, only to running it before the corpus has shifted enough to matter. |

Patterns that work:

- **Steady-state.** `ghost extract` daily (or wire it into a
  launchd/cron job). Run `ghost cluster && ghost synthesize` weekly.
  Your `~/.ghost/` files lag your conversations by up to a week, which
  is fine — these are durable preferences, not a feed.
- **Catching up after a gap.** `ghost extract --limit 20` repeatedly
  until `ghost status` is clear, then one `ghost cluster && ghost
  synthesize` at the end. Don't synthesize after every extract batch —
  you'll burn smart-model tokens regenerating the same files.
- **Iterating on a prompt.** Edit a prompt under `prompts/`, then
  rerun the affected stage. Fingerprinting (below) recomputes only
  what the prompt change touched; the rest is served from cache.

It is always safe to run any stage more often than recommended — the
ledger, content hashes, and artifact fingerprints keep wasted work
bounded. Running stages *less* often is also safe; the materialized
view just gets staler.

### Fingerprinting: no more deleting state to re-test

Every derived artifact (observations, clusters, synthesized files)
carries a fingerprint over its inputs, the prompt version, and the
model id. On each run, a stage compares fingerprints and recomputes
only what changed. Editing an extract prompt re-extracts; editing a
synthesize prompt only re-synthesizes. You rarely need to touch
`.state/`.

When you *do* want to force a stage to recompute regardless of
fingerprint (for example, while iterating on a prompt), each stage
has an override flag:

| Flag | On | Effect |
|---|---|---|
| `--limit N` | `extract`, `compose` | Process at most N unprocessed transcripts in the extract stage |
| `--dry-run` | `extract` | List what would be processed, then exit |
| `--reobserve` | `extract` | Force re-extract of all transcripts, ignoring the cache |
| `--recluster` | `cluster` | Force rebuild of clusters, ignoring the cache |
| `--resynth` | `synthesize` | Force re-synthesis of all outputs, ignoring the cache |
| `--estimate` | `compose` | Print a per-stage token + cost estimate and exit |
| `--config PATH` | any | Use a config file other than `~/.ghost/config.toml` |

## Day-to-day commands

```bash
ghost show core            # print identity.md, rules.md, rules.user.md
ghost show topics          # list topic files with last-modified
ghost show observations    # print recent extracted observations (--recent N)
ghost status               # ledger summary + last compose
ghost add-rule "<text>"    # pin a manual rule (survives recompose)
ghost forget <transcript>  # drop a conversation's observations + ledger entry
ghost install-skill        # (re)write the lazy-loading skill
```

## What lives where

```
~/.ghost/
  identity.md          Who you are, what you work on. Context for Claude. Always loaded.
  rules.md             Synthesized do/don't rules for how Claude works with you. Always loaded.
  rules.user.md        Your manual rules (ghost add-rule). Survives recompose. Always loaded.
  index.md             Lookup table — triggers that map tasks to topic files. Always loaded.
  topics/*.md          Deep guidance per domain. Loaded on demand by topic trigger.
  config.toml          Tunable thresholds and model selection.
  .state/              Ledger, observations, clusters, embeddings. Don't hand-edit.
```

### Identity vs. rules

These two layers do different jobs, and the distinction matters:

- **Identity** is context *for* Claude. "Sarah works in Kotlin on
  backend services at Miro" helps Claude calibrate its answers. It
  does NOT narrow Claude to backend-only or restrict its expertise.
- **Rules** are instructions *to* Claude. "Break comments at end of
  thought, not mid-sentence" governs Claude's output regardless of
  what's being written.

A rule must show up in at least 2 conversations across at least 2
different projects before it becomes a global rule. Single-project
guidance lives in `topics/<name>.md` and loads only when you're
working in that domain.

### Example topic file

Topics are the on-demand layer. `index.md` carries the triggers; the
topic file carries the guidance. A `~/.ghost/topics/pull-requests.md`
might look like:

```markdown
# Pull Requests

- Lead the description with a "why" section before the "what".
- Keep each PR to one logical change; split unrelated edits.
- Fill every template section or delete it; never leave it empty.
```

with a matching line in `index.md`:

```markdown
## Topics
- topics/pull-requests.md (triggers: pull-requests, pr description, pr scope, pr template)
```

Claude consults `index.md` at the start of a task; if a trigger
matches, it loads that one topic file and nothing else.

## How it works

Three-stage pipeline:

1. **extract** — per transcript, cheap model. Pulls atomic
   observations with evidence citations.
2. **cluster** — corpus-level. Embeds observations and groups them by
   cosine similarity, dedups near-duplicates, and merges evidence.
   Identity/rule observations use a tight similarity threshold
   (near-duplicate merging only); topics use a looser one so related
   preferences ("docs should lead with examples" / "example-first
   documentation") land in one cluster.
3. **synthesize** — corpus-level, smart model. Writes
   `identity.md`, `rules.md`, `index.md`, and `topics/*.md` from the
   clusters. Rules are filtered to those appearing in at least 2
   conversations across at least 2 projects.

Observations are an immutable, append-only log keyed by content hash.
The files in `~/.ghost/` are a regenerable materialized view. You can
re-run synthesis with a tweaked prompt without re-paying for
extraction.

## Updating the model

When Anthropic ships a new model, edit `~/.ghost/config.toml`:

```toml
[models]
cheap = "claude-haiku-4-5-20251001"
smart = "claude-opus-4-7"
```

No code change required. Stages reference roles (`cheap` / `smart`),
not specific model IDs. Ghost passes the resolved ID through to
`claude -p --model <id>`.

## Configuration

Edit `~/.ghost/config.toml`. Frequently tuned knobs:

- `models.cheap` / `models.smart` — model IDs for the extract vs
  synthesize stages.
- `models.embedding` — Voyage embedding model, used only when
  `VOYAGE_API_KEY` is set (otherwise Ollama is used).
- `thresholds.rule_min_evidence_count` — how many times a rule must
  appear before it can be global. Default 2.
- `thresholds.rule_min_project_count` — how many different projects.
  Default 2.
- `thresholds.cluster_cosine_identity_rule` — similarity threshold
  for bucketing identity/rule observations. Default 0.85 (tight).
- `thresholds.cluster_cosine_topic` — similarity threshold for
  bucketing topic observations. Default 0.75 (looser).
- `index.max_topic_entries` — cap on topics listed in `index.md`.
  Default 20.
- `batching.extract_workers` — concurrent extract calls. Default 5.

## Privacy

Ghost is local-first by design:

- LLM calls shell out to your authenticated `claude` CLI, reusing
  your existing Claude Code subscription. No API key, no separate
  account.
- Your transcripts and all derived state (`.state/`, `~/.ghost/`)
  stay on your machine.
- Embeddings run locally through Ollama by default. The Voyage
  embedding backend is opt-in and only active if you set
  `VOYAGE_API_KEY`.
- Nothing is uploaded, synced, or shared between users.

## Requirements

- Go 1.25+
- The `claude` CLI installed and logged in (ghost shells out to it,
  reusing your Claude Code subscription)
- An embedding backend for the `cluster` stage: a local
  [Ollama](https://ollama.com) (default; pull a model such as
  `nomic-embed-text`), or a `VOYAGE_API_KEY` to use Voyage instead.
  Without one, `cluster` fails with a connection error.
- Claude Code used enough to have transcript history under
  `~/.claude/projects/`

## Limitations

- Claude Code transcripts only. Other agents and sources (Slack,
  GitHub, Codex) are not ingested.
- Global, not per-project. Ghost complements project-local
  `CLAUDE.md` files; it does not replace them.
- Durable preferences, not a feed. Outputs lag your conversations by
  however often you run `synthesize`.

## Not goals

- Multi-source ingestion (Slack, GitHub, Codex, other agents)
- Hosted deployment, S3 sync, sharing between users
- MCP server mode, PDF export
- Replacing project-local `CLAUDE.md` files (ghost is global only)
