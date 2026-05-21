// Package anthropic provides a small Client interface around an LLM provider,
// implemented by shelling out to the `claude` CLI. The interface stays narrow
// (one Complete call) so tests can substitute a fake without touching the
// process boundary.
package anthropic

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Client is the minimal surface ghost needs from an LLM provider.
// Complete sends a single (system, user) turn and returns the model's text.
type Client interface {
	Complete(ctx context.Context, model, system, user string) (string, error)
}

type cliClient struct {
	bin string
}

// New constructs a Client that shells out to the `claude` CLI. It returns
// an error if `claude` is not on PATH.
func New() (Client, error) {
	bin, err := exec.LookPath("claude")
	if err != nil {
		return nil, fmt.Errorf("claude CLI not found on PATH (install Claude Code, or set ANTHROPIC_API_KEY-backed alternative): %w", err)
	}
	return &cliClient{bin: bin}, nil
}

// Complete runs `claude -p --bare --model <model> --system-prompt <system>` and
// pipes the user payload on stdin. `--bare` skips hooks, MCP, plugin sync, and
// CLAUDE.md auto-discovery so ghost's prompt isn't contaminated by the user's
// interactive environment.
func (c *cliClient) Complete(ctx context.Context, model, system, user string) (string, error) {
	args := []string{
		"-p",
		"--bare",
		"--model", model,
		"--system-prompt", system,
		"--output-format", "text",
	}
	cmd := exec.CommandContext(ctx, c.bin, args...)
	cmd.Stdin = strings.NewReader(user)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("claude exec: %w (stderr: %s)", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}
