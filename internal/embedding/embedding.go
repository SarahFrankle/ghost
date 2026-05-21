// Package embedding defines the Embedder interface and shared utilities used
// across stage 2's synthesis pipeline.
package embedding

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"math"
)

// Embedder is the minimal surface stage 2 needs. Implementations must return
// one vector per input text, in the same order.
type Embedder interface {
	Embed(ctx context.Context, model string, texts []string) ([][]float32, error)
}

// ObservationHash is the cache key for an observation's embedding. Kind +
// sub_key + text fully determine the embedded payload.
func ObservationHash(kind, subKey, text string) string {
	sum := sha256.Sum256([]byte(kind + "|" + subKey + "|" + text))
	return hex.EncodeToString(sum[:])
}

// Cosine returns the cosine similarity of two vectors, or 0 if either has zero
// magnitude (defensive — Voyage does not normally return zero vectors but a
// malformed response shouldn't NaN-poison the cluster step).
func Cosine(a, b []float32) float32 {
	if len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return float32(dot / (math.Sqrt(na) * math.Sqrt(nb)))
}
