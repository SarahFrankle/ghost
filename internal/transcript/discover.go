package transcript

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	doublestar "github.com/bmatcuk/doublestar/v4"
)

type Transcript struct {
	Path    string
	Project string
	ModTime time.Time
}

// osStat is overridable in tests but normally calls os.Stat.
var osStat = os.Stat

// Discover returns transcripts matching glob whose mtime is older than
// (now - activeWindow). The active-session skip is from the design spec.
func Discover(glob string, activeWindow time.Duration, now time.Time) ([]Transcript, error) {
	matches, err := doublestar.FilepathGlob(glob)
	if err != nil {
		return nil, err
	}
	cutoff := now.Add(-activeWindow)
	out := make([]Transcript, 0, len(matches))
	for _, p := range matches {
		if isSubagentTranscript(p) {
			continue
		}
		if isSidecarFile(p) {
			continue
		}
		fi, err := osStat(p)
		if err != nil {
			continue
		}
		if fi.ModTime().After(cutoff) {
			continue
		}
		out = append(out, Transcript{
			Path:    p,
			Project: projectFromPath(p),
			ModTime: fi.ModTime(),
		})
	}
	return out, nil
}

// isSubagentTranscript reports whether p lives under a `subagents/` directory.
// Claude Code writes dispatched subagent sessions there; their JSONL is
// dominated by tool_use/tool_result blocks and parses down to the single
// dispatch prompt, which is not a real conversation worth mining.
func isSubagentTranscript(p string) bool {
	return slices.Contains(strings.Split(filepath.ToSlash(p), "/"), "subagents")
}

// isSidecarFile reports whether p is one of the per-session sidecar JSONL
// files Claude Code writes alongside real transcripts (e.g. `ai-title`,
// which holds only a generated title and sessionId). These carry no
// conversation turns, so discovering them just pads the pending set with
// empty extractions and churns the observation cache. We detect them by the
// `type` of their first record; real transcripts open with a message or
// session-metadata record, never a sidecar type.
//
// Only single-record sidecar types are listed here. Metadata types that also
// appear as the first line of genuine transcripts (last-prompt, permission-mode,
// system, ...) are deliberately excluded — those files still contain turns.
func isSidecarFile(p string) bool {
	f, err := os.Open(p)
	if err != nil {
		return false // let the later stat/parse handle the error
	}
	defer f.Close()

	r := bufio.NewReader(f)
	for {
		line, err := r.ReadBytes('\n')
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) > 0 {
			var rec struct {
				Type string `json:"type"`
			}
			if jsonErr := json.Unmarshal(trimmed, &rec); jsonErr != nil {
				return false
			}
			return sidecarTypes[rec.Type]
		}
		if err != nil {
			return false // EOF before any non-empty record
		}
	}
}

// sidecarTypes are first-record `type` values that uniquely identify a
// non-conversation sidecar file (one record, no turns).
var sidecarTypes = map[string]bool{
	"ai-title": true,
}

// projectFromPath extracts the project segment from a Claude Code transcript path.
// Claude Code encodes cwd into the parent directory name, prefixed with `-`.
func projectFromPath(p string) string {
	parent := filepath.Base(filepath.Dir(p))
	return strings.TrimPrefix(parent, "-")
}
