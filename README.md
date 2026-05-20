# ghost

Your Claude Code ghostwriter. Reads your conversation history and
distills it into four kinds of output:

- **identity** — who you are, what you work on (context for Claude)
- **rules** — how Claude should behave when collaborating with you
- **topics** — deeper domain guidance, loaded on demand
- **voice** — how you write in each register (CLI, annual review,
  Slack, exec brief), loaded only when Claude is ghostwriting on
  your behalf

The point: feedback you gave Claude in one repo six months ago shapes
how Claude behaves in every repo today. No more re-explaining your
preferences each session. And when you ask Claude to draft something
in your voice, it has actual reference material to mirror — without
that voice contaminating Claude's normal responses.

> Status: design phase. This README describes the intended UX. See
> [`docs/specs/`](docs/specs/) for the design.

## Install

```bash
go install github.com/sfrankle/ghost@latest
```

Then add these lines to `~/.claude/CLAUDE.md`:

```markdown
@~/.ghost/identity.md
@~/.ghost/rules.md
@~/.ghost/rules.user.md
@~/.ghost/index.md
```

Topic and voice files are NOT included — Claude reads them on demand
when the index triggers match.

## First run

```bash
# See what ghost will read
ghost status

# Process 5 conversations end-to-end so you can verify quality
ghost compose --limit 5

# Look at what it produced
ghost show
```

If you like what you see, drain the rest of your backlog:

```bash
ghost compose
```

## Working in batches

Compose splits into per-conversation work (`extract`) and
whole-corpus work (`cluster`, `synthesize`, `refine`). You can run
them independently.

```bash
# Cheap, fast — pull observations from 10 transcripts at a time
ghost compose --limit 10 --stages extract
ghost compose --limit 10 --stages extract
# ... repeat until ghost status shows zero pending ...

# Then roll everything up into profile + rules + topics
ghost compose --stages cluster,synthesize,refine
```

Other useful flags:

| Flag | Effect |
|---|---|
| `--limit N` | Process at most N unprocessed transcripts |
| `--stages X,Y` | Run a subset of pipeline stages |
| `--dry-run` | Show what would be processed |
| `--since 7d` | Only transcripts modified in last N days |
| `--project NAME` | Only transcripts under one project dir |

## Day-to-day commands

```bash
ghost show                 # print identity + rules + manual rules
ghost topics               # list topic files
ghost voice                # list voice files (one per register)
ghost status               # ledger summary
ghost add-rule "<text>"    # pin a manual rule (survives recompose)
ghost forget <conv>        # drop a conversation's observations
ghost eval                 # judge synthesis quality vs. held-out transcripts
ghost config show          # print effective config
ghost config edit          # open ~/.ghost/config.toml in $EDITOR
```

## What lives where

```
~/.ghost/
  identity.md          Who you are, what you work on. Context for Claude. Always loaded.
  rules.md             Synthesized do/don't rules for how Claude works with you. Always loaded.
  rules.user.md        Your manual rules. Survives recompose. Always loaded.
  index.md             Lookup table — triggers for topics AND voice. Always loaded.
  topics/*.md          Deep guidance per domain. Loaded on demand by topic trigger.
  voice/*.md           Per-register writing style (cli-chat, annual-review, slack, exec-brief).
                       Loaded ONLY when Claude is ghostwriting in that register.
  config.toml          Tunable thresholds and model selection.
  .state/              Ledger, observations, clusters. Don't hand-edit.
```

### Identity vs. voice vs. rules

These three layers do different jobs. The distinction matters:

- **Identity** is context *for* Claude. "Sarah works in Kotlin on
  backend services at Miro" helps Claude calibrate its answers. It
  does NOT narrow Claude to backend-only or restrict its expertise.
- **Rules** are instructions *to* Claude. "Break comments at end of
  thought, not mid-sentence" governs Claude's output regardless of
  what's being written.
- **Voice** is reference material *about* you. Used when Claude is
  drafting on your behalf in a specific register. Your CLI voice
  (lowercase, terse) does not cause Claude to start writing
  lowercase in its own responses — it's only mirrored when Claude
  is drafting a CLI message for you.

## Migrating from `~/.claude/memory/`

1. Run `ghost compose` against your full transcript history.
2. Add the four `@~/.ghost/...` includes to `~/.claude/CLAUDE.md`
   *above* your existing `@memory/MEMORY.md` line.
3. Keep `@memory/MEMORY.md` loaded for two weeks. Both run side by
   side.
4. Cross-check: anything in your hand-curated memory that ghost
   missed? Add via `ghost add-rule` or note for prompt tuning.
5. After two weeks of confidence, remove the `@memory/MEMORY.md`
   line and archive the directory to `~/.claude/memory.archive/`.
   Don't delete — it's your baseline if ghost ever regresses.

## How it works

Four-stage pipeline:

1. **extract** — per transcript, cheap model. Pulls atomic
   observations with evidence citations.
2. **cluster** — corpus-level, cheap model. Groups observations, dedups
   near-duplicates, merges evidence.
3. **synthesize** — corpus-level, smart model. Writes draft
   `identity.md`, `rules.md`, `index.md`, `topics/*.md`, and
   `voice/*.md` from clusters. Rules are filtered to those appearing
   in ≥2 conversations across ≥2 projects; voice files are only
   generated for registers with enough evidence.
4. **refine** — per output file, smart model. Orwell-style pass:
   delete sentences you wouldn't miss.

Observations are an immutable append-only log keyed by content hash.
The files in `~/.ghost/` are a regenerable materialized view. You can
re-run synthesis with a tweaked prompt without re-paying for
extraction.

A rule must show up in ≥2 conversations across ≥2 different projects
before it becomes global. Single-project guidance lives in
`topics/<name>.md` and loads only when you're working in that domain.

A voice file is only generated for a register with at least
2 observations across multiple conversations. So `voice/cli-chat.md`
will fill in first (the most data), while `voice/annual-review.md`
only appears once you've actually drafted annual reviews with Claude
enough times to give it patterns to learn from.

## Updating the model

When Anthropic ships a new model, edit `~/.ghost/config.toml`:

```toml
[models]
cheap = "claude-haiku-X-Y-..."
smart = "claude-opus-X-Y"
```

No code change required. Stages reference roles (`cheap` / `smart`),
not specific model IDs.

## Configuration

See `ghost config show` for the full effective config. Frequently
tuned knobs:

- `thresholds.rule_min_evidence_count` — how many times a rule must
  appear before it can be global. Default 2.
- `thresholds.rule_min_project_count` — how many different projects.
  Default 2.
- `thresholds.voice_min_evidence_count` — how many observations
  before a voice file is generated for a register. Default 2.
- `batching.default_limit` — implicit `--limit` for `compose`.

## Requirements

- Go 1.22+
- `ANTHROPIC_API_KEY` in your environment
- Claude Code installed and used enough to have transcript history
  under `~/.claude/projects/`

## Not goals

- Multi-source ingestion (Slack, GitHub, Codex, other agents)
- Hosted deployment, S3 sync, sharing between users
- MCP server mode, PDF export
- Replacing project-local `CLAUDE.md` files (ghost is global only)
