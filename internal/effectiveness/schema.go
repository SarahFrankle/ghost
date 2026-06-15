// Package effectiveness measures whether ghost topic files are loaded for the
// right purpose, by analyzing the transcripts ghost already ingests.
package effectiveness

import (
	"bufio"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/SarahFrankle/ghost/internal/atomicfs"
	"github.com/SarahFrankle/ghost/internal/paths"
)

// Fit is the judge's purpose-fit verdict for one topic read.
type Fit string

const (
	FitYes     Fit = "yes"
	FitPartial Fit = "partial"
	FitNo      Fit = "no"
	FitUnknown Fit = "unknown" // judge not run, or failed
)

// TopicReadEvent is one detected Read of a ~/.ghost/topics/*.md file.
type TopicReadEvent struct {
	Timestamp          string `json:"ts"`
	TranscriptID       string `json:"transcript_id"`
	TopicSlug          string `json:"topic_slug"`
	TaskContextExcerpt string `json:"task_context_excerpt"`
	TriggerMatched     bool   `json:"trigger_matched"`
	Fit                Fit    `json:"fit"`
	FitReason          string `json:"fit_reason,omitempty"`
}

// AppendEvents appends events as JSON lines to path, creating it (and the
// parent dir) if needed. It reads any existing content and rewrites the whole
// file atomically, so a crash never leaves a torn line.
func AppendEvents(path string, evs []TopicReadEvent) error {
	if len(evs) == 0 {
		return nil
	}
	if err := paths.EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}
	existing, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	buf := append([]byte{}, existing...)
	for _, e := range evs {
		b, err := json.Marshal(e)
		if err != nil {
			return err
		}
		buf = append(buf, b...)
		buf = append(buf, '\n')
	}
	return atomicfs.WriteFile(path, buf, 0o644)
}

// ReadEvents reads all JSON-line events from path. A missing file yields nil.
func ReadEvents(path string) ([]TopicReadEvent, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var out []TopicReadEvent
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 16<<20)
	for sc.Scan() {
		b := sc.Bytes()
		if len(b) == 0 {
			continue
		}
		var e TopicReadEvent
		if err := json.Unmarshal(b, &e); err != nil {
			continue
		}
		out = append(out, e)
	}
	return out, sc.Err()
}
