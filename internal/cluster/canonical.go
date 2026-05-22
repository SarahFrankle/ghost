package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/SarahFrankle/ghost/internal/anthropic"
	"github.com/SarahFrankle/ghost/prompts"
)

// Canonicalizer fills in Cluster.Canonical for multi-member clusters
// using a cheap-model call. Single-member clusters keep their member's
// text. Failures are non-fatal: the cluster keeps its seed text and the
// error is reported via Log.
type Canonicalizer struct {
	Client anthropic.Client
	Model  string
	Log    func(format string, args ...any)
	OnCall func() // test hook; nil in production
}

type canonicalResponse struct {
	Canonical string `json:"canonical"`
	Same      bool   `json:"same"`
}

func (c *Canonicalizer) Apply(ctx context.Context, clusters []Cluster) error {
	var total int
	for _, cl := range clusters {
		if len(cl.Members) >= 2 {
			total++
		}
	}
	c.logf("canonical: %d multi-member cluster(s) to phrase", total)
	var done int
	for i := range clusters {
		if len(clusters[i].Members) < 2 {
			continue
		}
		if c.OnCall != nil {
			c.OnCall()
		}
		done++
		start := time.Now()
		c.logf("canonical: [%d/%d] %s (%d members)...", done, total, clusters[i].Kind, len(clusters[i].Members))
		payload := renderForCanonical(clusters[i])
		raw, err := c.Client.Complete(ctx, c.Model, prompts.ClusterCanonicalSystem(), payload)
		if err != nil {
			c.logf("canonical: cluster %d %s: %v", i, clusters[i].Kind, err)
			continue
		}
		parsed, err := parseCanonical(raw)
		if err != nil {
			c.logf("canonical: cluster %d: parse: %v", i, err)
			continue
		}
		if !parsed.Same || strings.TrimSpace(parsed.Canonical) == "" {
			c.logf("canonical: cluster %d: model says members are not the same; keeping seed", i)
			continue
		}
		clusters[i].Canonical = parsed.Canonical
		c.logf("canonical: [%d/%d] done in %s", done, total, time.Since(start).Round(time.Millisecond))
	}
	return nil
}

func (c *Canonicalizer) logf(format string, args ...any) {
	if c.Log != nil {
		c.Log(format, args...)
	}
}

func renderForCanonical(cl Cluster) string {
	var b strings.Builder
	fmt.Fprintf(&b, "kind: %s\n", cl.Kind)
	if cl.SubKey != "" {
		fmt.Fprintf(&b, "sub_key: %s\n", cl.SubKey)
	}
	b.WriteString("members:\n")
	for i, m := range cl.Members {
		fmt.Fprintf(&b, "  %d: %s\n", i+1, m.Text)
	}
	return b.String()
}

// parseCanonical extracts the first balanced JSON object from raw and
// decodes it as canonicalResponse. Uses balanced-brace scanning so a
// model response wrapped in stray prose still parses.
func parseCanonical(raw string) (canonicalResponse, error) {
	start := strings.Index(raw, "{")
	if start < 0 {
		return canonicalResponse{}, fmt.Errorf("no JSON object")
	}
	depth, inStr, esc := 0, false, false
	for i := start; i < len(raw); i++ {
		c := raw[i]
		if inStr {
			if esc {
				esc = false
				continue
			}
			switch c {
			case '\\':
				esc = true
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
				var out canonicalResponse
				if err := json.Unmarshal([]byte(raw[start:i+1]), &out); err != nil {
					return out, err
				}
				return out, nil
			}
		}
	}
	return canonicalResponse{}, fmt.Errorf("unbalanced JSON")
}
