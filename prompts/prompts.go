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
