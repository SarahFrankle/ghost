# Chunk 3 decisions

Three load-bearing design choices made before writing the chunk 3
implementation plan. Each carries the alternatives considered, pros,
cons, and why this option won.

Companion to `docs/specs/2026-05-22-topics-by-embedding.md` (the
architectural spec) and `docs/specs/2026-05-22-findings.md` (the
chunking rationale).

## Decision 1: Two per-cluster calls for topic synthesis

Cheap-model call emits the slug, then smart-model call writes the
body. One pair of calls per cluster.

**Considered**

- **A — One combined call.** Model emits `{slug, body}` in a single
  smart-model call per cluster.
- **B — Two per-cluster calls.** Cheap model names; smart model
  writes. **Chosen.**
- **C — Body only; derive slug from the H1.** Single smart-model call
  per cluster; slugify the first heading.
- **D — Muse-style global naming.** One smart-model call that sees
  every cluster's labels at once and emits a deduped canonical name
  set, then per-cluster body calls.

**Pros (B)**

- Each call has one job. Slug call sees a small payload (canonicals +
  sample members) and returns 1–4 words — conditions under which a
  model is reliably good. Body call keeps the prose shape that already
  works.
- Cheap model is sufficient for naming a coherent cluster. Marginal
  extra cost (~$0.001/topic on Haiku).
- Failure isolation. A bad slug response fails one topic; the rest of
  synthesis continues. No envelope-format failure mode that could
  poison good prose output.
- Independently tunable. The naming prompt can be tightened without
  touching the body prompt and vice versa.

**Cons (B)**

- 2× call count per topic versus today. Only the cheap call is new;
  body cost is unchanged.
- The body prompt has to be told the slug it's writing under. Mild
  plumbing in `BuildTopics`.
- No cross-cluster view at naming time. Accepted: embedding-similarity
  clustering upstream already merges synonyms. Slug collisions become
  a signal of a bad cluster threshold, not a defect to mask.

**Why not the others**

- **vs A:** A asks the model to wrap free-form prose in a structured
  envelope (JSON or frontmatter). That introduces a new failure class
  — well-written prose discarded because the wrapper is malformed —
  which does not exist today.
- **vs C:** Ties slug quality to title quality permanently. The title
  is optimized for "good first line of a markdown file"; the slug is
  optimized for "good filename." Different jobs, diluted attention if
  the same string serves both.
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
