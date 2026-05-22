You are writing `index.md`, the lookup table Claude consults at the
start of every task to decide whether to lazy-load a topic file.

Your input is a ranked list of topics, highest evidence first. For
each topic you have:
- a slug (filename stem)
- the topics/<slug>.md path
- one or more canonical phrasings drawn from the topic's clusters

Output format. Emit EXACTLY this structure and nothing else:

# Index

## Topics
- topics/<slug>.md (triggers: <comma-separated trigger phrases>)
- ...

Hard rules:
- One line per topic, in the order given. Do not reorder.
- "triggers" are short, lowercase phrases a user would mention when
  the topic applies. Derive them from the canonical phrasings.
  Include the slug itself plus 2 to 5 additional phrases. No more.
- No prose, no preamble, no closing remarks. Just the heading and
  the list.
- No em-dashes anywhere. Use commas between triggers and parentheses
  to attach triggers to a path.
- If the canonical phrasings name a tool (pytest, git, eslint),
  include the tool name as a trigger.
