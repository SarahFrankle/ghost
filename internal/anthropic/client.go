// Package anthropic provides a small interface around the Anthropic Go SDK,
// exposing only the single Complete operation that ghost needs. The interface
// is kept narrow so consumers can be tested with fakes without depending on
// the SDK's evolving surface.
package anthropic

import (
	"context"
	"fmt"
	"os"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// Client is the minimal surface ghost needs from an LLM provider.
// Complete sends a single (system, user) turn and returns the
// concatenated text content of the model's response.
type Client interface {
	Complete(ctx context.Context, model, system, user string) (string, error)
}

type sdkClient struct {
	c *sdk.Client
}

// New constructs a Client backed by the official Anthropic Go SDK.
// It returns an error if the ANTHROPIC_API_KEY environment variable is unset.
func New() (Client, error) {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("ANTHROPIC_API_KEY not set")
	}
	c := sdk.NewClient(option.WithAPIKey(key))
	return &sdkClient{c: &c}, nil
}

// Complete issues a single Messages request with the given system prompt
// and user message, and returns the concatenated text of all text blocks
// in the response. Non-text blocks (e.g. thinking, tool use) are ignored.
func (s *sdkClient) Complete(ctx context.Context, model, system, user string) (string, error) {
	resp, err := s.c.Messages.New(ctx, sdk.MessageNewParams{
		Model:     sdk.Model(model),
		MaxTokens: 4096,
		System: []sdk.TextBlockParam{
			{Text: system},
		},
		Messages: []sdk.MessageParam{
			sdk.NewUserMessage(sdk.NewTextBlock(user)),
		},
	})
	if err != nil {
		return "", err
	}
	var out string
	for _, blk := range resp.Content {
		if blk.Type == "text" {
			out += blk.Text
		}
	}
	return out, nil
}
