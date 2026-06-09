package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultsWhenNoFile(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(filepath.Join(dir, "missing.toml"))
	if err != nil {
		t.Fatalf("Load on missing file: %v", err)
	}
	if cfg.Models.Cheap == "" || cfg.Models.Smart == "" {
		t.Fatalf("defaults not populated: %+v", cfg.Models)
	}
	if cfg.Batching.ExtractWorkers != 5 {
		t.Fatalf("default extract_workers = %d, want 5", cfg.Batching.ExtractWorkers)
	}
}

func TestDefaultsClusterCosineThresholds(t *testing.T) {
	d := Defaults()
	if d.Thresholds.ClusterCosineIdentityRule != 0.85 {
		t.Fatalf("identity/rule default = %v, want 0.85", d.Thresholds.ClusterCosineIdentityRule)
	}
	if d.Thresholds.MinClusterSize != 3 {
		t.Fatalf("min cluster size default = %v, want 3", d.Thresholds.MinClusterSize)
	}
	if d.Models.Label == "" || d.Models.Theme == "" {
		t.Fatalf("label/theme model defaults must be set: %q / %q", d.Models.Label, d.Models.Theme)
	}
}

func TestOverridesFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := `
[batching]
extract_workers = 9
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Batching.ExtractWorkers != 9 {
		t.Fatalf("override not applied: %d", cfg.Batching.ExtractWorkers)
	}
	if cfg.Models.Cheap == "" {
		t.Fatalf("default cheap model lost after partial override")
	}
}
