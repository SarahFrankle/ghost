// Package prompts embeds the system prompts shipped with ghost so they
// are baked into the binary rather than read from disk at runtime.
package prompts

import _ "embed"

//go:embed extract.system.md
var extractSystem string

// ExtractSystem returns the system prompt used by the extract stage.
func ExtractSystem() string { return extractSystem }
