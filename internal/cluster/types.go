package cluster

import (
	"time"

	"github.com/SarahFrankle/ghost/internal/extract"
)

const SchemaVersion = 1

// ClusterMember is one observation as it appears inside a cluster.
type ClusterMember struct {
	ObservationHash string             `json:"observation_hash"`
	ConversationID  string             `json:"source"` // conversation ID; json tag kept as "source" for clusters.json compatibility
	Project         string             `json:"project"`
	Kind            extract.Kind       `json:"kind"`
	Text            string             `json:"text"`
	Evidence        string             `json:"evidence"`
	Context         string             `json:"context,omitempty"`
	Confidence      extract.Confidence `json:"confidence,omitempty"`
}

// SubKey returns the partition discriminator inside a kind.
// voice → Context; everything else → "". Only voice observations are
// partitioned by a free-text key; the rest pool together within their kind.
func (m ClusterMember) SubKey() string {
	if m.Kind == extract.KindVoice {
		return m.Context
	}
	return ""
}

// Cluster is a group of observations that describe the same thing.
type Cluster struct {
	Kind              extract.Kind    `json:"kind"`
	SubKey            string          `json:"sub_key,omitempty"`
	Canonical         string          `json:"canonical"`
	Members           []ClusterMember `json:"members"`
	EvidenceCount     int             `json:"evidence_count"`
	ProjectCount      int             `json:"project_count"`
	ConversationCount int             `json:"conversation_count"`
}

type ClustersFile struct {
	SchemaVersion    int       `json:"schema_version"`
	EmbeddingModelID string    `json:"embedding_model_id"`
	BuiltAt          time.Time `json:"built_at"`
	Fingerprint      string    `json:"fingerprint,omitempty"`
	Clusters         []Cluster `json:"clusters"`
}
