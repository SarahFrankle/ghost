package embedding

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"sync"

	"github.com/SarahFrankle/ghost/internal/atomicfs"
)

const cacheSchemaVersion = 1

type cacheFile struct {
	SchemaVersion    int                  `json:"schema_version"`
	EmbeddingModelID string               `json:"embedding_model_id"`
	Entries          map[string][]float32 `json:"entries"`
}

// Cache is an in-memory embedding cache backed by embeddings.json.
// A model-id mismatch on load discards every entry — cosine
// distributions differ across models, so old vectors are unsafe.
type Cache struct {
	mu      sync.Mutex
	model   string
	entries map[string][]float32
}

func LoadCache(path, currentModel string) (*Cache, error) {
	c := &Cache{model: currentModel, entries: map[string][]float32{}}
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return c, nil
		}
		return nil, err
	}
	var raw cacheFile
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	if raw.SchemaVersion > cacheSchemaVersion {
		return nil, errors.New("embeddings.json schema_version newer than binary supports")
	}
	if raw.EmbeddingModelID == currentModel && raw.Entries != nil {
		c.entries = raw.Entries
	}
	return c, nil
}

func (c *Cache) Empty() bool { c.mu.Lock(); defer c.mu.Unlock(); return len(c.entries) == 0 }
func (c *Cache) Model() string { c.mu.Lock(); defer c.mu.Unlock(); return c.model }
func (c *Cache) Get(h string) ([]float32, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.entries[h]
	return v, ok
}
func (c *Cache) Put(h string, v []float32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[h] = v
}

func (c *Cache) Save(path string) error {
	c.mu.Lock()
	body, err := json.MarshalIndent(cacheFile{
		SchemaVersion:    cacheSchemaVersion,
		EmbeddingModelID: c.model,
		Entries:          c.entries,
	}, "", "  ")
	c.mu.Unlock()
	if err != nil {
		return err
	}
	return atomicfs.WriteFile(path, body, 0o644)
}
