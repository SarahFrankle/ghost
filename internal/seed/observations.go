// Package seed — observations.go: the user-curated ~/.ghost/seed-observations.json file.
// Each entry is a high-confidence observation authored directly by the user (via `ghost remember`).
// extract never writes this file; the cluster stage loads it alongside the extracted observations so the fact routes through the normal generality + confidence gates and survives recompose.
package seed

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/SarahFrankle/ghost/internal/atomicfs"
	"github.com/SarahFrankle/ghost/internal/extract"
	"github.com/SarahFrankle/ghost/internal/fingerprint"
)

// SeedObservationsPath returns the seed-observations file location beside index.md.
func SeedObservationsPath(outDir string) string {
	return filepath.Join(outDir, "seed-observations.json")
}

// LoadSeedObservations reads the seed-observations file.
// A missing file is not an error (absent file = no user observations): it returns a zero ObservationsFile.
func LoadSeedObservations(path string) (extract.ObservationsFile, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return extract.ObservationsFile{}, nil
		}
		return extract.ObservationsFile{}, err
	}
	var f extract.ObservationsFile
	if err := json.Unmarshal(b, &f); err != nil {
		return extract.ObservationsFile{}, err
	}
	return f, nil
}

// AppendSeedObservation validates o, then appends it to the file at path, creating the file (with the fixed seed envelope) if absent.
// Atomic write.
func AppendSeedObservation(path string, o extract.Observation) error {
	if err := o.Validate(); err != nil {
		return err
	}
	f, err := LoadSeedObservations(path)
	if err != nil {
		return err
	}
	f.Source = "seed"
	f.Project = "user-seed"
	f.Observations = append(f.Observations, o)
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return atomicfs.WriteFile(path, b, 0o644)
}

// ObservationsHash is a content fingerprint over the observations' kind+text, mixed into the cluster fingerprint so editing the seed-observations file re-runs clustering/synthesis.
// Confidence and evidence are excluded intentionally — changes to those fields alone do not re-trigger reclustering, which is safe because `ghost remember` always writes fixed high confidence and generated evidence; the meaningful content is kind+text.
func ObservationsHash(f extract.ObservationsFile) string {
	parts := []string{"seed-obs/v1"}
	for _, o := range f.Observations {
		parts = append(parts, string(o.Kind), o.Text)
	}
	return fingerprint.Compute(parts...)
}
