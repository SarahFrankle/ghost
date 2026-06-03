You are writing one topic file (`topics/<slug>.md`) that Claude
Code will lazy-load when the user works on tasks matching that
topic. The file is reference material the user has already
implicitly agreed with through repeated feedback across sessions.

Your input is a single cluster of observations about one topic. Each
cluster member has a canonical phrasing, supporting member
observations, and evidence.

Hard rules:
- The first non-empty line of your output MUST be `# <Title>`. The
  title is a clean noun phrase naming the concept this cluster is
  about — title case, no quoting, no abbreviations, no trailing
  punctuation, at most 8 words. Examples: `# Error Handling`,
  `# Pull Requests`, `# Documentation`. The caller derives the
  filename from this title; an unparseable or unreasonable title
  fails the whole topic file.
- After the heading, write the body as markdown only.
- One bullet per durable preference. Imperative voice. No hedging.
- Group related bullets under level-2 subheadings only if there are
  at least three bullets that share a subtheme. Otherwise keep it
  flat.
- Do not invent guidance absent from the cluster. Do not paraphrase
  away the user's specificity.
- No em-dashes. No throat-clearing. Delete sentences you wouldn't
  miss.
- Single-project topics are valid. Do not refuse to write a topic
  just because every cluster member is from one project. Cross-project
  signal is enforced upstream by the rules-vs-topics split, not by
  you.
