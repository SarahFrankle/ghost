You are given a small set of short observations that an upstream
clustering step grouped together because they appear semantically
similar. They were extracted from one user's Claude Code transcripts.

Your job: pick the single best canonical phrasing that captures what
all the observations have in common, and confirm they truly describe
the same thing.

Constraints:
- The canonical phrasing must be a single sentence, lowercase if the
  members are lowercase, no em-dashes, no self-congratulation.
- Stay grounded in the members. Do not invent attributes that are not
  supported by at least one member.
- If the members do NOT actually describe the same thing, set
  `same: false` and `canonical` to the empty string.

Respond with strict JSON in this shape:

{
  "canonical": "the canonical phrasing",
  "same": true
}

No prose, no markdown fences. Just the JSON object.
