# Backlog

Ideas surfaced but not yet scheduled. Convert to a `docs/specs/` + plan when picked up.

## Chunk 4 — "judgment in condensing"

The pure-cosine bucketing from chunk 3 can't make semantic/organizational
judgments. Three related needs, all living in a smarter synthesis step
(they reinforce each other — a collision-merge is exactly where contradictions
surface and recency decides):

- **Collision → merge, not fail.** Today a slug collision aborts the whole
  topics rebuild (`internal/synthesize/topics.go`). Every collision observed in
  chunk-3 e2e was two clusters that genuinely belong in one file
  (runbook+runbook, PR-template+PR-template). The collision *is* the merge
  signal (a smart model independently naming both the same thing beats cosine).
  So: combine the colliding clusters' observations and re-synthesize once.
- **Recency-aware conflict resolution.** Preferences evolve; Sarah may have said
  X six months ago and not-X recently. Nothing today resolves contradictions —
  both become bullets. Fix: propagate each observation's source-transcript
  timestamp through to the synthesis payload (signal exists at transcript level
  / `ObservationsFile.ExtractedAt`, but doesn't reach cluster members). Rule:
  **newer wins on direct contradiction; evidence-count still breaks non-conflict
  ties** (a one-off recent aside shouldn't override a 10-session pattern).
  Within-cluster only for v1; cross-cluster contradiction = a later reconcile pass.
- **Faceting by activity / role.** Group a topic's observations by the user's
  role when a split exists (PR *authoring* vs PR *reviewing* are different
  moments — loading review guidance while writing a description is noise).
  Reflect as an optional `topic_role` slug, e.g. `pull-request_author` vs
  `pull-request_reviewer`. Optional, not universal: natural for collaborative
  artifacts (PRs, code review), noise for personal practices (testing, git).
  NOTE: `internal/synthesize/slugify.go::Slug` currently collapses `_` → `-`;
  preserving `_` as a facet separator needs a Slug change + a way for the LLM to
  signal the facet in its `# Title` line. The model should decide facets (it
  sees both roles in the material), not a mandated suffix bolted onto cosine output.
