package extract

import "github.com/SarahFrankle/ghost/prompts"

// SystemPrompt returns the embedded extract system prompt.
func SystemPrompt() string { return prompts.ExtractSystem() }
