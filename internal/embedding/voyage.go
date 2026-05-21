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

const DefaultVoyageBaseURL = "https://api.voyageai.com/v1"

// Voyage implements Embedder via the Voyage AI HTTP API.
//
// Auth uses the VOYAGE_API_KEY environment variable when constructed
// via NewVoyageFromEnv. Tests inject APIKey, BaseURL, and HTTPClient
// directly to avoid touching the environment or the real endpoint.
type Voyage struct {
	APIKey     string
	BaseURL    string
	HTTPClient *http.Client
}

func NewVoyageFromEnv() (*Voyage, error) {
	key := os.Getenv("VOYAGE_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("VOYAGE_API_KEY not set (required for ghost cluster stage)")
	}
	return &Voyage{
		APIKey:     key,
		BaseURL:    DefaultVoyageBaseURL,
		HTTPClient: &http.Client{Timeout: 60 * time.Second},
	}, nil
}

type voyageRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type voyageResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
}

func (v *Voyage) Embed(ctx context.Context, model string, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	body, err := json.Marshal(voyageRequest{Model: model, Input: texts})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", v.BaseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+v.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := v.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("voyage embed: status %d: %s", resp.StatusCode, string(raw))
	}
	var parsed voyageResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("voyage embed: decode: %w", err)
	}
	if len(parsed.Data) != len(texts) {
		return nil, fmt.Errorf("voyage embed: returned %d vectors for %d inputs", len(parsed.Data), len(texts))
	}
	out := make([][]float32, len(texts))
	for _, d := range parsed.Data {
		if d.Index < 0 || d.Index >= len(out) {
			return nil, fmt.Errorf("voyage embed: bad index %d", d.Index)
		}
		out[d.Index] = d.Embedding
	}
	return out, nil
}
