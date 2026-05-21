package extract

import "testing"

func TestValidateAcceptsAllKinds(t *testing.T) {
	cases := []Observation{
		{Kind: "identity", Text: "works at Miro", Evidence: "turn 1"},
		{Kind: "rule", Text: "don't mock the database", Evidence: "turn 9"},
		{Kind: "topic", Topic: "testing", Text: "integration > mocks", Evidence: "turn 9"},
		{Kind: "voice", Context: "cli-chat", Text: "lowercase fragments", Evidence: "turns 3,7"},
	}
	for _, c := range cases {
		if err := c.Validate(); err != nil {
			t.Errorf("Validate(%+v) = %v", c, err)
		}
	}
}

func TestValidateRejectsBadKind(t *testing.T) {
	o := Observation{Kind: "feeling", Text: "x", Evidence: "y"}
	if err := o.Validate(); err == nil {
		t.Fatal("expected error for unknown kind")
	}
}

func TestValidateRejectsMissingSubKey(t *testing.T) {
	bad := []Observation{
		{Kind: "voice", Text: "x", Evidence: "y"},
		{Kind: "topic", Text: "x", Evidence: "y"},
		{Kind: "identity", Text: "", Evidence: "y"},
		{Kind: "identity", Text: "x"},
	}
	for _, o := range bad {
		if err := o.Validate(); err == nil {
			t.Errorf("expected error for %+v", o)
		}
	}
}
