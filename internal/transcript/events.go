package transcript

import (
	"bufio"
	"encoding/json"
	"os"
)

// Event is one parsed item from a transcript: either a flattened text turn
// (Kind == "text") or a single tool_use block (Kind == "tool_use"). Unlike
// Parse, this preserves tool calls and the JSONL line each came from, so
// consumers can correlate a Read with the user turns surrounding it.
type Event struct {
	Line      int               // 1-based JSONL line number; multiple Events from the same line share this value.
	Timestamp string            // top-level event timestamp, if present
	Role      string            // user | assistant | ...
	Kind      string            // "text" | "tool_use"
	Text      string            // populated when Kind == "text"
	Tool      string            // tool name when Kind == "tool_use"
	Input     map[string]string // tool input args, string values only
}

// ParseEvents reads a Claude Code transcript JSONL and returns an ordered
// stream of text turns and tool_use blocks. Malformed lines are skipped.
func ParseEvents(path string) ([]Event, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []Event
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 16<<20)
	line := 0
	for sc.Scan() {
		line++
		b := sc.Bytes()
		if len(b) == 0 {
			continue
		}
		var ev rawEvent
		if err := json.Unmarshal(b, &ev); err != nil {
			continue
		}
		role, content := pickRoleAndContent(ev)
		if role == "" || len(content) == 0 {
			continue
		}
		out = append(out, blocksToEvents(content, role, ev.Timestamp, line)...)
	}
	return out, sc.Err()
}

// blocksToEvents turns a content field (string OR block array) into Events.
// A string becomes one text Event; an array yields one Event per text or
// tool_use block. Other block types are ignored.
func blocksToEvents(raw json.RawMessage, role, ts string, line int) []Event {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if s == "" {
			return nil
		}
		return []Event{{Line: line, Timestamp: ts, Role: role, Kind: "text", Text: s}}
	}
	var blocks []struct {
		Type  string         `json:"type"`
		Text  string         `json:"text"`
		Name  string         `json:"name"`
		Input map[string]any `json:"input"`
	}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil
	}
	var out []Event
	for _, blk := range blocks {
		switch blk.Type {
		case "text":
			if blk.Text != "" {
				out = append(out, Event{Line: line, Timestamp: ts, Role: role, Kind: "text", Text: blk.Text})
			}
		case "tool_use":
			out = append(out, Event{
				Line: line, Timestamp: ts, Role: role, Kind: "tool_use",
				Tool: blk.Name, Input: stringInputs(blk.Input),
			})
		}
	}
	return out
}

// stringInputs keeps only string-valued input args (file_path, pattern, etc.),
// which is all the audit needs; non-string values are dropped.
func stringInputs(in map[string]any) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}
	return out
}
