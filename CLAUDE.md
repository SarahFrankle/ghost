# ghost — project rules

## Build & checks

- To build, run `make build` (never bare `go build`). `make build` runs
  the full lint pass first, so compiling and linting always happen
  together.
- Before claiming work is done or committing, run `make check` and
  confirm it passes. `make check` auto-formats (`gofmt -w`) and then runs
  lint + build + test. The pre-commit hook runs the same `make check`, so
  a commit that skips it will be blocked anyway.
- Keep the tree clean: `gofmt`, `go vet`, `staticcheck`, and `modernize`
  must all be silent. Fix findings rather than suppressing them. Tool
  versions are pinned in the `Makefile` — bump them deliberately.

## Layout

`main.go` → `cmd/` (cobra commands) → `internal/*`. Nothing under
`internal/` imports `cmd/`; the dependency graph is strictly downward.

The pipeline is three stages, run by `ghost compose` (or each stage
standalone):

- `internal/source`, `internal/transcript` — discover and read Claude
  Code `.jsonl` transcripts.
- `internal/extract` — one `claude -p` call per transcript → atomic
  observation JSON. Owns the `Kind`/`Confidence` types and the schema.
- `internal/cluster` — groups observations. identity/voice embed
  (`internal/embedding`) and cosine-bucket; `preference` skips
  embeddings and runs label→theme→group. The largest package.
- `internal/synthesize` — routes preferences general-vs-scoped, gates by
  confidence, renders `identity.md`/`rules.md`/`topics/*.md`/`index.md`.
- Support: `internal/ledger` (per-transcript processing state),
  `internal/fingerprint` + per-stage fingerprints (idempotent skip),
  `internal/atomicfs` (temp+rename writes), `internal/anthropic` (the
  `claude -p` subprocess client), `internal/pricing`, `internal/config`,
  `internal/paths`, `internal/secrets`. `prompts/` holds the LLM system
  prompts (embedded, hashed into fingerprints); `skill/` is the
  lazy-loaded Claude Code skill.

## Gotchas

- LLM access is only ever shelling out to the `claude` CLI
  (`internal/anthropic`), never an API key. The subprocess flags there
  are load-bearing — read the comments before changing them.
- Editing a `prompts/*.md` file changes its hash and re-triggers the
  stages that depend on it. That is intended, not a bug.

## Design record

Specs live in `docs/superpowers/specs/`, plans in `…/plans/` (both
gitignored, local-only). `BACKLOG.md` is the authoritative deferred-work
log with explicit build triggers, and records retired mechanisms.
Treat the current design as a validated POC: prefer following existing
patterns over assuming they are best practice.
