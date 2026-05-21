package embedding

import (
	"math"
	"testing"
)

func TestObservationHashIsStable(t *testing.T) {
	a := ObservationHash("rule", "", "prefer integration tests")
	b := ObservationHash("rule", "", "prefer integration tests")
	if a != b {
		t.Fatalf("hash unstable: %s vs %s", a, b)
	}
	if len(a) != 64 {
		t.Fatalf("hash length = %d, want 64 hex chars", len(a))
	}
	if ObservationHash("rule", "", "x") == ObservationHash("identity", "", "x") {
		t.Fatalf("kind not part of hash")
	}
	if ObservationHash("voice", "cli-chat", "x") == ObservationHash("voice", "slack", "x") {
		t.Fatalf("sub_key not part of hash")
	}
}

func TestCosine(t *testing.T) {
	got := Cosine([]float32{1, 0}, []float32{1, 0})
	if math.Abs(float64(got)-1.0) > 1e-6 {
		t.Fatalf("identical vectors cosine = %v, want 1", got)
	}
	got = Cosine([]float32{1, 0}, []float32{0, 1})
	if math.Abs(float64(got)) > 1e-6 {
		t.Fatalf("orthogonal cosine = %v, want 0", got)
	}
	if Cosine([]float32{1, 0}, []float32{0, 0}) != 0 {
		t.Fatalf("zero-vector cosine must be 0, not NaN")
	}
}
