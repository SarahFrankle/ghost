package extract

import "testing"

func TestValidateAcceptsAllKinds(t *testing.T) {
	cases := []Observation{
		{Kind: "identity", Text: "works at Miro", Evidence: "turn 1"},
		{Kind: "preference", Text: "don't mock the database", Evidence: "turn 9"},
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
		{Kind: "identity", Text: "", Evidence: "y"},
		{Kind: "identity", Text: "x"},
	}
	for _, o := range bad {
		if err := o.Validate(); err == nil {
			t.Errorf("expected error for %+v", o)
		}
	}
}

func TestPreferenceKindValidatesWithoutTopicField(t *testing.T) {
	o := Observation{Kind: "preference", Text: "prefer table-driven tests", Evidence: "turn 4: prefer tables"}
	if err := o.Validate(); err != nil {
		t.Fatalf("preference-kind observation should validate with only required fields: %v", err)
	}
}

func TestValidate_PreferenceKindAccepted(t *testing.T) {
	o := Observation{Kind: "preference", Text: "always run make check before committing", Evidence: "turn 3: run make check"}
	if err := o.Validate(); err != nil {
		t.Fatalf("preference kind should be valid: %v", err)
	}
}

func TestValidate_RuleAndTopicRejected(t *testing.T) {
	for _, k := range []Kind{"rule", "topic"} {
		o := Observation{Kind: k, Text: "x", Evidence: "turn 1: x"}
		if err := o.Validate(); err == nil {
			t.Fatalf("kind %q should no longer be valid", k)
		}
	}
}
