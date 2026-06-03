# Chunk 3 decisions

Three load-bearing design choices made before writing the chunk 3
implementation plan. Each carries the alternatives considered, pros,
cons, and why this option won.

Companion to `docs/specs/2026-05-22-topics-by-embedding.md` (the
architectural spec) and `docs/specs/2026-05-22-findings.md` (the
chunking rationale).

## Decision 1: One smart call per cluster; slug derived from the H1

> **Revised 2026-05-22.** Original choice was B (two per-cluster
> calls: cheap-local slug + smart body). Reconsidered after noticing
> (1) the structural separation echoed muse's pipeline depth without
> muse's pipeline-depth justification — ghost's upstream cosine
> clustering already does the consolidation work that motivates
> multi-stage decomposition in muse; and (2) the local-Ollama naming
> call didn't actually reduce Claude token usage, since the body call
> is unchanged in either option — Ollama was additive, not
> substitutive. Body-with-H1, caller slugifies, is the simpler shape
> for ghost's actual product. Revised decision below.

> **Revised 2026-06-03.** The chunk-3 e2e falsified this decision's
> collision premise (Pro bullet 3 below: "slug collisions remain a clean
> signal ... the topic cosine threshold is wrong"). On `nomic-embed-text`
> a threshold sweep found *no* value that clears collisions — they live
> at the titling step, not the bucketing step (the smart model
> independently gives distinct-but-related clusters the same `# <Title>`).
> Behavior is now **collision → merge**: colliding clusters are combined
> and re-synthesized to a unique-slug fixpoint, never failed. The
> single-call-per-cluster, slug-from-H1 shape (the actual subject of this
> decision) is unchanged. See
> `docs/specs/2026-06-03-chunk-3-collision-merge-design.md`. Pro bullet 3
> below is retained as the original (now-overturned) rationale.

Single smart-model call per cluster. Model emits a body that begins
with `# <Title>`. Caller slugifies the title to produce the filename.

**Considered**

- **A — One combined call, structured envelope.** Model emits
  `{slug, body}` in a single smart-model call per cluster.
- **B — Two per-cluster calls.** Cheap (local) model names; smart
  model writes.
- **C — Body only; derive slug from the H1.** Single smart-model call
  per cluster; slugify the first heading. **Chosen.**
- **D — Muse-style global naming.** One smart-model call that sees
  every cluster's labels at once and emits a deduped canonical name
  set, then per-cluster body calls.

**Pros (C)**

- One model, one call, one prompt to maintain. No naming client, no
  naming prompt, no naming-backend config field, no per-cluster
  failure-mode for naming distinct from body.
- The H1 the reader sees and the filename they `@`-reference are the
  same concept at two grains. Coupling them is a feature, not a leak.
- Slug collisions remain a clean signal: two clusters producing
  identical (slugified) titles means the topic cosine threshold is
  wrong. Detection is a free byproduct of doing the body work.
- Slugifier is deterministic, ~20 lines, no model variance.
- No new dependency on a guessed local naming model.

**Cons (C)**

- Couples H1 phrasing to filename. If a future requirement wants a
  long human-readable title with a short filename slug, that lever is
  gone — would need to add an envelope (regress to A) or a second call
  (regress to B). Acceptable: not a current requirement, and the
  current cost of optionality is real (extra call, extra config,
  extra prompt, extra failure mode).
- Body prompt grows one rule ("start with `# <Title>` where the title
  is a clean noun phrase"). Mild.
- Body call's output now has to be parsed for the H1. Trivial — first
  line, regex.

**Why not the others**

- **vs A:** Asks the model to wrap free-form prose in a structured
  envelope (JSON or frontmatter). Introduces a new failure class —
  well-written prose discarded because the wrapper is malformed —
  which C does not have. The first-line-H1 convention is markdown's
  native shape, not an envelope.
- **vs B:** Structural decomposition only pays off if the cheap call
  is doing work the smart call would otherwise have to do, or if it
  substitutes for Claude work. Neither holds. Ghost's upstream cosine
  clustering has already settled "what concept is this?" by the time
  synthesis runs; the smart body call sees the same cluster regardless
  of whether a separate naming call happened first. Adds an Ollama
  client, a naming prompt, a backend-selection config, and a guessed
  local-model choice (`qwen2.5:3b`) for no Claude-token saving.
- **vs D:** Buys cross-cluster dedup that embedding clustering already
  provides. Muse needs global naming because it produces a single
  composite document; ghost produces per-file output, so the global
  view buys nothing and adds one large failure point.

## Decision 2: Split cosine threshold for identity/rule vs topics

Two config fields. Identity and rule keep the existing `0.85`. Topics
get a new field, default `0.75`.

**Considered**

- **A — Split into two fields.** Independent knobs.
  **Chosen.**
- **B — Single threshold, lowered for everyone.** Drop the one
  existing knob to ~0.78.
- **C — Single threshold, defer the split.** Keep 0.85 across the
  board; split later if topics come out fragmented.

**Pros (A)**

- Identity and rule want tight clusters — merging two distinct
  preferences destroys signal. Topics want loose clusters — merging
  `docs` and `documentation` is the whole point of chunk 3. The two
  populations have opposite requirements; one knob cannot serve both.
- Makes the asymmetry explicit in config rather than buried in code,
  so future tuning is a config edit, not a code change.
- Costs ~5 lines: one new config field, one branch in the bucketing
  helper.

**Cons (A)**

- One more config field for the user to understand. Mitigated by
  defaults that work without tuning.
- Two thresholds means two things that can drift out of sync if either
  is tuned in isolation. Low risk because they govern independent
  populations.

**Why not the others**

- **vs B:** Lowering the single threshold loosens identity and rule
  too. Distinct preferences would start merging, which is exactly the
  signal-destroying failure mode the tight threshold was preventing.
- **vs C:** Predictable outcome. The two populations have opposite
  requirements; deferring the split means the first chunk-3 run is
  guaranteed to be miscalibrated for at least one of them. Five lines
  of code now beats one corpus rebuild later.

## Decision 3: Remove the Topic field from the observation struct and extract prompt

Strip `Topic` from `internal/extract/Observation`. Remove
slug-shape rules and the `KNOWN TOPICS` injection from
`prompts/extract.system.md`. The cheap model no longer emits a
per-observation slug.

**Considered**

- **A — Remove from prompt and struct.** Clean cut. **Chosen.**
- **B — Keep field; drop validation and KNOWN TOPICS.** Model still
  emits a free-text hint; cluster ignores it.
- **C — Keep field as vestigial; strip prompt rules only.** Struct
  retains the field, model still produces it without constraints,
  nothing reads it.

**Pros (A)**

- Net deletion. The field, its validation, the KNOWN TOPICS plumbing
  in `cmd/compose.go`, the slug-shape rules in the extract prompt —
  all gone. No "what is this for" archaeology for future-Sarah.
- Forces alignment between the schema and the architecture. With the
  field present, a future change might accidentally start reading it
  again and reintroduce slug-as-key.
- Re-extract cost is unchanged from chunk 3's baseline. The extract
  prompt changes either way (KNOWN TOPICS and slug rules are going
  regardless), so the fingerprint mismatch — and the corpus rebuild —
  happens whether or not the field also goes. Removing it is free
  given the rebuild is already paid for.

**Cons (A)**

- Existing observation JSON on disk has a `topic` field that becomes
  ignored on unmarshal. Cosmetic; no parse failure. The files will be
  regenerated by the next compose run anyway.
- Loses the ability to `grep '"topic":` across observation files to
  find topic-kind entries. Same loss already applies to rule
  observations (they have no equivalent field). Use `"kind":"topic"`
  instead.

**Why not the others**

- **vs B:** The cheap model still does the work of inventing a slug,
  so the re-extract cost is identical to (A). Keeps vestigial code in
  the struct, the prompt, and downstream consumers indefinitely. Worst
  of both worlds on maintenance with no offsetting benefit.
- **vs C:** Strictly worse than B. Same re-extract cost, same
  vestigial code, plus the model produces unconstrained drift-prone
  output for a field nothing reads. No reason to pick this.
