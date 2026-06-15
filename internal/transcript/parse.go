package transcript

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
)

type Turn struct {
	Index int
	Role  string // "user" | "assistant" | "system" | "tool"
	Text  string
}

// rawEvent is a minimal projection over the Claude Code JSONL schema.
// Only the fields extract needs; unknown fields are ignored.
type rawEvent struct {
	Timestamp string `json:"timestamp"`
	Type      string `json:"type"`
	Role      string `json:"role"`
	Message   struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
	Content json.RawMessage `json:"content"`
}

// Parse reads a Claude Code transcript JSONL and returns one Turn per
// user/assistant message event, with content blocks flattened to text.
func Parse(path string) ([]Turn, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var turns []Turn
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 16<<20) // allow large lines
	idx := 0
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev rawEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue // skip malformed lines rather than abort
		}
		role, content := pickRoleAndContent(ev)
		if role == "" || len(content) == 0 {
			continue
		}
		text := flattenContent(content)
		if strings.TrimSpace(text) == "" {
			continue
		}
		turns = append(turns, Turn{Index: idx, Role: role, Text: text})
		idx++
	}
	return turns, sc.Err()
}

func pickRoleAndContent(ev rawEvent) (string, json.RawMessage) {
	if ev.Message.Role != "" {
		return ev.Message.Role, ev.Message.Content
	}
	return ev.Role, ev.Content
}

// flattenContent handles both string content and content-block arrays.
// Only "text" blocks contribute; thinking/tool_use/tool_result are skipped.
func flattenContent(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return ""
	}
	var b strings.Builder
	for _, blk := range blocks {
		if blk.Type == "text" && blk.Text != "" {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(blk.Text)
		}
	}
	return b.String()
}
