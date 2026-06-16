package effectiveness

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"sync"

	"github.com/SarahFrankle/ghost/internal/atomicfs"
)

// AuditLedger tracks how many JSONL lines of each transcript the audit has
// already scanned, so re-runs append only events from new lines (live
// transcripts grow). Keyed by transcript ID (the file path for claude-code).
type AuditLedger struct {
	mu      sync.Mutex
	Scanned map[string]int `json:"scanned_lines"`
}

func newAuditLedger() *AuditLedger { return &AuditLedger{Scanned: map[string]int{}} }

func LoadAuditLedger(path string) (*AuditLedger, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return newAuditLedger(), nil
		}
		return nil, err
	}
	l := newAuditLedger()
	if err := json.Unmarshal(b, l); err != nil {
		return nil, err
	}
	if l.Scanned == nil {
		l.Scanned = map[string]int{}
	}
	return l, nil
}

func (l *AuditLedger) ScannedLines(id string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.Scanned[id]
}

func (l *AuditLedger) SetScannedLines(id string, n int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.Scanned[id] = n
}

func (l *AuditLedger) Save(path string) error {
	l.mu.Lock()
	b, err := json.MarshalIndent(l, "", "  ")
	l.mu.Unlock()
	if err != nil {
		return err
	}
	return atomicfs.WriteFile(path, b, 0o644)
}
