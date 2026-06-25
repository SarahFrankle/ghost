---
name: ghost
description: Use at the start of any task. Checks the ghost index and reads matching topic files before responding. Triggers on any task touching an entry listed in ~/.ghost/index.md.
---

# Ghost. Lazy-load topic guidance

You have identity context and a rule set always loaded. You also
have an index at `~/.ghost/index.md` listing lazy-loaded topic
files under `~/.ghost/topics/`.

## Mechanical check (before responding to the user)

1. Read `~/.ghost/index.md` if you have not already this session.
2. Match the user's request against the trigger phrases for each
   topic entry.
3. If a topic entry matches, Read `~/.ghost/topics/<slug>.md`
   before writing code or answering.
   If a listed topic file is missing, skip that entry silently and load the rest — a missing file means the taxonomy was regenerated; it is not an error.
4. If nothing matches, proceed without loading anything.

A file loaded once per session stays in context. Do not re-Read
it. Do not load every topic at session start. Lazy loading is the
whole point.

## Identity is context, not a template

The always-loaded `identity.md` tells you who the user is. Use it
to calibrate your answers (their stack, expertise, organization),
not as a template to mimic. The user is a specialist in some
areas; you stay a generalist across all areas.

## When the index is missing

If `~/.ghost/index.md` does not exist, ghost has not been
composed yet. Proceed normally and do not warn the user. They
will run `ghost compose` when they want it.
