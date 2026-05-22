package cluster

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/SarahFrankle/ghost/internal/atomicfs"
)

const canonicalCacheSchemaVersion = 1

// CanonicalKey is a stable hash of everything that determines a
// cluster's canonical phrasing: kind, sub_key, and the sorted set of
// member texts. Member order does not matter — two clusters with the
// same members in different order produce the same key.
func CanonicalKey(cl Cluster) string {
	texts := make([]string, 0, len(cl.Members))
	for _, m := range cl.Members {
		texts = append(texts, m.Text)
	}
	sort.Strings(texts)
	h := sha256.New()
	h.Write([]byte(cl.Kind))
	h.Write([]byte{'|'})
	h.Write([]byte(cl.SubKey))
	h.Write([]byte{'|'})
	h.Write([]byte(strings.Join(texts, "\n")))
	return hex.EncodeToString(h.Sum(nil))
}

type canonicalCacheFile struct {
	SchemaVersion int               `json:"schema_version"`
	Model         string            `json:"model"`
	Entries       map[string]string `json:"entries"`
}

// CanonicalCache memoizes Canonicalizer results by CanonicalKey so a
// repeated compose run doesn't re-canonicalize unchanged clusters.
// A model-id mismatch on load discards every entry — different cheap
// models will phrase the same cluster differently.
type CanonicalCache struct {
	mu      sync.Mutex
	model   string
	entries map[string]string
}

func LoadCanonicalCache(path, currentModel string) (*CanonicalCache, error) {
	c := &CanonicalCache{model: currentModel, entries: map[string]string{}}
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return c, nil
		}
		return nil, err
	}
	var raw canonicalCacheFile
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	if raw.SchemaVersion > canonicalCacheSchemaVersion {
		return nil, errors.New("canonical_cache.json schema_version newer than binary supports")
	}
	if raw.Model == currentModel && raw.Entries != nil {
		c.entries = raw.Entries
	}
	return c, nil
}

func (c *CanonicalCache) Get(key string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.entries[key]
	return v, ok
}

func (c *CanonicalCache) Put(key, canonical string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = canonical
}

func (c *CanonicalCache) Save(path string) error {
	c.mu.Lock()
	body, err := json.MarshalIndent(canonicalCacheFile{
		SchemaVersion: canonicalCacheSchemaVersion,
		Model:         c.model,
		Entries:       c.entries,
	}, "", "  ")
	c.mu.Unlock()
	if err != nil {
		return err
	}
	return atomicfs.WriteFile(path, body, 0o644)
}
