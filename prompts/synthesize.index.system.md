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
- "triggers" are the natural, lowercase words a user would actually
  type when starting a task this topic should inform: the broad
  subject plus its common synonyms and short forms (for a testing
  topic: "tests", "testing", "write a test", "unit test") -- NOT the
  topic's internal specifics (avoid coined phrases like "paired
  constants" or "suffix naming"). The canonical phrasings tell you the
  subject; do not copy their wording as triggers.
- Always include the broad subject word on its own. Give 3 to 6
  triggers total. Include the slug only if it reads as a phrase a user
  would actually type; skip it when it is coined jargon.
- No prose, no preamble, no closing remarks. Just the heading and
  the list.
- No em-dashes anywhere. Use commas between triggers and parentheses
  to attach triggers to a path.
- If the canonical phrasings name a tool (pytest, git, eslint),
  include the tool name as a trigger.
