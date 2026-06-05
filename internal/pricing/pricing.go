package pricing

import "strings"

// Price is per-million-tokens, USD.
type Price struct {
	InputPerMTok  float64
	OutputPerMTok float64
}

// Table maps a model-id prefix to its price. Keys are matched with
// HasPrefix so date-suffixed model IDs (claude-haiku-4-5-20251001)
// still resolve.
var Table = map[string]Price{
	"claude-haiku-4-5":  {InputPerMTok: 1.0, OutputPerMTok: 5.0},
	"claude-opus-4-8":   {InputPerMTok: 15.0, OutputPerMTok: 75.0},
	"claude-opus-4-7":   {InputPerMTok: 15.0, OutputPerMTok: 75.0},
	"claude-sonnet-4-6": {InputPerMTok: 3.0, OutputPerMTok: 15.0},
	"voyage-3-lite":     {InputPerMTok: 0.02, OutputPerMTok: 0.0},
}

func Lookup(modelID string) (Price, bool) {
	for k, v := range Table {
		if strings.HasPrefix(modelID, k) {
			return v, true
		}
	}
	return Price{}, false
}

// EstimateTokens returns the rough token count for a byte payload,
// using Anthropic's published "~4 bytes per token" heuristic.
func EstimateTokens(bytes int) int { return (bytes + 3) / 4 }
