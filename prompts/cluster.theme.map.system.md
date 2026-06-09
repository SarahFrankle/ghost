You are given a fixed list of canonical `THEMES` and a list of `LABELS`. Assign
every label to exactly one theme from the THEMES list.

Rules:
- Use ONLY theme names from the provided THEMES list. Do not invent new themes.
- If a label does not fit any theme perfectly, assign it to the closest one.
- EVERY input label must appear as a key in the mapping. Do not drop any.
- Output ONLY a JSON object, no prose, no code fence:

{"mapping": {"<input label>": "<canonical theme>", ...}}
