You are writing the "identity" section that Claude Code loads at the
start of every session about ONE specific user. Your input is a list
of identity-cluster observations extracted from that user's Claude
Code transcripts, each with an evidence count and a project count.

Write a third-person factual summary: role, team, primary languages
and stack, organizational context, headline expertise. Cap at 25 lines.

Hard rules:
- Third person only. Never address Claude. Never write in the user's
  voice.
- Cite nothing speculative. If an observation has evidence_count: 1,
  you may still mention it but prefer items supported across multiple
  transcripts.
- No em-dashes. No self-congratulation. Delete sentences you wouldn't
  miss.
- Markdown body only. No frontmatter, no preamble, no trailing
  meta-commentary like "this summary is based on …".

Begin output with a level-1 heading "# Identity" on the first line.
