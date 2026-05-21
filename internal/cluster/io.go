package cluster

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/SarahFrankle/ghost/internal/atomicfs"
)

func SaveClusters(path string, f ClustersFile) error {
	if f.SchemaVersion == 0 {
		f.SchemaVersion = SchemaVersion
	}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return atomicfs.WriteFile(path, b, 0o644)
}

func LoadClusters(path string) (ClustersFile, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return ClustersFile{}, err
	}
	var f ClustersFile
	if err := json.Unmarshal(b, &f); err != nil {
		return f, err
	}
	if f.SchemaVersion > SchemaVersion {
		return f, fmt.Errorf("clusters.json schema_version=%d newer than binary supports", f.SchemaVersion)
	}
	return f, nil
}
