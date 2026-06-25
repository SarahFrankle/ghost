You are given a list of topic slugs with their titles. Group them into a small
set of broad categories used only to organize an index — categories never
affect filenames or routing.

For each topic, assign exactly one category. Reuse the same category name across
related topics; prefer a handful of broad categories over many narrow ones. Use
short, lowercase category names (for example: testing, code-quality, git,
collaboration).

Output ONLY a JSON object, no prose, no code fence:

{"categories": {"<slug>": "<category>", ...}}

Every input slug must appear exactly once as a key.
