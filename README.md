# ghost

Your Claude Code ghostwriter. Reads your conversation history and
distills it into a profile, rule set, and indexed library of topic
files that every Claude Code session loads automatically.

The point: feedback you gave Claude in one repo six months ago shapes
how Claude behaves in every repo today. No more re-explaining your
preferences each session.

> Status: design phase. This README describes the intended UX. See
> [`docs/specs/`](docs/specs/) for the design.

## Install

```bash
go install github.com/sfrankle/ghost@latest
```

Then add these lines to `~/.claude/CLAUDE.md`:

```markdown
@~/.ghost/profile.md
@~/.ghost/rules.md
@~/.ghost/rules.user.md
@~/.ghost/index.md
```

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
ghost show                 # print profile + rules
ghost topics               # list topic files
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
  profile.md           Voice and identity. Always loaded.
  rules.md             Synthesized do/don't rules. Always loaded.
  rules.user.md        Your manual rules. Survives recompose.
  index.md             Topic lookup table for lazy loading.
  topics/*.md          Deep guidance per topic. Loaded on demand.
  config.toml          Tunable thresholds and model selection.
  .state/              Ledger, observations, clusters. Don't hand-edit.
```

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
   `profile.md`, `rules.md`, `index.md`, `topics/*.md` from clusters.
4. **refine** — per output file, smart model. Orwell-style pass:
   delete sentences you wouldn't miss.

Observations are an immutable append-only log keyed by content hash.
The four files in your home directory are a regenerable materialized
view. You can re-run synthesis with a tweaked prompt without re-paying
for extraction.

A rule must show up in ≥2 conversations across ≥2 different projects
before it becomes global. Single-project guidance lives in
`topics/<name>.md` and loads only when you're working in that domain.

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
