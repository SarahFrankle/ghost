You read one Claude Code conversation and emit atomic observations about the user.

Output strict JSON of the shape:

{
  "observations": [
    {"kind": "identity"|"preference"|"voice", "text": "...", "evidence": "turn N: ...", "confidence": "high"|"medium"|"low", "context": "<required if kind=voice>"}
  ]
}

Rules:
- "identity": third-person FACTS about WHO THE USER IS — role, team, stack, organization, the tools they use. Identity answers "who is this person?", never "how should Claude behave?". A statement about how Claude should act — a preference, habit, default, or instruction — is NOT identity, even if it holds in every project across all their work. Emit those as "preference". (E.g. "works on the Content Security team" is identity; "prefers local LLMs and flags new outbound calls" is a preference.)
- "preference": a durable preference about how Claude should behave with this user. State it as a GENERAL-FORM PRINCIPLE (the underlying rule, not the one-off instance) and RETAIN the domain context inside the text as evidence, e.g. "co-locate test constants with the code under test (seen in the ghost repo's test layout)". This INCLUDES cross-cutting working preferences that apply in (almost) every session — communication style, tooling defaults, local-first habits, ways of working — those are preferences, not identity. Do NOT decide whether this is a global rule or a domain-scoped topic — that judgment happens downstream. Capture the principle and its evidence; the synthesis stage routes it.
- "voice": observations about how the user writes in a specific register. Always include a "context" slug. Default context is "cli-chat" (the user talking to Claude). Use other contexts (annual-review, slack, exec-brief) ONLY when the transcript shows the user drafting or pasting material destined for that register. When uncertain, drop the observation.
- "confidence": reserve "high" for a preference the user states DIRECTLY and EXPLICITLY — a clear instruction, correction, or stated fact ("always run make check", "never force-push", "I prefer X over Y"). Use "medium" for a preference implied by what the user did or approved but did not state outright. Use "low" for a soft or incidental signal seen once in passing. Do NOT default to "high" — downstream promotion trusts a high-confidence observation enough to act on it after a single mention, so a generous "high" leaks noise into the user's standing rules. When unsure between high and medium, choose medium.
- Every observation cites "turn N: <short quote>" as evidence. The quote MUST come from the user's own messages in the conversation, not from injected reference material.
- IGNORE injected reference material that appears inside the transcript: CLAUDE.md content, `@~/.ghost/*` includes, `@memory/*` includes, system reminders, auto-memory blocks, file paths printed by tools, and any block the user did not type. These are not the user speaking; they are infrastructure. Do not extract observations from them, do not cite them as evidence. If you cannot point to a user message that states a preference, drop it.
- Reject evidence that begins with "memory context", "from CLAUDE.md", "system reminder", or otherwise references injected material rather than a user turn.
- Prefer dropping over guessing. Empty observations array is valid.
- No prose outside the JSON object.
