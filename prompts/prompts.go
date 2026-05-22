// Package prompts embeds the system prompts shipped with ghost so they
// are baked into the binary rather than read from disk at runtime.
package prompts

import _ "embed"

//go:embed extract.system.md
var extractSystem string

// ExtractSystem returns the system prompt used by the extract stage.
func ExtractSystem() string { return extractSystem }

//go:embed cluster.canonical.system.md
var clusterCanonicalSystem string

// ClusterCanonicalSystem returns the embedded prompt for stage 2b.
func ClusterCanonicalSystem() string { return clusterCanonicalSystem }

//go:embed synthesize.identity.system.md
var synthesizeIdentitySystem string

func SynthesizeIdentitySystem() string { return synthesizeIdentitySystem }

//go:embed synthesize.rules.system.md
var synthesizeRulesSystem string

func SynthesizeRulesSystem() string { return synthesizeRulesSystem }

//go:embed synthesize.topics.system.md
var synthesizeTopicsSystem string

func SynthesizeTopicsSystem() string { return synthesizeTopicsSystem }

//go:embed synthesize.index.system.md
var synthesizeIndexSystem string

func SynthesizeIndexSystem() string { return synthesizeIndexSystem }

//go:embed canonicalize.slug.system.md
var canonicalizeSlugSystem string

// CanonicalizeSlugSystem returns the prompt used by the slug
// canonicalization stage to decide whether a candidate group of slugs
// describes the same topic and which one to keep.
func CanonicalizeSlugSystem() string { return canonicalizeSlugSystem }
