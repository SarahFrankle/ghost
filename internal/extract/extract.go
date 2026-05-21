package extract

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/SarahFrankle/ghost/internal/anthropic"
	"github.com/SarahFrankle/ghost/internal/secrets"
	"github.com/SarahFrankle/ghost/internal/transcript"
)

// Logger is the minimal sink extract uses to report dropped records.
type Logger interface {
	Printf(format string, args ...any)
}

type Runner struct {
	Client anthropic.Client
	Model  string
	Log    Logger
}

// Run extracts observations from one transcript. On success, returns
// a populated ObservationsFile. Malformed and secret-bearing observations
// are dropped (logged via r.Log).
func (r *Runner) Run(ctx context.Context, t transcript.Transcript, contentHash string) (ObservationsFile, error) {
	turns, err := transcript.Parse(t.Path)
	if err != nil {
		return ObservationsFile{}, err
	}
	if len(turns) == 0 {
		return ObservationsFile{
			Source:       t.Path,
			Project:      t.Project,
			ContentHash:  contentHash,
			ExtractedAt:  time.Now().UTC(),
			Observations: []Observation{},
		}, nil
	}

	userPayload := renderTurns(turns)
	raw, err := r.Client.Complete(ctx, r.Model, SystemPrompt(), userPayload)
	if err != nil {
		return ObservationsFile{}, fmt.Errorf("anthropic: %w", err)
	}

	parsed, err := parseObservations(raw)
	if err != nil {
		return ObservationsFile{}, fmt.Errorf("parse model output: %w", err)
	}

	kept := make([]Observation, 0, len(parsed))
	for _, o := range parsed {
		if err := o.Validate(); err != nil {
			r.logf("drop: schema invalid: %v", err)
			continue
		}
		if hit, pat := secrets.Detect(o.Text); hit {
			r.logf("drop: secret pattern %s in text", pat)
			continue
		}
		if hit, pat := secrets.Detect(o.Evidence); hit {
			r.logf("drop: secret pattern %s in evidence", pat)
			continue
		}
		kept = append(kept, o)
	}

	return ObservationsFile{
		Source:       t.Path,
		Project:      t.Project,
		ContentHash:  contentHash,
		ExtractedAt:  time.Now().UTC(),
		Observations: kept,
	}, nil
}

func (r *Runner) logf(format string, args ...any) {
	if r.Log != nil {
		r.Log.Printf(format, args...)
	}
}

func renderTurns(turns []transcript.Turn) string {
	var b strings.Builder
	for _, t := range turns {
		fmt.Fprintf(&b, "turn %d (%s): %s\n", t.Index, t.Role, t.Text)
	}
	return b.String()
}

// parseObservations is permissive about leading/trailing prose around the JSON.
func parseObservations(raw string) ([]Observation, error) {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("no JSON object found")
	}
	var wrap struct {
		Observations []Observation `json:"observations"`
	}
	if err := json.Unmarshal([]byte(raw[start:end+1]), &wrap); err != nil {
		return nil, err
	}
	return wrap.Observations, nil
}
