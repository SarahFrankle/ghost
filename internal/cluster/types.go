package cluster

import "time"

const SchemaVersion = 1

// ClusterMember is one observation as it appears inside a cluster.
type ClusterMember struct {
	ObservationHash string `json:"observation_hash"`
	Source          string `json:"source"`
	Project         string `json:"project"`
	Kind            string `json:"kind"`
	Text            string `json:"text"`
	Evidence        string `json:"evidence"`
	Context         string `json:"context,omitempty"`
	Confidence      string `json:"confidence,omitempty"`
}

// SubKey returns the partition discriminator inside a kind.
// voice → Context; everything else (including topic) → "". Topics are
// pooled together and merged by embedding cosine, not by a free-text key.
func (m ClusterMember) SubKey() string {
	if m.Kind == "voice" {
		return m.Context
	}
	return ""
}

// Cluster is a group of observations that describe the same thing.
type Cluster struct {
	Kind          string          `json:"kind"`
	SubKey        string          `json:"sub_key,omitempty"`
	Canonical     string          `json:"canonical"`
	Members       []ClusterMember `json:"members"`
	EvidenceCount int             `json:"evidence_count"`
	ProjectCount  int             `json:"project_count"`
}

type ClustersFile struct {
	SchemaVersion    int       `json:"schema_version"`
	EmbeddingModelID string    `json:"embedding_model_id"`
	BuiltAt          time.Time `json:"built_at"`
	Fingerprint      string    `json:"fingerprint,omitempty"`
	Clusters         []Cluster `json:"clusters"`
}
