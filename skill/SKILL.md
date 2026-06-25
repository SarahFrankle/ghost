---
name: ghost
description: Use when the user wants to durably record a fact about themselves or how they want work done ("remember that I…", "from now on…", "my X is Y"). Records it via `ghost remember` so it applies now and survives the next compose.
---

# Ghost. Remember user-authored facts

You have identity context and a rule set always loaded.
When the user states a durable fact about themselves or a standing preference, record it so it persists across `ghost compose` (which otherwise regenerates the docs and loses ad-hoc edits).

## When to act

Trigger when the user says things like "remember that…", "from now on…", "my Jira handle is…", "I always prefer…".
A one-off instruction scoped to the current task is NOT a remember — only durable facts.

## How to record

1. Classify the fact:
   - **identity** — who the user is (handles, role, team, tools, stack).
   - **preference** — how they want work done (a standing rule or habit).
2. Run: `ghost remember --kind <identity|preference> "<concise fact>"`.
3. Keep the text concise and self-contained — it is rendered verbatim now and re-synthesized into the right doc on the next compose.

The command applies the fact immediately (appends it to `identity.md` or `rules.md`) and stores it as a high-confidence seed observation that the next `ghost compose` routes to its proper home (identity, rules, or a topic).

## Identity is context, not a template

The always-loaded `identity.md` tells you who the user is.
Use it to calibrate your answers (their stack, expertise, organization), not as a template to mimic.
