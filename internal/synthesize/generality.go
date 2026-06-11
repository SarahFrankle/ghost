package synthesize

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/SarahFrankle/ghost/internal/anthropic"
	"github.com/SarahFrankle/ghost/internal/cluster"
	"github.com/SarahFrankle/ghost/prompts"
)

// RouteByGenerality classifies each preference cluster as general (-> rules)
// or domain-scoped (-> topics) via one batched LLM call, caching the verdict by
// VerdictFingerprint. On a cache hit (fingerprint match AND every cluster
// covered) it skips the model entirely. The split preserves input order.
func RouteByGenerality(ctx context.Context, client anthropic.Client, model string, clusters []cluster.Cluster, cachePath, generalityPromptHash string, logf func(string, ...any)) (general, scoped []cluster.Cluster, err error) {
	if len(clusters) == 0 {
		return nil, nil, nil
	}
	fp := VerdictFingerprint(clusters, generalityPromptHash, model)

	verdicts, ok := loadVerdictCache(cachePath, fp, clusters)
	if !ok {
		verdicts, err = askGenerality(ctx, client, model, clusters, logf)
		if err != nil {
			return nil, nil, err
		}
		if err := SaveVerdicts(cachePath, VerdictsFile{Fingerprint: fp, Verdicts: verdicts}); err != nil {
			return nil, nil, fmt.Errorf("save verdicts: %w", err)
		}
	}

	for _, c := range clusters {
		isGeneral, found := verdicts[c.Canonical]
		if !found {
			// Cache path guarantees coverage; live path defaults missing to
			// scoped. Treat any residual gap as scoped rather than erroring.
			isGeneral = false
		}
		if isGeneral {
			general = append(general, c)
		} else {
			scoped = append(scoped, c)
		}
	}
	return general, scoped, nil
}

// loadVerdictCache returns the cached verdict map iff the fingerprint matches
// and every input cluster has an entry. Otherwise (false) the caller recomputes.
func loadVerdictCache(path, fp string, clusters []cluster.Cluster) (map[string]bool, bool) {
	vf, err := LoadVerdicts(path)
	if err != nil || vf.Fingerprint != fp || vf.Verdicts == nil {
		return nil, false
	}
	for _, c := range clusters {
		if _, ok := vf.Verdicts[c.Canonical]; !ok {
			return nil, false
		}
	}
	return vf.Verdicts, true
}

func askGenerality(ctx context.Context, client anthropic.Client, model string, clusters []cluster.Cluster, logf func(string, ...any)) (map[string]bool, error) {
	var b strings.Builder
	b.WriteString("THEMES (verdicts MUST be returned in this order):\n")
	for i, c := range clusters {
		rep := c.Canonical
		if len(c.Members) > 0 {
			rep = c.Members[0].Text
		}
		fmt.Fprintf(&b, "%d. label=%q representative=%q projects=%d conversations=%d\n",
			i+1, c.Canonical, rep, c.ProjectCount, c.ConversationCount)
	}
	raw, err := client.Complete(ctx, model, prompts.SynthesizeGeneralitySystem(), b.String())
	if err != nil {
		return nil, fmt.Errorf("generality complete: %w", err)
	}
	return parseVerdicts(raw, clusters, logf)
}

// parseVerdicts matches verdicts to themes BY POSITION (input order), not by
// label string equality -- the model can normalise quotes/whitespace, so
// verbatim matching spuriously "misses" themes. A theme with no corresponding
// verdict (model returned fewer rows, or we ran past the array) DEFAULTS to
// domain-scoped (general=false): the conservative destination, since a
// mis-scoped general principle still surfaces as a topic while a dropped one
// vanishes. Defaults are logged and counted so calibration sees router drift.
// A partial response degrades; it never fails the rebuild.
func parseVerdicts(raw string, clusters []cluster.Cluster, logf func(string, ...any)) (map[string]bool, error) {
	s := strings.TrimSpace(raw)
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start < 0 || end <= start {
		return nil, fmt.Errorf("generality: no JSON object in reply")
	}
	var payload struct {
		Verdicts []struct {
			Label   string `json:"label"`
			General bool   `json:"general"`
		} `json:"verdicts"`
	}
	if err := json.Unmarshal([]byte(s[start:end+1]), &payload); err != nil {
		return nil, fmt.Errorf("generality: parse verdicts: %w", err)
	}
	out := make(map[string]bool, len(clusters))
	defaulted := 0
	for i, c := range clusters {
		if i < len(payload.Verdicts) {
			v := payload.Verdicts[i]
			out[c.Canonical] = v.General
			if v.Label != "" && v.Label != c.Canonical && logf != nil {
				logf("generality: verdict[%d] label %q != theme %q (matched by position)", i, v.Label, c.Canonical)
			}
			continue
		}
		out[c.Canonical] = false
		defaulted++
		if logf != nil {
			logf("generality: no verdict for theme %q; defaulting to domain-scoped", c.Canonical)
		}
	}
	if extra := len(payload.Verdicts) - len(clusters); extra > 0 && logf != nil {
		logf("generality: ignored %d extra verdict(s) beyond %d themes", extra, len(clusters))
	}
	if defaulted > 0 && logf != nil {
		logf("generality: %d/%d themes defaulted to domain-scoped (router returned a partial response)", defaulted, len(clusters))
	}
	return out, nil
}
