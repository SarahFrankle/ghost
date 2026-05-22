package fingerprint

import "testing"

func TestComputeDeterministic(t *testing.T) {
	a := Compute("alpha", "beta", "gamma")
	b := Compute("alpha", "beta", "gamma")
	if a != b {
		t.Fatalf("non-deterministic: %s vs %s", a, b)
	}
	if len(a) != 64 {
		t.Fatalf("want 64 hex chars, got %d", len(a))
	}
}

func TestComputeOrderSensitive(t *testing.T) {
	a := Compute("alpha", "beta")
	b := Compute("beta", "alpha")
	if a == b {
		t.Fatalf("order should matter")
	}
}

func TestComputeEmptyPartsDistinct(t *testing.T) {
	// "a" + "" + "b" must not collide with "a" + "b" — empty parts are
	// preserved as slots so a missing field doesn't shift the meaning of
	// later parts.
	a := Compute("a", "", "b")
	b := Compute("a", "b")
	if a == b {
		t.Fatalf("empty slot must not collapse")
	}
}
