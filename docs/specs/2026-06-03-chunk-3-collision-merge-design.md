# Chunk 3 completion: collision → merge

> **Status (2026-06-03):** design approved, pre-implementation. Completes
> chunk 3 (`docs/superpowers/plans/2026-05-22-chunk-3-embedding-topics.md`).
> Reverses chunk-3 Decision 1's "collision is a signal, fail loud" choice
> — see the Decision-1 revision in
> `docs/specs/2026-05-22-chunk-3-decisions.md`. Recency-aware conflict
> resolution and role faceting remain **chunk 4** (`BACKLOG.md`).

## Why this is chunk 3, not chunk 4

The chunk-3 design (`2026-05-22-chunk-3-design.md`, "Slug collision
handling") treats a slug collision as proof the topic cosine threshold
is wrong, and fails the whole topics rebuild so the user can tune the
threshold and re-run. Decision 1 (`2026-05-22-chunk-3-decisions.md:46`)
records the same premise: "two clusters producing identical slugified
titles means the topic cosine threshold is wrong."

The chunk-3 e2e falsified that premise. On `nomic-embed-text`:

- Topic-pair cosines compress into 0.65–0.74 (ceiling 0.7431), so the
  0.75 default never merges related clusters.
- A threshold sweep found **no** value that clears collisions: at 0.70
  four PR-description clusters collide on `pull-request-descriptions`;
  lowering to 0.68 clears that but surfaces a *new* collision on
  `pull-request-templates`; below 0.68 unrelated domains start blending
  (skill-design eating runbook specifics).

The collision does not live at the bucketing step. It lives at the
**titling** step: the smart model independently gives genuinely
distinct-but-related clusters the same `# <Title>`, which slugify
identically. No cosine threshold can fix a naming decision made
downstream of cosine.

Therefore fail-loud-on-collision is not a calibration aid — it is a
permanent wall that makes `ghost compose` unable to complete on the real
corpus. Removing it is the missing half of chunk-3 topic synthesis, not
new scope. Recency and faceting (the other two `BACKLOG.md` chunk-4
items) are genuinely new and stay deferred.

## Core insight

A slug collision is the **merge signal**, not an error. A smart model
independently naming two clusters the same thing is stronger evidence
they are one topic than cosine ever was. So a collision should *combine*
the colliding clusters and re-synthesize them as one topic, not abort.

This also dissolves the "what if the merged title still collides"
question: a second-order collision is simply another merge signal. We
keep merging until every slug is unique. There is no failure branch for
collisions at all.

## Scope

In scope (completes chunk 3):

- Replace fail-on-collision in `internal/synthesize/topics.go` with a
  merge-and-resynthesize loop that runs to a unique-slug fixpoint.
- Keep every other chunk-3 contract: all-or-nothing topics rebuild on
  any *synthesis* error, strict pipeline atomicity, slug derivation,
  evidence-ranked cap.

Explicitly out of scope (remains chunk 4, `BACKLOG.md`):

- Recency-aware conflict resolution (timestamp plumbing). When merged
  observations contradict, the model renders whatever it sees; both
  become bullets. No newer-wins rule in this chunk.
- Role/activity faceting (`topic_role`, `Slug` `_` handling).
- Any guard against over-merging. We trust the synthesis prompt's
  "specific noun phrase" title rule to keep distinct topics distinctly
  named. A bad merge, if it ever happens, surfaces as one incoherent
  topic file and is fixed via the prompt — not guarded against in code.

## Design

The merge logic follows the codebase's established pattern (`bucket.go`):
a pure function does the deterministic work; the caller supplies the
impure (LLM) part. This keeps the load-bearing logic — *what collides,
what merges into what* — testable without a live model.

### Components

**`mergeClusters(cs []cluster.Cluster) cluster.Cluster`** — pure,
deterministic. Combines a set of colliding clusters into one synthetic
cluster, mirroring how `Bucket` forms a cluster:

- `Members`: concatenation of all input members, in a deterministic
  order (sort by `ObservationHash`) so the same input always yields the
  same member order and the same re-synthesis payload.
- `EvidenceCount`: total member count (sum).
- `ProjectCount`: size of the union of member `Project` values.
- `Canonical`: the `Canonical` of the highest-evidence input cluster,
  ties broken by the lexicographically smallest `Canonical`, so the
  dominant cluster names the merged topic deterministically.
- `Kind`: `"topic"`; `SubKey`: `""`.

**The fixpoint loop** (in `topics.go`, parameterized over an injected
single-cluster synthesizer so tests drive it with a fake):

```
synthOne(cluster) -> (title, body, error)   // one LLM call, parallelizable

work := topicClusters
bodies := {}                                 // cluster index -> cached (title, body)
loop:
  synthesize every cluster in `work` that has no cached body, in parallel
  if any synthOne errored -> fail the whole topics rebuild (unchanged contract)
  slug every title; group cluster indices by slug
  if no group has size > 1:
    break                                    // fixpoint reached
  for each group with size > 1:
    merged := mergeClusters(group)
    replace the group's clusters in `work` with `merged`
    drop the group's cached bodies; mark `merged` as needing synthesis
    log: `topics: merged N clusters -> "<slug>"`
  // unchanged clusters keep their cached body; only merged ones re-synth
```

**`BuildTopics`** wires the real `client.Complete(...)` path in as
`synthOne` and returns the final unique-slug `[]TopicResult` /
`[]FileResult`, exactly as today. Its public signature is unchanged.

### Termination

Every loop iteration that finds a collision merges ≥2 clusters into 1,
so the working-set count strictly decreases. The loop terminates in ≤N
iterations (N = initial topic-cluster count); the degenerate floor is a
single all-absorbing topic. `mergeClusters` only combines, never splits,
so the count is monotonic — no oscillation is possible.

### Body caching

A cluster's body is synthesized once and cached by working-set identity.
Each iteration re-synthesizes only clusters that were freshly merged in
the previous iteration. The first iteration synthesizes all clusters; a
collision-free corpus therefore costs exactly one synthesis pass — no
regression versus today.

### Error handling (unchanged)

A `synthOne` error on any cluster still fails the entire topics rebuild.
Topics are all-or-nothing because `index.md` references the slug set;
partial success is not a useful state. The *only* removed behavior is
the slug-collision error path. Pipeline atomicity (`pipeline.go`:
tmpdir, strict replace, prior `topics/` preserved on failure) is
untouched.

### Interaction with rank + cap

A merged cluster's `EvidenceCount` is the sum of its parts, so it
naturally ranks higher in the pipeline's evidence-ranked cap
(`RankByEvidence` → `Cap`). No pipeline change is needed; merged topics
float up rather than getting dropped.

## Testing (TDD)

Drive the loop with a fake `synthOne` (no network, fully deterministic):

- **Distinct titles, no collision.** Two clusters, distinct titles → two
  files; `synthOne` called exactly twice (proves no spurious re-synth).
- **First-order merge.** Two clusters → same title → one merged file;
  evidence summed; merged member set is the union.
- **Second-order merge (fixpoint).** A+B merge and the merged cluster's
  re-synthesized title slugs to the same value as a third cluster C →
  all three fuse into one file. Proves the loop iterates past one round.
- **Caching.** In the first-order-merge case, the two unrelated
  bystander clusters are synthesized once each, not re-synthesized after
  the merge round.
- **Synth error fails the rebuild.** `synthOne` returns an error for one
  cluster → `BuildTopics` returns an error, no files.
- **Determinism.** Same input clusters → identical merged `Members`
  order, `Canonical`, and final slug set across repeated runs.
- **`mergeClusters` unit tests.** Member-order determinism, evidence
  sum, project union, canonical selection and tie-break.

Plus the real e2e: re-run `compose --stages cluster,synthesize` at the
default `cluster_cosine_topic = 0.75` and confirm it composes clean
(populated `~/.ghost/topics/`, index references the slugs, no collision
error), with the merge log lines showing the PR-family fusions.

## Files

### Modify

- `internal/synthesize/topics.go` — replace pass-2 collision check with
  the merge-to-fixpoint loop; add pure `mergeClusters`; factor the
  per-cluster synth+parse into an injectable `synthOne` so the loop is
  testable without the LLM. Public `BuildTopics` signature unchanged.
- `internal/synthesize/topics_test.go` — replace the collision-fails
  test with the merge/fixpoint/caching/determinism cases above; add
  `mergeClusters` unit tests. Keep the malformed-body and client-error
  tests (those failure modes are unchanged).
- `docs/specs/2026-05-22-chunk-3-decisions.md` — append a Decision-1
  revision recording that the e2e falsified the collision-as-signal
  premise and the chosen behavior is now collision → merge.
- `docs/superpowers/plans/2026-05-22-chunk-3-embedding-topics.md` —
  update the STATUS block: the threshold-tweak finish-line is dead;
  collision → merge is the finish-line.

### Untouched (deliberate)

- `internal/cluster/*` — bucketing is correct; the fix is entirely in
  synthesis.
- `internal/synthesize/pipeline.go` — atomicity, rank, cap all stand;
  merged clusters flow through unchanged.
- `internal/synthesize/slugify.go`, `index.go` — no change.
- `internal/config/config.go` — `cluster_cosine_topic` stays at the
  `0.75` default; it is now a granularity preference, not a
  correctness-critical knob.

## Open risks

- **Over-merge via a degenerate title.** Unconditional merge makes the
  synthesized title load-bearing: an over-generic title
  (`# Guidelines`) could silently fuse unrelated clusters. Accepted for
  this chunk (per scope). The synthesis prompt already mandates a
  specific noun-phrase title; the merge log makes any surprising fusion
  visible in output. If it becomes a real failure mode, the chunk-4
  faceting/guard work is where it gets addressed.
- **Cost of a merge round.** Each merge round adds one synthesis call
  per merged group. Bounded by the (small) topic-cluster count and far
  cheaper than the extract pass; not a concern at corpus scale.
