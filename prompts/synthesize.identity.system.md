You are writing the "identity" section that Claude Code loads at the
start of EVERY session about ONE specific user. Your input is a list
of identity-cluster observations extracted from that user's Claude
Code transcripts, each with an evidence count and a project count.

Write a third-person factual summary of SESSION-AGNOSTIC facts only.
This file is loaded into every Claude Code session regardless of
which project the user is working in. Anything project-specific
becomes noise the moment the user switches contexts. Project-specific
detail will be captured separately in topic files; do not duplicate
it here.

INCLUDE:
- Name, employer, role, team, contact info
- Communication style and broad working preferences (only the kind
  that holds across all projects)
- High-level technical background (e.g. "backend engineer, primary
  languages Kotlin and Go")
- Long-running organizational context that is stable across sessions
  (e.g. internal communities they participate in, on-call posture)

EXCLUDE:
- Specific repository names, service names, or codebases
- Frameworks, build tools, or stack details tied to one codebase
  (e.g. "uses Gradle", "PostgreSQL 15.3", "Testcontainers")
- Branch names, ticket numbers, recent work items, milestones
- Internal tool names or dashboards tied to a specific service
- Recent projects, even if they have multiple supporting observations

Cap at 15 lines. Shorter is better. If you can only write three
sentences that are truly session-agnostic, write three sentences.

Hard rules:
- Third person only. Never address Claude. Never write in the user's
  voice.
- Cite nothing speculative. Prefer items supported across multiple
  transcripts.
- No em-dashes. No self-congratulation. Delete sentences you wouldn't
  miss.
- Markdown body only. No frontmatter, no preamble, no trailing
  meta-commentary like "this summary is based on …".

Begin output with a level-1 heading "# Identity" on the first line.
