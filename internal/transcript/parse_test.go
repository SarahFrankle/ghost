package transcript

import "testing"

func TestParseReturnsTurns(t *testing.T) {
	turns, err := Parse("testdata/sample.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) == 0 {
		t.Fatal("expected at least one turn")
	}
	var sawUser, sawAssistant bool
	for _, tn := range turns {
		switch tn.Role {
		case "user":
			sawUser = true
		case "assistant":
			sawAssistant = true
		}
		if tn.Text == "" {
			t.Errorf("turn %d has empty text", tn.Index)
		}
	}
	if !sawUser || !sawAssistant {
		t.Fatalf("expected both roles; user=%v assistant=%v", sawUser, sawAssistant)
	}
}
