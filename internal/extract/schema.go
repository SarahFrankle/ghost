package extract

import (
	"errors"
	"fmt"
	"time"
)

type Observation struct {
	Kind       string `json:"kind"`
	Text       string `json:"text"`
	Evidence   string `json:"evidence"`
	Confidence string `json:"confidence,omitempty"`
	Topic      string `json:"topic,omitempty"`
	Context    string `json:"context,omitempty"`
}

type ObservationsFile struct {
	Source       string        `json:"source"`
	Project      string        `json:"project"`
	ContentHash  string        `json:"content_hash"`
	ExtractedAt  time.Time     `json:"extracted_at"`
	Observations []Observation `json:"observations"`
}

var validKinds = map[string]bool{
	"identity": true, "rule": true, "topic": true, "voice": true,
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
	if o.Kind == "topic" && o.Topic == "" {
		return errors.New("topic kind requires topic field")
	}
	if o.Kind == "voice" && o.Context == "" {
		return errors.New("voice kind requires context field")
	}
	return nil
}
