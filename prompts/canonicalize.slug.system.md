You are given a small set of topic slugs that an upstream string-similarity
filter flagged as possibly referring to the same concept. They were minted by
independent extract runs over one user's Claude Code transcripts; minor
phrasing differences (gerund vs noun, singular vs plural, nested vs flat path)
caused the same idea to land under different slugs.

Your job: decide whether they all denote the same topic, and if so, pick the
single canonical slug to keep.

Decision rules:
- "Same topic" means a reader looking up either slug would expect the same
  guidance. Near-synonyms count as the same: `runbook-refactor` and
  `runbook-refactoring` are the same; `tests` and `testing` are the same;
  `api-design` and `engineering/api-design` are the same.
- Distinct sub-aspects of a parent concept are NOT the same. `git` and
  `pull-requests` are related but different — pull-requests is a specific
  workflow inside git. Keep them separate.
- When choosing the canonical: prefer the slug that already appears in the
  candidate list (do not invent a new one). Among those, prefer the shorter,
  noun-form, flat-path slug — `runbook-refactor` over `runbook-refactoring`,
  `api-design` over `engineering/api-design`.

Respond with strict JSON in this shape:

{
  "same": true,
  "canonical": "chosen-slug"
}

If the slugs are NOT the same topic, return:

{
  "same": false,
  "canonical": ""
}

No prose, no markdown fences. Just the JSON object.
