package synthesize

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/SarahFrankle/ghost/internal/anthropic"
	"github.com/SarahFrankle/ghost/internal/atomicfs"
	"github.com/SarahFrankle/ghost/internal/fingerprint"
	"github.com/SarahFrankle/ghost/prompts"
)

type categorizeCache struct {
	Fingerprint string            `json:"fingerprint"`
	Categories  map[string]string `json:"categories"`
}

// Categorize assigns each topic slug a broad category for index organization.
// Seed-pinned slugs (pinned: slug -> category) pass through verbatim and are
// never sent to the model.
// The result is cached at cachePath keyed by the topic slug set, the pinned
// map, the model, and the prompt hash.
// An LLM or parse error is returned so the caller can degrade to a flat index.
func Categorize(ctx context.Context, client anthropic.Client, model string, topics []TopicResult, pinned map[string]string, cachePath, promptHash string) (map[string]string, error) {
	fp := categorizeFingerprint(topics, pinned, model, promptHash)
	if cachePath != "" {
		if b, err := os.ReadFile(cachePath); err == nil {
			var c categorizeCache
			if json.Unmarshal(b, &c) == nil && c.Fingerprint == fp && c.Categories != nil {
				return c.Categories, nil
			}
		}
	}

	out := map[string]string{}
	var unpinned []TopicResult
	for _, t := range topics {
		if cat, ok := pinned[t.Slug]; ok && cat != "" {
			out[t.Slug] = cat
			continue
		}
		unpinned = append(unpinned, t)
	}

	if len(unpinned) > 0 {
		var b strings.Builder
		b.WriteString("TOPICS:\n")
		for _, t := range unpinned {
			fmt.Fprintf(&b, "- slug=%s title=%q\n", t.Slug, t.Title)
		}
		raw, err := client.Complete(ctx, model, prompts.SynthesizeCategorizeSystem(), b.String())
		if err != nil {
			return nil, fmt.Errorf("categorize: %w", err)
		}
		got, err := parseCategories(raw)
		if err != nil {
			return nil, fmt.Errorf("categorize: %w", err)
		}
		for _, t := range unpinned {
			cat := strings.TrimSpace(got[t.Slug])
			if cat == "" {
				return nil, fmt.Errorf("categorize: no category for slug %q", t.Slug)
			}
			out[t.Slug] = cat
		}
	}

	if cachePath != "" {
		if body, err := json.MarshalIndent(categorizeCache{Fingerprint: fp, Categories: out}, "", "  "); err == nil {
			_ = atomicfs.WriteFile(cachePath, body, 0o644)
		}
	}
	return out, nil
}

func parseCategories(raw string) (map[string]string, error) {
	s := strings.TrimSpace(raw)
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start < 0 || end <= start {
		return nil, fmt.Errorf("no JSON object in reply")
	}
	var payload struct {
		Categories map[string]string `json:"categories"`
	}
	if err := json.Unmarshal([]byte(s[start:end+1]), &payload); err != nil {
		return nil, fmt.Errorf("parse categories: %w", err)
	}
	if len(payload.Categories) == 0 {
		return nil, fmt.Errorf("empty categories")
	}
	return payload.Categories, nil
}

func categorizeFingerprint(topics []TopicResult, pinned map[string]string, model, promptHash string) string {
	slugs := make([]string, 0, len(topics))
	for _, t := range topics {
		slugs = append(slugs, t.Slug)
	}
	sort.Strings(slugs)
	pins := make([]string, 0, len(pinned))
	for k, v := range pinned {
		pins = append(pins, k+"="+v)
	}
	sort.Strings(pins)
	parts := []string{"categorize/v1", model, promptHash}
	parts = append(parts, slugs...)
	parts = append(parts, pins...)
	return fingerprint.Compute(parts...)
}
