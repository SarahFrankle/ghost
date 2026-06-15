package effectiveness

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/SarahFrankle/ghost/internal/anthropic"
	"github.com/SarahFrankle/ghost/internal/atomicfs"
	"github.com/SarahFrankle/ghost/internal/fingerprint"
)

type cachedVerdict struct {
	Fit    Fit    `json:"fit"`
	Reason string `json:"reason"`
}

// Judge rates topic-read purpose-fit via an anthropic.Client, caching verdicts
// by a fingerprint over (topic body, task context, prompt, model). Modeled on
// internal/synthesize/verdictcache.go.
type Judge struct {
	client     anthropic.Client
	model      string
	system     string
	promptHash string
	cachePath  string

	mu    sync.Mutex
	cache map[string]cachedVerdict
}

func NewJudge(client anthropic.Client, model, system, promptHash, cachePath string) *Judge {
	j := &Judge{client: client, model: model, system: system, promptHash: promptHash, cachePath: cachePath, cache: map[string]cachedVerdict{}}
	if b, err := os.ReadFile(cachePath); err == nil {
		_ = json.Unmarshal(b, &j.cache)
	}
	return j
}

func (j *Judge) key(ev TopicReadEvent, topicBody string) string {
	return fingerprint.Compute("judge/v1", j.model, j.promptHash, ev.TopicSlug, topicBody, ev.TaskContextExcerpt)
}

// Judge returns the purpose-fit verdict for ev, given the topic's body.
// On client error it returns FitUnknown with a nil error (audit never blocks).
func (j *Judge) Judge(ctx context.Context, ev TopicReadEvent, topicBody string) (Fit, string, error) {
	k := j.key(ev, topicBody)
	j.mu.Lock()
	if v, ok := j.cache[k]; ok {
		j.mu.Unlock()
		return v.Fit, v.Reason, nil
	}
	j.mu.Unlock()

	user := fmt.Sprintf("TASK:\n%s\n\nTOPIC LOADED: %s\nTOPIC BODY:\n%s", ev.TaskContextExcerpt, ev.TopicSlug, topicBody)
	out, err := j.client.Complete(ctx, j.model, j.system, user)
	if err != nil {
		return FitUnknown, "", nil
	}
	fit, reason := parseFitLine(strings.TrimSpace(out))
	j.mu.Lock()
	j.cache[k] = cachedVerdict{Fit: fit, Reason: reason}
	j.mu.Unlock()
	return fit, reason, nil
}

func (j *Judge) SaveCache() error {
	j.mu.Lock()
	b, err := json.MarshalIndent(j.cache, "", "  ")
	j.mu.Unlock()
	if err != nil {
		return err
	}
	return atomicfs.WriteFile(j.cachePath, b, 0o644)
}

var fitRe = regexp.MustCompile(`(?i)^FIT:\s*(yes|partial|no)\s*[—-]\s*(.*)$`)

// parseFitLine parses "FIT: <yes|partial|no> — <reason>". Unrecognized input
// yields FitUnknown, "".
func parseFitLine(line string) (Fit, string) {
	m := fitRe.FindStringSubmatch(strings.TrimSpace(line))
	if m == nil {
		return FitUnknown, ""
	}
	reason := strings.TrimSpace(m[2])
	switch strings.ToLower(m[1]) {
	case "yes":
		return FitYes, reason
	case "partial":
		return FitPartial, reason
	case "no":
		return FitNo, reason
	}
	return FitUnknown, ""
}
