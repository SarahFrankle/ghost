# Topics by embedding (not slug)

Status: idea, deferred. Written 2026-05-22 after canonicalize was built
and started revealing the limits of slugs-as-bucketing-keys.

## The problem

Topics in ghost are bucketed by an exact-match string: the `topic` slug
the extract model picks per observation. That string is free-form. The
same concept gets minted under multiple slugs across transcripts:

- `runbook-refactor` vs `runbook-refactoring` (morphological variant)
- `docs` vs `documentation` (abbreviation)
- `engineering/api-design` vs `api-design` (nested vs flat)
- `decision-analysis` vs `decision-docs` (genuine synonyms)

Each variant becomes a separate `topics/*.md` file. The user's
~/.ghost/topics/ ends up with ~120 files where ~40 distinct concepts
exist.

We currently fight this with three layers:

1. Slug-shape rules in the extract prompt (noun form, no abbreviations,
   etc.).
2. A `KNOWN TOPICS` block injected into the extract prompt, listing
   existing topic files so the model is biased toward reusing slugs.
3. A `canonicalize` stage that proposes near-synonym groups (string
   heuristics + embedding similarity) and asks a cheap-model judge to
   merge them, persisted as `.state/slug_aliases.json` and applied at
   read time in cluster.

These layers work but each one is patching a symptom of a deeper
choice: the slug is treated as a category key when it's actually free
text picked by an LLM with single-transcript visibility.

## The idea

Stop using the slug as the bucketing key. Bucket topic observations by
content-embedding similarity, the way the within-kind clustering step
already works for identity and rule observations. Generate a slug at
synthesize time, from the clustered content, with the smart model.

```
extract:   observation gets kind=topic; slug becomes a free-form hint
           (or is dropped entirely)
cluster:   topic observations bucket by cosine similarity over .Text,
           not by SubKey equality
synthesize: each cluster gets named at write time via a smart-model
           call that sees the whole cluster; emits topics/<slug>.md
index:     references the synthesized slugs
```

Drift becomes architecturally impossible: two transcripts can use any
words they want for the same concept; if their observations are
semantically similar, they cluster together. The slug is a derived
label, not an authored key.

## Considered alternatives

### 1. Fixed taxonomy (rejected)

Pre-define the legal topic slugs; reject any others at extract time.
Drift becomes literally impossible.

Why rejected: requires up-front curation, locks the system into a
worldview, defeats the "let the data show what topics matter" property
that makes ghost interesting. Doesn't scale across users with
different domains.

### 2. Stricter slug grammar + GC (rejected)

Tighten the slug regex, reject abbreviations syntactically, add a GC
pass that prunes orphaned alias entries.

Why rejected: still patching the same root cause. The grammar can
prevent `docs` vs `documentation`, but not `decision-analysis` vs
`decision-docs` or any other genuine-synonym case. We'd build a fourth
drift-prevention layer on top of three existing ones.

### 3. Re-extract on every prompt change (rejected)

When the slug rules change, re-extract everything so old slugs get
re-minted under the new rules.

Why rejected: extract is the cheapest stage but still costs cheap-model
tokens times N transcripts. Doesn't help with genuine synonyms — the
model still mints whatever it picks, just under new rules. Doesn't
solve cross-transcript drift either (still single-transcript visibility
at extract time).

### 4. Keep canonicalize, build it further (rejected for the rewrite)

Add slug-content embedding caching, better fingerprints, an override
UI for the alias map, a "split" operation. Make the existing pipeline
more accurate.

Why rejected: each addition is another knob on a fundamentally
patch-shaped solution. The current canonicalize already has three
heuristics + an LLM judge + a hand-editable alias map. Adding more
makes it more capable but not more principled. If the goal is "no
drift, ever," the right move is to remove the surface that drifts —
the slug as a key — not to add more defense in depth around it.

### 5. RAG over observation corpus instead of static files (rejected)

Drop static `topics/*.md` files entirely. Index observations and let
Claude retrieve relevant ones at query time.

Why rejected: doesn't fit Claude Code's `@`-include model, which is
static and loaded at session start. Would require runtime retrieval
infrastructure ghost doesn't have. Different product.

## Why the embedding-cluster approach

It's the only option that makes drift structurally impossible without
constraining what topics can exist. Free text in, derived labels out.
The slug is generated last, by the smart model, with full cluster
visibility — exactly the conditions under which a model is good at
naming things. The cheap-model-extracting-a-string-per-transcript
setup is exactly the conditions under which a model is bad at naming
things.

The within-kind clustering step already uses cosine similarity for
identity and rule observations. Extending it to topics is a smaller
conceptual change than it sounds: we're removing the SubKey-equality
special case for `kind: topic`, not inventing new machinery.

## Pros

- **Drift becomes impossible.** Two transcripts can phrase the same
  preference any way they want and still cluster together.
- **Three drift-prevention layers collapse to zero.** No slug-shape
  rules in the extract prompt, no `KNOWN TOPICS` injection, no
  canonicalize package, no alias map, no aliases applied at read time.
- **Naming moves to the right place.** The smart model names topics
  with full visibility into the cluster, instead of the cheap model
  inventing a string from one transcript.
- **Aligns with the rest of ghost.** Cluster already uses embeddings
  for within-bucket grouping. This change makes topics consistent with
  identity and rule.
- **Smaller code.** Net deletion across the codebase.

## Cons

- **Real rewrite, not a patch.** Touches extract schema, cluster
  bucketing, synthesize, index generation. Probably a half-day of
  focused work plus a corpus rebuild.
- **Loses slug-based grep.** Currently you can `grep '"topic": "docs"'`
  across observation files. After: you grep by content. Same property
  already applies to `rule` observations, so not a new compromise, but
  it is a change.
- **Synthesize gets an extra smart-model call per topic** — one to
  name the cluster, in addition to the existing one to write the file.
  Could be folded into the existing call by asking the model to emit
  both slug and body in one response. Marginal cost.
- **Cluster threshold tuning becomes load-bearing for topics.** Today
  the cosine threshold only affects within-SubKey grouping; topics with
  different slugs never merge regardless. After the rewrite, the
  threshold decides whether two semantically-close observations land in
  the same topic. Setting it too low loses distinctions; too high
  reproduces drift. Will need empirical tuning on the actual corpus.
- **Re-cluster + re-synthesize required.** All existing
  `.state/clusters.json` and `~/.ghost/topics/*.md` get regenerated.
  Observations on disk stay valid — their `topic` field becomes
  vestigial (still recorded, no longer load-bearing).
- **First implementation may produce odd clusters.** Embedding-based
  bucketing on diverse topic observations could split a concept that a
  human would keep together (e.g., "git" and "pull-requests" might or
  might not cluster depending on the embedder). Iteration on threshold
  + sample fingerprint design is likely.

## Sketch of the change

Files affected:

- `internal/extract/schema.go` — `Topic` field becomes optional, no
  validation. Or removed and replaced with nothing (cluster doesn't
  need it).
- `prompts/extract.system.md` — strip slug-shape rules and
  `KNOWN TOPICS` block instruction. Topic-kind observations just need
  `kind: "topic"` plus text+evidence.
- `cmd/compose.go` — remove `KnownTopics` plumbing, remove the
  `canonicalize` stage, simplify `runExtract` and `runCluster`.
- `internal/cluster/bucket.go` — drop the topic SubKey special case;
  topic observations bucket by cosine similarity on `.Text`. May need
  a separate bucketing helper for topics since identity/rule keep
  their current behavior.
- `internal/cluster/pipeline.go` — remove `TopicAliases` field and
  the resolve-at-read shim.
- `internal/canonicalize/` — delete the package.
- `prompts/canonicalize.slug.system.md` — delete.
- `prompts/synthesize.topics.system.md` — update to emit `{slug, body}`
  given a cluster of observations. The model picks the slug.
- `internal/synthesize/topics.go` — input is now `[]cluster.Cluster`,
  not `map[string][]cluster.Cluster`. Each cluster becomes one topic
  file; the slug is parsed from the model output.
- `internal/synthesize/index.go` — already takes capped topics;
  signature stays similar, but the slug source changes.

State files to nuke before first run:

```bash
rm -rf ~/.ghost/.state/clusters.json
rm -rf ~/.ghost/.state/canonical_cache.json
rm -rf ~/.ghost/.state/slug_aliases.json
rm -rf ~/.ghost/topics/
```

Observations and embeddings stay valid (they're keyed by content, not
slug).

## Tuning knobs after the rewrite

- Topic cluster cosine threshold. Currently
  `thresholds.cluster_cosine_threshold = 0.85`. After the rewrite this
  governs how aggressively topics merge. Likely needs a separate
  threshold so identity/rule (which want tight clusters) and topics
  (which want looser conceptual groupings) can be tuned independently.
- Embedding model. The current Voyage / Ollama setup carries over; no
  change needed.
- Max topic count. Already capped via `index.max_topic_entries`. Stays.

## When to do this

Not yet. Conditions that would tip me toward doing it:

- Drift recurs after the current canonicalize work, and the temptation
  arises to add a fourth prevention layer. That's the signal that the
  patch approach has hit diminishing returns.
- A second user starts using ghost. Each user has a different topic
  vocabulary; the slug-shape rules and `KNOWN TOPICS` mechanisms don't
  generalize across users without per-user tuning.
- The user (Sarah) finds the slug-based mental model awkward enough
  to slow them down — e.g., wishing they could merge two topics
  without hand-editing `slug_aliases.json`.

Until one of those, the current solution is fine.
