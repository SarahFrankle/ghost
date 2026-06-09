package cluster

import (
	"path/filepath"
	"testing"
)

func TestLabelCacheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "labels.json")

	c, err := LoadLabelCache(path, "haiku", "ph1")
	if err != nil {
		t.Fatal(err)
	}
	c.Put("h1", "pr review")
	if err := c.Save(path); err != nil {
		t.Fatal(err)
	}

	c2, err := LoadLabelCache(path, "haiku", "ph1")
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := c2.Get("h1"); !ok || v != "pr review" {
		t.Fatalf("Get(h1) = %q,%v; want pr review,true", v, ok)
	}
}

func TestLabelCacheModelMismatchDiscards(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "labels.json")
	c, _ := LoadLabelCache(path, "haiku", "ph1")
	c.Put("h1", "x")
	_ = c.Save(path)

	// Different model -> entries discarded.
	c2, _ := LoadLabelCache(path, "sonnet", "ph1")
	if _, ok := c2.Get("h1"); ok {
		t.Fatal("model mismatch should discard entries")
	}
	// Different prompt hash -> entries discarded.
	c3, _ := LoadLabelCache(path, "haiku", "ph2")
	if _, ok := c3.Get("h1"); ok {
		t.Fatal("prompt mismatch should discard entries")
	}
}
