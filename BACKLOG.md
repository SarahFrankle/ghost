# Backlog

Ideas surfaced but not yet scheduled. Convert to a `docs/superpowers/specs/`
spec + plan when picked up. (Specs and plans are local-only / gitignored — see
the project doc-location convention.)

## Recency-aware conflict resolution — DEFERRED (design settled 2026-06-03)

> **Status:** design fully worked out (2026-06-03 brainstorm) but **not built.**
> The composed corpus shows **zero contradictions** across 20 topic files /
> ~70 bullets — because Sarah has been using Claude at work under six months,
> not because contradictions won't happen. Building the "newer wins" prompt
> rule now would sit live over a clean corpus for months, where its only
> possible effect is the downside: wrongly judging two non-contradictory
> bullets as conflicting and dropping one. No upside until a real contradiction
> exists.
>
> **Build trigger:** the first time a composed topic file shows two real entries
> where the *older* one should have lost. Build against that concrete bug;
> ~1 hour given the settled design below.
>
> **Canonical motivating example (not yet in the corpus):** the 2026-06-03
> doc-location flip — old preference "specs in `docs/specs/`" vs new "use
> superpowers defaults, gitignored." Same topic, newer should win. Once that
> gets extracted + clustered, it is exactly the case recency resolves.

The problem: preferences evolve. Today nothing resolves contradictions within a
topic cluster — synthesis renders whatever it sees, so both can become bullets
(or the model silently picks a winner with no recency signal).

**Settled design (4 decisions, 2026-06-03):**

1. **Recency source = transcript time.** Plumb `source.Conversation.ModTime`
   (the transcript file's mod time — "when Sarah had the conversation") through
   `extract → ObservationsFile.SourceModTime → cluster.ClusterMember.SourceTime
   → synthesis payload`. NOT `ExtractedAt`: it collapses on a full re-extract,
   since every observation in one compose run shares ~one timestamp.
2. **Model-driven resolution, dated members.** Render each topic-cluster member
   with its date (`member N (YYYY-MM-DD, project=…): …`) and add one rule to
   `prompts/synthesize.topics.system.md`: on direct contradiction the newer date
   wins and the older is omitted; evidence count breaks ties only when there is
   no contradiction. Within-cluster only for v1 (cross-cluster reconcile stays
   deferred). Dated rendering is **topic-only** — `renderClusters` (identity /
   rules) stays byte-identical so those files don't churn or inherit a
   drop-risk rule. Zero-time members render `undated` and never win on recency.
3. **Backfill via extract namespace bump `extract/v1` → `extract/v2`.** Adding a
   struct field does NOT change `ObservationsFingerprint` (it hashes prompt +
   model + content hash, not the output schema), so existing files would stay
   undated. The bump forces a one-time full re-extract (cheap model,
   content-identical observations, now dated) → clusters rebuild → topics
   regenerate. "Rebuild paid once per chunk," as in chunk 3.
4. **No faceting, no Slug change.** Topic grouping concatenates all same-theme
   observations into one cluster, so `SourceTime` rides into the topic's
   synthesis payload for free — no clustering change needed.

> **Updated 2026-06-09:** decision 4 originally leaned on the collision→merge
> fixpoint as the point where contradictions surface and recency decides. That
> mechanism was **retired** in the topic-clustering redesign (label→theme→group;
> `mergeClusters` and the fixpoint are gone). The design still holds: the themed
> topic cluster is now where same-topic observations — and any contradictions —
> converge. `renderTopicPayload` is still the place to add dated rendering, but
> it now emits a `TITLE:`/`CLUSTER:` payload (the title is the themed label, not
> a model-invented H1).

Files (when built): `internal/extract/{schema,extract}.go`,
`internal/cluster/{types,pipeline}.go`, `internal/synthesize/topics.go`
(`renderTopicPayload` dated formatter), `prompts/synthesize.topics.system.md`.
Untouched: `slugify.go`, `config.go`, identity / rules.

## Voice — synthesized writing samples — DEFERRED (2026-06-04)

> **Status:** half-scaffolded, never wired end to end. Removed from the public
> README in chunk 5 so cold readers aren't sold a feature that produces nothing.

**The idea:** a fourth output kind alongside identity / rules / topics. Per
*register* (cli-chat, annual-review, slack, exec-brief), ghost would keep
reference samples of how Sarah actually writes, mirrored **only** when Claude is
ghostwriting on her behalf in that register, never bleeding into Claude's normal
responses.

**Current state in code (left in place, inert):**
- `internal/extract/schema.go` accepts `voice` as a valid observation `kind`
  and requires a `context` field on it (the register).
- `prompts/extract.system.md` and the synthesize prompts still mention `voice`.
- **Nothing downstream consumes it.** `synthesize` writes no `voice/*.md`; there
  is no `ghost voice` command. The dead `Voice.Enabled` /
  `voice_min_evidence_count` config knobs were removed in chunk 5 hygiene.

So extract *can* tag a voice observation today, but it dead-ends at clustering.

**Why deferred (not built, not ripped out):** matches the "test against real
data before building" stance. Voice only pays off once Sarah drafts in a fixed
register often enough that stored samples beat writing from scratch. Until then
it is speculative surface. Ripping out the extract scaffolding would force a
corpus re-extract (prompt-hash change) for no benefit, so the inert `kind` stays.

**Build trigger:** the first time Sarah wants Claude to draft in her voice for a
recurring register (most likely `cli-chat` or `slack`) and the absence of stored
samples is actually felt. Build against that register: synthesize `voice/<reg>.md`
from the already-tagged voice observations, add `ghost voice` to list them, and
re-document in the README.

## Faceting by activity / role — DECLINED (2026-06-03)

Considered grouping a topic's observations by the user's role (e.g.
`pull-request_author` vs `pull-request_reviewer`) via an optional `topic_role`
slug. **Declined:** too few topics split cleanly by role to justify the cost — a
`slugify.Slug` change to preserve `_` as a facet separator, an LLM protocol for
signaling the facet in the `# Title` line, and direct tension with
collision→merge (faceting deliberately *prevents* the very merges that feature
exists to perform). Reconsider only if a concrete, recurring role-split topic
appears that flat-topic output handles badly.

## Refactor to simplify
Do a full review of the whole repo / app. Look for places where we can use abstraction or shared classes.

Example: 
- when a compose stage is running, show clean in-place counter of progress logs (this should be standardised across all stages)
- abstract out / use shared logic for how this is implemented
- ensure logs all have the same format (with datetimestamps, for example)

> **Partly done 2026-06-09:** the in-place counter is now shared via
> `cmd.stderrCounter(label)` (TTY-only, quiet when piped), used by both the
> synthesize-topics and cluster-labeling stages. Still outstanding: the
> bounded-parallel fan-out is hand-rolled in three places
> (`synthesize.synthAll`, `cluster.labelAll`, `cluster.mapBatches`) and has
> begun to drift — extract one shared helper. Tracked in the chunk-2 review
> follow-ups in the topic-clustering design spec.

This repo so far should be considered a POC. DO NOT assume that because a pattern has been started that it is best practice. Claude works so quickly, if we're sure it's a better pattern, it's better to refactor. Ask a critic first if it's a good case for refactoring.

## Done / absorbed elsewhere

- **Collision → merge** (was the third "judgment in condensing" item): shipped
  in chunk 3 completion (`docs/superpowers/specs/2026-06-03-chunk-3-collision-merge-design.md`),
  then **retired 2026-06-09** by the topic-clustering redesign. Topics no longer
  cluster by cosine and no longer self-name via a model H1, so slug collisions
  can't arise from independent synthesis: grouping happens upstream by themed
  label, and two distinct labels that slugify the same now fail loud (a
  theme-prompt bug) rather than merging. `mergeClusters` + the fixpoint were
  deleted.

- **Topic clustering redesign — label→theme→group** (replaces cosine-for-topics):
  shipped 2026-06-09 across three commits on `topic-clustering-redesign`
  (`docs/superpowers/specs/2026-06-08-topic-clustering-redesign-design.md`).
  Topic observations skip embeddings; a cheap model labels each, a smart model
  consolidates labels into themes (two-pass identify→map), and observations
  group by exact theme. The theme names the topic (deterministic slug, no
  collision class). On the real corpus: 199 topic obs → 17 themes, 0 dropped.
