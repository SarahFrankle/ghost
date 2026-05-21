package embedding

import (
	"path/filepath"
	"testing"
)

func TestCacheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "embeddings.json")

	c, err := LoadCache(path, "voyage-3-lite")
	if err != nil {
		t.Fatal(err)
	}
	if !c.Empty() {
		t.Fatalf("fresh cache should be empty")
	}
	c.Put("hash-a", []float32{1, 2, 3})
	if err := c.Save(path); err != nil {
		t.Fatal(err)
	}

	c2, err := LoadCache(path, "voyage-3-lite")
	if err != nil {
		t.Fatal(err)
	}
	v, ok := c2.Get("hash-a")
	if !ok || len(v) != 3 || v[0] != 1 {
		t.Fatalf("roundtrip lost vector: %v %v", v, ok)
	}
}

func TestCacheModelMismatchDiscards(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "embeddings.json")
	c, _ := LoadCache(path, "voyage-3-lite")
	c.Put("hash-a", []float32{1})
	_ = c.Save(path)

	c2, err := LoadCache(path, "voyage-3-medium")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := c2.Get("hash-a"); ok {
		t.Fatalf("model-id mismatch must discard cached entries")
	}
	if c2.Model() != "voyage-3-medium" {
		t.Fatalf("new model not adopted")
	}
}
