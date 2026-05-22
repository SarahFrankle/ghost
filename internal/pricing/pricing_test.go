package pricing

import "testing"

func TestLookupResolvesDatedModelID(t *testing.T) {
	p, ok := Lookup("claude-haiku-4-5-20251001")
	if !ok {
		t.Fatal("Lookup did not resolve dated model id")
	}
	if p.InputPerMTok != 1.0 {
		t.Fatalf("unexpected price: %+v", p)
	}
}

func TestEstimateTokensRoughlyBytesOver4(t *testing.T) {
	if got := EstimateTokens(40); got != 10 {
		t.Fatalf("want 10, got %d", got)
	}
	if got := EstimateTokens(3); got != 1 {
		t.Fatalf("rounding up failed, got %d", got)
	}
}
