package ledger

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"sync"
	"time"
)

const CurrentSchemaVersion = 1

type Entry struct {
	ContentHash      string    `json:"content_hash"`
	ProcessedAt      time.Time `json:"processed_at"`
	ObservationsFile string    `json:"observations_file"`
	MessageCount     int       `json:"message_count"`
}

type LastCompose struct {
	At             time.Time `json:"at"`
	StagesRun      []string  `json:"stages_run"`
	PromptsVersion string    `json:"prompts_version"`
}

type Ledger struct {
	mu            sync.Mutex
	SchemaVersion int              `json:"schema_version"`
	Conversations map[string]Entry `json:"conversations"`
	LastCompose   *LastCompose     `json:"last_compose,omitempty"`
}

func New() *Ledger {
	return &Ledger{
		SchemaVersion: CurrentSchemaVersion,
		Conversations: map[string]Entry{},
	}
}

// Load reads ledger.json from path, or returns an empty ledger if absent.
func Load(path string) (*Ledger, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return New(), nil
		}
		return nil, err
	}
	l := New()
	if err := json.Unmarshal(b, l); err != nil {
		return nil, err
	}
	if l.Conversations == nil {
		l.Conversations = map[string]Entry{}
	}
	return l, nil
}

// Save atomically writes the ledger to path.
func (l *Ledger) Save(path string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	b, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (l *Ledger) Mark(path string, e Entry) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if e.ProcessedAt.IsZero() {
		e.ProcessedAt = time.Now().UTC()
	}
	l.Conversations[path] = e
}

func (l *Ledger) NeedsProcessing(path, contentHash string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	cur, ok := l.Conversations[path]
	if !ok {
		return true
	}
	return cur.ContentHash != contentHash
}

func (l *Ledger) Forget(path string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.Conversations[path]; !ok {
		return false
	}
	delete(l.Conversations, path)
	return true
}

func (l *Ledger) SetLastCompose(stages []string, promptsVersion string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.LastCompose = &LastCompose{
		At:             time.Now().UTC(),
		StagesRun:      stages,
		PromptsVersion: promptsVersion,
	}
}
