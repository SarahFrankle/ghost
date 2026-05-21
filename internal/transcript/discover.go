package transcript

import (
	"os"
	"path/filepath"
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

// projectFromPath extracts the project segment from a Claude Code transcript path.
// Claude Code encodes cwd into the parent directory name, prefixed with `-`.
func projectFromPath(p string) string {
	parent := filepath.Base(filepath.Dir(p))
	return strings.TrimPrefix(parent, "-")
}
