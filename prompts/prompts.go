// Package prompts embeds the system prompts shipped with ghost so they
// are baked into the binary rather than read from disk at runtime.
package prompts

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
)

// hashOf returns the hex SHA-256 of s. Used to fingerprint embedded prompts
// so cached artifacts can detect prompt edits without manual cache busting.
func hashOf(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

//go:embed extract.system.md
var extractSystem string

// ExtractSystem returns the system prompt used by the extract stage.
func ExtractSystem() string { return extractSystem }

// ExtractSystemHash returns a stable hex hash of the extract prompt, so
// cached observations can be invalidated when the prompt changes.
func ExtractSystemHash() string { return hashOf(extractSystem) }

//go:embed synthesize.identity.system.md
var synthesizeIdentitySystem string

func SynthesizeIdentitySystem() string     { return synthesizeIdentitySystem }
func SynthesizeIdentitySystemHash() string { return hashOf(synthesizeIdentitySystem) }

//go:embed synthesize.rules.system.md
var synthesizeRulesSystem string

func SynthesizeRulesSystem() string     { return synthesizeRulesSystem }
func SynthesizeRulesSystemHash() string { return hashOf(synthesizeRulesSystem) }

//go:embed synthesize.topics.system.md
var synthesizeTopicsSystem string

func SynthesizeTopicsSystem() string     { return synthesizeTopicsSystem }
func SynthesizeTopicsSystemHash() string { return hashOf(synthesizeTopicsSystem) }

//go:embed synthesize.index.system.md
var synthesizeIndexSystem string

func SynthesizeIndexSystem() string     { return synthesizeIndexSystem }
func SynthesizeIndexSystemHash() string { return hashOf(synthesizeIndexSystem) }

//go:embed synthesize.generality.system.md
var synthesizeGeneralitySystem string

func SynthesizeGeneralitySystem() string     { return synthesizeGeneralitySystem }
func SynthesizeGeneralitySystemHash() string { return hashOf(synthesizeGeneralitySystem) }

//go:embed cluster.label.system.md
var clusterLabelSystem string

func ClusterLabelSystem() string     { return clusterLabelSystem }
func ClusterLabelSystemHash() string { return hashOf(clusterLabelSystem) }

//go:embed cluster.theme.identify.system.md
var clusterThemeIdentifySystem string

// ClusterThemeIdentifySystem returns the prompt for theme pass 1: distilling
// the full label vocabulary into 15–25 canonical theme names.
func ClusterThemeIdentifySystem() string     { return clusterThemeIdentifySystem }
func ClusterThemeIdentifySystemHash() string { return hashOf(clusterThemeIdentifySystem) }

//go:embed cluster.theme.map.system.md
var clusterThemeMapSystem string

// ClusterThemeMapSystem returns the prompt for theme pass 2: mapping a batch of
// labels onto the fixed theme set produced by pass 1.
func ClusterThemeMapSystem() string     { return clusterThemeMapSystem }
func ClusterThemeMapSystemHash() string { return hashOf(clusterThemeMapSystem) }
