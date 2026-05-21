package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const DefaultOllamaBaseURL = "http://localhost:11434"

// Ollama implements Embedder via the Ollama local HTTP API
// (POST /api/embed). It is a fully local alternative to Voyage; no
// credentials are needed because the daemon listens only on the
// loopback interface by default.
//
// Tests inject BaseURL and HTTPClient to point at an httptest server.
type Ollama struct {
	BaseURL    string
	HTTPClient *http.Client
}

func NewOllamaFromEnv() *Ollama {
	base := os.Getenv("OLLAMA_HOST")
	if base == "" {
		base = DefaultOllamaBaseURL
	}
	return &Ollama{
		BaseURL:    base,
		HTTPClient: &http.Client{Timeout: 120 * time.Second},
	}
}

type ollamaRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type ollamaResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}

func (o *Ollama) Embed(ctx context.Context, model string, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	body, err := json.Marshal(ollamaRequest{Model: model, Input: texts})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", o.BaseURL+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama embed: %w (is the daemon running at %s?)", err, o.BaseURL)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("ollama embed: status %d: %s", resp.StatusCode, string(raw))
	}
	var parsed ollamaResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("ollama embed: decode: %w", err)
	}
	if len(parsed.Embeddings) != len(texts) {
		return nil, fmt.Errorf("ollama embed: returned %d vectors for %d inputs (model %q may not be pulled — try `ollama pull %s`)", len(parsed.Embeddings), len(texts), model, model)
	}
	return parsed.Embeddings, nil
}
