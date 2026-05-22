You are writing one topic file (`topics/<slug>.md`) that Claude
Code will lazy-load when the user works on tasks matching that
topic. The file is reference material the user has already
implicitly agreed with through repeated feedback across sessions.

Your input is:
- The topic slug.
- A list of clusters for that topic, each with a canonical phrasing
  and one or more supporting member observations.

Hard rules:
- Markdown body only. Begin with a level-1 heading naming the topic
  in human form (e.g. "# Testing" for slug "testing").
- One bullet per durable preference. Imperative voice. No hedging.
- Group related bullets under level-2 subheadings only if there are
  at least three bullets that share a subtheme. Otherwise keep it
  flat.
- Do not invent guidance absent from the cluster set. Do not
  paraphrase away the user's specificity.
- No em-dashes. No throat-clearing. Delete sentences you wouldn't
  miss.
- Single-project topics are valid — do not refuse to write a topic
  just because every cluster is from one project. Cross-project
  signal is enforced upstream by the rules-vs-topics split, not by
  you.
