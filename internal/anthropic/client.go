// Package anthropic provides a small Client interface around an LLM provider,
// implemented by shelling out to the `claude` CLI. The interface stays narrow
// (one Complete call) so tests can substitute a fake without touching the
// process boundary.
package anthropic

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
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

// Complete runs `claude -p --model <model> --system-prompt <system>` and pipes
// the user payload on stdin. `--system-prompt` replaces the default Claude Code
// system prompt, which prevents the user's interactive context (CLAUDE.md
// includes, dynamic sections) from contaminating ghost's extract prompt.
// `--bare` would also disable hooks/MCP, but it forces ANTHROPIC_API_KEY auth —
// incompatible with the OAuth/keychain auth used by `claude` CLI subscriptions.
func (c *cliClient) Complete(ctx context.Context, model, system, user string) (string, error) {
	args := []string{
		"-p",
		"--model", model,
		"--system-prompt", system,
		"--output-format", "text",
		// Isolate from the user's interactive environment so observations
		// reflect transcript content, not the caller's shell context.
		// --setting-sources "" disables CLAUDE.md / MEMORY.md auto-discovery.
		"--setting-sources", "",
		"--disable-slash-commands",
		"--tools", "",
		// Don't let ghost's own subprocess calls land as new transcripts under
		// ~/.claude/projects/ — that would create a feedback loop where the
		// next compose run extracts from ghost's own prior extractions.
		"--no-session-persistence",
	}
	cmd := exec.CommandContext(ctx, c.bin, args...)

	// Suppress the ai-title sidecar. `--no-session-persistence` stops the
	// transcript write but NOT the background Haiku call that generates a
	// session title, which still drops a one-record `{"type":"ai-title"}`
	// JSONL under ~/.claude/projects/<cwd>/. Across many compose runs those
	// stubs accumulate in ghost's own project dir. CLAUDE_CODE_DISABLE_TERMINAL_TITLE
	// skips that title call in `claude -p`, so no sidecar is written.
	cmd.Env = append(os.Environ(), "CLAUDE_CODE_DISABLE_TERMINAL_TITLE=1")

	// Hand the payload to claude as a real file (fd 0), not a strings.Reader.
	// os/exec connects an *os.File stdin directly to the child; any other
	// io.Reader instead spawns a parent-side copier goroutine. Under a wide
	// concurrent fan-out those goroutines can be starved past claude's 3s
	// stdin-wait, so claude exits with "no stdin data received". A file the
	// child opens itself removes that race regardless of concurrency.
	stdin, err := writeTempStdin(user)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = stdin.Close()
		_ = os.Remove(stdin.Name())
	}()
	cmd.Stdin = stdin

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("claude exec: %w (stderr: %s)", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// writeTempStdin writes payload to a temp file and rewinds it to the start,
// returning the open *os.File for use as a child process's stdin. The caller
// owns closing and removing the file.
func writeTempStdin(payload string) (*os.File, error) {
	f, err := os.CreateTemp("", "ghost-stdin-*")
	if err != nil {
		return nil, fmt.Errorf("create stdin temp: %w", err)
	}
	if _, err := io.WriteString(f, payload); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return nil, fmt.Errorf("write stdin temp: %w", err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return nil, fmt.Errorf("rewind stdin temp: %w", err)
	}
	return f, nil
}
