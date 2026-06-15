package extract

import (
	"errors"
	"fmt"
	"time"
)

// Kind is the closed set of observation categories the extract stage emits.
type Kind string

const (
	KindIdentity   Kind = "identity"
	KindPreference Kind = "preference"
	KindVoice      Kind = "voice"
)

// Confidence is the model's stated confidence in an observation. Only High is
// load-bearing downstream (it lets a directly-asserted preference promote even
// when stated once); the softer values are carried through for context.
type Confidence string

const (
	ConfidenceHigh   Confidence = "high"
	ConfidenceMedium Confidence = "medium"
	ConfidenceLow    Confidence = "low"
)

type Observation struct {
	Kind       Kind       `json:"kind"`
	Text       string     `json:"text"`
	Evidence   string     `json:"evidence"`
	Confidence Confidence `json:"confidence,omitempty"`
	Context    string     `json:"context,omitempty"`
}

type ObservationsFile struct {
	Source       string        `json:"source"`
	Project      string        `json:"project"`
	ContentHash  string        `json:"content_hash"`
	ExtractedAt  time.Time     `json:"extracted_at"`
	Fingerprint  string        `json:"fingerprint,omitempty"`
	Observations []Observation `json:"observations"`
}

var validKinds = map[Kind]bool{
	KindIdentity: true, KindPreference: true, KindVoice: true,
}

func (o Observation) Validate() error {
	if !validKinds[o.Kind] {
		return fmt.Errorf("invalid kind %q", o.Kind)
	}
	if o.Text == "" {
		return errors.New("text required")
	}
	if o.Evidence == "" {
		return errors.New("evidence required")
	}
	if o.Kind == KindVoice && o.Context == "" {
		return errors.New("voice kind requires context field")
	}
	return nil
}
