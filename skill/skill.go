// Package skill ships the Claude Code skill descriptor embedded in
// the ghost binary and installs it under ~/.claude/skills/ghost/.
package skill

import _ "embed"

//go:embed SKILL.md
var skillMD string

// Content returns the SKILL.md body shipped with this binary.
func Content() string { return skillMD }

// DefaultInstallDir is where Claude Code expects user skills.
const DefaultInstallDir = "~/.claude/skills/ghost"
