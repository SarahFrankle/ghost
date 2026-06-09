package cluster

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"sync"

	"github.com/SarahFrankle/ghost/internal/atomicfs"
)

const labelCacheSchemaVersion = 1

type labelCacheFile struct {
	SchemaVersion int               `json:"schema_version"`
	LabelModelID  string            `json:"label_model_id"`
	PromptHash    string            `json:"prompt_hash"`
	Entries       map[string]string `json:"entries"`
}

// LabelCache is an in-memory cache of per-observation topic labels backed by
// labels.json. A model-id or prompt-hash mismatch on load discards every
// entry — a different label model or prompt produces a different vocabulary,
// so stale labels are unsafe to reuse.
type LabelCache struct {
	mu         sync.Mutex
	model      string
	promptHash string
	entries    map[string]string
}

func LoadLabelCache(path, currentModel, currentPromptHash string) (*LabelCache, error) {
	c := &LabelCache{model: currentModel, promptHash: currentPromptHash, entries: map[string]string{}}
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return c, nil
		}
		return nil, err
	}
	var raw labelCacheFile
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	if raw.SchemaVersion > labelCacheSchemaVersion {
		return nil, errors.New("labels.json schema_version newer than binary supports")
	}
	if raw.LabelModelID == currentModel && raw.PromptHash == currentPromptHash && raw.Entries != nil {
		c.entries = raw.Entries
	}
	return c, nil
}

func (c *LabelCache) Get(h string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.entries[h]
	return v, ok
}

func (c *LabelCache) Put(h, label string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[h] = label
}

func (c *LabelCache) Save(path string) error {
	c.mu.Lock()
	body, err := json.MarshalIndent(labelCacheFile{
		SchemaVersion: labelCacheSchemaVersion,
		LabelModelID:  c.model,
		PromptHash:    c.promptHash,
		Entries:       c.entries,
	}, "", "  ")
	c.mu.Unlock()
	if err != nil {
		return err
	}
	return atomicfs.WriteFile(path, body, 0o644)
}
