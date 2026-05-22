package extract

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/SarahFrankle/ghost/internal/anthropic"
	"github.com/SarahFrankle/ghost/internal/fingerprint"
	"github.com/SarahFrankle/ghost/internal/secrets"
	"github.com/SarahFrankle/ghost/internal/source"
	"github.com/SarahFrankle/ghost/prompts"
)

// ObservationsFingerprint returns the cache key for an observations file
// built from these inputs. Callers compute the expected fingerprint with
// the same arguments and compare it against the on-disk file; mismatch
// means a prompt, model, or input change and the file must be rebuilt.
func ObservationsFingerprint(sourceName, project, contentHash, model string) string {
	return fingerprint.Compute(
		"extract/v1",
		sourceName,
		project,
		contentHash,
		prompts.ExtractSystemHash(),
		model,
	)
}

// Logger is the minimal sink extract uses to report dropped records.
type Logger interface {
	Printf(format string, args ...any)
}

type Runner struct {
	Client anthropic.Client
	Model  string
	Log    Logger
	// KnownTopics is the sorted list of topic slugs that already exist on
	// disk. The extract prompt receives these as a KNOWN TOPICS block and
	// is instructed to reuse an existing slug when a candidate is a
	// near-synonym, closing the slug-drift feedback loop.
	KnownTopics []string
}

// Run extracts observations from one conversation. On success, returns
// a populated ObservationsFile. Malformed and secret-bearing observations
// are dropped (logged via r.Log).
func (r *Runner) Run(ctx context.Context, src source.Source, c source.Conversation, contentHash string) (ObservationsFile, error) {
	turns, err := src.Parse(ctx, c)
	if err != nil {
		return ObservationsFile{}, err
	}
	fp := ObservationsFingerprint(c.Source, c.Project, contentHash, r.Model)
	if !hasAssistantTurn(turns) {
		return ObservationsFile{
			Source:       c.ID,
			Project:      c.Project,
			ContentHash:  contentHash,
			ExtractedAt:  time.Now().UTC(),
			Fingerprint:  fp,
			Observations: []Observation{},
		}, nil
	}

	userPayload := renderPayload(r.KnownTopics, turns)
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
		if isInjectedSource(o.Evidence) {
			r.logf("drop: evidence cites injected material, not a user turn: %q", o.Evidence)
			continue
		}
		kept = append(kept, o)
	}

	return ObservationsFile{
		Source:       c.ID,
		Project:      c.Project,
		ContentHash:  contentHash,
		ExtractedAt:  time.Now().UTC(),
		Fingerprint:  fp,
		Observations: kept,
	}, nil
}

// hasAssistantTurn reports whether turns contains at least one assistant
// text turn. Transcripts without one are typically subagent-style runs whose
// substantive content is tool_use/tool_result blocks that Parse strips out;
// running extract on them produces low-signal observations from a single
// dispatch prompt.
func hasAssistantTurn(turns []source.Turn) bool {
	for _, t := range turns {
		if t.Role == "assistant" {
			return true
		}
	}
	return false
}

func (r *Runner) logf(format string, args ...any) {
	if r.Log != nil {
		r.Log.Printf(format, args...)
	}
}

// isInjectedSource returns true when the evidence string does not cite
// a user turn. The extract prompt requires "turn N: <quote>"; evidence
// in any other form (memory context, CLAUDE.md, system reminders, etc.)
// means the model could not find a user message to support the claim.
func isInjectedSource(evidence string) bool {
	e := strings.ToLower(strings.TrimSpace(evidence))
	return !strings.HasPrefix(e, "turn")
}

func renderPayload(knownTopics []string, turns []source.Turn) string {
	var b strings.Builder
	if len(knownTopics) > 0 {
		b.WriteString("KNOWN TOPICS (reuse a slug verbatim if your candidate is a near-synonym):\n")
		for _, s := range knownTopics {
			fmt.Fprintf(&b, "- %s\n", s)
		}
		b.WriteString("\nTRANSCRIPT:\n")
	}
	for _, t := range turns {
		fmt.Fprintf(&b, "turn %d (%s): %s\n", t.Index, t.Role, t.Text)
	}
	return b.String()
}

// parseObservations is permissive about leading/trailing prose around the JSON.
// It finds the first balanced top-level object, tracking JSON string literals
// so braces inside quoted prose don't throw off the span.
func parseObservations(raw string) ([]Observation, error) {
	span, ok := firstBalancedObject(raw)
	if !ok {
		return nil, fmt.Errorf("no JSON object found")
	}
	var wrap struct {
		Observations []Observation `json:"observations"`
	}
	if err := json.Unmarshal([]byte(span), &wrap); err != nil {
		return nil, err
	}
	return wrap.Observations, nil
}

// firstBalancedObject returns the substring of raw covering the first
// top-level `{...}` whose braces balance. It skips braces that appear
// inside JSON string literals (handling backslash escapes).
func firstBalancedObject(raw string) (string, bool) {
	start := strings.Index(raw, "{")
	if start < 0 {
		return "", false
	}
	depth := 0
	inStr := false
	escaped := false
	for i := start; i < len(raw); i++ {
		c := raw[i]
		if inStr {
			if escaped {
				escaped = false
				continue
			}
			switch c {
			case '\\':
				escaped = true
			case '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return raw[start : i+1], true
			}
		}
	}
	return "", false
}
