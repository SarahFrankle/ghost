# Migrating off ~/.claude/memory/

Operational checklist for retiring the hand-curated memory directory
in favour of ghost's synthesized artifacts. The technical work
(atomic writes, always-loaded includes, SKILL.md) is shipped. What
remains is human review across ~two weeks of real use.

## Day 0 (already done)

- `~/.claude/CLAUDE.md` includes `@~/.ghost/identity.md`,
  `@~/.ghost/rules.md`, `@~/.ghost/rules.user.md`,
  `@~/.ghost/index.md`.
- `@~/.claude/projects/-Users-sarah-dev-projects-ghost/memory/MEMORY.md`
  (auto-memory) is still active. Both load. No conflict: auto-memory
  carries per-project notes, distinct from ghost's global synthesis.
- `ghost install-skill` has been run so Claude Code knows about the
  lazy-load skill.

## Week 1: review pass

After ~7 days of normal use:

1. `ghost show core` and read `identity.md` + `rules.md` end to end.
2. `ghost show topics` and skim each `~/.ghost/topics/<slug>.md`.
3. Open the relevant `~/.claude/memory/*.md` and the auto-memory
   `MEMORY.md`. For each fact:
   - **Captured correctly** by ghost: no action.
   - **Missed:** if it's durable across projects, run
     `ghost add-rule "<text>"`. If it's project-scoped, leave it in
     auto-memory. It will surface as a topic file once a second
     project agrees with it.
   - **Wrong:** identify the source transcript, then
     `ghost forget <transcript-path>`. Re-run
     `ghost compose --stages cluster,synthesize`.
4. Verify lazy-loaded topics actually fire. Start a session that
   should match a topic's triggers and check whether Claude reads
   the file. If a topic does not fire, its triggers in `index.md`
   are off. Either tune the prompt
   (`prompts/synthesize.index.system.md`) and recompose, or add an
   explicit user rule via `ghost add-rule`.

## Day 14: archive

If week-1 review came out clean:

1. Remove the `@memory/MEMORY.md` line from `~/.claude/CLAUDE.md`.
2. `mv ~/.claude/projects/-Users-sarah-dev-projects-ghost/memory ~/.claude/projects/-Users-sarah-dev-projects-ghost/memory.archive`.
3. Do not delete the archive. It is the cross-check baseline if
   ghost regresses later.

From this point, session feedback flows into transcripts and is
picked up by the next `ghost compose` run. The reactive
"save this as memory" pattern is no longer load-bearing.

## When to recompose

- After `ghost add-rule`: rules.user.md is the source of truth, but
  `rules.md` synthesis is subtractive against it. Recompose to drop
  any conflicting synthesized rule.
- After `ghost forget`: clusters and synthesis are stale.
- Otherwise weekly is fine. The cheap-model extract stage is the
  expensive one; cluster + synthesize is cheap. Use
  `ghost compose --estimate` before any large backfill.
